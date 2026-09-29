package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/blob"
	"github.com/nhatbui/pp-hub/apps/server/internal/collab"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// An image dropped into a block document is a tus upload naming the document
// (attach_to) and a row of the document's, served back only through it. What
// each person may do with it, and how long its bytes live, resolve against
// real grants and the blob reference triggers — a real Postgres or a loud
// skip, like the rest of this package's database tests.

// attach uploads body as an attachment of doc in one creation-with-upload
// request.
func (rig *collabRig) attach(t *testing.T, u store.User, doc store.Node, name string, body []byte) rigResponse {
	t.Helper()
	return rig.request(t, u, http.MethodPost, "/api/v1/uploads", map[string]string{
		"Tus-Resumable":   "1.0.0",
		"Upload-Length":   strconv.Itoa(len(body)),
		"Upload-Metadata": tusMeta("filename", name, "attach_to", uuidString(doc.ID)),
		"Content-Type":    "application/offset+octet-stream",
	}, bytes.NewReader(body))
}

func (rig *collabRig) attachment(t *testing.T, u store.User, doc store.Node, hash string) rigResponse {
	t.Helper()
	return rig.request(t, u, http.MethodGet, "/api/v1/nodes/"+uuidString(doc.ID)+"/attachments/"+hash, nil, nil)
}

// Attaching takes edit on a markdown document; reading takes download on it,
// and only the document holding a hash serves it.
func TestAttachmentsFollowTheirDocumentsAccess(t *testing.T) {
	rig := newCollabRig(t, collab.Config{})
	f := rig.fixture(t, "attach", "# Photos\n")
	other := rig.putMarkdown(t, f.space, f.owner, "other.md", "# Other\n")
	txt := rig.putMarkdown(t, f.space, f.owner, "notes.txt", "plain\n")
	png := onePixelPNG(t)
	pngHash := blob.Hash(png)

	for _, tc := range []struct {
		name   string
		u      store.User
		target store.Node
		want   int
	}{
		{"reader", f.reader, f.doc, http.StatusForbidden},
		{"view only", f.viewer, f.doc, http.StatusForbidden},
		{"member without a grant", f.stranger, f.doc, http.StatusForbidden},
		{"not a markdown document", f.owner, txt, http.StatusBadRequest},
	} {
		if res := rig.attach(t, tc.u, tc.target, "photo.png", png); res.code != tc.want {
			t.Fatalf("attach as %s: %d, want %d; body=%s", tc.name, res.code, tc.want, res.body)
		}
	}

	res := rig.attach(t, f.editor, f.doc, "photo.png", png)
	if res.code != http.StatusCreated || res.header.Get("Upload-Attachment") != pngHash {
		t.Fatalf("attach as editor: %d, Upload-Attachment %q; want 201, %s; body=%s",
			res.code, res.header.Get("Upload-Attachment"), pngHash, res.body)
	}
	head := rig.request(t, f.editor, http.MethodHead, res.header.Get("Location"), map[string]string{"Tus-Resumable": "1.0.0"}, nil)
	if head.code != http.StatusOK || head.header.Get("Upload-Attachment") != pngHash {
		t.Fatalf("HEAD after commit: %d, Upload-Attachment %q; want 200, %s", head.code, head.header.Get("Upload-Attachment"), pngHash)
	}

	// Chunked: the hash comes with the PATCH that completes the upload.
	pdf := []byte("%PDF-1.4 not really a pdf\n")
	created := rig.request(t, f.editor, http.MethodPost, "/api/v1/uploads", map[string]string{
		"Tus-Resumable":   "1.0.0",
		"Upload-Length":   strconv.Itoa(len(pdf)),
		"Upload-Metadata": tusMeta("filename", "spec.pdf", "attach_to", uuidString(f.doc.ID)),
	}, nil)
	if created.code != http.StatusCreated || created.header.Get("Upload-Attachment") != "" {
		t.Fatalf("create: %d, Upload-Attachment %q before any byte", created.code, created.header.Get("Upload-Attachment"))
	}
	done := rig.request(t, f.editor, http.MethodPatch, created.header.Get("Location"), map[string]string{
		"Tus-Resumable": "1.0.0",
		"Content-Type":  "application/offset+octet-stream",
		"Upload-Offset": "0",
	}, bytes.NewReader(pdf))
	pdfHash := blob.Hash(pdf)
	if done.code != http.StatusNoContent || done.header.Get("Upload-Attachment") != pdfHash {
		t.Fatalf("final PATCH: %d, Upload-Attachment %q; want 204, %s", done.code, done.header.Get("Upload-Attachment"), pdfHash)
	}

	for _, tc := range []struct {
		name string
		u    store.User
		want int
	}{
		{"owner", f.owner, http.StatusOK},
		{"editor", f.editor, http.StatusOK},
		{"reader", f.reader, http.StatusOK},
		{"view only", f.viewer, http.StatusOK},
		{"member without a grant", f.stranger, http.StatusForbidden},
	} {
		got := rig.attachment(t, tc.u, f.doc, pngHash)
		if got.code != tc.want {
			t.Fatalf("read as %s: %d, want %d", tc.name, got.code, tc.want)
		}
		if got.code != http.StatusOK {
			continue
		}
		if !bytes.Equal(got.body, png) {
			t.Fatalf("read as %s: %d bytes that are not the upload", tc.name, len(got.body))
		}
		for h, want := range map[string]string{
			"Content-Type":           "image/png",
			"X-Content-Type-Options": "nosniff",
			"Cache-Control":          "private, max-age=31536000, immutable",
		} {
			if v := got.header.Get(h); v != want {
				t.Fatalf("read as %s: %s %q, want %q", tc.name, h, v, want)
			}
		}
		if cd := got.header.Get("Content-Disposition"); !strings.HasPrefix(cd, "inline") {
			t.Fatalf("an image must render in place: Content-Disposition %q", cd)
		}
	}
	if cd := rig.attachment(t, f.owner, f.doc, pdfHash).header.Get("Content-Disposition"); !strings.HasPrefix(cd, "attachment") {
		t.Fatalf("a PDF must download, not render: Content-Disposition %q", cd)
	}
	if code := rig.attachment(t, f.owner, other, pngHash).code; code != http.StatusNotFound {
		t.Fatalf("another document's attachment through this one: %d, want 404", code)
	}
	if code := rig.attachment(t, f.owner, f.doc, "not-a-hash").code; code != http.StatusNotFound {
		t.Fatalf("a malformed hash: %d, want 404", code)
	}
}

