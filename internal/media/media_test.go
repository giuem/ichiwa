package media

import (
	"bytes"
	"context"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/giuem/ichiwa/internal/source"
)

func TestSelectStreams(t *testing.T) {
	s360 := source.Stream{URL: "360", Width: 640, Height: 360, Bitrate: 300000, Container: source.ContainerMP4}
	s720 := source.Stream{URL: "720", Width: 720, Height: 1280, Bitrate: 1200000, Container: source.ContainerMP4}
	s1080 := source.Stream{URL: "1080", Width: 1080, Height: 1920, Bitrate: 2500000, Container: source.ContainerMP4}
	s4k := source.Stream{URL: "4k", Width: 2160, Height: 3840, Bitrate: 9000000, Container: source.ContainerMP4}

	got := SelectStreams([]source.Stream{s4k, s360, s1080, s720}, source.PlayPrefer{MaxHeight: 1080, HEVC: true})
	order := []string{got[0].URL, got[1].URL, got[2].URL, got[3].URL}
	if strings.Join(order, ",") != "1080,720,360,4k" {
		t.Fatalf("order = %v", order)
	}
}

func TestSelectStreamsHEVC(t *testing.T) {
	hevc := source.Stream{URL: "hevc", Width: 1080, Height: 1920, Codec: "bytevc1", Container: source.ContainerMP4}
	h264 := source.Stream{URL: "h264", Width: 720, Height: 1280, Codec: "h264", Container: source.ContainerMP4}
	unknown := source.Stream{URL: "unknown", Width: 720, Height: 1280, Container: source.ContainerMP4}

	if got := SelectStreams([]source.Stream{h264, hevc}, source.PlayPrefer{MaxHeight: 1080, HEVC: true}); got[0].URL != "hevc" {
		t.Fatalf("HEVC-capable should prefer 1080p HEVC, got %s", got[0].URL)
	}
	if got := SelectStreams([]source.Stream{hevc, h264}, source.PlayPrefer{MaxHeight: 1080, HEVC: false}); got[0].URL != "h264" {
		t.Fatalf("non-HEVC should prefer H.264, got %s", got[0].URL)
	}
	if got := SelectStreams([]source.Stream{hevc, unknown}, source.PlayPrefer{MaxHeight: 1080, HEVC: false}); got[0].URL != "unknown" {
		t.Fatalf("non-HEVC should prefer unknown codec over HEVC, got %s", got[0].URL)
	}
}

func TestCacheKey(t *testing.T) {
	k1 := CacheKey("a", "bc", "d")
	k2 := CacheKey("ab", "c", "d")
	if k1 == k2 {
		t.Fatal("CacheKey collided across field boundaries")
	}
	if len(k1) != 64 {
		t.Fatalf("CacheKey length = %d, want 64", len(k1))
	}
	k3 := CacheKey("hongguo", "../etc/passwd", "ep/1")
	if strings.ContainsAny(k3, "/\\.") {
		t.Fatalf("CacheKey contains path characters: %q", k3)
	}
}

func TestEpisodeDownloadDecryptBackupAndDedup(t *testing.T) {
	key, _ := hex.DecodeString("53891e0a11528165bac48415cdc614c2")
	enc, clearData := buildMP4(t, key, "encv", "hvc1", samplesOf(4, [][2]int{{5, 32}, {3, 17}}))

	var backupHits atomic.Int32
	var gotReferer atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/primary.mp4" {
			http.Error(w, "fail", http.StatusBadGateway)
			return
		}
		gotReferer.Store(r.Header.Get("Referer"))
		backupHits.Add(1)
		time.Sleep(40 * time.Millisecond)
		_, _ = w.Write(enc)
	}))
	defer srv.Close()

	mgr, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	var resolveHits atomic.Int32
	resolve := func(ctx context.Context, prefer source.PlayPrefer) (*source.Playback, error) {
		resolveHits.Add(1)
		return &source.Playback{
			Streams: []source.Stream{{
				URL:        srv.URL + "/primary.mp4",
				BackupURLs: []string{srv.URL + "/backup.mp4"},
				Container:  source.ContainerMP4,
				Headers:    map[string]string{"Referer": "https://example.com/", "Host": "evil.com"},
				Encryption: &source.Encryption{Scheme: source.EncryptionCENC, Key: hex.EncodeToString(key)},
			}},
		}, nil
	}

	// Concurrent callers share one download.
	var wg sync.WaitGroup
	paths := make([]string, 3)
	errs := make([]error, 3)
	for i := range 3 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			paths[i], errs[i] = mgr.Episode(context.Background(), "hongguo", "m1", "e1", resolve)
		}(i)
	}
	wg.Wait()
	for i := range 3 {
		if errs[i] != nil {
			t.Fatalf("caller %d failed: %v", i, errs[i])
		}
		if paths[i] != paths[0] {
			t.Fatalf("path mismatch: %q vs %q", paths[i], paths[0])
		}
	}
	if resolveHits.Load() != 1 || backupHits.Load() != 1 {
		t.Fatalf("dedup failed: resolves=%d hits=%d", resolveHits.Load(), backupHits.Load())
	}
	if gotReferer.Load() != "https://example.com/" {
		t.Fatalf("Referer = %v", gotReferer.Load())
	}
	saved, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	if IsCENCEncrypted(saved) {
		t.Fatal("cached file is still encrypted")
	}
	at := bytes.Index(saved, []byte("mdat"))
	if !bytes.Equal(saved[at:], clearData[at:]) {
		t.Fatal("decrypted samples mismatch")
	}

	// Cache hit does not call resolve again.
	path2, err := mgr.Episode(context.Background(), "hongguo", "m1", "e1", resolve)
	if err != nil || path2 != paths[0] || resolveHits.Load() != 1 {
		t.Fatalf("cache hit failed: path=%q err=%v resolves=%d", path2, err, resolveHits.Load())
	}
}

func TestTrimCacheLRU(t *testing.T) {
	mgr, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	mk := func(name string, size int, age time.Duration) string {
		p := filepath.Join(mgr.cacheDir, name)
		if err := os.WriteFile(p, make([]byte, size), 0o644); err != nil {
			t.Fatal(err)
		}
		ts := now.Add(-age)
		_ = os.Chtimes(p, ts, ts)
		return p
	}
	old := mk("old.mp4", 60, 3*time.Hour)
	mid := mk("mid.mp4", 60, 2*time.Hour)
	recent := mk("new.mp4", 60, time.Hour)
	part := mk("stale.mp4.part", 50, time.Minute)

	mgr.trimCache(100)

	if _, err := os.Stat(part); !os.IsNotExist(err) {
		t.Error("stale .part file was not removed")
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Error("oldest file was not evicted")
	}
	if _, err := os.Stat(mid); !os.IsNotExist(err) {
		t.Error("second oldest file should be evicted to fit limit 100")
	}
	if _, err := os.Stat(recent); err != nil {
		t.Error("newest file should be retained")
	}
}

func TestPreloadCanceledBeforeDelay(t *testing.T) {
	mgr, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var called atomic.Int32
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	mgr.Preload(ctx, "s", "m", "e", 20*time.Millisecond, func(context.Context, source.PlayPrefer) (*source.Playback, error) {
		called.Add(1)
		return nil, nil
	})
	time.Sleep(50 * time.Millisecond)
	if called.Load() != 0 {
		t.Fatal("preload ran despite canceled context")
	}
}
