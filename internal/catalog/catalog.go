// Package catalog reaches Hongguo through openhongguo: the catalog, and
// episodes downloaded and decrypted into a cache on disk.
package catalog

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/beilunyang/openhongguo/adapters"
	"github.com/beilunyang/openhongguo/sdk"
	"github.com/beilunyang/openhongguo/sdk/model"

	"hongguo/internal/cenc"
)

// Show is one drama as the grids and lists show it.
type Show struct {
	ID       string
	Title    string
	Desc     string
	Cover    string
	Episodes int
	Tag      string // a short note: a category, a heat score, a rank
}

func showOf(s model.SeriesSummary) Show {
	return Show{ID: s.ID, Title: s.Title, Desc: s.Description, Cover: s.CoverURL, Episodes: s.EpisodeCount, Tag: s.Category}
}

// Backend talks to Hongguo through openhongguo and keeps the episodes it
// fetched, decrypted, on disk.
type Backend struct {
	client   *sdk.Client
	cacheDir string
	http     *http.Client

	mu       sync.Mutex
	inflight map[string]*fetchJob
}

type fetchJob struct {
	done chan struct{}
	path string
	err  error
}

// New makes a backend keeping its anonymous device in dataDir and the
// episodes it fetched in cacheDir.
func New(dataDir, cacheDir string) (*Backend, error) {
	opts := sdk.Options{RetryAttempts: 3, DeviceResets: sdk.Int(3)}
	if l := openLog(filepath.Join(dataDir, "sdk.log")); l != nil {
		opts.Logger = l
	}
	client, err := adapters.NewClient(filepath.Join(dataDir, "anonymous.json"), opts)
	if err != nil {
		return nil, err
	}
	b := &Backend{
		client:   client,
		cacheDir: filepath.Join(cacheDir, "episodes"),
		http:     &http.Client{Transport: &http.Transport{ResponseHeaderTimeout: 30 * time.Second}},
		inflight: map[string]*fetchJob{},
	}
	if err := os.MkdirAll(b.cacheDir, 0o755); err != nil {
		return nil, err
	}
	go b.trimCache(2 << 30)
	return b, nil
}

// openLog opens the log the SDK writes device resets and failures to,
// starting it afresh once it passes 1 MB.
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

func ctxTimeout(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}

// Discover loads a page of a bookmall channel.
func (b *Backend) Discover(channel, cursor string) ([]Show, string, bool, error) {
	ctx, cancel := ctxTimeout(30 * time.Second)
	defer cancel()
	page, err := b.client.Discover(ctx, sdk.DiscoverOptions{Channel: channel, Cursor: cursor})
	if err != nil {
		return nil, "", false, err
	}
	out := make([]Show, 0, len(page.Items))
	for _, s := range page.Items {
		out = append(out, showOf(s))
	}
	return out, page.Cursor, page.HasMore && page.Cursor != "", nil
}

// Search loads a page of search results.
func (b *Backend) Search(query, cursor string) ([]Show, string, bool, error) {
	ctx, cancel := ctxTimeout(30 * time.Second)
	defer cancel()
	page, err := b.client.Search(ctx, query, sdk.SearchOptions{Cursor: cursor})
	if errors.Is(err, sdk.ErrAnonymousLimited) || errors.Is(err, sdk.ErrEndOfList) {
		return nil, "", false, nil
	}
	if err != nil {
		return nil, "", false, err
	}
	out := make([]Show, 0, len(page.Items))
	for _, s := range page.Items {
		out = append(out, showOf(s))
	}
	return out, page.Cursor, page.HasMore && page.Cursor != "", nil
}

// Lacking reports whether a show still lacks its cover or episode count,
// which Discover, Search and Rank leave for Fill.
func Lacking(s Show) bool { return s.Cover == "" || s.Episodes == 0 }

// Fill returns shows with the covers and counts they lack looked up. Pages
// come back without that lookup, a round trip of its own, so they show
// sooner; this fills them in after.
func (b *Backend) Fill(shows []Show) []Show {
	ctx, cancel := ctxTimeout(30 * time.Second)
	defer cancel()
	out := append([]Show(nil), shows...)
	b.fillCovers(ctx, out)
	return out
}

// fillCovers looks up the covers (and counts) search results lack.
func (b *Backend) fillCovers(ctx context.Context, shows []Show) {
	var ids []string
	for _, s := range shows {
		if Lacking(s) {
			ids = append(ids, s.ID)
		}
	}
	if len(ids) == 0 {
		return
	}
	details, err := b.client.GetSeriesBatch(ctx, ids)
	if err != nil {
		return
	}
	byID := map[string]*model.Series{}
	for _, d := range details {
		if d.Series != nil {
			byID[d.Series.ID] = d.Series
		}
	}
	for i := range shows {
		if d := byID[shows[i].ID]; d != nil {
			if shows[i].Cover == "" {
				shows[i].Cover = d.CoverURL
			}
			if shows[i].Episodes == 0 {
				shows[i].Episodes = d.EpisodeCount
			}
			if shows[i].Desc == "" {
				shows[i].Desc = d.Description
			}
		}
	}
}

