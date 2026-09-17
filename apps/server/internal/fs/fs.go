// Package fs implements the file tree: nodes, versions, upload/download.
// See docs/architecture/data-model.md and storage.md.
package fs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/blob"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

var (
	ErrNotFound        = errors.New("node not found")
	ErrNotFile         = errors.New("node is not a file")
	ErrNotDir          = errors.New("node is not a directory")
	ErrCycle           = errors.New("cannot move a folder into its own descendant")
	ErrNameConflict    = errors.New("a node with this name already exists in the target folder")
	ErrVersionConflict = errors.New("stale version: node was modified concurrently")
	ErrQuotaExceeded   = errors.New("this upload would exceed the space's storage limit")
)

const (
	maxTreeDepth = 100
	// How many suffixed names a restore will try before giving up.
	maxRestoreNameAttempts = 50
)

// VersionHook is called after a new version is committed (e.g. to enqueue previews).
type VersionHook func(ctx context.Context, ver store.NodeVersion, name string)

type Service struct {
	q         *store.Queries
	blobs     blob.Store
	onVersion VersionHook
}

func NewService(q *store.Queries, blobs blob.Store) *Service {
	return &Service{q: q, blobs: blobs}
}

// SetVersionHook registers a post-commit callback (preview enqueue).
func (s *Service) SetVersionHook(h VersionHook) {
	s.onVersion = h
}
func (s *Service) Mkdir(ctx context.Context, spaceID, parentID pgtype.UUID, name string, creator pgtype.UUID) (store.Node, error) {
	if parentID.Valid {
		parent, err := s.get(ctx, parentID)
		if err != nil {
			return store.Node{}, err
		}
		if parent.Kind != store.NodeKindDir {
			return store.Node{}, ErrNotDir
		}
	}
	n, err := s.q.CreateNode(ctx, store.CreateNodeParams{
		SpaceID:   spaceID,
		ParentID:  parentID,
		Kind:      store.NodeKindDir,
		Name:      name,
		CreatedBy: creator,
	})
	if isUniqueViolation(err) {
		return store.Node{}, ErrNameConflict
	}
	return n, err
}

// PutFile streams content into the blob store and commits a new version.
// If fileID is zero, creates a new file node under parentID with name.
func (s *Service) PutFile(ctx context.Context, spaceID, parentID, fileID pgtype.UUID, name string, r io.Reader, creator pgtype.UUID) (store.Node, store.NodeVersion, error) {
	var node store.Node
	var err error

	if fileID.Valid {
		node, err = s.get(ctx, fileID)
		if err != nil {
			return store.Node{}, store.NodeVersion{}, err
		}
		if node.Kind != store.NodeKindFile {
			return store.Node{}, store.NodeVersion{}, ErrNotFile
		}
	} else {
		node, err = s.q.CreateNode(ctx, store.CreateNodeParams{
			SpaceID:   spaceID,
			ParentID:  parentID,
			Kind:      store.NodeKindFile,
			Name:      name,
			CreatedBy: creator,
		})
		if isUniqueViolation(err) {
			// Same name in this folder: treat as a new version of the existing file.
			existing, gErr := s.findChildByName(ctx, spaceID, parentID, name)
			if gErr != nil {
				return store.Node{}, store.NodeVersion{}, gErr
			}
			if existing.Kind != store.NodeKindFile {
				return store.Node{}, store.NodeVersion{}, ErrNameConflict
			}
			node = existing
		} else if err != nil {
			return store.Node{}, store.NodeVersion{}, err
		}
	}

	hash, size, err := s.blobs.Put(ctx, r)
	if err != nil {
		return store.Node{}, store.NodeVersion{}, fmt.Errorf("blob put: %w", err)
	}

	// Quota is checked after the bytes land, not before: the request does not
	// declare its length (a streamed multipart part has no reliable size), so
	// the only honest figure is the one measured on the way in. An upload that
	// breaches the limit is rejected and its blob left unreferenced, which the
	// GC sweep reclaims within the hour.
	if err := s.checkQuota(ctx, spaceID, node, size); err != nil {
		return store.Node{}, store.NodeVersion{}, err
	}

	mimeType := mime.TypeByExtension(filepath.Ext(node.Name))
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}

	ver, err := s.q.AddVersion(ctx, store.AddVersionParams{
		NodeID:    node.ID,
		BlobHash:  hash,
		Size:      size,
		Mime:      mimeType,
		CreatedBy: creator,
	})
	if err != nil {
		return store.Node{}, store.NodeVersion{}, err
	}
	if err := s.q.UpsertBlob(ctx, store.UpsertBlobParams{Hash: hash, Size: size}); err != nil {
		return node, ver, err
	}
	if err := s.q.IncrBlobRef(ctx, hash); err != nil {
		return node, ver, err
	}
	if err := s.q.SetCurrentVersion(ctx, store.SetCurrentVersionParams{ID: node.ID, CurrentVersionID: ver.ID}); err != nil {
		return node, ver, err
	}
	node.CurrentVersionID = ver.ID
	if s.onVersion != nil {
		s.onVersion(ctx, ver, node.Name)
	}
	return node, ver, nil
}

