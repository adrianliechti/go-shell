// Package icns renders an Apple .icns icon from a single square source image
// (ideally 1024x1024), replacing the sips/iconutil pipeline.
package icns

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/png"
	"io"

	"golang.org/x/image/draw"
)

// The PNG-based entry types modern macOS reads, matching what iconutil
// emits for a full iconset (16..512 plus their @2x variants).
var entries = []struct {
	kind string
	size int
}{
	{"icp4", 16},   // 16x16
	{"icp5", 32},   // 32x32
	{"ic07", 128},  // 128x128
	{"ic08", 256},  // 256x256
	{"ic09", 512},  // 512x512
	{"ic10", 1024}, // 512x512@2x
	{"ic11", 32},   // 16x16@2x
	{"ic12", 64},   // 32x32@2x
	{"ic13", 256},  // 128x128@2x
	{"ic14", 512},  // 256x256@2x
}

// Encode writes src as an .icns file with all standard icon sizes.
func Encode(w io.Writer, src image.Image) error {
	rendered := map[int][]byte{}

	var body bytes.Buffer

	for _, entry := range entries {
		img, ok := rendered[entry.size]

		if !ok {
			var err error

			if img, err = render(src, entry.size); err != nil {
				return err
			}

			rendered[entry.size] = img
		}

		body.WriteString(entry.kind)
		binary.Write(&body, binary.BigEndian, uint32(8+len(img)))
		body.Write(img)
	}

	var file bytes.Buffer
	file.WriteString("icns")
	binary.Write(&file, binary.BigEndian, uint32(8+body.Len()))
	body.WriteTo(&file)

	_, err := w.Write(file.Bytes())
	return err
}

func render(src image.Image, size int) ([]byte, error) {
	dst := image.NewRGBA(image.Rect(0, 0, size, size))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Src, nil)

	var buf bytes.Buffer

	if err := png.Encode(&buf, dst); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}
