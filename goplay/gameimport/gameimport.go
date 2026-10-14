// Package gameimport finds a game's files in whatever a player hands over: plain folders,
// loose files, zip files, CD-ROM images (ISO 9660 .iso, raw BIN/CUE with 2352-byte mode 1
// or mode 2 sectors) and GOG installers (Inno Setup 5.5 to 6.4 setup_*.exe, with their
// setup_*-N.bin slices beside them). Containers nest: a zip can hold an ISO or an
// installer, a folder can hold any of them.
//
// The package has no dependency on any game and only one outside this module
// (github.com/ulikunitz/xz, pure Go, for the installers' LZMA); copy the folder as it is
// into another port.
//
//	files, err := gameimport.Find(src, func(name string) bool {
//		return strings.EqualFold(path.Base(name), "GAME.DAT")
//	})
package gameimport

import (
	"archive/zip"
	"bytes"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Find walks src (a folder or a single file) and returns the contents of every file whose
// name want accepts, keyed by that name. A name is the file's slash-separated path from
// src, through any containers: "MM3/MM3.CC", "game.zip/MM3/MM3.CC", "cd.iso/MM3/MM3.CC",
// or for an installer's file "setup_game.exe/MM3.CC" (Inno's "{app}\" is dropped). Names
// keep the case they are stored with; compare them case-insensitively. Files want accepts
// are not opened as containers. A file that turns out not to be the container its
// extension suggests (an .exe that is no installer, a .bin that is a slice) is skipped; an
// installer that cannot be extracted (encrypted, missing slices) fails Find.
func Find(src string, want func(name string) bool) (map[string][]byte, error) {
	out := map[string][]byte{}
	return out, Walk(src, want, func(name string, data []byte) error { out[name] = data; return nil })
}

// Walk is Find for games too big to hold at once: each file want accepts goes to emit as it
// is read, one at a time (the bytes are emit's to keep).
func Walk(src string, want func(name string) bool, emit func(name string, data []byte) error) error {
	var files []file
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if rel == "." {
			rel = d.Name()
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		files = append(files, file{filepath.ToSlash(rel), info.Size(), func() (io.ReaderAt, error) { return os.Open(p) }})
		return nil
	})
	if err != nil {
		return err
	}
	return scan(files, "", false, want, emit)
}

// file is one file of a folder or container; open gives its bytes (size of them).
type file struct {
	name string
	size int64
	open func() (io.ReaderAt, error)
}

// scan reads what want accepts from files and looks into the containers among them. Inside an
// installer, .exe files are not opened as installers: sniffing each would decompress it.
func scan(files []file, prefix string, inInno bool, want func(string) bool, emit func(string, []byte) error) error {
	for _, f := range files {
		name := prefix + f.name
		var list func(io.ReaderAt, int64) ([]file, error)
		switch strings.ToLower(path.Ext(f.name)) {
		case ".zip":
			list = listZip
		case ".iso", ".bin", ".img", ".ima", ".gog":
			list = listCD
		case ".dat": // GOG ships some CD games as an ISO named GAME.DAT
			list = func(r io.ReaderAt, n int64) ([]file, error) {
				if !isISO(r) {
					return nil, nil
				}
				return listCD(r, n)
			}
		case ".hdr": // InstallShield: data1.hdr indexes the dataN.cab volumes beside it
			list = func(r io.ReaderAt, n int64) ([]file, error) { return listInstallShield(r, n, f.name, files) }
		case ".exe":
			if inInno {
				break
			}
			list = func(r io.ReaderAt, n int64) ([]file, error) { return listInno(r, n, f.name, files) }
		}
		if !want(name) && list == nil {
			continue
		}
		r, err := f.open()
		if err != nil {
			return err
		}
		if want(name) {
			b := make([]byte, f.size)
			if _, err := r.ReadAt(b, 0); err != nil && err != io.EOF {
				return err
			}
			if err := emit(name, b); err != nil {
				return err
			}
			continue
		}
		sub, err := list(r, f.size)
		if err != nil {
			return err
		}
		if err := scan(sub, name+"/", inInno || strings.EqualFold(path.Ext(f.name), ".exe"), want, emit); err != nil {
			return err
		}
	}
	return nil
}

// listZip lists a zip's files; each is inflated whole into memory when opened.
func listZip(r io.ReaderAt, size int64) ([]file, error) {
	z, err := zip.NewReader(r, size)
	if err != nil {
		return nil, nil // not a zip after all
	}
	var out []file
	for _, zf := range z.File {
		if zf.FileInfo().IsDir() {
			continue
		}
		out = append(out, file{zf.Name, int64(zf.UncompressedSize64), func() (io.ReaderAt, error) {
			rc, err := zf.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			b, err := io.ReadAll(rc)
			return bytes.NewReader(b), err
		}})
	}
	return out, nil
}
