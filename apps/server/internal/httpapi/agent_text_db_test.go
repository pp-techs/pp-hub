package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nhatbui/pp-hub/apps/server/internal/blob"
	"github.com/nhatbui/pp-hub/apps/server/internal/fs"
	"github.com/nhatbui/pp-hub/apps/server/internal/officeedit"
	"github.com/nhatbui/pp-hub/apps/server/internal/share"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// These drive the public MCP tools, real permissions, the blob store and SQL
// versions. Source is data: formatting, markup and line endings survive exactly.
func TestMcpTextFilesPreserveSourceBytes(t *testing.T) {
	rig := newMCPRig(t)
	f := rig.collabRig.fixture(t, "mcptextbytes", "# Document\n")
	owner := rig.agent(t, f.owner)
	ctx := context.Background()
	for _, tc := range []struct {
		name, before, after, mime string
	}{
		{"index.html", "<!doctype html>\r\n<p>old</p>\r\n", "<!doctype html>\r\n<script>throw new Error('source only')</script>\r\n", ""},
		{"style.css", "/* old */\r\n", "/* keep whitespace */\r\na { color: red; }  \r\n", ""},
		{"app.js", "const old = 1;\n", "const source = '<b>not markup</b>';\n", ""},
		{"app.ts", "const n: number = 1;\n", "type State = 'ready' | 'done';\r\nconst n: number = 2;", "video/mp2t"},
		{"config.json", "{\"old\": 1}\n", "{\r\n  \"duplicate\": 1, \"duplicate\": 2\r\n}\r\n", "application/json"},
		{"config.data", "{\"old\": 1}\n", "{\"new\": 2}\r\n", "application/json"},
		{"config.yaml", "# old\nvalue: 1\n", "# preserve comment\r\nvalue: &anchor 2\r\ncopy: *anchor\r\n", "application/octet-stream"},
		{"image.svg", "<svg xmlns=\"http://www.w3.org/2000/svg\"/>\n", "<svg xmlns=\"http://www.w3.org/2000/svg\"><text>é</text></svg>\r\n", "image/svg+xml"},
		{"data.xml", "<?xml version=\"1.0\"?><old/>\n", "<?xml version=\"1.0\"?>\r\n<!-- keep -->\r\n<value>2</value>", "application/xml"},
		{"table.csv", "name,value\r\nold,1\r\n", "name,value\r\n\"line\r\nbreak\",\"quoted \"\"value\"\"\"\r\n", ""},
		{"notes.txt", "old\n", "\ufeffTiếng Việt\r\n\ttrailing spaces  ", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			node := rig.putMarkdown(t, f.space, f.owner, tc.name, tc.before)
			if tc.mime != "" {
				old := rig.versions(t, node)[0]
				ver, err := rig.q.AddVersion(ctx, store.AddVersionParams{NodeID: node.ID, BlobHash: old.BlobHash, Size: old.Size, Mime: tc.mime, CreatedBy: f.owner.ID})
				if err != nil {
					t.Fatal(err)
				}
				if err := rig.q.SetCurrentVersion(ctx, store.SetCurrentVersionParams{ID: node.ID, CurrentVersionID: ver.ID}); err != nil {
					t.Fatal(err)
				}
			}
			id := uuidString(node.ID)
			var read agentTextDTO
			mustTool(t, owner, "read_text_file", map[string]any{"node_id": id}, &read)
			if read.Markdown || read.Text != tc.before || read.ETag != blob.Hash([]byte(tc.before)) {
				t.Fatalf("source read changed bytes: %+v", read)
			}
			base := len(rig.versions(t, node))
			mustTool(t, owner, "update_text_file", map[string]any{"node_id": id, "text": tc.after, "base_etag": read.ETag}, nil)
			versions := rig.versions(t, node)
			if len(versions) != base+1 || versions[0].CreatedBy != f.owner.ID || rig.content(t, versions[0]) != tc.after {
				t.Fatalf("text replacement changed content/authorship/history: %+v", versions)
			}
			if tc.mime != "" && versions[0].Mime != tc.mime {
				t.Fatalf("content edit changed MIME from %q to %q", tc.mime, versions[0].Mime)
			}
			mustTool(t, owner, "read_text_file", map[string]any{"node_id": id}, &read)
			if read.Text != tc.after || read.ETag != blob.Hash([]byte(tc.after)) {
				t.Fatalf("updated source differs: %+v", read)
			}
			mustTool(t, owner, "update_text_file", map[string]any{"node_id": id, "text": tc.after, "base_etag": read.ETag}, nil)
			if len(rig.versions(t, node)) != base+1 {
				t.Fatal("identical source made a new version")
			}
			if text, isErr := callTool(t, owner, "update_text_file", map[string]any{"node_id": id, "text": tc.before, "base_etag": blob.Hash([]byte(tc.before))}); !isErr || !strings.Contains(text, "409") {
				t.Fatalf("stale source was accepted: %s", text)
			}
			if text, isErr := callTool(t, owner, "read_document", map[string]any{"node_id": id}); !isErr || !strings.Contains(text, "415") {
				t.Fatalf("source was presented as a Markdown document: %s", text)
			}
			if text, isErr := callTool(t, owner, "update_document", map[string]any{"node_id": id, "markdown": "# Wrong API\n", "base_etag": read.ETag}); !isErr || !strings.Contains(text, "415") {
				t.Fatalf("Markdown write replaced source: %s", text)
			}
			if len(rig.versions(t, node)) != base+1 || rig.content(t, rig.versions(t, node)[0]) != tc.after {
				t.Fatal("a refused write changed source or history")
			}
		})
	}
}

