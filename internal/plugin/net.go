package plugin

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"sync/atomic"

	"golang.org/x/net/publicsuffix"

	"github.com/giuem/ichiwa/internal/netguard"
)

func newJar() *cookiejar.Jar {
	jar, _ := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	return jar
}

// callTransport sends one call's fetches: only to the plugin's hosts,
// through the guard's transport, with the plugin's cookies (§11). It
// notes a network failure, so that a call that fails after one is
// reported as a network error rather than the plugin's.
type callTransport struct {
	hosts  netguard.Hosts
	jar    *cookiejar.Jar
	base   http.RoundTripper
	failed *atomic.Bool
	logs   *Logs
}

func (t *callTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if !t.hosts.AllowsURL(req.URL) {
		return nil, fmt.Errorf("%w: 插件没有声明 %s", netguard.ErrDenied, req.URL.Hostname())
	}
	orig := req
	if ownsCookies(req) {
		// The plugin manages its cookies, even with an empty header,
		// which goes unsent (§11).
		if v := req.Header.Values("Cookie"); len(v) > 0 && strings.Join(v, "") == "" {
			req = req.Clone(req.Context())
			req.Header.Del("Cookie")
		}
	} else if cookies := t.jar.Cookies(req.URL); len(cookies) > 0 {
		req = req.Clone(req.Context())
		for _, c := range cookies {
			req.AddCookie(c)
		}
	}
	resp, err := t.base.RoundTrip(req)
	if resp != nil {
		// A redirect's request links back to this one as the plugin
		// made it, for ownsCookies.
		resp.Request = orig
	}
	if err != nil {
		if errors.Is(err, netguard.ErrDenied) {
			t.logs.Hostf("拒绝请求 %s：%v", netguard.Redact(req.URL), err)
		} else if req.Context().Err() == nil {
			t.failed.Store(true)
		}
		return nil, err
	}
	if rc := resp.Cookies(); len(rc) > 0 {
		t.jar.SetCookies(req.URL, rc)
	}
	return resp, nil
}

// ownsCookies tells whether the plugin set a Cookie header, even an empty
// one, on the fetch req comes from: on req itself, or, for a redirect, on
// the first request, as the header goes on a redirect to another host.
func ownsCookies(req *http.Request) bool {
	first := req
	for first.Response != nil && first.Response.Request != nil {
		first = first.Response.Request
	}
	_, own := first.Header["Cookie"]
	return own
}

// allowRequest is webapi's Allow hook: every request and redirect must go
// to a declared host over http or https, and a Host header the plugin set
// must name the same host.
func allowRequest(hosts netguard.Hosts, logs *Logs) func(*http.Request) error {
	return func(req *http.Request) error {
		if !hosts.AllowsURL(req.URL) {
			logs.Hostf("拒绝请求 %s：插件没有声明这个地址", netguard.Redact(req.URL))
			return fmt.Errorf("%w: 插件没有声明 %s", netguard.ErrDenied, req.URL.Hostname())
		}
		if req.Host != "" && !strings.EqualFold(req.Host, req.URL.Host) {
			return fmt.Errorf("%w: Host 请求头和地址不一致", netguard.ErrDenied)
		}
		return nil
	}
}

func (p *Plugin) client(hosts netguard.Hosts, failed *atomic.Bool) *http.Client {
	return &http.Client{
		Transport: &callTransport{
			hosts:  hosts,
			jar:    p.jar,
			base:   p.transport,
			failed: failed,
			logs:   p.logs,
		},
		CheckRedirect: netguard.CheckRedirect(hosts.AllowsURL),
	}
}

// ImageClient downloads the plugin's images: its hosts only, every
// redirect checked, through the guard, without its cookies (§11).
func (p *Plugin) ImageClient() *http.Client {
	hosts := p.Manifest().Hosts()
	return &http.Client{
		Transport:     p.transport,
		CheckRedirect: netguard.CheckRedirect(hosts.AllowsURL),
	}
}
