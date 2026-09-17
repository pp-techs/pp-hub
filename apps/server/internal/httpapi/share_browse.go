package httpapi

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path"
	"strconv"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/share"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// errOutsideShare marks a request for a node the link does not cover.
var errOutsideShare = errors.New("node is not inside this share")

// shareInfoDTO is what an anonymous visitor is told about the link itself.
// The banner needs to say who shared, what they granted, and when it lapses;
// without those the page cannot explain why some actions are missing.
type shareInfoDTO struct {
	Node     nodeDTO `json:"node"`
	Level    string  `json:"level"`
	CanDownload bool  `json:"can_download"`
	SharedBy  string `json:"shared_by,omitempty"`
	ExpiresAt string `json:"expires_at,omitempty"`
}

// resolveShareNode returns a node inside the shared subtree, or errOutsideShare.
//
// A link grants a subtree, not a database. Every node id supplied by an
// anonymous caller is therefore walked back up to the share root before it is
// served — without this, holding any valid token would let a visitor read any
// node in the system by guessing ids.
func (s *Server) resolveShareNode(
	r *http.Request,
	root store.Node,
	nodeID pgtype.UUID,
) (store.Node, error) {
	if nodeID == root.ID {
		return root, nil
	}
	node, err := s.fs.Get(r.Context(), nodeID)
	if err != nil {
		return store.Node{}, err
	}
	if node.SpaceID != root.SpaceID {
		return store.Node{}, errOutsideShare
	}

	cur := node.ParentID
	for depth := 0; depth < maxShareDepth && cur.Valid; depth++ {
		if cur == root.ID {
			return node, nil
		}
		parent, err := s.fs.Get(r.Context(), cur)
		if err != nil {
			return store.Node{}, errOutsideShare
		}
		cur = parent.ParentID
	}
	return store.Node{}, errOutsideShare
}

// maxShareDepth bounds the containment walk. Matches fs.maxTreeDepth.
const maxShareDepth = 100

// shareScope resolves the link and the requested node together, writing the
// response itself on any failure. `dir` (or `node`) selects a descendant;
// absent means the share root.
func (s *Server) shareScope(w http.ResponseWriter, r *http.Request, param string) (store.Share, store.Node, bool) {
	sh, root := s.resolveLinkNode(w, r)
	if !sh.ID.Valid {
		return sh, root, false
	}

	raw := r.URL.Query().Get(param)
	if raw == "" {
		return sh, root, true
	}
	var nodeID pgtype.UUID
	if err := nodeID.Scan(raw); err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid node id")
		return sh, root, false
	}

	node, err := s.resolveShareNode(r, root, nodeID)
	if err != nil {
		// Deliberately 404, not 403: confirming that a node exists but sits
		// outside the share is itself more than the visitor should learn.
		writeProblem(w, http.StatusNotFound, "Not Found", "not found in this share")
		return sh, root, false
	}
	return sh, node, true
}

// handleShareChildren lists a folder inside the shared subtree.
func (s *Server) handleShareChildren(w http.ResponseWriter, r *http.Request) {
	_, node, ok := s.shareScope(w, r, "dir")
	if !ok {
		return
	}
	if node.Kind != store.NodeKindDir {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "not a folder")
		return
	}

	children, err := s.fs.ListWithMeta(r.Context(), node.SpaceID, node.ID)
	if err != nil {
		Error(w, r, err)
		return
	}
	out := make([]nodeDTO, 0, len(children))
	for _, c := range children {
		// Owner identity is internal; a link visitor has no account and no
		// business learning who works here.
		dto := nodeToDTOMeta(c)
		dto.Owner = ""
		dto.OwnerID = ""
		out = append(out, dto)
	}
	writeJSON(w, http.StatusOK, out)
}

// handleShareNodeContent serves a file inside the share inline, so a view-only
// link can still preview what it points at. It needs view, not download — the
// same split the authenticated routes use, where /content is CapView and only
// /download is CapDownload.
func (s *Server) handleShareNodeContent(w http.ResponseWriter, r *http.Request) {
	s.serveShareFile(w, r, "inline", false)
}

// handleShareNodeDownload forces a download of a file inside the share.
func (s *Server) handleShareNodeDownload(w http.ResponseWriter, r *http.Request) {
	s.serveShareFile(w, r, "attachment", true)
}

