package main

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"image"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"hongguo/internal/mac"
)

// Posters loads cover images in the background and keeps them as bitmaps.
// Get and its map are used on the main thread only.
type Posters struct {
	dir     string
	http    *http.Client
	slots   chan struct{}
	entries map[string]*poster
	redraw  func()
}

type poster struct {
	bmp    *ui.Bitmap
	img    *image.RGBA
	failed bool
}

func newPosters(cacheDir string, redraw func()) *Posters {
	dir := filepath.Join(cacheDir, "posters")
	_ = os.MkdirAll(dir, 0o755)
	return &Posters{
		dir:     dir,
		http:    &http.Client{Timeout: 30 * time.Second},
		slots:   make(chan struct{}, 8),
		entries: map[string]*poster{},
		redraw:  redraw,
	}
}

// posterKey drops the signature of a URL, which changes on each fetch.
func posterKey(url string) string {
	if i := strings.IndexByte(url, '?'); i >= 0 {
		return url[:i]
	}
	return url
}

// Get returns the bitmap of url, or nil while it loads.
func (p *Posters) Get(url string) *ui.Bitmap {
	if url == "" {
		return nil
	}
	key := posterKey(url)
	if e, ok := p.entries[key]; ok {
		return e.bmp
	}
	e := &poster{}
	p.entries[key] = e
	go func() {
		p.slots <- struct{}{}
		img, err := p.load(key, url)
		<-p.slots
		mygo.RunOnMain(func() {
			if err != nil {
				e.failed = true
				return
			}
			e.bmp, e.img = ui.NewBitmap(img), img
			p.redraw()
		})
	}()
	return nil
}

// Image returns the decoded cover of url, nil until it loaded.
func (p *Posters) Image(url string) *image.RGBA {
	if e, ok := p.entries[posterKey(url)]; ok {
		return e.img
	}
	return nil
}

func (p *Posters) load(key, url string) (*image.RGBA, error) {
	sum := sha1.Sum([]byte(key))
	file := filepath.Join(p.dir, hex.EncodeToString(sum[:]))
	data, err := os.ReadFile(file)
	if err != nil {
		resp, err := p.http.Get(url)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
		}
		data, err = io.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}
		_ = os.WriteFile(file, data, 0o644)
	}
	// Posters show at most ~240 DIPs wide, twice that in pixels.
	return mac.DecodeImage(data, 480)
}
