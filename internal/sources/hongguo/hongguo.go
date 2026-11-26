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

	mu     sync.Mutex
	days   map[string]calDay // the 上新 calendar's days read lately
	panels map[string]panelCache
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
	if l := openLog(filepath.Join(dataDir, "sdk.log")); l != nil {
		opts.Logger = l
	}
	client, err := adapters.NewClient(filepath.Join(dataDir, "anonymous.json"), opts)
	if err != nil {
		return nil, err
	}
	return &Source{
		client: client,
		days:   map[string]calDay{},
		panels: map[string]panelCache{},
	}, nil
}

func openLog(path string) *log.Logger {
	flags := os.O_CREATE | os.O_WRONLY | os.O_APPEND
	if fi, err := os.Stat(path); err == nil && fi.Size() > 1<<20 {
		flags |= os.O_TRUNC
	}
	f, err := os.OpenFile(path, flags, 0o644)
	if err != nil {
		return nil
	}
	return log.New(f, "", log.LstdFlags)
}

func withTimeout(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, d)
}

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
	case strings.HasPrefix(r.ID, "person:"):
		return s.person(ctx, strings.TrimPrefix(r.ID, "person:"), r.Cursor)
	case strings.HasPrefix(r.ID, "related:"):
		return s.related(ctx, strings.TrimPrefix(r.ID, "related:"))
	default:
		return nil, source.NewError(source.ErrNotFound, "集合不存在", nil)
	}
}

func (s *Source) discover(ctx context.Context, channel, cursor string) (*source.CollectionPage, error) {
	ctx, cancel := withTimeout(ctx, 30*time.Second)
	defer cancel()
	page, err := s.client.Discover(ctx, sdk.DiscoverOptions{Channel: channel, Cursor: cursor})
	if err != nil {
		return nil, wrapErr(err)
	}
	items := make([]source.Card, 0, len(page.Items))
	for _, it := range page.Items {
		items = append(items, cardOfSummary(it))
	}
	next := ""
	if page.HasMore && page.Cursor != "" {
		next = page.Cursor
	}
	return &source.CollectionPage{
		Title:  channel,
		Layout: source.LayoutGrid,
		Items:  items,
		Cursor: next,
	}, nil
}

