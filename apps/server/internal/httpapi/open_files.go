package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/collab"
	"github.com/nhatbui/pp-hub/apps/server/internal/share"
)

// Saving an open file now (Ctrl/Cmd+S, and the admin's open-files page).
//
// An office document's edits live in the Document Server and a block
// document's in its collaboration room, and neither reaches the file's version
// history until the editor goes quiet or the periodic save comes round. A
// person pressing save, or an operator about to redeploy, asks for that to
// happen now. The editor behind a file is read from its lock: `wopi_…` is an
// OnlyOffice session, `collab:…` a room (see ListOpenEditorLocks).

// saveState is what a save request found.
type saveState string

const (
	// saveQueued: the Document Server accepted the request; the version
	// follows a moment later, from its own PutFile.
	saveQueued saveState = "queued"
	// saveWritten: the room wrote its edits as a version before answering.
	saveWritten saveState = "saved"
	// saveNothing: no editor holds the file, or nothing was typed since it
	// was last saved.
	saveNothing saveState = "unchanged"
)

// errNoDocumentServer: this deployment has no Document Server to ask.
var errNoDocumentServer = errors.New("office editor is not configured")

// saveLocked writes back whatever the editor named by lock holds for node.
// An empty lock, or one held by something else (WebDAV, an agent's edit), has
// nothing to save.
func (s *Server) saveLocked(ctx context.Context, nodeID pgtype.UUID, lock string) (saveState, error) {
	switch {
	case strings.HasPrefix(lock, "wopi_"):
		if s.officeSaver == nil {
			return "", errNoDocumentServer
		}
		queued, err := s.officeSaver.Save(ctx, lock)
		if err != nil {
			return "", err
		}
		if queued {
			return saveQueued, nil
		}
		return saveNothing, nil
	case collab.IsLockToken(lock):
		if err := s.collab.Flush(ctx, nodeID); err != nil {
			return "", err
		}
		return saveWritten, nil
	}
	return saveNothing, nil
}

// saveFailed answers a save that could not be asked for.
func saveFailed(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, errNoDocumentServer) {
		writeProblem(w, http.StatusConflict, "Conflict", err.Error())
		return
	}
	slog.WarnContext(r.Context(), "save open file", "err", err)
	writeProblem(w, http.StatusBadGateway, "Bad Gateway", "the editor did not accept the save")
}

type saveResultDTO struct {
	State saveState `json:"state"`
}

// handleSaveNode asks the editor holding a file to save it now. It takes edit
// on the file: it is the act of someone who could have typed the edits.
func (s *Server) handleSaveNode(w http.ResponseWriter, r *http.Request) {
	u, node, _ := s.requireNodeCaps(w, r, share.CapEdit)
	if u == nil {
		return
	}
	lock, err := s.wopi.CurrentLock(r.Context(), node.ID)
	if err != nil {
		Error(w, r, err)
		return
	}
	state, err := s.saveLocked(r.Context(), node.ID, lock)
	if err != nil {
		saveFailed(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, saveResultDTO{State: state})
}

// openFileDTO is one row of the admin's list of files open in an editor.
type openFileDTO struct {
	NodeID    string `json:"node_id"`
	Name      string `json:"name"`
	SpaceID   string `json:"space_id"`
	SpaceName string `json:"space_name"`
	// Editor is "office" (OnlyOffice), "blocks" (a block document's room) or
	// "other" (a WebDAV client or an agent holding the file). Only the first
	// two can be saved from here.
	Editor string `json:"editor"`
	// LockedAt is when the file was opened; RefreshedAt the last time its
	// editor renewed the lock, which an open tab does every few minutes.
	LockedAt    string `json:"locked_at"`
	RefreshedAt string `json:"refreshed_at"`
	// VersionNo and SavedAt describe the file's current version — what a
	// deploy would leave behind.
	VersionNo int32  `json:"version_no,omitempty"`
	SavedAt   string `json:"saved_at,omitempty"`
}

func editorOf(lock string) string {
	switch {
	case strings.HasPrefix(lock, "wopi_"):
		return "office"
	case collab.IsLockToken(lock):
		return "blocks"
	}
	return "other"
}

// handleAdminOpenFiles lists the files open in an editor right now, so an
// operator can see what a restart would interrupt.
func (s *Server) handleAdminOpenFiles(w http.ResponseWriter, r *http.Request) {
	if s.requireAdmin(w, r) == nil {
		return
	}
	rows, err := s.store.ListOpenEditorLocks(r.Context())
	if err != nil {
		Error(w, r, err)
		return
	}
	out := make([]openFileDTO, 0, len(rows))
	for _, row := range rows {
		out = append(out, openFileDTO{
			NodeID:      uuidString(row.NodeID),
			Name:        row.Name,
			SpaceID:     uuidString(row.SpaceID),
			SpaceName:   row.SpaceName,
			Editor:      editorOf(row.LockToken),
			LockedAt:    row.LockedAt.Time.Format(timeLayout),
			RefreshedAt: row.RefreshedAt.Time.Format(timeLayout),
			VersionNo:   row.VersionNo.Int32,
			SavedAt:     optionalTime(row.VersionAt),
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// adminSaveDTO totals an admin's save-everything request. A file is counted
// once: queued and saved ones will be a version shortly, unchanged ones had
// nothing to save, failed ones were not accepted by their editor.
type adminSaveDTO struct {
	Queued    int `json:"queued"`
	Saved     int `json:"saved"`
	Unchanged int `json:"unchanged"`
	Failed    int `json:"failed"`
}

// handleAdminSaveOpenFiles asks every open editor to save, one file at a time.
// A file that fails does not stop the rest; the answer counts them.
func (s *Server) handleAdminSaveOpenFiles(w http.ResponseWriter, r *http.Request) {
	admin := s.requireAdmin(w, r)
	if admin == nil {
		return
	}
	rows, err := s.store.ListOpenEditorLocks(r.Context())
	if err != nil {
		Error(w, r, err)
		return
	}
	var out adminSaveDTO
	for _, row := range rows {
		state, err := s.saveLocked(r.Context(), row.NodeID, row.LockToken)
		if err != nil {
			slog.WarnContext(r.Context(), "admin save open file", "err", err, "node_id", uuidString(row.NodeID))
			out.Failed++
			continue
		}
		switch state {
		case saveQueued:
			out.Queued++
		case saveWritten:
			out.Saved++
		default:
			out.Unchanged++
		}
	}
	slog.InfoContext(r.Context(), "admin saved open files", "by", admin.Email,
		"queued", out.Queued, "saved", out.Saved, "unchanged", out.Unchanged, "failed", out.Failed)
	writeJSON(w, http.StatusOK, out)
}

// handleAdminSaveOpenFile asks the editor holding one file to save it.
func (s *Server) handleAdminSaveOpenFile(w http.ResponseWriter, r *http.Request) {
	admin := s.requireAdmin(w, r)
	if admin == nil {
		return
	}
	nodeID, err := uuidParam(r, "nodeID")
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid node id")
		return
	}
	lock, err := s.wopi.CurrentLock(r.Context(), nodeID)
	if err != nil {
		Error(w, r, err)
		return
	}
	state, err := s.saveLocked(r.Context(), nodeID, lock)
	if err != nil {
		saveFailed(w, r, err)
		return
	}
	slog.InfoContext(r.Context(), "admin saved open file", "by", admin.Email, "node_id", uuidString(nodeID), "state", state)
	writeJSON(w, http.StatusOK, saveResultDTO{State: state})
}