func TestMcpTextFilesRespectPermissionsLocksAndLimits(t *testing.T) {
	rig := newMCPRig(t)
	f := rig.collabRig.fixture(t, "mcptextaccess", "# Document\n")
	ctx := context.Background()
	node := rig.putMarkdown(t, f.space, f.owner, "table.csv", "name,value\r\nold,1\r\n")
	grantDirect(t, rig.srv, node, f.editor, f.owner, share.GrantCaps{Level: share.LevelEdit})
	grantDirect(t, rig.srv, node, f.viewer, f.owner, share.GrantCaps{Level: share.LevelView})
	owner := rig.agent(t, f.owner)
	viewer := rig.agent(t, f.viewer)
	stranger := rig.agent(t, f.stranger)
	id := uuidString(node.ID)
	var read agentTextDTO
	mustTool(t, viewer, "read_text_file", map[string]any{"node_id": id}, &read)
	if text, isErr := callTool(t, viewer, "update_text_file", map[string]any{"node_id": id, "text": "no", "base_etag": read.ETag}); !isErr || !strings.Contains(text, "403") {
		t.Fatalf("view-only grant wrote source: %s", text)
	}
	if text, isErr := callTool(t, stranger, "read_text_file", map[string]any{"node_id": id}); !isErr || !strings.Contains(text, "403") {
		t.Fatalf("unshared source was exposed: %s", text)
	}
	if n, err := rig.q.AcquireLock(ctx, store.AcquireLockParams{NodeID: node.ID, LockToken: "wopi_csv_editor"}); err != nil || n != 1 {
		t.Fatalf("take editor lock: %d %v", n, err)
	}
	for _, name := range []string{"read_text_file", "update_text_file"} {
		args := map[string]any{"node_id": id}
		if name == "update_text_file" {
			args["text"], args["base_etag"] = "not saved", read.ETag
		}
		if text, isErr := callTool(t, owner, name, args); !isErr || !strings.Contains(text, "409") {
			t.Fatalf("%s ignored the live editor: %s", name, text)
		}
	}
	lock, err := rig.q.GetLock(ctx, node.ID)
	if err != nil || lock.LockToken != "wopi_csv_editor" {
		t.Fatalf("agent stole the editor lock: %+v %v", lock, err)
	}
	if err := rig.q.DeleteLock(ctx, store.DeleteLockParams{NodeID: node.ID, LockToken: lock.LockToken}); err != nil {
		t.Fatal(err)
	}
	mustTool(t, rig.agent(t, f.editor), "update_text_file", map[string]any{"node_id": id, "text": "", "base_etag": read.ETag}, nil)
	if got := rig.content(t, rig.versions(t, node)[0]); got != "" {
		t.Fatalf("empty content was not an intentional replacement: %q", got)
	}
	if _, err := rig.q.GetLock(ctx, node.ID); err == nil {
		t.Fatal("successful text update leaked its lock")
	}
	for _, tc := range []struct {
		name, content, status string
	}{
		{"binary.txt", "PK\x03\x04\x00\x00", "415"},
		{"invalid.json", "{\xff}", "415"},
		{"binary.pdf", "%PDF-1.7\nASCII is still a PDF\n", "415"},
		{"binary-ascii.txt", "%PDF-1.7\nASCII is still a PDF\n", "415"},
		{"oversized.txt", strings.Repeat("x", maxAgentTextBytes+1), "413"},
	} {
		n := rig.putMarkdown(t, f.space, f.owner, tc.name, tc.content)
		if text, isErr := callTool(t, owner, "read_text_file", map[string]any{"node_id": uuidString(n.ID)}); !isErr || !strings.Contains(text, tc.status) {
			t.Fatalf("read %s: %.200s", tc.name, text)
		}
		if text, isErr := callTool(t, owner, "update_text_file", map[string]any{"node_id": uuidString(n.ID), "text": "replacement", "base_etag": blob.Hash([]byte(tc.content))}); !isErr || !strings.Contains(text, tc.status) {
			t.Fatalf("replaced %s as text: %.200s", tc.name, text)
		}
		if len(rig.versions(t, n)) != 1 || rig.content(t, rig.versions(t, n)[0]) != tc.content {
			t.Fatalf("refused operation changed %s", tc.name)
		}
	}
	if text, isErr := callTool(t, owner, "update_text_file", map[string]any{"node_id": id, "text": strings.Repeat("x", maxAgentTextBytes+1), "base_etag": blob.Hash(nil)}); !isErr || !strings.Contains(text, "413") {
		t.Fatalf("oversized replacement accepted: %.200s", text)
	}
	// HTML-sensitive ASCII escapes to six JSON bytes per decoded byte. The
	// advertised 1 MiB limit is on file bytes, not on its JSON representation.
	boundary := rig.putMarkdown(t, f.space, f.owner, "boundary.txt", strings.Repeat(">", maxAgentTextBytes))
	mustTool(t, owner, "read_text_file", map[string]any{"node_id": uuidString(boundary.ID)}, &read)
	mustTool(t, owner, "update_text_file", map[string]any{"node_id": uuidString(boundary.ID), "text": strings.Repeat("<", maxAgentTextBytes), "base_etag": read.ETag}, nil)
	if got := rig.content(t, rig.versions(t, boundary)[0]); got != strings.Repeat("<", maxAgentTextBytes) {
		t.Fatal("maximum-size text was truncated or changed")
	}
	if text, isErr := callTool(t, owner, "update_text_file", map[string]any{"node_id": uuidString(boundary.ID), "text": strings.Repeat("<", maxAgentTextBytes+1), "base_etag": blob.Hash([]byte(strings.Repeat("<", maxAgentTextBytes)))}); !isErr || !strings.Contains(text, "413") {
		t.Fatalf("escaped oversized text was accepted: %.200s", text)
	}
	if versions := rig.versions(t, boundary); len(versions) != 2 || rig.content(t, versions[0]) != strings.Repeat("<", maxAgentTextBytes) {
		t.Fatal("oversized text refusal changed original bytes or history")
	}
}

