// Package hongguo is a temporary built-in source.Source backed by openhongguo.
// It uses Hongguo's original drama and episode IDs so records match the future
// JS plugin.
package hongguo

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/beilunyang/openhongguo/adapters"
	"github.com/beilunyang/openhongguo/sdk"
	"github.com/beilunyang/openhongguo/sdk/model"

	"github.com/giuem/ichiwa/internal/source"
)

// SourceID is the protocol ID of the Hongguo source.
const SourceID = "hongguo"

// Collection IDs exposed by this source.
const (
	ColRecommend = "channel:推荐"
	ColLive      = "channel:真人剧"
	ColComic     = "channel:漫剧"
	ColRank      = "rank"
	ColFilter    = "filter"
)

// Source implements source.Source using openhongguo.
type Source struct {
	client *sdk.Client
	logs   *os.File // the SDK's log, nil when it did not open

	calendarBudget time.Duration // max wait for calendar notes in Media; defaults to defaultCalendarBudget
	profileWait    time.Duration // max wait for an actor's profile after their works; defaults to defaultProfileWait

	mu          sync.Mutex
	days        map[string]calDay // the 上新 calendar's days read lately
	calInflight map[string]*calFlight
	panels      map[string]panelCache
	seasons     map[string]seasonsCache // the season lists read lately, by media ID
}

type panelCache struct {
	rows []model.FilterRow
	at   time.Time
}

var _ source.Source = (*Source)(nil)

// New creates a Hongguo source storing its anonymous device state and SDK log
// in dataDir.
func New(dataDir string) (*Source, error) {
	opts := sdk.Options{RetryAttempts: 3, DeviceResets: sdk.Int(3)}
	logs := openLog(filepath.Join(dataDir, "sdk.log"))
	if logs != nil {
		opts.Logger = log.New(logs, "", log.LstdFlags)
	}
	client, err := adapters.NewClient(filepath.Join(dataDir, "anonymous.json"), opts)
	if err != nil {
		if logs != nil {
			logs.Close()
		}
		return nil, err
	}
	return &Source{
		client: client,
		logs:   logs,
		days:   map[string]calDay{},
		panels: map[string]panelCache{},
	}, nil
}

// Close closes the SDK's log, which the source keeps open while the app
// runs; the source is not called after.
func (s *Source) Close() error {
	if s.logs == nil {
		return nil
	}
	return s.logs.Close()
}

func openLog(path string) *os.File {
	flags := os.O_CREATE | os.O_WRONLY | os.O_APPEND
	if fi, err := os.Stat(path); err == nil && fi.Size() > 1<<20 {
		flags |= os.O_TRUNC
	}
	f, err := os.OpenFile(path, flags, 0o644)
	if err != nil {
		return nil
	}
	return f
}

func withTimeout(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, d)
}

// again starts the cursor of a list that ended early. Hongguo's lists say
// they ended now and then while asking again goes on, so a list never
// ends: its last page asks for itself again, and the host, which stops
// after a few pages that bring nothing new, decides the end.
const again = "again:"

// nextCursor is the cursor after a page asked for at cursor, which had
// more after it at next.
func nextCursor(hasMore bool, next, cursor string) string {
	if hasMore && next != "" {
		return next
	}
	return again + resume(cursor)
}

// resume is the cursor to ask Hongguo for, again's taken off.
func resume(cursor string) string { return strings.TrimPrefix(cursor, again) }

// The headers of the pages of the navigation, which say what each is.
var navHeaders = map[string]*source.Header{
	ColRecommend: {Title: "推荐", Description: "为你精选的热门短剧"},
	ColLive:      {Title: "真人剧", Description: "真人出演，好戏连台"},
	ColComic:     {Title: "漫剧", Description: "动态漫剧，脑洞大开"},
	ColRank:      {Title: "排行榜", Description: "看看大家都在追什么"},
	ColFilter:    {Title: "筛选", Description: "按题材、设定找到想看的剧"},
}

// header is the header of a page of the navigation, on its first page.
func header(id, cursor string) *source.Header {
	if resume(cursor) != "" {
		return nil
	}
	return navHeaders[id]
}

// Header is the header of the navigation page id, nil for none: for the
// pages the app kept before Hongguo said what they are.
func Header(id string) *source.Header { return navHeaders[id] }

