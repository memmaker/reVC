package main

import (
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
)

// writeCover makes dst/COVER.PNG, goplay's floppy label, from the game's first loading
// screen (txd/LOADSC0.TXD, a 512x512 8-bit paletted picture shown at 4:3), 320x240.
func writeCover(dst string) error {
	b, err := os.ReadFile(filepath.Join(dst, "txd", "LOADSC0.TXD"))
	if err != nil {
		return err
	}
	src, err := txdPicture(b)
	if err != nil {
		return err
	}
	// ponytail: nearest-neighbour scaling, fine for a label
	out := image.NewRGBA(image.Rect(0, 0, 320, 240))
	for y := range 240 {
		for x := range 320 {
			out.Set(x, y, src.At(x*src.Bounds().Dx()/320, y*src.Bounds().Dy()/240))
		}
	}
	f, err := os.Create(filepath.Join(dst, "COVER.PNG"))
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, out)
}

// txdPicture decodes the first texture of a RenderWare D3D8 texture dictionary, which for
// the loading screens is 8-bit paletted (PAL8 | C888): header, 256 RGBA palette entries,
// a data size, then the pixels.
func txdPicture(b []byte) (image.Image, error) {
	le := binary.LittleEndian
	for off := 0; off+12 <= len(b); {
		typ, size := le.Uint32(b[off:]), int(le.Uint32(b[off+4:]))
		switch typ {
		case 0x16: // texture dictionary: look inside
			off += 12
			continue
		case 0x15: // texture native
			d := off + 24 // past the section and its struct header
			if d+88 > len(b) {
				return nil, errors.New("short texture")
			}
			format, w, h := le.Uint32(b[d+72:]), int(le.Uint16(b[d+80:])), int(le.Uint16(b[d+82:]))
			if format&0x2000 == 0 || b[d+84] != 8 {
				return nil, errors.New("loading screen is not 8-bit paletted")
			}
			pal, px := b[d+88:], d+88+1024+4
			if px+w*h > len(b) {
				return nil, errors.New("short texture")
			}
			img := image.NewRGBA(image.Rect(0, 0, w, h))
			for i := range w * h {
				c := pal[int(b[px+i])*4:]
				img.Set(i%w, i/w, color.RGBA{c[0], c[1], c[2], 255})
			}
			return img, nil
		}
		off += 12 + size
	}
	return nil, errors.New("no texture in TXD")
}