func TestMcpTextFileMarkdownStillReseedsCollaboration(t *testing.T) {
	rig := newMCPRig(t)
	f := rig.collabRig.fixture(t, "mcptextmd", "# Plan\n\nOriginal.\n")
	owner := rig.agent(t, f.owner)
	room := rig.mustSession(t, f.owner, f.doc)
	peer := rig.mustDial(t, f.owner, room.Room)
	var read agentTextDTO
	mustTool(t, owner, "read_text_file", map[string]any{"node_id": uuidString(f.doc.ID)}, &read)
	if !read.Markdown {
		t.Fatal("Markdown was treated as an ordinary text file")
	}
	if !peer.edit(t, "# Plan\n\nUnsaved edit.\n") {
		t.Fatal("room refused edit")
	}
	if text, isErr := callTool(t, owner, "update_text_file", map[string]any{"node_id": uuidString(f.doc.ID), "text": "# Replacement\n", "base_etag": read.ETag}); !isErr || !strings.Contains(text, "409") {
		t.Fatalf("text API overwrote unsaved Markdown: %s", text)
	}
	mustTool(t, owner, "read_text_file", map[string]any{"node_id": uuidString(f.doc.ID)}, &read)
	mustTool(t, owner, "update_text_file", map[string]any{"node_id": uuidString(f.doc.ID), "text": "# Replacement\n", "base_etag": read.ETag}, nil)
	waitUntil(t, "text API replacement in Markdown room", func() bool { return strings.Contains(peer.markdown(), "# Replacement") })
	if text, isErr := callTool(t, owner, "update_text_file", map[string]any{"node_id": uuidString(f.doc.ID), "text": "<script>not Markdown</script>\n", "base_etag": blob.Hash([]byte("# Replacement\n"))}); !isErr || !strings.Contains(text, "422") {
		t.Fatalf("text API bypassed Markdown representability: %s", text)
	}
}

