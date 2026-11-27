package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/giuem/ichiwa/internal/netguard"
	"github.com/giuem/ichiwa/internal/source"
)

// netEnv runs the network plugin against two local servers. The names of
// its manifest resolve to loopback through a fake resolver; only the
// address of the good server is exempt from the guard, as a public
// server would be. The evil server stands for the machine's own
// services, which a name the plugin controls points at.
type netEnv struct {
	p          *Plugin
	good, evil *httptest.Server
	goodPort   string
	evilPort   string
	lookups    atomic.Int32
}

func newNetEnv(t *testing.T) *netEnv {
	t.Helper()
	e := &netEnv{}
	mux := http.NewServeMux()
	mux.HandleFunc("/echo", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "host=%s auth=%s cookie=%s", r.Host, r.Header.Get("Authorization"), r.Header.Get("Cookie"))
	})
	mux.HandleFunc("/cookies", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%q", r.Header["Cookie"])
	})
	mux.HandleFunc("/set", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "sid", Value: "s3cret", Path: "/"})
		fmt.Fprint(w, "set")
	})
	mux.HandleFunc("/to", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, r.URL.Query().Get("u"), http.StatusFound)
	})
	mux.HandleFunc("/loop", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/loop", http.StatusFound)
	})
	e.good = httptest.NewServer(mux)
	t.Cleanup(e.good.Close)
	e.evil = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "internal secrets")
	}))
	t.Cleanup(e.evil.Close)
	_, e.goodPort, _ = net.SplitHostPort(e.good.Listener.Addr().String())
	_, e.evilPort, _ = net.SplitHostPort(e.evil.Listener.Addr().String())
	goodAddr := netip.MustParseAddrPort(e.good.Listener.Addr().String())
	guard := &netguard.Guard{
		Lookup: func(ctx context.Context, host string) ([]netip.Addr, error) {
			e.lookups.Add(1)
			switch {
			case host == "api.example.test", host == "evil.example.test", strings.HasSuffix(host, ".cdn.example.test"):
				return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
			}
			return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
		},
		Exempt: func(ap netip.AddrPort) bool { return ap == goodAddr },
	}
	e.p = open(t, "hostile/network", Options{Guard: guard, Timeout: 5 * time.Second})
	return e
}

func (e *netEnv) fetch(t *testing.T, url string, init map[string]any) (string, error) {
	t.Helper()
	req := source.CollectionRequest{ID: url}
	if init != nil {
		b, _ := json.Marshal(init)
		req.Filters = map[string]string{"init": string(b)}
	}
	page, err := e.p.Collection(context.Background(), req)
	if err != nil {
		return "", err
	}
	return page.Title, nil
}

func (e *netEnv) api(path string) string {
	return "http://api.example.test:" + e.goodPort + path
}

func TestNetworkAllowedHost(t *testing.T) {
	e := newNetEnv(t)
	body, err := e.fetch(t, e.api("/echo"), nil)
	if err != nil || !strings.HasPrefix(body, "host=api.example.test:") {
		t.Fatalf("fetch of a declared host = %q, %v", body, err)
	}
}

func TestNetworkUndeclaredHostAndIPs(t *testing.T) {
	e := newNetEnv(t)
	for _, u := range []string{
		"http://other.test:" + e.goodPort + "/echo",       // not declared
		"http://127.0.0.1:" + e.goodPort + "/echo",        // an IP: never declared
		"http://[::1]:" + e.goodPort + "/echo",            // IPv6 loopback
		"http://169.254.169.254/latest/meta-data/",        // cloud metadata
		"http://10.0.0.1/",                                // private network
		"http://localhost:" + e.goodPort + "/echo",        // localhost
		"file:///etc/passwd",                              // not http
		"http://user:pw@api.example.test:" + e.goodPort,   // credentials in the URL
		"http://cdn.example.test:" + e.goodPort + "/echo", // the wildcard's own apex is not declared
	} {
		body, err := e.fetch(t, u, nil)
		if err == nil {
			t.Errorf("fetch %s was allowed: %q", u, body)
		}
	}
}

