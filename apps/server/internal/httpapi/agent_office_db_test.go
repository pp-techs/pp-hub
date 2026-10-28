package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nhatbui/pp-hub/apps/server/internal/activity"
	"github.com/nhatbui/pp-hub/apps/server/internal/audit"
	"github.com/nhatbui/pp-hub/apps/server/internal/blob"
	"github.com/nhatbui/pp-hub/apps/server/internal/officeedit"
	"github.com/nhatbui/pp-hub/apps/server/internal/share"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// The runner is a controlled boundary here, not an alternative Office
// implementation: real CLI semantics are covered by officeedit. These tests
// prove the API's persisted versions, actual grant resolution, lock exclusion,
// audit and activity through authenticated MCP calls against real Postgres.
type officeTestEngine func(context.Context, officeedit.Request) (officeedit.Response, error)

func (f officeTestEngine) Do(ctx context.Context, req officeedit.Request) (officeedit.Response, error) {
	return f(ctx, req)
}

func officeTestDOCX(t *testing.T, text string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, part := range []struct{ name, content string }{
		{"[Content_Types].xml", `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`},
		{"_rels/.rels", `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`},
		{"word/document.xml", `<?xml version="1.0"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>` + html.EscapeString(text) + `</w:t></w:r></w:p></w:body></w:document>`},
	} {
		w, err := zw.Create(part.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(part.content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func officeFixture(t *testing.T, rig mcpRig, label string, content []byte) collabFixture {
	t.Helper()
	f := rig.collabRig.fixture(t, label, "# Fixture\n")
	n, _, err := rig.srv.fs.PutFile(context.Background(), f.space.ID, pgtype.UUID{}, pgtype.UUID{}, label+".docx", bytes.NewReader(content), f.owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	f.doc = n
	grantDirect(t, rig.srv, n, f.editor, f.owner, share.GrantCaps{Level: share.LevelEdit})
	grantDirect(t, rig.srv, n, f.viewer, f.owner, share.GrantCaps{Level: share.LevelView})
	return f
}

func officeEditArgs(node store.Node, etag string) map[string]any {
	return map[string]any{
		"node_id": uuidString(node.ID), "base_etag": etag,
		"operations": []map[string]any{{"op": "set", "path": "/body/p[1]", "props": map[string]string{"text": "Final budget"}}},
	}
}

func officeActivityCount(t *testing.T, rig mcpRig, node store.Node) int {
	t.Helper()
	rows, err := rig.srv.activity.ForSpace(context.Background(), node.SpaceID, 100)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, row := range rows {
		if row.NodeID == node.ID && row.Kind == string(activity.KindUploaded) {
			n++
		}
	}
	return n
}

func TestMcpOfficeReadsAndEditsRespectAccessAndVersions(t *testing.T) {
	rig := newMCPRig(t)
	before, after := officeTestDOCX(t, "Draft budget"), officeTestDOCX(t, "Final budget")
	f := officeFixture(t, rig, "officeaccess", before)
	rig.srv.officeEditor = officeTestEngine(func(_ context.Context, req officeedit.Request) (officeedit.Response, error) {
		if req.Action == "read" {
			return officeedit.Response{Data: json.RawMessage(`{"path":"/body","children":[{"path":"/body/p[1]","text":"Draft budget"}]}`)}, nil
		}
		return officeedit.Response{Content: after}, nil
	})
	viewer, editor, stranger := rig.agent(t, f.viewer), rig.agent(t, f.editor), rig.agent(t, f.stranger)
	id := uuidString(f.doc.ID)
	if text, isErr := callTool(t, stranger, "read_office_file", map[string]any{"node_id": id}); !isErr || !strings.Contains(text, "403") {
		t.Fatalf("ungranted member read Office: %s", text)
	}
	var read agentOfficeDTO
	mustTool(t, viewer, "read_office_file", map[string]any{"node_id": id}, &read)
	if read.ETag != blob.Hash(before) || read.Format != "docx" || read.Version != 1 || !bytes.Contains(read.Data, []byte("Draft budget")) {
		t.Fatalf("structured read did not describe the current saved file: %+v", read)
	}
	if text, isErr := callTool(t, viewer, "edit_office_file", officeEditArgs(f.doc, read.ETag)); !isErr || !strings.Contains(text, "403") {
		t.Fatalf("view-only person edited Office: %s", text)
	}

	// The same current Office editor lock prevents stale reads and edits.
	ctx := context.Background()
	if n, err := rig.q.AcquireLock(ctx, store.AcquireLockParams{NodeID: f.doc.ID, LockToken: "wopi_live_office"}); err != nil || n != 1 {
		t.Fatalf("editor lock: %d %v", n, err)
	}
	for name, args := range map[string]map[string]any{
		"read_office_file": {"node_id": id}, "edit_office_file": officeEditArgs(f.doc, read.ETag),
	} {
		if text, isErr := callTool(t, editor, name, args); !isErr || !strings.Contains(text, "409") {
			t.Fatalf("%s bypassed live Office lock: %s", name, text)
		}
	}
	if err := rig.q.DeleteLock(ctx, store.DeleteLockParams{NodeID: f.doc.ID, LockToken: "wopi_live_office"}); err != nil {
		t.Fatal(err)
	}

	var edited struct {
		ETag    string `json:"etag"`
		Version int32  `json:"version"`
	}
	mustTool(t, editor, "edit_office_file", officeEditArgs(f.doc, read.ETag), &edited)
	ver, err := rig.q.GetCurrentVersion(ctx, f.doc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if edited.ETag != blob.Hash(after) || edited.Version != 2 || ver.CreatedBy != f.editor.ID || rig.content(t, ver) != string(after) {
		t.Fatalf("edit did not persist an editor-authored version: %+v %+v", edited, ver)
	}
	if n := officeActivityCount(t, rig, f.doc); n != 1 {
		t.Fatalf("changed edit recorded %d upload activities, want 1", n)
	}
	mustTool(t, editor, "edit_office_file", officeEditArgs(f.doc, edited.ETag), nil)
	if len(rig.versions(t, f.doc)) != 2 || officeActivityCount(t, rig, f.doc) != 1 {
		t.Fatal("byte-identical edit added a version or upload activity")
	}
	if text, isErr := callTool(t, editor, "edit_office_file", officeEditArgs(f.doc, read.ETag)); !isErr || !strings.Contains(text, "409") {
		t.Fatalf("stale edit succeeded: %s", text)
	}
	lock, err := rig.srv.wopi.CurrentLock(ctx, f.doc.ID)
	if err != nil || lock != "" {
		t.Fatalf("edit left a lock: %q %v", lock, err)
	}
	rows, err := rig.q.ListSecurityEvents(ctx, store.ListSecurityEventsParams{Limit: 100, Node: f.doc.ID, Kind: pgtype.Text{String: string(audit.KindFileViewed), Valid: true}})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, row := range rows {
		var detail map[string]any
		_ = json.Unmarshal(row.Detail, &detail)
		if row.UserID == f.viewer.ID && detail["via"] == "mcp" {
			found = true
		}
	}
	if !found {
		t.Fatal("structured view was not audited as this MCP viewer")
	}
	for _, method := range []string{http.MethodGet, http.MethodPut} {
		if res := rig.do(t, f.owner, method, "/api/v1/nodes/"+id+"/office", nil); res.code != http.StatusNotFound && res.code != http.StatusMethodNotAllowed {
			t.Fatalf("Office route leaked to cookie API: %s %d", method, res.code)
		}
	}
	if res := rig.do(t, f.owner, http.MethodGet, "/api/v1/office/schema?format=docx", nil); res.code != http.StatusNotFound && res.code != http.StatusMethodNotAllowed {
		t.Fatalf("Office schema leaked to cookie API: %d", res.code)
	}
	rig.srv.officeEditor = nil
	mustTool(t, editor, "get_office_schema", map[string]any{"format": "docx"}, nil)
	if text, isErr := callTool(t, editor, "read_office_file", map[string]any{"node_id": id}); !isErr || !strings.Contains(text, "503") {
		t.Fatalf("unconfigured runner did not refuse: %s", text)
	}
}

func TestMcpOfficeSerializesConcurrentEdits(t *testing.T) {
	rig := newMCPRig(t)
	before, after := officeTestDOCX(t, "Draft budget"), officeTestDOCX(t, "Final budget")
	f := officeFixture(t, rig, "officeconcurrent", before)
	entered, release := make(chan struct{}), make(chan struct{})
	rig.srv.officeEditor = officeTestEngine(func(ctx context.Context, _ officeedit.Request) (officeedit.Response, error) {
		close(entered)
		select {
		case <-release:
			return officeedit.Response{Content: after}, nil
		case <-ctx.Done():
			return officeedit.Response{}, ctx.Err()
		}
	})
	editor := rig.agent(t, f.editor)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	type result struct {
		response *mcp.CallToolResult
		err      error
	}
	done := make(chan result, 1)
	go func() {
		r, err := editor.CallTool(ctx, &mcp.CallToolParams{Name: "edit_office_file", Arguments: officeEditArgs(f.doc, blob.Hash(before))})
		done <- result{r, err}
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("first edit never reached the runner")
	}
	lock, err := rig.srv.wopi.CurrentLock(ctx, f.doc.ID)
	if err != nil || !strings.HasPrefix(lock, "agent-office:") {
		t.Fatalf("processing edit did not hold a unique agent lock: %q %v", lock, err)
	}
	if text, isErr := callTool(t, editor, "edit_office_file", officeEditArgs(f.doc, blob.Hash(before))); !isErr || !strings.Contains(text, "409") {
		t.Fatalf("concurrent edit was not excluded: %s", text)
	}
	if text, isErr := callTool(t, editor, "read_office_file", map[string]any{"node_id": uuidString(f.doc.ID)}); !isErr || !strings.Contains(text, "409") {
		t.Fatalf("read during an unsaved edit was not excluded: %s", text)
	}
	close(release)
	select {
	case r := <-done:
		if r.err != nil || r.response == nil || r.response.IsError {
			t.Fatalf("first edit failed: %+v %v", r.response, r.err)
		}
	case <-ctx.Done():
		t.Fatal("first edit did not finish")
	}
	versions := rig.versions(t, f.doc)
	if len(versions) != 2 || rig.content(t, versions[0]) != string(after) {
		t.Fatal("concurrent calls committed more than the accepted edit")
	}
	if lock, err := rig.srv.wopi.CurrentLock(ctx, f.doc.ID); err != nil || lock != "" {
		t.Fatalf("committed edit kept its lock: %q %v", lock, err)
	}
}

func TestMcpOfficeRechecksAfterProcessing(t *testing.T) {
	rig := newMCPRig(t)
	before, after, competing := officeTestDOCX(t, "Draft budget"), officeTestDOCX(t, "Final budget"), officeTestDOCX(t, "Someone else's budget")
	for _, scenario := range []string{"permission", "trash", "ancestor-trash", "move", "rename", "version", "lock", "processing"} {
		t.Run(scenario, func(t *testing.T) {
			f := officeFixture(t, rig, "officerecheck"+scenario, before)
			var parent store.Node
			if scenario == "ancestor-trash" {
				var err error
				parent, err = rig.srv.fs.Mkdir(context.Background(), f.space.ID, pgtype.UUID{}, "Parent", f.owner.ID)
				if err == nil {
					err = rig.srv.fs.Move(context.Background(), f.doc.ID, parent.ID, f.owner.ID)
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			wantStatus, wantHash, wantVersions := "409", blob.Hash(before), 1
			if scenario == "permission" {
				wantStatus = "403"
			} else if scenario == "trash" || scenario == "ancestor-trash" {
				wantStatus = "404"
			} else if scenario == "processing" {
				wantStatus = "422"
			} else if scenario == "version" {
				wantHash, wantVersions = blob.Hash(competing), 2
			}
			rig.srv.officeEditor = officeTestEngine(func(ctx context.Context, _ officeedit.Request) (officeedit.Response, error) {
				var err error
				switch scenario {
				case "permission":
					_, err = rig.srv.share.UpsertDirect(ctx, f.doc.ID, f.editor.ID, share.GrantCaps{Level: share.LevelView}, pgtype.Timestamptz{}, f.owner.ID, false)
				case "trash":
					err = rig.srv.fs.Delete(ctx, f.doc.ID, f.owner.ID)
				case "ancestor-trash":
					err = rig.srv.fs.Delete(ctx, parent.ID, f.owner.ID)
				case "move":
					var dir store.Node
					dir, err = rig.srv.fs.Mkdir(ctx, f.space.ID, pgtype.UUID{}, "Destination", f.owner.ID)
					if err == nil {
						err = rig.srv.fs.Move(ctx, f.doc.ID, dir.ID, f.owner.ID)
					}
				case "rename":
					err = rig.srv.fs.Rename(ctx, f.doc.ID, "renamed.docx")
				case "version":
					_, _, err = rig.srv.fs.PutFile(ctx, f.space.ID, f.doc.ParentID, f.doc.ID, f.doc.Name, bytes.NewReader(competing), f.owner.ID)
				case "lock":
					err = rig.q.UpsertLock(ctx, store.UpsertLockParams{NodeID: f.doc.ID, LockToken: "foreign-editor"})
				case "processing":
					return officeedit.Response{}, officeedit.ErrProcessing
				}
				if err != nil {
					return officeedit.Response{}, fmt.Errorf("scenario mutation: %w", err)
				}
				return officeedit.Response{Content: after}, nil
			})
			if text, isErr := callTool(t, rig.agent(t, f.editor), "edit_office_file", officeEditArgs(f.doc, blob.Hash(before))); !isErr || !strings.Contains(text, wantStatus) {
				t.Fatalf("%s was not rechecked: %s", scenario, text)
			}
			current, err := rig.q.GetNodeAny(context.Background(), f.doc.ID)
			if err != nil {
				t.Fatal(err)
			}
			ver, err := rig.q.GetVersion(context.Background(), current.CurrentVersionID)
			if err != nil {
				t.Fatal(err)
			}
			if ver.BlobHash != wantHash || len(rig.versions(t, f.doc)) != wantVersions || officeActivityCount(t, rig, f.doc) != 0 {
				t.Fatalf("refused %s edit changed persisted state: %+v", scenario, ver)
			}
			lock, err := rig.srv.wopi.CurrentLock(context.Background(), f.doc.ID)
			wantLock := ""
			if scenario == "lock" {
				wantLock = "foreign-editor"
			}
			if err != nil || lock != wantLock {
				t.Fatalf("refused edit released the wrong lock: %q %v", lock, err)
			}
		})
	}
}

func TestMcpOfficeRejectsUnsupportedAndOversizedFiles(t *testing.T) {
	rig := newMCPRig(t)
	before, after := officeTestDOCX(t, "Draft budget"), officeTestDOCX(t, "Final budget")
	for _, scenario := range []string{"extension", "mime", "declared-size", "actual-size", "output-size", "unavailable", "invalid-operation"} {
		t.Run(scenario, func(t *testing.T) {
			f := officeFixture(t, rig, "officelimits"+scenario, before)
			base, wantStatus := blob.Hash(before), "415"
			mimeType := "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
			size := int64(len(before))
			switch scenario {
			case "extension":
				if err := rig.srv.fs.Rename(context.Background(), f.doc.ID, "legacy.doc"); err != nil {
					t.Fatal(err)
				}
			case "mime":
				mimeType = "application/zip"
			case "declared-size":
				size, wantStatus = officeedit.MaxFileBytes+1, "413"
			case "actual-size":
				large := make([]byte, officeedit.MaxFileBytes+1)
				if _, _, err := rig.srv.fs.PutFile(context.Background(), f.space.ID, f.doc.ParentID, f.doc.ID, f.doc.Name, bytes.NewReader(large), f.owner.ID); err != nil {
					t.Fatal(err)
				}
				base, size, wantStatus = blob.Hash(large), 1, "413"
			case "output-size":
				wantStatus = "502"
			case "unavailable":
				wantStatus = "503"
			case "invalid-operation":
				wantStatus = "422"
			}
			if scenario == "mime" || scenario == "declared-size" || scenario == "actual-size" {
				ver, err := rig.q.AddVersion(context.Background(), store.AddVersionParams{
					NodeID: f.doc.ID, BlobHash: base, Size: size, Mime: mimeType, CreatedBy: f.owner.ID,
				})
				if err != nil {
					t.Fatal(err)
				}
				if err := rig.q.SetCurrentVersion(context.Background(), store.SetCurrentVersionParams{ID: f.doc.ID, CurrentVersionID: ver.ID}); err != nil {
					t.Fatal(err)
				}
			}
			count := len(rig.versions(t, f.doc))
			rig.srv.officeEditor = officeTestEngine(func(_ context.Context, _ officeedit.Request) (officeedit.Response, error) {
				switch scenario {
				case "output-size":
					return officeedit.Response{Content: make([]byte, officeedit.MaxFileBytes+1)}, nil
				case "unavailable":
					return officeedit.Response{}, officeedit.ErrUnavailable
				case "invalid-operation":
					return officeedit.Response{}, officeedit.ErrInvalidRequest
				}
				return officeedit.Response{Content: after, Data: json.RawMessage(`{"path":"/"}`)}, nil
			})
			editor := rig.agent(t, f.editor)
			if text, isErr := callTool(t, editor, "edit_office_file", officeEditArgs(f.doc, base)); !isErr || !strings.Contains(text, wantStatus) {
				t.Fatalf("%s input/output was accepted: %s", scenario, text)
			}
			if scenario == "extension" || scenario == "mime" || scenario == "declared-size" || scenario == "actual-size" {
				if text, isErr := callTool(t, editor, "read_office_file", map[string]any{"node_id": uuidString(f.doc.ID)}); !isErr || !strings.Contains(text, wantStatus) {
					t.Fatalf("%s file was readable: %s", scenario, text)
				}
			}
			current, err := rig.q.GetCurrentVersion(context.Background(), f.doc.ID)
			if err != nil || current.BlobHash != base || len(rig.versions(t, f.doc)) != count || officeActivityCount(t, rig, f.doc) != 0 {
				t.Fatalf("refused %s operation changed stored content: %+v %v", scenario, current, err)
			}
			if lock, err := rig.srv.wopi.CurrentLock(context.Background(), f.doc.ID); err != nil || lock != "" {
				t.Fatalf("refused %s operation kept its lock: %q %v", scenario, lock, err)
			}
		})
	}
}

func TestMcpOfficeReadDoesNotOutliveViewPermission(t *testing.T) {
	rig := newMCPRig(t)
	f := officeFixture(t, rig, "officereadrevoked", officeTestDOCX(t, "Confidential budget"))
	rig.srv.officeEditor = officeTestEngine(func(ctx context.Context, _ officeedit.Request) (officeedit.Response, error) {
		if err := rig.q.DeleteSharesByGranteeInSpace(ctx, store.DeleteSharesByGranteeInSpaceParams{SpaceID: f.space.ID, GranteeUserID: f.viewer.ID}); err != nil {
			return officeedit.Response{}, err
		}
		return officeedit.Response{Data: json.RawMessage(`{"text":"Confidential budget"}`)}, nil
	})
	text, isErr := callTool(t, rig.agent(t, f.viewer), "read_office_file", map[string]any{"node_id": uuidString(f.doc.ID)})
	if !isErr || !strings.Contains(text, "403") || strings.Contains(text, "Confidential budget") {
		t.Fatalf("read disclosed content after permission revocation: %s", text)
	}
}
