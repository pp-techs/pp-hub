package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/activity"
	"github.com/nhatbui/pp-hub/apps/server/internal/fs"
	"github.com/nhatbui/pp-hub/apps/server/internal/share"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// File uploads for agents, mounted only on the in-process agent dispatcher.
// The tus flow exists for big files and slow uplinks; an agent's payload is
// bounded by maxAgentFileBytes and arrives whole in JSON, so upload_file
// creates a file and update_file replaces one in a single request. Both
// commit through the filesystem service like an upload's commit does:
// capability checks, quota, editor locks and the version history are one
// code path.

// decodeAgentContent reads a body's content field: the bytes themselves for
// encoding utf8, base64-decoded for encoding base64 (what download_file
// returns). The bytes must fit maxAgentFileBytes.
func decodeAgentContent(w http.ResponseWriter, content, encoding string) ([]byte, bool) {
	var raw []byte
	switch encoding {
	case "", "utf8":
		raw = []byte(content)
	case "base64":
		b, err := base64.StdEncoding.DecodeString(content)
		if err != nil {
			writeProblem(w, http.StatusBadRequest, "Bad Request", "content is not valid base64")
			return nil, false
		}
		raw = b
	default:
		writeProblem(w, http.StatusBadRequest, "Bad Request", "encoding must be utf8 or base64")
		return nil, false
	}
	if len(raw) > maxAgentFileBytes {
		writeProblem(w, http.StatusRequestEntityTooLarge, "Payload Too Large",
			"the file is over 8 MiB")
		return nil, false
	}
	return raw, true
}

// handleUploadFile is POST /spaces/{spaceID}/files, mounted only on the agent
// dispatcher: upload_file's new file, under folder_id or at the top of the
// space. The name rule and the permission are the upload flow's
// (authorizeUploadTarget): a name already used in the folder is refused
// rather than renamed or replaced — replacing is update_file's explicit call.
func (s *Server) handleUploadFile(w http.ResponseWriter, r *http.Request) {
	// JSON carries the bytes in base64: a third more on the wire.
	if !limitBody(w, r, maxAgentFileBytes*4/3+64<<10) {
		return
	}
	u := caller(r)
	spaceID, err := uuidParam(r, "spaceID")
	if err != nil {
		writeProblem(w, http.StatusNotFound, "Not Found", "no such space")
		return
	}
	var body struct {
		FolderID string `json:"folder_id"`
		Name     string `json:"name"`
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid body")
		return
	}
	name, ok := plainFileName(body.Name)
	if !ok {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "name must be a plain file name")
		return
	}
	content, ok := decodeAgentContent(w, body.Content, body.Encoding)
	if !ok {
		return
	}
	t := uploadTarget{spaceID: spaceID, name: name}
	if body.FolderID != "" {
		if err := t.parentID.Scan(body.FolderID); err != nil {
			writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid folder_id")
			return
		}
	}
	if !s.authorizeUploadTarget(w, r, &t) {
		return
	}
	node, ver, err := s.fs.PutFile(r.Context(), t.spaceID, t.parentID, pgtype.UUID{}, t.name,
		bytes.NewReader(content), u.ID)
	if err != nil {
		s.nodeError(w, r, err)
		return
	}
	s.activity.Node(r.Context(), u.ID, node, activity.KindUploaded, map[string]any{"size": ver.Size})
	writeJSON(w, http.StatusCreated, nodeToDTO(node, &ver).withCaps(share.CapsOwner))
}

// handleReplaceFile is PUT /nodes/{nodeID}/file, mounted only on the agent
// dispatcher: update_file's bytes, replacing the file's content as a new
// version (the previous one stays in the history). The file keeps its name
// and owner, as a replacement upload's does; when the caller names a
// base_etag the write is conditional on the content still matching it.
//
// Markdown documents and supported text files are refused: update_document
// and update_text_file hold the editor lock and the representability rules
// those formats need. A file open in any editor is refused too — this path,
// unlike an office edit session, has no lock to pair with an unlock.
func (s *Server) handleReplaceFile(w http.ResponseWriter, r *http.Request) {
	if !limitBody(w, r, maxAgentFileBytes*4/3+64<<10) {
		return
	}
	u, node, _ := s.requireNodeCaps(w, r, share.CapEdit)
	if u == nil {
		return
	}
	if node.Kind != store.NodeKindFile {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "a folder has no content")
		return
	}
	var body struct {
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
		BaseETag string `json:"base_etag"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid body")
		return
	}
	content, ok := decodeAgentContent(w, body.Content, body.Encoding)
	if !ok {
		return
	}
	cur, err := s.store.GetCurrentVersion(r.Context(), node.ID)
	if err != nil {
		s.nodeError(w, r, err)
		return
	}
	if fs.IsMarkdown(cur.Mime) {
		writeProblem(w, http.StatusUnsupportedMediaType, "Unsupported Media Type",
			"use update_document for a Markdown document")
		return
	}
	if isAgentText(node.Name, cur.Mime) {
		writeProblem(w, http.StatusUnsupportedMediaType, "Unsupported Media Type",
			"use update_text_file for text files")
		return
	}
	token, release, ok := s.acquireAgentTextLock(w, r, node.ID)
	if !ok {
		return
	}
	defer release()

	var written store.NodeVersion
	accessRefused := false
	change := func(ctx context.Context) error {
		latestUser, latestNode, _ := s.requireNodeCaps(w, r.WithContext(ctx), share.CapEdit)
		if latestUser == nil {
			accessRefused = true
			return errors.New("file write access was refused")
		}
		if latestNode.SpaceID != node.SpaceID || latestNode.ParentID != node.ParentID || latestNode.Name != node.Name {
			return fs.ErrStaleVersion
		}
		check := s.agentCommitCheck(u.ID, node, token, false)
		var err error
		if body.BaseETag != "" {
			_, written, err = s.fs.PutFileIfCurrent(ctx, node.SpaceID, node.ParentID, node.ID, node.Name,
				bytes.NewReader(content), u.ID, body.BaseETag, check)
		} else {
			_, written, err = s.fs.PutFileWithCheck(ctx, node.SpaceID, node.ParentID, node.ID, node.Name,
				bytes.NewReader(content), u.ID, check)
		}
		return err
	}
	if err := change(r.Context()); err != nil {
		if agentEditProblem(w, err) {
			return
		}
		if accessRefused {
			return
		}
		if errors.Is(err, fs.ErrStaleVersion) {
			writeProblem(w, http.StatusConflict, "Conflict",
				"the file changed since you read it; read it again and reapply your change")
			return
		}
		s.nodeError(w, r, err)
		return
	}
	if written.ID != cur.ID {
		s.activity.Node(r.Context(), u.ID, node, activity.KindUploaded, map[string]any{
			"size": written.Size, "version": written.VersionNo,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"node_id": uuidString(node.ID), "version": written.VersionNo, "etag": written.BlobHash,
	})
}
