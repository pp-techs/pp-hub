package httpapi

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/nhatbui/pp-hub/apps/server/internal/collab"
	"github.com/nhatbui/pp-hub/apps/server/internal/share"
)

type fakeSaver struct {
	saved   []string
	queued  bool
	err     error
	nothing map[string]bool
}

func (f *fakeSaver) Save(_ context.Context, key string) (bool, error) {
	if f.err != nil {
		return false, f.err
	}
	f.saved = append(f.saved, key)
	return f.queued && !f.nothing[key], nil
}

func TestSaveNodeAsksTheEditorHoldingTheFile(t *testing.T) {
	rig := newCollabRig(t, collab.Config{})
	ctx := context.Background()
	f := rig.fixture(t, "save-node", "# Draft\n")
	office := rig.putMarkdown(t, f.space, f.owner, "report.docx", "v1")
	grantDirect(t, rig.srv, office, f.editor, f.owner, share.GrantCaps{Level: share.LevelEdit})
	grantDirect(t, rig.srv, office, f.reader, f.owner, share.GrantCaps{Level: share.LevelView})
	saver := &fakeSaver{queued: true}
	rig.srv.officeSaver = saver
	path := "/api/v1/nodes/" + uuidString(office.ID) + "/save"

	// Nobody has it open: nothing to ask, and the Document Server is not
	// bothered.
	res := rig.do(t, f.editor, http.MethodPost, path, nil)
	if res.code != http.StatusOK || decodeJSON[saveResultDTO](t, res).State != saveNothing {
		t.Fatalf("unlocked file: %d %s, want 200 unchanged", res.code, res.body)
	}
	if len(saver.saved) != 0 {
		t.Fatalf("Document Server asked about a file nobody has open: %v", saver.saved)
	}

	// Open in OnlyOffice: the session key is the lock, and the save is queued.
	if err := rig.srv.wopi.Lock(ctx, office.ID, "wopi_save_test"); err != nil {
		t.Fatal(err)
	}
	res = rig.do(t, f.editor, http.MethodPost, path, nil)
	if res.code != http.StatusOK || decodeJSON[saveResultDTO](t, res).State != saveQueued {
		t.Fatalf("open office file: %d %s, want 200 queued", res.code, res.body)
	}
	if len(saver.saved) != 1 || saver.saved[0] != "wopi_save_test" {
		t.Fatalf("saved keys = %v, want [wopi_save_test]", saver.saved)
	}

	// Edited since the last save: the Document Server says there is nothing.
	saver.nothing = map[string]bool{"wopi_save_test": true}
	res = rig.do(t, f.editor, http.MethodPost, path, nil)
	if decodeJSON[saveResultDTO](t, res).State != saveNothing {
		t.Fatalf("no edits since last save: %s, want unchanged", res.body)
	}

	// A reader cannot have typed anything, and a stranger cannot see the file.
	if res := rig.do(t, f.reader, http.MethodPost, path, nil); res.code != http.StatusForbidden {
		t.Errorf("reader: %d, want 403", res.code)
	}
	if res := rig.do(t, f.stranger, http.MethodPost, path, nil); res.code != http.StatusNotFound && res.code != http.StatusForbidden {
		t.Errorf("stranger: %d, want 404 or 403", res.code)
	}

	// The Document Server failing is a bad gateway, not a silent success.
	saver.err = errors.New("command service timeout")
	if res := rig.do(t, f.editor, http.MethodPost, path, nil); res.code != http.StatusBadGateway {
		t.Errorf("failing Document Server: %d, want 502", res.code)
	}

	// No Document Server configured at all.
	rig.srv.officeSaver = nil
	if res := rig.do(t, f.editor, http.MethodPost, path, nil); res.code != http.StatusConflict {
		t.Errorf("no Document Server: %d, want 409", res.code)
	}
}

func TestSaveNodeFlushesABlockDocumentsRoom(t *testing.T) {
	rig := newCollabRig(t, collab.Config{})
	f := rig.fixture(t, "save-room", "# Draft\n")
	sess := rig.mustSession(t, f.owner, f.doc)
	peer := rig.mustDial(t, f.owner, sess.Room)
	defer peer.close()
	if !peer.edit(t, "# Draft\n\nedited\n") {
		t.Fatal("edit was not accepted")
	}

	res := rig.do(t, f.owner, http.MethodPost, "/api/v1/nodes/"+uuidString(f.doc.ID)+"/save", nil)
	if res.code != http.StatusOK || decodeJSON[saveResultDTO](t, res).State != saveWritten {
		t.Fatalf("save: %d %s, want 200 saved", res.code, res.body)
	}
	// The room's quiet period has not passed; the save is why this is already
	// a version.
	if n := len(rig.versions(t, f.doc)); n != 2 {
		t.Fatalf("%d versions after saving, want 2", n)
	}
}

