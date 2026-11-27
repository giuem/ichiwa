package media

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/giuem/ichiwa/internal/netguard"
	"github.com/giuem/ichiwa/internal/source"
)

// sandboxEnv is a media server on loopback reached as the declared
// cdn.example.test, and the manager sandboxing a plugin source "p" to it.
type sandboxEnv struct {
	mgr  *Manager
	srv  *httptest.Server
	port string
	hits atomic.Int32
}

const mp4Bytes = "\x00\x00\x00\x18ftypisom\x00\x00\x02\x00isomiso2"

func newSandboxEnv(t *testing.T, handler http.Handler) *sandboxEnv {
	t.Helper()
	e := &sandboxEnv{}
	e.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		e.hits.Add(1)
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(e.srv.Close)
	_, e.port, _ = net.SplitHostPort(e.srv.Listener.Addr().String())
	ap := netip.MustParseAddrPort(e.srv.Listener.Addr().String())
	mgr, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	mgr.SetGuard(&netguard.Guard{
		Lookup: func(_ context.Context, host string) ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
		},
		Exempt: func(a netip.AddrPort) bool { return a == ap },
	})
	mgr.SetPolicy(func(id string) Policy {
		if id == "p" {
			return Policy{Sandboxed: true, Hosts: netguard.Hosts{"cdn.example.test", "*.ok.example.test"}}
		}
		return Policy{Sandboxed: true}
	})
	e.mgr = mgr
	return e
}

func (e *sandboxEnv) url(host, path string) string { return "http://" + host + ":" + e.port + path }

func streams(urls ...string) Resolver {
	return func(context.Context, source.PlayPrefer) (*source.Playback, error) {
		var ss []source.Stream
		for _, u := range urls {
			ss = append(ss, source.Stream{URL: u, Container: source.ContainerMP4})
		}
		return &source.Playback{Streams: ss}, nil
	}
}

func TestDownloadChecksEveryRedirect(t *testing.T) {
	var e *sandboxEnv
	e = newSandboxEnv(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/bad":
			http.Redirect(w, r, e.url("elsewhere.test", "/v.mp4"), http.StatusFound)
		case "/good":
			http.Redirect(w, r, e.url("a.ok.example.test", "/v.mp4"), http.StatusFound)
		default:
			io.WriteString(w, mp4Bytes)
		}
	}))
	if _, err := e.mgr.Episode(context.Background(), "p", "m", "bad", streams(e.url("cdn.example.test", "/bad"))); err == nil {
		t.Fatal("a redirect to an undeclared host was followed")
	}
	if e.hits.Load() != 1 {
		t.Fatalf("the undeclared host was fetched: %d hits", e.hits.Load())
	}
	path, err := e.mgr.Episode(context.Background(), "p", "m", "good", streams(e.url("cdn.example.test", "/good")))
	if err != nil || path == "" {
		t.Fatalf("a redirect to a declared host: %q, %v", path, err)
	}
}

func TestDownloadRefusesUndeclaredAndInternal(t *testing.T) {
	e := newSandboxEnv(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, mp4Bytes) }))
	for _, u := range []string{
		e.url("elsewhere.test", "/v.mp4"),
		e.url("127.0.0.1", "/v.mp4"),
		"file:///etc/passwd",
	} {
		if _, err := e.mgr.Episode(context.Background(), "p", "m", u, streams(u)); err == nil {
			t.Errorf("%s downloaded", u)
		}
	}
	// A source the policy does not know reaches nothing.
	if _, err := e.mgr.Episode(context.Background(), "unknown", "m", "e", streams(e.url("cdn.example.test", "/v.mp4"))); err == nil {
		t.Error("an unknown source downloaded")
	}
	if e.hits.Load() != 0 {
		t.Fatalf("%d requests reached the server", e.hits.Load())
	}
	// A declared name pointing at a loopback address that is not exempt.
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, mp4Bytes) }))
	defer other.Close()
	_, port, _ := net.SplitHostPort(other.Listener.Addr().String())
	u := "http://cdn.example.test:" + port + "/v.mp4"
	if _, err := e.mgr.Episode(context.Background(), "p", "m", "internal", streams(u)); err == nil {
		t.Error("a declared name resolving to loopback downloaded")
	}
}