func (s *Server) serveShareFile(w http.ResponseWriter, r *http.Request, disposition string, needDownload bool) {
	sh, node, ok := s.shareScope(w, r, "node")
	if !ok {
		return
	}
	if needDownload && !linkAllowsDownload(w, sh) {
		return
	}
	if node.Kind != store.NodeKindFile {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "not a file")
		return
	}

	n, ver, rc, err := s.fs.OpenFile(r.Context(), node.ID)
	if err != nil {
		s.nodeError(w, r, err)
		return
	}
	defer rc.Close()

	if disposition == "inline" {
		disposition = inlineDisposition(ver.Mime)
	}
	w.Header().Set("Content-Type", ver.Mime)
	w.Header().Set("Content-Disposition", contentDisposition(disposition, n.Name))
	w.Header().Set("X-Content-Type-Options", "nosniff")

	if rs, ok := rc.(io.ReadSeeker); ok {
		http.ServeContent(w, r, n.Name, ver.CreatedAt.Time, rs)
		return
	}
	w.Header().Set("Content-Length", strconv.FormatInt(ver.Size, 10))
	_, _ = io.Copy(w, rc)
}

// handleShareDownloadAll streams the shared folder as a zip.
func (s *Server) handleShareDownloadAll(w http.ResponseWriter, r *http.Request) {
	sh, node, ok := s.shareScope(w, r, "dir")
	if !ok {
		return
	}
	if !linkAllowsDownload(w, sh) {
		return
	}
	if node.Kind != store.NodeKindDir {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "not a folder")
		return
	}
	s.streamZip(w, r, node)
}

// linkAllowsDownload reports whether the link grants at least download,
// writing 403 when it does not.
func linkAllowsDownload(w http.ResponseWriter, sh store.Share) bool {
	caps, err := share.LinkCaps(sh)
	if err != nil || !caps.Download {
		writeProblem(w, http.StatusForbidden, "Forbidden", "this link does not allow downloads")
		return false
	}
	return true
}

// streamZip writes a folder's subtree as a zip, entry by entry.
//
// Nothing is buffered: each blob is copied straight from the store into the
// zip writer and into the response, so a 4 GB folder costs one file handle at
// a time rather than 4 GB of memory. The trade is that an error partway
// through cannot become an HTTP status — the 200 and the first bytes are
// already sent — so a failed entry is skipped and the archive completes
// without it rather than delivering a truncated file that looks whole.
func (s *Server) streamZip(w http.ResponseWriter, r *http.Request, root store.Node) {
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", contentDisposition("attachment", root.Name+".zip"))
	w.Header().Set("X-Content-Type-Options", "nosniff")

	zw := zip.NewWriter(w)
	defer zw.Close()

	err := s.fs.WalkFiles(r.Context(), root.ID, func(dir string, node store.Node) error {
		_, _, rc, err := s.fs.OpenFile(r.Context(), node.ID)
		if err != nil {
			return err
		}
		defer rc.Close()

		entry, err := zw.Create(path.Join(dir, node.Name))
		if err != nil {
			return err
		}
		_, err = io.Copy(entry, rc)
		return err
	})
	if err != nil {
		// The response is already committed; the archive is closed as-is.
		fmt.Fprintf(w, "\n[pp-hub] archive incomplete: %v\n", err)
	}
}

func contentDisposition(disposition, filename string) string {
	cd := mime.FormatMediaType(disposition, map[string]string{"filename": filename})
	if cd == "" { // unencodable filename — keep the disposition, drop the name
		return disposition
	}
	return cd
}

// handleShareInfo tells the landing page about the link itself, not just the
// node: who shared it, what it grants, and when it lapses.
func (s *Server) handleShareInfo(w http.ResponseWriter, r *http.Request) {
	sh, node := s.resolveLinkNode(w, r)
	if !sh.ID.Valid {
		return
	}

	dto := nodeToDTO(node, nil)
	if node.Kind == store.NodeKindFile {
		if ver, err := s.store.GetCurrentVersion(r.Context(), node.ID); err == nil {
			dto = nodeToDTO(node, &ver)
		}
	}
	dto.Owner = ""
	dto.OwnerID = ""

	caps, _ := share.LinkCaps(sh)
	out := shareInfoDTO{Node: dto, Level: string(sh.Level), CanDownload: caps.Download}
	if sh.ExpiresAt.Valid {
		out.ExpiresAt = sh.ExpiresAt.Time.Format(timeLayout)
	}
	if u, err := s.store.GetUser(r.Context(), sh.CreatedBy); err == nil {
		out.SharedBy = displayName(u)
	}
	writeJSON(w, http.StatusOK, out)
}

// displayName prefers the user-set name over the OIDC one, falling back to the
// email local part so a share banner never reads "shared by ".
func displayName(u store.User) string {
	if u.DisplayName != "" {
		return u.DisplayName
	}
	if u.Name != "" {
		return u.Name
	}
	return u.Email
}
