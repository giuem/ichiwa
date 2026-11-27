package media

import (
	"errors"
	"net/url"
	"strings"
)

// The playlist rewriter. A player and the proxy must read a playlist the
// same way, or a URI the proxy takes for a comment reaches the player
// untouched. So the playlist is parsed line by line, ending lines the
// ways players do (CR LF, CR, LF), and written out again from what was
// understood: only known tags, their attributes parsed and every
// attribute that carries an address pointed at the proxy, lines ended
// with LF. What is not understood is left out.

var errPlaylist = errors.New("播放列表格式不对")

// Tags written through as they are: none carries an address.
var plainTags = map[string]bool{
	"EXTM3U":                       true,
	"EXTINF":                       true,
	"EXT-X-VERSION":                true,
	"EXT-X-TARGETDURATION":         true,
	"EXT-X-MEDIA-SEQUENCE":         true,
	"EXT-X-DISCONTINUITY-SEQUENCE": true,
	"EXT-X-DISCONTINUITY":          true,
	"EXT-X-ENDLIST":                true,
	"EXT-X-PLAYLIST-TYPE":          true,
	"EXT-X-I-FRAMES-ONLY":          true,
	"EXT-X-INDEPENDENT-SEGMENTS":   true,
	"EXT-X-BYTERANGE":              true,
	"EXT-X-PROGRAM-DATE-TIME":      true,
	"EXT-X-GAP":                    true,
	"EXT-X-BITRATE":                true,
	"EXT-X-ALLOW-CACHE":            true,
}

// Tags with an attribute list, and the kind of what their URI attribute
// points at.
var attrTags = map[string]string{
	"EXT-X-KEY":                kindOther,
	"EXT-X-SESSION-KEY":        kindOther,
	"EXT-X-MAP":                kindOther,
	"EXT-X-MEDIA":              kindPlaylist,
	"EXT-X-STREAM-INF":         kindOther, // no URI attribute; the next line
	"EXT-X-I-FRAME-STREAM-INF": kindPlaylist,
	"EXT-X-SESSION-DATA":       kindOther,
	"EXT-X-DATERANGE":          kindOther,
	"EXT-X-START":              kindOther,
	"EXT-X-PRELOAD-HINT":       kindOther,
	"EXT-X-RENDITION-REPORT":   kindPlaylist,
	"EXT-X-PART":               kindOther,
	"EXT-X-PART-INF":           kindOther,
	"EXT-X-SERVER-CONTROL":     kindOther,
	"EXT-X-SKIP":               kindOther,
	"EXT-X-CONTENT-STEERING":   "", // left out: its manifest lists more addresses
	"EXT-X-DEFINE":             "", // left out: variables build addresses
}

type attr struct {
	name, value string
	quoted      bool
}

// isAddressAttr tells whether a player may fetch the value of an
// attribute: URI, any *-URI (SERVER-URI, X-ASSET-URI) and the
// interstitial X-ASSET-LIST.
func isAddressAttr(name string) bool {
	return name == "URI" || strings.HasSuffix(name, "-URI") || name == "X-ASSET-LIST"
}

// splitLines splits a playlist at CR LF, CR and LF.
func splitLines(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return strings.Split(s, "\n")
}

// parseAttrs parses an attribute list, leniently as to blank space
// around names, = and values, and to the case of names.
func parseAttrs(s string) ([]attr, bool) {
	var out []attr
	for {
		s = strings.TrimLeft(s, " \t")
		if s == "" {
			return out, true
		}
		eq := strings.IndexByte(s, '=')
		if eq <= 0 {
			return nil, false
		}
		name := strings.ToUpper(strings.TrimSpace(s[:eq]))
		for _, c := range name {
			if !(c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
				return nil, false
			}
		}
		s = strings.TrimLeft(s[eq+1:], " \t")
		a := attr{name: name}
		if strings.HasPrefix(s, `"`) {
			end := strings.IndexByte(s[1:], '"')
			if end < 0 {
				return nil, false
			}
			a.value, a.quoted = s[1:1+end], true
			s = strings.TrimLeft(s[2+end:], " \t")
		} else {
			end := strings.IndexByte(s, ',')
			if end < 0 {
				end = len(s)
			}
			a.value = strings.TrimSpace(s[:end])
			if a.value == "" || strings.ContainsAny(a.value, "\" \t") {
				return nil, false
			}
			s = s[end:]
		}
		out = append(out, a)
		if s == "" {
			return out, true
		}
		if s[0] != ',' {
			return nil, false
		}
		s = s[1:]
	}
}

// rewritePlaylist rewrites an HLS playlist so that every address in it
// is what proxify returns for it, resolved against base, with the kind
// of what the address points at. An address that does not parse gets
// proxify(nil, kind). A playlist with control characters is refused.
func rewritePlaylist(body []byte, base *url.URL, proxify func(*url.URL, string) string) ([]byte, error) {
	text := strings.TrimPrefix(string(body), "\ufeff")
	for _, c := range text {
		if c < 0x20 && c != '\t' && c != '\r' && c != '\n' || c == 0x7f {
			return nil, errPlaylist
		}
	}
	resolve := func(raw, kind string) string {
		ref, err := url.Parse(strings.TrimSpace(raw))
		if err != nil {
			return proxify(nil, kind)
		}
		return proxify(base.ResolveReference(ref), kind)
	}
	var b strings.Builder
	streamInf := false // the next URI line is a variant playlist
	for _, line := range splitLines(text) {
		t := strings.TrimSpace(line)
		switch {
		case t == "":
			continue
		case !strings.HasPrefix(t, "#"):
			kind := kindOther
			if streamInf {
				kind = kindPlaylist
			}
			streamInf = false
			b.WriteString(resolve(t, kind))
		case !strings.HasPrefix(t, "#EXT"):
			continue // a comment
		default:
			name, value, hasValue := strings.Cut(t[1:], ":")
			if plainTags[name] {
				b.WriteString(t)
				break
			}
			kind, known := attrTags[name]
			if !known || kind == "" || !hasValue {
				continue
			}
			attrs, ok := parseAttrs(value)
			if !ok {
				continue
			}
			if name == "EXT-X-DATERANGE" && interstitial(attrs) {
				continue
			}
			b.WriteString("#" + name + ":")
			for i, a := range attrs {
				if i > 0 {
					b.WriteByte(',')
				}
				switch {
				case isAddressAttr(a.name):
					b.WriteString(a.name + `="` + resolve(a.value, kind) + `"`)
				case a.quoted && strings.Contains(a.value, "//"):
					// Any other attribute that holds a URL, which some
					// player may yet fetch, is kept only as an address.
					b.WriteString(a.name + `="` + resolve(a.value, kindOther) + `"`)
				case a.quoted:
					b.WriteString(a.name + `="` + a.value + `"`)
				default:
					b.WriteString(a.name + "=" + a.value)
				}
			}
			streamInf = name == "EXT-X-STREAM-INF"
		}
		b.WriteByte('\n')
	}
	return []byte(b.String()), nil
}

// interstitial tells whether a date range schedules an interstitial,
// whose assets the player fetches by addresses in the range or in a
// list it downloads.
func interstitial(attrs []attr) bool {
	for _, a := range attrs {
		if a.name == "CLASS" && a.value == "com.apple.hls.interstitial" {
			return true
		}
		if strings.HasPrefix(a.name, "X-ASSET") {
			return true
		}
	}
	return false
}
