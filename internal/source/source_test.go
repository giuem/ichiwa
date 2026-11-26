package source

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestInfoHas(t *testing.T) {
	base := Info{ID: "demo", Capabilities: []Capability{CapDanmaku, CapSearchHome}}
	if !base.Has(CapDanmaku) {
		t.Fatal("missing declared capability danmaku")
	}
	if base.Has(CapSearchHome) {
		t.Fatal("search.home must require search")
	}
	if base.Has(CapSearchSuggest) {
		t.Fatal("search.suggest must require search")
	}
	if base.Has("unknown.cap") {
		t.Fatal("unknown capability reported true")
	}

	full := Info{ID: "demo", Capabilities: []Capability{CapSearch, CapSearchHome, CapSearchSuggest, CapRefresh}}
	for _, c := range []Capability{CapSearch, CapSearchHome, CapSearchSuggest, CapRefresh} {
		if !full.Has(c) {
			t.Errorf("expected capability %q", c)
		}
	}
}

func TestErrors(t *testing.T) {
	cause := errors.New("boom")
	e := NewError(ErrUnavailable, "  该集已下架  ", cause)
	if !Is(e, ErrUnavailable) || CodeOf(e) != ErrUnavailable {
		t.Fatalf("code = %q", CodeOf(e))
	}
	if !errors.Is(e, cause) {
		t.Fatal("Unwrap lost cause")
	}
	if Message(e) != "该集已下架" {
		t.Fatalf("Message = %q", Message(e))
	}
	if RetryAfter(e) != 0 {
		t.Fatalf("RetryAfter = %v, want 0", RetryAfter(e))
	}

	wrapped := fmt.Errorf("outer: %w", RateLimitedError("", 0, nil))
	if !Is(wrapped, ErrRateLimited) || RetryAfter(wrapped) != DefaultRetryAfter {
		t.Fatalf("rate limited: code=%q retry=%v", CodeOf(wrapped), RetryAfter(wrapped))
	}
	if Message(wrapped) != DefaultMessage(ErrRateLimited) {
		t.Fatalf("default rateLimited message = %q", Message(wrapped))
	}

	custom := RateLimitedError("稍候", 12*time.Second, nil)
	if RetryAfter(custom) != 12*time.Second {
		t.Fatalf("custom RetryAfter = %v", RetryAfter(custom))
	}

	// Non-protocol errors classify as ErrPlugin.
	plain := errors.New("unexpected")
	if !Is(plain, ErrPlugin) || CodeOf(plain) != ErrPlugin {
		t.Fatalf("plain error code = %q", CodeOf(plain))
	}
	if CodeOf(nil) != "" || Is(nil, ErrPlugin) {
		t.Fatal("nil error should have empty code")
	}

	// Unknown error code normalizes to ErrPlugin and long messages are capped.
	longMsg := strings.Repeat("字", MaxErrorMessageRunes+20)
	unknown := NewError("bogus", longMsg, nil)
	if unknown.Code != ErrPlugin {
		t.Fatalf("unknown code = %q, want plugin", unknown.Code)
	}
	if got := len([]rune(unknown.Message)); got != MaxErrorMessageRunes {
		t.Fatalf("message runes = %d, want %d", got, MaxErrorMessageRunes)
	}
	if got := Message(context.DeadlineExceeded); got == "" {
		t.Fatal("deadline exceeded should produce a message")
	}
}

