package source

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"slices"
	"strconv"
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
	s = Shape(strings.TrimSpace(string(s)))
	switch s {
	case ShapePortrait, ShapeLandscape, ShapeSquare, ShapeCircle:
		return s
	default:
		return ShapePortrait
	}
}

func cleanLayout(l Layout) Layout {
	l = Layout(strings.TrimSpace(string(l)))
	switch l {
	case LayoutGrid, LayoutList, LayoutRanked:
		return l
	default:
		return LayoutGrid
	}
}

func cleanSectionLayout(l SectionLayout) SectionLayout {
	l = SectionLayout(strings.TrimSpace(string(l)))
	switch l {
	case SectionRow, SectionGrid, SectionRanked:
		return l
	default:
		return SectionRow
	}
}

func cleanIcon(ic Icon) Icon {
	ic = Icon(strings.TrimSpace(string(ic)))
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
	a = Aspect(strings.TrimSpace(string(a)))
	switch a {
	case AspectPortrait, AspectLandscape:
		return a
	default:
		return fallback
	}
}

func cleanLock(l Lock) Lock {
	l = Lock(strings.TrimSpace(string(l)))
	switch l {
	case LockNone, LockLogin, LockVIP, LockPaid, LockUnavailable:
		return l
	default:
		// A lock this host does not know is still a lock: play nothing
		// rather than an episode the source says is locked.
		return LockUnavailable
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

func cleanCards(in []Card, limit int, mediaOnly bool, opts CleanOptions) []Card {
	var out []Card
	seen := make(map[string]bool, len(in))
	for _, c := range in {
		if mediaOnly && c.Kind != CardMedia {
			continue
		}
		cleaned, ok := CleanCard(c, opts)
		if !ok || seen[cleaned.ID] {
			continue
		}
		seen[cleaned.ID] = true
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
	seenKeys := make(map[string]bool, len(in))
	for _, row := range in {
		key := strings.TrimSpace(row.Key)
		title := strings.TrimSpace(row.Title)
		if key == "" || title == "" || seenKeys[key] {
			continue
		}
		var opts []FilterOption
		seenVals := make(map[string]bool, len(row.Options))
		for _, o := range row.Options {
			ot := strings.TrimSpace(o.Title)
			if ot == "" {
				continue
			}
			ov := strings.TrimSpace(o.Value)
			if seenVals[ov] {
				continue
			}
			seenVals[ov] = true
			opts = append(opts, FilterOption{Value: ov, Title: ot})
		}
		if len(opts) == 0 {
			continue
		}
		seenKeys[key] = true
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
		items := cleanCards(s.Items, MaxPageItems, false, opts)
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
// item is required; duplicates by ID are dropped keeping the first, and items
// beyond MaxNavItems are dropped.
func CleanNavigation(in []NavItem) ([]NavItem, error) {
	var out []NavItem
	seen := make(map[string]bool, len(in))
	for _, it := range in {
		id, ok := validID(it.ID)
		if !ok || seen[id] {
			continue
		}
		title := truncateRunes(strings.TrimSpace(it.Title), MaxNavTitleRunes)
		if title == "" {
			continue
		}
		seen[id] = true
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
		Header: cleanHeader(p.Header, opts),
		Layout: cleanLayout(p.Layout),
		Items:  cleanCards(p.Items, MaxPageItems, false, opts),
		Cursor: cleanCursor(p.Cursor),
	}
	if firstPage {
		out.Filters = cleanFilters(p.Filters)
		out.Sections = cleanSections(p.Sections, opts)
	}
	return out, nil
}

// CleanMedia validates and sanitizes a Media detail response. Missing id, title,
// or valid episodes fails with ErrPlugin. Duplicate episode and season IDs keep
// only the first entry.
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
	seenEp := make(map[string]bool, len(m.Episodes))
	for _, ep := range m.Episodes {
		epID, ok := validID(ep.ID)
		if !ok || seenEp[epID] {
			continue
		}
		seenEp[epID] = true
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
	seenSeason := make(map[string]bool, len(m.Seasons))
	for _, s := range m.Seasons {
		sid, ok := validID(s.ID)
		if !ok || seenSeason[sid] {
			continue
		}
		st := strings.TrimSpace(s.Title)
		if st == "" {
			continue
		}
		seenSeason[sid] = true
		avail := s.IsAvailable()
		seasons = append(seasons, Season{
			ID:        sid,
			Title:     st,
			Name:      strings.TrimSpace(s.Name),
			Image:     cleanURL(s.Image, opts),
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
		People:      cleanCards(m.People, MaxPageItems, false, opts),
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
	if EncryptionScheme(strings.TrimSpace(string(e.Scheme))) != EncryptionCENC {
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

// CleanDanmakuPage drops invalid danmaku entries, deduplicates non-empty IDs
// keeping the first, and sorts the rest by Time.
func CleanDanmakuPage(p *DanmakuPage) (*DanmakuPage, error) {
	if p == nil {
		return nil, NewError(ErrPlugin, "弹幕响应为空", nil)
	}
	items := make([]Danmaku, 0, len(p.Items))
	seen := make(map[string]bool, len(p.Items))
	for _, d := range p.Items {
		text := strings.TrimSpace(d.Text)
		if text == "" || math.IsNaN(d.Time) || math.IsInf(d.Time, 0) || d.Time < 0 {
			continue
		}
		id := strings.TrimSpace(d.ID)
		if id != "" {
			if seen[id] {
				continue
			}
			seen[id] = true
		}
		items = append(items, Danmaku{
			ID:   id,
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

// CleanRefresh validates refreshed media cards, dropping non-media cards,
// deduplicating by ID, and capping at MaxRefreshIDs.
func CleanRefresh(in []Card, opts CleanOptions) []Card {
	return cleanCards(in, MaxRefreshIDs, true, opts)
}

// ---- Tolerant JSON decoding (§6) ----

func parseJSONObject(raw []byte) (map[string]json.RawMessage, error) {
	if len(raw) > MaxResponseBytes {
		return nil, NewError(ErrPlugin, "片源返回数据过大", fmt.Errorf("response size %d exceeds %d", len(raw), MaxResponseBytes))
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, NewError(ErrPlugin, "片源返回格式错误", fmt.Errorf("expected JSON object"))
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &obj); err != nil {
		return nil, NewError(ErrPlugin, "片源返回格式错误", err)
	}
	return obj, nil
}

func rawIsNull(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null"))
}

// rawString decodes a JSON string or converts a JSON number token to its
// decimal string representation per §6 ("id 等字符串字段给了数字时转成字符串").
func rawString(raw json.RawMessage) (string, bool) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return "", false
	}
	switch trimmed[0] {
	case '"':
		var s string
		if err := json.Unmarshal(trimmed, &s); err != nil {
			return "", false
		}
		return s, true
	case '-', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		var n json.Number
		dec := json.NewDecoder(bytes.NewReader(trimmed))
		dec.UseNumber()
		if err := dec.Decode(&n); err != nil || dec.More() {
			return "", false
		}
		f, err := n.Float64()
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			return "", false
		}
		return string(trimmed), true
	default:
		return "", false
	}
}

func rawOptString(obj map[string]json.RawMessage, key string) string {
	s, _ := rawString(obj[key])
	return s
}

func rawFloat(raw json.RawMessage) (float64, bool) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return 0, false
	}
	if trimmed[0] != '-' && (trimmed[0] < '0' || trimmed[0] > '9') {
		return 0, false
	}
	var f float64
	if err := json.Unmarshal(trimmed, &f); err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, false
	}
	return f, true
}

func rawNonNegFloat(raw json.RawMessage) (float64, bool) {
	f, ok := rawFloat(raw)
	if !ok || f < 0 {
		return 0, false
	}
	return f, true
}

func rawNonNegInt(raw json.RawMessage) (int, bool) {
	f, ok := rawNonNegFloat(raw)
	if !ok {
		return 0, false
	}
	fl := math.Floor(f)
	if fl >= math.Ldexp(1, strconv.IntSize-1) {
		return 0, false
	}
	return int(fl), true
}

func rawNonNegInt64(raw json.RawMessage) (int64, bool) {
	f, ok := rawNonNegFloat(raw)
	if !ok {
		return 0, false
	}
	fl := math.Floor(f)
	if fl >= math.Ldexp(1, 63) {
		return 0, false
	}
	return int64(fl), true
}

func rawBool(raw json.RawMessage) (bool, bool) {
	trimmed := bytes.TrimSpace(raw)
	if bytes.Equal(trimmed, []byte("true")) {
		return true, true
	}
	if bytes.Equal(trimmed, []byte("false")) {
		return false, true
	}
	return false, false
}

func rawArray(raw json.RawMessage) ([]json.RawMessage, bool) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '[' {
		return nil, false
	}
	var arr []json.RawMessage
	if err := json.Unmarshal(trimmed, &arr); err != nil {
		return nil, false
	}
	return arr, true
}

func rawObject(raw json.RawMessage) (map[string]json.RawMessage, bool) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, false
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &obj); err != nil {
		return nil, false
	}
	return obj, true
}

func rawStringSlice(raw json.RawMessage) []string {
	arr, ok := rawArray(raw)
	if !ok {
		return nil
	}
	var out []string
	for _, el := range arr {
		if s, ok := rawString(el); ok {
			out = append(out, s)
		}
	}
	return out
}

func rawStringMap(raw json.RawMessage) map[string]string {
	obj, ok := rawObject(raw)
	if !ok {
		return nil
	}
	var out map[string]string
	for k, v := range obj {
		if s, ok := rawString(v); ok {
			if out == nil {
				out = make(map[string]string, len(obj))
			}
			out[k] = s
		}
	}
	return out
}

func decodeMetaList(raw json.RawMessage) []Meta {
	arr, ok := rawArray(raw)
	if !ok {
		return nil
	}
	var out []Meta
	for _, el := range arr {
		obj, ok := rawObject(el)
		if !ok {
			continue
		}
		val, ok := rawString(obj["value"])
		if !ok {
			continue
		}
		out = append(out, Meta{
			Label: rawOptString(obj, "label"),
			Value: val,
		})
	}
	return out
}

func decodeCards(raw json.RawMessage) []Card {
	arr, ok := rawArray(raw)
	if !ok {
		return nil
	}
	var out []Card
	for _, el := range arr {
		obj, ok := rawObject(el)
		if !ok {
			continue
		}
		kind, ok := rawString(obj["kind"])
		if !ok {
			continue
		}
		id, ok := rawString(obj["id"])
		if !ok {
			continue
		}
		title, ok := rawString(obj["title"])
		if !ok {
			continue
		}
		out = append(out, Card{
			Kind:     CardKind(kind),
			ID:       id,
			Title:    title,
			Subtitle: rawOptString(obj, "subtitle"),
			Image:    rawOptString(obj, "image"),
			Shape:    Shape(rawOptString(obj, "shape")),
			Badges:   rawStringSlice(obj["badges"]),
			Note:     rawOptString(obj, "note"),
		})
	}
	return out
}

func decodeHeader(raw json.RawMessage) *Header {
	obj, ok := rawObject(raw)
	if !ok {
		return nil
	}
	title, ok := rawString(obj["title"])
	if !ok {
		return nil
	}
	return &Header{
		Title:       title,
		Image:       rawOptString(obj, "image"),
		Shape:       Shape(rawOptString(obj, "shape")),
		Description: rawOptString(obj, "description"),
		Meta:        decodeMetaList(obj["meta"]),
	}
}

func decodeFilters(raw json.RawMessage) []FilterRow {
	arr, ok := rawArray(raw)
	if !ok {
		return nil
	}
	var out []FilterRow
	for _, el := range arr {
		obj, ok := rawObject(el)
		if !ok {
			continue
		}
		key, ok := rawString(obj["key"])
		if !ok {
			continue
		}
		title, ok := rawString(obj["title"])
		if !ok {
			continue
		}
		optArr, ok := rawArray(obj["options"])
		if !ok {
			continue
		}
		var opts []FilterOption
		for _, oel := range optArr {
			oobj, ok := rawObject(oel)
			if !ok {
				continue
			}
			ot, ok := rawString(oobj["title"])
			if !ok {
				continue
			}
			var ov string
			if rawVal, exists := oobj["value"]; exists && !rawIsNull(rawVal) {
				s, ok := rawString(rawVal)
				if !ok {
					continue
				}
				ov = s
			}
			opts = append(opts, FilterOption{Value: ov, Title: ot})
		}
		out = append(out, FilterRow{
			Key:     key,
			Title:   title,
			Options: opts,
			Value:   rawOptString(obj, "value"),
		})
	}
	return out
}

func decodeSections(raw json.RawMessage) []Section {
	arr, ok := rawArray(raw)
	if !ok {
		return nil
	}
	var out []Section
	for _, el := range arr {
		obj, ok := rawObject(el)
		if !ok {
			continue
		}
		title, ok := rawString(obj["title"])
		if !ok {
			continue
		}
		out = append(out, Section{
			Title:      title,
			Layout:     SectionLayout(rawOptString(obj, "layout")),
			Items:      decodeCards(obj["items"]),
			Collection: rawOptString(obj, "collection"),
			More:       rawOptString(obj, "more"),
		})
	}
	return out
}

// DecodeNavigation decodes and cleans a navigation() JSON response ({items: [...]}).
func DecodeNavigation(raw []byte) ([]NavItem, error) {
	top, err := parseJSONObject(raw)
	if err != nil {
		return nil, err
	}
	arr, ok := rawArray(top["items"])
	if !ok {
		return nil, NewError(ErrPlugin, "片源返回缺少必需字段", fmt.Errorf("missing or invalid required field %q", "items"))
	}
	items := make([]NavItem, 0, len(arr))
	for _, el := range arr {
		obj, ok := rawObject(el)
		if !ok {
			continue
		}
		id, ok := rawString(obj["id"])
		if !ok {
			continue
		}
		title, ok := rawString(obj["title"])
		if !ok {
			continue
		}
		items = append(items, NavItem{
			ID:    id,
			Title: title,
			Icon:  Icon(rawOptString(obj, "icon")),
		})
	}
	return CleanNavigation(items)
}

// DecodeCollectionPage decodes and cleans a collection/search/searchHome JSON response.
func DecodeCollectionPage(raw []byte, firstPage bool, opts CleanOptions) (*CollectionPage, error) {
	top, err := parseJSONObject(raw)
	if err != nil {
		return nil, err
	}
	page := CollectionPage{
		Title:  rawOptString(top, "title"),
		Header: decodeHeader(top["header"]),
		Layout: Layout(rawOptString(top, "layout")),
		Items:  decodeCards(top["items"]),
		Cursor: rawOptString(top, "cursor"),
	}
	if firstPage {
		page.Filters = decodeFilters(top["filters"])
		page.Sections = decodeSections(top["sections"])
	}
	return CleanCollectionPage(&page, firstPage, opts)
}

// DecodeMedia decodes and cleans a media() JSON response.
func DecodeMedia(raw []byte, opts CleanOptions) (*Media, error) {
	top, err := parseJSONObject(raw)
	if err != nil {
		return nil, err
	}
	id, ok := rawString(top["id"])
	if !ok {
		return nil, NewError(ErrPlugin, "片源返回缺少必需字段", fmt.Errorf("missing or invalid required field %q", "id"))
	}
	title, ok := rawString(top["title"])
	if !ok {
		return nil, NewError(ErrPlugin, "片源返回缺少必需字段", fmt.Errorf("missing or invalid required field %q", "title"))
	}
	epArr, ok := rawArray(top["episodes"])
	if !ok {
		return nil, NewError(ErrPlugin, "片源返回缺少必需字段", fmt.Errorf("missing or invalid required field %q", "episodes"))
	}

	episodes := make([]Episode, 0, len(epArr))
	for _, el := range epArr {
		obj, ok := rawObject(el)
		if !ok {
			continue
		}
		epID, ok := rawString(obj["id"])
		if !ok {
			continue
		}
		dur, _ := rawNonNegFloat(obj["duration"])
		episodes = append(episodes, Episode{
			ID:       epID,
			Title:    rawOptString(obj, "title"),
			Duration: dur,
			Image:    rawOptString(obj, "image"),
			Badges:   rawStringSlice(obj["badges"]),
			Aspect:   Aspect(rawOptString(obj, "aspect")),
			Lock:     Lock(rawOptString(obj, "lock")),
		})
	}

	var seasons []Season
	if seasonArr, ok := rawArray(top["seasons"]); ok {
		seasons = make([]Season, 0, len(seasonArr))
		for _, el := range seasonArr {
			obj, ok := rawObject(el)
			if !ok {
				continue
			}
			sid, ok := rawString(obj["id"])
			if !ok {
				continue
			}
			st, ok := rawString(obj["title"])
			if !ok {
				continue
			}
			cur, _ := rawBool(obj["current"])
			var avail *bool
			if b, ok := rawBool(obj["available"]); ok {
				avail = Bool(b)
			}
			seasons = append(seasons, Season{
				ID:        sid,
				Title:     st,
				Name:      rawOptString(obj, "name"),
				Image:     rawOptString(obj, "image"),
				Current:   cur,
				Available: avail,
				Note:      rawOptString(obj, "note"),
			})
		}
	}

	m := Media{
		ID:          id,
		Title:       title,
		Image:       rawOptString(top, "image"),
		Backdrop:    rawOptString(top, "backdrop"),
		Description: rawOptString(top, "description"),
		Badges:      rawStringSlice(top["badges"]),
		Meta:        decodeMetaList(top["meta"]),
		Tags:        rawStringSlice(top["tags"]),
		People:      decodeCards(top["people"]),
		Aspect:      Aspect(rawOptString(top, "aspect")),
		Seasons:     seasons,
		Episodes:    episodes,
		Sections:    decodeSections(top["sections"]),
	}
	return CleanMedia(&m, opts)
}

// DecodePlayback decodes and cleans a play() JSON response.
func DecodePlayback(raw []byte, opts CleanOptions) (*Playback, error) {
	top, err := parseJSONObject(raw)
	if err != nil {
		return nil, err
	}
	arr, ok := rawArray(top["streams"])
	if !ok {
		return nil, NewError(ErrPlugin, "片源返回缺少必需字段", fmt.Errorf("missing or invalid required field %q", "streams"))
	}
	streams := make([]Stream, 0, len(arr))
	for _, el := range arr {
		obj, ok := rawObject(el)
		if !ok {
			continue
		}
		u, ok := rawString(obj["url"])
		if !ok {
			continue
		}
		container, ok := rawString(obj["container"])
		if !ok {
			continue
		}
		w, _ := rawNonNegInt(obj["width"])
		h, _ := rawNonNegInt(obj["height"])
		br, _ := rawNonNegInt64(obj["bitrate"])
		var enc *Encryption
		if encRaw, exists := obj["encryption"]; exists && !rawIsNull(encRaw) {
			if eobj, ok := rawObject(encRaw); ok {
				enc = &Encryption{
					Scheme: EncryptionScheme(rawOptString(eobj, "scheme")),
					Key:    rawOptString(eobj, "key"),
				}
			} else {
				// Non-null malformed encryption invalidates the stream.
				enc = &Encryption{}
			}
		}
		streams = append(streams, Stream{
			URL:        u,
			BackupURLs: rawStringSlice(obj["backupUrls"]),
			Container:  Container(container),
			Headers:    rawStringMap(obj["headers"]),
			Codec:      rawOptString(obj, "codec"),
			Width:      w,
			Height:     h,
			Bitrate:    br,
			Encryption: enc,
		})
	}
	exp, _ := rawNonNegInt64(top["expiresAt"])
	return CleanPlayback(&Playback{Streams: streams, ExpiresAt: exp}, opts)
}

// DecodeSuggest decodes and cleans a suggest() JSON response ({items: [...]}).
func DecodeSuggest(raw []byte) ([]string, error) {
	top, err := parseJSONObject(raw)
	if err != nil {
		return nil, err
	}
	arr, ok := rawArray(top["items"])
	if !ok {
		return nil, NewError(ErrPlugin, "片源返回缺少必需字段", fmt.Errorf("missing or invalid required field %q", "items"))
	}
	items := make([]string, 0, len(arr))
	for _, el := range arr {
		if s, ok := rawString(el); ok {
			items = append(items, s)
		}
	}
	return CleanSuggest(items), nil
}

// DecodeDanmakuPage decodes and cleans a danmaku() JSON response.
func DecodeDanmakuPage(raw []byte) (*DanmakuPage, error) {
	top, err := parseJSONObject(raw)
	if err != nil {
		return nil, err
	}
	arr, ok := rawArray(top["items"])
	if !ok {
		return nil, NewError(ErrPlugin, "片源返回缺少必需字段", fmt.Errorf("missing or invalid required field %q", "items"))
	}
	items := make([]Danmaku, 0, len(arr))
	for _, el := range arr {
		obj, ok := rawObject(el)
		if !ok {
			continue
		}
		text, ok := rawString(obj["text"])
		if !ok {
			continue
		}
		t, ok := rawNonNegFloat(obj["time"])
		if !ok {
			continue
		}
		items = append(items, Danmaku{
			ID:   rawOptString(obj, "id"),
			Text: text,
			Time: t,
		})
	}
	return CleanDanmakuPage(&DanmakuPage{
		Items:  items,
		Cursor: rawOptString(top, "cursor"),
	})
}

// DecodeRefresh decodes and cleans a refresh() JSON response ({items: [...]}).
func DecodeRefresh(raw []byte, opts CleanOptions) ([]Card, error) {
	top, err := parseJSONObject(raw)
	if err != nil {
		return nil, err
	}
	if _, ok := rawArray(top["items"]); !ok {
		return nil, NewError(ErrPlugin, "片源返回缺少必需字段", fmt.Errorf("missing or invalid required field %q", "items"))
	}
	return CleanRefresh(decodeCards(top["items"]), opts), nil
}
