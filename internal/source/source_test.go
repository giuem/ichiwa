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

	// A cancellation carries no code: only the caller knows whether it
	// canceled the call. A timeout is a plugin failure (§10). A code the
	// source gave wins over a context error behind it.
	for _, ce := range []error{context.Canceled, fmt.Errorf("wrapped: %w", context.Canceled)} {
		if CodeOf(ce) != "" {
			t.Fatalf("CodeOf(%v) = %q, want empty", ce, CodeOf(ce))
		}
		if Message(ce) == "" {
			t.Fatalf("Message(%v) is empty; a source's own cancellation must be shown", ce)
		}
	}
	for _, de := range []error{context.DeadlineExceeded, fmt.Errorf("wrapped: %w", context.DeadlineExceeded)} {
		if CodeOf(de) != ErrPlugin {
			t.Fatalf("CodeOf(%v) = %q, want %q", de, CodeOf(de), ErrPlugin)
		}
	}
	coded := NewError(ErrUnavailable, "该集需要登录才能观看", context.DeadlineExceeded)
	if CodeOf(coded) != ErrUnavailable {
		t.Fatalf("CodeOf(coded timeout) = %q, want %q", CodeOf(coded), ErrUnavailable)
	}
	coded = NewError(ErrRateLimited, "", fmt.Errorf("wrapped: %w", context.Canceled))
	if CodeOf(coded) != ErrRateLimited {
		t.Fatalf("CodeOf(coded cancel) = %q, want %q", CodeOf(coded), ErrRateLimited)
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

	// Non-first page preserves header per §6, and ignores filters and sections.
	nextPage, err := DecodeCollectionPage([]byte(raw), false, opts)
	if err != nil {
		t.Fatal(err)
	}
	if nextPage.Header == nil || nextPage.Header.Title != "演员" {
		t.Fatalf("non-first page should keep header: %+v", nextPage.Header)
	}
	if len(nextPage.Filters) != 0 || len(nextPage.Sections) != 0 {
		t.Fatalf("non-first page should omit filters/sections: %+v", nextPage)
	}
}

