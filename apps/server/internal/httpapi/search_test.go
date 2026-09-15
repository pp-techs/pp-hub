package httpapi

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

func query(raw string) url.Values {
	v, err := url.ParseQuery(raw)
	if err != nil {
		panic(err)
	}
	return v
}

// A query below the minimum is a normal state of the palette, not an error —
// it happens on the first keystroke of every search. It must be recognised
// before anything reaches the database.
func TestParseSearchQueryTooShort(t *testing.T) {
	cases := map[string]bool{
		"q=":       true,
		"q=+":      true,
		"q=a":      true,
		"q=ab":     false,
		"q=++ab++": false,
		// Counted in runes, not bytes: two CJK characters are two characters.
		"q=%E6%97%A5":          true,
		"q=%E6%97%A5%E6%9C%AC": false,
	}
	for raw, wantShort := range cases {
		got, err := parseSearchQuery(query(raw))
		if err != nil {
			t.Fatalf("%s: unexpected error %v", raw, err)
		}
		if got.tooShort != wantShort {
			t.Errorf("%s: tooShort = %v, want %v", raw, got.tooShort, wantShort)
		}
	}
}

func TestParseSearchQueryKind(t *testing.T) {
	for _, raw := range []string{"q=report&kind=dir", "q=report&kind=file"} {
		got, err := parseSearchQuery(query(raw))
		if err != nil {
			t.Fatalf("%s: unexpected error %v", raw, err)
		}
		if !got.kind.Valid {
			t.Errorf("%s: kind not set", raw)
		}
	}

	got, err := parseSearchQuery(query("q=report"))
	if err != nil || got.kind.Valid {
		t.Errorf("absent kind should stay null, got %+v err %v", got.kind, err)
	}

	if _, err := parseSearchQuery(query("q=report&kind=folder")); err == nil {
		t.Error("kind=folder should be rejected")
	}
}

// The palette shows a page, not a report; an unbounded limit would let a
// caller pull the whole drive through the search index.
func TestParseSearchQueryLimit(t *testing.T) {
	cases := map[string]int32{
		"q=report":            defaultSearchLimit,
		"q=report&limit=5":    5,
		"q=report&limit=50":   maxSearchLimit,
		"q=report&limit=5000": maxSearchLimit,
	}
	for raw, want := range cases {
		got, err := parseSearchQuery(query(raw))
		if err != nil {
			t.Fatalf("%s: unexpected error %v", raw, err)
		}
		if got.limit != want {
			t.Errorf("%s: limit = %d, want %d", raw, got.limit, want)
		}
	}

	for _, raw := range []string{"q=report&limit=0", "q=report&limit=-1", "q=report&limit=x"} {
		if _, err := parseSearchQuery(query(raw)); err == nil {
			t.Errorf("%s should be rejected", raw)
		}
	}
}

func TestParseSearchQueryBadSpace(t *testing.T) {
	if _, err := parseSearchQuery(query("q=report&space=not-a-uuid")); err == nil {
		t.Error("malformed space id should be rejected")
	}
}

// The empty result must serialise as [] and never as null, or the palette's
// result list would have to guard every render.
func TestWriteShortQueryResultIsEmptyArray(t *testing.T) {
	rec := httptest.NewRecorder()
	writeShortQueryResult(rec)

	if rec.Code != 200 {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if got := strings.TrimSpace(rec.Body.String()); got != "[]" {
		t.Errorf("body = %s, want []", got)
	}
}

// Guards the DTO mapping for a folder, which has no current version and so
// falls back to the node's own created_at for modified_at.
func TestSearchRowToDTOFolder(t *testing.T) {
	var row store.SearchNodesRow
	_ = row.ID.Scan("11111111-1111-1111-1111-111111111111")
	_ = row.SpaceID.Scan("22222222-2222-2222-2222-222222222222")
	row.Kind = store.NodeKindDir
	row.Name = "Invoices"
	row.SpaceName = "Acme Core"
	row.ParentName = "Documents"

	dto := searchRowToDTO(row)
	if dto.Name != "Invoices" || dto.SpaceName != "Acme Core" || dto.ParentName != "Documents" {
		t.Errorf("unexpected dto %+v", dto)
	}
	if dto.ModifiedAt == "" {
		t.Error("a folder should fall back to created_at for modified_at")
	}
	if dto.Size != 0 {
		t.Errorf("a folder should report no size, got %d", dto.Size)
	}
}
