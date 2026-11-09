package app

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func TestDecodeImage(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 40, 80))
	for y := range 80 {
		for x := range 40 {
			src.Set(x, y, color.NRGBA{R: 200, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, src); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ limit, w, h int }{{20, 20, 40}, {100, 40, 80}, {0, 40, 80}} {
		img, err := decodeImage(buf.Bytes(), c.limit)
		if err != nil {
			t.Fatal(err)
		}
		if b := img.Bounds(); b.Dx() != c.w || b.Dy() != c.h {
			t.Errorf("limit %d: got %v", c.limit, b)
		}
		if got := img.RGBAAt(c.w/2, c.h/2); got.R != 200 || got.A != 255 {
			t.Errorf("limit %d: pixel %v", c.limit, got)
		}
	}
	if _, err := decodeImage([]byte("not an image"), 0); err == nil {
		t.Error("garbage decoded")
	}
}
