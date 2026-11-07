//go:build darwin

package mac

import (
	"errors"
	"image"
	"math"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego/objc"
)

// Player plays a local media file with AVPlayer. AVPlayer makes the
// sound and keeps the clock; Frame copies the picture due now into an
// *image.RGBA for the UI to draw. All methods are safe for concurrent use.
type Player struct {
	mu     sync.Mutex
	player objc.ID
	item   objc.ID
	output objc.ID
	closed bool
}

// Open prepares a player for the file at path. It starts paused.
func Open(path string) (*Player, error) {
	if err := load(); err != nil {
		return nil, err
	}
	p := &Player{}
	var err error
	pool(func() {
		url := class("NSURL").Send(sel("fileURLWithPath:"), nsString(path))
		item := class("AVPlayerItem").Send(sel("playerItemWithURL:"), url)
		if item == 0 {
			err = errors.New("mac: cannot open media")
			return
		}
		num := class("NSNumber").Send(sel("numberWithUnsignedInt:"), uintptr(0x42475241)) // 'BGRA'
		attrs := class("NSDictionary").Send(sel("dictionaryWithObject:forKey:"), num, kCVPixelFormatKey)
		out := class("AVPlayerItemVideoOutput").Send(sel("alloc")).Send(sel("initWithPixelBufferAttributes:"), attrs)
		item.Send(sel("addOutput:"), out)
		player := class("AVPlayer").Send(sel("alloc")).Send(sel("initWithPlayerItem:"), item)
		// Stop at the end rather than advance: the app picks what follows.
		player.Send(sel("setActionAtItemEnd:"), uintptr(2)) // AVPlayerActionAtItemEndNone
		p.player = player
		p.item = item.Send(sel("retain"))
		p.output = out
	})
	return p, err
}

func (p *Player) do(fn func()) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return
	}
	pool(fn)
}

// Play starts or resumes playback.
func (p *Player) Play() { p.do(func() { p.player.Send(sel("play")) }) }

// Pause holds playback where it is.
func (p *Player) Pause() { p.do(func() { p.player.Send(sel("pause")) }) }

// Playing reports whether the player's rate is above zero.
func (p *Player) Playing() bool {
	var r float32
	p.do(func() { r = msgRetF32(p.player, sel("rate")) })
	return r > 0
}

// SetRate sets the playback speed; it also starts playback.
func (p *Player) SetRate(r float64) {
	p.do(func() { msgSendF32(p.player, sel("setRate:"), float32(r)) })
}

// SetVolume sets the volume between 0 and 1.
func (p *Player) SetVolume(v float64) {
	p.do(func() { msgSendF32(p.player, sel("setVolume:"), float32(v)) })
}

// Seek moves to sec seconds, exactly.
func (p *Player) Seek(sec float64) {
	if sec < 0 {
		sec = 0
	}
	p.do(func() {
		msgSendTimeTol(p.player, sel("seekToTime:toleranceBefore:toleranceAfter:"), cmTimeMakeWithSeconds(sec, 600), kCMTimeZero, kCMTimeZero)
	})
}

func seconds(t CMTime) float64 {
	s := cmTimeGetSeconds(t)
	if math.IsNaN(s) || math.IsInf(s, 0) {
		return 0
	}
	return s
}

// Position is the current time in seconds.
func (p *Player) Position() float64 {
	var s float64
	p.do(func() { s = seconds(msgRetTime(p.player, sel("currentTime"))) })
	return s
}

// Duration is the length in seconds, 0 until known.
func (p *Player) Duration() float64 {
	var s float64
	p.do(func() { s = seconds(msgRetTime(p.item, sel("duration"))) })
	return s
}

// Failed reports whether the media could not be played.
func (p *Player) Failed() bool {
	var st int64
	p.do(func() { st = msgRetInt(p.item, sel("status")) })
	return st == 2
}

// Frame copies the frame due now into dst when there is a new one, and
// returns the image holding it (dst, or a new image when dst is nil or
// of another size). ok is false when no new frame is due.
func (p *Player) Frame(dst *image.RGBA) (img *image.RGBA, ok bool) {
	p.do(func() {
		t := msgSendHostTime(p.output, sel("itemTimeForHostTime:"), caCurrentMediaTime())
		if !msgSendHasNew(p.output, sel("hasNewPixelBufferForItemTime:"), t) {
			return
		}
		pb := msgSendCopyPixels(p.output, sel("copyPixelBufferForItemTime:itemTimeForDisplay:"), t, 0)
		if pb == 0 {
			return
		}
		defer cvRelease(pb)
		cvLock(pb, 1)
		defer cvUnlock(pb, 1)
		w, h, bpr := int(cvWidth(pb)), int(cvHeight(pb)), int(cvBytesPerRow(pb))
		base := cvBase(pb)
		if base == 0 || w == 0 || h == 0 {
			return
		}
		if dst == nil || dst.Rect.Dx() != w || dst.Rect.Dy() != h {
			dst = image.NewRGBA(image.Rect(0, 0, w, h))
		}
		src := unsafe.Slice((*byte)(*(*unsafe.Pointer)(unsafe.Pointer(&base))), bpr*h)
		for y := 0; y < h; y++ {
			s := src[y*bpr : y*bpr+w*4]
			d := dst.Pix[y*dst.Stride : y*dst.Stride+w*4]
			for x := 0; x < len(s); x += 4 {
				d[x], d[x+1], d[x+2], d[x+3] = s[x+2], s[x+1], s[x], 255
			}
		}
		img, ok = dst, true
	})
	return img, ok
}

// Close stops playback and frees the player.
func (p *Player) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return
	}
	pool(func() {
		p.player.Send(sel("pause"))
		p.player.Send(sel("replaceCurrentItemWithPlayerItem:"), objc.ID(0))
		p.item.Send(sel("removeOutput:"), p.output)
		p.output.Send(sel("release"))
		p.item.Send(sel("release"))
		p.player.Send(sel("release"))
	})
	p.closed = true
}
