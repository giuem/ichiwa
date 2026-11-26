// Package media selects playable streams from a source.Playback, downloads and
// decrypts CENC-protected MP4 episodes, caches them on disk with LRU eviction,
// and preloads upcoming episodes.
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
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

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

	mu       sync.Mutex
	inflight map[string]*fetchJob
}

type fetchJob struct {
	done chan struct{}
	path string
	err  error
}

// New creates a Manager storing cached episodes in cacheDir/episodes and trims
// existing cached files to DefaultCacheLimit in the background.
func New(cacheDir string) (*Manager, error) {
	dir := filepath.Join(cacheDir, "episodes")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	m := &Manager{
		cacheDir: dir,
		http:     &http.Client{Transport: &http.Transport{ResponseHeaderTimeout: 30 * time.Second}},
		limit:    DefaultCacheLimit,
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
// for the same episode share one download.
func (m *Manager) Episode(ctx context.Context, sourceID, mediaID, episodeID string, resolve Resolver) (string, error) {
	key := CacheKey(sourceID, mediaID, episodeID)
	path := filepath.Join(m.cacheDir, key+".mp4")
	if _, err := os.Stat(path); err == nil {
		now := time.Now()
		_ = os.Chtimes(path, now, now)
		return path, nil
	}
	return m.runSingle(ctx, key, func() (string, error) {
		fetchCtx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		pb, err := resolve(fetchCtx, Prefer())
		if err != nil {
			return "", err
		}
		return m.downloadPlayback(fetchCtx, path, pb, Prefer())
	})
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

func (m *Manager) runSingle(ctx context.Context, key string, work func() (string, error)) (string, error) {
	m.mu.Lock()
	job, ok := m.inflight[key]
	if !ok {
		job = &fetchJob{done: make(chan struct{})}
		m.inflight[key] = job
		go func() {
			job.path, job.err = work()
			m.mu.Lock()
			delete(m.inflight, key)
			m.mu.Unlock()
			close(job.done)
		}()
	}
	m.mu.Unlock()
	select {
	case <-job.done:
		return job.path, job.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (m *Manager) downloadPlayback(ctx context.Context, path string, pb *source.Playback, prefer source.PlayPrefer) (string, error) {
	if pb == nil || len(pb.Streams) == 0 {
		return "", errors.New("没有可用的视频流")
	}
	streams := SelectStreams(pb.Streams, prefer)
	var lastErr error = errors.New("没有可用的视频流")
	for _, s := range streams {
		if s.Container != source.ContainerMP4 {
			lastErr = errors.New("暂不支持该播放格式")
			continue
		}
		urls := make([]string, 0, 1+len(s.BackupURLs))
		if s.URL != "" {
			urls = append(urls, s.URL)
		}
		urls = append(urls, s.BackupURLs...)
		headers := source.CleanStreamHeaders(s.Headers)
		for _, u := range urls {
			if u == "" {
				continue
			}
			data, err := m.download(ctx, u, headers)
			if err != nil {
				lastErr = err
				continue
			}
			var key string
			if s.Encryption != nil {
				if s.Encryption.Scheme != source.EncryptionCENC {
					return "", fmt.Errorf("不支持的加密方案：%s", s.Encryption.Scheme)
				}
				key = s.Encryption.Key
			}
			if key != "" || IsCENCEncrypted(data) {
				if key == "" {
					return "", errors.New("该集已加密，但没有拿到密钥")
				}
				if err := DecryptCENC(data, key); err != nil {
					return "", fmt.Errorf("解密失败：%w", err)
				}
			}
			tmp := path + ".part"
			if err := os.WriteFile(tmp, data, 0o644); err != nil {
				return "", err
			}
			if err := os.Rename(tmp, path); err != nil {
				_ = os.Remove(tmp)
				return "", err
			}
			go m.trimCache(m.limit)
			return path, nil
		}
	}
	return "", fmt.Errorf("下载失败：%w", lastErr)
}

func (m *Manager) download(ctx context.Context, rawURL string, headers map[string]string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := m.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// SelectStreams returns a copy of streams ordered from most to least preferred
// for prefer: codecs the device cannot decode are ranked last, then the highest
// short-edge resolution up to prefer.MaxHeight, falling back above it only if
// nothing smaller is offered.
func SelectStreams(streams []source.Stream, prefer source.PlayPrefer) []source.Stream {
	out := append([]source.Stream(nil), streams...)
	maxHeight := prefer.MaxHeight
	if maxHeight <= 0 {
		maxHeight = 1080
	}
	sort.SliceStable(out, func(i, j int) bool {
		return rankStream(out[i], maxHeight, prefer.HEVC) > rankStream(out[j], maxHeight, prefer.HEVC)
	})
	return out
}

const hevcPenalty = 1 << 20

func rankStream(s source.Stream, maxHeight int, hevc bool) int {
	score := rankBySize(s, maxHeight)
	if !hevc && isHEVC(s.Codec) {
		return score - hevcPenalty
	}
	return score
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
			key := strings.TrimSuffix(e.Name(), ".mp4.part")
			m.mu.Lock()
			_, active := m.inflight[key]
			m.mu.Unlock()
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