// An attachment is a blob reference like a version: counted while any copy
// of its document holds it — a copied document takes its attachments along —
// released when the last one is purged, and billed to the space meanwhile.
func TestAttachmentReferencesAreCounted(t *testing.T) {
	rig := newCollabRig(t, collab.Config{})
	ctx := context.Background()
	f := rig.fixture(t, "attachrefs", "# Refs\n")
	content := randomBytes(t, 4096)
	hash := blob.Hash(content)
	refs := func() int32 {
		t.Helper()
		b, err := rig.q.GetBlob(ctx, hash)
		if err != nil {
			t.Fatalf("get blob: %v", err)
		}
		return b.RefCount
	}

	for i := range 2 {
		// The same bytes attached twice to one document are one attachment.
		if res := rig.attach(t, f.owner, f.doc, "scan.png", content); res.code != http.StatusCreated || res.header.Get("Upload-Attachment") != hash {
			t.Fatalf("attach #%d: %d %q", i+1, res.code, res.header.Get("Upload-Attachment"))
		}
		if n := refs(); n != 1 {
			t.Fatalf("after attach #%d: ref_count %d, want 1", i+1, n)
		}
	}
	used, err := rig.q.SumSpaceUsage(ctx, f.space.ID)
	if err != nil {
		t.Fatal(err)
	}
	if want := int64(len("# Refs\n") + len(content)); used != want {
		t.Fatalf("space usage %d, want %d: the document's version and its attachment", used, want)
	}

	res := rig.do(t, f.owner, http.MethodPost, "/api/v1/nodes/"+uuidString(f.doc.ID)+"/copy", map[string]string{})
	if res.code != http.StatusCreated {
		t.Fatalf("copy: %d %s", res.code, res.body)
	}
	var dto nodeDTO
	if err := json.Unmarshal(res.body, &dto); err != nil {
		t.Fatal(err)
	}
	var copyID pgtype.UUID
	if err := copyID.Scan(dto.ID); err != nil {
		t.Fatal(err)
	}
	copied, err := rig.q.GetNode(ctx, copyID)
	if err != nil {
		t.Fatal(err)
	}
	if n := refs(); n != 2 {
		t.Fatalf("after copy: ref_count %d, want 2", n)
	}
	if got := rig.attachment(t, f.owner, copied, hash); got.code != http.StatusOK || !bytes.Equal(got.body, content) {
		t.Fatalf("the copy's attachment: %d", got.code)
	}

	purge := func(n store.Node) {
		t.Helper()
		if err := rig.srv.fs.Delete(ctx, n.ID, f.owner.ID); err != nil {
			t.Fatalf("trash %s: %v", n.Name, err)
		}
		if _, err := rig.srv.fs.Purge(ctx, n.ID); err != nil {
			t.Fatalf("purge %s: %v", n.Name, err)
		}
	}
	purge(copied)
	if n := refs(); n != 1 {
		t.Fatalf("after purging the copy: ref_count %d, want 1", n)
	}
	purge(f.doc)
	if n := refs(); n != 0 {
		t.Fatalf("after purging the document: ref_count %d, want 0", n)
	}
	drift, err := rig.q.RecountBlobRefs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range drift {
		if d.Hash == hash {
			t.Fatalf("the triggers left ref_count wrong: recount corrected it to %d", d.RefCount)
		}
	}
}
