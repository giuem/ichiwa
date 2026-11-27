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
	if req.Header.Get("Cookie") == "" {
		if cookies := t.jar.Cookies(req.URL); len(cookies) > 0 {
			req = req.Clone(req.Context())
			for _, c := range cookies {
				req.AddCookie(c)
			}
		}
	}
	resp, err := t.base.RoundTrip(req)
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