func (s *Source) Info() source.Info {
	return source.Info{
		ID:      SourceID,
		Name:    "红果短剧",
		Version: "0.1.0",
		Capabilities: []source.Capability{
			source.CapSearch,
			source.CapSearchHome,
			source.CapDanmaku,
			source.CapRefresh,
		},
	}
}

func (s *Source) Navigation(ctx context.Context) ([]source.NavItem, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return []source.NavItem{
		{ID: ColRecommend, Title: "推荐", Icon: source.IconHome},
		{ID: ColLive, Title: "真人剧", Icon: source.IconFilm},
		{ID: ColComic, Title: "漫剧", Icon: source.IconSparkles},
		{ID: ColRank, Title: "排行榜", Icon: source.IconTrophy},
		{ID: ColFilter, Title: "筛选", Icon: source.IconFilter},
	}, nil
}

func (s *Source) Collection(ctx context.Context, r source.CollectionRequest) (*source.CollectionPage, error) {
	switch {
	case strings.HasPrefix(r.ID, "channel:"):
		return s.discover(ctx, strings.TrimPrefix(r.ID, "channel:"), r.Cursor)
	case r.ID == ColRank:
		return s.rank(ctx, r.Filters, r.Cursor)
	case r.ID == ColFilter:
		return s.filter(ctx, r.Filters, r.Cursor)
	case strings.HasPrefix(r.ID, "person:") && resume(r.Cursor) != "":
		return s.personWorks(ctx, strings.TrimPrefix(r.ID, "person:"), r.Cursor)
	case strings.HasPrefix(r.ID, "person:"):
		return s.person(ctx, strings.TrimPrefix(r.ID, "person:"))
	case strings.HasPrefix(r.ID, "related:"):
		return s.related(ctx, strings.TrimPrefix(r.ID, "related:"))
	default:
		return nil, source.NewError(source.ErrNotFound, "集合不存在", nil)
	}
}

func (s *Source) discover(ctx context.Context, channel, cursor string) (*source.CollectionPage, error) {
	ctx, cancel := withTimeout(ctx, 30*time.Second)
	defer cancel()
	page, err := s.client.Discover(ctx, sdk.DiscoverOptions{Channel: channel, Cursor: resume(cursor)})
	if err != nil {
		return nil, wrapErr(err)
	}
	items := make([]source.Card, 0, len(page.Items))
	for _, it := range page.Items {
		items = append(items, cardOfSummary(it))
	}
	return &source.CollectionPage{
		Title:  channel,
		Header: header("channel:"+channel, cursor),
		Layout: source.LayoutGrid,
		Items:  items,
		Cursor: nextCursor(page.HasMore, page.Cursor, cursor),
	}, nil
}

func (s *Source) rank(ctx context.Context, filters map[string]string, cursor string) (*source.CollectionPage, error) {
	ctx, cancel := withTimeout(ctx, 30*time.Second)
	defer cancel()
	var query sdk.ShortRankQuery
	if resume(cursor) == "" {
		query.Content, query.Board = rankChoice(filters)
	} else {
		query.Cursor = resume(cursor)
	}
	r, err := s.client.ShortRank(ctx, query)
	if err != nil {
		return nil, wrapErr(err)
	}
	p := &source.CollectionPage{
		Title:  "排行榜",
		Header: header(ColRank, cursor),
		Layout: source.LayoutRanked,
		Items:  rankCards(r.Items),
		Cursor: nextCursor(r.HasMore, r.Cursor, cursor),
	}
	// The actors' rankings end when they say.
	if len(p.Items) > 0 && p.Items[0].Kind == source.CardCollection && strings.HasPrefix(p.Cursor, again) {
		p.Cursor = ""
	}
	if resume(cursor) == "" {
		p.Filters = rankFilters(r)
	}
	return p, nil
}

// rankChoice is the category and the board chosen. A board's value starts
// with its category's, so that the board chosen in another category,
// which the host sends along when the category changes, is left out.
func rankChoice(filters map[string]string) (content, board string) {
	content = strings.TrimSpace(filters["content"])
	cat, b, ok := strings.Cut(strings.TrimSpace(filters["board"]), ":")
	if !ok || content != "" && cat != content {
		return content, ""
	}
	if content == "" {
		content = cat
	}
	return content, b
}