// checkQuota rejects an upload that would push the space past quota_bytes.
//
// For a new version of an existing file only the *difference* counts, since
// the old version's bytes stop counting toward usage the moment the new one
// becomes current. A space with quota 0 is unmetered.
func (s *Service) checkQuota(ctx context.Context, spaceID pgtype.UUID, node store.Node, incoming int64) error {
	space, err := s.q.GetSpace(ctx, spaceID)
	if err != nil {
		return err
	}
	if space.QuotaBytes <= 0 {
		return nil
	}

	used, err := s.q.SumSpaceUsage(ctx, spaceID)
	if err != nil {
		return err
	}

	delta := incoming
	if node.CurrentVersionID.Valid {
		if current, err := s.q.GetVersion(ctx, node.CurrentVersionID); err == nil {
			delta = incoming - current.Size
		}
	}
	if delta <= 0 {
		return nil
	}
	if used+delta > space.QuotaBytes {
		return ErrQuotaExceeded
	}
	return nil
}

// OpenFile returns the current version metadata and a reader for its blob.
func (s *Service) OpenFile(ctx context.Context, nodeID pgtype.UUID) (store.Node, store.NodeVersion, io.ReadCloser, error) {
	node, err := s.get(ctx, nodeID)
	if err != nil {
		return store.Node{}, store.NodeVersion{}, nil, err
	}
	if node.Kind != store.NodeKindFile {
		return store.Node{}, store.NodeVersion{}, nil, ErrNotFile
	}
	ver, err := s.q.GetCurrentVersion(ctx, nodeID)
	if err != nil {
		return store.Node{}, store.NodeVersion{}, nil, err
	}
	rc, err := s.blobs.Get(ctx, ver.BlobHash)
	if err != nil {
		if errors.Is(err, blob.ErrNotExist) {
			return store.Node{}, store.NodeVersion{}, nil, fmt.Errorf("blob %s missing on disk", ver.BlobHash[:12])
		}
		return store.Node{}, store.NodeVersion{}, nil, err
	}
	return node, ver, rc, nil
}

// List returns children of a folder (or space root when parent is zero).
func (s *Service) List(ctx context.Context, spaceID, parentID pgtype.UUID) ([]store.Node, error) {
	return s.q.ListChildren(ctx, store.ListChildrenParams{SpaceID: spaceID, ParentID: parentID})
}

// ListWithMeta returns children with latest version + owner metadata.
func (s *Service) ListWithMeta(ctx context.Context, spaceID, parentID pgtype.UUID) ([]store.NodeWithMeta, error) {
	rows, err := s.q.ListChildrenWithMeta(ctx, store.ListChildrenWithMetaParams{SpaceID: spaceID, ParentID: parentID})
	if err != nil {
		return nil, err
	}
	return childRowsToMeta(rows), nil
}

