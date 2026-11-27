package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/giuem/ichiwa/internal/source"
)

func open(t *testing.T, dir string, opts Options) *Plugin {
	t.Helper()
	p, err := Open(filepath.Join("testdata", dir), opts)
	if err != nil {
		t.Fatalf("Open(%s): %v", dir, err)
	}
	return p
}

func code(err error) source.ErrorCode { return source.CodeOf(err) }

func TestExampleAllFunctions(t *testing.T) {
	p := open(t, "example", Options{})
	ctx := context.Background()
	info := p.Info()
	if info.ID != "example" || info.Name != "示例片源" || len(info.Network) != 4 || info.ImageHeaders["Referer"] == "" {
		t.Fatalf("Info = %+v", info)
	}
	for _, c := range []source.Capability{source.CapSearch, source.CapSearchSuggest, source.CapSearchHome, source.CapDanmaku, source.CapRefresh} {
		if !info.Has(c) {
			t.Errorf("missing capability %s", c)
		}
	}

	nav, err := p.Navigation(ctx)
	if err != nil || len(nav) != 3 || nav[0].ID != "home" || nav[0].Icon != source.IconHome {
		t.Fatalf("Navigation = %+v, %v", nav, err)
	}

	home, err := p.Collection(ctx, source.CollectionRequest{ID: "home"})
	if err != nil || len(home.Items) != 2 || home.Cursor != "2" || len(home.Sections) != 2 {
		t.Fatalf("home = %+v, %v", home, err)
	}
	if home.Sections[0].Items[0].Kind != source.CardCollection || home.Sections[0].Items[0].Shape != source.ShapeCircle {
		t.Errorf("person card = %+v", home.Sections[0].Items[0])
	}
	if !strings.HasPrefix(home.Items[0].Image, "https://picsum.photos/") {
		t.Errorf("image dropped: %+v", home.Items[0])
	}
	next, err := p.Collection(ctx, source.CollectionRequest{ID: "home", Cursor: home.Cursor})
	if err != nil || len(next.Items) != 2 || next.Sections != nil {
		t.Fatalf("home page 2 = %+v, %v", next, err)
	}

	rank, err := p.Collection(ctx, source.CollectionRequest{ID: "rank", Filters: map[string]string{"board": "new"}})
	if err != nil || rank.Layout != source.LayoutRanked || rank.Filters[0].Value != "new" || rank.Items[0].ID != "bipbop" {
		t.Fatalf("rank = %+v, %v", rank, err)
	}

	filter, err := p.Collection(ctx, source.CollectionRequest{ID: "filter", Filters: map[string]string{"genre": "奇幻"}})
	if err != nil || len(filter.Filters) != 2 || len(filter.Filters[1].Options) != 2 || filter.Items[0].ID != "sintel" {
		t.Fatalf("filter = %+v, %v", filter, err)
	}
	if filter.Filters[0].Options[0].Value != "" {
		t.Errorf("an option without value is the empty value: %+v", filter.Filters[0].Options[0])
	}

	// A page whose list failed: the page and the error both (§6).
	broken, err := p.Collection(ctx, source.CollectionRequest{ID: "filter", Filters: map[string]string{"genre": "broken"}})
	if broken == nil || len(broken.Filters) != 2 || code(err) != source.ErrNetwork || source.Message(err) != "列表暂时取不到，请重试" {
		t.Fatalf("broken = %+v, %v", broken, err)
	}

	person, err := p.Collection(ctx, source.CollectionRequest{ID: "person:p1"})
	if err != nil || person.Header == nil || person.Header.Title != "大兔子" || len(person.Items) != 2 {
		t.Fatalf("person = %+v, %v", person, err)
	}
	if _, err := p.Collection(ctx, source.CollectionRequest{ID: "nope"}); code(err) != source.ErrNotFound {
		t.Fatalf("unknown collection: %v", err)
	}

	m, err := p.Media(ctx, "bunny")
	if err != nil || len(m.Episodes) != 6 || len(m.Seasons) != 3 || m.Episodes[5].Lock != source.LockVIP || m.Episodes[0].Lock != source.LockNone {
		t.Fatalf("media = %+v, %v", m, err)
	}
	if m.Seasons[2].IsAvailable() || !m.Seasons[0].Current || m.Sections[0].Collection != "related:bunny" {
		t.Errorf("seasons/sections = %+v %+v", m.Seasons, m.Sections)
	}
	// The second call comes from host.cache.
	if m2, err := p.Media(ctx, "bunny"); err != nil || m2.Title != m.Title {
		t.Fatalf("cached media = %+v, %v", m2, err)
	}

	pb, err := p.Play(ctx, source.PlayRequest{MediaID: "bunny", EpisodeID: "bunny-1", Prefer: source.PlayPrefer{MaxHeight: 1080, HEVC: true}})
	if err != nil || len(pb.Streams) != 3 || pb.Streams[1].Codec != "hevc" || pb.ExpiresAt == 0 {
		t.Fatalf("play = %+v, %v", pb, err)
	}
	if _, err := p.Play(ctx, source.PlayRequest{MediaID: "bunny", EpisodeID: "bunny-6"}); code(err) != source.ErrUnavailable {
		t.Fatalf("locked play: %v", err)
	}
	hls, err := p.Play(ctx, source.PlayRequest{MediaID: "bipbop", EpisodeID: "bipbop-1"})
	if err != nil || hls.Streams[0].Container != source.ContainerHLS {
		t.Fatalf("hls = %+v, %v", hls, err)
	}

	res, err := p.Search(ctx, source.SearchRequest{Query: "兔子"})
	if err != nil || len(res.Items) != 2 {
		t.Fatalf("search = %+v, %v", res, err)
	}
	sug, err := p.Suggest(ctx, "兔")
	if err != nil || len(sug) != 2 {
		t.Fatalf("suggest = %v, %v", sug, err)
	}
	hot, err := p.SearchHome(ctx)
	if err != nil || len(hot.Sections) != 2 || hot.Sections[0].Layout != source.SectionRanked {
		t.Fatalf("searchHome = %+v, %v", hot, err)
	}
	dm, err := p.Danmaku(ctx, source.DanmakuRequest{MediaID: "bunny", EpisodeID: "bunny-1"})
	if err != nil || len(dm.Items) != 10 || dm.Cursor != "5" || len(dm.Items[0].ID) != 32 {
		t.Fatalf("danmaku = %+v, %v", dm, err)
	}
	dm2, err := p.Danmaku(ctx, source.DanmakuRequest{MediaID: "bunny", EpisodeID: "bunny-1", Cursor: dm.Cursor})
	if err != nil || dm2.Cursor != "" || dm2.Items[0].Time != 5 {
		t.Fatalf("danmaku page 2 = %+v, %v", dm2, err)
	}
	cards, err := p.Refresh(ctx, []string{"sintel", "nope", "bunny"})
	if err != nil || len(cards) != 2 {
		t.Fatalf("refresh = %+v, %v", cards, err)
	}

	// The navigation counted its launches in host.storage.
	if v, _ := p.storage.Get("launches"); v != "1" {
		t.Errorf("launches = %q", v)
	}
	if !strings.Contains(p.Logs().Text(), "navigation，第 1 次") {
		t.Errorf("log lacks the console line:\n%s", p.Logs().Text())
	}
}

