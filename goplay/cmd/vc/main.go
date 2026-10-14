// vc is Vice City's goplay import: `vc -import SRC -data DIR` finds a Vice City install in
// whatever SRC holds (folders, zips, CD images, the Inno Setup installer on the disc) and
// copies the folders the reVC engine reads into DIR, with reVC's own gamefiles laid over
// them and a COVER.PNG for the floppy label. The game itself is the Emscripten build
// (wasm/), which goplay runs on DIR; this program only ever imports.
package main

import (
	"embed"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path"
	"path/filepath"
	"strings"

	"revc-goplay/gameimport"
)

// gamefiles is reVC's overlay (../gamefiles, copied next to this file by build.sh).
//
//go:embed all:gamefiles
var gamefiles embed.FS

// folders the engine reads, lower-case; movies (intro videos), mp3 (user tracks) and
// skins stay behind
var folders = []string{"anim", "audio", "data", "models", "text", "txd"}

// rel gives a name's path from the game's root ("Audio/sfx.raw") when it lies in one of
// folders, else "".
func rel(name string) string {
	parts := strings.Split(name, "/")
	for i := len(parts) - 2; i >= 0; i-- {
		for _, f := range folders {
			if strings.EqualFold(parts[i], f) {
				return path.Join(parts[i:]...)
			}
		}
	}
	return ""
}

func importGame(src, dst string) error {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	n := 0
	// ponytail: two installs in one drop land on top of each other; the markers below still
	// have to come from one of them
	err := gameimport.Walk(src, func(name string) bool { return rel(name) != "" }, func(name string, data []byte) error {
		p := filepath.Join(dst, filepath.FromSlash(rel(name)))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if n++; n%50 == 0 {
			fmt.Fprintf(os.Stderr, "%d files\n", n)
		}
		return os.WriteFile(p, data, 0o644)
	})
	if err != nil {
		return err
	}
	// File name case varies between releases (sfx.raw vs sfx.RAW); the engine's casepath copes, so do we.
	for _, m := range []string{"models/gta3.img", "data/default.dat", "Audio/sfx.raw"} {
		if !existsFold(filepath.Join(dst, filepath.FromSlash(m))) {
			os.RemoveAll(dst)
			return errors.New("no Vice City in " + src + " (missing " + m + ")")
		}
	}
	err = fs.WalkDir(gamefiles, "gamefiles", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Name() == ".DS_Store" {
			return err
		}
		b, err := gamefiles.ReadFile(p)
		if err != nil {
			return err
		}
		out := filepath.Join(dst, filepath.FromSlash(strings.TrimPrefix(p, "gamefiles/")))
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return err
		}
		return os.WriteFile(out, b, 0o644)
	})
	if err != nil {
		return err
	}
	return writeCover(dst)
}

func main() {
	src := flag.String("import", "", "find Vice City in this folder or file and copy it to -data")
	data := flag.String("data", os.Getenv("GOPLAY_DATA"), "the game directory")
	flag.Parse()
	if *src == "" || *data == "" {
		log.Fatal("usage: vc -import SRC -data DIR")
	}
	if err := importGame(*src, *data); err != nil {
		log.Fatal(err)
	}
}

// existsFold reports whether p exists, ignoring the case of its last component.
func existsFold(p string) bool {
	ents, err := os.ReadDir(filepath.Dir(p))
	if err != nil {
		return false
	}
	for _, e := range ents {
		if strings.EqualFold(e.Name(), filepath.Base(p)) {
			return true
		}
	}
	return false
}
