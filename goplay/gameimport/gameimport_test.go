package gameimport

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
)

var cc = bytes.Repeat([]byte("MM3 cc data "), 1000) // 12000 bytes: spans sectors

func isCC(name string) bool { return strings.EqualFold(path.Base(name), "mm3.cc") }

func find(t *testing.T, name string, data []byte) map[string][]byte {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Find(dir, isCC)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func want(t *testing.T, got map[string][]byte, name string) {
	t.Helper()
	if len(got) != 1 || !bytes.Equal(got[name], cc) {
		keys := []string{}
		for k := range got {
			keys = append(keys, k)
		}
		t.Fatalf("want %s, got %v", name, keys)
	}
}

// makeISO builds an ISO 9660 image with GAME/MM3.CC;1 and README.TXT;1.
func makeISO() []byte {
	const S = isoSector
	img := make([]byte, (22+(len(cc)+S-1)/S)*S)
	rec := func(name string, lba, size int, dir bool) []byte {
		r := make([]byte, 33+len(name)+(1-len(name)%2))
		r[0] = byte(len(r))
		binary.LittleEndian.PutUint32(r[2:], uint32(lba))
		binary.BigEndian.PutUint32(r[6:], uint32(lba))
		binary.LittleEndian.PutUint32(r[10:], uint32(size))
		binary.BigEndian.PutUint32(r[14:], uint32(size))
		if dir {
			r[25] = 2
		}
		r[32] = byte(len(name))
		copy(r[33:], name)
		return r
	}
	dirSec := func(lba int, recs ...[]byte) {
		off := lba * S
		for _, r := range recs {
			off += copy(img[off:], r)
		}
	}
	pvd := img[16*S:]
	copy(pvd, "\x01CD001\x01")
	copy(pvd[156:], rec("\x00", 18, S, true))
	copy(img[17*S:], "\xffCD001\x01")
	dirSec(18, rec("\x00", 18, S, true), rec("\x01", 18, S, true), rec("GAME", 19, S, true), rec("README.TXT;1", 20, 5, false))
	dirSec(19, rec("\x00", 19, S, true), rec("\x01", 18, S, true), rec("MM3.CC;1", 21, len(cc), false))
	copy(img[20*S:], "hello")
	copy(img[21*S:], cc)
	return img
}

// makeBIN turns an ISO into a raw track of 2352-byte sectors of the given mode (1 or 2).
func makeBIN(iso []byte, mode byte) []byte {
	var out []byte
	for i := 0; i < len(iso); i += isoSector {
		sec := make([]byte, 2352)
		copy(sec, "\x00\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\x00")
		sec[15] = mode
		data := 16
		if mode == 2 {
			data = 24
		}
		copy(sec[data:], iso[i:i+isoSector])
		out = append(out, sec...)
	}
	return out
}

func makeZip(t *testing.T, files map[string][]byte) []byte {
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	for n, d := range files {
		w, err := z.Create(n)
		if err != nil {
			t.Fatal(err)
		}
		w.Write(d)
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestFolder(t *testing.T) { want(t, find(t, "mm3.cc", cc), "mm3.cc") }

func TestZip(t *testing.T) {
	want(t, find(t, "game.zip", makeZip(t, map[string][]byte{"MM3/Mm3.Cc": cc, "MM3/README": []byte("x")})), "game.zip/MM3/Mm3.Cc")
	// a zip holding a CD image
	want(t, find(t, "cd.zip", makeZip(t, map[string][]byte{"cd/mm3.iso": makeISO()})), "cd.zip/cd/mm3.iso/GAME/MM3.CC")
}

func TestISO(t *testing.T) { want(t, find(t, "mm3.iso", makeISO()), "mm3.iso/GAME/MM3.CC") }

func TestBIN(t *testing.T) {
	want(t, find(t, "mm3.bin", makeBIN(makeISO(), 1)), "mm3.bin/GAME/MM3.CC")
	want(t, find(t, "mm3.bin", makeBIN(makeISO(), 2)), "mm3.bin/GAME/MM3.CC")
}

// TestInno extracts from real GOG installers when there are any: INNO_SETUP names one,
// else ~/Downloads/setup_*.exe. Every GOG installer has a goggame-*.info; from Might and
// Magic III's, MM3.CC is compared with $MM3_DATA/MM3.CC when that is set.
func TestInno(t *testing.T) {
	setups := []string{os.Getenv("INNO_SETUP")}
	if setups[0] == "" {
		home, _ := os.UserHomeDir()
		setups, _ = filepath.Glob(filepath.Join(home, "Downloads", "setup_*.exe"))
	}
	if len(setups) == 0 {
		t.Skip("no GOG installer found (set INNO_SETUP)")
	}
	for _, s := range setups {
		got, err := Find(s, func(n string) bool { return path.Ext(n) == ".info" || isCC(n) })
		if err != nil {
			t.Fatal(err)
		}
		info := false
		for n, b := range got {
			info = info || path.Ext(n) == ".info" && bytes.Contains(b, []byte(`"gameId"`))
		}
		if !info {
			t.Errorf("%s: no goggame-*.info", s)
		}
		ref, _ := os.ReadFile(filepath.Join(os.Getenv("MM3_DATA"), "MM3.CC"))
		mm3 := got[filepath.Base(s)+"/MM3.CC"]
		if mm3 == nil && strings.Contains(s, "might_and_magic_3") {
			t.Errorf("%s: no MM3.CC", s)
		}
		if mm3 != nil && ref != nil && !bytes.Equal(mm3, ref) {
			t.Errorf("%s: MM3.CC differs from $MM3_DATA's", s)
		}
		t.Logf("%s: %d files, MM3.CC %d bytes", filepath.Base(s), len(got), len(mm3))
	}
}