func rankFilters(r *model.ShortRanking) []source.FilterRow {
	if len(r.Contents) == 0 {
		return nil
	}
	contentOpts := make([]source.FilterOption, 0, len(r.Contents))
	var selectedContent *model.RankContent
	for i := range r.Contents {
		ct := &r.Contents[i]
		contentOpts = append(contentOpts, source.FilterOption{Value: ct.ID, Title: ct.Name})
		if ct.ID == r.Content {
			selectedContent = ct
		}
	}
	if selectedContent == nil {
		selectedContent = &r.Contents[0]
	}
	rows := []source.FilterRow{{
		Key:     "content",
		Title:   "榜单类别",
		Options: contentOpts,
		Value:   r.Content,
	}}
	// The actor ranking may have no boards.
	if len(selectedContent.Boards) > 0 {
		boardOpts := make([]source.FilterOption, 0, len(selectedContent.Boards))
		for _, b := range selectedContent.Boards {
			boardOpts = append(boardOpts, source.FilterOption{Value: selectedContent.ID + ":" + b.ID, Title: b.Name})
		}
		board := ""
		if r.Board != "" {
			board = selectedContent.ID + ":" + r.Board
		}
		rows = append(rows, source.FilterRow{
			Key:     "board",
			Title:   "榜单",
			Options: boardOpts,
			Value:   board,
		})
	}
	return rows
}

func rankCards(items []model.RankItem) []source.Card {
	var out []source.Card
	for _, it := range items {
		switch {
		case it.Promoted:
		case it.SeriesID != "":
			note := it.Score
			if note == "" && it.Votes > 0 {
				note = heat(it.Votes)
			}
			out = append(out, source.Card{
				Kind:   source.CardMedia,
				ID:     it.SeriesID,
				Title:  it.Title,
				Image:  it.CoverURL,
				Shape:  source.ShapePortrait,
				Badges: episodesBadges(it.EpisodeCount),
				Note:   note,
			})
		case it.UserID != "":
			out = append(out, source.Card{
				Kind:     source.CardCollection,
				ID:       "person:" + it.UserID,
				Title:    it.Actor,
				Subtitle: strings.Join(it.Details, " · "),
				Image:    it.AvatarURL,
				Shape:    source.ShapeCircle,
				Note:     it.Score,
			})
		}
	}
	return out
}

func (s *Source) filterPanel(ctx context.Context, genre string) ([]model.FilterRow, error) {
	s.mu.Lock()
	cached, ok := s.panels[genre]
	s.mu.Unlock()
	if ok && time.Since(cached.at) < 10*time.Minute {
		return cached.rows, nil
	}
	rows, err := s.client.ShortFilterPanel(ctx, genre)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.panels[genre] = panelCache{rows: rows, at: time.Now()}
	s.mu.Unlock()
	return rows, nil
}

const genreKey = "genre"

func (s *Source) filter(ctx context.Context, chosen map[string]string, cursor string) (*source.CollectionPage, error) {
	ctx, cancel := withTimeout(ctx, 30*time.Second)
	defer cancel()

	var filterRows []source.FilterRow
	var query sdk.ShortFilterQuery
	first := resume(cursor) == ""
	if first {
		genre := strings.TrimSpace(chosen[genreKey])
		rows, err := s.filterPanel(ctx, genre)
		if err != nil {
			return nil, wrapErr(err)
		}
		var effective map[string]string
		effective, filterRows = buildFilterRows(rows, chosen)
		query.Selections = selections(effective)
	} else {
		// Pagination resumes the filter embedded in cursor; sending selections
		// alongside cursor causes openhongguo to reject mismatched filters.
		query.Cursor = resume(cursor)
	}

	page, err := s.client.ShortFilter(ctx, query)
	if errors.Is(err, sdk.ErrContentUnavailable) && first {
		return &source.CollectionPage{
			Title:   "筛选",
			Header:  header(ColFilter, cursor),
			Layout:  source.LayoutGrid,
			Filters: filterRows,
		}, nil
	}
	if err != nil {
		if first && len(filterRows) > 0 {
			return &source.CollectionPage{
				Title:   "筛选",
				Header:  header(ColFilter, cursor),
				Layout:  source.LayoutGrid,
				Filters: filterRows,
			}, wrapErr(err)
		}
		return nil, wrapErr(err)
	}
	items := make([]source.Card, 0, len(page.Items))
	for _, it := range page.Items {
		items = append(items, cardOfSummary(it))
	}
	return &source.CollectionPage{
		Title:   "筛选",
		Header:  header(ColFilter, cursor),
		Layout:  source.LayoutGrid,
		Filters: filterRows,
		Items:   items,
		Cursor:  nextCursor(page.HasMore, page.Cursor, cursor),
	}, nil
}

