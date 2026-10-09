package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/auth"
	"github.com/nhatbui/pp-hub/apps/server/internal/collab"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
	"github.com/nhatbui/pp-hub/apps/server/internal/sysconfig"
)

// WebDAV (dav.go) end to end: the production router, real tokens, real
// grants, rooms and versions. Skips without PP_TEST_DATABASE_URL like the
// rest of this package's database tests.

type davRig struct {
	*collabRig
}

func newDavRig(t *testing.T) davRig {
	t.Helper()
	rig := newCollabRig(t, collab.Config{})
	rig.srv.sysconfig = sysconfig.NewService(rig.q, nil)
	d := davRig{collabRig: rig}
	d.setSwitch(t, sysconfig.KeyWebDAVEnabled, true)
	return d
}

func (rig davRig) setSwitch(t *testing.T, key string, on bool) {
	t.Helper()
	socialRig{collabRig: rig.collabRig}.setSwitch(t, key, on)
}

// token mints a personal token of purpose for u.
func (rig davRig) token(t *testing.T, u store.User, purpose auth.TokenPurpose) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	_, raw, err := rig.srv.auth.CreateAgentToken(req, u, "test", time.Hour, purpose)
	if err != nil {
		t.Fatalf("mint token: %v", err)
	}
	return raw
}

// dav sends one WebDAV request with token as the Basic password.
func (rig davRig) dav(t *testing.T, token, method, p string, hdr map[string]string, body string) rigResponse {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, rig.url+davPrefix+(&url.URL{Path: p}).EscapedPath(), r)
	if err != nil {
		t.Fatal(err)
	}
	req.SetBasicAuth("someone@example.com", token)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, p, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return rigResponse{code: resp.StatusCode, header: resp.Header, body: b}
}

func (rig davRig) move(t *testing.T, token, from, to string) rigResponse {
	t.Helper()
	return rig.dav(t, token, "MOVE", from, map[string]string{"Destination": rig.url + davPrefix + (&url.URL{Path: to}).EscapedPath(), "Overwrite": "T"}, "")
}

// live is the space's live nodes whose name match accepts, anywhere in its
// tree, sub-pages included.
func (rig davRig) live(t *testing.T, sp store.Space, match func(string) bool) []store.Node {
	t.Helper()
	var out []store.Node
	queue := []pgtype.UUID{{}}
	for len(queue) > 0 {
		kids, err := rig.srv.fs.List(context.Background(), sp.ID, queue[0])
		if err != nil {
			t.Fatal(err)
		}
		queue = queue[1:]
		for _, n := range kids {
			if match(n.Name) {
				out = append(out, n)
			}
			queue = append(queue, n.ID)
		}
	}
	return out
}

func named(name string) func(string) bool { return func(n string) bool { return n == name } }

func (rig davRig) current(t *testing.T, n store.Node) string {
	t.Helper()
	ver, err := rig.q.GetCurrentVersion(context.Background(), n.ID)
	if err != nil {
		t.Fatal(err)
	}
	return rig.content(t, ver)
}

func TestDavTokensOpenOnlyTheirEndpoint(t *testing.T) {
	rig := newDavRig(t)
	f := rig.fixture(t, "davtok", "# hi\n")
	dav := rig.token(t, f.owner, auth.TokenPurposeDAV)
	mcpTok := rig.token(t, f.owner, auth.TokenPurposeMCP)

	if res := rig.dav(t, dav, "PROPFIND", "/", map[string]string{"Depth": "1"}, ""); res.code != 207 {
		t.Fatalf("PROPFIND with a dav token = %d, want 207: %s", res.code, res.body)
	}
	if res := rig.dav(t, mcpTok, "PROPFIND", "/", map[string]string{"Depth": "1"}, ""); res.code != http.StatusUnauthorized {
		t.Fatalf("PROPFIND with an mcp token = %d, want 401", res.code)
	}
	if _, err := rig.srv.auth.AuthenticateAgentToken(context.Background(), dav, auth.TokenPurposeMCP); err == nil {
		t.Fatal("a dav token authenticated for MCP")
	}

	rig.setSwitch(t, sysconfig.KeyWebDAVEnabled, false)
	if res := rig.dav(t, dav, "PROPFIND", "/", map[string]string{"Depth": "1"}, ""); res.code != http.StatusNotFound {
		t.Fatalf("PROPFIND with WebDAV off = %d, want 404", res.code)
	}
}