func TestHLSProxy(t *testing.T) {
	// The proxy works the same where the player cannot stream HLS.
	defer func(v bool) { hlsPlays = v }(hlsPlays)
	hlsPlays = true
	var gotReferer atomic.Value
	var e *sandboxEnv
	e = newSandboxEnv(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotReferer.Store(r.Header.Get("Referer"))
		switch r.URL.Path {
		case "/master.m3u8":
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			body := fmt.Sprintf("#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=1000\nlow/index.m3u8\n#EXT-X-MEDIA:TYPE=AUDIO,URI=\"%s\"\n", e.url("a.ok.example.test", "/audio.m3u8"))
			http.ServeContent(w, r, "master.m3u8", time.Time{}, strings.NewReader(body))
		case "/low/index.m3u8":
			// No content type: sniffed. A key, a declared segment, an
			// undeclared one, a local file and a redirect.
			fmt.Fprintf(w, "#EXTM3U\r\n#EXT-X-KEY:METHOD=AES-128,URI=\"../key.bin\",IV=0x1\r\n#EXTINF:4,\r\nseg1.ts\r\n#EXTINF:4,\r\n%s\r\n#EXTINF:4,\r\nfile:///etc/passwd\r\n", e.url("elsewhere.test", "/seg2.ts"))
		case "/key.bin":
			io.WriteString(w, "0123456789abcdef")
		case "/low/seg1.ts":
			w.Header().Set("Content-Type", "video/mp2t")
			http.ServeContent(w, r, "seg1.ts", time.Time{}, strings.NewReader("segment-bytes"))
		default:
			http.NotFound(w, r)
		}
	}))
	mgr := e.mgr
	it, err := mgr.Open(context.Background(), "p", "m", "e", func(context.Context, source.PlayPrefer) (*source.Playback, error) {
		return &source.Playback{Streams: []source.Stream{{
			URL: e.url("cdn.example.test", "/master.m3u8"), Container: source.ContainerHLS,
			Headers: map[string]string{"Referer": "https://example.test/"},
		}}}, nil
	})
	if err != nil || it.URL == "" || it.Path != "" {
		t.Fatalf("Open = %+v, %v", it, err)
	}
	pu, _ := url.Parse(it.URL)
	if h, _, _ := net.SplitHostPort(pu.Host); h != "127.0.0.1" {
		t.Fatalf("proxy listens on %s", pu.Host)
	}
	get := func(u string, hdr map[string]string) (int, string) {
		req, _ := http.NewRequest("GET", u, nil)
		for k, v := range hdr {
			if k == "Host" {
				req.Host = v
			} else {
				req.Header.Set(k, v)
			}
		}
		resp, err := http.DefaultTransport.RoundTrip(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}
	// Players ask for a range of a playlist now and then; the answer is
	// the whole playlist, rewritten, even where the server honors ranges.
	code, ranged := get(it.URL, map[string]string{"Range": "bytes=0-"})
	if code != 200 || strings.Contains(ranged, "\nlow/index.m3u8") {
		t.Fatalf("ranged master = %d\n%s", code, ranged)
	}
	code, master := get(it.URL, nil)
	if code != 200 || strings.Contains(master, "cdn.example.test") || strings.Contains(master, "ok.example.test") {
		t.Fatalf("master = %d\n%s", code, master)
	}
	if gotReferer.Load() != "https://example.test/" {
		t.Fatalf("Referer = %v", gotReferer.Load())
	}
	var variant string
	for _, l := range strings.Split(master, "\n") {
		if strings.HasPrefix(l, "http://127.0.0.1") {
			variant = l
		}
	}
	code, media := get(variant, nil)
	if code != 200 || !strings.Contains(media, "\r\n") {
		t.Fatalf("variant = %d\n%s", code, media)
	}
	lines := strings.Split(strings.ReplaceAll(media, "\r", ""), "\n")
	key := uriAttr.FindStringSubmatch(lines[1])[1]
	seg1, seg2, local := lines[3], lines[5], lines[7]
	if code, body := get(key, nil); code != 200 || body != "0123456789abcdef" {
		t.Errorf("key = %d %q", code, body)
	}
	if code, body := get(seg1, map[string]string{"Range": "bytes=0-6"}); code != 206 || body != "segment" {
		t.Errorf("ranged segment = %d %q", code, body)
	}
	for _, u := range []string{seg2, local} {
		if !strings.HasSuffix(u, "/-/blocked") {
			t.Errorf("an URI outside the policy was proxied: %s", u)
		}
		if code, _ := get(u, nil); code != 403 {
			t.Errorf("%s = %d", u, code)
		}
	}
	// A forged target outside the policy, in a valid session.
	forged := strings.Replace(seg1, strings.Split(strings.TrimPrefix(seg1, "http://"+pu.Host+"/"), "/")[1],
		b64(e.url("elsewhere.test", "/x")), 1)
	if code, _ := get(forged, nil); code != 403 {
		t.Errorf("forged target = %d", code)
	}
	// Another Host, as a rebinding page would send, and an unknown session.
	if code, _ := get(it.URL, map[string]string{"Host": "evil.test:" + pu.Port()}); code != 403 {
		t.Errorf("foreign Host = %d", code)
	}
	if code, _ := get("http://"+pu.Host+"/0000/"+b64(e.url("cdn.example.test", "/master.m3u8"))+"/x.m3u8", nil); code != 404 {
		t.Errorf("unknown session = %d", code)
	}
}

func TestHLSNotCachedOrPreloaded(t *testing.T) {
	e := newSandboxEnv(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "#EXTM3U\n") }))
	_, err := e.mgr.Episode(context.Background(), "p", "m", "e", func(context.Context, source.PlayPrefer) (*source.Playback, error) {
		return &source.Playback{Streams: []source.Stream{{URL: e.url("cdn.example.test", "/a.m3u8"), Container: source.ContainerHLS}}}, nil
	})
	if err == nil {
		t.Fatal("an HLS-only episode was cached")
	}
}

func b64(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