// buildFilterRows converts Hongguo's filter rows to protocol FilterRows. Non-
// genre option values are prefixed with "<genre>:" so changing genre drops
// selections from every other row statelessly.
func buildFilterRows(rows []model.FilterRow, chosen map[string]string) (map[string]string, []source.FilterRow) {
	effective := make(map[string]string, len(chosen))
	genre := ""
	for _, row := range rows {
		if row.Key != genreKey {
			continue
		}
		if val := strings.TrimSpace(chosen[genreKey]); val != "" {
			for _, o := range row.Options {
				if o.Value == val {
					genre = val
					break
				}
			}
		}
		break
	}
	prefix := genre + ":"
	out := make([]source.FilterRow, 0, len(rows))
	for _, row := range rows {
		opts := make([]source.FilterOption, 0, len(row.Options)+1)
		opts = append(opts, source.FilterOption{Value: "", Title: row.Name})
		if row.Key == genreKey {
			for _, o := range row.Options {
				opts = append(opts, source.FilterOption{Value: o.Value, Title: o.Name})
			}
			if genre != "" {
				effective[row.Key] = genre
			}
			out = append(out, source.FilterRow{
				Key:     row.Key,
				Title:   row.Name,
				Options: opts,
				Value:   genre,
			})
			continue
		}
		val := strings.TrimSpace(chosen[row.Key])
		rawVal, hasPrefix := strings.CutPrefix(val, prefix)
		selected := ""
		for _, o := range row.Options {
			encoded := prefix + o.Value
			opts = append(opts, source.FilterOption{Value: encoded, Title: o.Name})
			if hasPrefix && rawVal != "" && o.Value == rawVal {
				effective[row.Key] = rawVal
				selected = encoded
			}
		}
		out = append(out, source.FilterRow{
			Key:     row.Key,
			Title:   row.Name,
			Options: opts,
			Value:   selected,
		})
	}
	return effective, out
}

func selections(chosen map[string]string) map[string][]string {
	out := map[string][]string{}
	for k, v := range chosen {
		if v = strings.TrimSpace(v); v != "" {
			out[k] = []string{v}
		}
	}
	return out
}

// person is an actor's page: their profile as the header, their works as
// the items. The two are read at once. Without the works the page has
// nothing to show, so their failure fails the page at once and the host
// offers a retry; once they are in, the profile gets at most profileWait
// more, and a profile that fails or is late only leaves the header out, so
// the host shows the card it opened the page from.
func (s *Source) person(ctx context.Context, userID string) (*source.CollectionPage, error) {
	ctx, cancel := withTimeout(ctx, 30*time.Second)
	defer cancel()
	pctx, pcancel := context.WithCancel(ctx)
	defer pcancel()
	profile := make(chan *model.Person, 1)
	go func() {
		p, err := s.client.Person(pctx, userID)
		if err != nil {
			if pctx.Err() == nil {
				log.Printf("hongguo: profile of person %s: %v", userID, err)
			}
			p = nil
		}
		profile <- p
	}()
	page, err := s.personWorks(ctx, userID, "")
	if err != nil {
		return nil, err
	}
	page.Title = "" // the profile's name, or the card's the host shows
	wait := s.profileWait
	if wait <= 0 {
		wait = defaultProfileWait
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case p := <-profile:
		if p != nil {
			page.Header = personHeader(p)
			page.Title = page.Header.Title
		}
	case <-timer.C:
		log.Printf("hongguo: profile of person %s is late; showing the works without it", userID)
	case <-ctx.Done():
	}
	return page, nil
}

// defaultProfileWait is how long an actor's works wait for their profile.
const defaultProfileWait = time.Second

func (s *Source) personWorks(ctx context.Context, userID, cursor string) (*source.CollectionPage, error) {
	ctx, cancel := withTimeout(ctx, 30*time.Second)
	defer cancel()
	worksPage, err := s.client.PersonWorks(ctx, userID, resume(cursor))
	if err != nil {
		return nil, wrapErr(err)
	}
	out := &source.CollectionPage{
		Title:  "作品",
		Layout: source.LayoutGrid,
	}
	for _, w := range worksPage.Items {
		if w.SeriesID != "" {
			out.Items = append(out.Items, cardOfWork(w))
		}
	}
	out.Cursor = nextCursor(worksPage.HasMore, worksPage.Cursor, cursor)
	return out, nil
}

