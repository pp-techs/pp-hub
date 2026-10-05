package httpapi

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/activity"
	"github.com/nhatbui/pp-hub/apps/server/internal/fs"
	"github.com/nhatbui/pp-hub/apps/server/internal/share"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
	"github.com/nhatbui/pp-hub/apps/server/internal/uploads"
)

// Resumable uploads, speaking tus 1.0.0 (core protocol plus the creation and
// termination extensions): https://tus.io/protocols/resumable-upload.
//
// Why a protocol rather than one POST: a proxy in front decides how long a
// request may take, and the answer is often a minute. A multi-megabyte file
// over a slow uplink does not fit in one; its chunks do. The client sizes
// chunks from measured throughput, and a chunk that dies anyway loses only
// the bytes that never arrived — the server keeps every byte it received and
// the client asks (HEAD) where to resume.
//
// Every route sits in the authenticated, same-origin /api/v1 group. An
// upload is visible only to the account that created it.

const tusVersion = "1.0.0"

// tusHeaders sets the response headers every tus answer carries, and refuses
// a client speaking another version of the protocol. A missing
// Tus-Resumable header is tolerated rather than refused; a different one is
// not.
func tusHeaders(w http.ResponseWriter, r *http.Request) bool {
	w.Header().Set("Tus-Resumable", tusVersion)
	if v := r.Header.Get("Tus-Resumable"); v != "" && v != tusVersion {
		w.Header().Set("Tus-Version", tusVersion)
		writeProblem(w, http.StatusPreconditionFailed, "Precondition Failed", "unsupported tus version "+v)
		return false
	}
	return true
}

// uploadTarget is where an upload's bytes will land: a new file named Name
// under ParentID in SpaceID, a new version of NodeID when that is set, or an
// attachment of the block document AttachTo when that is.
type uploadTarget struct {
	spaceID  pgtype.UUID
	parentID pgtype.UUID
	nodeID   pgtype.UUID
	attachTo pgtype.UUID
	name     string
}

// authorizeUploadTarget checks the caller may put bytes where the upload is
// headed, filling in the space for a replacement or an attachment. It runs at
// creation and again at commit: access can be revoked, and an editor opened,
// while the bytes are still arriving.
func (s *Server) authorizeUploadTarget(w http.ResponseWriter, r *http.Request, t *uploadTarget) bool {
	if t.attachTo.Valid {
		// An image dropped into a document is part of editing it. The room's
		// lock does not refuse it: an attachment changes no byte of the
		// markdown, which names it only once the editor inserts it.
		u, doc, _ := s.requireNodeCapsByID(w, r, t.attachTo, share.CapEdit)
		if u == nil || !s.isMarkdownFile(w, r, doc) {
			return false
		}
		t.spaceID = doc.SpaceID
		return true
	}
	if t.nodeID.Valid {
		// A replacement lands as a new version of an existing file: the node
		// keeps its name and owner, the version is authored by the uploader.
		u, node, _ := s.requireNodeCapsByID(w, r, t.nodeID, share.CapEdit)
		if u == nil {
			return false
		}
		if node.Kind != store.NodeKindFile {
			writeProblem(w, http.StatusBadRequest, "Bad Request", "not a file")
			return false
		}
		// A .docx open in OnlyOffice is a lock row with a 30-minute TTL and
		// the WOPI save path honours it; new bytes from here would become the
		// editor's next save's base and its work would be gone. Refused
		// rather than locked: this path has no unlock to pair with a lock.
		// A block document's room holds a lock too, and its next write-back
		// would overwrite the upload the same way.
		if s.lockedByEditor(w, r, node.ID, http.StatusLocked, false) {
			return false
		}
		t.spaceID, t.parentID, t.name = node.SpaceID, node.ParentID, node.Name
		return true
	}
	// requireParentWrite gates the space's top level on membership and a
	// folder on edit, which is how somebody outside the space holding an
	// external edit grant on a folder can add to it. An upload lands in a
	// folder only: a sub-page is made through POST /documents, never by
	// uploading a .md under a document, so it is described as no page.
	u := caller(r)
	return s.requireParentWrite(w, r, t.spaceID, t.parentID, u, fs.Child{Kind: store.NodeKindFile})
}

