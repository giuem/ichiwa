package plugin

import (
	"context"
	"slices"
	"sync"

	"github.com/giuem/ichiwa/internal/source"
)

// queue lets at most n calls of a plugin run at once; the others wait, in
// the order they came. A call made ahead of need (source.Background) waits
// for every call the user waits for, and leaves one slot to them.
type queue struct {
	mu     sync.Mutex
	n      int
	busy   int
	fg, bg []chan struct{}
}

func newQueue(n int) *queue { return &queue{n: n} }

// bgLimit is how many slots calls made ahead of need may take.
func (q *queue) bgLimit() int { return max(q.n-1, 1) }

// acquire waits for a slot for a call with ctx, or for ctx to end.
func (q *queue) acquire(ctx context.Context) error {
	background := source.IsBackground(ctx)
	q.mu.Lock()
	if background && q.busy < q.bgLimit() && len(q.fg) == 0 && len(q.bg) == 0 ||
		!background && q.busy < q.n && len(q.fg) == 0 {
		q.busy++
		q.mu.Unlock()
		return nil
	}
	turn := make(chan struct{})
	if background {
		q.bg = append(q.bg, turn)
	} else {
		q.fg = append(q.fg, turn)
	}
	q.mu.Unlock()
	select {
	case <-turn:
		return nil
	case <-ctx.Done():
		q.mu.Lock()
		waiting := slices.Contains(q.fg, turn) || slices.Contains(q.bg, turn)
		q.fg = slices.DeleteFunc(q.fg, func(c chan struct{}) bool { return c == turn })
		q.bg = slices.DeleteFunc(q.bg, func(c chan struct{}) bool { return c == turn })
		q.mu.Unlock()
		if !waiting {
			q.release() // its turn came as it ended
		}
		return ctx.Err()
	}
}

// release frees the slot of a call done, for the first call waiting.
func (q *queue) release() {
	q.mu.Lock()
	defer q.mu.Unlock()
	switch {
	case len(q.fg) > 0:
		close(q.fg[0])
		q.fg = q.fg[1:]
	case len(q.bg) > 0 && q.busy <= q.bgLimit():
		close(q.bg[0])
		q.bg = q.bg[1:]
	default:
		q.busy--
	}
}
