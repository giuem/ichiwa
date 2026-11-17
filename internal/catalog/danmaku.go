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
		text := emojify(strings.Join(strings.Fields(d.Text), " "))
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

// danmakuEmoji are the service's emoji codes, which its own player shows
// as small pictures, and the system emoji closest to each.
var danmakuEmoji = map[string]string{
	"[笑哭]": "😂", "[捂脸]": "🤦", "[赞]": "👍", "[爱慕]": "🥰", "[吃瓜]": "🍉",
	"[偷笑]": "🤭", "[爽]": "😎", "[奸笑]": "😏", "[微笑]": "🙂", "[送花]": "💐",
	"[笑]": "😄", "[爱心]": "❤️", "[大笑]": "😆", "[哭]": "😭", "[尬笑]": "😅",
	"[什么]": "😳", "[鼓掌]": "👏", "[探究]": "🧐", "[委屈]": "🥺", "[没看够]": "🥺",
	"[惊呆]": "😱", "[震惊]": "😱", "[惊讶]": "😮", "[流泪]": "😭", "[快哭了]": "🥹",
	"[害羞]": "😊", "[怒]": "😠", "[生气]": "😠", "[发怒]": "😡", "[呲牙]": "😁",
	"[色]": "😍", "[可怜]": "🥺", "[强]": "💪", "[加油]": "💪", "[抱拳]": "🙏",
	"[比心]": "🫶", "[玫瑰]": "🌹", "[疑问]": "❓", "[再见]": "👋", "[OK]": "👌",
	"[胜利]": "✌️", "[握手]": "🤝", "[飞吻]": "😘", "[亲亲]": "😚", "[擦汗]": "😅",
	"[难过]": "😞", "[心碎]": "💔", "[火]": "🔥", "[灵机一动]": "💡", "[泣不成声]": "😭",
	"[可爱]": "🥰", "[酷]": "😎", "[晕]": "😵", "[睡]": "😴", "[思考]": "🤔",
	"[无语]": "😑", "[白眼]": "🙄", "[坏笑]": "😏", "[得意]": "😏", "[调皮]": "😜",
	"[石化]": "🗿",
}

// emojify turns the emoji codes in a line into emoji, leaving codes it
// does not know as they are.
func emojify(s string) string {
	if !strings.Contains(s, "[") {
		return s
	}
	var b strings.Builder
	for {
		i := strings.IndexByte(s, '[')
		if i < 0 {
			break
		}
		j := strings.IndexByte(s[i:], ']')
		if j < 0 {
			break
		}
		b.WriteString(s[:i])
		// A code inside another bracket, as in "[[笑]", starts at the last.
		if k := strings.LastIndexByte(s[i:i+j], '['); k > 0 {
			b.WriteString(s[i : i+k])
			i += k
			j -= k
		}
		code := s[i : i+j+1]
		if e, ok := danmakuEmoji[code]; ok {
			b.WriteString(e)
		} else {
			b.WriteString(code)
		}
		s = s[i+j+1:]
	}
	b.WriteString(s)
	return b.String()
}