func TestAdminOpenFiles(t *testing.T) {
	rig := newCollabRig(t, collab.Config{})
	ctx := context.Background()
	f := rig.fixture(t, "open-files", "# Draft\n")
	admin := feedUser(t, rig.q, "open-files-admin")
	if _, err := rig.srv.store.DB().Exec(ctx, `UPDATE users SET is_admin = true WHERE id = $1`, admin.ID); err != nil {
		t.Fatal(err)
	}
	admin.IsAdmin = true
	rig.login(t, admin)

	sheet := rig.putMarkdown(t, f.space, f.owner, "plan.xlsx", "v1")
	other := rig.putMarkdown(t, f.space, f.owner, "mounted.txt", "v1")
	if err := rig.srv.wopi.Lock(ctx, sheet.ID, "wopi_admin_list"); err != nil {
		t.Fatal(err)
	}
	if err := rig.srv.wopi.Lock(ctx, f.doc.ID, "collab:admin-list"); err != nil {
		t.Fatal(err)
	}
	if err := rig.srv.wopi.Lock(ctx, other.ID, "opaquetoken"); err != nil {
		t.Fatal(err)
	}
	saver := &fakeSaver{queued: true}
	rig.srv.officeSaver = saver

	// Only an administrator may see or act on other people's open files.
	for _, c := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/admin/open-files"},
		{http.MethodPost, "/api/v1/admin/open-files/save"},
		{http.MethodPost, "/api/v1/admin/open-files/" + uuidString(sheet.ID) + "/save"},
	} {
		if res := rig.do(t, f.owner, c.method, c.path, nil); res.code != http.StatusForbidden {
			t.Errorf("%s %s as owner: %d, want 403", c.method, c.path, res.code)
		}
	}

	res := rig.do(t, admin, http.MethodGet, "/api/v1/admin/open-files", nil)
	if res.code != http.StatusOK {
		t.Fatalf("list: %d %s", res.code, res.body)
	}
	editors := map[string]string{}
	for _, row := range decodeJSON[[]openFileDTO](t, res) {
		editors[row.NodeID] = row.Editor
		if row.NodeID == uuidString(sheet.ID) && (row.Name != "plan.xlsx" || row.SpaceName == "" || row.VersionNo != 1 || row.RefreshedAt == "") {
			t.Errorf("office row = %+v", row)
		}
	}
	want := map[string]string{
		uuidString(sheet.ID): "office",
		uuidString(f.doc.ID): "blocks",
		uuidString(other.ID): "other",
	}
	for id, editor := range want {
		if editors[id] != editor {
			t.Errorf("editor of %s = %q, want %q", id, editors[id], editor)
		}
	}

	// One file: only the editor holding it is asked.
	res = rig.do(t, admin, http.MethodPost, "/api/v1/admin/open-files/"+uuidString(sheet.ID)+"/save", nil)
	if res.code != http.StatusOK || decodeJSON[saveResultDTO](t, res).State != saveQueued {
		t.Fatalf("save one: %d %s, want queued", res.code, res.body)
	}
	if len(saver.saved) != 1 || saver.saved[0] != "wopi_admin_list" {
		t.Fatalf("saved = %v, want only wopi_admin_list", saver.saved)
	}

	// Everything: the office file is queued, the room written, the other
	// holder left alone. Locks other tests hold may be in the totals, so they
	// are lower bounds.
	saver.saved = nil
	res = rig.do(t, admin, http.MethodPost, "/api/v1/admin/open-files/save", nil)
	if res.code != http.StatusOK {
		t.Fatalf("save all: %d %s", res.code, res.body)
	}
	got := decodeJSON[adminSaveDTO](t, res)
	if got.Queued < 1 || got.Saved < 1 || got.Unchanged < 1 {
		t.Errorf("totals = %+v, want at least one queued, saved and unchanged", got)
	}
	found := false
	for _, k := range saver.saved {
		found = found || k == "wopi_admin_list"
	}
	if !found {
		t.Errorf("saved = %v, missing wopi_admin_list", saver.saved)
	}

	// A failing editor is counted and does not stop the rest.
	saver.err = errors.New("command service timeout")
	res = rig.do(t, admin, http.MethodPost, "/api/v1/admin/open-files/save", nil)
	got = decodeJSON[adminSaveDTO](t, res)
	if res.code != http.StatusOK || got.Failed < 1 || got.Saved < 1 {
		t.Errorf("with a failing Document Server: %d %+v, want 200 with a failure and a saved room", res.code, got)
	}
}
