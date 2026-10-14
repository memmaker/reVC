package gameimport

// InstallShield cabinets (data1.hdr + dataN.cab), the InstallShield 6 and later layout as
// libunshield describes it (lib/cabfile.h, lib/file.c): the .hdr holds a common header, a
// cabinet descriptor with the file table (directory names, then 0x57-byte file descriptors),
// each descriptor naming the volume its bytes lie in. Compressed files are a sequence of
// chunks, uint16 size + that many bytes of raw deflate. A file split over volumes continues at
// the next volume's first-file offset.

import (
	"bytes"
	"compress/flate"
	"encoding/binary"
	"fmt"
	"io"
	"path"
	"strings"
)

const (
	isSignature     = "ISc("
	isCommonHeader  = 20
	isCabDescOffset = 12 // cab_descriptor_offset in the common header
	isFileDescSize  = 0x57
	isFlagSplit     = 1
	isFlagObfuscate = 2
	isFlagCompress  = 4
	isFlagInvalid   = 8
	isMaxFileGroups = 71
	isVolumeHeader  = 64 // v6+: 16 uint32 after the common header
)

// listInstallShield lists the files a data1.hdr describes. Not an InstallShield header: no
// files, no error.
func listInstallShield(r io.ReaderAt, size int64, name string, siblings []file) ([]file, error) {
	hdr := make([]byte, size)
	if _, err := r.ReadAt(hdr, 0); err != nil && err != io.EOF {
		return nil, err
	}
	le := binary.LittleEndian
	if len(hdr) < isCommonHeader || string(hdr[:4]) != isSignature {
		return nil, nil
	}
	version := le.Uint32(hdr[4:])
	major := uint32(0)
	switch version >> 24 {
	case 1:
		major = (version >> 12) & 0xf
	case 2, 4:
		major = version & 0xffff
		if major != 0 {
			major /= 100
		}
	}
	if major < 6 {
		return nil, fmt.Errorf("%s: InstallShield %d cabinet, only 6 and later are supported", name, major)
	}
	cab := int(le.Uint32(hdr[isCabDescOffset:]))
	if cab+0x30 > len(hdr) {
		return nil, fmt.Errorf("%s: truncated InstallShield header", name)
	}
	d := hdr[cab:]
	fileTableOff := int(le.Uint32(d[0xc:]))
	dirCount := int(le.Uint32(d[0x1c:]))
	fileCount := int(le.Uint32(d[0x28:]))
	fileTableOff2 := int(le.Uint32(d[0x2c:]))
	ft := cab + fileTableOff
	cstr := func(at int) string {
		if at < 0 || at >= len(hdr) {
			return ""
		}
		end := bytes.IndexByte(hdr[at:], 0)
		if end < 0 {
			end = len(hdr) - at
		}
		return string(hdr[at : at+end])
	}
	// the volumes beside the header: data1.cab, data2.cab ... by their number
	prefix := strings.TrimSuffix(strings.ToLower(name), ".hdr")
	volumes := map[int]file{}
	for _, s := range siblings {
		l := strings.ToLower(s.name)
		if strings.HasSuffix(l, ".cab") && strings.HasPrefix(l, strings.TrimRight(prefix, "0123456789")) {
			var n int
			fmt.Sscanf(strings.TrimSuffix(l, ".cab")[len(strings.TrimRight(prefix, "0123456789")):], "%d", &n)
			volumes[n] = s
		}
	}
	var out []file
	for i := 0; i < fileCount; i++ {
		at := ft + fileTableOff2 + i*isFileDescSize
		if at+isFileDescSize > len(hdr) {
			break
		}
		p := hdr[at:]
		flags := le.Uint16(p)
		if flags&isFlagInvalid != 0 {
			continue
		}
		fd := isFile{
			index:      i,
			flags:      flags,
			expanded:   int64(le.Uint64(p[2:])),
			compressed: int64(le.Uint64(p[10:])),
			offset:     int64(le.Uint64(p[18:])),
			volume:     int(le.Uint16(p[0x55:])),
			volumes:    volumes,
		}
		nameOff := int(le.Uint32(p[0x3a:]))
		dirIdx := int(le.Uint16(p[0x3e:]))
		fname := cstr(ft + nameOff)
		dir := ""
		if dirIdx < dirCount && ft+dirIdx*4+4 <= len(hdr) {
			dir = cstr(ft + int(le.Uint32(hdr[ft+dirIdx*4:])))
		}
		if fname == "" {
			continue
		}
		full := path.Join(strings.ReplaceAll(dir, "\\", "/"), fname)
		out = append(out, file{name: full, size: fd.expanded, open: fd.open})
	}
	return out, nil
}

