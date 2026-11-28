//go:build !darwin

package ui

import (
	"os"
	"sync"
	"testing"
)

// Posters decode at the same time, eight at most.
func TestDecodePosterHEIC(t *testing.T) {
	data, err := os.ReadFile("testdata/icon.heic")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			img, err := decodePoster(data, 32)
			if err != nil {
				t.Error(err)
				return
			}
			if b := img.Bounds(); b.Dx() != 32 || b.Dy() != 32 {
				t.Errorf("bounds %v", b)
			}
			if img.RGBAAt(16, 16).A == 0 {
				t.Error("the center is blank")
			}
		})
	}
	wg.Wait()
}