func TestDecodeNavigation(t *testing.T) {
	if _, err := DecodeNavigation([]byte(`[]`)); !Is(err, ErrPlugin) {
		t.Fatalf("array should fail as plugin: %v", err)
	}
	if _, err := DecodeNavigation([]byte(`{}`)); !Is(err, ErrPlugin) {
		t.Fatalf("missing items should fail as plugin: %v", err)
	}
	if _, err := DecodeNavigation([]byte(`{"items": [{"id":"","title":"bad"}]}`)); !Is(err, ErrPlugin) {
		t.Fatalf("all invalid items should fail as plugin: %v", err)
	}

	raw := `{"items":[
		{"id":" ch1 ","title":" 推荐频道超长名字测试 ","icon":"home"},
		{"id":"ch2","title":"排行","icon":"bogus"},
		{"id":"","title":"skip"},
		{"id":"ch3","title":"   "}
	]}`
	items, err := DecodeNavigation([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d items: %+v", len(items), items)
	}
	if items[0].ID != "ch1" || items[0].Title != "推荐频道超长名字" || items[0].Icon != IconHome {
		t.Fatalf("item[0] = %+v", items[0])
	}
	if items[1].Icon != "" {
		t.Fatalf("unknown icon should be cleared: %+v", items[1])
	}

	// Cap at MaxNavItems.
	var many []NavItem
	for i := range MaxNavItems + 5 {
		many = append(many, NavItem{ID: fmt.Sprintf("c%d", i), Title: "项"})
	}
	got, err := CleanNavigation(many)
	if err != nil || len(got) != MaxNavItems {
		t.Fatalf("len = %d, err = %v", len(got), err)
	}
}

func TestCleanCollectionPageAndCards(t *testing.T) {
	opts := CleanOptions{
		AllowHost: func(host string) bool { return host == "cdn.example.com" },
	}
	longID := strings.Repeat("a", MaxIDBytes+1)
	longCursor := strings.Repeat("c", MaxCursorBytes+1)
	raw := fmt.Sprintf(`{
		"title": " 精选 ",
		"layout": "unknown",
		"header": {"title": " 演员 ", "image": "https://cdn.example.com/a.jpg", "shape": "circle", "meta": [{"value":" 100万 "},{"value":""}]},
		"filters": [
			{"key":"genre","title":"题材","options":[{"value":"","title":"全部"},{"value":"1","title":"甜宠"}],"value":"1"},
			{"key":"","title":"bad","options":[{"value":"x","title":"x"}]}
		],
		"sections": [
			{"title":"热播","layout":"bad","collection":"hot:1"},
			{"title":"empty"}
		],
		"items": [
			{"kind":"media","id":" 1 ","title":" 剧一 ","image":"https://cdn.example.com/1.jpg","shape":"weird","badges":[" 完结 "," 超长角标超过八个字 ","第三个被丢"],"note":"%s"},
			{"kind":"media","id":"2","title":"剧二","image":"https://evil.example.org/2.jpg"},
			{"kind":"media","id":"3","title":"剧三","image":"javascript:alert(1)"},
			{"kind":"bad","id":"4","title":"bad kind"},
			{"kind":"media","id":"%s","title":"too long id"},
			{"kind":"media","id":"5","title":"   "}
		],
		"cursor": "%s"
	}`, strings.Repeat("热", MaxNoteRunes+5), longID, longCursor)

	page, err := DecodeCollectionPage([]byte(raw), true, opts)
	if err != nil {
		t.Fatal(err)
	}
	if page.Title != "精选" || page.Layout != LayoutGrid {
		t.Fatalf("page meta = %+v", page)
	}
	if page.Header == nil || page.Header.Title != "演员" || page.Header.Shape != ShapeCircle || len(page.Header.Meta) != 1 {
		t.Fatalf("header = %+v", page.Header)
	}
	if len(page.Filters) != 1 || len(page.Sections) != 1 || page.Sections[0].Layout != SectionRow {
		t.Fatalf("filters=%+v sections=%+v", page.Filters, page.Sections)
	}
	if len(page.Items) != 3 {
		t.Fatalf("items = %+v", page.Items)
	}
	c0 := page.Items[0]
	if c0.ID != "1" || c0.Title != "剧一" || c0.Shape != ShapePortrait || c0.Image != "https://cdn.example.com/1.jpg" {
		t.Fatalf("c0 = %+v", c0)
	}
	if !slices.Equal(c0.Badges, []string{"完结", "超长角标超过八个"}) {
		t.Fatalf("badges = %v", c0.Badges)
	}
	if len([]rune(c0.Note)) != MaxNoteRunes {
		t.Fatalf("note runes = %d", len([]rune(c0.Note)))
	}
	if page.Items[1].Image != "" || page.Items[2].Image != "" {
		t.Fatalf("disallowed URLs should be stripped: %+v %+v", page.Items[1], page.Items[2])
	}
	if page.Cursor != "" {
		t.Fatalf("overlong cursor should be cleared, got len %d", len(page.Cursor))
	}

	// Non-first page ignores header, filters, and sections.
	nextPage, err := DecodeCollectionPage([]byte(raw), false, opts)
	if err != nil {
		t.Fatal(err)
	}
	if nextPage.Header != nil || len(nextPage.Filters) != 0 || len(nextPage.Sections) != 0 {
		t.Fatalf("non-first page should omit header/filters/sections: %+v", nextPage)
	}
}

func TestDecodeMedia(t *testing.T) {
	if _, err := DecodeMedia([]byte(`{"id":"1","title":"t"}`), CleanOptions{}); !Is(err, ErrPlugin) {
		t.Fatalf("missing episodes should fail as plugin: %v", err)
	}
	raw := `{
		"id": "m1",
		"title": " 测试剧 ",
		"aspect": "unknown",
		"seasons": [
			{"id": "s1", "title": "第一季", "current": true},
			{"id": "s2", "title": "第二季", "available": false, "note": " 即将上线 "}
		],
		"episodes": [
			{"id": "e1", "duration": -5, "lock": "bogus"},
			{"id": "", "title": "bad"},
			{"id": "e2", "duration": 62.5, "lock": "vip"}
		]
	}`
	m, err := DecodeMedia([]byte(raw), CleanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if m.Aspect != AspectLandscape {
		t.Fatalf("aspect = %q, want landscape", m.Aspect)
	}
	if len(m.Seasons) != 2 || !m.Seasons[0].IsAvailable() || m.Seasons[1].IsAvailable() || m.Seasons[1].Note != "即将上线" {
		t.Fatalf("seasons = %+v", m.Seasons)
	}
	if len(m.Episodes) != 2 || m.Episodes[0].Duration != 0 || m.Episodes[0].Lock != LockNone || m.Episodes[1].Lock != LockVIP {
		t.Fatalf("episodes = %+v", m.Episodes)
	}
}

func TestDecodePlayback(t *testing.T) {
	opts := CleanOptions{
		AllowHost: func(host string) bool { return host == "media.example.com" },
	}
	raw := `{
		"streams": [
			{
				"url": "https://media.example.com/v1.mp4",
				"backupUrls": ["https://media.example.com/v2.mp4", "https://evil.com/v3.mp4"],
				"container": "mp4",
				"headers": {
					"Referer": "https://media.example.com/",
					"Host": "spoof.example.com",
					"Range": "bytes=0-10",
					"Connection": "close",
					"Content-Length": "99",
					"Transfer-Encoding": "chunked",
					"X-Bad": "a\r\nb"
				},
				"width": 1080,
				"height": 1920,
				"encryption": {"scheme": "cenc", "key": "00112233445566778899AABBCCDDEEFF"}
			},
			{"url": "https://media.example.com/bad-enc.mp4", "container": "mp4", "encryption": {"scheme": "cenc", "key": "short"}},
			{"url": "https://media.example.com/bad-container.flv", "container": "flv"},
			{"url": "https://evil.com/v.mp4", "container": "mp4"}
		],
		"expiresAt": 1700000000
	}`
	pb, err := DecodePlayback([]byte(raw), opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(pb.Streams) != 1 || pb.ExpiresAt != 1700000000 {
		t.Fatalf("playback = %+v", pb)
	}
	s := pb.Streams[0]
	if !slices.Equal(s.BackupURLs, []string{"https://media.example.com/v2.mp4"}) {
		t.Fatalf("backupUrls = %v", s.BackupURLs)
	}
	if len(s.Headers) != 1 || s.Headers["Referer"] != "https://media.example.com/" {
		t.Fatalf("headers = %v", s.Headers)
	}
	if s.Encryption == nil || s.Encryption.Key != "00112233445566778899aabbccddeeff" {
		t.Fatalf("encryption = %+v", s.Encryption)
	}
}

func TestDanmakuSuggestRefreshAndMaxSize(t *testing.T) {
	page, err := CleanDanmakuPage(&DanmakuPage{
		Items: []Danmaku{
			{ID: "2", Text: " late ", Time: 10.5},
			{ID: "3", Text: "   ", Time: 1},
			{ID: "4", Text: "neg", Time: -1},
			{ID: "5", Text: "nan", Time: math.NaN()},
			{ID: "1", Text: "early", Time: 2.25},
		},
		Cursor: " next ",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 || page.Items[0].Text != "early" || page.Items[1].Text != "late" || page.Cursor != "next" {
		t.Fatalf("danmaku = %+v", page)
	}

	sugg, err := DecodeSuggest([]byte(`{"items":[" a ","a","b","","c","d","e","f","g","h","i","j","k"]}`))
	if err != nil || len(sugg) != MaxSuggestions || sugg[0] != "a" {
		t.Fatalf("suggest = %v, err = %v", sugg, err)
	}

	var cards []Card
	for i := range MaxRefreshIDs + 5 {
		cards = append(cards, Card{Kind: CardMedia, ID: fmt.Sprintf("%d", i), Title: "t"})
	}
	if got := CleanRefresh(cards, CleanOptions{}); len(got) != MaxRefreshIDs {
		t.Fatalf("refresh len = %d, want %d", len(got), MaxRefreshIDs)
	}

	huge := make([]byte, MaxResponseBytes+1)
	if _, err := DecodeCollectionPage(huge, true, CleanOptions{}); !Is(err, ErrPlugin) {
		t.Fatalf("oversized response should fail as plugin: %v", err)
	}
}
