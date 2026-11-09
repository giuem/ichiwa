package app

import (
	"image"

	"hongguo/internal/mac"
)

func decodePoster(data []byte, maxWidth int) (*image.RGBA, error) {
	return mac.DecodeImage(data, maxWidth)
}
