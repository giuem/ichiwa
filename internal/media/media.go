// Package media selects playable streams from a source.Playback, downloads and
// decrypts CENC-protected MP4 episodes, caches them on disk with LRU eviction,
// preloads upcoming episodes, and serves HLS streams to the player through a
// loopback proxy. Everything it fetches for a plugin stays within the hosts
// the plugin declared (docs/plugin-design.md §7, §11).
package media

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/giuem/ichiwa/internal/netguard"
	"github.com/giuem/ichiwa/internal/source"
)

// DefaultCacheLimit is the maximum disk space used by cached episodes (2 GiB).
const DefaultCacheLimit int64 = 2 << 30

// Resolver fetches playback streams for an episode when it is not cached.
type Resolver func(ctx context.Context, prefer source.PlayPrefer) (*source.Playback, error)

// Manager handles stream selection, downloading, CENC decryption, disk caching,
// concurrent deduplication, and preloading for episodes.
type Manager struct {
	cacheDir string
	http     *http.Client
	limit    int64
	guard    *netguard.Guard
	sandbox  http.RoundTripper // dials through guard
	policy   func(sourceID string) Policy
	hls      *hlsProxy

	mu       sync.Mutex
	inflight map[string]*fetchJob
}

type fetchJob struct {
	done chan struct{}
	item Item
	err  error
}

// Item is what the player opens: a local file, or the loopback URL of an
// HLS stream.
type Item struct {
	Path string
	URL  string
}

// Policy is what the media of one source may reach.
type Policy struct {
	// Sandboxed sends every request through the network guard and only
	// to Hosts; a built-in source the host trusts is not sandboxed.
	Sandboxed bool
	Hosts     netguard.Hosts
}

// PolicyFor is the policy of a source: a plugin's hosts, or none for a
// built-in source.
func PolicyFor(info source.Info) Policy {
	if info.Network == nil {
		return Policy{}
	}
	return Policy{Sandboxed: true, Hosts: netguard.Hosts(info.Network)}
}

func (p Policy) allows(u *url.URL) bool {
	if u == nil || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	return !p.Sandboxed || p.Hosts.AllowsURL(u)
}

// SetPolicy tells the manager what each source's media may reach. A source
// the function does not know should get a sandboxed Policy without hosts.
// Until it is set every source is trusted.
func (m *Manager) SetPolicy(f func(sourceID string) Policy) { m.policy = f }

// SetGuard replaces the network guard, for tests.
func (m *Manager) SetGuard(g *netguard.Guard) {
	m.guard = g
	m.sandbox = g.Transport()
}

func (m *Manager) policyOf(sourceID string) Policy {
	if m.policy == nil {
		return Policy{}
	}
	return m.policy(sourceID)
}

// client returns the client for a policy: through the guard and with
// every redirect checked when sandboxed.
func (m *Manager) client(p Policy) *http.Client {
	if !p.Sandboxed {
		return m.http
	}
	return &http.Client{
		Transport:     m.sandbox,
		CheckRedirect: netguard.CheckRedirect(p.allows),
	}
}

// New creates a Manager storing cached episodes in cacheDir/episodes and trims
// existing cached files to DefaultCacheLimit in the background.
func New(cacheDir string) (*Manager, error) {
	dir := filepath.Join(cacheDir, "episodes")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	guard := &netguard.Guard{}
	m := &Manager{
		cacheDir: dir,
		http:     &http.Client{Transport: &http.Transport{Proxy: http.ProxyFromEnvironment, ResponseHeaderTimeout: 30 * time.Second}},
		limit:    DefaultCacheLimit,
		guard:    guard,
		sandbox:  guard.Transport(),
		inflight: map[string]*fetchJob{},
	}
	go m.trimCache(m.limit)
	return m, nil
}

// Prefer returns the playback stream preferences for this machine.
func Prefer() source.PlayPrefer {
	return source.PlayPrefer{
		MaxHeight: 1080,
		HEVC:      runtime.GOOS == "darwin",
	}
}