// HotBoard is a hot-search board.
type HotBoard struct {
	Name  string
	Items []Show
}

// Hot loads the hot-search boards.
func (b *Backend) Hot() ([]HotBoard, error) {
	ctx, cancel := ctxTimeout(30 * time.Second)
	defer cancel()
	hs, err := b.client.HotSearch(ctx)
	if err != nil {
		return nil, err
	}
	var out []HotBoard
	for _, board := range hs.Boards {
		hb := HotBoard{Name: board.Name}
		for _, it := range board.Items {
			if it.SeriesID == "" {
				continue
			}
			hb.Items = append(hb.Items, Show{ID: it.SeriesID, Title: it.Title, Cover: it.CoverURL, Episodes: it.EpisodeCount, Tag: it.Heat})
		}
		b.fillCovers(ctx, hb.Items)
		if len(hb.Items) > 0 {
			out = append(out, hb)
		}
	}
	return out, nil
}

// RankPage is a page of a ranking.
type RankPage struct {
	Contents []model.RankContent
	Content  string
	Board    string
	Items    []Show
	Actors   []Actor // on the actors' boards, in place of Items
	Cursor   string
	HasMore  bool
}

// Rank loads a ranking: content and board are IDs, empty for the default.
func (b *Backend) Rank(content, board, cursor string) (*RankPage, error) {
	ctx, cancel := ctxTimeout(30 * time.Second)
	defer cancel()
	r, err := b.client.ShortRank(ctx, sdk.ShortRankQuery{Content: content, Board: board, Cursor: cursor})
	if err != nil {
		return nil, err
	}
	p := &RankPage{Contents: r.Contents, Content: r.Content, Board: r.Board, Cursor: r.Cursor, HasMore: r.HasMore && r.Cursor != ""}
	p.Items, p.Actors = rankRows(r.Items)
	return p, nil
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

// Detail is a drama with its episodes.
type Detail struct {
	Show
	Status   string
	Heat     string
	Tags     []string
	Episodes []model.Episode
}

// Series loads a drama and all its episodes.
func (b *Backend) Series(id string) (*Detail, error) {
	ctx, cancel := ctxTimeout(45 * time.Second)
	defer cancel()
	s, err := b.client.GetSeries(ctx, id)
	if err != nil {
		return nil, err
	}
	d := &Detail{Show: showOf(s.SeriesSummary), Tags: s.Subtitles}
	if s.PlayCount > 0 {
		d.Heat = heat(s.PlayCount) + "播放"
	} else if s.Heat > 0 {
		d.Heat = heat(s.Heat) + "热度"
	}
	cursor := ""
	for range 50 {
		page, err := b.client.ListEpisodes(ctx, id, sdk.EpisodeOptions{Cursor: cursor})
		if err != nil {
			if len(d.Episodes) > 0 {
				break
			}
			return nil, err
		}
		d.Episodes = append(d.Episodes, page.Items...)
		if !page.HasMore || page.Cursor == "" || page.Cursor == cursor {
			break
		}
		cursor = page.Cursor
	}
	if len(d.Episodes) == 0 {
		return nil, errors.New("这部剧暂时没有可看的剧集")
	}
	d.Show.Episodes = len(d.Episodes)
	return d, nil
}

// Recommend loads dramas related to one.
func (b *Backend) Recommend(id string) ([]Show, error) {
	ctx, cancel := ctxTimeout(30 * time.Second)
	defer cancel()
	r, err := b.client.SeriesRecommend(ctx, id)
	if err != nil {
		return nil, err
	}
	out := make([]Show, 0, len(r.Items))
	for _, s := range r.Items {
		if s.ID != id {
			out = append(out, showOf(s))
		}
	}
	b.fillCovers(ctx, out)
	return out, nil
}

// Episode returns the path of a playable, decrypted copy of an episode,
// downloading it first unless it is cached. Concurrent calls for the same
// episode share one download.
func (b *Backend) Episode(ctx context.Context, episodeID string) (string, error) {
	path := filepath.Join(b.cacheDir, episodeID+".mp4")
	if _, err := os.Stat(path); err == nil {
		now := time.Now()
		_ = os.Chtimes(path, now, now)
		return path, nil
	}
	b.mu.Lock()
	job, ok := b.inflight[episodeID]
	if !ok {
		job = &fetchJob{done: make(chan struct{})}
		b.inflight[episodeID] = job
		go func() {
			job.path, job.err = b.fetch(episodeID, path)
			b.mu.Lock()
			delete(b.inflight, episodeID)
			b.mu.Unlock()
			close(job.done)
		}()
	}
	b.mu.Unlock()
	select {
	case <-job.done:
		return job.path, job.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (b *Backend) fetch(episodeID, path string) (string, error) {
	ctx, cancel := ctxTimeout(3 * time.Minute)
	defer cancel()
	pb, err := b.client.ResolvePlayback(ctx, episodeID, sdk.PlaybackOptions{})
	if pb == nil {
		return "", friendly(err)
	}
	var key string
	if pb.Protected {
		k, kerr := sdk.ContentKey(pb)
		if kerr != nil {
			return "", fmt.Errorf("无法解密该集：%w", kerr)
		}
		key = k.Hex
	}
	if pb.AudioURL != "" {
		return "", errors.New("暂不支持音视频分离的作品")
	}
	streams := append([]model.Stream(nil), pb.Streams...)
	// Highest quality up to 1080p first.
	sort.SliceStable(streams, func(i, j int) bool {
		return rankStream(streams[i]) > rankStream(streams[j])
	})
	var lastErr error = errors.New("没有可用的视频流")
	for _, s := range streams {
		for _, u := range []string{s.URL, s.BackupURL} {
			if u == "" {
				continue
			}
			data, err := b.download(ctx, u)
			if err != nil {
				lastErr = err
				continue
			}
			if key != "" || cenc.IsEncrypted(data) {
				if key == "" {
					return "", errors.New("该集已加密，但没有拿到密钥")
				}
				if err := cenc.Decrypt(data, key); err != nil {
					return "", fmt.Errorf("解密失败：%w", err)
				}
			}
			tmp := path + ".part"
			if err := os.WriteFile(tmp, data, 0o644); err != nil {
				return "", err
			}
			if err := os.Rename(tmp, path); err != nil {
				return "", err
			}
			return path, nil
		}
	}
	return "", fmt.Errorf("下载失败：%w", lastErr)
}

func rankStream(s model.Stream) int {
	short := min(s.Width, s.Height)
	if short == 0 {
		short = 480
	}
	if short > 1080 {
		return 1080 - short // too big, rank below 1080p
	}
	return short*10 + int(min(s.Bitrate/100000, 9))
}

func (b *Backend) download(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := b.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// userError is a message for the interface that keeps the error behind it,
// so callers can still tell what happened with errors.Is.
type userError struct {
	msg string
	err error
}

func (e *userError) Error() string { return e.msg }
func (e *userError) Unwrap() error { return e.err }

func friendly(err error) error {
	msg := ""
	switch {
	case err == nil:
		return errors.New("没有拿到播放地址")
	case errors.Is(err, sdk.ErrAuthenticationRequired):
		msg = "该集需要登录才能观看"
	case errors.Is(err, sdk.ErrAnonymousLimited):
		msg = "服务端暂时限制了匿名播放，请稍后再试"
	case errors.Is(err, sdk.ErrRateLimited):
		msg = "请求太频繁，请稍后再试"
	case errors.Is(err, sdk.ErrContentUnavailable):
		msg = "该内容已下架"
	case errors.Is(err, sdk.ErrTransport):
		msg = "网络连接失败"
	default:
		return err
	}
	return &userError{msg, err}
}

// Unavailable reports an episode the anonymous device cannot play: the
// server returned nothing even on fresh devices, or wants a login.
// Autoplay skips such episodes.
func Unavailable(err error) bool {
	return errors.Is(err, sdk.ErrAnonymousLimited) || errors.Is(err, sdk.ErrAuthenticationRequired)
}

// Limited reports a failure that retrying at once will not fix: the SDK
// already registered fresh devices and all came back empty, so each retry
// only registers more. Wait a while first.
func Limited(err error) bool {
	return errors.Is(err, sdk.ErrAnonymousLimited) || errors.Is(err, sdk.ErrRateLimited)
}

// ErrText turns an error into a short message for the interface.
func ErrText(err error) string {
	if err == nil {
		return ""
	}
	msg := friendly(err).Error()
	msg = strings.TrimPrefix(msg, "openhongguo: ")
	return msg
}

// trimCache deletes the least recently used episodes beyond limit bytes.
func (b *Backend) trimCache(limit int64) {
	entries, err := os.ReadDir(b.cacheDir)
	if err != nil {
		return
	}
	type file struct {
		path string
		size int64
		mod  time.Time
	}
	var files []file
	var total int64
	for _, e := range entries {
		info, err := e.Info()
		if err != nil || info.IsDir() {
			continue
		}
		p := filepath.Join(b.cacheDir, e.Name())
		if strings.HasSuffix(e.Name(), ".part") {
			_ = os.Remove(p)
			continue
		}
		files = append(files, file{p, info.Size(), info.ModTime()})
		total += info.Size()
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mod.Before(files[j].mod) })
	for _, f := range files {
		if total <= limit {
			break
		}
		if os.Remove(f.path) == nil {
			total -= f.size
		}
	}
}
