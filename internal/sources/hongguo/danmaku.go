package hongguo

import (
	"context"
	"strings"
	"time"

	"github.com/beilunyang/openhongguo/sdk"

	"github.com/giuem/ichiwa/internal/source"
)

func (s *Source) Danmaku(ctx context.Context, r source.DanmakuRequest) (*source.DanmakuPage, error) {
	ctx, cancel := withTimeout(ctx, 30*time.Second)
	defer cancel()
	page, err := s.client.ListDanmaku(ctx, r.MediaID, r.EpisodeID, sdk.DanmakuOptions{Cursor: r.Cursor})
	if err != nil {
		return nil, wrapErr(err)
	}
	out := &source.DanmakuPage{Items: make([]source.Danmaku, 0, len(page.Items))}
	for _, d := range page.Items {
		text := emojify(strings.Join(strings.Fields(d.Text), " "))
		if text == "" || d.OffsetMS < 0 {
			continue
		}
		out.Items = append(out.Items, source.Danmaku{
			ID:   d.ID,
			Text: text,
			Time: float64(d.OffsetMS) / 1000.0,
		})
	}
	if page.HasMore && page.Cursor != "" && page.Cursor != r.Cursor {
		out.Cursor = page.Cursor
	}
	return out, nil
}

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