// A real blob store with a synchronization point lets the test inspect the
// actual SQL lock while an MCP replacement is between its read and commit.
type pausedTextBlobStore struct {
	blob.Store
	reached chan struct{}
	proceed chan struct{}
}

func (b *pausedTextBlobStore) Put(ctx context.Context, r io.Reader) (string, int64, error) {
	close(b.reached)
	select {
	case <-b.proceed:
		return b.Store.Put(ctx, r)
	case <-ctx.Done():
		return "", 0, ctx.Err()
	}
}

func TestMcpTextUpdateHoldsEditorLockThroughCommit(t *testing.T) {
	rig := newMCPRig(t)
	f := rig.collabRig.fixture(t, "mcptextlock", "# Document\n")
	node := rig.putMarkdown(t, f.space, f.owner, "locked.csv", "a,b\r\n1,2\r\n")
	owner := rig.agent(t, f.owner)
	paused := &pausedTextBlobStore{Store: rig.srv.blobs, reached: make(chan struct{}), proceed: make(chan struct{})}
	rig.srv.fs = fs.NewService(rig.q, paused)
	var release sync.Once
	unpause := func() { release.Do(func() { close(paused.proceed) }) }
	defer unpause()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	type outcome struct {
		result *mcp.CallToolResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := owner.CallTool(ctx, &mcp.CallToolParams{Name: "update_text_file", Arguments: map[string]any{
			"node_id": uuidString(node.ID), "text": "a,b\r\n3,4\r\n", "base_etag": blob.Hash([]byte("a,b\r\n1,2\r\n")),
		}})
		done <- outcome{result, err}
	}()
	select {
	case <-paused.reached:
	case <-ctx.Done():
		t.Fatal("update did not reach its conditional write")
	}
	lock, err := rig.q.GetLock(ctx, node.ID)
	if err != nil || !strings.HasPrefix(lock.LockToken, "agent-text:") {
		t.Fatalf("update has no unique text lock: %+v %v", lock, err)
	}
	if n, err := rig.q.AcquireLock(ctx, store.AcquireLockParams{NodeID: node.ID, LockToken: "wopi_csv_late_editor"}); err != nil || n != 0 {
		t.Fatalf("editor could take the in-flight update's lock: %d %v", n, err)
	}
	unpause()
	select {
	case out := <-done:
		if out.err != nil || out.result == nil || out.result.IsError {
			t.Fatalf("update failed: %+v %v", out.result, out.err)
		}
	case <-ctx.Done():
		t.Fatal("update did not complete")
	}
	if n, err := rig.q.AcquireLock(ctx, store.AcquireLockParams{NodeID: node.ID, LockToken: "wopi_csv_late_editor"}); err != nil || n != 1 {
		t.Fatalf("completed update did not release its lock: %d %v", n, err)
	}
	_ = rig.q.DeleteLock(ctx, store.DeleteLockParams{NodeID: node.ID, LockToken: "wopi_csv_late_editor"})
	if got := rig.content(t, rig.versions(t, node)[0]); got != "a,b\r\n3,4\r\n" {
		t.Fatalf("committed CSV differs: %q", got)
	}
}

// Both writers have resolved the same node before their bytes are admitted.
// The storage transaction must serialize version numbering and compare the
// expected hash under that same lock, not before it.
type gatedTextReader struct {
	io.Reader
	ctx     context.Context
	reached chan<- struct{}
	proceed <-chan struct{}
	once    sync.Once
}

func (r *gatedTextReader) Read(p []byte) (int, error) {
	r.once.Do(func() {
		r.reached <- struct{}{}
	})
	select {
	case <-r.proceed:
		return r.Reader.Read(p)
	case <-r.ctx.Done():
		return 0, r.ctx.Err()
	}
}

