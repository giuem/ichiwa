//go:build darwin

// Package mac reaches the macOS media frameworks through purego, without
// cgo: AVFoundation plays an episode (sound, clock, seeking) and hands its
// frames over as pixels for MyGo's native UI to draw, and ImageIO decodes
// the HEIC posters the catalog serves.
package mac

import (
	"runtime"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

// CMTime mirrors CoreMedia's CMTime.
type CMTime struct {
	Value     int64
	Timescale int32
	Flags     uint32
	Epoch     int64
}

type cgPoint struct{ X, Y float64 }
type cgSize struct{ W, H float64 }
type cgRect struct {
	Origin cgPoint
	Size   cgSize
}

var (
	loadOnce sync.Once
	loadErr  error

	libobjc uintptr

	// objc_msgSend with typed signatures, for struct and float arguments.
	msgSendTimeTol    func(id objc.ID, sel objc.SEL, t, before, after CMTime)
	msgSendF32        func(id objc.ID, sel objc.SEL, v float32)
	msgRetF32         func(id objc.ID, sel objc.SEL) float32
	msgSendHostTime   func(id objc.ID, sel objc.SEL, host float64) CMTime
	msgSendHasNew     func(id objc.ID, sel objc.SEL, t CMTime) bool
	msgSendCopyPixels func(id objc.ID, sel objc.SEL, t CMTime, display uintptr) uintptr
	msgRetTime        func(id objc.ID, sel objc.SEL) CMTime
	msgRetInt         func(id objc.ID, sel objc.SEL) int64

	cmTimeGetSeconds      func(t CMTime) float64
	cmTimeMakeWithSeconds func(sec float64, scale int32) CMTime
	caCurrentMediaTime    func() float64

	cvLock        func(pb uintptr, flags uint64) int32
	cvUnlock      func(pb uintptr, flags uint64) int32
	cvBase        func(pb uintptr) uintptr
	cvBytesPerRow func(pb uintptr) uintptr
	cvWidth       func(pb uintptr) uintptr
	cvHeight      func(pb uintptr) uintptr
	cvRelease     func(pb uintptr)

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

	kCVPixelFormatKey objc.ID
	kCMTimeZero       CMTime
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
		libobjc = open("/usr/lib/libobjc.A.dylib")
		open("/System/Library/Frameworks/Foundation.framework/Foundation")
		open("/System/Library/Frameworks/AVFoundation.framework/AVFoundation")
		cm := open("/System/Library/Frameworks/CoreMedia.framework/CoreMedia")
		cv := open("/System/Library/Frameworks/CoreVideo.framework/CoreVideo")
		qc := open("/System/Library/Frameworks/QuartzCore.framework/QuartzCore")
		cf := open("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation")
		cg := open("/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics")
		io := open("/System/Library/Frameworks/ImageIO.framework/ImageIO")
		if loadErr != nil {
			return
		}
		for _, fn := range []any{&msgSendTimeTol, &msgSendF32, &msgRetF32, &msgSendHostTime, &msgSendHasNew, &msgSendCopyPixels, &msgRetTime, &msgRetInt} {
			purego.RegisterLibFunc(fn, libobjc, "objc_msgSend")
		}
		purego.RegisterLibFunc(&cmTimeGetSeconds, cm, "CMTimeGetSeconds")
		purego.RegisterLibFunc(&cmTimeMakeWithSeconds, cm, "CMTimeMakeWithSeconds")
		purego.RegisterLibFunc(&caCurrentMediaTime, qc, "CACurrentMediaTime")
		purego.RegisterLibFunc(&cvLock, cv, "CVPixelBufferLockBaseAddress")
		purego.RegisterLibFunc(&cvUnlock, cv, "CVPixelBufferUnlockBaseAddress")
		purego.RegisterLibFunc(&cvBase, cv, "CVPixelBufferGetBaseAddress")
		purego.RegisterLibFunc(&cvBytesPerRow, cv, "CVPixelBufferGetBytesPerRow")
		purego.RegisterLibFunc(&cvWidth, cv, "CVPixelBufferGetWidth")
		purego.RegisterLibFunc(&cvHeight, cv, "CVPixelBufferGetHeight")
		purego.RegisterLibFunc(&cvRelease, cv, "CVBufferRelease")
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

		sym, err := purego.Dlsym(cv, "kCVPixelBufferPixelFormatTypeKey")
		if err != nil {
			loadErr = err
			return
		}
		kCVPixelFormatKey = **(**objc.ID)(unsafe.Pointer(&sym))
		zero, err := purego.Dlsym(cm, "kCMTimeZero")
		if err != nil {
			loadErr = err
			return
		}
		kCMTimeZero = **(**CMTime)(unsafe.Pointer(&zero))
	})
	return loadErr
}

var sels sync.Map

func sel(name string) objc.SEL {
	if s, ok := sels.Load(name); ok {
		return s.(objc.SEL)
	}
	s := objc.RegisterName(name)
	sels.Store(name, s)
	return s
}

func class(name string) objc.ID { return objc.ID(objc.GetClass(name)) }

// pool runs fn inside an autorelease pool on a locked thread, so the
// temporary objects Cocoa returns are freed when it ends.
func pool(fn func()) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	p := class("NSAutoreleasePool").Send(sel("alloc")).Send(sel("init"))
	defer p.Send(sel("drain"))
	fn()
}

func nsString(s string) objc.ID {
	b := []byte(s)
	if len(b) == 0 {
		b = []byte{0}
		return class("NSString").Send(sel("string"))
	}
	str := class("NSString").Send(sel("alloc")).Send(sel("initWithBytes:length:encoding:"), unsafe.Pointer(&b[0]), uintptr(len(b)), uintptr(4))
	runtime.KeepAlive(b)
	return str.Send(sel("autorelease"))
}
