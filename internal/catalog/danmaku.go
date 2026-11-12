package catalog

import (
	"context"
	"strings"
	"time"

	"github.com/beilunyang/openhongguo/sdk"
)

// Danmaku is one comment flying over an episode: its text and when in the
// episode it appears.
type Danmaku struct {
	ID   string
	Text string
	At   time.Duration
}

// DanmakuPage is one page of an episode's danmaku: the comments, sorted by
// when they appear, and the cursor of the next page, empty after the last.
type DanmakuPage struct {
	Items []Danmaku
	Next  string
}

// Danmaku loads a page of the danmaku of an episode of a drama, from its
// start when cursor is empty. A page is a stretch of the episode, which
// can hold none, so a caller pages on until Next is empty.
func (b *Backend) Danmaku(ctx context.Context, seriesID, episodeID, cursor string) (*DanmakuPage, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	page, err := b.client.ListDanmaku(ctx, seriesID, episodeID, sdk.DanmakuOptions{Cursor: cursor})
	if err != nil {
		return nil, err
	}
	out := &DanmakuPage{Items: make([]Danmaku, 0, len(page.Items))}
	for _, d := range page.Items {
		text := strings.Join(strings.Fields(d.Text), " ")
		if text == "" || d.OffsetMS < 0 {
			continue
		}
		out.Items = append(out.Items, Danmaku{ID: d.ID, Text: text, At: time.Duration(d.OffsetMS) * time.Millisecond})
	}
	if page.HasMore && page.Cursor != "" && page.Cursor != cursor {
		out.Next = page.Cursor
	}
	return out, nil
}
