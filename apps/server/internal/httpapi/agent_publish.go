package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"path"
	"sort"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/activity"
	"github.com/nhatbui/pp-hub/apps/server/internal/publish"
	"github.com/nhatbui/pp-hub/apps/server/internal/share"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// handleWriteSiteFiles is PUT /nodes/{id}/site-files, mounted only on the
// agent dispatcher: publish_artifact's files, written into the folder it is
// about to publish. It is the one agent route that writes bytes that are not
// markdown, and it writes them only there — into a folder that is already a
// publication's source or has nothing in it yet — so it cannot become a way
// to overwrite an arbitrary folder's files.
//
// The folder ends up holding exactly the given files: one that exists is
// replaced by a new version, a missing one is created (subfolders too), and
// a file the set no longer names goes to the trash, recoverable like any
// other delete. Takes edit on the folder; the space's quota applies per file
// as for any upload, and the whole set is bounded by maxAgentFileBytes.
func (s *Server) handleWriteSiteFiles(w http.ResponseWriter, r *http.Request) {
	if !s.publish.Enabled() {
		writeProblem(w, http.StatusForbidden, "Forbidden", "publishing is not available on this instance")
		return
	}
	u, folder, _ := s.requireNodeCaps(w, r, share.CapEdit)
	if u == nil {
		return
	}
	if folder.Kind != store.NodeKindDir {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "site files go into a folder")
		return
	}
	// JSON carries the bytes in base64: a third more on the wire.
	if !limitBody(w, r, maxAgentFileBytes*4/3+64<<10) {
		return
	}
	var body struct {
		Files []struct {
			Path    string `json:"path"`
			Content []byte `json:"content"`
		} `json:"files"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid body")
		return
	}
	if len(body.Files) == 0 {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "files is empty")
		return
	}
	if len(body.Files) > publish.MaxFiles {
		writeProblem(w, http.StatusRequestEntityTooLarge, "Payload Too Large", publish.ErrTooManyFiles.Error())
		return
	}
	want := make(map[string][]byte, len(body.Files))
	var total int
	for _, f := range body.Files {
		p, dir, ok := publish.CleanRelPath(f.Path)
		if !ok || dir {
			writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid file path "+f.Path+": use a relative path like css/app.css")
			return
		}
		if _, dup := want[p]; dup {
			writeProblem(w, http.StatusBadRequest, "Bad Request", "the path "+p+" is given twice")
			return
		}
		want[p] = f.Content
		total += len(f.Content)
	}
	if total > maxAgentFileBytes {
		writeProblem(w, http.StatusRequestEntityTooLarge, "Payload Too Large", "the files are over 8 MiB together")
		return
	}

	ctx := r.Context()
	existing, err := s.siteTree(r, folder)
	if err != nil {
		Error(w, r, err)
		return
	}
	if len(existing) > 0 {
		if _, err := s.publish.ForNode(ctx, folder.ID); errors.Is(err, publish.ErrNotFound) {
			writeProblem(w, http.StatusConflict, "Conflict",
				"this folder already holds files and is not published; publish into a new folder name")
			return
		} else if err != nil {
			Error(w, r, err)
			return
		}
	}

	dirs := map[string]pgtype.UUID{"": folder.ID}
	for p, n := range existing {
		if n.Kind == store.NodeKindDir {
			dirs[p] = n.ID
		}
	}
	paths := make([]string, 0, len(want))
	for p := range want {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	written := 0
	for _, p := range paths {
		parent, ok := s.ensureSiteDir(w, r, folder, dirs, existing, path.Dir(p), u)
		if !ok {
			return
		}
		var fileID pgtype.UUID
		if n, ok := existing[p]; ok {
			if n.Kind != store.NodeKindFile {
				writeProblem(w, http.StatusConflict, "Conflict", p+" is a folder, not a file")
				return
			}
			if s.lockedByEditor(w, r, n.ID, http.StatusLocked, false) {
				return
			}
			fileID = n.ID
		}
		node, _, err := s.fs.PutFile(ctx, folder.SpaceID, parent, fileID, path.Base(p), bytes.NewReader(want[p]), u.ID)
		if err != nil {
			s.nodeError(w, r, err)
			return
		}
		if !fileID.Valid {
			s.activity.Node(ctx, u.ID, node, activity.KindCreated, nil)
		}
		written++
	}
	removed := 0
	for p, n := range existing {
		if n.Kind != store.NodeKindFile {
			continue
		}
		if _, keep := want[p]; keep {
			continue
		}
		if err := s.fs.Delete(ctx, n.ID, u.ID); err != nil {
			s.nodeError(w, r, err)
			return
		}
		removed++
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"folder_id": uuidString(folder.ID), "written": written, "removed": removed,
	})
}

// siteTree maps every live node under folder (files and folders, extension
// nodes left out) by its path relative to folder.
func (s *Server) siteTree(r *http.Request, folder store.Node) (map[string]store.Node, error) {
	out := map[string]store.Node{}
	var walk func(parent store.Node, prefix string, depth int) error
	walk = func(parent store.Node, prefix string, depth int) error {
		if depth > 100 || len(out) > publish.MaxFiles*2 {
			return errors.New("site folder is too deep or too large")
		}
		children, err := s.fs.List(r.Context(), parent.SpaceID, parent.ID)
		if err != nil {
			return err
		}
		for _, c := range children {
			if c.Kind == store.NodeKindExt {
				continue
			}
			p := prefix + c.Name
			out[p] = c
			if c.Kind == store.NodeKindDir {
				if err := walk(c, p+"/", depth+1); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return out, walk(folder, "", 0)
}

// ensureSiteDir returns the folder at rel under the site folder, creating
// the missing ones on the way.
func (s *Server) ensureSiteDir(w http.ResponseWriter, r *http.Request, root store.Node,
	dirs map[string]pgtype.UUID, existing map[string]store.Node, rel string, u *store.User,
) (pgtype.UUID, bool) {
	if rel == "." {
		rel = ""
	}
	if id, ok := dirs[rel]; ok {
		return id, true
	}
	if n, ok := existing[rel]; ok && n.Kind != store.NodeKindDir {
		writeProblem(w, http.StatusConflict, "Conflict", rel+" is a file, not a folder")
		return pgtype.UUID{}, false
	}
	parent, ok := s.ensureSiteDir(w, r, root, dirs, existing, path.Dir(rel), u)
	if !ok {
		return pgtype.UUID{}, false
	}
	n, err := s.fs.Mkdir(r.Context(), root.SpaceID, parent, path.Base(rel), u.ID)
	if err != nil {
		s.nodeError(w, r, err)
		return pgtype.UUID{}, false
	}
	s.activity.Node(r.Context(), u.ID, n, activity.KindCreated, nil)
	dirs[rel] = n.ID
	return n.ID, true
}
