package plugin

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/giuem/ichiwa/internal/netguard"
	"github.com/giuem/ichiwa/internal/source"
)

// APIVersion is the protocol major version this host runs (§12).
const APIVersion = 1

// Manifest is plugin.json (§4).
type Manifest struct {
	ID            string              `json:"id"`
	Name          string              `json:"name"`
	Description   string              `json:"description,omitempty"`
	Kind          string              `json:"kind"`
	Version       string              `json:"version"`
	APIVersion    int                 `json:"apiVersion"`
	MinAppVersion string              `json:"minAppVersion,omitempty"`
	Main          string              `json:"main"`
	Icon          string              `json:"icon,omitempty"`
	License       string              `json:"license,omitempty"`
	Homepage      string              `json:"homepage,omitempty"`
	Network       []string            `json:"-"`
	ImageHeaders  map[string]string   `json:"-"`
	Capabilities  []source.Capability `json:"capabilities,omitempty"`
}

// Hosts returns the hosts the plugin may reach.
func (m *Manifest) Hosts() netguard.Hosts { return netguard.Hosts(m.Network) }

var (
	idPattern     = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
	semverPattern = regexp.MustCompile(`^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`)
	appVerPattern = regexp.MustCompile(`^\d{4}\.\d{3,4}\.\d+$`)
)

// knownCapabilities are the optional capabilities this host knows, with
// the function each needs (§5). Others are ignored.
var knownCapabilities = map[source.Capability]string{
	source.CapSearch:        "search",
	source.CapSearchSuggest: "suggest",
	source.CapSearchHome:    "searchHome",
	source.CapDanmaku:       "danmaku",
	source.CapRefresh:       "refresh",
}

// requiredFunctions every plugin exports.
var requiredFunctions = []string{"navigation", "collection", "media", "play"}

// ParseManifest reads and checks plugin.json. appVersion is this host's
// version, for minAppVersion; "" or a development version skips that
// check.
func ParseManifest(data []byte, appVersion string) (*Manifest, error) {
	var raw struct {
		ID            any             `json:"id"`
		Name          any             `json:"name"`
		Description   any             `json:"description"`
		Kind          any             `json:"kind"`
		Version       any             `json:"version"`
		APIVersion    any             `json:"apiVersion"`
		MinAppVersion any             `json:"minAppVersion"`
		Main          any             `json:"main"`
		Icon          any             `json:"icon"`
		License       any             `json:"license"`
		Homepage      any             `json:"homepage"`
		Permissions   json.RawMessage `json:"permissions"`
		Images        json.RawMessage `json:"images"`
		Capabilities  json.RawMessage `json:"capabilities"`
	}
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, errors.New("plugin.json 不是 JSON 对象")
	}
	if err := json.Unmarshal(trimmed, &raw); err != nil {
		return nil, fmt.Errorf("plugin.json 格式错误：%w", err)
	}
	str := func(field string, v any, required bool) (string, error) {
		if v == nil {
			if required {
				return "", fmt.Errorf("plugin.json 缺少 %s", field)
			}
			return "", nil
		}
		s, ok := v.(string)
		if !ok {
			return "", fmt.Errorf("plugin.json 的 %s 必须是字符串", field)
		}
		s = strings.TrimSpace(s)
		if required && s == "" {
			return "", fmt.Errorf("plugin.json 的 %s 不能为空", field)
		}
		return s, nil
	}
	m := &Manifest{}
	var err error
	if m.ID, err = str("id", raw.ID, true); err != nil {
		return nil, err
	}
	if !idPattern.MatchString(m.ID) {
		return nil, fmt.Errorf("id %q 不合格：只能用小写字母、数字和 . _ -，以字母或数字开头，不超过 64 个字符", m.ID)
	}
	if m.Name, err = str("name", raw.Name, true); err != nil {
		return nil, err
	}
	if utf8.RuneCountInString(m.Name) > 32 {
		return nil, errors.New("name 不能超过 32 个字")
	}
	if m.Description, err = str("description", raw.Description, false); err != nil {
		return nil, err
	}
	if utf8.RuneCountInString(m.Description) > 200 {
		return nil, errors.New("description 不能超过 200 个字")
	}
	if m.Kind, err = str("kind", raw.Kind, true); err != nil {
		return nil, err
	}
	if m.Kind != "source" {
		return nil, fmt.Errorf("不支持的插件种类 %q，目前只有 source", m.Kind)
	}
	if m.Version, err = str("version", raw.Version, true); err != nil {
		return nil, err
	}
	if !semverPattern.MatchString(m.Version) {
		return nil, fmt.Errorf("version %q 不是语义化版本", m.Version)
	}
	api, ok := raw.APIVersion.(float64)
	if !ok {
		return nil, errors.New("plugin.json 缺少数字 apiVersion")
	}
	if api != APIVersion {
		return nil, fmt.Errorf("插件需要协议版本 %v，这个一集支持 %d", api, APIVersion)
	}
	m.APIVersion = APIVersion
	if m.MinAppVersion, err = str("minAppVersion", raw.MinAppVersion, false); err != nil {
		return nil, err
	}
	if m.MinAppVersion != "" {
		if !appVerPattern.MatchString(m.MinAppVersion) {
			return nil, fmt.Errorf("minAppVersion %q 的格式应为 YYYY.MDD.N", m.MinAppVersion)
		}
		if appVerPattern.MatchString(appVersion) && compareAppVersions(appVersion, m.MinAppVersion) < 0 {
			return nil, fmt.Errorf("插件需要一集 %s 或更新的版本，请先更新一集", m.MinAppVersion)
		}
	}
	if m.Main, err = str("main", raw.Main, true); err != nil {
		return nil, err
	}
	if err := checkPath(m.Main); err != nil {
		return nil, fmt.Errorf("main：%w", err)
	}
	if !strings.HasSuffix(m.Main, ".js") && !strings.HasSuffix(m.Main, ".mjs") {
		return nil, fmt.Errorf("main %q 必须是 .js 文件", m.Main)
	}
	if m.Icon, err = str("icon", raw.Icon, false); err != nil {
		return nil, err
	}
	if m.Icon != "" {
		if err := checkPath(m.Icon); err != nil {
			return nil, fmt.Errorf("icon：%w", err)
		}
	}
	if m.License, err = str("license", raw.License, false); err != nil {
		return nil, err
	}
	if m.Homepage, err = str("homepage", raw.Homepage, false); err != nil {
		return nil, err
	}
	if m.Network, err = parseNetwork(raw.Permissions); err != nil {
		return nil, err
	}
	if m.ImageHeaders, err = parseImageHeaders(raw.Images); err != nil {
		return nil, err
	}
	m.Capabilities = parseCapabilities(raw.Capabilities)
	return m, nil
}

