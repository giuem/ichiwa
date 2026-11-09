//go:build !darwin

package app

import "image"

// decodePoster cannot read HEIC here yet: see issue 15.
func decodePoster(data []byte, maxWidth int) (*image.RGBA, error) {
	return decodeImage(data, maxWidth)
}
