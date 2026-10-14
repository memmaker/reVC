package gameimport

// Inno Setup, the subset that gets files out of a GOG installer: versions 5.2 to 6.4, stored,
// zlib, bzip2, LZMA1 and LZMA2 chunks, data inside the .exe or in setup-N.bin slices beside
// it. The layout follows innoextract's description of the format (setup/header.cpp and
// friends): the loader table points at a 64-byte version string and two CRC'd blocks, the
// first holding the setup header and its entry lists (we read through them to the file
// entries), the second the data entries saying where each file's bytes lie in which chunk.

import (
	"bytes"
	"cmp"
	"compress/bzip2"
	"compress/zlib"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"math"
	"path"
	"slices"
	"strings"
	"unicode/utf16"

	"github.com/ulikunitz/xz/lzma"
)

// Inno Setup layout sizes and markers.
const (
	maxLoaderSearch    = 16 << 20 // the loader table lies in the first 16 MiB of the .exe
	loaderMagicSize    = 12
	loaderTableSize    = 28
	loaderHeaderOffset = 20
	loaderDataOffset   = 24
	versionStringSize  = 64
	setupHeaderStrings = 27 // app name ... close applications filter
	leadBytesSize      = 32
	windowsVersionSize = 20
	passwordHashSize   = 4 + 44 // 6.4+: hash and salt
	legacyPasswordSize = 20 + 8 // SHA-1 and salt
	sha1Size           = 20
	md5Size            = 16
	sha256Size         = 32
	md5HexSize         = 32
	fileEntryFixed     = 20 // min/max versions before the location
	blockHeaderSize    = 9  // CRC32, stored size, compressed flag
	blockPieceSize     = 4096
	lzmaHeaderSize     = 13
	lzma2MaxProperty   = 40
	lzma2DictShift     = 11
	sliceHeaderSize    = 12
	sliceEndOfFile     = 26 // the DOS end-of-file byte after "idsk"
	sliceHeaderEOFAt   = 7
	sliceHeaderSizeAt  = 8
	flagX86Filter      = 1 << 4
	flagEncrypted      = 1 << 6
	flagSolidChunk     = 1 << 7
	byteBits           = 8
	everyVersionFlags  = 5 + 4 + 1 + 3 + 19 + 4 + 3 // the header flags every version from 5.5 has
	methodStored       = 0
	methodZlib         = 1
	methodBzip2        = 2
	methodLZMA1        = 3
	methodLZMA2        = 4
)

// iv packs an Inno Setup version a.b.c.d into one comparable number.
func iv(a, b, c, d uint32) uint32 {
	return a<<(3*byteBits) | b<<(2*byteBits) | c<<byteBits | d
}

