package source

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"slices"
	"strings"
	"unicode/utf8"
)

// Protocol limits defined in docs/plugin-design.md.
const (
	MaxResponseBytes     = 4 << 20 // 4 MiB JSON per call
	MaxIDBytes           = 1024
	MaxCursorBytes       = 4096
	MaxPageItems         = 200
	MaxEpisodes          = 5000
	MaxNavItems          = 12
	MaxSuggestions       = 10
	MaxRefreshIDs        = 50
	MaxBadges            = 2
	MaxBadgeRunes        = 8
	MaxNoteRunes         = 24
	MaxNavTitleRunes     = 8
	MaxErrorMessageRunes = 120
)

// CleanOptions configures validation and sanitization of source responses.
type CleanOptions struct {
	// AllowHost reports whether URLs targeting host (hostname without port) are
	// permitted. When nil, any non-empty http/https host is accepted.
	AllowHost func(host string) bool
}

// forbiddenStreamHeaders lists HTTP headers a stream is not allowed to set.
var forbiddenStreamHeaders = map[string]bool{
	"host":              true,
	"range":             true,
	"connection":        true,
	"content-length":    true,
	"transfer-encoding": true,
}

// AllowedImageHeaders lists HTTP headers a source manifest may attach to image
// downloads.
var AllowedImageHeaders = map[string]bool{
	"referer":    true,
	"user-agent": true,
	"origin":     true,
}

func truncateRunes(s string, maxRunes int) string {
	if maxRunes <= 0 || utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	n := 0
	for _, r := range s {
		if n >= maxRunes {
			break
		}
		b.WriteRune(r)
		n++
	}
	return b.String()
}

func validID(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" || len(s) > MaxIDBytes {
		return "", false
	}
	return s, true
}

func cleanCursor(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > MaxCursorBytes {
		return ""
	}
	return s
}

func cleanURL(raw string, opts CleanOptions) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return ""
	}
	host := u.Hostname()
	if host == "" {
		return ""
	}
	if opts.AllowHost != nil && !opts.AllowHost(strings.ToLower(host)) {
		return ""
	}
	return u.String()
}

func cleanShape(s Shape) Shape {
	switch s {
	case ShapePortrait, ShapeLandscape, ShapeSquare, ShapeCircle:
		return s
	default:
		return ShapePortrait
	}
}

func cleanLayout(l Layout) Layout {
	switch l {
	case LayoutGrid, LayoutList, LayoutRanked:
		return l
	default:
		return LayoutGrid
	}
}

func cleanSectionLayout(l SectionLayout) SectionLayout {
	switch l {
	case SectionRow, SectionGrid, SectionRanked:
		return l
	default:
		return SectionRow
	}
}

func cleanIcon(ic Icon) Icon {
	switch ic {
	case IconHome, IconFire, IconStar, IconSparkles, IconTrophy, IconFilter,
		IconFilm, IconTV, IconDrama, IconAnime, IconMusic, IconNews,
		IconSport, IconKids, IconCalendar, IconGrid:
		return ic
	default:
		return ""
	}
}

func cleanAspect(a Aspect, fallback Aspect) Aspect {
	switch a {
	case AspectPortrait, AspectLandscape:
		return a
	default:
		return fallback
	}
}

func cleanLock(l Lock) Lock {
	switch l {
	case LockLogin, LockVIP, LockPaid, LockUnavailable:
		return l
	default:
		return LockNone
	}
}

func cleanBadges(in []string) []string {
	var out []string
	for _, b := range in {
		b = truncateRunes(strings.TrimSpace(b), MaxBadgeRunes)
		if b == "" {
			continue
		}
		out = append(out, b)
		if len(out) >= MaxBadges {
			break
		}
	}
	return out
}

func cleanMeta(in []Meta) []Meta {
	var out []Meta
	for _, m := range in {
		v := strings.TrimSpace(m.Value)
		if v == "" {
			continue
		}
		out = append(out, Meta{Label: strings.TrimSpace(m.Label), Value: v})
	}
	return out
}

func cleanTags(in []string) []string {
	var out []string
	for _, t := range in {
		t = strings.TrimSpace(t)
		if t != "" {
			out = append(out, t)
		}
	}
	return out
}

// CleanCard validates and normalizes a single Card. It returns false when the
// card is missing required fields or has an invalid kind/id and must be dropped.
func CleanCard(c Card, opts CleanOptions) (Card, bool) {
	if c.Kind != CardMedia && c.Kind != CardCollection {
		return Card{}, false
	}
	id, ok := validID(c.ID)
	if !ok {
		return Card{}, false
	}
	title := strings.TrimSpace(c.Title)
	if title == "" {
		return Card{}, false
	}
	return Card{
		Kind:     c.Kind,
		ID:       id,
		Title:    title,
		Subtitle: strings.TrimSpace(c.Subtitle),
		Image:    cleanURL(c.Image, opts),
		Shape:    cleanShape(c.Shape),
		Badges:   cleanBadges(c.Badges),
		Note:     truncateRunes(strings.TrimSpace(c.Note), MaxNoteRunes),
	}, true
}

