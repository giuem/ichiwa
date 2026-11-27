// Package netguard keeps the network requests made for a source plugin
// inside what the plugin declared (docs/plugin-design.md §11): only the
// hosts of its permissions, every redirect checked again, credentials
// dropped when a redirect leaves the host, and no connection to an
// address of this machine or its network, whatever a name resolves to.
//
// The plugin runtime, the media downloads and the HLS proxy share it, so
// that a plugin cannot reach through one what the others refuse.
package netguard

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/publicsuffix"
)

// MaxRedirects is the most redirects one request follows.
const MaxRedirects = 10

// ErrDenied is wrapped by every error of a request the guard refused, so
// that callers can tell a refusal from a network failure.
var ErrDenied = errors.New("netguard: denied")

func denied(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrDenied, fmt.Sprintf(format, args...))
}

// Hosts is a plugin's list of allowed hosts: exact names, and
// "*.example.com" for every subdomain of example.com (not example.com
// itself).
type Hosts []string

// Allows reports whether host, a hostname without port, is in the list.
// An IP address is never allowed: a manifest cannot name one.
func (h Hosts) Allows(host string) bool {
	host = normalizeHost(host)
	if host == "" {
		return false
	}
	if _, err := netip.ParseAddr(host); err == nil {
		return false
	}
	for _, p := range h {
		p = normalizeHost(p)
		if rest, ok := strings.CutPrefix(p, "*."); ok {
			if strings.HasSuffix(host, "."+rest) {
				return true
			}
			continue
		}
		if host == p {
			return true
		}
	}
	return false
}

// AllowsURL reports whether u is an http or https URL without user info
// whose host is in the list.
func (h Hosts) AllowsURL(u *url.URL) bool {
	if u == nil || u.User != nil {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	return h.Allows(u.Hostname())
}

func normalizeHost(h string) string {
	h = strings.ToLower(strings.TrimSpace(h))
	h = strings.TrimSuffix(h, ".")
	h = strings.TrimSuffix(strings.TrimPrefix(h, "["), "]")
	return h
}

// ValidPattern checks one entry of permissions.network (§4): an exact
// domain name, or "*." and a domain under a registrable domain. IP
// addresses, localhost, ports and public suffixes are refused.
func ValidPattern(p string) error {
	raw := p
	p = strings.ToLower(strings.TrimSpace(p))
	if p == "" {
		return errors.New("空的主机名")
	}
	if p != raw {
		return fmt.Errorf("主机名 %q 要用小写、不带空白", raw)
	}
	host, wildcard := strings.CutPrefix(p, "*.")
	if strings.ContainsAny(host, ":/[]@*") {
		return fmt.Errorf("主机名 %q 不能带端口、路径或通配符", raw)
	}
	if _, err := netip.ParseAddr(host); err == nil {
		return fmt.Errorf("不能写 IP 地址 %q", raw)
	}
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return fmt.Errorf("不能写 %q", raw)
	}
	if !validDomain(host) {
		return fmt.Errorf("%q 不是有效的域名", raw)
	}
	suffix, icann := publicsuffix.PublicSuffix(host)
	if suffix == host && (icann || !strings.Contains(host, ".")) {
		return fmt.Errorf("%q 是公共后缀，不能整个放行", raw)
	}
	if wildcard {
		if _, err := publicsuffix.EffectiveTLDPlusOne(host); err != nil {
			return fmt.Errorf("通配符 %q 必须是可注册域名的子域", raw)
		}
	}
	return nil
}

func validDomain(h string) bool {
	if len(h) > 253 || !strings.Contains(h, ".") {
		return false
	}
	for _, label := range strings.Split(h, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for i := 0; i < len(label); i++ {
			c := label[i]
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
				return false
			}
		}
	}
	return true
}

// blockedPrefixes are the networks no plugin may connect to: this machine,
// private and link-local networks, CGNAT, multicast, the unspecified and
// reserved ranges, and the cloud metadata addresses within them.
var blockedPrefixes = func() []netip.Prefix {
	var out []netip.Prefix
	for _, s := range []string{
		"0.0.0.0/8",      // "this network", the unspecified address
		"10.0.0.0/8",     // private
		"100.64.0.0/10",  // CGNAT, Alibaba Cloud's metadata 100.100.100.200
		"127.0.0.0/8",    // loopback
		"169.254.0.0/16", // link-local, the metadata of most clouds
		"172.16.0.0/12",  // private
		"192.0.0.0/24",   // IETF protocol assignments
		"192.168.0.0/16", // private
		"224.0.0.0/4",    // multicast
		"240.0.0.0/4",    // reserved, broadcast
		"::/128",         // unspecified
		"::1/128",        // loopback
		"fc00::/7",       // unique local, AWS's metadata fd00:ec2::254
		"fe80::/10",      // link-local
		"fec0::/10",      // site-local, deprecated
		"ff00::/8",       // multicast
		"100::/64",       // discard
		"2001:db8::/32",  // documentation
		"::ffff:0:0/96",  // IPv4-mapped, checked again unmapped
		"64:ff9b:1::/48", // local-use NAT64
	} {
		out = append(out, netip.MustParsePrefix(s))
	}
	return out
}()