func TestDavListsWhatTheCallerMayDownload(t *testing.T) {
	rig := newDavRig(t)
	f := rig.fixture(t, "davlist", "# shared\n")
	sp := "/" + f.space.Name

	reader := rig.dav(t, rig.token(t, f.reader, auth.TokenPurposeDAV), "PROPFIND", sp, map[string]string{"Depth": "1"}, "")
	if reader.code != 207 || !strings.Contains(string(reader.body), "davlist.md") {
		t.Fatalf("reader listing = %d %s, want the document", reader.code, reader.body)
	}
	viewerTok := rig.token(t, f.viewer, auth.TokenPurposeDAV)
	viewer := rig.dav(t, viewerTok, "PROPFIND", sp, map[string]string{"Depth": "1"}, "")
	if viewer.code != 207 || strings.Contains(string(viewer.body), "davlist.md") {
		t.Fatalf("view-only listing = %d %s, want no document", viewer.code, viewer.body)
	}
	if res := rig.dav(t, viewerTok, http.MethodGet, sp+"/davlist.md", nil, ""); res.code != http.StatusNotFound {
		t.Fatalf("view-only GET = %d, want 404", res.code)
	}
	if res := rig.dav(t, rig.token(t, f.reader, auth.TokenPurposeDAV), "PROPFIND", sp, map[string]string{"Depth": "infinity"}, ""); res.code != http.StatusForbidden {
		t.Fatalf("Depth: infinity = %d, want 403", res.code)
	}
}

func TestDavStaleWriteLandsAsConflictedCopy(t *testing.T) {
	rig := newDavRig(t)
	f := rig.fixture(t, "davconflict", "# doc\n")
	tok := rig.token(t, f.owner, auth.TokenPurposeDAV)
	file := rig.putMarkdown(t, f.space, f.owner, "notes.txt", "one")
	p := "/" + f.space.Name + "/notes.txt"

	if res := rig.dav(t, tok, http.MethodGet, p, nil, ""); res.code != 200 || string(res.body) != "one" {
		t.Fatalf("GET = %d %q", res.code, res.body)
	}
	rig.putVersion(t, file, f.editor, "two (from the web)")

	if res := rig.dav(t, tok, http.MethodPut, p, nil, "three (from the mount)"); res.code != http.StatusCreated {
		t.Fatalf("stale PUT = %d %s, want 201", res.code, res.body)
	}
	if got := rig.current(t, file); got != "two (from the web)" {
		t.Fatalf("the web's version was overwritten: %q", got)
	}
	copies := rig.live(t, f.space, func(n string) bool {
		return strings.HasPrefix(n, "notes (conflicted copy ") && strings.HasSuffix(n, ").txt")
	})
	if len(copies) != 1 {
		t.Fatalf("conflicted copies = %d, want 1", len(copies))
	}
	if got := rig.current(t, copies[0]); got != "three (from the mount)" {
		t.Fatalf("conflicted copy holds %q", got)
	}

	// Having read the current version, the next write is a plain version.
	rig.dav(t, tok, http.MethodGet, p, nil, "")
	if res := rig.dav(t, tok, http.MethodPut, p, nil, "four"); res.code != http.StatusNoContent {
		t.Fatalf("fresh PUT = %d %s, want 204", res.code, res.body)
	}
	if got := rig.current(t, file); got != "four" {
		t.Fatalf("current = %q, want four", got)
	}
}

func TestDavMarkdownWriteGoesThroughTheDocument(t *testing.T) {
	rig := newDavRig(t)
	f := rig.fixture(t, "davmd", "# Title\n\nfirst\n")
	tok := rig.token(t, f.editor, auth.TokenPurposeDAV)
	p := "/" + f.space.Name + "/davmd.md"

	if res := rig.dav(t, tok, http.MethodGet, p, nil, ""); res.code != 200 {
		t.Fatalf("GET = %d", res.code)
	}
	if res := rig.dav(t, tok, http.MethodPut, p, nil, "# Title\n\nsecond\n"); res.code != http.StatusNoContent {
		t.Fatalf("PUT = %d %s, want 204", res.code, res.body)
	}
	if got := rig.current(t, f.doc); !strings.Contains(got, "second") {
		t.Fatalf("document = %q", got)
	}
	if res := rig.dav(t, tok, http.MethodPut, p, nil, "<div>raw html</div>\n"); res.code != http.StatusUnprocessableEntity {
		t.Fatalf("lossy PUT = %d, want 422", res.code)
	}
}