func cleanCards(in []Card, limit int, opts CleanOptions) []Card {
	var out []Card
	for _, c := range in {
		cleaned, ok := CleanCard(c, opts)
		if !ok {
			continue
		}
		out = append(out, cleaned)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func cleanHeader(h *Header, opts CleanOptions) *Header {
	if h == nil {
		return nil
	}
	title := strings.TrimSpace(h.Title)
	if title == "" {
		return nil
	}
	return &Header{
		Title:       title,
		Image:       cleanURL(h.Image, opts),
		Shape:       cleanShape(h.Shape),
		Description: strings.TrimSpace(h.Description),
		Meta:        cleanMeta(h.Meta),
	}
}

func cleanFilters(in []FilterRow) []FilterRow {
	var out []FilterRow
	for _, row := range in {
		key := strings.TrimSpace(row.Key)
		title := strings.TrimSpace(row.Title)
		if key == "" || title == "" {
			continue
		}
		var opts []FilterOption
		for _, o := range row.Options {
			ot := strings.TrimSpace(o.Title)
			if ot == "" {
				continue
			}
			opts = append(opts, FilterOption{Value: strings.TrimSpace(o.Value), Title: ot})
		}
		if len(opts) == 0 {
			continue
		}
		out = append(out, FilterRow{
			Key:     key,
			Title:   title,
			Options: opts,
			Value:   strings.TrimSpace(row.Value),
		})
	}
	return out
}

func cleanSections(in []Section, opts CleanOptions) []Section {
	var out []Section
	for _, s := range in {
		title := strings.TrimSpace(s.Title)
		if title == "" {
			continue
		}
		items := cleanCards(s.Items, MaxPageItems, opts)
		col, _ := validID(s.Collection)
		more, _ := validID(s.More)
		if len(items) == 0 && col == "" {
			continue
		}
		out = append(out, Section{
			Title:      title,
			Layout:     cleanSectionLayout(s.Layout),
			Items:      items,
			Collection: col,
			More:       more,
		})
	}
	return out
}

// CleanNavigation validates and sanitizes navigation items. At least one valid
// item is required; items beyond MaxNavItems are dropped.
func CleanNavigation(in []NavItem) ([]NavItem, error) {
	var out []NavItem
	for _, it := range in {
		id, ok := validID(it.ID)
		if !ok {
			continue
		}
		title := truncateRunes(strings.TrimSpace(it.Title), MaxNavTitleRunes)
		if title == "" {
			continue
		}
		out = append(out, NavItem{
			ID:    id,
			Title: title,
			Icon:  cleanIcon(it.Icon),
		})
		if len(out) >= MaxNavItems {
			break
		}
	}
	if len(out) == 0 {
		return nil, NewError(ErrPlugin, "导航列表为空", nil)
	}
	return out, nil
}

// CleanCollectionPage validates and sanitizes a CollectionPage. When firstPage
// is false (a paginated request with a cursor), filters and sections are ignored.
func CleanCollectionPage(p *CollectionPage, firstPage bool, opts CleanOptions) (*CollectionPage, error) {
	if p == nil {
		return nil, NewError(ErrPlugin, "集合响应为空", nil)
	}
	out := &CollectionPage{
		Title:  strings.TrimSpace(p.Title),
		Layout: cleanLayout(p.Layout),
		Items:  cleanCards(p.Items, MaxPageItems, opts),
		Cursor: cleanCursor(p.Cursor),
	}
	if firstPage {
		out.Header = cleanHeader(p.Header, opts)
		out.Filters = cleanFilters(p.Filters)
		out.Sections = cleanSections(p.Sections, opts)
	}
	return out, nil
}

// CleanMedia validates and sanitizes a Media detail response. Missing id, title,
// or valid episodes fails with ErrPlugin.
func CleanMedia(m *Media, opts CleanOptions) (*Media, error) {
	if m == nil {
		return nil, NewError(ErrPlugin, "媒体详情为空", nil)
	}
	id, ok := validID(m.ID)
	if !ok {
		return nil, NewError(ErrPlugin, "媒体详情缺少有效 ID", nil)
	}
	title := strings.TrimSpace(m.Title)
	if title == "" {
		return nil, NewError(ErrPlugin, "媒体详情缺少标题", nil)
	}
	var episodes []Episode
	for _, ep := range m.Episodes {
		epID, ok := validID(ep.ID)
		if !ok {
			continue
		}
		dur := ep.Duration
		if math.IsNaN(dur) || math.IsInf(dur, 0) || dur < 0 {
			dur = 0
		}
		episodes = append(episodes, Episode{
			ID:       epID,
			Title:    strings.TrimSpace(ep.Title),
			Duration: dur,
			Image:    cleanURL(ep.Image, opts),
			Badges:   cleanBadges(ep.Badges),
			Aspect:   cleanAspect(ep.Aspect, ""),
			Lock:     cleanLock(ep.Lock),
		})
		if len(episodes) >= MaxEpisodes {
			break
		}
	}
	if len(episodes) == 0 {
		return nil, NewError(ErrPlugin, "媒体详情没有有效剧集", nil)
	}
	var seasons []Season
	for _, s := range m.Seasons {
		sid, ok := validID(s.ID)
		if !ok {
			continue
		}
		st := strings.TrimSpace(s.Title)
		if st == "" {
			continue
		}
		avail := s.IsAvailable()
		seasons = append(seasons, Season{
			ID:        sid,
			Title:     st,
			Current:   s.Current,
			Available: Bool(avail),
			Note:      strings.TrimSpace(s.Note),
		})
	}
	return &Media{
		ID:          id,
		Title:       title,
		Image:       cleanURL(m.Image, opts),
		Backdrop:    cleanURL(m.Backdrop, opts),
		Description: strings.TrimSpace(m.Description),
		Badges:      cleanBadges(m.Badges),
		Meta:        cleanMeta(m.Meta),
		Tags:        cleanTags(m.Tags),
		People:      cleanCards(m.People, MaxPageItems, opts),
		Aspect:      cleanAspect(m.Aspect, AspectLandscape),
		Seasons:     seasons,
		Episodes:    episodes,
		Sections:    cleanSections(m.Sections, opts),
	}, nil
}

// CleanStreamHeaders removes forbidden or malformed HTTP headers from a Stream.
func CleanStreamHeaders(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	var out map[string]string
	for k, v := range in {
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if k == "" || v == "" || strings.ContainsAny(k, "\r\n") || strings.ContainsAny(v, "\r\n") {
			continue
		}
		if forbiddenStreamHeaders[strings.ToLower(k)] {
			continue
		}
		if out == nil {
			out = make(map[string]string, len(in))
		}
		out[k] = v
	}
	return out
}

func cleanEncryption(e *Encryption) (*Encryption, bool) {
	if e == nil {
		return nil, true
	}
	if e.Scheme != EncryptionCENC {
		return nil, false
	}
	key := strings.ToLower(strings.TrimSpace(e.Key))
	if len(key) != 32 {
		return nil, false
	}
	if _, err := hex.DecodeString(key); err != nil {
		return nil, false
	}
	return &Encryption{Scheme: EncryptionCENC, Key: key}, true
}

// CleanPlayback validates and sanitizes a Playback response. At least one valid
// stream is required.
func CleanPlayback(pb *Playback, opts CleanOptions) (*Playback, error) {
	if pb == nil {
		return nil, NewError(ErrPlugin, "播放响应为空", nil)
	}
	var streams []Stream
	for _, s := range pb.Streams {
		if s.Container != ContainerMP4 && s.Container != ContainerHLS {
			continue
		}
		u := cleanURL(s.URL, opts)
		if u == "" {
			continue
		}
		enc, ok := cleanEncryption(s.Encryption)
		if !ok {
			continue
		}
		var backups []string
		for _, bu := range s.BackupURLs {
			if cu := cleanURL(bu, opts); cu != "" {
				backups = append(backups, cu)
			}
		}
		streams = append(streams, Stream{
			URL:        u,
			BackupURLs: backups,
			Container:  s.Container,
			Headers:    CleanStreamHeaders(s.Headers),
			Codec:      strings.TrimSpace(s.Codec),
			Width:      max(s.Width, 0),
			Height:     max(s.Height, 0),
			Bitrate:    max(s.Bitrate, 0),
			Encryption: enc,
		})
	}
	if len(streams) == 0 {
		return nil, NewError(ErrPlugin, "没有可用的视频流", nil)
	}
	exp := pb.ExpiresAt
	if exp < 0 {
		exp = 0
	}
	return &Playback{Streams: streams, ExpiresAt: exp}, nil
}

// CleanSuggest trims, deduplicates, and caps search suggestions at MaxSuggestions.
func CleanSuggest(in []string) []string {
	var out []string
	seen := make(map[string]bool, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
		if len(out) >= MaxSuggestions {
			break
		}
	}
	return out
}

// CleanDanmakuPage drops invalid danmaku entries and sorts the rest by Time.
func CleanDanmakuPage(p *DanmakuPage) (*DanmakuPage, error) {
	if p == nil {
		return nil, NewError(ErrPlugin, "弹幕响应为空", nil)
	}
	items := make([]Danmaku, 0, len(p.Items))
	for _, d := range p.Items {
		text := strings.TrimSpace(d.Text)
		if text == "" || math.IsNaN(d.Time) || math.IsInf(d.Time, 0) || d.Time < 0 {
			continue
		}
		items = append(items, Danmaku{
			ID:   strings.TrimSpace(d.ID),
			Text: text,
			Time: d.Time,
		})
	}
	slices.SortStableFunc(items, func(a, b Danmaku) int {
		switch {
		case a.Time < b.Time:
			return -1
		case a.Time > b.Time:
			return 1
		default:
			return 0
		}
	})
	return &DanmakuPage{
		Items:  items,
		Cursor: cleanCursor(p.Cursor),
	}, nil
}

// CleanRefresh validates refreshed media cards, capping at MaxRefreshIDs.
func CleanRefresh(in []Card, opts CleanOptions) []Card {
	return cleanCards(in, MaxRefreshIDs, opts)
}

// decodeObject validates size (<= MaxResponseBytes), checks that raw is a JSON
// object, ensures every required key is present (and non-null), and unmarshals
// into dst.
func decodeObject(raw []byte, dst any, required ...string) error {
	if len(raw) > MaxResponseBytes {
		return NewError(ErrPlugin, "片源返回数据过大", fmt.Errorf("response size %d exceeds %d", len(raw), MaxResponseBytes))
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return NewError(ErrPlugin, "片源返回格式错误", fmt.Errorf("expected JSON object"))
	}
	if len(required) > 0 {
		var top map[string]json.RawMessage
		if err := json.Unmarshal(trimmed, &top); err != nil {
			return NewError(ErrPlugin, "片源返回格式错误", err)
		}
		for _, k := range required {
			v, ok := top[k]
			if !ok || bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
				return NewError(ErrPlugin, "片源返回缺少必需字段", fmt.Errorf("missing required field %q", k))
			}
		}
	}
	if err := json.Unmarshal(trimmed, dst); err != nil {
		return NewError(ErrPlugin, "片源返回格式错误", err)
	}
	return nil
}

// DecodeNavigation decodes and cleans a navigation() JSON response ({items: [...]}).
func DecodeNavigation(raw []byte) ([]NavItem, error) {
	var resp struct {
		Items []NavItem `json:"items"`
	}
	if err := decodeObject(raw, &resp, "items"); err != nil {
		return nil, err
	}
	return CleanNavigation(resp.Items)
}

// DecodeCollectionPage decodes and cleans a collection/search/searchHome JSON response.
func DecodeCollectionPage(raw []byte, firstPage bool, opts CleanOptions) (*CollectionPage, error) {
	var page CollectionPage
	if err := decodeObject(raw, &page); err != nil {
		return nil, err
	}
	return CleanCollectionPage(&page, firstPage, opts)
}

// DecodeMedia decodes and cleans a media() JSON response.
func DecodeMedia(raw []byte, opts CleanOptions) (*Media, error) {
	var m Media
	if err := decodeObject(raw, &m, "id", "title", "episodes"); err != nil {
		return nil, err
	}
	return CleanMedia(&m, opts)
}

// DecodePlayback decodes and cleans a play() JSON response.
func DecodePlayback(raw []byte, opts CleanOptions) (*Playback, error) {
	var pb Playback
	if err := decodeObject(raw, &pb, "streams"); err != nil {
		return nil, err
	}
	return CleanPlayback(&pb, opts)
}

// DecodeSuggest decodes and cleans a suggest() JSON response ({items: [...]}).
func DecodeSuggest(raw []byte) ([]string, error) {
	var resp struct {
		Items []string `json:"items"`
	}
	if err := decodeObject(raw, &resp, "items"); err != nil {
		return nil, err
	}
	return CleanSuggest(resp.Items), nil
}

// DecodeDanmakuPage decodes and cleans a danmaku() JSON response.
func DecodeDanmakuPage(raw []byte) (*DanmakuPage, error) {
	var page DanmakuPage
	if err := decodeObject(raw, &page, "items"); err != nil {
		return nil, err
	}
	return CleanDanmakuPage(&page)
}

// DecodeRefresh decodes and cleans a refresh() JSON response ({items: [...]}).
func DecodeRefresh(raw []byte, opts CleanOptions) ([]Card, error) {
	var resp struct {
		Items []Card `json:"items"`
	}
	if err := decodeObject(raw, &resp, "items"); err != nil {
		return nil, err
	}
	return CleanRefresh(resp.Items, opts), nil
}
