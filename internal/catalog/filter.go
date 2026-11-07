package catalog

import (
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/beilunyang/openhongguo/sdk"
	"github.com/beilunyang/openhongguo/sdk/model"
)

// FilterRow is a row of the filter panel: its key, the name of its "all"
// option (全部主题), and the options to choose one of.
type FilterRow = model.FilterRow

// GenreKey is the row choosing live action, comic or AI dramas. The other
// rows' options depend on it.
const GenreKey = "genre"

// FilterPanel loads the filter panel's rows for a genre, empty for all.
func (b *Backend) FilterPanel(genre string) ([]FilterRow, error) {
	ctx, cancel := ctxTimeout(30 * time.Second)
	defer cancel()
	return b.client.ShortFilterPanel(ctx, genre)
}

// Filter loads a page of the dramas matching the chosen options, a value
// for each row's key. A cursor continues the same choice.
func (b *Backend) Filter(chosen map[string]string, cursor string) ([]Show, string, bool, error) {
	ctx, cancel := ctxTimeout(30 * time.Second)
	defer cancel()
	page, err := b.client.ShortFilter(ctx, sdk.ShortFilterQuery{Selections: selections(chosen), Cursor: cursor})
	if errors.Is(err, sdk.ErrContentUnavailable) && cursor == "" {
		return nil, "", false, nil // nothing matches
	}
	if err != nil {
		return nil, "", false, err
	}
	out := make([]Show, 0, len(page.Items))
	for _, s := range page.Items {
		out = append(out, showOf(s))
	}
	b.fillCovers(ctx, out)
	return out, page.Cursor, page.HasMore && page.Cursor != "", nil
}

// selections turns one chosen value per row into the SDK's query, leaving
// out the rows left at "all".
func selections(chosen map[string]string) map[string][]string {
	out := map[string][]string{}
	for k, v := range chosen {
		if v = strings.TrimSpace(v); v != "" {
			out[k] = []string{v}
		}
	}
	return out
}

// FilterKey names a choice, the same for the same options in any order.
func FilterKey(chosen map[string]string) string {
	var parts []string
	for k, v := range chosen {
		if v != "" {
			parts = append(parts, k+"="+v)
		}
	}
	sort.Strings(parts)
	return strings.Join(parts, "&")
}
