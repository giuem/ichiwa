package media

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/giuem/ichiwa/internal/netguard"
)

// The HLS loopback proxy (§7): the player opens a playlist on 127.0.0.1,
// and every request it makes from there (variant playlists, segments,
// AES-128 keys) comes back to the proxy, which adds the stream's headers
// and fetches it under the source's policy. A playlist is rewritten so
// that each URI in it points at the proxy again, so the player never
// reaches the network, or this machine's files, by itself.

const (
	maxPlaylistBytes = 4 << 20
	hlsSessionTTL    = 12 * time.Hour
)

type hlsSession struct {
	policy  Policy
	headers map[string]string
	client  *http.Client
	created time.Time
}

type hlsProxy struct {
	addr string // 127.0.0.1:port
	srv  *http.Server

	mu       sync.Mutex
	sessions map[string]*hlsSession
}

func startHLSProxy() (*hlsProxy, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	p := &hlsProxy{addr: ln.Addr().String(), sessions: map[string]*hlsSession{}}
	p.srv = &http.Server{Handler: p, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = p.srv.Serve(ln) }()
	return p, nil
}

func (p *hlsProxy) add(s *hlsSession) string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	token := hex.EncodeToString(b[:])
	p.mu.Lock()
	defer p.mu.Unlock()
	for k, old := range p.sessions {
		if time.Since(old.created) > hlsSessionTTL {
			delete(p.sessions, k)
		}
	}
	p.sessions[token] = s
	return token
}

func (p *hlsProxy) session(token string) *hlsSession {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.sessions[token]
}

// Kinds of proxied URL: a playlist the player parses, or anything else
// (a segment, a key, a map). The proxy answers a playlist only whole,
// with 200, and rewritten.
const (
	kindPlaylist = "p"
	kindOther    = "s"
)

// url returns the proxy's URL for target of kind in the session token.
// The last segment keeps the target's file name, whose extension players
// look at.
func (p *hlsProxy) url(token, kind string, target *url.URL) string {
	name := path.Base(target.Path)
	if name == "." || name == "/" || name == "" {
		name = "index"
	}
	return "http://" + p.addr + "/" + token + "/" + kind + "/" + base64.RawURLEncoding.EncodeToString([]byte(target.String())) + "/" + url.PathEscape(name)
}

// blocked is the URL a playlist gets in place of a URI the proxy will
// not fetch.
func (p *hlsProxy) blocked(token string) string {
	return "http://" + p.addr + "/" + token + "/-/-/blocked"
}

// serveHLS opens a proxy session for the stream at rawURL and checks that
// its playlist loads, so a dead stream falls back to the next one.
func (m *Manager) serveHLS(ctx context.Context, rawURL string, headers map[string]string, policy Policy) (Item, error) {
	u, err := url.Parse(rawURL)
	if err != nil || !policy.allows(u) {
		return Item{}, fmt.Errorf("%w: 媒体地址不在片源声明的范围内", netguard.ErrDenied)
	}
	m.mu.Lock()
	if m.hls == nil {
		if m.hls, err = startHLSProxy(); err != nil {
			m.mu.Unlock()
			return Item{}, err
		}
	}
	proxy := m.hls
	m.mu.Unlock()
	s := &hlsSession{policy: policy, headers: headers, client: m.client(policy), created: time.Now()}
	resp, err := s.fetch(ctx, u, "")
	if err != nil {
		return Item{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Item{}, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if ok, _ := isPlaylist(resp, bufio.NewReader(resp.Body)); !ok {
		return Item{}, errors.New("不是 HLS 播放列表")
	}
	token := proxy.add(s)
	return Item{URL: proxy.url(token, kindPlaylist, u)}, nil
}

func (s *hlsSession) fetch(ctx context.Context, u *url.URL, rangeHeader string) (*http.Response, error) {
	if !s.policy.allows(u) {
		return nil, fmt.Errorf("%w: %s 不在片源声明的范围内", netguard.ErrDenied, u.Hostname())
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	for k, v := range s.headers {
		req.Header.Set(k, v)
	}
	if rangeHeader != "" {
		req.Header.Set("Range", rangeHeader)
	}
	return s.client.Do(req)
}

// sniffBytes is how far into a body isPlaylist looks for #EXTM3U, past a
// byte order mark and blank space.
const sniffBytes = 1024

func isPlaylist(resp *http.Response, br *bufio.Reader) (bool, *bufio.Reader) {
	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	if strings.Contains(ct, "mpegurl") {
		return true, br
	}
	head, _ := br.Peek(sniffBytes)
	head = bytes.TrimPrefix(head, []byte("\xef\xbb\xbf"))
	return bytes.HasPrefix(bytes.TrimLeft(head, " \t\r\n"), []byte("#EXTM3U")), br
}

func (p *hlsProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Only the player on this machine, by this address: a page in a
	// browser that rebinds a name to 127.0.0.1 sends another Host.
	if r.Host != p.addr {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/"), "/", 4)
	if len(parts) != 4 {
		http.NotFound(w, r)
		return
	}
	token, kind := parts[0], parts[1]
	s := p.session(token)
	if s == nil {
		http.NotFound(w, r)
		return
	}
	if kind != kindPlaylist && kind != kindOther {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	target, err := url.Parse(string(raw))
	if err != nil || !s.policy.allows(target) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	// A playlist is rewritten whole, so a range of it is never asked for:
	// players ask for "bytes=0-" now and then.
	rng := r.Header.Get("Range")
	if kind == kindPlaylist {
		rng = ""
	}
	fail := func(err error) {
		if errors.Is(err, netguard.ErrDenied) {
			http.Error(w, "forbidden", http.StatusForbidden)
		} else {
			http.Error(w, "bad gateway", http.StatusBadGateway)
		}
	}
	resp, err := s.fetch(r.Context(), target, rng)
	if err != nil {
		fail(err)
		return
	}
	defer resp.Body.Close()
	br := bufio.NewReader(resp.Body)
	// What the player takes for a playlist, by how it asked or by what
	// came back, goes out only rewritten: anything else (a partial
	// answer, an error page, a body that does not parse) would reach the
	// player as the server wrote it.
	if looks, _ := isPlaylist(resp, br); kind == kindPlaylist || looks {
		if resp.StatusCode != http.StatusOK || !looks {
			http.Error(w, "bad gateway", http.StatusBadGateway)
			return
		}
		body, err := io.ReadAll(io.LimitReader(br, maxPlaylistBytes+1))
		if err != nil || len(body) > maxPlaylistBytes {
			http.Error(w, "bad gateway", http.StatusBadGateway)
			return
		}
		out, err := rewritePlaylist(body, resp.Request.URL, func(abs *url.URL, kind string) string {
			if abs == nil || !s.policy.allows(abs) {
				return p.blocked(token)
			}
			return p.url(token, kind, abs)
		})
		if err != nil {
			http.Error(w, "bad gateway", http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		if r.Method == http.MethodGet {
			_, _ = w.Write(out)
		}
		return
	}
	for _, k := range []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges", "Last-Modified", "Etag"} {
		if v := resp.Header.Get(k); v != "" {
			w.Header().Set(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	if r.Method == http.MethodGet {
		_, _ = io.Copy(w, br)
	}
}