// Word on Windows: write the new copy to a temporary name, set the original
// aside under another, put the copy in its place, delete the set-aside one.
func TestDavSafeSaveKeepsTheFile(t *testing.T) {
	rig := newDavRig(t)
	f := rig.fixture(t, "davsave", "# doc\n")
	tok := rig.token(t, f.owner, auth.TokenPurposeDAV)
	file := rig.putMarkdown(t, f.space, f.owner, "report.docx", "v1")
	sp := "/" + f.space.Name

	rig.dav(t, tok, http.MethodGet, sp+"/report.docx", nil, "")
	steps := []struct {
		name string
		res  func() rigResponse
		want int
	}{
		{"write temp", func() rigResponse { return rig.dav(t, tok, http.MethodPut, sp+"/~WRD0001.tmp", nil, "v2") }, 201},
		{"set aside", func() rigResponse { return rig.move(t, tok, sp+"/report.docx", sp+"/~WRL0002.tmp") }, 201},
		{"original looks gone", func() rigResponse { return rig.dav(t, tok, http.MethodGet, sp+"/report.docx", nil, "") }, 404},
		{"put in place", func() rigResponse { return rig.move(t, tok, sp+"/~WRD0001.tmp", sp+"/report.docx") }, 201},
		{"drop set-aside", func() rigResponse { return rig.dav(t, tok, http.MethodDelete, sp+"/~WRL0002.tmp", nil, "") }, 204},
	}
	for _, s := range steps {
		if res := s.res(); res.code != s.want {
			t.Fatalf("%s = %d %s, want %d", s.name, res.code, res.body, s.want)
		}
	}
	live := rig.live(t, f.space, named("report.docx"))
	if len(live) != 1 || live[0].ID != file.ID {
		t.Fatalf("report.docx nodes = %v, want the original %v", live, file.ID)
	}
	if got := rig.current(t, file); got != "v2" {
		t.Fatalf("current = %q, want v2", got)
	}
	if n := len(rig.versions(t, file)); n != 2 {
		t.Fatalf("versions = %d, want 2", n)
	}
	for _, scratch := range []string{"~WRD0001.tmp", "~WRL0002.tmp"} {
		if len(rig.live(t, f.space, named(scratch))) != 0 {
			t.Fatalf("%s became a node", scratch)
		}
	}
}