func (s *Source) rank(ctx context.Context, filters map[string]string, cursor string) (*source.CollectionPage, error) {
	ctx, cancel := withTimeout(ctx, 30*time.Second)
	defer cancel()
	r, err := s.client.ShortRank(ctx, sdk.ShortRankQuery{
		Content: strings.TrimSpace(filters["content"]),
		Board:   strings.TrimSpace(filters["board"]),
		Cursor:  cursor,
	})
	if err != nil {
		return nil, wrapErr(err)
	}
	p := &source.CollectionPage{
		Title:  "排行榜",
		Layout: source.LayoutRanked,
		Items:  rankCards(r.Items),
	}
	if r.HasMore && r.Cursor != "" {
		p.Cursor = r.Cursor
	}
	if cursor == "" {
		p.Filters = rankFilters(r)
	}
	return p, nil
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
			boardOpts = append(boardOpts, source.FilterOption{Value: b.ID, Title: b.Name})
		}
		rows = append(rows, source.FilterRow{
			Key:     "board",
			Title:   "榜单",
			Options: boardOpts,
			Value:   r.Board,
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

func (s *Source) filter(ctx context.Context, chosen map[string]string, cursor string) (*source.CollectionPage, error) {
	ctx, cancel := withTimeout(ctx, 30*time.Second)
	defer cancel()

	var filterRows []source.FilterRow
	effective := chosen
	if cursor == "" {
		genre := strings.TrimSpace(chosen["genre"])
		rows, err := s.filterPanel(ctx, genre)
		if err != nil {
			return nil, wrapErr(err)
		}
		effective, filterRows = buildFilterRows(rows, chosen)
	}

	page, err := s.client.ShortFilter(ctx, sdk.ShortFilterQuery{
		Selections: selections(effective),
		Cursor:     cursor,
	})
	if errors.Is(err, sdk.ErrContentUnavailable) && cursor == "" {
		return &source.CollectionPage{
			Title:   "筛选",
			Layout:  source.LayoutGrid,
			Filters: filterRows,
		}, nil
	}
	if err != nil {
		return nil, wrapErr(err)
	}
	items := make([]source.Card, 0, len(page.Items))
	for _, it := range page.Items {
		items = append(items, cardOfSummary(it))
	}
	next := ""
	if page.HasMore && page.Cursor != "" {
		next = page.Cursor
	}
	return &source.CollectionPage{
		Title:   "筛选",
		Layout:  source.LayoutGrid,
		Filters: filterRows,
		Items:   items,
		Cursor:  next,
	}, nil
}

// buildFilterRows converts Hongguo's filter rows to protocol FilterRows and
// drops any chosen values that do not exist in the current genre's options.
func buildFilterRows(rows []model.FilterRow, chosen map[string]string) (map[string]string, []source.FilterRow) {
	effective := make(map[string]string, len(chosen))
	out := make([]source.FilterRow, 0, len(rows))
	for _, row := range rows {
		val := strings.TrimSpace(chosen[row.Key])
		valid := false
		opts := make([]source.FilterOption, 0, len(row.Options)+1)
		opts = append(opts, source.FilterOption{Value: "", Title: row.Name})
		for _, o := range row.Options {
			opts = append(opts, source.FilterOption{Value: o.Value, Title: o.Name})
			if val != "" && o.Value == val {
				valid = true
			}
		}
		if valid {
			effective[row.Key] = val
		} else {
			val = ""
		}
		out = append(out, source.FilterRow{
			Key:     row.Key,
			Title:   row.Name,
			Options: opts,
			Value:   val,
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

func (s *Source) person(ctx context.Context, userID, cursor string) (*source.CollectionPage, error) {
	ctx, cancel := withTimeout(ctx, 30*time.Second)
	defer cancel()

	var (
		header    *source.Header
		headerErr error
		worksPage *model.WorkPage
		worksErr  error
	)
	if cursor == "" {
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			p, err := s.client.Person(ctx, userID)
			if err != nil {
				headerErr = err
				return
			}
			header = personHeader(p)
		}()
		go func() {
			defer wg.Done()
			worksPage, worksErr = s.client.PersonWorks(ctx, userID, "")
		}()
		wg.Wait()
		if worksErr != nil && headerErr != nil {
			return nil, wrapErr(worksErr)
		}
	} else {
		worksPage, worksErr = s.client.PersonWorks(ctx, userID, cursor)
		if worksErr != nil {
			return nil, wrapErr(worksErr)
		}
	}

	out := &source.CollectionPage{
		Header: header,
		Layout: source.LayoutGrid,
	}
	if header != nil {
		out.Title = header.Title
	}
	if worksPage != nil {
		for _, w := range worksPage.Items {
			if w.SeriesID != "" {
				out.Items = append(out.Items, cardOfWork(w))
			}
		}
		if worksPage.HasMore && worksPage.Cursor != "" {
			out.Cursor = worksPage.Cursor
		}
	}
	if worksErr != nil && len(out.Items) == 0 && header == nil {
		return nil, wrapErr(worksErr)
	}
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
		seasons  []source.Season
	)

	var wg sync.WaitGroup
	wg.Add(2)
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
	go func() {
		defer wg.Done()
		// Seasons and calendar notes are best-effort so they never block or fail playback.
		sCtx, sCancel := context.WithTimeout(ctx, 10*time.Second)
		defer sCancel()
		seasons = s.loadSeasons(sCtx, id)
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
	page, err := s.client.Search(ctx, r.Query, sdk.SearchOptions{Cursor: r.Cursor})
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
	next := ""
	if page.HasMore && page.Cursor != "" {
		next = page.Cursor
	}
	return &source.CollectionPage{
		Layout: source.LayoutGrid,
		Items:  items,
		Cursor: next,
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
