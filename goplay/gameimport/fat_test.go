package gameimport

import (
	"os"
	"strings"
	"testing"
)

func TestFloppyImage(t *testing.T) {
	src := os.Getenv("FLOPPY_IMG")
	if src == "" {
		t.Skip("set FLOPPY_IMG to a FAT floppy image")
	}
	files, err := Find(src, func(name string) bool { return !strings.HasSuffix(name, ".img") })
	if err != nil || len(files) == 0 {
		t.Fatalf("%d files, %v", len(files), err)
	}
	for n := range files {
		t.Log(n)
	}
}
