package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nhatbui/pp-hub/apps/server/internal/audit"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// The node filter is a recursive walk in SQL and the export a keyset loop
// over the same predicate; both only mean anything against real Postgres.
func TestSecurityEventsNodeFilterAndExport(t *testing.T) {
	srv, q := feedEnv(t)
	srv.audit = audit.New(q)
	ctx := context.Background()

	owner := feedUser(t, q, "secev-owner")
	sp, err := srv.spaces.Create(ctx, fmt.Sprintf("secev-%d", os.Getpid()), owner.ID)
	if err != nil {
		t.Fatalf("create space: %v", err)
	}
	t.Cleanup(func() { _ = q.DeleteSpace(ctx, sp.ID) })

	mk := func(parent pgtype.UUID, kind store.NodeKind, name string) store.Node {
		t.Helper()
		n, err := q.CreateNode(ctx, store.CreateNodeParams{
			SpaceID: sp.ID, ParentID: parent, Kind: kind, Name: name, CreatedBy: owner.ID,
		})
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		return n
	}
	folder := mk(pgtype.UUID{}, store.NodeKindDir, "secev-folder")
	file := mk(folder.ID, store.NodeKindFile, "secev-inside.txt")
	sibling := mk(pgtype.UUID{}, store.NodeKindFile, "secev-sibling.txt")
	var purged pgtype.UUID
	_, _ = rand.Read(purged.Bytes[:])
	purged.Valid = true

	record := func(node pgtype.UUID, subject string) {
		t.Helper()
		if err := q.RecordSecurityEvent(ctx, store.RecordSecurityEventParams{
			Kind: string(audit.KindFileDownloaded), UserID: owner.ID, Subject: subject,
			Detail: []byte(`{"name":"gone.txt"}`), NodeID: node,
		}); err != nil {
			t.Fatalf("record: %v", err)
		}
	}
	record(file.ID, "=cmd")
	record(sibling.ID, owner.Email)
	record(purged, owner.Email)
	// security_events has no FK to nodes on purpose, so deleting the space
	// leaves these behind; the log is shared with the dev instance.
	t.Cleanup(func() {
		pool, err := pgxpool.New(ctx, os.Getenv("PP_TEST_DATABASE_URL"))
		if err != nil {
			return
		}
		defer pool.Close()
		_, _ = pool.Exec(ctx, `DELETE FROM security_events WHERE node_id = ANY($1)`,
			[]pgtype.UUID{file.ID, sibling.ID, purged})
	})

	list := func(query string) (int, securityEventPage) {
		t.Helper()
		req := withTestUser(httptest.NewRequest("GET", "/api/v1/admin/security-events?"+query, nil), true)
		rec := httptest.NewRecorder()
		srv.handleAdminSecurityEvents(rec, req)
		var page securityEventPage
		if rec.Code == http.StatusOK {
			if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
				t.Fatalf("decode: %v", err)
			}
		}
		return rec.Code, page
	}

	code, page := list("node=" + folder.ID.String())
	if code != http.StatusOK || page.Total != 1 || len(page.Events) != 1 {
		t.Fatalf("folder filter: code %d, %+v", code, page)
	}
	if got := page.Events[0]; got.NodeID != file.ID.String() || got.NodeName != file.Name {
		t.Fatalf("folder filter returned %+v, want the file inside", got)
	}

	code, page = list("node=" + purged.String())
	if code != http.StatusOK || len(page.Events) != 1 || page.Events[0].NodeName != "gone.txt" {
		t.Fatalf("purged node filter: code %d, %+v", code, page)
	}

	if code, _ := list("from=notatime"); code != http.StatusBadRequest {
		t.Fatalf("bad from: got %d, want 400", code)
	}

	req := withTestUser(httptest.NewRequest("GET", "/api/v1/admin/security-events/export?node="+folder.ID.String(), nil), true)
	rec := httptest.NewRecorder()
	srv.handleAdminSecurityEventsExport(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("export: %d %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.HasPrefix(body, "\ufefftime,kind,user_id,") {
		t.Fatalf("export must start with the BOM and header, got %q", body[:min(len(body), 60)])
	}
	rows, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(body, "\ufeff"))).ReadAll()
	if err != nil {
		t.Fatalf("parse csv: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("export rows = %d, want header + 1: %q", len(rows), body)
	}
	if subject := rows[1][5]; subject != "'=cmd" {
		t.Fatalf("formula-looking subject exported as %q, want it defused", subject)
	}
}
