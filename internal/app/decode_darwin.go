package app

import (
	"image"

	"github.com/giuem/ichiwa/internal/mac"
)

func decodePoster(data []byte, maxWidth int) (*image.RGBA, error) {
	return mac.DecodeImage(data, maxWidth)
}
