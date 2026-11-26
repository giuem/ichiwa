package sourcetest

import (
	"context"
	"errors"
	"testing"

	"github.com/giuem/ichiwa/internal/source"
)

func TestFakeSourceCapabilitiesAndCancel(t *testing.T) {
	s := &Source{
		InfoData: source.Info{ID: "fake", Name: "Fake", Capabilities: []source.Capability{source.CapSearch, source.CapRefresh}},
		NavItems: []source.NavItem{{ID: "home", Title: "首页", Icon: source.IconHome}},
		RefreshCards: map[string]source.Card{
			"1": {Kind: source.CardMedia, ID: "1", Title: "甲"},
		},
	}
	ctx := context.Background()
	if nav, err := s.Navigation(ctx); err != nil || len(nav) != 1 {
		t.Fatalf("Navigation = %v, %v", nav, err)
	}
	if _, err := s.Danmaku(ctx, source.DanmakuRequest{}); !errors.Is(err, source.ErrUnsupported) {
		t.Fatalf("Danmaku = %v, want ErrUnsupported", err)
	}
	if _, err := s.SearchHome(ctx); !errors.Is(err, source.ErrUnsupported) {
		t.Fatalf("SearchHome = %v, want ErrUnsupported", err)
	}
	if cards, err := s.Refresh(ctx, []string{"1", "2"}); err != nil || len(cards) != 1 {
		t.Fatalf("Refresh = %v, %v", cards, err)
	}

	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := s.Navigation(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Navigation = %v", err)
	}
}
