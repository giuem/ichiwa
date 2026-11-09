package app

import (
	"bytes"
	"image"
	_ "image/gif"  // decodeImage
	_ "image/jpeg" // decodeImage
	_ "image/png"  // decodeImage

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp" // decodeImage
)

// decodeImage decodes the formats Go knows into RGBA, scaled down so that
// its width is at most maxWidth (0 keeps its size).
func decodeImage(data []byte, maxWidth int) (*image.RGBA, error) {
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if maxWidth > 0 && w > maxWidth {
		h = max(h*maxWidth/w, 1)
		w = maxWidth
	}
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.CatmullRom.Scale(out, out.Bounds(), src, b, draw.Src, nil)
	return out, nil
}