func TestDecodeMedia(t *testing.T) {
	if _, err := DecodeMedia([]byte(`{"id":"1","title":"t"}`), CleanOptions{}); !Is(err, ErrPlugin) {
		t.Fatalf("missing episodes should fail as plugin: %v", err)
	}
	opts := CleanOptions{
		AllowHost: func(host string) bool { return host == "cdn.example.com" },
	}
	raw := `{
		"id": "m1",
		"title": " 测试剧 ",
		"aspect": " portrait ",
		"seasons": [
			{"id": "s1", "title": "第一季", "name": " 测试剧 第一季 ", "image": "https://cdn.example.com/s1.jpg", "current": true},
			{"id": "s1", "title": "重复季", "name": "重复"},
			{"id": "s2", "title": "第二季", "name": "测试剧 第二季", "image": "https://evil.example.org/s2.jpg", "available": false, "note": " 即将上线 "}
		],
		"episodes": [
			{"id": "e1", "title": "第一集", "duration": -5, "lock": "bogus"},
			{"id": "e1", "title": "重复的第一集", "duration": 99},
			{"id": "", "title": "bad"},
			{"id": "e2", "duration": 62.5, "lock": " vip "},
			{"id": "e3", "lock": ""},
			{"id": "e4", "lock": true},
			{"id": "e5", "lock": 0},
			{"id": "e6", "lock": null}
		]
	}`
	m, err := DecodeMedia([]byte(raw), opts)
	if err != nil {
		t.Fatal(err)
	}
	if m.Aspect != AspectPortrait {
		t.Fatalf("aspect = %q, want portrait (enum values are trimmed)", m.Aspect)
	}
	if len(m.Seasons) != 2 {
		t.Fatalf("seasons = %+v, want 2 deduplicated", m.Seasons)
	}
	if !m.Seasons[0].IsAvailable() || m.Seasons[0].Name != "测试剧 第一季" || m.Seasons[0].Image != "https://cdn.example.com/s1.jpg" {
		t.Fatalf("season[0] = %+v", m.Seasons[0])
	}
	if m.Seasons[1].IsAvailable() || m.Seasons[1].Name != "测试剧 第二季" || m.Seasons[1].Image != "" || m.Seasons[1].Note != "即将上线" {
		t.Fatalf("season[1] = %+v", m.Seasons[1])
	}
	// An unknown lock is still a lock; " vip " is trimmed, not read as playable.
	// A lock of the wrong type locks too; null and "" do not.
	if len(m.Episodes) != 6 || m.Episodes[0].Title != "第一集" || m.Episodes[0].Duration != 0 ||
		m.Episodes[0].Lock != LockUnavailable || m.Episodes[1].Lock != LockVIP || m.Episodes[2].Lock != LockNone ||
		m.Episodes[3].Lock != LockUnavailable || m.Episodes[4].Lock != LockUnavailable || m.Episodes[5].Lock != LockNone {
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

func TestDecodeTolerantMalformedFields(t *testing.T) {
	// CollectionPage: numeric card id, string badges, string mixed in items,
	// numeric note, numeric meta.value, duplicate card IDs.
	colRaw := `{
		"title": "混合脏数据",
		"header": {
			"title": "某演员",
			"image": 123,
			"meta": [
				{"label": "评分", "value": 8.9},
				{"label": "坏项", "value": {"nested": true}},
				"not-an-object"
			]
		},
		"items": [
			"stray-string",
			123,
			null,
			{"kind": "media", "id": 7423819283746182736, "title": "数字ID剧", "badges": "不是数组", "note": 98765},
			{"kind": "media", "id": "7423819283746182736", "title": "重复ID应被丢弃"},
			{"kind": "media", "id": "ok2", "title": "正常剧", "badges": ["完结", 10, {"bad": 1}], "note": 9.5}
		]
	}`
	page, err := DecodeCollectionPage([]byte(colRaw), true, CleanOptions{})
	if err != nil {
		t.Fatalf("DecodeCollectionPage failed on malformed optional/item fields: %v", err)
	}
	if page.Header == nil || len(page.Header.Meta) != 1 || page.Header.Meta[0].Value != "8.9" || page.Header.Image != "" {
		t.Fatalf("header = %+v", page.Header)
	}
	if len(page.Items) != 2 {
		t.Fatalf("items = %+v, want 2", page.Items)
	}
	if page.Items[0].ID != "7423819283746182736" || page.Items[0].Title != "数字ID剧" || len(page.Items[0].Badges) != 0 || page.Items[0].Note != "98765" {
		t.Fatalf("items[0] = %+v", page.Items[0])
	}
	if page.Items[1].ID != "ok2" || !slices.Equal(page.Items[1].Badges, []string{"完结", "10"}) || page.Items[1].Note != "9.5" {
		t.Fatalf("items[1] = %+v", page.Items[1])
	}

	// Media: numeric media/episode/season IDs, string episode duration dropped,
	// non-object episode skipped, numeric meta.value kept.
	mediaRaw := `{
		"id": 1001,
		"title": "容错剧",
		"meta": [{"value": 80}, "bad-meta"],
		"seasons": [
			"bad-season",
			{"id": 2001, "title": "第1季", "name": 2001, "available": "not-bool"}
		],
		"episodes": [
			"not-an-object",
			{"id": 101, "title": "第1集", "duration": "60"},
			{"id": "102", "duration": 42.5}
		]
	}`
	m, err := DecodeMedia([]byte(mediaRaw), CleanOptions{})
	if err != nil {
		t.Fatalf("DecodeMedia failed on malformed fields: %v", err)
	}
	if m.ID != "1001" || len(m.Meta) != 1 || m.Meta[0].Value != "80" {
		t.Fatalf("media meta = %+v", m)
	}
	if len(m.Seasons) != 1 || m.Seasons[0].ID != "2001" || !m.Seasons[0].IsAvailable() {
		t.Fatalf("seasons = %+v", m.Seasons)
	}
	if len(m.Episodes) != 2 || m.Episodes[0].ID != "101" || m.Episodes[0].Duration != 0 || m.Episodes[1].Duration != 42.5 {
		t.Fatalf("episodes = %+v", m.Episodes)
	}

	// Media: required field wrong type still fails as plugin error.
	if _, err := DecodeMedia([]byte(`{"id":{"bad":1},"title":"t","episodes":[]}`), CleanOptions{}); !Is(err, ErrPlugin) {
		t.Fatalf("wrong type for required id should fail as plugin: %v", err)
	}
	if _, err := DecodeMedia([]byte(`{"id":"1","title":"t","episodes":"not-array"}`), CleanOptions{}); !Is(err, ErrPlugin) {
		t.Fatalf("wrong type for required episodes should fail as plugin: %v", err)
	}

	// Playback: floating point expiresAt, bitrate, width, height are floored.
	pbRaw := `{
		"streams": [
			"bad-stream",
			{
				"url": "https://cdn.example.com/a.mp4",
				"container": "mp4",
				"width": 1080.9,
				"height": 1920.7,
				"bitrate": 2500000.8,
				"headers": {"Referer": "https://cdn.example.com/", "Bad": ["x"]}
			},
			{
				"url": "https://cdn.example.com/bad-enc.mp4",
				"container": "mp4",
				"encryption": "not-an-object"
			}
		],
		"expiresAt": 1700000000.9
	}`
	pb, err := DecodePlayback([]byte(pbRaw), CleanOptions{})
	if err != nil {
		t.Fatalf("DecodePlayback failed on float numbers: %v", err)
	}
	if pb.ExpiresAt != 1700000000 || len(pb.Streams) != 1 {
		t.Fatalf("playback = %+v", pb)
	}
	st := pb.Streams[0]
	if st.Width != 1080 || st.Height != 1920 || st.Bitrate != 2500000 || len(st.Headers) != 1 {
		t.Fatalf("stream = %+v", st)
	}

	// Danmaku: non-object item and string time dropped; duplicate non-empty ID kept first.
	dmRaw := `{
		"items": [
			"bad",
			{"id": 1, "text": "第一条", "time": 5.5},
			{"id": "1", "text": "重复ID", "time": 6},
			{"id": "2", "text": "坏时间", "time": "5"},
			{"text": "无ID一", "time": 1},
			{"text": "无ID二", "time": 2}
		]
	}`
	dm, err := DecodeDanmakuPage([]byte(dmRaw))
	if err != nil {
		t.Fatalf("DecodeDanmakuPage failed: %v", err)
	}
	if len(dm.Items) != 3 || dm.Items[0].Text != "无ID一" || dm.Items[1].Text != "无ID二" || dm.Items[2].ID != "1" || dm.Items[2].Text != "第一条" {
		t.Fatalf("danmaku = %+v", dm.Items)
	}

	// Navigation: non-object skipped, numeric id stringified, duplicates keep first.
	navRaw := `{"items": ["bad", {"id": 1, "title": "首页"}, {"id": "1", "title": "重复"}, {"id": "2", "title": "排行", "icon": 99}]}`
	nav, err := DecodeNavigation([]byte(navRaw))
	if err != nil || len(nav) != 2 || nav[0].ID != "1" || nav[0].Title != "首页" || nav[1].Icon != "" {
		t.Fatalf("nav = %+v, err = %v", nav, err)
	}
}

func TestCleanRefreshDropsCollectionAndDedups(t *testing.T) {
	raw := `{"items": [
		{"kind": "collection", "id": "person:1", "title": "演员"},
		{"kind": "media", "id": "m1", "title": "剧一"},
		{"kind": "media", "id": "m1", "title": "剧一重复"},
		"bad",
		{"kind": "media", "id": 2, "title": "剧二"}
	]}`
	cards, err := DecodeRefresh([]byte(raw), CleanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(cards) != 2 || cards[0].ID != "m1" || cards[0].Title != "剧一" || cards[1].ID != "2" {
		t.Fatalf("refresh cards = %+v", cards)
	}
}

func TestEnumsAreTrimmed(t *testing.T) {
	if got := cleanShape(" circle "); got != ShapeCircle {
		t.Errorf("shape = %q", got)
	}
	if got := cleanLayout(" ranked "); got != LayoutRanked {
		t.Errorf("layout = %q", got)
	}
	if got := cleanSectionLayout(" grid "); got != SectionGrid {
		t.Errorf("section layout = %q", got)
	}
	if got := cleanIcon(" fire "); got != IconFire {
		t.Errorf("icon = %q", got)
	}
	if e, ok := cleanEncryption(&Encryption{Scheme: " cenc ", Key: "00112233445566778899aabbccddeeff"}); !ok || e.Scheme != EncryptionCENC {
		t.Errorf("encryption = %+v, %v", e, ok)
	}
}

func TestDecodePageError(t *testing.T) {
	cases := []struct {
		raw   string
		code  ErrorCode
		msg   string
		retry time.Duration
	}{
		{`{"items":[]}`, "", "", 0},
		{`{"error":null}`, "", "", 0},
		{`{"error":{"code":"network","message":" 列表取不到 "}}`, ErrNetwork, "列表取不到", 0},
		{`{"error":{"code":"rateLimited","retryAfter":12.5}}`, ErrRateLimited, "", 12500 * time.Millisecond},
		{`{"error":{"code":"teapot"}}`, ErrPlugin, "", 0},
		{`{"error":"broken"}`, ErrPlugin, "", 0},
		{`{"error":{"code":"notFound","retryAfter":-3}}`, ErrNotFound, "", 0},
	}
	for _, c := range cases {
		e := DecodePageError([]byte(c.raw))
		if c.code == "" {
			if e != nil {
				t.Errorf("%s: %v", c.raw, e)
			}
			continue
		}
		if e == nil || e.Code != c.code || e.Message != c.msg || e.RetryAfter != c.retry {
			t.Errorf("%s: %+v", c.raw, e)
		}
	}
}

// A host with other than ASCII is dropped before its case is lowered:
// "İ" lowers to "i", but the transport's IDNA maps it to another host.
func TestCleanURLRefusesNonASCIIHosts(t *testing.T) {
	opts := CleanOptions{AllowHost: func(host string) bool { return host == "cdn.big.example" }}
	if got := cleanURL("http://cdn.big.example/v.mp4", opts); got == "" {
		t.Fatal("the declared host was dropped")
	}
	for _, raw := range []string{
		"http://cdn.bİg.example/leak?q=1",
		"http://cdn.b%C4%B0g.example/leak",
		"http://cdn.bíg.example/x",
	} {
		if got := cleanURL(raw, opts); got != "" {
			t.Errorf("%q kept as %q", raw, got)
		}
	}
}