func personHeader(p *model.Person) *source.Header {
	var meta []source.Meta
	for _, t := range p.Tags {
		if t = strings.TrimSpace(t); t != "" {
			meta = append(meta, source.Meta{Value: t})
		}
	}
	for _, t := range p.Labels {
		if t = strings.TrimSpace(t); t != "" {
			meta = append(meta, source.Meta{Value: t})
		}
	}
	if p.Followers > 0 {
		meta = append(meta, source.Meta{Label: "粉丝", Value: heat(p.Followers)})
	}
	if p.Likes > 0 {
		meta = append(meta, source.Meta{Label: "获赞", Value: heat(p.Likes)})
	}
	return &source.Header{
		Title:       p.Name,
		Image:       p.AvatarURL,
		Shape:       source.ShapeCircle,
		Description: p.Description,
		Meta:        meta,
	}
}

func cardOfWork(w model.Work) source.Card {
	note := ""
	for _, t := range w.Tags {
		if !strings.HasSuffix(t, "集") {
			note = t
			break
		}
	}
	if note == "" && w.Plays > 0 {
		note = heat(w.Plays) + "播放"
	}
	return source.Card{
		Kind:     source.CardMedia,
		ID:       w.SeriesID,
		Title:    w.Title,
		Subtitle: w.Description,
		Image:    w.CoverURL,
		Shape:    source.ShapePortrait,
		Badges:   episodesBadges(w.EpisodeCount),
		Note:     note,
	}
}

func (s *Source) related(ctx context.Context, seriesID string) (*source.CollectionPage, error) {
	ctx, cancel := withTimeout(ctx, 30*time.Second)
	defer cancel()
	r, err := s.client.SeriesRecommend(ctx, seriesID)
	if err != nil {
		return nil, wrapErr(err)
	}
	items := make([]source.Card, 0, len(r.Items))
	for _, it := range r.Items {
		if it.ID != seriesID {
			items = append(items, cardOfSummary(it))
		}
	}
	s.fillCardCovers(ctx, items)
	return &source.CollectionPage{
		Title:  "相关推荐",
		Layout: source.LayoutList,
		Items:  items,
	}, nil
}

func (s *Source) Media(ctx context.Context, id string) (*source.Media, error) {
	ctx, cancel := withTimeout(ctx, 45*time.Second)
	defer cancel()

	var (
		series   *model.Series
		serErr   error
		episodes []model.Episode
		epErr    error
	)
	base, annotated := s.loadSeasons(id)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		series, serErr = s.client.GetSeries(ctx, id)
		if serErr != nil {
			return
		}
		cursor := ""
		for range 50 {
			page, err := s.client.ListEpisodes(ctx, id, sdk.EpisodeOptions{Cursor: cursor})
			if err != nil {
				if len(episodes) > 0 {
					break
				}
				epErr = err
				return
			}
			episodes = append(episodes, page.Items...)
			if !page.HasMore || page.Cursor == "" || page.Cursor == cursor {
				break
			}
			cursor = page.Cursor
		}
	}()
	wg.Wait()

	if serErr != nil {
		return nil, wrapErr(serErr)
	}
	if epErr != nil {
		return nil, wrapErr(epErr)
	}
	if len(episodes) == 0 {
		return nil, source.NewError(source.ErrUnavailable, "这部剧暂时没有可看的剧集", nil)
	}

	entries := s.awaitSeasons(ctx, base, annotated)
	var seasons []source.Season
	if len(entries) >= 2 {
		seasons = make([]source.Season, 0, len(entries))
		for _, e := range entries {
			seasons = append(seasons, toProtocolSeason(e, e.ID == id))
		}
	}

	meta := []source.Meta{{Value: fmt.Sprintf("全%d集", len(episodes))}}
	if series.PlayCount > 0 {
		meta = append(meta, source.Meta{Value: heat(series.PlayCount) + "播放"})
	} else if series.Heat > 0 {
		meta = append(meta, source.Meta{Value: heat(series.Heat) + "热度"})
	}

	eps := make([]source.Episode, 0, len(episodes))
	for _, ep := range episodes {
		e := source.Episode{
			ID:       ep.ID,
			Title:    ep.Title,
			Duration: ep.Duration.Seconds(),
			Image:    ep.CoverURL,
		}
		if ep.Vertical {
			e.Aspect = source.AspectPortrait
		}
		if ep.IsNew {
			e.Badges = []string{"新"}
		}
		if !ep.Playable {
			e.Lock = source.LockUnavailable
		}
		eps = append(eps, e)
	}

	return &source.Media{
		ID:          series.ID,
		Title:       series.Title,
		Image:       series.CoverURL,
		Description: series.Description,
		Badges:      episodesBadges(len(episodes)),
		Meta:        meta,
		Tags:        series.Subtitles,
		Aspect:      source.AspectPortrait,
		Seasons:     seasons,
		Episodes:    eps,
		Sections: []source.Section{{
			Title:      "相关推荐",
			Layout:     source.SectionRow,
			Collection: "related:" + series.ID,
		}},
	}, nil
}