// handleCreateUpload opens an upload (tus creation). The declared length is
// checked against the upload cap and the space's quota before a byte is
// accepted — including the bytes other uploads into the space have already
// promised — so an upload that cannot land is refused now, not after an hour
// of transfer.
func (s *Server) handleCreateUpload(w http.ResponseWriter, r *http.Request) {
	if !tusHeaders(w, r) {
		return
	}
	u := caller(r)

	length, err := strconv.ParseInt(r.Header.Get("Upload-Length"), 10, 64)
	if err != nil || length < 0 {
		// Upload-Defer-Length is deliberately unsupported: the ceilings
		// below need the length up front.
		writeProblem(w, http.StatusBadRequest, "Bad Request", "Upload-Length is required")
		return
	}
	meta, err := parseUploadMetadata(r.Header.Get("Upload-Metadata"))
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", err.Error())
		return
	}

	var t uploadTarget
	switch {
	case meta["node_id"] != "" && meta["attach_to"] != "":
		writeProblem(w, http.StatusBadRequest, "Bad Request", "node_id and attach_to are exclusive")
		return
	case meta["node_id"] != "":
		if err := t.nodeID.Scan(meta["node_id"]); err != nil {
			writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid node_id")
			return
		}
	default:
		name, ok := plainFileName(meta["filename"])
		if !ok {
			writeProblem(w, http.StatusBadRequest, "Bad Request", "filename must be a plain file name")
			return
		}
		t.name = name
		if v := meta["attach_to"]; v != "" {
			if err := t.attachTo.Scan(v); err != nil {
				writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid attach_to")
				return
			}
			break
		}
		if err := t.spaceID.Scan(meta["space_id"]); err != nil {
			writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid space_id")
			return
		}
		if v := meta["parent_id"]; v != "" {
			if err := t.parentID.Scan(v); err != nil {
				writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid parent_id")
				return
			}
		}
	}

	if limit := s.uploadCap(); limit > 0 && length > limit {
		writeProblem(w, http.StatusRequestEntityTooLarge, "Payload Too Large",
			"this upload is larger than this server accepts")
		return
	}
	if !s.authorizeUploadTarget(w, r, &t) {
		return
	}
	pending, err := s.uploads.PendingBytes(r.Context(), t.spaceID)
	if err != nil {
		Error(w, r, err)
		return
	}
	if err := s.fs.EnsureRoom(r.Context(), t.spaceID, pending+length); err != nil {
		s.nodeError(w, r, err)
		return
	}
	withData := isChunk(r)
	if withData && r.ContentLength > length {
		writeProblem(w, http.StatusRequestEntityTooLarge, "Payload Too Large",
			"this chunk runs past the length the upload declared")
		return
	}

	up, err := s.uploads.Create(r.Context(), uploads.CreateParams{
		SpaceID:   t.spaceID,
		ParentID:  t.parentID,
		NodeID:    t.nodeID,
		AttachTo:  t.attachTo,
		Name:      t.name,
		TotalSize: length,
		CreatedBy: u.ID,
	})
	if err != nil {
		Error(w, r, err)
		return
	}
	// Set before any bytes are read, so even a failed first chunk tells the
	// client where to resume.
	w.Header().Set("Location", "/api/v1/uploads/"+uuidString(up.ID))
	w.Header().Set("Upload-Expires", up.ExpiresAt.Time.UTC().Format(http.TimeFormat))
	next := int64(0)
	if withData {
		// creation-with-upload: the first chunk rides on the POST. A small
		// file is then one request, as it was before uploads were
		// resumable, instead of two — which is what a folder of a thousand
		// photos spends its rate limit on. Nobody else knows this upload's
		// id yet, so there is no one to lock against.
		var ok bool
		if next, ok = s.receive(w, r, up, 0); !ok {
			return
		}
	}
	w.Header().Set("Upload-Offset", strconv.FormatInt(next, 10))
	w.WriteHeader(http.StatusCreated)
}