func TestConditionalFileCommitSerializesCompetingWriters(t *testing.T) {
	rig := newMCPRig(t)
	f := rig.collabRig.fixture(t, "textcas", "# Document\n")
	for _, conditional := range []bool{true, false} {
		name := "ordinary.txt"
		if conditional {
			name = "conditional.txt"
		}
		t.Run(name, func(t *testing.T) {
			node := rig.putMarkdown(t, f.space, f.owner, name, "original\n")
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			reached := make(chan struct{}, 2)
			proceed := make(chan struct{})
			var release sync.Once
			unpause := func() { release.Do(func() { close(proceed) }) }
			defer unpause()
			type outcome struct {
				version store.NodeVersion
				err     error
			}
			done := make(chan outcome, 2)
			for _, body := range []string{"first\n", "second\n"} {
				go func(body string) {
					r := &gatedTextReader{Reader: strings.NewReader(body), ctx: ctx, reached: reached, proceed: proceed}
					var ver store.NodeVersion
					var err error
					if conditional {
						_, ver, err = rig.srv.fs.PutFileIfCurrent(ctx, node.SpaceID, node.ParentID, node.ID, node.Name, r, f.owner.ID, blob.Hash([]byte("original\n")), nil)
					} else {
						_, ver, err = rig.srv.fs.PutFile(ctx, node.SpaceID, node.ParentID, node.ID, node.Name, r, f.owner.ID)
					}
					done <- outcome{ver, err}
				}(body)
			}
			for range 2 {
				select {
				case <-reached:
				case <-ctx.Done():
					t.Fatal("writers did not reach the shared base")
				}
			}
			unpause()
			success, stale := 0, 0
			for range 2 {
				select {
				case out := <-done:
					switch {
					case out.err == nil:
						success++
					case errors.Is(out.err, fs.ErrStaleVersion):
						stale++
					default:
						t.Fatalf("concurrent commit failed unexpectedly: %v", out.err)
					}
				case <-ctx.Done():
					t.Fatal("writers did not finish")
				}
			}
			if conditional && (success != 1 || stale != 1) || !conditional && (success != 2 || stale != 0) {
				t.Fatalf("conditional=%t: %d committed, %d stale", conditional, success, stale)
			}
			versions := rig.versions(t, node)
			if len(versions) != 1+success || versions[0].VersionNo != int32(1+success) {
				t.Fatalf("concurrency corrupted version history: %+v", versions)
			}
			current, err := rig.q.GetCurrentVersion(ctx, node.ID)
			if err != nil || current.ID != versions[0].ID {
				t.Fatalf("current version is not the committed head: %+v %v", current, err)
			}
		})
	}
}

