package gameimport

import (
	"encoding/binary"
	"errors"
	"io"
	"strings"
	"unicode/utf16"
)

// ISO 9660 and raw CD sector layout.
const (
	isoSector         = 2048
	rawSector         = 2352
	firstVolumeSector = 16 // the primary volume descriptor
	rawHeaderSize     = 16 // sync pattern (12 bytes), address (3) and mode (1)
	rawSyncSize       = 12
	rawModeAt         = 15
	rawMode2          = 2
	rawMode1Skip      = 16
	rawMode2Skip      = 24 // mode 2 form 1: an 8-byte subheader after the header
	pvdRootRecord     = 156
	isoMagicSize      = 6
	maxISODepth       = 16
	recordExtent      = 2  // little-endian start sector
	recordSize        = 10 // little-endian byte size
	recordFlags       = 25
	recordNameLength  = 32
	recordName        = 33
	minRecordSize     = 34
	flagDirectory     = 2
)

// listCD lists an ISO 9660 image, or a raw BIN track (2352-byte sectors, mode 1 or mode 2
// form 1) by its 2048 user bytes per sector, or a FAT disk image. Not an image: no files,
// no error.
func listCD(r io.ReaderAt, size int64) ([]file, error) {
	if !isISO(r) {
		var s [rawHeaderSize]byte
		if _, err := r.ReadAt(s[:], firstVolumeSector*rawSector); err != nil || string(s[:rawSyncSize]) != "\x00\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\x00" {
			return listFAT(r, size) // a floppy (or hard disk partition) image
		}
		raw := rawCD{r, rawMode1Skip}
		if s[rawModeAt] == rawMode2 {
			raw.skip = rawMode2Skip
		}
		if !isISO(raw) {
			return nil, nil
		}
		r = raw
	}
	// The primary descriptor's names are 8.3 and upper-case (Setup-1.bin becomes
	// SETUP_1.BIN); a Joliet supplementary descriptor (type 2, UCS-2 escape %/@, %/C or
	// %/E), when there is one, has the names as written.
	vd, joliet := make([]byte, isoSector), false
	r.ReadAt(vd, firstVolumeSector*isoSector)
	for i := int64(firstVolumeSector + 1); ; i++ {
		b := make([]byte, isoSector)
		if _, err := r.ReadAt(b, i*isoSector); err != nil || b[0] == 255 || string(b[1:6]) != "CD001" {
			break
		}
		if b[0] == 2 && b[88] == '%' && b[89] == '/' && (b[90] == '@' || b[90] == 'C' || b[90] == 'E') {
			vd, joliet = b, true
			break
		}
	}
	var out []file
	return out, isoWalk(r, binary.LittleEndian.Uint32(vd[pvdRootRecord+recordExtent:]), binary.LittleEndian.Uint32(vd[pvdRootRecord+recordSize:]), "", &out, 0, joliet)
}

func isISO(r io.ReaderAt) bool {
	var b [isoMagicSize]byte
	_, err := r.ReadAt(b[:], firstVolumeSector*isoSector)
	return err == nil && string(b[:]) == "\x01CD001"
}

// isoWalk appends the files of the directory at lba (size bytes) and below it, names without
// the ";1" version.
func isoWalk(r io.ReaderAt, lba, size uint32, dir string, out *[]file, depth int, joliet bool) error {
	if depth > maxISODepth {
		return errors.New("an ISO directory tree too deep")
	}
	buf := make([]byte, size)
	if _, err := r.ReadAt(buf, int64(lba)*isoSector); err != nil && err != io.EOF {
		return err
	}
	for off := 0; off < len(buf); {
		n := int(buf[off])
		if n == 0 { // no record crosses a sector: the rest of this one is padding
			off = (off/isoSector + 1) * isoSector
			continue
		}
		if n < minRecordSize || off+n > len(buf) || recordName+int(buf[off+recordNameLength]) > n {
			return errors.New("a broken ISO directory")
		}
		rec := buf[off : off+n]
		off += n
		name := string(rec[recordName : recordName+int(rec[recordNameLength])])
		if name == "\x00" || name == "\x01" { // . and ..
			continue
		}
		if joliet {
			u := make([]uint16, len(name)/2)
			for i := range u {
				u[i] = binary.BigEndian.Uint16([]byte(name[2*i:]))
			}
			name = string(utf16.Decode(u))
		}
		if i := strings.IndexByte(name, ';'); i >= 0 {
			name = name[:i]
		}
		name = dir + strings.TrimSuffix(name, ".")
		at, n32 := binary.LittleEndian.Uint32(rec[recordExtent:]), binary.LittleEndian.Uint32(rec[recordSize:])
		if rec[recordFlags]&flagDirectory != 0 {
			if err := isoWalk(r, at, n32, name+"/", out, depth+1, joliet); err != nil {
				return err
			}
			continue
		}
		*out = append(*out, file{name, int64(n32), func() (io.ReaderAt, error) {
			return io.NewSectionReader(r, int64(at)*isoSector, int64(n32)), nil
		}})
	}
	return nil
}

// rawCD reads a raw track's user data: 2048 bytes at skip in every 2352-byte sector.
type rawCD struct {
	r    io.ReaderAt
	skip int64
}

func (c rawCD) ReadAt(p []byte, off int64) (int, error) {
	n := 0
	for n < len(p) {
		sec, in := off/isoSector, off%isoSector
		m, err := c.r.ReadAt(p[n:min(len(p), n+int(isoSector-in))], sec*rawSector+c.skip+in)
		n += m
		off += int64(m)
		if err != nil {
			return n, err
		}
	}
	return n, nil
}