// ListGrantedWithMeta returns only the children of parentID that userID holds a
// grant on directly.
//
// This is the listing a member sees in a folder they have no access to
// themselves — most importantly the space root, where it becomes the list of
// the places their access starts. Callers must check the parent's rank first:
// access inherits downward, so anyone with a grant on an ancestor should get
// the full ListWithMeta instead.
func (s *Service) ListGrantedWithMeta(ctx context.Context, spaceID, parentID, userID pgtype.UUID) ([]store.NodeWithMeta, error) {
	rows, err := s.q.ListGrantedChildrenWithMeta(ctx, store.ListGrantedChildrenWithMetaParams{
		SpaceID:  spaceID,
		ParentID: parentID,
		UserID:   userID,
	})
	if err != nil {
		return nil, err
	}
	// The two queries select the same columns, so sqlc generates structurally
	// identical rows and one mapper serves both.
	plain := make([]store.ListChildrenWithMetaRow, len(rows))
	for i, r := range rows {
		plain[i] = store.ListChildrenWithMetaRow(r)
	}
	return childRowsToMeta(plain), nil
}

func childRowsToMeta(rows []store.ListChildrenWithMetaRow) []store.NodeWithMeta {
	out := make([]store.NodeWithMeta, len(rows))
	for i, r := range rows {
		ownerID := pgtype.UUID{}
		if r.OwnerUid.Valid {
			ownerID = r.OwnerUid
		}
		ownerName := ""
		if r.OwnerName.Valid {
			ownerName = r.OwnerName.String
		}
		ownerEmail := ""
		if r.OwnerEmail.Valid {
			ownerEmail = r.OwnerEmail.String
		}
		out[i] = store.NodeWithMeta{
			Node: store.Node{
				ID:               r.ID,
				TenantID:         r.TenantID,
				SpaceID:          r.SpaceID,
				ParentID:         r.ParentID,
				Kind:             store.NodeKind(r.Kind),
				Name:             r.Name,
				CurrentVersionID: r.CurrentVersionID,
				DeletedAt:        r.DeletedAt,
				CreatedBy:        r.CreatedBy,
				CreatedAt:        r.CreatedAt,
			},
			VerID:        r.VID,
			VerBlobHash:  r.BlobHash.String,
			VerSize:      r.VSize.Int64,
			VerMime:      r.Mime.String,
			VerVersionNo: int32(r.VersionNo.Int32),
			VerCreatedBy: r.VCreatedBy,
			VerCreatedAt: r.VCreatedAt,
			OwnerID:      ownerID,
			OwnerName:    ownerName,
			OwnerEmail:   ownerEmail,
		}
	}
	return out
}

func (s *Service) Get(ctx context.Context, nodeID pgtype.UUID) (store.Node, error) {
	return s.get(ctx, nodeID)
}

// Rename changes a node's name within its current parent.
func (s *Service) Rename(ctx context.Context, nodeID pgtype.UUID, name string) error {
	err := s.q.RenameNode(ctx, store.RenameNodeParams{ID: nodeID, Name: name})
	if isUniqueViolation(err) {
		return ErrNameConflict
	}
	return err
}

// Move relocates a node under a new parent (zero = space root).
func (s *Service) Move(ctx context.Context, nodeID, newParentID pgtype.UUID) error {
	if newParentID.Valid {
		parent, err := s.get(ctx, newParentID)
		if err != nil {
			return err
		}
		if parent.Kind != store.NodeKindDir {
			return ErrNotDir
		}
		// Cycle check: newParent must not be a descendant of node.
		isDesc, err := s.isDescendant(ctx, newParentID, nodeID)
		if err != nil {
			return err
		}
		if isDesc || newParentID == nodeID {
			return ErrCycle
		}
	}
	err := s.q.MoveNode(ctx, store.MoveNodeParams{ID: nodeID, ParentID: newParentID})
	if isUniqueViolation(err) {
		return ErrNameConflict
	}
	return err
}