func (s *Source) Play(ctx context.Context, r source.PlayRequest) (*source.Playback, error) {
	ctx, cancel := withTimeout(ctx, 30*time.Second)
	defer cancel()
	pb, err := s.client.ResolvePlayback(ctx, r.EpisodeID, sdk.PlaybackOptions{})
	if pb == nil {
		if err == nil {
			return nil, source.NewError(source.ErrUnavailable, "没有拿到播放地址", nil)
		}
		return nil, wrapErr(err)
	}
	var enc *source.Encryption
	if pb.Protected {
		k, kerr := sdk.ContentKey(pb)
		if kerr != nil {
			return nil, source.NewError(source.ErrPlugin, fmt.Sprintf("无法解密该集：%v", kerr), kerr)
		}
		enc = &source.Encryption{Scheme: source.EncryptionCENC, Key: k.Hex}
	}
	if pb.AudioURL != "" {
		return nil, source.NewError(source.ErrUnavailable, "暂不支持音视频分离的作品", nil)
	}
	var streams []source.Stream
	for _, st := range pb.Streams {
		if st.Dash || (st.URL == "" && st.BackupURL == "") {
			continue
		}
		primary, backups := st.URL, []string(nil)
		if primary == "" {
			primary = st.BackupURL
		} else if st.BackupURL != "" {
			backups = []string{st.BackupURL}
		}
		container := source.ContainerMP4
		if st.HLS {
			container = source.ContainerHLS
		}
		streams = append(streams, source.Stream{
			URL:        primary,
			BackupURLs: backups,
			Container:  container,
			Codec:      st.Codec,
			Width:      st.Width,
			Height:     st.Height,
			Bitrate:    st.Bitrate,
			Encryption: enc,
		})
	}
	if len(streams) == 0 {
		return nil, source.NewError(source.ErrUnavailable, "没有可用的视频流", nil)
	}
	return &source.Playback{Streams: streams}, nil
}

func (s *Source) Search(ctx context.Context, r source.SearchRequest) (*source.CollectionPage, error) {
	ctx, cancel := withTimeout(ctx, 30*time.Second)
	defer cancel()
	page, err := s.client.Search(ctx, r.Query, sdk.SearchOptions{Cursor: resume(r.Cursor)})
	if errors.Is(err, sdk.ErrAnonymousLimited) || errors.Is(err, sdk.ErrEndOfList) {
		return &source.CollectionPage{Layout: source.LayoutGrid}, nil
	}
	if err != nil {
		return nil, wrapErr(err)
	}
	items := make([]source.Card, 0, len(page.Items))
	for _, it := range page.Items {
		items = append(items, cardOfSummary(it))
	}
	return &source.CollectionPage{
		Layout: source.LayoutGrid,
		Items:  items,
		Cursor: nextCursor(page.HasMore, page.Cursor, r.Cursor),
	}, nil
}