func TestNetworkNameResolvingToLoopback(t *testing.T) {
	e := newNetEnv(t)
	body, err := e.fetch(t, "http://evil.example.test:"+e.evilPort+"/", nil)
	if err == nil || strings.Contains(body, "secrets") {
		t.Fatalf("a declared name resolving to loopback was reached: %q, %v", body, err)
	}
	if code(err) != source.ErrPlugin {
		t.Errorf("a refused address is the plugin's error, not the network's: %v", err)
	}
	if !strings.Contains(e.p.Logs().Text(), "内网") {
		t.Errorf("the log does not tell the refusal:\n%s", e.p.Logs().Text())
	}
}

func TestNetworkHostHeaderSpoof(t *testing.T) {
	e := newNetEnv(t)
	body, err := e.fetch(t, e.api("/echo"), map[string]any{"headers": map[string]string{"Host": "evil.example.test"}})
	if err == nil && strings.Contains(body, "host=evil") {
		t.Fatalf("a Host header other than the URL's was sent: %q", body)
	}
}

func TestRedirectToUndeclaredHost(t *testing.T) {
	e := newNetEnv(t)
	for _, target := range []string{
		"http://other.test:" + e.goodPort + "/echo",
		"http://127.0.0.1:" + e.evilPort + "/",
		"http://evil.example.test:" + e.evilPort + "/",
	} {
		body, err := e.fetch(t, e.api("/to?u="+target), nil)
		if err == nil {
			t.Errorf("redirect to %s followed: %q", target, body)
		}
	}
}

func TestRedirectLimit(t *testing.T) {
	e := newNetEnv(t)
	if body, err := e.fetch(t, e.api("/loop"), nil); err == nil {
		t.Fatalf("endless redirects followed: %q", body)
	}
}

func TestRedirectAcrossHostsDropsCredentials(t *testing.T) {
	e := newNetEnv(t)
	init := map[string]any{"headers": map[string]string{"Authorization": "Bearer t0ken", "Cookie": "mine=1"}}
	// Same host: the credentials stay.
	body, err := e.fetch(t, e.api("/to?u=/echo"), init)
	if err != nil || !strings.Contains(body, "auth=Bearer t0ken") || !strings.Contains(body, "cookie=mine=1") {
		t.Fatalf("same-host redirect = %q, %v", body, err)
	}
	// Another declared host: they go.
	other := "http://img.cdn.example.test:" + e.goodPort + "/echo"
	body, err = e.fetch(t, e.api("/to?u="+other), init)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(body, "host=img.cdn.example.test") || strings.Contains(body, "t0ken") || strings.Contains(body, "mine=1") {
		t.Fatalf("cross-host redirect kept credentials: %q", body)
	}
}