// Delete tombstones a node, recording who did it so the trash can say so.
// Copy duplicates a node into destParent, recursing through folders.
//
// Files are copied by *reference*: the new version points at the same blob and
// bumps its ref_count. Content-addressed storage means the bytes are already
// identical, so copying a 2 GB video costs one row, not 2 GB — and the GC
// sweep will not reclaim the blob while either copy still points at it.
//
// Only the current version is carried over. History belongs to the file it
// happened to, and a copy is a new file that has not been edited yet.
func (s *Service) Copy(ctx context.Context, nodeID, destParent pgtype.UUID, creator pgtype.UUID) (store.Node, error) {
	node, err := s.get(ctx, nodeID)
	if err != nil {
		return store.Node{}, err
	}
	if destParent.Valid {
		parent, err := s.get(ctx, destParent)
		if err != nil {
			return store.Node{}, err
		}
		if parent.Kind != store.NodeKindDir {
			return store.Node{}, ErrNotDir
		}
		// Copying a folder into its own subtree would recurse forever.
		if node.Kind == store.NodeKindDir {
			inside, err := s.isDescendant(ctx, destParent, nodeID)
			if err != nil {
				return store.Node{}, err
			}
			if inside || destParent == nodeID {
				return store.Node{}, ErrCycle
			}
		}
	}
	return s.copyInto(ctx, node, destParent, node.Name, creator, 0)
}

func (s *Service) copyInto(
	ctx context.Context,
	src store.Node,
	destParent pgtype.UUID,
	name string,
	creator pgtype.UUID,
	depth int,
) (store.Node, error) {
	if depth >= maxTreeDepth {
		return store.Node{}, fmt.Errorf("folder nesting exceeds %d levels", maxTreeDepth)
	}

	// A copy landing beside its original needs a free name.
	finalName := name
	var created store.Node
	var err error
	for attempt := 1; attempt <= maxRestoreNameAttempts; attempt++ {
		created, err = s.q.CreateNode(ctx, store.CreateNodeParams{
			SpaceID:   src.SpaceID,
			ParentID:  destParent,
			Kind:      src.Kind,
			Name:      finalName,
			CreatedBy: creator,
		})
		if err == nil {
			break
		}
		if !isUniqueViolation(err) {
			return store.Node{}, err
		}
		finalName = suffixName(name, attempt+1)
	}
	if err != nil {
		return store.Node{}, ErrNameConflict
	}

	if src.Kind == store.NodeKindDir {
		children, err := s.q.ListChildren(ctx, store.ListChildrenParams{
			SpaceID: src.SpaceID, ParentID: src.ID,
		})
		if err != nil {
			return store.Node{}, err
		}
		for _, child := range children {
			if _, err := s.copyInto(ctx, child, created.ID, child.Name, creator, depth+1); err != nil {
				return store.Node{}, err
			}
		}
		return created, nil
	}

	// File: point a fresh version at the same blob.
	if !src.CurrentVersionID.Valid {
		return created, nil
	}
	current, err := s.q.GetVersion(ctx, src.CurrentVersionID)
	if err != nil {
		return store.Node{}, err
	}
	ver, err := s.q.AddVersion(ctx, store.AddVersionParams{
		NodeID:    created.ID,
		BlobHash:  current.BlobHash,
		Size:      current.Size,
		Mime:      current.Mime,
		CreatedBy: creator,
	})
	if err != nil {
		return store.Node{}, err
	}
	if err := s.q.IncrBlobRef(ctx, current.BlobHash); err != nil {
		return store.Node{}, err
	}
	if err := s.q.SetCurrentVersion(ctx, store.SetCurrentVersionParams{
		ID: created.ID, CurrentVersionID: ver.ID,
	}); err != nil {
		return store.Node{}, err
	}
	created.CurrentVersionID = ver.ID
	if s.onVersion != nil {
		s.onVersion(ctx, ver, created.Name)
	}
	return created, nil
}