func parseNetwork(raw json.RawMessage) ([]string, error) {
	out := []string{}
	if len(raw) == 0 || string(bytes.TrimSpace(raw)) == "null" {
		return out, nil
	}
	var perms struct {
		Network json.RawMessage `json:"network"`
	}
	if err := json.Unmarshal(raw, &perms); err != nil {
		return nil, errors.New("permissions 必须是对象")
	}
	if len(perms.Network) == 0 || string(bytes.TrimSpace(perms.Network)) == "null" {
		return out, nil
	}
	var hosts []string
	if err := json.Unmarshal(perms.Network, &hosts); err != nil {
		return nil, errors.New("permissions.network 必须是字符串数组")
	}
	seen := map[string]bool{}
	for _, h := range hosts {
		if err := netguard.ValidPattern(h); err != nil {
			return nil, fmt.Errorf("permissions.network：%w", err)
		}
		if !seen[h] {
			seen[h] = true
			out = append(out, h)
		}
	}
	return out, nil
}

func parseImageHeaders(raw json.RawMessage) (map[string]string, error) {
	if len(raw) == 0 || string(bytes.TrimSpace(raw)) == "null" {
		return nil, nil
	}
	var images struct {
		Headers map[string]string `json:"headers"`
	}
	if err := json.Unmarshal(raw, &images); err != nil {
		return nil, errors.New("images.headers 必须是字符串到字符串的对象")
	}
	var out map[string]string
	for k, v := range images.Headers {
		if !source.AllowedImageHeaders[strings.ToLower(strings.TrimSpace(k))] {
			return nil, fmt.Errorf("images.headers 不允许 %q，只能用 Referer、User-Agent、Origin", k)
		}
		if strings.ContainsAny(k+v, "\r\n") {
			return nil, fmt.Errorf("images.headers 的 %q 含有换行", k)
		}
		if out == nil {
			out = map[string]string{}
		}
		out[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	return out, nil
}

func parseCapabilities(raw json.RawMessage) []source.Capability {
	var list []any
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil
	}
	var out []source.Capability
	seen := map[source.Capability]bool{}
	for _, v := range list {
		s, ok := v.(string)
		if !ok {
			continue
		}
		c := source.Capability(strings.TrimSpace(s))
		if _, known := knownCapabilities[c]; !known || seen[c] {
			continue
		}
		seen[c] = true
		out = append(out, c)
	}
	return out
}

// Functions returns the exports the plugin must have: the four every
// plugin has and one per capability it declares.
func (m *Manifest) Functions() []string {
	out := append([]string(nil), requiredFunctions...)
	for _, c := range m.Capabilities {
		out = append(out, knownCapabilities[c])
	}
	return out
}

// compareAppVersions compares YYYY.MDD.N versions segment by segment.
func compareAppVersions(a, b string) int {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := range max(len(as), len(bs)) {
		var x, y int
		if i < len(as) {
			x, _ = strconv.Atoi(as[i])
		}
		if i < len(bs) {
			y, _ = strconv.Atoi(bs[i])
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}
