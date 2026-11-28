package plugin

import (
	"context"
	"testing"
	"time"

	"github.com/giuem/ichiwa/internal/source"
)

// acquired tells whether q lets a call in within a moment.
func acquired(t *testing.T, q *queue, ctx context.Context) chan error {
	t.Helper()
	got := make(chan error, 1)
	go func() { got <- q.acquire(ctx) }()
	return got
}

func waits(t *testing.T, got chan error) bool {
	t.Helper()
	select {
	case err := <-got:
		if err != nil {
			t.Fatal(err)
		}
		return false
	case <-time.After(50 * time.Millisecond):
		return true
	}
}

// Calls made ahead of need never keep the user's waiting: they leave a
// slot free, and give their turn to the user's.
func TestBackgroundCallsYield(t *testing.T) {
	q := newQueue(4)
	bg := source.Background(context.Background())
	fg := context.Background()
	for range 3 {
		if waits(t, acquired(t, q, bg)) {
			t.Fatal("a background call waited with slots free")
		}
	}
	if !waits(t, acquired(t, q, bg)) {
		t.Fatal("background calls took the last slot")
	}
	if waits(t, acquired(t, q, fg)) {
		t.Fatal("the user's call waited behind background calls")
	}
	// All four busy: a background call, then a user's, wait; a slot
	// freed goes to the user's.
	fgWait := acquired(t, q, fg)
	if !waits(t, fgWait) {
		t.Fatal("a fifth call ran")
	}
	q.release()
	if waits(t, fgWait) {
		t.Fatal("the user's call waited for a background call that came first")
	}
	// A call that gives up waiting leaves the line.
	ctx, cancel := context.WithCancel(fg)
	gone := acquired(t, q, ctx)
	waits(t, gone)
	cancel()
	if err := <-gone; err == nil {
		t.Fatal("a canceled wait got a slot")
	}
}