func (s *Service) Delete(ctx context.Context, nodeID, actorID pgtype.UUID) error {
	return s.q.SoftDeleteNode(ctx, store.SoftDeleteNodeParams{ID: nodeID, DeletedBy: actorID})
}

// ExpiredTrash lists tombstones past their space's retention window.
func (s *Service) ExpiredTrash(ctx context.Context, limit int32) ([]store.ListExpiredTrashRow, error) {
	return s.q.ListExpiredTrash(ctx, limit)
}

// Versions lists the immutable version history of a file.
func (s *Service) Versions(ctx context.Context, nodeID pgtype.UUID) ([]store.NodeVersion, error) {
	return s.q.ListVersions(ctx, nodeID)
}

// RestoreVersion points the node at an older version.
func (s *Service) RestoreVersion(ctx context.Context, nodeID, versionID pgtype.UUID) error {
	ver, err := s.q.GetVersion(ctx, versionID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if ver.NodeID != nodeID {
		return ErrNotFound
	}
	return s.q.SetCurrentVersion(ctx, store.SetCurrentVersionParams{ID: nodeID, CurrentVersionID: versionID})
}

// ListDeleted returns soft-deleted nodes in a space.
func (s *Service) ListDeleted(ctx context.Context, spaceID pgtype.UUID) ([]store.Node, error) {
	return s.q.ListDeleted(ctx, spaceID)
}

// WalkFiles visits every file in the subtree rooted at dirID, depth-first,
// passing the folder path relative to the root ("" at the top).
//
// Used by the zip streamer, which needs one file open at a time rather than a
// materialised tree, so the walk hands back nodes as it finds them instead of
// building a slice.
func (s *Service) WalkFiles(
	ctx context.Context,
	dirID pgtype.UUID,
	visit func(dir string, node store.Node) error,
) error {
	root, err := s.get(ctx, dirID)
	if err != nil {
		return err
	}
	return s.walkFiles(ctx, root, "", 0, visit)
}

func (s *Service) walkFiles(
	ctx context.Context,
	dir store.Node,
	prefix string,
	depth int,
	visit func(dir string, node store.Node) error,
) error {
	if depth >= maxTreeDepth {
		return fmt.Errorf("folder nesting exceeds %d levels", maxTreeDepth)
	}
	children, err := s.q.ListChildren(ctx, store.ListChildrenParams{
		SpaceID:  dir.SpaceID,
		ParentID: dir.ID,
	})
	if err != nil {
		return err
	}
	for _, child := range children {
		if child.Kind == store.NodeKindDir {
			next := child.Name
			if prefix != "" {
				next = prefix + "/" + child.Name
			}
			if err := s.walkFiles(ctx, child, next, depth+1, visit); err != nil {
				return err
			}
			continue
		}
		if err := visit(prefix, child); err != nil {
			return err
		}
	}
	return nil
}

// --- Stars ---

// Star bookmarks a node for one user. Idempotent.
func (s *Service) Star(ctx context.Context, userID, nodeID pgtype.UUID) error {
	return s.q.StarNode(ctx, store.StarNodeParams{UserID: userID, NodeID: nodeID})
}

// Unstar removes the bookmark. Removing one that is not there is not an error.
func (s *Service) Unstar(ctx context.Context, userID, nodeID pgtype.UUID) error {
	return s.q.UnstarNode(ctx, store.UnstarNodeParams{UserID: userID, NodeID: nodeID})
}

// Starred lists a user's bookmarked nodes, newest star first. An invalid
// spaceID spans every space; a set one scopes the listing to that space.
func (s *Service) Starred(ctx context.Context, userID, spaceID pgtype.UUID, limit int32) ([]store.ListStarredNodesRow, error) {
	return s.q.ListStarredNodes(ctx, store.ListStarredNodesParams{UserID: userID, SpaceID: spaceID, Limit: limit})
}

// StarredAmong reports which of the given nodes the user has starred, so a
// listing can render star state in one query rather than one per row.
func (s *Service) StarredAmong(ctx context.Context, userID pgtype.UUID, nodeIDs []pgtype.UUID) (map[pgtype.UUID]bool, error) {
	if len(nodeIDs) == 0 {
		return map[pgtype.UUID]bool{}, nil
	}
	rows, err := s.q.ListStarredIDs(ctx, store.ListStarredIDsParams{UserID: userID, Column2: nodeIDs})
	if err != nil {
		return nil, err
	}
	out := make(map[pgtype.UUID]bool, len(rows))
	for _, id := range rows {
		out[id] = true
	}
	return out, nil
}

// --- Quota ---

// RecountUsage recomputes a space's used_bytes from its live files and stores
// it. Called after anything that changes the total rather than by a background
// job, so the rail's storage meter is right the moment a user looks at it. The
// query is a single indexed sum; at MVP scale this is cheaper than the
// machinery a job would need.
func (s *Service) RecountUsage(ctx context.Context, spaceID pgtype.UUID) error {
	total, err := s.q.SumSpaceUsage(ctx, spaceID)
	if err != nil {
		return err
	}
	return s.q.UpdateSpaceUsedBytes(ctx, store.UpdateSpaceUsedBytesParams{ID: spaceID, UsedBytes: total})
}

// UsageByCategory breaks usage down by broad MIME family across the user's
// spaces, or one space when spaceID is set.
func (s *Service) UsageByCategory(ctx context.Context, userID, spaceID pgtype.UUID) ([]store.SumSpaceUsageByCategoryRow, error) {
	return s.q.SumSpaceUsageByCategory(ctx, store.SumSpaceUsageByCategoryParams{UserID: userID, SpaceID: spaceID})
}

// Search runs a name search scoped to the caller's space membership.
func (s *Service) Search(ctx context.Context, arg store.SearchNodesParams) ([]store.SearchNodesRow, error) {
	return s.q.SearchNodes(ctx, arg)
}

// ListShared returns the places shared with the caller — the topmost nodes
// they hold a grant on. An invalid spaceID spans every space; a set one scopes
// the listing to that space. includeOwned is always false here, so own items
// are excluded — your own folder is not something shared with you.
func (s *Service) ListShared(ctx context.Context, userID, spaceID pgtype.UUID) ([]store.NodeWithMeta, error) {
	return s.EntryPointsWithMeta(ctx, userID, spaceID, false)
}

// EntryPointsWithMeta returns the topmost nodes the caller can reach. includeOwned
// adds their own created items (nodes where created_by = user). A zero spaceID
// spans every space; a set one scopes to that space — the member's top level.
func (s *Service) EntryPointsWithMeta(ctx context.Context, userID, spaceID pgtype.UUID, includeOwned bool) ([]store.NodeWithMeta, error) {
	rows, err := s.q.ListEntryPoints(ctx, store.ListEntryPointsParams{
		UserID:        userID,
		SpaceID:       spaceID,
		IncludeOwned:  includeOwned,
	})
	if err != nil {
		return nil, err
	}
	// Selects the same columns as ListChildrenWithMeta, so one mapper serves
	// both and an entry point renders with the size and owner a listing shows.
	plain := make([]store.ListChildrenWithMetaRow, len(rows))
	for i, r := range rows {
		plain[i] = store.ListChildrenWithMetaRow(r)
	}
	return childRowsToMeta(plain), nil
}

// HasChildren reports whether a node has any child nodes.
func (s *Service) HasChildren(ctx context.Context, nodeID pgtype.UUID) (int64, error) {
	return s.q.HasChildren(ctx, nodeID)
}

// isDescendant reports whether nodeID is reachable by walking up from candidateID
// (i.e., candidateID sits inside the subtree rooted at nodeID).
func (s *Service) isDescendant(ctx context.Context, candidateID, nodeID pgtype.UUID) (bool, error) {
	cur := candidateID
	for depth := 0; depth < maxTreeDepth; depth++ {
		if !cur.Valid {
			return false, nil
		}
		if cur == nodeID {
			return true, nil
		}
		parent, err := s.q.GetNodeParent(ctx, cur)
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		cur = parent
	}
	return false, fmt.Errorf("tree depth exceeded %d", maxTreeDepth)
}

func (s *Service) get(ctx context.Context, id pgtype.UUID) (store.Node, error) {
	n, err := s.q.GetNode(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return store.Node{}, ErrNotFound
	}
	return n, err
}

// findChildByName returns the live child of parentID with the exact name.
func (s *Service) findChildByName(ctx context.Context, spaceID, parentID pgtype.UUID, name string) (store.Node, error) {
	children, err := s.q.ListChildren(ctx, store.ListChildrenParams{SpaceID: spaceID, ParentID: parentID})
	if err != nil {
		return store.Node{}, err
	}
	for _, c := range children {
		if c.Name == name {
			return c, nil
		}
	}
	return store.Node{}, ErrNotFound
}

func isUniqueViolation(err error) bool {
	var pgErr interface{ SQLState() string }
	if errors.As(err, &pgErr) {
		return pgErr.SQLState() == "23505"
	}
	return false
}

// --- Trash lifecycle ---

// ErrNotDeleted is returned when a trash operation targets a live node.
var ErrNotDeleted = errors.New("node is not in the trash")

// RestoreOutcome reports the adjustments a restore had to make to land the
// node somewhere valid. Both are surfaced to the user rather than applied
// silently — "where did my file go" is the whole point of the trash.
type RestoreOutcome struct {
	// Relocated is true when the original parent was itself trashed and the
	// node was put back at the top level of its space instead.
	Relocated bool
	// RenamedFrom is set when a live sibling had taken the original name and
	// the node came back under a suffixed one.
	RenamedFrom string
}

// GetAny resolves a node whether or not it is soft-deleted.
func (s *Service) GetAny(ctx context.Context, nodeID pgtype.UUID) (store.Node, error) {
	n, err := s.q.GetNodeAny(ctx, nodeID)
	if errors.Is(err, pgx.ErrNoRows) {
		return store.Node{}, ErrNotFound
	}
	return n, err
}

// Restore lifts a node out of the trash.
//
// Two things can be in the way. The original parent may itself be trashed, in
// which case there is no folder to return to and the node goes to the space
// root. And a live sibling may have taken the name in the meantime, which the
// partial unique index on (space_id, parent_id, name) WHERE deleted_at IS NULL
// rejects — so the name gets a numeric suffix. Restoring a folder implicitly
// restores its subtree, because Delete only tombstones the node it was given.
func (s *Service) Restore(ctx context.Context, nodeID pgtype.UUID) (store.Node, RestoreOutcome, error) {
	node, err := s.GetAny(ctx, nodeID)
	if err != nil {
		return store.Node{}, RestoreOutcome{}, err
	}
	if !node.DeletedAt.Valid {
		return store.Node{}, RestoreOutcome{}, ErrNotDeleted
	}

	var out RestoreOutcome
	parentID := node.ParentID
	buried, err := s.hasDeletedAncestor(ctx, node)
	if err != nil {
		return store.Node{}, RestoreOutcome{}, err
	}
	if buried {
		parentID = pgtype.UUID{} // space root
		out.Relocated = true
	}

	name := node.Name
	for attempt := 1; attempt <= maxRestoreNameAttempts; attempt++ {
		err = s.q.RestoreNode(ctx, store.RestoreNodeParams{ID: nodeID, ParentID: parentID, Name: name})
		if err == nil {
			if name != node.Name {
				out.RenamedFrom = node.Name
			}
			node.DeletedAt = pgtype.Timestamptz{}
			node.ParentID = parentID
			node.Name = name
			return node, out, nil
		}
		if !isUniqueViolation(err) {
			return store.Node{}, RestoreOutcome{}, err
		}
		name = suffixName(node.Name, attempt+1)
	}
	return store.Node{}, RestoreOutcome{}, ErrNameConflict
}

// Purge permanently removes a trashed node and its subtree, and releases the
// blobs the subtree's versions were holding.
//
// The delete and the ref-count decrements are not in one transaction — the fs
// service holds a *store.Queries, not a pool. They are ordered so that a crash
// between them leaves ref counts too HIGH, which only delays garbage
// collection. The reverse order could drop a blob another node still points at.
func (s *Service) Purge(ctx context.Context, nodeID pgtype.UUID) error {
	node, err := s.GetAny(ctx, nodeID)
	if err != nil {
		return err
	}
	if !node.DeletedAt.Valid {
		return ErrNotDeleted
	}
	hashes, err := s.q.ListSubtreeBlobHashes(ctx, nodeID)
	if err != nil {
		return err
	}
	if err := s.q.PurgeNode(ctx, nodeID); err != nil {
		return err
	}
	for _, h := range hashes {
		if err := s.q.DecrBlobRef(ctx, h); err != nil {
			return fmt.Errorf("release blob %s: %w", h[:min(12, len(h))], err)
		}
	}
	return nil
}

// EmptyTrash purges every trashed node in a space and returns how many it
// removed. Nested tombstones disappear with their ancestor's cascade, so a
// child already gone by the time its turn comes is not an error.
func (s *Service) EmptyTrash(ctx context.Context, spaceID pgtype.UUID) (int, error) {
	ids, err := s.q.ListDeletedIDs(ctx, spaceID)
	if err != nil {
		return 0, err
	}
	purged := 0
	for _, id := range ids {
		switch err := s.Purge(ctx, id); {
		case err == nil:
			purged++
		case errors.Is(err, ErrNotFound):
			// Already removed by an ancestor's cascade.
		default:
			return purged, err
		}
	}
	return purged, nil
}

// ListDeletedWithMeta returns the trash listing with version, owner, original
// location and child-count metadata.
func (s *Service) ListDeletedWithMeta(ctx context.Context, spaceID pgtype.UUID) ([]store.ListDeletedWithMetaRow, error) {
	return s.q.ListDeletedWithMeta(ctx, spaceID)
}

// hasDeletedAncestor walks up from node looking for a tombstoned parent.
func (s *Service) hasDeletedAncestor(ctx context.Context, node store.Node) (bool, error) {
	cur := node.ParentID
	for depth := 0; depth < maxTreeDepth && cur.Valid; depth++ {
		parent, err := s.q.GetNodeAny(ctx, cur)
		if errors.Is(err, pgx.ErrNoRows) {
			// Parent row is gone entirely (purged): nothing to return to.
			return true, nil
		}
		if err != nil {
			return false, err
		}
		if parent.DeletedAt.Valid {
			return true, nil
		}
		cur = parent.ParentID
	}
	return false, nil
}

// suffixName turns "report.pdf" into "report (2).pdf", keeping the extension
// where there is one so the restored file still opens in the right app.
func suffixName(name string, n int) string {
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	// A dotfile is all "extension" as far as filepath is concerned; suffixing
	// it would produce a leading space, so treat the whole thing as the base.
	if base == "" {
		base, ext = name, ""
	}
	return fmt.Sprintf("%s (%d)%s", base, n, ext)
}
