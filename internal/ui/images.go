package ui

import (
	"context"
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
)

// Posters loads cover images in the background and keeps them as bitmaps.
// Get and its map are used on the main thread only.
type Posters struct {
	dir     string
	http    *http.Client
	slots   chan struct{}
	entries map[string]*poster
	redraw  func()
	// route returns the client and headers for an image; nil takes the
	// plain client, as in tests.
	route func(url string) (*http.Client, map[string]string)
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

// maxPosterBytes caps the download of one image.
const maxPosterBytes = 16 << 20

func (p *Posters) load(key, url string) (*image.RGBA, error) {
	sum := sha1.Sum([]byte(key))
	file := filepath.Join(p.dir, hex.EncodeToString(sum[:]))
	data, err := os.ReadFile(file)
	if err != nil {
		client, headers := p.http, map[string]string(nil)
		if p.route != nil {
			if c, h := p.route(url); c != nil {
				client, headers = c, h
			}
		}
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		resp, err := client.Do(req.WithContext(ctx))
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
		}
		data, err = io.ReadAll(io.LimitReader(resp.Body, maxPosterBytes+1))
		if err != nil {
			return nil, err
		}
		if len(data) > maxPosterBytes {
			return nil, fmt.Errorf("image larger than %d bytes", maxPosterBytes)
		}
		_ = os.WriteFile(file, data, 0o644)
	}
	// Posters show at most ~240 DIPs wide, twice that in pixels.
	return decodePoster(data, 480)
}