var (
	nat64     = netip.MustParsePrefix("64:ff9b::/96")
	sixToFour = netip.MustParsePrefix("2002::/16")
)

// Blocked reports whether a plugin must not connect to ip.
func Blocked(ip netip.Addr) bool {
	if !ip.IsValid() {
		return true
	}
	ip = ip.Unmap()
	if ip.Is4() {
		return blockedIn(ip)
	}
	// An IPv6 address that carries an IPv4 one reaches that one.
	switch {
	case nat64.Contains(ip):
		b := ip.As16()
		return Blocked(netip.AddrFrom4([4]byte{b[12], b[13], b[14], b[15]}))
	case sixToFour.Contains(ip):
		b := ip.As16()
		return Blocked(netip.AddrFrom4([4]byte{b[2], b[3], b[4], b[5]}))
	}
	return blockedIn(ip)
}

func blockedIn(ip netip.Addr) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
		return true
	}
	for _, p := range blockedPrefixes {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

// Guard dials only addresses that are not Blocked. The zero Guard uses
// the system resolver.
type Guard struct {
	// Lookup resolves a host name; nil means net.DefaultResolver.
	Lookup func(ctx context.Context, host string) ([]netip.Addr, error)
	// Exempt lets tests reach their own servers on loopback: an address
	// it returns true for is dialed even when Blocked. Never set outside
	// tests.
	Exempt func(netip.AddrPort) bool
	// Dial dials one checked address; nil means a net.Dialer.
	Dial func(ctx context.Context, network, addr string) (net.Conn, error)
}

func (g *Guard) lookup(ctx context.Context, host string) ([]netip.Addr, error) {
	if g != nil && g.Lookup != nil {
		return g.Lookup(ctx, host)
	}
	return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
}

func (g *Guard) allowed(ap netip.AddrPort) bool {
	if g != nil && g.Exempt != nil && g.Exempt(ap) {
		return true
	}
	return !Blocked(ap.Addr())
}

// DialContext resolves addr and dials it, refusing when any address the
// name resolves to is Blocked: a name that points at this machine or its
// network is refused as a whole, not tried address by address. It dials
// the address it checked, so a second lookup cannot change it.
func (g *Guard) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	port, err := strconv.ParseUint(portStr, 10, 16)
	if err != nil {
		return nil, fmt.Errorf("netguard: bad port %q", portStr)
	}
	var addrs []netip.Addr
	if ip, err := netip.ParseAddr(host); err == nil {
		addrs = []netip.Addr{ip}
	} else {
		addrs, err = g.lookup(ctx, host)
		if err != nil {
			return nil, err
		}
		if len(addrs) == 0 {
			return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
		}
	}
	for _, ip := range addrs {
		if !g.allowed(netip.AddrPortFrom(ip.Unmap(), uint16(port))) {
			return nil, denied("%s 指向本机或内网地址 %s", host, ip)
		}
	}
	dial := (&net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	if g != nil && g.Dial != nil {
		dial = g.Dial
	}
	var lastErr error
	for _, ip := range addrs {
		c, err := dial(ctx, network, netip.AddrPortFrom(ip.Unmap(), uint16(port)).String())
		if err == nil {
			return c, nil
		}
		lastErr = err
		if ctx.Err() != nil {
			break
		}
	}
	return nil, lastErr
}

// Transport returns an http.Transport that dials through g. It uses no
// proxy: through one, the guard could not see where a name resolves.
func (g *Guard) Transport() *http.Transport {
	return &http.Transport{
		Proxy:                 nil,
		DialContext:           g.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          32,
		MaxIdleConnsPerHost:   4,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		ExpectContinueTimeout: time.Second,
	}
}

// sensitiveHeaders are dropped when a redirect leaves the host.
var sensitiveHeaders = []string{"Authorization", "Cookie", "Cookie2", "Proxy-Authorization", "Www-Authenticate"}

// CheckRedirect returns an http.Client CheckRedirect that follows at
// most MaxRedirects redirects, each to an http or https URL that allow
// accepts, and drops credentials once a redirect leaves the host of the
// first request.
func CheckRedirect(allow func(*url.URL) bool) func(req *http.Request, via []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if len(via) >= MaxRedirects {
			return denied("跳转超过 %d 次", MaxRedirects)
		}
		if allow != nil && !allow(req.URL) {
			return denied("跳转到未声明的地址 %s", redact(req.URL))
		}
		if len(via) > 0 && !sameHost(via[0].URL, req.URL, via) {
			StripCredentials(req.Header)
		}
		return nil
	}
}

func sameHost(first, next *url.URL, via []*http.Request) bool {
	if !strings.EqualFold(first.Host, next.Host) {
		return false
	}
	for _, r := range via {
		if !strings.EqualFold(first.Host, r.URL.Host) {
			return false
		}
	}
	return true
}

// StripCredentials drops the headers that carry credentials.
func StripCredentials(h http.Header) {
	for _, k := range sensitiveHeaders {
		h.Del(k)
	}
}

// redact drops the query of u, which may carry tokens, for messages.
func redact(u *url.URL) string {
	if u == nil {
		return ""
	}
	c := *u
	c.RawQuery, c.Fragment, c.User = "", "", nil
	return c.String()
}

// Redact is redact for other packages' messages.
func Redact(u *url.URL) string { return redact(u) }
