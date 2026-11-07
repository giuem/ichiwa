//go:build darwin

package mac

import (
	"errors"
	"image"
	"runtime"
	"unsafe"
)

// DecodeImage decodes any image ImageIO knows (HEIC, AVIF, JPEG, PNG,
// WebP...) into RGBA, scaled down so that its width is at most maxWidth
// (0 keeps its size).
func DecodeImage(data []byte, maxWidth int) (*image.RGBA, error) {
	if err := load(); err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, errors.New("mac: empty image")
	}
	cfData := cfDataCreate(0, unsafe.Pointer(&data[0]), len(data))
	runtime.KeepAlive(data)
	if cfData == 0 {
		return nil, errors.New("mac: CFDataCreate failed")
	}
	defer cfRelease(cfData)
	src := cgSrcCreate(cfData, 0)
	if src == 0 {
		return nil, errors.New("mac: unknown image format")
	}
	defer cfRelease(src)
	img := cgSrcImage(src, 0, 0)
	if img == 0 {
		return nil, errors.New("mac: cannot decode image")
	}
	defer cfRelease(img)
	w, h := int(cgImageWidth(img)), int(cgImageHeight(img))
	if w == 0 || h == 0 {
		return nil, errors.New("mac: empty image")
	}
	if maxWidth > 0 && w > maxWidth {
		h = h * maxWidth / w
		w = maxWidth
		if h < 1 {
			h = 1
		}
	}
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	cs := cgColorSpaceRGB()
	defer cfRelease(cs)
	// kCGImageAlphaPremultipliedLast | kCGBitmapByteOrder32Big: RGBA bytes.
	ctx := cgBitmapCreate(unsafe.Pointer(&out.Pix[0]), uintptr(w), uintptr(h), 8, uintptr(out.Stride), cs, 1|4<<12)
	if ctx == 0 {
		return nil, errors.New("mac: CGBitmapContextCreate failed")
	}
	defer cfRelease(ctx)
	cgSetInterp(ctx, 3) // high
	cgDrawImage(ctx, cgRect{Size: cgSize{float64(w), float64(h)}}, img)
	runtime.KeepAlive(out)
	return out, nil
}
