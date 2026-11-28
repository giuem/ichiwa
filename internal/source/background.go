package source

import "context"

type backgroundKey struct{}

// Background is ctx for calls the host makes ahead of need, as the first
// pages of pages not shown yet: a source lets the calls the user waits for
// go first.
func Background(ctx context.Context) context.Context {
	return context.WithValue(ctx, backgroundKey{}, true)
}

// IsBackground tells ctx is for calls made ahead of need.
func IsBackground(ctx context.Context) bool {
	b, _ := ctx.Value(backgroundKey{}).(bool)
	return b
}
