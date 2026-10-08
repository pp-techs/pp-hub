package httpapi

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/audit"
	"github.com/nhatbui/pp-hub/apps/server/internal/blockdoc"
	"github.com/nhatbui/pp-hub/apps/server/internal/share"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
	"github.com/nhatbui/pp-hub/apps/server/internal/users"
	"github.com/nhatbui/pp-hub/apps/server/internal/wopi"
)

// errOutsideShare marks a request for a node the link does not cover.
var errOutsideShare = errors.New("node is not inside this share")

// shareInfoDTO is what an anonymous visitor is told about the link itself.
// The banner needs to say who shared, what they granted, and when it lapses;
// without those the page cannot explain why some actions are missing.
type shareInfoDTO struct {
	Node        nodeDTO `json:"node"`
	Level       string  `json:"level"`
	CanDownload bool    `json:"can_download"`
	SharedBy    string  `json:"shared_by,omitempty"`
	ExpiresAt   string  `json:"expires_at,omitempty"`
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
		// An extension node has no link view in this release.
		if c.Kind == store.NodeKindExt {
			continue
		}
		// Owner identity is internal; a link visitor has no account and no
		// business learning who works here.
		dto := nodeToDTOMeta(c)
		dto.Owner = ""
		dto.OwnerID = ""
		out = append(out, dto)
	}
	s.attachChildPageCounts(r.Context(), out)
	writeJSON(w, http.StatusOK, out)
}

// handleShareNodeContent serves a file inside the share inline, for the
// browser to render — the point of a link. Viewing is what a link grants, so
// this needs no download flag; only the routes that hand the bytes over
// deliberately (/node-download, /download, /download-all) do.
func (s *Server) handleShareNodeContent(w http.ResponseWriter, r *http.Request) {
	s.serveShareFile(w, r, "inline", false, audit.KindFileViewed)
}

// handleShareNodeDownload forces a download of a file inside the share, and
// so needs the link's download flag.
func (s *Server) handleShareNodeDownload(w http.ResponseWriter, r *http.Request) {
	s.serveShareFile(w, r, "attachment", true, audit.KindFileDownloaded)
}

func (s *Server) serveShareFile(w http.ResponseWriter, r *http.Request, disposition string, needDownload bool, kind audit.Kind) {
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
	s.audit.RecordRead(r, audit.Read{Kind: kind, ShareID: sh.ID, SharedBy: sh.CreatedBy, Node: n})

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
	s.audit.RecordRead(r, audit.Read{Kind: audit.KindFolderDownloaded, ShareID: sh.ID, SharedBy: sh.CreatedBy, Node: node})
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

// streamZip writes a folder's subtree as a zip, entry by entry, the block
// documents in it exported with their sub-pages and attachments as
// handleExport has them (blockdoc.PrepareZip).
//
// Nothing is buffered but one document's markdown: each blob is copied
// straight from the store into the zip writer and into the response, so a
// 4 GB folder costs one file handle at a time rather than 4 GB of memory.
// Only the layout is read first, nodes without content, since a link between
// two pages in the archive is written as a path between their entries. The
// trade is that an error partway through cannot become an HTTP status — the
// 200 and the first bytes are already sent — so the archive is left
// unfinished rather than delivered as a truncated file that looks whole.
func (s *Server) streamZip(w http.ResponseWriter, r *http.Request, root store.Node) {
	exp, err := blockdoc.New(s.fs).PrepareZip(r.Context(), root, s.externalURL)
	if err != nil {
		s.nodeError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", contentDisposition("attachment", root.Name+".zip"))
	w.Header().Set("X-Content-Type-Options", "nosniff")

	if err := exp.WriteZip(r.Context(), w); err != nil {
		// The response is already committed; the archive ends here.
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

	if node.Kind == store.NodeKindExt {
		writeProblem(w, http.StatusUnprocessableEntity, "Unprocessable Entity",
			"this item cannot be opened through a link")
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
	// A name, never an address. The visitor is anonymous and the link is
	// forwardable, so an email here is a company's directory entry handed to
	// whoever the link reaches; with no name set the banner says nothing.
	if u, err := s.store.GetUser(r.Context(), sh.CreatedBy); err == nil {
		out.SharedBy = publicName(u)
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

// publicName is what anonymous visitors may see of a person: the name they
// chose or the IdP's, and nothing when neither is set.
func publicName(u store.User) string {
	if u.DisplayName != "" {
		return u.DisplayName
	}
	return u.Name
}

// handleShareEditSession returns an OnlyOffice session URL for an office file inside a share.
func (s *Server) handleShareEditSession(w http.ResponseWriter, r *http.Request) {
	sh, node, ok := s.shareScope(w, r, "node")
	if !ok {
		return
	}
	if node.Kind != store.NodeKindFile {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "not a file")
		return
	}
	ver, err := s.store.GetCurrentVersion(r.Context(), node.ID)
	if err != nil {
		s.nodeError(w, r, err)
		return
	}
	if !isOfficeNode(node, ver.Mime) {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "file type not supported by editor")
		return
	}

	caps, _ := share.LinkCaps(sh)
	permRank, mode := 1, "view"
	if caps.Download {
		permRank = 2
	}

	token, err := s.wopi.SignToken(sh.CreatedBy, node.ID, permRank)
	if err != nil {
		Error(w, r, err)
		return
	}

	nodeIDStr, _ := node.ID.Value()
	wopiSrc := s.wopiServerURL + "/wopi/files/" + nodeIDStr.(string)
	editorURL := s.onlyOfficeURL + "/hosting/wopi/" + wopiDocumentType(node.Name) + "/" + mode +
		"?WOPISrc=" + url.QueryEscape(wopiSrc)
	if ui := strings.TrimSpace(r.URL.Query().Get("ui")); ui != "" && users.ValidLocale(ui) {
		editorURL += "&ui=" + url.QueryEscape(ui)
	}
	switch r.URL.Query().Get("theme") {
	case "light":
		editorURL += "&thm=1"
	case "dark":
		editorURL += "&thm=0"
	}
	s.audit.RecordRead(r, audit.Read{
		Kind:     audit.KindFileOpened,
		ShareID:  sh.ID,
		SharedBy: sh.CreatedBy,
		Node:     node,
		Detail:   map[string]any{"mode": mode},
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"editor_url":       editorURL,
		"wopi_src":         wopiSrc,
		"token":            token,
		"access_token_ttl": time.Now().Add(wopi.TokenTTL).UnixMilli(),
		"can_write":        false,
	})
}