// An editor that writes a whole new file and renames it over the original.
func TestDavRenameOverIsANewVersion(t *testing.T) {
	rig := newDavRig(t)
	f := rig.fixture(t, "davover", "# doc\n")
	tok := rig.token(t, f.owner, auth.TokenPurposeDAV)
	file := rig.putMarkdown(t, f.space, f.owner, "plan.txt", "old")
	sp := "/" + f.space.Name

	rig.dav(t, tok, http.MethodGet, sp+"/plan.txt", nil, "")
	if res := rig.dav(t, tok, http.MethodPut, sp+"/plan.txt.new", nil, "new"); res.code != 201 {
		t.Fatalf("PUT temp = %d %s", res.code, res.body)
	}
	if res := rig.move(t, tok, sp+"/plan.txt.new", sp+"/plan.txt"); res.code != http.StatusNoContent {
		t.Fatalf("MOVE over = %d %s, want 204", res.code, res.body)
	}
	if got := rig.current(t, file); got != "new" {
		t.Fatalf("current = %q, want new", got)
	}
	if len(rig.live(t, f.space, named("plan.txt.new"))) != 0 {
		t.Fatal("the temporary copy survived")
	}
	trash, err := rig.srv.fs.ListDeleted(context.Background(), f.space.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range trash {
		if n.Name == "plan.txt.new" {
			t.Fatal("the temporary copy was trashed rather than purged")
		}
	}

	// Deleting the file and writing its replacement under its name.
	if res := rig.dav(t, tok, http.MethodDelete, sp+"/plan.txt", nil, ""); res.code != 204 {
		t.Fatalf("DELETE = %d", res.code)
	}
	if res := rig.dav(t, tok, http.MethodPut, sp+"/plan.txt", nil, "newer"); res.code != 201 {
		t.Fatalf("PUT after DELETE = %d %s", res.code, res.body)
	}
	live := rig.live(t, f.space, named("plan.txt"))
	if len(live) != 1 || live[0].ID != file.ID || rig.current(t, file) != "newer" {
		t.Fatalf("plan.txt after delete+write = %v, want the original node holding newer", live)
	}
}

func TestDavPagesFolderMakesSubPages(t *testing.T) {
	rig := newDavRig(t)
	f := rig.fixture(t, "davpages", "# Parent\n")
	tok := rig.token(t, f.owner, auth.TokenPurposeDAV)
	sp := "/" + f.space.Name

	if res := rig.dav(t, tok, "MKCOL", sp+"/davpages.pages", nil, ""); res.code != 201 {
		t.Fatalf("MKCOL pages = %d %s", res.code, res.body)
	}
	if res := rig.dav(t, tok, http.MethodPut, sp+"/davpages.pages/Child.md", nil, "# Child\n\nhello\n"); res.code != 201 {
		t.Fatalf("PUT sub-page = %d %s", res.code, res.body)
	}
	kids := rig.live(t, f.space, named("Child.md"))
	if len(kids) != 1 || kids[0].ParentID != f.doc.ID {
		t.Fatalf("Child.md = %v, want one sub-page of %v", kids, f.doc.ID)
	}
	if got := rig.current(t, kids[0]); !strings.Contains(got, "hello") {
		t.Fatalf("sub-page holds %q", got)
	}
	if res := rig.dav(t, tok, http.MethodPut, sp+"/davpages.pages/data.csv", nil, "a,b"); res.code != http.StatusForbidden {
		t.Fatalf("PUT non-markdown sub-page = %d, want 403", res.code)
	}
}

func TestDavRefusesNamesThatDifferOnlyInCase(t *testing.T) {
	rig := newDavRig(t)
	f := rig.fixture(t, "davcase", "# doc\n")
	tok := rig.token(t, f.owner, auth.TokenPurposeDAV)
	rig.putMarkdown(t, f.space, f.owner, "Budget.xlsx", "x")
	if res := rig.dav(t, tok, http.MethodPut, "/"+f.space.Name+"/budget.xlsx", nil, "y"); res.code != http.StatusConflict {
		t.Fatalf("PUT budget.xlsx beside Budget.xlsx = %d, want 409", res.code)
	}
}

func TestDavTokenPurposeOnTheTokenList(t *testing.T) {
	rig := newDavRig(t)
	f := rig.fixture(t, "davlistok", "# doc\n")
	res := rig.do(t, f.owner, http.MethodPost, "/api/v1/users/me/agent-tokens", map[string]any{"name": "laptop", "purpose": "dav"})
	if res.code != http.StatusCreated {
		t.Fatalf("create dav token = %d %s", res.code, res.body)
	}
	var created agentTokenDTO
	if err := json.Unmarshal(res.body, &created); err != nil || created.Purpose != "dav" || created.Token == "" {
		t.Fatalf("created = %+v (%v)", created, err)
	}
	rig.setSwitch(t, sysconfig.KeyWebDAVEnabled, false)
	if res := rig.do(t, f.owner, http.MethodPost, "/api/v1/users/me/agent-tokens", map[string]any{"name": "laptop", "purpose": "dav"}); res.code != http.StatusForbidden {
		t.Fatalf("create dav token with WebDAV off = %d, want 403", res.code)
	}
}

// Names that only look like an editor's temporary files are the person's
// files: they must not vanish.
func TestDavKeepsFilesWithTemporaryLookingNames(t *testing.T) {
	rig := newDavRig(t)
	f := rig.fixture(t, "davtmp", "# doc\n")
	tok := rig.token(t, f.owner, auth.TokenPurposeDAV)
	sp := "/" + f.space.Name
	for _, name := range []string{"results.tmp", "notes~", "~WRD0001.tmp"} {
		if res := rig.dav(t, tok, http.MethodPut, sp+"/"+name, nil, "data"); res.code != http.StatusCreated {
			t.Fatalf("PUT %s = %d %s", name, res.code, res.body)
		}
		if len(rig.live(t, f.space, named(name))) != 1 {
			t.Fatalf("%s is not a file", name)
		}
	}
	if res := rig.dav(t, tok, http.MethodPut, sp+"/.DS_Store", nil, "meta"); res.code != http.StatusCreated {
		t.Fatalf("PUT .DS_Store = %d", res.code)
	}
	if len(rig.live(t, f.space, named(".DS_Store"))) != 0 {
		t.Fatal(".DS_Store became a node")
	}
}

// vim: the file is moved to name~, the new copy written under its name, the
// backup deleted. The backup reads the old bytes meanwhile.
func TestDavBackupRenameKeepsTheFile(t *testing.T) {
	rig := newDavRig(t)
	f := rig.fixture(t, "davbak", "# doc\n")
	tok := rig.token(t, f.owner, auth.TokenPurposeDAV)
	file := rig.putMarkdown(t, f.space, f.owner, "notes.txt", "old")
	sp := "/" + f.space.Name

	if res := rig.move(t, tok, sp+"/notes.txt", sp+"/notes.txt~"); res.code != 201 {
		t.Fatalf("MOVE to backup = %d %s", res.code, res.body)
	}
	if res := rig.dav(t, tok, http.MethodPut, sp+"/notes.txt", nil, "new"); res.code != 201 {
		t.Fatalf("PUT new copy = %d %s", res.code, res.body)
	}
	if res := rig.dav(t, tok, http.MethodGet, sp+"/notes.txt~", nil, ""); res.code != 200 || string(res.body) != "old" {
		t.Fatalf("backup = %d %q, want the old bytes", res.code, res.body)
	}
	if res := rig.dav(t, tok, http.MethodDelete, sp+"/notes.txt~", nil, ""); res.code != 204 {
		t.Fatalf("DELETE backup = %d", res.code)
	}
	live := rig.live(t, f.space, named("notes.txt"))
	if len(live) != 1 || live[0].ID != file.ID || rig.current(t, file) != "new" {
		t.Fatalf("notes.txt = %v, want the original node holding new", live)
	}

	// Set aside and deleted with no save in between: the file is deleted.
	if res := rig.move(t, tok, sp+"/notes.txt", sp+"/notes.txt~"); res.code != 201 {
		t.Fatalf("MOVE to backup = %d", res.code)
	}
	if res := rig.dav(t, tok, http.MethodDelete, sp+"/notes.txt~", nil, ""); res.code != 204 {
		t.Fatalf("DELETE = %d", res.code)
	}
	if len(rig.live(t, f.space, named("notes.txt"))) != 0 {
		t.Fatal("deleting the set-aside name left the file in place")
	}
}

func TestDavAliasNeedsTheFileStillReachable(t *testing.T) {
	rig := newDavRig(t)
	f := rig.fixture(t, "davalias", "# shared\n")
	tok := rig.token(t, f.editor, auth.TokenPurposeDAV)
	sp := "/" + f.space.Name
	if res := rig.move(t, tok, sp+"/davalias.md", sp+"/~WRL0001.tmp"); res.code != 201 {
		t.Fatalf("set aside = %d %s", res.code, res.body)
	}
	shares, err := rig.srv.share.ListForNode(context.Background(), f.doc.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, sh := range shares {
		if sh.GranteeUserID == f.editor.ID {
			if _, err := rig.srv.share.Revoke(context.Background(), f.doc.ID, sh.ID); err != nil {
				t.Fatal(err)
			}
		}
	}
	if res := rig.dav(t, tok, http.MethodGet, sp+"/~WRL0001.tmp", nil, ""); res.code != http.StatusNotFound {
		t.Fatalf("alias after revoke = %d, want 404", res.code)
	}
}

func TestDavOverwriteNeverTrashesWhatItCannotReplace(t *testing.T) {
	rig := newDavRig(t)
	f := rig.fixture(t, "davover2", "# Parent\n")
	tok := rig.token(t, f.owner, auth.TokenPurposeDAV)
	sp := "/" + f.space.Name
	rig.dav(t, tok, "MKCOL", sp+"/davover2.pages", nil, "")
	rig.dav(t, tok, http.MethodPut, sp+"/davover2.pages/Child.md", nil, "# Child\n")
	rig.putMarkdown(t, f.space, f.owner, "other.txt", "x")

	if res := rig.move(t, tok, sp+"/other.txt", sp+"/davover2.pages"); res.code != http.StatusForbidden {
		t.Fatalf("MOVE onto a pages folder = %d, want 403", res.code)
	}
	if len(rig.live(t, f.space, named("davover2.md"))) != 1 || len(rig.live(t, f.space, named("Child.md"))) != 1 {
		t.Fatal("the document or its sub-page was trashed")
	}

	// A folder cannot cross spaces; the folder it would replace stays.
	other, err := rig.srv.spaces.Create(context.Background(), f.space.Name+"-b", f.owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rig.q.DeleteSpace(context.Background(), other.ID) })
	rig.dav(t, tok, "MKCOL", sp+"/proj", nil, "")
	rig.dav(t, tok, "MKCOL", "/"+other.Name+"/proj", nil, "")
	if res := rig.move(t, tok, sp+"/proj", "/"+other.Name+"/proj"); res.code != http.StatusForbidden {
		t.Fatalf("MOVE folder across spaces = %d, want 403", res.code)
	}
	if len(rig.live(t, other, named("proj"))) != 1 {
		t.Fatal("the destination folder was trashed")
	}
}

func TestDavDeletedFolderStaysDeleted(t *testing.T) {
	rig := newDavRig(t)
	f := rig.fixture(t, "davrmdir", "# doc\n")
	tok := rig.token(t, f.owner, auth.TokenPurposeDAV)
	sp := "/" + f.space.Name
	rig.dav(t, tok, "MKCOL", sp+"/build", nil, "")
	if res := rig.dav(t, tok, http.MethodDelete, sp+"/build", nil, ""); res.code != 204 {
		t.Fatalf("DELETE = %d", res.code)
	}
	if res := rig.dav(t, tok, http.MethodPut, sp+"/build", nil, "x"); res.code != http.StatusCreated {
		t.Fatalf("PUT where the folder was = %d %s, want 201", res.code, res.body)
	}
	live := rig.live(t, f.space, named("build"))
	if len(live) != 1 || live[0].Kind != store.NodeKindFile {
		t.Fatalf("build = %v, want one file", live)
	}
}

func TestDavLockNeedsEdit(t *testing.T) {
	rig := newDavRig(t)
	f := rig.fixture(t, "davlock", "# doc\n")
	p := "/" + f.space.Name + "/davlock.md"
	body := `<?xml version="1.0"?><D:lockinfo xmlns:D="DAV:"><D:lockscope><D:exclusive/></D:lockscope><D:locktype><D:write/></D:locktype></D:lockinfo>`
	hdr := map[string]string{"Content-Type": "application/xml", "Depth": "0", "Timeout": "Second-600"}
	if res := rig.dav(t, rig.token(t, f.reader, auth.TokenPurposeDAV), "LOCK", p, hdr, body); res.code != 423 {
		t.Fatalf("LOCK by a reader = %d, want 423", res.code)
	}
	if res := rig.dav(t, rig.token(t, f.editor, auth.TokenPurposeDAV), "LOCK", p, hdr, body); res.code != 200 {
		t.Fatalf("LOCK by an editor = %d %s, want 200", res.code, res.body)
	}
	if res := rig.dav(t, rig.token(t, f.owner, auth.TokenPurposeDAV), http.MethodPut, p, nil, "# owner\n"); res.code != 423 {
		t.Fatalf("PUT under someone else's lock = %d, want 423", res.code)
	}
}

func TestDavRefusesANameTakenByAHiddenFile(t *testing.T) {
	rig := newDavRig(t)
	f := rig.fixture(t, "davhidden", "# doc\n")
	// The viewer sees no davhidden.md: they may only view it.
	if res := rig.dav(t, rig.token(t, f.viewer, auth.TokenPurposeDAV), http.MethodPut, "/"+f.space.Name+"/davhidden.md", nil, "# mine\n"); res.code != http.StatusConflict {
		t.Fatalf("PUT over a hidden name = %d, want 409", res.code)
	}
	if n := len(rig.live(t, f.space, func(n string) bool { return strings.HasPrefix(n, "davhidden") })); n != 1 {
		t.Fatalf("davhidden* nodes = %d, want 1", n)
	}
}