// listInno lists the files of the Inno Setup installer r (named name, among siblings for its
// slices). Not an installer: no files, no error.
func listInno(r io.ReaderAt, size int64, name string, siblings []file) ([]file, error) {
	head := make([]byte, min(size, maxLoaderSearch))
	n, _ := r.ReadAt(head, 0)
	head = head[:n]
	at := bytes.Index(head, []byte("rDlPtS\xcd\xe6\xd7{\x0b*"))
	if at < 0 {
		at = bytes.Index(head, []byte("nS5W7dT\x83\xaa\x1b\x0fj"))
	}
	if at < 0 || at+loaderMagicSize+loaderTableSize > len(head) {
		return nil, nil
	}
	t := head[at+loaderMagicSize:]
	le := binary.LittleEndian
	if rev := le.Uint32(t); rev != 1 {
		return nil, fmt.Errorf("%s: Inno Setup loader table revision %d, not supported", name, rev)
	}
	hdrOff, dataOff := int64(le.Uint32(t[loaderHeaderOffset:])), int64(le.Uint32(t[loaderDataOffset:]))

	vs := make([]byte, versionStringSize)
	if _, err := r.ReadAt(vs, hdrOff); err != nil {
		return nil, err
	}
	vstr := string(bytes.TrimRight(vs, "\x00"))
	var a, b, c, d uint32
	i := strings.IndexByte(vstr, '(')
	if !strings.HasPrefix(vstr, "Inno Setup Setup Data") || i < 0 {
		return nil, fmt.Errorf("%s: unknown Inno Setup data version %q", name, vstr)
	}
	fmt.Sscanf(vstr[i:], "(%d.%d.%d.%d", &a, &b, &c, &d)
	v := iv(a, b, c, d)
	uni := strings.Contains(vstr, "(u)") || strings.Contains(vstr, "(U)") || a >= 6
	if v < iv(5, 2, 0, 0) {
		return nil, fmt.Errorf("%s: %s is older than Inno Setup 5.2, not supported", name, vstr)
	}
	fail := func(err error) ([]file, error) { return nil, fmt.Errorf("%s (%s): %w", name, vstr, err) }

	b1, next, err := innoBlock(r, hdrOff+versionStringSize)
	if err != nil {
		return fail(err)
	}
	b2, _, err := innoBlock(r, next)
	if err != nil {
		return fail(err)
	}

	// The setup header, skipped field by field up to what we need.
	h := &rd{b: b1, uni: uni}
	nstr := setupHeaderStrings
	if v < iv(5, 3, 8, 0) {
		nstr-- // create uninstall registry key
	}
	if v < iv(5, 3, 10, 0) {
		nstr-- // uninstallable
	}
	if v < iv(5, 5, 0, 0) {
		nstr-- // close applications filter
	}
	if v >= iv(5, 2, 1, 0) && v < iv(5, 3, 10, 0) {
		nstr++ // uninstaller signature
	}
	if v >= iv(5, 5, 6, 0) {
		nstr++
	}
	if v >= iv(5, 6, 1, 0) {
		nstr += 2
	}
	if v >= iv(6, 3, 0, 0) {
		nstr += 2
	}
	h.strs(nstr + 4) // + license, info before, info after, compiled code
	if !uni {
		h.skip(leadBytesSize)
	}
	var cnt [16]int // languages, messages, permissions, types, components, tasks, directories, files, data entries, ...
	for i := range cnt {
		cnt[i] = int(h.u32())
	}
	h.skip(windowsVersionSize)
	if v < iv(6, 4, 0, 1) {
		h.skip(8) // back colors
	}
	if v < iv(5, 5, 7, 0) {
		h.skip(4) // image back color
	}
	if v >= iv(6, 0, 0, 0) {
		h.skip(9) // wizard style, resize percents
	}
	if v >= iv(5, 5, 7, 0) {
		h.skip(1) // image alpha format
	}
	if v >= iv(6, 4, 0, 0) {
		h.skip(passwordHashSize)
	} else if v >= iv(5, 3, 9, 0) {
		h.skip(legacyPasswordSize)
	} else {
		h.skip(md5Size + 8) // MD5 and salt
	}
	h.skip(8) // extra disk space
	slicesPerDisk := int(h.u32())
	h.skip(3) // uninstall log mode, dir exists warning, privileges required
	if v >= iv(5, 7, 0, 0) {
		h.skip(1) // privileges override
	}
	h.skip(2) // language dialog, detection
	compression := h.u8()
	if v < iv(6, 3, 0, 0) {
		h.skip(2) // architectures
	}
	if v >= iv(5, 2, 1, 0) && v < iv(5, 3, 10, 0) {
		h.skip(8) // signed uninstaller size, checksum
	}
	if v >= iv(5, 3, 3, 0) {
		h.skip(2) // disable dir/group page
	}
	if v >= iv(5, 5, 0, 0) {
		h.skip(8) // uninstall display size
	} else if v >= iv(5, 3, 6, 0) {
		h.skip(4)
	}
	h.flags(headerFlagCount(v, uni))

	langFixed := 4 + 16 + 1
	if !uni || v < iv(5, 3, 0, 0) {
		langFixed += 4 // code page
	}
	for _, e := range []struct{ strs, fixed int }{
		{10, langFixed},                     // languages
		{2, 4},                              // messages
		{1, 0},                              // permissions
		{4, windowsVersionSize + 1 + 1 + 8}, // types
		{5, 8 + 4 + 1 + windowsVersionSize + 1 + 8}, // components
		{6, 4 + 1 + windowsVersionSize + 1},         // tasks
		{7, 4 + windowsVersionSize + 2 + 1},         // directories
	} {
		for range cnt[0] {
			h.strs(e.strs)
			h.skip(e.fixed)
		}
		cnt = [16]int(append(cnt[1:], 0))
	}
	// GOG Galaxy installers store a file as parts: the first entry's BeforeInstall is
	// before_install('md5', 'real name', parts), each part's AfterInstall
	// after_install('md5', size, unpacked size), and each part's bytes are zlib streams.
	type entry struct {
		dest   string
		locs   []uint32
		size   int64
		galaxy bool
		md5    string
	}
	var ents []*entry
	var cur *entry
	parts := 0
	for range cnt[0] { // files
		h.str() // source
		dest := h.str()
		h.str() // font
		if v >= iv(5, 2, 5, 0) {
			h.str() // assembly
		}
		h.strs(4) // components, tasks, languages, check
		after, before := h.str(), h.str()
		h.skip(fileEntryFixed)
		loc := h.u32()
		h.skip(4 + 8 + 2 + 4 + 1) // attributes, external size, permission, flags, type
		if a := call(before, "before_install", "before_install_dependency"); a != nil {
			cur = &entry{dest: dest, galaxy: true, md5: a[0]}
			if len(a) > 1 && a[1] != "" {
				cur.dest = a[1]
			}
			parts = 1
			if len(a) > 2 {
				fmt.Sscan(a[2], &parts)
			}
			ents = append(ents, cur)
		}
		if a := call(after, "after_install", "after_install_dependency"); a != nil && parts > 0 && len(a) > 2 {
			var n int64
			fmt.Sscan(a[2], &n)
			cur.locs, cur.size = append(cur.locs, loc), cur.size+n
			parts--
			continue
		}
		parts = 0
		ents = append(ents, &entry{dest: dest, locs: []uint32{loc}})
	}
	if h.err != nil {
		return fail(h.err)
	}

	type data struct {
		slice, chunkOff  uint32
		off, size, csize uint64
		sum              []byte
		flags            uint16
	}
	g := &rd{b: b2}
	var dat []data
	for range cnt[1] {
		var e data
		e.slice = g.u32()
		g.u32() // last slice
		e.chunkOff = g.u32()
		e.off, e.size, e.csize = g.u64(), g.u64(), g.u64()
		switch {
		case v >= iv(6, 4, 0, 0):
			e.sum = g.bytes(sha256Size)
		case v >= iv(5, 3, 9, 0):
			e.sum = g.bytes(sha1Size)
		default:
			e.sum = g.bytes(md5Size)
		}
		g.skip(16) // file time, file version
		e.flags = uint16(g.u8()) | uint16(g.u8())<<byteBits
		if v >= iv(6, 3, 0, 0) {
			g.skip(1) // sign mode
		}
		dat = append(dat, e)
	}
	if g.err != nil {
		return fail(g.err)
	}

	// chunk returns a chunk's stored bytes, read on from slice to slice.
	chunk := func(e data) (io.Reader, error) {
		var src io.Reader
		if dataOff != 0 {
			src = io.NewSectionReader(r, dataOff+int64(e.chunkOff), 4+int64(e.csize))
		} else {
			base := strings.TrimSuffix(path.Base(name), path.Ext(name))
			src = &sliceReader{n: int(e.slice), pos: int64(e.chunkOff), open: func(n int) (io.ReaderAt, error) {
				sn := fmt.Sprintf("%s-%d.bin", base, n+1)
				if slicesPerDisk > 1 {
					sn = fmt.Sprintf("%s-%d%c.bin", base, n/slicesPerDisk+1, 'a'+n%slicesPerDisk)
				}
				for _, f := range siblings {
					if path.Dir(f.name) == path.Dir(name) && strings.EqualFold(path.Base(f.name), sn) {
						return f.open()
					}
				}
				return nil, errors.New("missing installer slice " + sn)
			}}
		}
		var magic [4]byte
		if _, err := io.ReadFull(src, magic[:]); err != nil {
			return nil, err
		}
		if string(magic[:]) != "zlb\x1a" {
			return nil, errors.New("bad chunk magic")
		}
		return io.LimitReader(src, int64(e.csize)), nil
	}

	// Files are read in the order they lie in the chunks (out is sorted so), and a solid
	// chunk's decoder is kept from one file to the next: skipping forward to the next file
	// costs only the bytes between, where restarting would decompress from the chunk's start.
	var dec struct {
		slice, chunkOff uint32
		pos             uint64
		r               io.Reader
	}
	// extract returns the bytes of data entry e.
	extract := func(e data) ([]byte, error) {
		if e.flags&flagEncrypted != 0 {
			return nil, errors.New("encrypted (password-protected) installer, not supported")
		}
		if e.flags&flagX86Filter != 0 {
			return nil, errors.New("x86 call filter not supported")
		}
		src := dec.r
		if src == nil || dec.slice != e.slice || dec.chunkOff != e.chunkOff || dec.pos > e.off {
			var err error
			if src, err = chunk(e); err != nil {
				return nil, err
			}
			if e.flags&flagSolidChunk != 0 {
				if src, err = decompress(src, compression, 0); err != nil {
					return nil, err
				}
			}
			dec.slice, dec.chunkOff, dec.pos, dec.r = e.slice, e.chunkOff, 0, src
		}
		if _, err := io.CopyN(io.Discard, src, int64(e.off-dec.pos)); err != nil {
			dec.r = nil
			return nil, err
		}
		buf := make([]byte, e.size)
		if _, err := io.ReadFull(src, buf); err != nil {
			dec.r = nil
			return nil, err
		}
		dec.pos = e.off + e.size
		var sum []byte
		switch len(e.sum) {
		case sha256Size:
			s := sha256.Sum256(buf)
			sum = s[:]
		case sha1Size:
			s := sha1.Sum(buf)
			sum = s[:]
		default:
			s := md5.Sum(buf)
			sum = s[:]
		}
		if !bytes.Equal(sum, e.sum) {
			return nil, errors.New("checksum mismatch")
		}
		return buf, nil
	}

	slices.SortStableFunc(ents, func(a, b *entry) int {
		if len(a.locs) == 0 || len(b.locs) == 0 || int(a.locs[0]) >= len(dat) || int(b.locs[0]) >= len(dat) {
			return 0
		}
		x, y := dat[a.locs[0]], dat[b.locs[0]]
		return cmp.Or(cmp.Compare(x.slice, y.slice), cmp.Compare(x.chunkOff, y.chunkOff), cmp.Compare(x.off, y.off))
	})
	var out []file
	for _, en := range ents {
		if len(en.locs) == 0 || int(en.locs[0]) >= len(dat) {
			continue // an external file, not in the installer
		}
		if !en.galaxy {
			en.size = int64(dat[en.locs[0]].size)
		}
		fname := strings.TrimPrefix(strings.ReplaceAll(en.dest, `\`, "/"), "{app}/")
		out = append(out, file{fname, en.size, func() (io.ReaderAt, error) {
			var all []byte
			for _, l := range en.locs {
				if int(l) >= len(dat) {
					return nil, fmt.Errorf("%s/%s: bad data location", name, fname)
				}
				b, err := extract(dat[l])
				if err == nil && en.galaxy {
					var z io.Reader
					if z, err = zlib.NewReader(bytes.NewReader(b)); err == nil {
						b, err = io.ReadAll(z)
					}
				}
				if err != nil {
					return nil, fmt.Errorf("%s/%s: %w", name, fname, err)
				}
				all = append(all, b...)
			}
			if en.galaxy && len(en.md5) == md5HexSize && fmt.Sprintf("%x", md5.Sum(all)) != strings.ToLower(en.md5) {
				return nil, fmt.Errorf("%s/%s: checksum mismatch", name, fname)
			}
			return bytes.NewReader(all), nil
		}})
	}
	return out, nil
}

// call returns the arguments of a Pascal call to one of names in code ('quoted' strings
// unquoted), or nil when code is no such call.
func call(code string, names ...string) []string {
	code = strings.TrimSpace(code)
	i := strings.IndexByte(code, '(')
	if i < 0 || !slices.Contains(names, strings.TrimSpace(code[:i])) {
		return nil
	}
	var args []string
	var arg strings.Builder
	quoted := false
	for j := i + 1; j < len(code); j++ {
		c := code[j]
		switch {
		case c == '\'' && quoted && j+1 < len(code) && code[j+1] == '\'':
			arg.WriteByte(c)
			j++
		case c == '\'':
			quoted = !quoted
		case !quoted && (c == ',' || c == ')'):
			args = append(args, strings.TrimSpace(arg.String()))
			arg.Reset()
			if c == ')' {
				return args
			}
		default:
			arg.WriteByte(c)
		}
	}
	return nil
}

// headerFlagCount is the number of option flags the setup header stores (load_flags).
func headerFlagCount(v uint32, uni bool) int {
	n := everyVersionFlags
	if v < iv(5, 3, 10, 0) {
		n++ // uninstallable
	}
	if v < iv(5, 3, 3, 0) {
		n += 2 // disable dir page, disable program group page
	}
	if v < iv(5, 3, 8, 0) {
		n++ // create uninstall registry key, in place of use previous language
		n--
	}
	if v < iv(5, 3, 9, 0) {
		n-- // disable welcome page
	}
	if v < iv(5, 5, 0, 0) {
		n -= 3 // close applications, restart applications, allow network drive
	}
	if v < iv(6, 4, 0, 1) {
		n += 5 // window flags, back color horizontal
	}
	if v < iv(5, 6, 1, 0) {
		n += 2 // changes associations, changes environment
	}
	if !uni {
		n++
	}
	if v >= iv(5, 5, 7, 0) {
		n++
	}
	if v >= iv(6, 0, 0, 0) {
		n += 3
	}
	if v >= iv(6, 3, 0, 0) {
		n++
	}
	return n
}

// innoBlock reads the CRC'd (and LZMA1-compressed) block at at; next is the offset after it.
func innoBlock(r io.ReaderAt, at int64) (data []byte, next int64, err error) {
	var h [blockHeaderSize]byte
	if _, err := r.ReadAt(h[:], at); err != nil {
		return nil, 0, err
	}
	le := binary.LittleEndian
	if crc32.ChecksumIEEE(h[4:]) != le.Uint32(h[:]) {
		return nil, 0, errors.New("header block CRC mismatch")
	}
	raw := make([]byte, le.Uint32(h[4:]))
	if _, err := r.ReadAt(raw, at+blockHeaderSize); err != nil {
		return nil, 0, err
	}
	next = at + blockHeaderSize + int64(len(raw))
	for len(raw) > 0 { // blockPieceSize pieces, each after its CRC32
		if len(raw) < 5 {
			return nil, 0, errors.New("truncated header block")
		}
		p := raw[4 : 4+min(blockPieceSize, len(raw)-4)]
		if crc32.ChecksumIEEE(p) != le.Uint32(raw) {
			return nil, 0, errors.New("header block CRC mismatch")
		}
		data = append(data, p...)
		raw = raw[4+len(p):]
	}
	if h[8] == 0 {
		return data, next, nil
	}
	src, err := decompress(bytes.NewReader(data), methodLZMA1, 0)
	if err != nil {
		return nil, 0, err
	}
	data, err = io.ReadAll(src)
	if err == io.ErrUnexpectedEOF { // no end marker: the stream just stops
		err = nil
	}
	return data, next, err
}

// decompress decodes src by Inno's compression method (5.3.9+ numbering); need, when not 0,
// is how many bytes the caller reads, which bounds the LZMA dictionary.
func decompress(src io.Reader, method byte, need uint64) (io.Reader, error) {
	dict := func(d uint64) int {
		if need > 0 && d > need {
			d = max(need, lzma.MinDictCap)
		}
		return int(d)
	}
	switch method {
	case methodStored:
		return src, nil
	case methodZlib:
		return zlib.NewReader(src)
	case methodBzip2:
		return bzip2.NewReader(src), nil
	case methodLZMA1: // 5 property bytes, then a raw LZMA1 stream: give it a .lzma header
		var p [5]byte
		if _, err := io.ReadFull(src, p[:]); err != nil {
			return nil, err
		}
		var hdr [lzmaHeaderSize]byte
		hdr[0] = p[0]
		binary.LittleEndian.PutUint32(hdr[1:], uint32(dict(uint64(binary.LittleEndian.Uint32(p[1:])))))
		size := uint64(math.MaxUint64)
		if need > 0 {
			size = need
		}
		binary.LittleEndian.PutUint64(hdr[5:], size)
		return lzma.NewReader(io.MultiReader(bytes.NewReader(hdr[:]), src))
	case methodLZMA2: // 1 property byte, then raw LZMA2
		var p [1]byte
		if _, err := io.ReadFull(src, p[:]); err != nil {
			return nil, err
		}
		if p[0] > lzma2MaxProperty {
			return nil, errors.New("bad LZMA2 property")
		}
		d := uint64(math.MaxUint32)
		if p[0] < lzma2MaxProperty {
			d = uint64(2|p[0]&1) << (p[0]/2 + lzma2DictShift)
		}
		return lzma.Reader2Config{DictCap: dict(d)}.NewReader2(src)
	}
	return nil, fmt.Errorf("compression method %d not supported", method)
}

// sliceReader reads on through setup-N.bin slices from slice n at pos.
type sliceReader struct {
	n         int
	pos, size int64
	r         io.ReaderAt
	open      func(n int) (io.ReaderAt, error)
}

func (s *sliceReader) Read(p []byte) (int, error) {
	for s.r == nil || s.pos >= s.size {
		if s.r != nil {
			s.n, s.pos, s.r = s.n+1, sliceHeaderSize, nil
		}
		r, err := s.open(s.n)
		if err != nil {
			return 0, err
		}
		var h [sliceHeaderSize]byte
		if _, err := r.ReadAt(h[:], 0); err != nil || string(h[:4]) != "idsk" || h[sliceHeaderEOFAt] != sliceEndOfFile {
			return 0, errors.New("bad installer slice header")
		}
		s.r, s.size = r, int64(binary.LittleEndian.Uint32(h[sliceHeaderSizeAt:]))
	}
	n, err := s.r.ReadAt(p[:min(int64(len(p)), s.size-s.pos)], s.pos)
	s.pos += int64(n)
	if err == io.EOF && n > 0 {
		err = nil
	}
	return n, err
}

// rd reads the little-endian fields of a header block; past its end it records an error.
type rd struct {
	b   []byte
	uni bool
	err error
}

func (r *rd) bytes(n int) []byte {
	if n < 0 || n > len(r.b) {
		r.err, r.b = errors.New("setup header ends early (unsupported Inno Setup variant?)"), nil
		return make([]byte, max(n, 0))
	}
	p := r.b[:n]
	r.b = r.b[n:]
	return p
}
func (r *rd) skip(n int)  { r.bytes(n) }
func (r *rd) u8() byte    { return r.bytes(1)[0] }
func (r *rd) u32() uint32 { return binary.LittleEndian.Uint32(r.bytes(4)) }
func (r *rd) u64() uint64 { return binary.LittleEndian.Uint64(r.bytes(8)) }

// flags skips a set of n flag bits; 3-byte sets are padded to 4.
func (r *rd) flags(n int) {
	if n = (n + byteBits - 1) / byteBits; n == 3 {
		n = 4
	}
	r.skip(n)
}
func (r *rd) strs(n int) {
	for range n {
		r.str()
	}
}

// str reads a length-prefixed string: UTF-16LE in Unicode installers, else Latin-1.
func (r *rd) str() string {
	b := r.bytes(int(r.u32()))
	if !r.uni {
		u := make([]rune, len(b))
		for i, c := range b {
			u[i] = rune(c)
		}
		return string(u)
	}
	u := make([]uint16, len(b)/2)
	for i := range u {
		u[i] = binary.LittleEndian.Uint16(b[2*i:])
	}
	return string(utf16.Decode(u))
}
