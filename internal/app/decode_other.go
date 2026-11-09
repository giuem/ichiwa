//go:build !darwin

package app

import (
	"image"

	_ "github.com/gen2brain/heic" // decodeImage reads the catalog's HEIC posters
)

func decodePoster(data []byte, maxWidth int) (*image.RGBA, error) {
	return decodeImage(data, maxWidth)
}