// handleUploadOffset answers where an upload stands (tus HEAD). A committed
// upload reports its full length, which is how a client that lost the final
// response learns the file landed.
func (s *Server) handleUploadOffset(w http.ResponseWriter, r *http.Request) {
	if !tusHeaders(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	up, ok := s.loadUpload(w, r)
	if !ok {
		return
	}
	w.Header().Set("Upload-Offset", strconv.FormatInt(up.Uploaded, 10))
	w.Header().Set("Upload-Length", strconv.FormatInt(up.TotalSize, 10))
	w.Header().Set("Upload-Expires", up.ExpiresAt.Time.UTC().Format(http.TimeFormat))
	setUploadAttachment(w, up)
	w.WriteHeader(http.StatusOK)
}

// handleUploadChunk appends a chunk (tus PATCH) and, when it is the last one,
// commits the file before answering: a 204 at full length means the file is
// in place.
func (s *Server) handleUploadChunk(w http.ResponseWriter, r *http.Request) {
	if !tusHeaders(w, r) {
		return
	}
	if !isChunk(r) {
		writeProblem(w, http.StatusUnsupportedMediaType, "Unsupported Media Type",
			"chunks must be sent as application/offset+octet-stream")
		return
	}
	offset, err := strconv.ParseInt(r.Header.Get("Upload-Offset"), 10, 64)
	if err != nil || offset < 0 {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "Upload-Offset is required")
		return
	}
	id, err := uuidParam(r, "uploadID")
	if err != nil {
		writeProblem(w, http.StatusNotFound, "Not Found", "no such upload")
		return
	}
	// A retried final chunk waits here for the commit already running
	// instead of starting a second one.
	unlock, err := s.uploads.Lock(r.Context(), id)
	if err != nil {
		return // the client gave up while waiting
	}
	defer unlock()
	up, ok := s.loadUpload(w, r)
	if !ok {
		return
	}

	if up.CompletedAt.Valid {
		if offset == up.TotalSize {
			w.Header().Set("Upload-Offset", strconv.FormatInt(up.TotalSize, 10))
			setUploadAttachment(w, up)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeUploadConflict(w, up.TotalSize)
		return
	}
	if offset != up.Uploaded {
		writeUploadConflict(w, up.Uploaded)
		return
	}
	if r.ContentLength > up.TotalSize-offset {
		writeProblem(w, http.StatusRequestEntityTooLarge, "Payload Too Large",
			"this chunk runs past the length the upload declared")
		return
	}
	next, ok := s.receive(w, r, up, offset)
	if !ok {
		return
	}
	w.Header().Set("Upload-Offset", strconv.FormatInt(next, 10))
	w.WriteHeader(http.StatusNoContent)
}

// isChunk reports whether the request body is upload bytes.
func isChunk(r *http.Request) bool {
	ct, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	return ct == "application/offset+octet-stream"
}

// receive appends the request body to up at offset and, when that completes
// the upload, commits it. On success it has written no response and returns
// the new offset; on failure it has answered and returns false.
//
// It does not run on the request's context: a client that disconnects
// mid-chunk is the case this protocol exists for, and the bytes it did send
// must still be recorded — and a commit, once started, must not be abandoned
// halfway.
func (s *Server) receive(w http.ResponseWriter, r *http.Request, up store.Upload, offset int64) (int64, bool) {
	ctx := context.WithoutCancel(r.Context())
	next, err := s.uploads.Append(ctx, up, offset, r.Body)
	switch {
	case errors.Is(err, uploads.ErrNotFound):
		writeProblem(w, http.StatusNotFound, "Not Found", "no such upload")
		return 0, false
	case errors.Is(err, uploads.ErrOffsetMismatch):
		writeUploadConflict(w, up.Uploaded)
		return 0, false
	case err != nil && next == offset:
		// Nothing was written: a disk error, or a body that broke before
		// its first byte.
		Error(w, r, err)
		return 0, false
	case err != nil:
		// The body broke part-way. What arrived is recorded; whoever is
		// still listening learns where to resume.
		slog.InfoContext(ctx, "upload chunk interrupted", "upload", up.ID, "offset", next, "err", err)
		w.Header().Set("Upload-Offset", strconv.FormatInt(next, 10))
		writeProblem(w, http.StatusBadRequest, "Bad Request", "the chunk was interrupted; resume from Upload-Offset")
		return 0, false
	}
	if next == up.TotalSize && !s.commitUpload(w, r.WithContext(ctx), up) {
		return 0, false
	}
	return next, true
}

// commitUpload turns a complete upload into a file or a new version, and
// reports whether it did; on failure it has answered.
//
// Authorization is checked again first. What happens to the upload on
// failure depends on whether retrying the commit could succeed: a lock or a
// server error may clear, so the bytes are kept and the client's retry is an
// empty final PATCH; no longer having access, a target that is gone, or a
// full space will not, so the upload is dropped instead of holding disk and
// quota until it expires.
func (s *Server) commitUpload(w http.ResponseWriter, r *http.Request, up store.Upload) bool {
	ctx := r.Context()
	u := caller(r)
	t := uploadTarget{spaceID: up.SpaceID, parentID: up.ParentID, nodeID: up.NodeID, attachTo: up.AttachTo, name: up.Name}
	rec := &statusRecorder{ResponseWriter: w}
	if !s.authorizeUploadTarget(rec, r, &t) {
		if rec.status != http.StatusLocked && rec.status < 500 {
			s.dropUpload(ctx, up)
		}
		return false
	}
	if t.attachTo.Valid {
		return s.commitAttachment(w, r, up, t)
	}
	node, ver, err := s.fs.PutStagedFile(ctx, t.spaceID, t.parentID, t.nodeID, t.name, s.uploads.Path(up), u.ID)
	if err != nil {
		if errors.Is(err, fs.ErrQuotaExceeded) || errors.Is(err, fs.ErrNotFound) || errors.Is(err, fs.ErrNotFile) {
			s.dropUpload(ctx, up)
		}
		s.nodeError(w, r, err)
		return false
	}
	if err := s.uploads.Complete(ctx, up.ID); err != nil {
		// The file is committed; a stale row only means a HEAD for it
		// reports a short offset until it expires.
		slog.WarnContext(ctx, "upload completed but not marked", "upload", up.ID, "err", err)
	}
	detail := map[string]any{"size": ver.Size}
	if t.nodeID.Valid {
		detail["version"] = ver.VersionNo
	}
	s.activity.Node(ctx, u.ID, node, activity.KindUploaded, detail)
	return true
}

// commitAttachment is commitUpload for an attachment: the bytes become a row
// of the document's, and the answer names their hash (Upload-Attachment), the
// `attachment:<hash>` the editor writes into the markdown. No activity entry:
// the document is being edited, not uploaded.
func (s *Server) commitAttachment(w http.ResponseWriter, r *http.Request, up store.Upload, t uploadTarget) bool {
	ctx := r.Context()
	att, err := s.fs.AddStagedAttachment(ctx, t.attachTo, t.name, s.uploads.Path(up), caller(r).ID)
	if err != nil {
		if errors.Is(err, fs.ErrQuotaExceeded) || errors.Is(err, fs.ErrNotFound) || errors.Is(err, fs.ErrNotFile) {
			s.dropUpload(ctx, up)
		}
		s.nodeError(w, r, err)
		return false
	}
	if err := s.uploads.CompleteAttachment(ctx, up.ID, att.BlobHash); err != nil {
		// The attachment is committed; only a client that lost this answer
		// and asks HEAD misses the hash.
		slog.WarnContext(ctx, "upload completed but not marked", "upload", up.ID, "err", err)
	}
	w.Header().Set("Upload-Attachment", att.BlobHash)
	return true
}

// setUploadAttachment names the attachment a committed attachment upload
// became, for a client asking again after losing the final answer.
func setUploadAttachment(w http.ResponseWriter, up store.Upload) {
	if up.AttachmentHash.Valid {
		w.Header().Set("Upload-Attachment", up.AttachmentHash.String)
	}
}

// handleTerminateUpload abandons an upload and frees its staged bytes (tus
// termination).
func (s *Server) handleTerminateUpload(w http.ResponseWriter, r *http.Request) {
	if !tusHeaders(w, r) {
		return
	}
	id, err := uuidParam(r, "uploadID")
	if err != nil {
		writeProblem(w, http.StatusNotFound, "Not Found", "no such upload")
		return
	}
	unlock, err := s.uploads.Lock(r.Context(), id)
	if err != nil {
		return
	}
	defer unlock()
	up, ok := s.loadUpload(w, r)
	if !ok {
		return
	}
	if err := s.uploads.Terminate(r.Context(), up); err != nil {
		Error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// loadUpload resolves {uploadID} to the caller's upload, answering 404 for
// anything else — unknown, expired, terminated or somebody else's.
func (s *Server) loadUpload(w http.ResponseWriter, r *http.Request) (store.Upload, bool) {
	id, err := uuidParam(r, "uploadID")
	if err != nil {
		writeProblem(w, http.StatusNotFound, "Not Found", "no such upload")
		return store.Upload{}, false
	}
	up, err := s.uploads.Get(r.Context(), id, caller(r).ID)
	if errors.Is(err, uploads.ErrNotFound) {
		writeProblem(w, http.StatusNotFound, "Not Found", "no such upload")
		return store.Upload{}, false
	}
	if err != nil {
		Error(w, r, err)
		return store.Upload{}, false
	}
	return up, true
}

func (s *Server) dropUpload(ctx context.Context, up store.Upload) {
	if err := s.uploads.Terminate(ctx, up); err != nil {
		slog.WarnContext(ctx, "failed upload not removed", "upload", up.ID, "err", err)
	}
}

// writeUploadConflict is tus's answer to a PATCH at the wrong offset. The
// current offset rides along so a client can resume without a HEAD.
func writeUploadConflict(w http.ResponseWriter, offset int64) {
	w.Header().Set("Upload-Offset", strconv.FormatInt(offset, 10))
	writeProblem(w, http.StatusConflict, "Conflict", "Upload-Offset does not match the upload")
}

// parseUploadMetadata decodes tus's Upload-Metadata header: comma-separated
// pairs of a key and an optional base64 value.
func parseUploadMetadata(h string) (map[string]string, error) {
	out := map[string]string{}
	if strings.TrimSpace(h) == "" {
		return out, nil
	}
	for _, pair := range strings.Split(h, ",") {
		key, val, _ := strings.Cut(strings.TrimSpace(pair), " ")
		if key == "" {
			return nil, errors.New("malformed Upload-Metadata")
		}
		if _, dup := out[key]; dup {
			return nil, fmt.Errorf("duplicate Upload-Metadata key %q", key)
		}
		dec, err := base64.StdEncoding.DecodeString(strings.TrimSpace(val))
		if err != nil || !utf8.Valid(dec) {
			return nil, fmt.Errorf("Upload-Metadata %q is not base64 UTF-8", key)
		}
		out[key] = string(dec)
	}
	return out, nil
}

// plainFileName accepts a name that can sit in a folder as-is: no path
// separators, not a dot entry, not blank.
func plainFileName(name string) (string, bool) {
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\\x00") {
		return "", false
	}
	return name, true
}

// statusRecorder remembers the status a helper answered with, so the caller
// can decide what to do about the failure it already reported.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}