func TestUnsupportedCapability(t *testing.T) {
	p := open(t, "hostile/throws", Options{})
	if _, err := p.Search(context.Background(), source.SearchRequest{Query: "x"}); !errors.Is(err, source.ErrUnsupported) {
		t.Fatalf("Search without the capability: %v", err)
	}
	if _, err := p.Danmaku(context.Background(), source.DanmakuRequest{}); !errors.Is(err, source.ErrUnsupported) {
		t.Fatalf("Danmaku without the capability: %v", err)
	}
}

func TestErrorCodes(t *testing.T) {
	p := open(t, "hostile/throws", Options{Timeout: 2 * time.Second})
	ctx := context.Background()
	call := func(id string) error {
		_, err := p.Collection(ctx, source.CollectionRequest{ID: id})
		return err
	}
	err := call("rate")
	if code(err) != source.ErrRateLimited || source.RetryAfter(err) != 12*time.Second || source.Message(err) != "慢一点" {
		t.Errorf("rate: %v (%v)", err, source.RetryAfter(err))
	}
	if err := call("gone"); code(err) != source.ErrNotFound {
		t.Errorf("gone: %v", err)
	}
	for _, id := range []string{"unknown-code", "fake", "string", "async", "number", "never"} {
		if err := call(id); code(err) != source.ErrPlugin {
			t.Errorf("%s: code %q, %v", id, code(err), err)
		}
	}
	if err := call("timer"); err != nil {
		t.Errorf("a promise a timer settles: %v", err)
	}
	if !strings.Contains(p.Logs().Text(), "异步出错") {
		t.Errorf("the log lacks the thrown error:\n%s", p.Logs().Text())
	}
}

func TestTopLevelThrow(t *testing.T) {
	p := open(t, "hostile/toplevel", Options{})
	if _, err := p.Collection(context.Background(), source.CollectionRequest{ID: "x"}); code(err) != source.ErrPlugin {
		t.Fatalf("top-level throw: %v", err)
	}
	if !strings.Contains(p.Logs().Text(), "顶层出错") {
		t.Errorf("log:\n%s", p.Logs().Text())
	}
}

func TestFreshRuntimePerCall(t *testing.T) {
	p := open(t, "hostile/globals", Options{})
	for range 3 {
		page, err := p.Collection(context.Background(), source.CollectionRequest{ID: "x"})
		if err != nil || page.Title != "1,1" {
			t.Fatalf("state leaked between calls: %+v, %v", page, err)
		}
	}
}

func TestCanceledCallIsNotAnError(t *testing.T) {
	p := open(t, "hostile/loop", Options{})
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	_, err := p.Collection(ctx, source.CollectionRequest{ID: "x"})
	if !errors.Is(err, context.Canceled) || code(err) != "" {
		t.Fatalf("canceled call: %v", err)
	}
}

func TestRawCallReturnsJSON(t *testing.T) {
	p := open(t, "example", Options{})
	raw, err := p.Call(context.Background(), "suggest", map[string]string{"query": "辛"})
	if err != nil {
		t.Fatal(err)
	}
	var v struct{ Items []string }
	if err := json.Unmarshal(raw, &v); err != nil || len(v.Items) != 1 || v.Items[0] != "辛特尔" {
		t.Fatalf("raw = %s, %v", raw, err)
	}
}