func TestCookies(t *testing.T) {
	e := newNetEnv(t)
	if _, err := e.fetch(t, e.api("/set"), nil); err != nil {
		t.Fatal(err)
	}
	// The plugin's jar keeps the cookie for later calls to that host.
	body, err := e.fetch(t, e.api("/echo"), nil)
	if err != nil || !strings.Contains(body, "cookie=sid=s3cret") {
		t.Fatalf("stored cookie not sent: %q, %v", body, err)
	}
	// Not to another host.
	body, err = e.fetch(t, "http://img.cdn.example.test:"+e.goodPort+"/echo", nil)
	if err != nil || strings.Contains(body, "s3cret") {
		t.Fatalf("cookie sent to another host: %q, %v", body, err)
	}
	// A request that sets its own Cookie gets nothing added.
	body, err = e.fetch(t, e.api("/echo"), map[string]any{"headers": map[string]string{"Cookie": "own=1"}})
	if err != nil || !strings.HasSuffix(body, "cookie=own=1") {
		t.Fatalf("own Cookie header = %q, %v", body, err)
	}
	// An empty Cookie header counts as the plugin's own too, and is not
	// sent.
	body, err = e.fetch(t, e.api("/cookies"), map[string]any{"headers": map[string]string{"Cookie": ""}})
	if err != nil || body != "[]" {
		t.Fatalf("empty Cookie header = %q, %v", body, err)
	}
	body, err = e.fetch(t, "http://none.cdn.example.test:"+e.goodPort+"/cookies", map[string]any{"headers": map[string]string{"Cookie": ""}})
	if err != nil || body != "[]" {
		t.Fatalf("empty Cookie header, none stored = %q, %v", body, err)
	}
	// The plugin's own Cookie header goes on a redirect to another host,
	// and the stored cookies of that host are not added in its place.
	other := "http://img.cdn.example.test:" + e.goodPort
	if _, err := e.fetch(t, other+"/set", nil); err != nil {
		t.Fatal(err)
	}
	for _, c := range []string{"mine=1", ""} {
		body, err = e.fetch(t, e.api("/to?u="+other+"/cookies"), map[string]any{"headers": map[string]string{"Cookie": c}})
		if err != nil || body != "[]" {
			t.Fatalf("redirect with Cookie %q = %q, %v", c, body, err)
		}
	}
	// Without a header of its own, the stored ones are sent after a
	// redirect.
	body, err = e.fetch(t, e.api("/to?u="+other+"/cookies"), nil)
	if err != nil || body != `["sid=s3cret"]` {
		t.Fatalf("redirect without a Cookie header = %q, %v", body, err)
	}

	// Cookies live in memory per plugin: another plugin has none.
	otherPlugin := open(t, "hostile/network", Options{Guard: e.p.opts.Guard})
	page, err := otherPlugin.Collection(context.Background(), source.CollectionRequest{ID: e.api("/echo")})
	if err != nil || strings.Contains(page.Title, "s3cret") {
		t.Fatalf("cookie shared between plugins: %+v, %v", page, err)
	}
}

func TestNetworkFailureIsNetworkError(t *testing.T) {
	e := newNetEnv(t)
	addr := e.good.Listener.Addr().String()
	e.good.Close()
	_, err := e.fetch(t, "http://api.example.test:"+strings.Split(addr, ":")[1]+"/echo", nil)
	if code(err) != source.ErrNetwork {
		t.Fatalf("a refused connection: code %q, %v", code(err), err)
	}
}

func TestConcurrencyLimit(t *testing.T) {
	var mu sync.Mutex
	var now, peak int
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		now++
		peak = max(peak, now)
		mu.Unlock()
		<-release
		mu.Lock()
		now--
		mu.Unlock()
		fmt.Fprint(w, "ok")
	}))
	defer srv.Close()
	addr := netip.MustParseAddrPort(srv.Listener.Addr().String())
	guard := &netguard.Guard{
		Lookup: func(context.Context, string) ([]netip.Addr, error) { return []netip.Addr{addr.Addr()}, nil },
		Exempt: func(ap netip.AddrPort) bool { return ap == addr },
	}
	p := open(t, "hostile/network", Options{Guard: guard})
	url := fmt.Sprintf("http://api.example.test:%d/", addr.Port())
	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() {
			if _, err := p.Collection(context.Background(), source.CollectionRequest{ID: url}); err != nil {
				t.Error(err)
			}
		})
	}
	time.Sleep(300 * time.Millisecond)
	mu.Lock()
	got := peak
	mu.Unlock()
	close(release)
	wg.Wait()
	if got != MaxConcurrent {
		t.Fatalf("%d calls ran at once, want %d", got, MaxConcurrent)
	}
}

func TestEndlessLoopIsInterrupted(t *testing.T) {
	p := open(t, "hostile/loop", Options{Timeout: 300 * time.Millisecond})
	start := time.Now()
	_, err := p.Collection(context.Background(), source.CollectionRequest{ID: "x"})
	if code(err) != source.ErrPlugin || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("endless loop: %v", err)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("interrupt took %v", d)
	}
}

