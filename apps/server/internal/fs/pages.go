package fs

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// PageTitle is a page's title: its node name without `.md`, whatever its case
// (M43). Renaming the node is retitling the page.
func PageTitle(name string) string {
	if len(name) >= 3 && strings.EqualFold(name[len(name)-3:], ".md") {
		return name[:len(name)-3]
	}
	return name
}

// PageTitles answers, in one query, the titles of the pages ids names that
// are live files in space, keyed by the id strings as given — what
// mdoc.RenderOptions.PageTitle reads when a document is rendered. Ids that
// are not UUIDs, trashed, missing or in another space are absent, so they
// render as Untitled.
func (s *Service) PageTitles(ctx context.Context, space pgtype.UUID, ids []string) (map[string]string, error) {
	byID := make(map[[16]byte][]string, len(ids))
	uuids := make([]pgtype.UUID, 0, len(ids))
	for _, id := range ids {
		var u pgtype.UUID
		if u.Scan(id) != nil {
			continue
		}
		if _, seen := byID[u.Bytes]; !seen {
			uuids = append(uuids, u)
		}
		byID[u.Bytes] = append(byID[u.Bytes], id)
	}
	titles := make(map[string]string, len(ids))
	if len(uuids) == 0 {
		return titles, nil
	}
	rows, err := s.q.ListPageTitles(ctx, store.ListPageTitlesParams{Ids: uuids, SpaceID: space})
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		for _, id := range byID[r.ID.Bytes] {
			titles[id] = PageTitle(r.Name)
		}
	}
	return titles, nil
}