// CacheKey returns the SHA-256 hex digest identifying an episode on disk.
// Plugin-supplied IDs are never used directly as filenames.
func CacheKey(sourceID, mediaID, episodeID string) string {
	h := sha256.New()
	for _, s := range []string{sourceID, mediaID, episodeID} {
		var n [8]byte
		binary.BigEndian.PutUint64(n[:], uint64(len(s)))
		h.Write(n[:])
		h.Write([]byte(s))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Episode returns the local file path of a playable, decrypted copy of the
// episode, downloading it via resolve unless already cached. Concurrent calls
// for the same episode share one download. Only MP4 streams are cached; an
// episode that only has HLS streams has no file.
func (m *Manager) Episode(ctx context.Context, sourceID, mediaID, episodeID string, resolve Resolver) (string, error) {
	it, err := m.open(ctx, sourceID, mediaID, episodeID, resolve, false)
	return it.Path, err
}

// Open returns what the player opens for the episode: the cached file, a
// new download of the best MP4 stream, or the loopback URL of the best HLS
// stream, in the order SelectStreams ranks them.
func (m *Manager) Open(ctx context.Context, sourceID, mediaID, episodeID string, resolve Resolver) (Item, error) {
	return m.open(ctx, sourceID, mediaID, episodeID, resolve, true)
}

func (m *Manager) open(ctx context.Context, sourceID, mediaID, episodeID string, resolve Resolver, hls bool) (Item, error) {
	key := CacheKey(sourceID, mediaID, episodeID)
	path := filepath.Join(m.cacheDir, key+".mp4")
	if _, err := os.Stat(path); err == nil {
		now := time.Now()
		_ = os.Chtimes(path, now, now)
		return Item{Path: path}, nil
	}
	if sourceID == "hongguo" && isSafeLegacyID(episodeID) {
		legacy := filepath.Join(m.cacheDir, episodeID+".mp4")
		if legacy != path {
			if err := os.Rename(legacy, path); err == nil {
				now := time.Now()
				_ = os.Chtimes(path, now, now)
				return Item{Path: path}, nil
			}
			// A concurrent caller may have renamed legacy to path first.
			if _, err := os.Stat(path); err == nil {
				now := time.Now()
				_ = os.Chtimes(path, now, now)
				return Item{Path: path}, nil
			}
		}
	}
	policy := m.policyOf(sourceID)
	jobKey := key
	if !hls {
		jobKey += ":mp4"
	}
	return m.runSingle(ctx, jobKey, func() (Item, error) {
		fetchCtx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		pb, err := resolve(fetchCtx, Prefer())
		if err != nil {
			return Item{}, err
		}
		return m.downloadPlayback(fetchCtx, path, pb, Prefer(), policy, hls)
	})
}

func isSafeLegacyID(id string) bool {
	return id != "" && id != "." && id != ".." && !strings.ContainsAny(id, `/\`)
}

// Fetch downloads and caches an episode using an already-resolved Playback.
func (m *Manager) Fetch(ctx context.Context, sourceID, mediaID, episodeID string, pb *source.Playback) (string, error) {
	return m.Episode(ctx, sourceID, mediaID, episodeID, func(context.Context, source.PlayPrefer) (*source.Playback, error) {
		return pb, nil
	})
}

// Preload schedules a background download of an episode after delay, unless ctx
// is canceled before the delay elapses or the episode is already cached.
func (m *Manager) Preload(ctx context.Context, sourceID, mediaID, episodeID string, delay time.Duration, resolve Resolver) {
	go func() {
		if delay > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		} else if ctx.Err() != nil {
			return
		}
		_, _ = m.Episode(ctx, sourceID, mediaID, episodeID, resolve)
	}()
}

func (m *Manager) runSingle(ctx context.Context, key string, work func() (Item, error)) (Item, error) {
	m.mu.Lock()
	job, ok := m.inflight[key]
	if !ok {
		job = &fetchJob{done: make(chan struct{})}
		m.inflight[key] = job
		go func() {
			job.item, job.err = work()
			m.mu.Lock()
			delete(m.inflight, key)
			m.mu.Unlock()
			close(job.done)
		}()
	}
	m.mu.Unlock()
	select {
	case <-job.done:
		return job.item, job.err
	case <-ctx.Done():
		return Item{}, ctx.Err()
	}
}

// ErrNoPlayableStream is the error of an episode none of whose streams
// this device can play.
var ErrNoPlayableStream = errors.New("这台设备放不了这一集的视频格式")

func (m *Manager) downloadPlayback(ctx context.Context, path string, pb *source.Playback, prefer source.PlayPrefer, policy Policy, hls bool) (Item, error) {
	if pb == nil || len(pb.Streams) == 0 {
		return Item{}, errors.New("没有可用的视频流")
	}
	streams := SelectStreams(pb.Streams, prefer)
	if len(streams) == 0 {
		return Item{}, ErrNoPlayableStream
	}
	var lastErr error = errors.New("没有可用的视频流")
	for _, s := range streams {
		headers := source.CleanStreamHeaders(s.Headers)
		urls := make([]string, 0, 1+len(s.BackupURLs))
		if s.URL != "" {
			urls = append(urls, s.URL)
		}
		urls = append(urls, s.BackupURLs...)
		if s.Container == source.ContainerHLS {
			if !hls {
				lastErr = errors.New("HLS 不缓存")
				continue
			}
			if s.Encryption != nil {
				// CENC applies to the MP4 the host downloads; an HLS
				// stream decrypts only with HLS's own AES-128.
				lastErr = errors.New("暂不支持加密的 HLS")
				continue
			}
			for _, u := range urls {
				it, err := m.serveHLS(ctx, u, headers, policy)
				if err != nil {
					lastErr = err
					continue
				}
				return it, nil
			}
			continue
		}
		if s.Container != source.ContainerMP4 {
			lastErr = errors.New("暂不支持该播放格式")
			continue
		}
		for _, u := range urls {
			if u == "" {
				continue
			}
			data, err := m.download(ctx, u, headers, policy)
			if err != nil {
				lastErr = err
				continue
			}
			var key string
			if s.Encryption != nil {
				if s.Encryption.Scheme != source.EncryptionCENC {
					return Item{}, fmt.Errorf("不支持的加密方案：%s", s.Encryption.Scheme)
				}
				key = s.Encryption.Key
			}
			if key != "" || IsCENCEncrypted(data) {
				if key == "" {
					return Item{}, errors.New("该集已加密，但没有拿到密钥")
				}
				if err := DecryptCENC(data, key); err != nil {
					return Item{}, fmt.Errorf("解密失败：%w", err)
				}
			}
			f, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.part")
			if err != nil {
				return Item{}, err
			}
			tmp := f.Name()
			_, err = f.Write(data)
			if cerr := f.Close(); err == nil {
				err = cerr
			}
			if err != nil {
				_ = os.Remove(tmp)
				return Item{}, err
			}
			if err := os.Rename(tmp, path); err != nil {
				_ = os.Remove(tmp)
				return Item{}, err
			}
			go m.trimCache(m.limit)
			return Item{Path: path}, nil
		}
	}
	return Item{}, fmt.Errorf("下载失败：%w", lastErr)
}

// maxEpisodeBytes caps one downloaded episode.
const maxEpisodeBytes = 2 << 30

func (m *Manager) download(ctx context.Context, rawURL string, headers map[string]string, policy Policy) ([]byte, error) {
	u, err := url.Parse(rawURL)
	if err != nil || !policy.allows(u) {
		return nil, fmt.Errorf("%w: 媒体地址不在片源声明的范围内", netguard.ErrDenied)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := m.client(policy).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxEpisodeBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxEpisodeBytes {
		return nil, errors.New("视频文件太大")
	}
	return data, nil
}

// SelectStreams returns the streams this device can play, ordered from most
// to least preferred for prefer (§7): codecs and containers the device
// cannot play are dropped, then the highest short-edge resolution up to
// prefer.MaxHeight comes first, falling back above it only if nothing
// smaller is offered.
func SelectStreams(streams []source.Stream, prefer source.PlayPrefer) []source.Stream {
	out := make([]source.Stream, 0, len(streams))
	for _, s := range streams {
		if Playable(s, prefer) {
			out = append(out, s)
		}
	}
	maxHeight := prefer.MaxHeight
	if maxHeight <= 0 {
		maxHeight = 1080
	}
	sort.SliceStable(out, func(i, j int) bool {
		return rankBySize(out[i], maxHeight) > rankBySize(out[j], maxHeight)
	})
	return out
}

// hlsPlays tells whether the system's player streams HLS: AVFoundation
// and Media Foundation do; GStreamer, which plays video on Linux, needs a
// plugin that is not always there, and mygo does not promise it.
var hlsPlays = runtime.GOOS != "linux"

// Playable reports whether this device plays s. H.264 plays everywhere,
// HEVC where prefer.HEVC says the device decodes it, and AV1, VP9 and VP8
// nowhere for now: no system's player is sure to decode them. A codec
// the source did not name is tried.
func Playable(s source.Stream, prefer source.PlayPrefer) bool {
	if s.Container == source.ContainerHLS && !hlsPlays {
		return false
	}
	switch codecFamily(s.Codec) {
	case "hevc":
		return prefer.HEVC
	case "av1", "vp9", "vp8":
		return false
	}
	return true
}

func codecFamily(codec string) string {
	c := strings.ToLower(strings.TrimSpace(codec))
	switch {
	case c == "":
		return ""
	case isHEVC(c):
		return "hevc"
	case strings.HasPrefix(c, "av1") || strings.HasPrefix(c, "av01"):
		return "av1"
	case strings.HasPrefix(c, "vp9") || strings.HasPrefix(c, "vp09"):
		return "vp9"
	case strings.HasPrefix(c, "vp8") || strings.HasPrefix(c, "vp08"):
		return "vp8"
	}
	return c
}

func isHEVC(codec string) bool {
	codec = strings.ToLower(codec)
	for _, name := range []string{"h265", "h.265", "hevc", "hvc1", "hev1", "bytevc1"} {
		if strings.Contains(codec, name) {
			return true
		}
	}
	return false
}

func rankBySize(s source.Stream, maxHeight int) int {
	short := min(s.Width, s.Height)
	if short == 0 {
		short = max(s.Width, s.Height)
	}
	if short == 0 {
		short = 480
	}
	if short > maxHeight {
		return maxHeight - short // too big, rank below streams <= maxHeight
	}
	return short*10 + int(min(s.Bitrate/100000, 9))
}

// trimCache deletes partial downloads and the least recently used episodes
// beyond limit bytes.
func (m *Manager) trimCache(limit int64) {
	entries, err := os.ReadDir(m.cacheDir)
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
		p := filepath.Join(m.cacheDir, e.Name())
		if strings.HasSuffix(e.Name(), ".part") {
			// Skip active in-flight writes.
			key, _, _ := strings.Cut(e.Name(), ".")
			m.mu.Lock()
			_, active := m.inflight[key]
			_, activeMP4 := m.inflight[key+":mp4"]
			m.mu.Unlock()
			active = active || activeMP4
			if !active {
				_ = os.Remove(p)
			}
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