func TestConditionalFileCommitRefusesChangedMetadataAndTrash(t *testing.T) {
	rig := newMCPRig(t)
	f := rig.collabRig.fixture(t, "textcasmeta", "# Document\n")
	ctx := context.Background()
	node := rig.putMarkdown(t, f.space, f.owner, "metadata.txt", "original\n")
	base := rig.versions(t, node)[0]
	for _, snapshot := range []struct {
		space, parent pgtype.UUID
		name          string
	}{
		{f.doc.ID, node.ParentID, node.Name},
		{node.SpaceID, f.doc.ID, node.Name},
		{node.SpaceID, node.ParentID, "renamed.txt"},
	} {
		if _, _, err := rig.srv.fs.PutFileIfCurrent(ctx, snapshot.space, snapshot.parent, node.ID, snapshot.name, strings.NewReader("replacement\n"), f.owner.ID, base.BlobHash, nil); !errors.Is(err, fs.ErrStaleVersion) {
			t.Fatalf("changed node metadata accepted: %v", err)
		}
	}
	if _, _, err := rig.srv.fs.PutFileIfCurrent(ctx, node.SpaceID, node.ParentID, node.ID, node.Name, strings.NewReader("replacement\n"), f.owner.ID, "", nil); !errors.Is(err, fs.ErrStaleVersion) {
		t.Fatalf("missing base hash accepted: %v", err)
	}
	if len(rig.versions(t, node)) != 1 {
		t.Fatal("refused metadata replacement wrote a version")
	}
	if err := rig.srv.fs.Delete(ctx, node.ID, f.owner.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := rig.srv.fs.PutFileIfCurrent(ctx, node.SpaceID, node.ParentID, node.ID, node.Name, strings.NewReader("replacement\n"), f.owner.ID, base.BlobHash, nil); !errors.Is(err, fs.ErrNotFound) {
		t.Fatalf("trashed file was replaced: %v", err)
	}
	dir, err := rig.srv.fs.Mkdir(ctx, f.space.ID, pgtype.UUID{}, "trashed-parent", f.owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	buried, original, err := rig.srv.fs.PutFile(ctx, f.space.ID, dir.ID, pgtype.UUID{}, "buried.txt", strings.NewReader("buried\n"), f.owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := rig.srv.fs.Delete(ctx, dir.ID, f.owner.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := rig.srv.fs.PutFileIfCurrent(ctx, buried.SpaceID, buried.ParentID, buried.ID, buried.Name, strings.NewReader("replacement\n"), f.owner.ID, original.BlobHash, nil); !errors.Is(err, fs.ErrNotFound) {
		t.Fatalf("file buried in trash was replaced: %v", err)
	}
	if len(rig.versions(t, buried)) != 1 {
		t.Fatal("refused buried replacement changed version history")
	}
}

func TestMcpEditRechecksAccessAndLockAtCommit(t *testing.T) {
	for _, format := range []string{"text", "office"} {
		for _, scenario := range []string{"permission", "lock"} {
			t.Run(format+"/"+scenario, func(t *testing.T) {
				rig := newMCPRig(t)
				f := rig.collabRig.fixture(t, "commitguard"+format+scenario, "# Fixture\n")
				before, after := "original\n", "replacement\n"
				name := "source.txt"
				if format == "office" {
					name = "source.docx"
					before, after = string(officeTestDOCX(t, "original")), string(officeTestDOCX(t, "replacement"))
					rig.srv.officeEditor = officeTestEngine(func(context.Context, officeedit.Request) (officeedit.Response, error) {
						return officeedit.Response{Content: []byte(after)}, nil
					})
				}
				node := rig.putMarkdown(t, f.space, f.owner, name, before)
				grantDirect(t, rig.srv, node, f.editor, f.owner, share.GrantCaps{Level: share.LevelEdit})
				editor := rig.agent(t, f.editor)
				paused := &pausedTextBlobStore{Store: rig.srv.blobs, reached: make(chan struct{}), proceed: make(chan struct{})}
				rig.srv.fs = fs.NewService(rig.q, paused)
				var release sync.Once
				unpause := func() { release.Do(func() { close(paused.proceed) }) }
				defer unpause()
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				type outcome struct {
					result *mcp.CallToolResult
					err    error
				}
				done := make(chan outcome, 1)
				go func() {
					tool := "update_text_file"
					args := map[string]any{"node_id": uuidString(node.ID), "text": after, "base_etag": blob.Hash([]byte(before))}
					if format == "office" {
						tool, args = "edit_office_file", officeEditArgs(node, blob.Hash([]byte(before)))
					}
					result, err := editor.CallTool(ctx, &mcp.CallToolParams{Name: tool, Arguments: args})
					done <- outcome{result, err}
				}()
				select {
				case <-paused.reached:
				case <-ctx.Done():
					t.Fatal("edit did not reach blob storage")
				}
				wantStatus := "403"
				if scenario == "permission" {
					_, err := rig.srv.share.UpsertDirect(ctx, node.ID, f.editor.ID, share.GrantCaps{Level: share.LevelView}, pgtype.Timestamptz{}, f.owner.ID, false)
					if err != nil {
						t.Fatal(err)
					}
				} else {
					wantStatus = "409"
					if err := rig.q.UpsertLock(ctx, store.UpsertLockParams{NodeID: node.ID, LockToken: "foreign-editor"}); err != nil {
						t.Fatal(err)
					}
				}
				unpause()
				select {
				case got := <-done:
					encoded, _ := json.Marshal(got.result)
					if got.err != nil || got.result == nil || !got.result.IsError || !strings.Contains(string(encoded), wantStatus) {
						t.Fatalf("commit ignored %s change: %+v %v", scenario, got.result, got.err)
					}
				case <-ctx.Done():
					t.Fatal("edit did not finish")
				}
				versions := rig.versions(t, node)
				if len(versions) != 1 || rig.content(t, versions[0]) != before {
					t.Fatal("refused edit changed content or history")
				}
			})
		}
	}
}
