package netguard

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"testing"
)

func TestBlocked(t *testing.T) {
	blocked := []string{
		"127.0.0.1", "127.8.9.10", "::1", "0.0.0.0", "::", "10.1.2.3", "172.16.0.1", "172.31.255.255",
		"192.168.1.1", "169.254.169.254", "100.64.0.1", "100.100.100.200", "224.0.0.1", "239.255.255.250",
		"255.255.255.255", "fe80::1", "fc00::1", "fd00:ec2::254", "ff02::1", "::ffff:127.0.0.1",
		"::ffff:10.0.0.1", "64:ff9b::7f00:1", "64:ff9b::a9fe:a9fe", "2002:7f00:1::1", "2002:c0a8:101::1",
		"::808:808", "::ffff:0:808:808", "168.63.129.16",
	}
	for _, s := range blocked {
		if !Blocked(netip.MustParseAddr(s)) {
			t.Errorf("%s is not blocked", s)
		}
	}
	allowed := []string{"1.1.1.1", "8.8.8.8", "172.32.0.1", "100.128.0.1", "198.18.0.1", "2606:4700::1111", "64:ff9b::808:808"}
	for _, s := range allowed {
		if Blocked(netip.MustParseAddr(s)) {
			t.Errorf("%s is blocked", s)
		}
	}
}

func TestDialRefusesNamesResolvingInside(t *testing.T) {
	g := &Guard{Lookup: func(_ context.Context, host string) ([]netip.Addr, error) {
		switch host {
		case "inside.example":
			return []netip.Addr{netip.MustParseAddr("192.168.0.10")}, nil
		case "mixed.example": // one public and one private address: refused as a whole
			return []netip.Addr{netip.MustParseAddr("1.1.1.1"), netip.MustParseAddr("10.0.0.1")}, nil
		case "metadata.example":
			return []netip.Addr{netip.MustParseAddr("169.254.169.254")}, nil
		}
		return nil, errors.New("no such host")
	}, Dial: func(context.Context, string, string) (net.Conn, error) {
		t.Fatal("dialed a refused address")
		return nil, nil
	}}
	for _, addr := range []string{"inside.example:80", "mixed.example:443", "metadata.example:80", "127.0.0.1:80", "[::1]:443", "[::ffff:127.0.0.1]:80"} {
		_, err := g.DialContext(context.Background(), "tcp", addr)
		if !errors.Is(err, ErrDenied) {
			t.Errorf("%s: %v", addr, err)
		}
		// The plugin learns that it was refused, not which address.
		if err != nil && (strings.Contains(err.Error(), "192.168") || strings.Contains(err.Error(), "10.0.0.1") || strings.Contains(err.Error(), "169.254")) {
			t.Errorf("%s: the message tells the address: %v", addr, err)
		}
	}
}

// This machine's public addresses, and the IPv6 network beside them,
// are inside too.
func TestDialRefusesLocalNetworks(t *testing.T) {
	var dialed []string
	g := &Guard{
		Lookup: func(_ context.Context, host string) ([]netip.Addr, error) {
			switch host {
			case "me.example":
				return []netip.Addr{netip.MustParseAddr("2a00:1450:4001:1::5")}, nil
			case "nas.example":
				return []netip.Addr{netip.MustParseAddr("2a00:1450:4001:1::99")}, nil
			case "me4.example":
				return []netip.Addr{netip.MustParseAddr("203.0.113.5")}, nil
			case "out.example":
				return []netip.Addr{netip.MustParseAddr("2a00:1450:4001:2::1")}, nil
			case "out4.example": // beside the IPv4 address: a proxy's fake-ip
				return []netip.Addr{netip.MustParseAddr("203.0.113.6")}, nil
			}
			return nil, errors.New("no such host")
		},
		Local: func() []netip.Prefix {
			return []netip.Prefix{netip.MustParsePrefix("2a00:1450:4001:1::/64"), netip.MustParsePrefix("203.0.113.5/32")}
		},
		Dial: func(_ context.Context, _, addr string) (net.Conn, error) {
			dialed = append(dialed, addr)
			return nil, errors.New("stop")
		},
	}
	for _, host := range []string{"me.example", "nas.example", "me4.example"} {
		if _, err := g.DialContext(context.Background(), "tcp", host+":80"); !errors.Is(err, ErrDenied) {
			t.Errorf("%s: %v", host, err)
		}
	}
	for _, host := range []string{"out.example", "out4.example"} {
		if _, err := g.DialContext(context.Background(), "tcp", host+":80"); errors.Is(err, ErrDenied) {
			t.Errorf("%s: %v", host, err)
		}
	}
	if len(dialed) != 2 {
		t.Fatalf("dialed %v", dialed)
	}
	// By default, the addresses of the machine's own interfaces.
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		t.Skip(err)
	}
	for _, a := range addrs {
		if ipn, ok := a.(*net.IPNet); ok {
			ip, _ := netip.AddrFromSlice(ipn.IP)
			if !(&Guard{}).local(ip) {
				t.Errorf("%s is not local", ip)
			}
		}
	}
}

func TestDialUsesCheckedAddress(t *testing.T) {
	var dialed string
	g := &Guard{
		Lookup: func(context.Context, string) ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
		},
		Dial: func(_ context.Context, _, addr string) (net.Conn, error) {
			dialed = addr
			return nil, errors.New("stop")
		},
	}
	g.DialContext(context.Background(), "tcp", "example.com:443")
	if dialed != "93.184.216.34:443" {
		t.Fatalf("dialed %q, not the address checked", dialed)
	}
}

func TestTransportRefusesLoopbackServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("secret")) }))
	defer srv.Close()
	c := &http.Client{Transport: (&Guard{}).Transport()}
	_, err := c.Get(srv.URL)
	if !errors.Is(err, ErrDenied) {
		t.Fatalf("loopback server reached: %v", err)
	}
	// With the server exempt, as tests do, it works.
	ap := netip.MustParseAddrPort(srv.Listener.Addr().String())
	c = &http.Client{Transport: (&Guard{Exempt: func(a netip.AddrPort) bool { return a == ap }}).Transport()}
	resp, err := c.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
}

func TestHosts(t *testing.T) {
	h := Hosts{"api.example.com", "*.cdn.example.com"}
	cases := map[string]bool{
		"api.example.com":       true,
		"API.Example.COM.":      true,
		"a.cdn.example.com":     true,
		"a.b.cdn.example.com":   true,
		"cdn.example.com":       false,
		"example.com":           false,
		"evilapi.example.com":   false,
		"api.example.com.evil":  false,
		"xcdn.example.com":      false,
		"127.0.0.1":             false,
		"":                      false,
		"ap\u0130.example.com":  false, // İ lowers to i, but IDNA maps it to i̇
		"api.example.com\u00a0": false,
	}
	for host, want := range cases {
		if got := h.Allows(host); got != want {
			t.Errorf("Allows(%q) = %v", host, got)
		}
	}
	for raw, want := range map[string]bool{
		"https://api.example.com/x":     true,
		"http://api.example.com:8080/x": true,
		"ftp://api.example.com/":        false,
		"https://u:p@api.example.com/":  false,
		"https://api.example.com.evil/": false,
	} {
		u, _ := url.Parse(raw)
		if got := h.AllowsURL(u); got != want {
			t.Errorf("AllowsURL(%q) = %v", raw, got)
		}
	}
}

func TestValidPattern(t *testing.T) {
	for _, p := range []string{"api.example.com", "*.example.com", "*.cdn.example.com", "example.test", "github.io", "a-b.example.co.uk",
		"xn--bcher-kva.example", "0x08.example.com", "1.2.3.example"} {
		if err := ValidPattern(p); err != nil {
			t.Errorf("%q: %v", p, err)
		}
	}
	for _, p := range []string{"", "*", "*.com", "com", "*.co.uk", "co.uk", "*.github.io", "localhost", "a.localhost",
		"127.0.0.1", "::1", "[::1]", "example.com:443", "https://example.com", "Example.com", " example.com",
		"*.*.example.com", "exa mple.com", "-a.example.com", "example",
		"8.526344", "0x08.0x08.0x08.0x08", "1.2.3.0x4", "010.010.010.010", "a.0x", "b\u00fccher.example", "cdn.b\u0130g.example"} {
		if err := ValidPattern(p); err == nil {
			t.Errorf("%q accepted", p)
		}
	}
}

func TestCheckRedirect(t *testing.T) {
	allow := Hosts{"a.example.com", "b.example.com"}.AllowsURL
	check := CheckRedirect(allow)
	mk := func(raw string) *http.Request {
		r, _ := http.NewRequest("GET", raw, nil)
		r.Header.Set("Authorization", "Bearer x")
		r.Header.Set("Cookie", "c=1")
		return r
	}
	first := mk("https://a.example.com/1")
	same := mk("https://a.example.com/2")
	if err := check(same, []*http.Request{first}); err != nil || same.Header.Get("Authorization") == "" {
		t.Fatalf("same host: %v %v", err, same.Header)
	}
	other := mk("https://b.example.com/2")
	if err := check(other, []*http.Request{first}); err != nil || other.Header.Get("Authorization") != "" || other.Header.Get("Cookie") != "" {
		t.Fatalf("other host kept credentials: %v %v", err, other.Header)
	}
	// Back to the first host after leaving it: still dropped.
	back := mk("https://a.example.com/3")
	if err := check(back, []*http.Request{first, other}); err != nil || back.Header.Get("Authorization") != "" {
		t.Fatalf("back on the first host after another: %v %v", err, back.Header)
	}
	// To http on the same host: dropped, as they would go in the clear.
	plain := mk("http://a.example.com/2")
	if err := check(plain, []*http.Request{first}); err != nil || plain.Header.Get("Authorization") != "" || plain.Header.Get("Cookie") != "" {
		t.Fatalf("https to http kept credentials: %v %v", err, plain.Header)
	}
	again := mk("https://a.example.com/3")
	if err := check(again, []*http.Request{first, plain}); err != nil || again.Header.Get("Authorization") != "" {
		t.Fatalf("back to https after http kept credentials: %v %v", err, again.Header)
	}
	upgraded := mk("https://a.example.com/2")
	if err := check(upgraded, []*http.Request{mk("http://a.example.com/1")}); err != nil || upgraded.Header.Get("Authorization") == "" {
		t.Fatalf("http to https: %v %v", err, upgraded.Header)
	}
	if err := check(mk("https://c.example.com/"), []*http.Request{first}); !errors.Is(err, ErrDenied) {
		t.Fatalf("undeclared host: %v", err)
	}
	if err := check(mk("file:///etc/passwd"), []*http.Request{first}); !errors.Is(err, ErrDenied) {
		t.Fatalf("file URL: %v", err)
	}
	via := make([]*http.Request, MaxRedirects)
	for i := range via {
		via[i] = first
	}
	if err := check(mk("https://a.example.com/x"), via); !errors.Is(err, ErrDenied) {
		t.Fatalf("redirect limit: %v", err)
	}
}
