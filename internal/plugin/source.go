package plugin

import (
	"context"
	"encoding/json"

	"github.com/giuem/ichiwa/internal/source"
)

// The adapter from a plugin to source.Source: each method calls the
// export of the same name and decodes what it returns with the tolerant
// decoders of package source (§6), so a plugin and a built-in source
// are cleaned the same way.

var _ source.Source = (*Plugin)(nil)

// Info describes the plugin from its manifest.
func (p *Plugin) Info() source.Info {
	l := p.cur.Load()
	m := l.pkg.Manifest
	return source.Info{
		ID:           m.ID,
		Name:         m.Name,
		Description:  m.Description,
		Version:      m.Version,
		Icon:         l.pkg.Icon,
		ImageHeaders: m.ImageHeaders,
		Capabilities: append([]source.Capability(nil), m.Capabilities...),
		Network:      append([]string{}, m.Network...),
	}
}

func (p *Plugin) clean() source.CleanOptions {
	return source.CleanOptions{AllowHost: p.Manifest().Hosts().Allows}
}

func (p *Plugin) has(c source.Capability) bool { return p.Info().Has(c) }

func (p *Plugin) Navigation(ctx context.Context) ([]source.NavItem, error) {
	raw, err := p.Call(ctx, "navigation", nil)
	if err != nil {
		return nil, err
	}
	return source.DecodeNavigation(raw)
}

// page decodes a collection page and the error of its items, which the
// host shows in place of the list while it keeps the rest of the page.
func (p *Plugin) page(raw json.RawMessage, first bool) (*source.CollectionPage, error) {
	page, err := source.DecodeCollectionPage(raw, first, p.clean())
	if err != nil {
		return nil, err
	}
	if pe := source.DecodePageError(raw); pe != nil {
		p.logs.Hostf("列表部分失败（%s）：%s", pe.Code, pe.Error())
		return page, pe
	}
	return page, nil
}

func (p *Plugin) Collection(ctx context.Context, r source.CollectionRequest) (*source.CollectionPage, error) {
	raw, err := p.Call(ctx, "collection", r)
	if err != nil {
		return nil, err
	}
	return p.page(raw, r.Cursor == "")
}

func (p *Plugin) Media(ctx context.Context, id string) (*source.Media, error) {
	raw, err := p.Call(ctx, "media", map[string]string{"id": id})
	if err != nil {
		return nil, err
	}
	return source.DecodeMedia(raw, p.clean())
}

func (p *Plugin) Play(ctx context.Context, r source.PlayRequest) (*source.Playback, error) {
	raw, err := p.Call(ctx, "play", r)
	if err != nil {
		return nil, err
	}
	return source.DecodePlayback(raw, p.clean())
}

func (p *Plugin) Search(ctx context.Context, r source.SearchRequest) (*source.CollectionPage, error) {
	if !p.has(source.CapSearch) {
		return nil, source.ErrUnsupported
	}
	raw, err := p.Call(ctx, "search", r)
	if err != nil {
		return nil, err
	}
	return p.page(raw, r.Cursor == "")
}

func (p *Plugin) SearchHome(ctx context.Context) (*source.CollectionPage, error) {
	if !p.has(source.CapSearchHome) {
		return nil, source.ErrUnsupported
	}
	raw, err := p.Call(ctx, "searchHome", nil)
	if err != nil {
		return nil, err
	}
	return p.page(raw, true)
}

func (p *Plugin) Suggest(ctx context.Context, query string) ([]string, error) {
	if !p.has(source.CapSearchSuggest) {
		return nil, source.ErrUnsupported
	}
	raw, err := p.Call(ctx, "suggest", map[string]string{"query": query})
	if err != nil {
		return nil, err
	}
	return source.DecodeSuggest(raw)
}

func (p *Plugin) Danmaku(ctx context.Context, r source.DanmakuRequest) (*source.DanmakuPage, error) {
	if !p.has(source.CapDanmaku) {
		return nil, source.ErrUnsupported
	}
	raw, err := p.Call(ctx, "danmaku", r)
	if err != nil {
		return nil, err
	}
	return source.DecodeDanmakuPage(raw)
}

func (p *Plugin) Refresh(ctx context.Context, ids []string) ([]source.Card, error) {
	if !p.has(source.CapRefresh) {
		return nil, source.ErrUnsupported
	}
	if len(ids) > source.MaxRefreshIDs {
		ids = ids[:source.MaxRefreshIDs]
	}
	raw, err := p.Call(ctx, "refresh", map[string][]string{"ids": ids})
	if err != nil {
		return nil, err
	}
	return source.DecodeRefresh(raw, p.clean())
}
