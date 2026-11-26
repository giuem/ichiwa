// Package sourcetest provides a configurable fake source.Source for tests.
package sourcetest

import (
	"context"
	"sync"

	"github.com/giuem/ichiwa/internal/source"
)

// Source is an in-memory fake implementation of source.Source.
// Zero values are valid and return empty results or ErrUnsupported according to
// InfoData.Capabilities.
type Source struct {
	InfoData source.Info

	NavItems       []source.NavItem
	Collections    map[string]*source.CollectionPage
	Medias         map[string]*source.Media
	Playbacks      map[string]*source.Playback // keyed by episodeID
	SearchHomePage *source.CollectionPage
	Suggestions    []string
	Danmakus       map[string]*source.DanmakuPage // keyed by episodeID
	RefreshCards   map[string]source.Card

	OnNavigation func(ctx context.Context) ([]source.NavItem, error)
	OnCollection func(ctx context.Context, r source.CollectionRequest) (*source.CollectionPage, error)
	OnMedia      func(ctx context.Context, id string) (*source.Media, error)
	OnPlay       func(ctx context.Context, r source.PlayRequest) (*source.Playback, error)
	OnSearch     func(ctx context.Context, r source.SearchRequest) (*source.CollectionPage, error)
	OnSearchHome func(ctx context.Context) (*source.CollectionPage, error)
	OnSuggest    func(ctx context.Context, query string) ([]string, error)
	OnDanmaku    func(ctx context.Context, r source.DanmakuRequest) (*source.DanmakuPage, error)
	OnRefresh    func(ctx context.Context, ids []string) ([]source.Card, error)

	mu    sync.Mutex
	Calls []string
}

var _ source.Source = (*Source)(nil)

func (s *Source) record(name string) {
	s.mu.Lock()
	s.Calls = append(s.Calls, name)
	s.mu.Unlock()
}

func (s *Source) Info() source.Info { return s.InfoData }

func (s *Source) Navigation(ctx context.Context) ([]source.NavItem, error) {
	s.record("navigation")
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.OnNavigation != nil {
		return s.OnNavigation(ctx)
	}
	return append([]source.NavItem(nil), s.NavItems...), nil
}

func (s *Source) Collection(ctx context.Context, r source.CollectionRequest) (*source.CollectionPage, error) {
	s.record("collection:" + r.ID)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.OnCollection != nil {
		return s.OnCollection(ctx, r)
	}
	if p, ok := s.Collections[r.ID]; ok {
		return p, nil
	}
	return nil, source.NewError(source.ErrNotFound, "集合不存在", nil)
}

func (s *Source) Media(ctx context.Context, id string) (*source.Media, error) {
	s.record("media:" + id)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.OnMedia != nil {
		return s.OnMedia(ctx, id)
	}
	if m, ok := s.Medias[id]; ok {
		return m, nil
	}
	return nil, source.NewError(source.ErrNotFound, "作品不存在", nil)
}

func (s *Source) Play(ctx context.Context, r source.PlayRequest) (*source.Playback, error) {
	s.record("play:" + r.EpisodeID)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.OnPlay != nil {
		return s.OnPlay(ctx, r)
	}
	if pb, ok := s.Playbacks[r.EpisodeID]; ok {
		return pb, nil
	}
	return nil, source.NewError(source.ErrUnavailable, "没有可用的视频流", nil)
}

func (s *Source) Search(ctx context.Context, r source.SearchRequest) (*source.CollectionPage, error) {
	s.record("search:" + r.Query)
	if !s.InfoData.Has(source.CapSearch) {
		return nil, source.ErrUnsupported
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.OnSearch != nil {
		return s.OnSearch(ctx, r)
	}
	return &source.CollectionPage{Layout: source.LayoutGrid}, nil
}

func (s *Source) SearchHome(ctx context.Context) (*source.CollectionPage, error) {
	s.record("searchHome")
	if !s.InfoData.Has(source.CapSearchHome) {
		return nil, source.ErrUnsupported
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.OnSearchHome != nil {
		return s.OnSearchHome(ctx)
	}
	if s.SearchHomePage != nil {
		return s.SearchHomePage, nil
	}
	return &source.CollectionPage{}, nil
}

func (s *Source) Suggest(ctx context.Context, query string) ([]string, error) {
	s.record("suggest:" + query)
	if !s.InfoData.Has(source.CapSearchSuggest) {
		return nil, source.ErrUnsupported
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.OnSuggest != nil {
		return s.OnSuggest(ctx, query)
	}
	return append([]string(nil), s.Suggestions...), nil
}

func (s *Source) Danmaku(ctx context.Context, r source.DanmakuRequest) (*source.DanmakuPage, error) {
	s.record("danmaku:" + r.EpisodeID)
	if !s.InfoData.Has(source.CapDanmaku) {
		return nil, source.ErrUnsupported
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.OnDanmaku != nil {
		return s.OnDanmaku(ctx, r)
	}
	if p, ok := s.Danmakus[r.EpisodeID]; ok {
		return p, nil
	}
	return &source.DanmakuPage{}, nil
}

func (s *Source) Refresh(ctx context.Context, ids []string) ([]source.Card, error) {
	s.record("refresh")
	if !s.InfoData.Has(source.CapRefresh) {
		return nil, source.ErrUnsupported
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.OnRefresh != nil {
		return s.OnRefresh(ctx, ids)
	}
	var out []source.Card
	for _, id := range ids {
		if c, ok := s.RefreshCards[id]; ok {
			out = append(out, c)
		}
	}
	return out, nil
}