type isFile struct {
	index                        int
	flags                        uint16
	expanded, compressed, offset int64
	volume                       int
	volumes                      map[int]file
}

// open extracts the file whole into memory, following it across volumes.
func (f isFile) open() (io.ReaderAt, error) {
	out := make([]byte, 0, f.expanded)
	vol := f.volume
	// reader over the compressed bytes of this file, volume by volume
	var cur io.ReaderAt
	var pos, left int64
	openVol := func(first bool) error {
		v, ok := f.volumes[vol]
		if !ok {
			return fmt.Errorf("InstallShield volume data%d.cab missing", vol)
		}
		r, err := v.open()
		if err != nil {
			return err
		}
		vh := make([]byte, isCommonHeader+isVolumeHeader)
		if _, err := r.ReadAt(vh, 0); err != nil {
			return err
		}
		le := binary.LittleEndian
		u32 := func(i int) int64 { return int64(le.Uint32(vh[isCommonHeader+i*4:])) }
		firstIdx, lastIdx := int(u32(2)), int(u32(3))
		pos, left = f.offset, f.compressed
		if f.flags&isFlagSplit != 0 || !first {
			switch f.index {
			case lastIdx:
				pos, left = u32(10)|u32(11)<<32, u32(14)|u32(15)<<32
			case firstIdx:
				pos, left = u32(4)|u32(5)<<32, u32(8)|u32(9)<<32
			}
		}
		if f.flags&isFlagCompress == 0 && left == 0 {
			left = f.expanded
		}
		cur = r
		return nil
	}
	if err := openVol(true); err != nil {
		return nil, err
	}
	seed := uint32(0)
	read := func(n int64) ([]byte, error) {
		b := make([]byte, 0, n)
		for int64(len(b)) < n {
			if left == 0 {
				vol++
				if err := openVol(false); err != nil {
					return nil, err
				}
			}
			k := min(n-int64(len(b)), left)
			chunk := make([]byte, k)
			if _, err := cur.ReadAt(chunk, pos); err != nil && err != io.EOF {
				return nil, err
			}
			pos += k
			left -= k
			b = append(b, chunk...)
		}
		if f.flags&isFlagObfuscate != 0 {
			for i := range b {
				x := b[i] ^ 0xd5
				b[i] = (x>>2 | x<<6) - byte(seed%0x47)
				seed++
			}
		}
		return b, nil
	}
	if f.flags&isFlagCompress == 0 {
		b, err := read(f.expanded)
		return bytes.NewReader(b), err
	}
	for int64(len(out)) < f.expanded {
		sz, err := read(2)
		if err != nil {
			return nil, err
		}
		n := int64(binary.LittleEndian.Uint16(sz))
		if n == 0 {
			return nil, fmt.Errorf("InstallShield chunk of size 0")
		}
		c, err := read(n)
		if err != nil {
			return nil, err
		}
		b, err := io.ReadAll(flate.NewReader(bytes.NewReader(c)))
		if err != nil && len(b) == 0 {
			return nil, fmt.Errorf("InstallShield chunk: %w", err)
		}
		out = append(out, b...)
	}
	return bytes.NewReader(out[:f.expanded]), nil
}