func TestMemoryLimit(t *testing.T) {
	p := open(t, "hostile/memory", Options{Timeout: 20 * time.Second})
	_, err := p.Collection(context.Background(), source.CollectionRequest{ID: "x"})
	if code(err) != source.ErrPlugin || source.Message(err) != "片源占用内存过多" {
		t.Fatalf("memory blow-up: %v", err)
	}
	// Strings the host makes count too.
	for _, id := range []string{"hex", "base64"} {
		_, err := p.Collection(context.Background(), source.CollectionRequest{ID: id})
		if code(err) != source.ErrPlugin || source.Message(err) != "片源占用内存过多" {
			t.Errorf("%s: %v", id, err)
		}
	}
}

func TestDynamicCodeRefused(t *testing.T) {
	p := open(t, "hostile/eval", Options{})
	for _, id := range []string{"eval", "function", "indirect", "async-function", "import"} {
		page, err := p.Collection(context.Background(), source.CollectionRequest{ID: id})
		if err == nil {
			t.Errorf("%s ran: %+v", id, page)
		} else if code(err) != source.ErrPlugin {
			t.Errorf("%s: %v", id, err)
		}
	}
	if page, err := p.Collection(context.Background(), source.CollectionRequest{ID: "none"}); err != nil || page.Title != "none" {
		t.Fatalf("the plugin itself runs: %+v, %v", page, err)
	}
}

func TestOversizedResult(t *testing.T) {
	p := open(t, "hostile/bigjson", Options{})
	_, err := p.Collection(context.Background(), source.CollectionRequest{ID: "x"})
	if code(err) != source.ErrPlugin || source.Message(err) != "片源返回数据过大" {
		t.Fatalf("5 MiB result: %v", err)
	}
}

func TestDirtyResult(t *testing.T) {
	p := open(t, "hostile/dirty", Options{})
	page, err := p.Collection(context.Background(), source.CollectionRequest{ID: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if page.Title != "脏数据" || page.Layout != source.LayoutGrid || page.Cursor != "" {
		t.Errorf("page = %+v", page)
	}
	if len(page.Items) != 2 {
		t.Fatalf("items = %+v", page.Items)
	}
	first, second := page.Items[0], page.Items[1]
	if first.ID != "7" || first.Title != "数字 ID" || first.Image != "" || len(first.Badges) != 2 || first.Badges[0] != "一二三四五六七八" {
		t.Errorf("first = %+v", first)
	}
	if second.ID != "ok" || second.Title != "好的" || second.Image != "" || second.Shape != source.ShapePortrait || second.Note != "5" {
		t.Errorf("second = %+v", second)
	}
}

func TestStorageQuota(t *testing.T) {
	p := open(t, "hostile/storage", Options{})
	if _, err := p.Collection(context.Background(), source.CollectionRequest{ID: "x"}); code(err) != source.ErrPlugin {
		t.Fatalf("an uncaught quota error: %v", err)
	}
	page, err := p.Collection(context.Background(), source.CollectionRequest{ID: "catch"})
	if err != nil || !strings.HasPrefix(page.Title, "caught: host.storage 超出") {
		t.Fatalf("a caught quota error: %+v, %v", page, err)
	}
	if _, ok := p.storage.Get("big"); ok {
		t.Fatal("the value past the quota was kept")
	}
}

func TestStoragePersistsPerPlugin(t *testing.T) {
	dir := t.TempDir()
	p := open(t, "hostapi", Options{DataDir: dir})
	call := func(p *Plugin) map[string]any {
		raw, err := p.Call(context.Background(), "collection", map[string]string{"id": "storage"})
		if err != nil {
			t.Fatal(err)
		}
		var v map[string]any
		_ = json.Unmarshal(raw, &v)
		return v
	}
	if v := call(p); v["before"] != nil || v["after"] != "v1" || v["missing"] != true {
		t.Fatalf("first = %v", v)
	}
	// A new instance reads what the first saved.
	if v := call(open(t, "hostapi", Options{DataDir: dir})); v["before"] != "v1" || v["after"] != "v2" {
		t.Fatalf("after reopening = %v", v)
	}
	// Another plugin's storage is its own.
	if _, ok := open(t, "example", Options{DataDir: dir}).storage.Get("k"); ok {
		t.Fatal("storage shared between plugins")
	}
}
