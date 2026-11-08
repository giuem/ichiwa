//go:build darwin

// Package mac reaches ImageIO through purego, without cgo, to decode the
// HEIC posters the catalog serves.
package mac

import (
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

type cgPoint struct{ X, Y float64 }
type cgSize struct{ W, H float64 }
type cgRect struct {
	Origin cgPoint
	Size   cgSize
}

var (
	loadOnce sync.Once
	loadErr  error

	cfDataCreate    func(alloc uintptr, bytes unsafe.Pointer, n int) uintptr
	cfRelease       func(ref uintptr)
	cgSrcCreate     func(data uintptr, opts uintptr) uintptr
	cgSrcImage      func(src uintptr, idx uintptr, opts uintptr) uintptr
	cgImageWidth    func(img uintptr) uintptr
	cgImageHeight   func(img uintptr) uintptr
	cgColorSpaceRGB func() uintptr
	cgBitmapCreate  func(data unsafe.Pointer, w, h, bpc, bpr uintptr, cs uintptr, info uint32) uintptr
	cgDrawImage     func(ctx uintptr, r cgRect, img uintptr)
	cgSetInterp     func(ctx uintptr, q int32)
)

func load() error {
	loadOnce.Do(func() {
		open := func(path string) uintptr {
			if loadErr != nil {
				return 0
			}
			h, err := purego.Dlopen(path, purego.RTLD_NOW|purego.RTLD_GLOBAL)
			if err != nil {
				loadErr = err
			}
			return h
		}
		cf := open("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation")
		cg := open("/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics")
		io := open("/System/Library/Frameworks/ImageIO.framework/ImageIO")
		if loadErr != nil {
			return
		}
		purego.RegisterLibFunc(&cfDataCreate, cf, "CFDataCreate")
		purego.RegisterLibFunc(&cfRelease, cf, "CFRelease")
		purego.RegisterLibFunc(&cgSrcCreate, io, "CGImageSourceCreateWithData")
		purego.RegisterLibFunc(&cgSrcImage, io, "CGImageSourceCreateImageAtIndex")
		purego.RegisterLibFunc(&cgImageWidth, cg, "CGImageGetWidth")
		purego.RegisterLibFunc(&cgImageHeight, cg, "CGImageGetHeight")
		purego.RegisterLibFunc(&cgColorSpaceRGB, cg, "CGColorSpaceCreateDeviceRGB")
		purego.RegisterLibFunc(&cgBitmapCreate, cg, "CGBitmapContextCreate")
		purego.RegisterLibFunc(&cgDrawImage, cg, "CGContextDrawImage")
		purego.RegisterLibFunc(&cgSetInterp, cg, "CGContextSetInterpolationQuality")
	})
	return loadErr
}