func (s *Source) SearchHome(ctx context.Context) (*source.CollectionPage, error) {
	ctx, cancel := withTimeout(ctx, 30*time.Second)
	defer cancel()
	hs, err := s.client.HotSearch(ctx)
	if err != nil {
		return nil, wrapErr(err)
	}
	var sections []source.Section
	for _, board := range hs.Boards {
		var items []source.Card
		for _, it := range board.Items {
			if it.SeriesID == "" {
				continue
			}
			items = append(items, source.Card{
				Kind:   source.CardMedia,
				ID:     it.SeriesID,
				Title:  it.Title,
				Image:  it.CoverURL,
				Shape:  source.ShapePortrait,
				Badges: episodesBadges(it.EpisodeCount),
				Note:   it.Heat,
			})
		}
		s.fillCardCovers(ctx, items)
		if len(items) > 0 {
			sections = append(sections, source.Section{
				Title:  board.Name,
				Layout: source.SectionRanked,
				Items:  items,
			})
		}
	}
	return &source.CollectionPage{Sections: sections}, nil
}

func (s *Source) Suggest(context.Context, string) ([]string, error) {
	return nil, source.ErrUnsupported
}

func (s *Source) Refresh(ctx context.Context, ids []string) ([]source.Card, error) {
	ctx, cancel := withTimeout(ctx, 30*time.Second)
	defer cancel()
	if len(ids) == 0 {
		return nil, nil
	}
	if len(ids) > source.MaxRefreshIDs {
		ids = ids[:source.MaxRefreshIDs]
	}
	details, err := s.client.GetSeriesBatch(ctx, ids)
	if err != nil {
		return nil, wrapErr(err)
	}
	out := make([]source.Card, 0, len(details))
	for _, d := range details {
		if d.Series != nil {
			out = append(out, cardOfSummary(d.Series.SeriesSummary))
		}
	}
	return out, nil
}

func (s *Source) fillCardCovers(ctx context.Context, cards []source.Card) {
	var ids []string
	for _, c := range cards {
		if c.Kind == source.CardMedia && (c.Image == "" || len(c.Badges) == 0) {
			ids = append(ids, c.ID)
		}
	}
	if len(ids) == 0 {
		return
	}
	details, err := s.client.GetSeriesBatch(ctx, ids)
	if err != nil {
		return
	}
	byID := map[string]*model.Series{}
	for _, d := range details {
		if d.Series != nil {
			byID[d.Series.ID] = d.Series
		}
	}
	for i := range cards {
		if d := byID[cards[i].ID]; d != nil {
			if cards[i].Image == "" {
				cards[i].Image = d.CoverURL
			}
			if len(cards[i].Badges) == 0 && d.EpisodeCount > 0 {
				cards[i].Badges = episodesBadges(d.EpisodeCount)
			}
			if cards[i].Subtitle == "" {
				cards[i].Subtitle = d.Description
			}
		}
	}
}

func cardOfSummary(s model.SeriesSummary) source.Card {
	return source.Card{
		Kind:     source.CardMedia,
		ID:       s.ID,
		Title:    s.Title,
		Subtitle: s.Description,
		Image:    s.CoverURL,
		Shape:    source.ShapePortrait,
		Badges:   episodesBadges(s.EpisodeCount),
		Note:     s.Category,
	}
}

func episodesBadges(n int) []string {
	if n > 0 {
		return []string{fmt.Sprintf("全%d集", n)}
	}
	return nil
}

func heat(n int64) string {
	switch {
	case n >= 100_000_000:
		return fmt.Sprintf("%.1f亿", float64(n)/1e8)
	case n >= 10_000:
		return fmt.Sprintf("%.1f万", float64(n)/1e4)
	}
	return fmt.Sprint(n)
}

func wrapErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	switch {
	case errors.Is(err, sdk.ErrAuthenticationRequired):
		return source.NewError(source.ErrUnavailable, "该集需要登录才能观看", err)
	case errors.Is(err, sdk.ErrAnonymousLimited):
		e := source.NewError(source.ErrUnavailable, "服务端暂时限制了匿名播放，请稍后再试", err)
		e.RetryAfter = source.DefaultRetryAfter
		return e
	case errors.Is(err, sdk.ErrRateLimited):
		return source.RateLimitedError("请求太频繁，请稍后再试", source.DefaultRetryAfter, err)
	case errors.Is(err, sdk.ErrContentUnavailable):
		return source.NewError(source.ErrUnavailable, "该内容已下架", err)
	case errors.Is(err, sdk.ErrTransport):
		return source.NewError(source.ErrNetwork, "网络连接失败", err)
	default:
		return source.NewError(source.ErrPlugin, strings.TrimPrefix(err.Error(), "openhongguo: "), err)
	}
}
