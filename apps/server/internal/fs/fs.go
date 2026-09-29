// Package fs implements the file tree: nodes, versions, upload/download.
// See docs/architecture/data-model.md and storage.md.
package fs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/blob"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

var (
	ErrNotFound      = errors.New("node not found")
	ErrNotFile       = errors.New("node is not a file")
	ErrNotDir        = errors.New("node is not a directory")
	ErrCycle         = errors.New("cannot move a folder into its own descendant")
	ErrNameConflict  = errors.New("a node with this name already exists in the target folder")
	ErrQuotaExceeded = errors.New("this upload would exceed the space's storage limit")
	// ErrTeamRootFixed refuses to nest or relocate a team folder: a team folder
	// is always top level, or "which container owns this" stops having one
	// answer.
	ErrTeamRootFixed = errors.New("a team folder cannot be moved or nested")
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
	onRestore RestoreHook
	retention func() int
}

func NewService(q *store.Queries, blobs blob.Store) *Service {
	return &Service{q: q, blobs: blobs}
}

// SetVersionHook registers a post-commit callback (preview enqueue).
func (s *Service) SetVersionHook(h VersionHook) {
	s.onVersion = h
}

// RestoreHook is called after RestoreVersion re-points a node at an older
// version. No version is committed, so the VersionHook does not fire; what
// is keyed to the node's current version (the content-search index) still
// has to follow.
type RestoreHook func(ctx context.Context, ver store.NodeVersion)

// SetRestoreHook registers the post-restore callback (text re-extraction).
func (s *Service) SetRestoreHook(h RestoreHook) {
	s.onRestore = h
}

// SetVersionRetention registers how many versions of a file to keep, read at
// every save so an administrator's change applies without a restart. Unset,
// or answering 0, keeps every version.
func (s *Service) SetVersionRetention(keep func() int) {
	s.retention = keep
}

// inTx runs fn with a copy of the service bound to one transaction, so every
// statement fn issues — through s.q or any helper — commits or rolls back as
// one unit. Nested calls open savepoints.
func (s *Service) inTx(ctx context.Context, fn func(*Service) error) error {
	return s.q.InTx(ctx, func(q *store.Queries) error {
		tx := *s
		tx.q = q
		return fn(&tx)
	})
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
//
// If fileID is zero this always creates a NEW node: a name already taken in
// the folder gets a " (2)" suffix rather than a version stacked onto whatever
// was already there. Versioning silently made ownership depend on a name
// collision — upload into a colleague's folder and you owned your file, unless
// somebody had used the name first, in which case you owned nothing and had
// just overwritten their work. Replacing a file is an explicit act now: an
// upload that names the node (fileID).
func (s *Service) PutFile(ctx context.Context, spaceID, parentID, fileID pgtype.UUID, name string, r io.Reader, creator pgtype.UUID) (store.Node, store.NodeVersion, error) {
	return s.putFile(ctx, spaceID, parentID, fileID, name, creator, func(ctx context.Context) (string, int64, error) {
		return s.blobs.Put(ctx, r)
	})
}

// PutStagedFile is PutFile for content already complete on local disk — a
// finished resumable upload. The blob store takes the file over (see
// blob.Store.Ingest); on failure it is left at path for the caller.
func (s *Service) PutStagedFile(ctx context.Context, spaceID, parentID, fileID pgtype.UUID, name, path string, creator pgtype.UUID) (store.Node, store.NodeVersion, error) {
	return s.putFile(ctx, spaceID, parentID, fileID, name, creator, func(ctx context.Context) (string, int64, error) {
		return s.blobs.Ingest(ctx, path)
	})
}

func (s *Service) putFile(
	ctx context.Context,
	spaceID, parentID, fileID pgtype.UUID,
	name string,
	creator pgtype.UUID,
	putBlob func(context.Context) (string, int64, error),
) (store.Node, store.NodeVersion, error) {
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
	}

	// The store claims the hash before installing the bytes, so from here
	// the blob has a row and is pinned against GC. Nothing below needs to
	// clean it up on failure: an unused claim lapses and the sweep reclaims
	// the bytes like any other unreferenced blob.
	hash, size, err := putBlob(ctx)
	if err != nil {
		return store.Node{}, store.NodeVersion{}, fmt.Errorf("blob put: %w", err)
	}

	// One transaction for the file and its version, so no failure leaves a
	// node without content or a version that is not current. The version row
	// is the blob reference; a trigger counts it in the same statement.
	var ver store.NodeVersion
	err = s.inTx(ctx, func(s *Service) error {
		// Quota is checked after the bytes land, not before: the declared
		// length of a request is not trustworthy, so the only honest figure
		// is the one measured on the way in. It runs before the node exists,
		// so a refused upload leaves no empty file behind in the folder.
		if err := s.EnsureRoom(ctx, spaceID, size); err != nil {
			return err
		}
		if !fileID.Valid {
			var err error
			node, err = s.createWithFreeName(ctx, store.CreateNodeParams{
				SpaceID:   spaceID,
				ParentID:  parentID,
				Kind:      store.NodeKindFile,
				Name:      name,
				CreatedBy: creator,
			})
			if err != nil {
				return err
			}
		}
		mimeType := mime.TypeByExtension(filepath.Ext(node.Name))
		if mimeType == "" {
			mimeType = "application/octet-stream"
		}
		var err error
		ver, err = s.q.AddVersion(ctx, store.AddVersionParams{
			NodeID:    node.ID,
			BlobHash:  hash,
			Size:      size,
			Mime:      mimeType,
			CreatedBy: creator,
		})
		if err != nil {
			return err
		}
		return s.q.SetCurrentVersion(ctx, store.SetCurrentVersionParams{ID: node.ID, CurrentVersionID: ver.ID})
	})
	if err != nil {
		return store.Node{}, store.NodeVersion{}, err
	}
	node.CurrentVersionID = ver.ID
	if err := s.pruneVersions(ctx, node.ID); err != nil {
		// The new version is committed; a failed prune only leaves history
		// longer than the policy asks for, and the next save retries it.
		slog.WarnContext(ctx, "version prune failed", "node", node.ID, "err", err)
	}
	if err := s.RecountUsage(ctx, spaceID); err != nil {
		slog.WarnContext(ctx, "usage recount failed", "space", spaceID, "err", err)
	}
	if s.onVersion != nil {
		s.onVersion(ctx, ver, node.Name)
	}
	return node, ver, nil
}

// createWithFreeName creates p, suffixing the name (`report (2).txt`) while
// it is taken. Each attempt runs in a savepoint of its own: inside a larger
// transaction a unique violation would otherwise abort the whole of it.
func (s *Service) createWithFreeName(ctx context.Context, p store.CreateNodeParams) (store.Node, error) {
	base := p.Name
	for attempt := 1; attempt <= maxRestoreNameAttempts; attempt++ {
		var created store.Node
		err := s.inTx(ctx, func(s *Service) error {
			var err error
			created, err = s.q.CreateNode(ctx, p)
			return err
		})
		if err == nil {
			return created, nil
		}
		if !isUniqueViolation(err) {
			return store.Node{}, err
		}
		p.Name = suffixName(base, attempt+1)
	}
	return store.Node{}, ErrNameConflict
}

// pruneVersions applies the version retention policy to one file: every
// version beyond the newest keep−1 non-current ones is deleted, and the
// triggers release its blob (and its previews') to GC. The current version
// always survives and counts as one of the kept, so a policy of 1 keeps only
// the file as it is now.
func (s *Service) pruneVersions(ctx context.Context, nodeID pgtype.UUID) error {
	if s.retention == nil {
		return nil
	}
	keep := s.retention()
	if keep <= 0 {
		return nil // 0 keeps every version
	}
	return s.q.PruneVersions(ctx, store.PruneVersionsParams{NodeID: nodeID, Keep: int32(keep - 1)})
}

// EnsureRoom rejects adding incoming bytes to a space that would push it past
// quota_bytes. Usage counts every stored version, trash included, so a new
// version costs its full size: the version it supersedes is still on disk. A
// space with quota 0 is unmetered.
func (s *Service) EnsureRoom(ctx context.Context, spaceID pgtype.UUID, incoming int64) error {
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
	if used+incoming > space.QuotaBytes {
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
				TeamRootID:       r.TeamRootID,
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
//
// A move is the only operation that changes which container a node belongs to,
// so it is also the only place `team_root_id` has to be maintained — across
// the whole subtree, since containment is inherited. Leaving a team folder
// additionally reassigns authorship to the mover: outside a container
// `created_by` IS access, and content arriving in personal space owned by
// whoever happened to upload it inside the team would hand them back a private
// copy of the team's work. Drive transfers ownership on the way out of a
// shared drive for the same reason; `node_versions.created_by` keeps the real
// authors.
//
// The three writes — reparent, container, authorship — are one transaction: a
// crash between them would leave personal content still marked as a team's,
// or team content with no personal owner, and every resolution query trusts
// those columns.
func (s *Service) Move(ctx context.Context, nodeID, newParentID, actorID pgtype.UUID) error {
	return s.inTx(ctx, func(s *Service) error { return s.move(ctx, nodeID, newParentID, actorID) })
}

func (s *Service) move(ctx context.Context, nodeID, newParentID, actorID pgtype.UUID) error {
	node, err := s.get(ctx, nodeID)
	if err != nil {
		return err
	}
	if node.TeamRootID == node.ID {
		return ErrTeamRootFixed
	}
	var destTeam pgtype.UUID
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
		destTeam = parent.TeamRootID
	}
	err = s.q.MoveNode(ctx, store.MoveNodeParams{ID: nodeID, ParentID: newParentID})
	if isUniqueViolation(err) {
		return ErrNameConflict
	}
	if err != nil || node.TeamRootID == destTeam {
		return err
	}
	if err := s.q.SetSubtreeTeamRoot(ctx, store.SetSubtreeTeamRootParams{
		RootID: nodeID, TeamRootID: destTeam,
	}); err != nil {
		return err
	}
	if !destTeam.Valid {
		return s.q.ReassignAuthor(ctx, store.ReassignAuthorParams{RootID: nodeID, UserID: actorID})
	}
	return nil
}

// CreateTeamFolder makes a top-level folder the team owns: `created_by` records
// who set it up but grants nothing, so access comes only from grants on the
// folder. The caller gets the first manager grant (edit + download + share) in
// the same transaction — without it the folder would be unreachable the
// moment it existed, including to its creator, and a crash between the two
// writes would leave exactly that.
func (s *Service) CreateTeamFolder(ctx context.Context, spaceID pgtype.UUID, name string, creator pgtype.UUID) (store.Node, error) {
	var node store.Node
	err := s.inTx(ctx, func(s *Service) error {
		n, err := s.q.CreateTeamRoot(ctx, store.CreateTeamRootParams{
			SpaceID: spaceID, Name: name, CreatedBy: creator,
		})
		if isUniqueViolation(err) {
			return ErrNameConflict
		}
		if err != nil {
			return err
		}
		if err := s.q.MarkTeamRoot(ctx, n.ID); err != nil {
			return err
		}
		n.TeamRootID = n.ID
		if _, err := s.q.CreateShare(ctx, store.CreateShareParams{
			NodeID:        n.ID,
			Kind:          store.ShareKindDirect,
			GranteeUserID: creator,
			Level:         store.ShareLevelEdit,
			CanDownload:   true,
			CanShare:      true,
			CreatedBy:     creator,
		}); err != nil {
			return err
		}
		node = n
		return nil
	})
	return node, err
}

// TeamRoots lists a space's team folders.
func (s *Service) TeamRoots(ctx context.Context, spaceID pgtype.UUID) ([]pgtype.UUID, error) {
	return s.q.ListTeamRoots(ctx, spaceID)
}

// TeamManagerCount reports how many live grants on a team folder carry
// can_share. Zero means nobody can administer it.
func (s *Service) TeamManagerCount(ctx context.Context, teamRootID pgtype.UUID) (int64, error) {
	return s.q.CountTeamManagers(ctx, teamRootID)
}

// Delete tombstones a node, recording who did it so the trash can say so.
// Copy duplicates a node into destParent, recursing through folders.
//
// Files are copied by *reference*: the new version points at the same blob,
// and its row is one more reference to it. Content-addressed storage means
// the bytes are already identical, so copying a 2 GB video costs one row, not
// 2 GB — and the GC sweep will not reclaim the blob while either copy still
// points at it.
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
	params := store.CreateNodeParams{
		SpaceID:   src.SpaceID,
		ParentID:  destParent,
		Kind:      src.Kind,
		Name:      name,
		CreatedBy: creator,
	}

	if src.Kind == store.NodeKindDir {
		created, err := s.createWithFreeName(ctx, params)
		if err != nil {
			return store.Node{}, err
		}
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

	// File: a new node whose version points at the same blob, created as one
	// unit so a failure never leaves a copy without content. The version row
	// is the new reference; a trigger counts it. A block document's
	// attachments come along the same way, so its `attachment:` references
	// resolve in the copy without rewriting the markdown.
	var created store.Node
	var ver store.NodeVersion
	err := s.inTx(ctx, func(s *Service) error {
		var err error
		created, err = s.createWithFreeName(ctx, params)
		if err != nil || !src.CurrentVersionID.Valid {
			return err
		}
		current, err := s.q.GetVersion(ctx, src.CurrentVersionID)
		if err != nil {
			return err
		}
		ver, err = s.q.AddVersion(ctx, store.AddVersionParams{
			NodeID:    created.ID,
			BlobHash:  current.BlobHash,
			Size:      current.Size,
			Mime:      current.Mime,
			CreatedBy: creator,
		})
		if err != nil {
			return err
		}
		if err := s.q.SetCurrentVersion(ctx, store.SetCurrentVersionParams{
			ID: created.ID, CurrentVersionID: ver.ID,
		}); err != nil {
			return err
		}
		return s.q.CopyAttachments(ctx, store.CopyAttachmentsParams{
			DestID: created.ID, SrcID: src.ID, CreatedBy: creator,
		})
	})
	if err != nil {
		return store.Node{}, err
	}
	if !ver.ID.Valid {
		return created, nil
	}
	created.CurrentVersionID = ver.ID
	// After the commit: the hook enqueues a preview job another process may
	// pick up at once, and it must find the version there.
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
	if err := s.q.SetCurrentVersion(ctx, store.SetCurrentVersionParams{ID: nodeID, CurrentVersionID: versionID}); err != nil {
		return err
	}
	if s.onRestore != nil {
		s.onRestore(ctx, ver)
	}
	return nil
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

// RecountUsage recomputes a space's used_bytes from every stored version,
// trash included, and stores it. Called after anything that changes the total
// rather than by a background job, so the rail's storage meter is right the
// moment a user looks at it. The
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
		UserID:       userID,
		SpaceID:      spaceID,
		IncludeOwned: includeOwned,
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

// RescuedNode reports one subtree a purge refused to destroy.
type RescuedNode struct {
	Node  store.Node
	Owner pgtype.UUID
}

// Purge permanently removes a trashed node and its subtree. The versions go
// with it by cascade, and the triggers on them release their blobs to GC.
//
// Content the node's owner did not create is lifted out first. A folder owner
// can trash and purge what sits inside their folder, but destroying a
// colleague's file as a side effect of housekeeping is not a power anybody
// asked for — Drive never deletes another account's file when you empty your
// own trash. Each rescued subtree lands at its own owner's top level, keeping
// its deleted_at, so a file the colleague had trashed themselves stays in
// their trash instead of quietly coming back.
//
// Rescue and delete are one transaction, so a failure part-way leaves
// nothing half done: no rescued subtree without the purge. A failed purge
// therefore reports no rescued nodes — none of them moved.
func (s *Service) Purge(ctx context.Context, nodeID pgtype.UUID) ([]RescuedNode, error) {
	var rescued []RescuedNode
	err := s.inTx(ctx, func(s *Service) error {
		node, err := s.GetAny(ctx, nodeID)
		if err != nil {
			return err
		}
		if !node.DeletedAt.Valid {
			return ErrNotDeleted
		}
		rescued, err = s.rescueForeignSubtrees(ctx, node)
		if err != nil {
			return err
		}
		return s.q.PurgeNode(ctx, nodeID)
	})
	if err != nil {
		return nil, err
	}
	return rescued, nil
}

// rescueForeignSubtrees detaches every topmost node under root that root's
// owner did not create, so the cascade cannot reach it.
//
// Inside a team folder there is nothing to rescue: the container owns the whole
// subtree, `created_by` is a byline, and scattering a team's files across the
// personal top levels of whoever happened to upload them would be the opposite
// of what a team folder is for. A manager purging team content purges it.
func (s *Service) rescueForeignSubtrees(ctx context.Context, root store.Node) ([]RescuedNode, error) {
	if root.TeamRootID.Valid {
		return nil, nil
	}
	foreign, err := s.q.ListForeignTopmostInSubtree(ctx, store.ListForeignTopmostInSubtreeParams{
		RootID: root.ID,
		UserID: root.CreatedBy,
	})
	if err != nil {
		return nil, err
	}
	rescued := make([]RescuedNode, 0, len(foreign))
	for _, f := range foreign {
		node, err := s.GetAny(ctx, f.ID)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				continue
			}
			return rescued, err
		}
		// The top level is a folder like any other as far as the partial unique
		// index is concerned, so the name may already be taken up there.
		name := node.Name
		for attempt := 1; ; attempt++ {
			// Each attempt in its own savepoint: a unique violation aborts the
			// enclosing transaction otherwise, and the retry would fail on that.
			err = s.q.InTx(ctx, func(q *store.Queries) error {
				return q.DetachNodeToTopLevel(ctx, store.DetachNodeToTopLevelParams{ID: node.ID, Name: name})
			})
			if !isUniqueViolation(err) {
				break
			}
			if attempt >= maxRestoreNameAttempts {
				return rescued, ErrNameConflict
			}
			name = suffixName(node.Name, attempt+1)
		}
		if err != nil {
			return rescued, err
		}
		node.ParentID = pgtype.UUID{}
		node.Name = name
		rescued = append(rescued, RescuedNode{Node: node, Owner: f.CreatedBy})
	}
	return rescued, nil
}

// ForeignOwnedCount reports how many live nodes under nodeID somebody other
// than userID created. A destructive action says so before it runs.
func (s *Service) ForeignOwnedCount(ctx context.Context, nodeID, userID pgtype.UUID) (int64, error) {
	return s.q.CountForeignOwnedInSubtree(ctx, store.CountForeignOwnedInSubtreeParams{
		RootID: nodeID,
		UserID: userID,
	})
}

// InheritedAccess is everyone reaching a node from above it — owners of the
// node or an ancestor, and grants on an ancestor — one row per principal.
// Rows come nearest-first and keep the nearest folder as `via`; a principal
// reached through several folders gets the union of what they grant, since
// that is what resolution answers. An owner row absorbs the same person's
// grants: owning outranks anything a grant adds.
func (s *Service) InheritedAccess(ctx context.Context, nodeID pgtype.UUID) ([]store.ListInheritedAccessRow, error) {
	rows, err := s.q.ListInheritedAccess(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	owners := map[pgtype.UUID]bool{}
	for _, r := range rows {
		if r.Kind == "owner" {
			owners[r.PrincipalID] = true
		}
	}
	index := map[string]int{}
	out := make([]store.ListInheritedAccessRow, 0, len(rows))
	for _, r := range rows {
		if r.Kind == "user" && owners[r.PrincipalID] {
			continue
		}
		key := r.Kind + ":" + r.PrincipalID.String()
		if i, seen := index[key]; seen {
			if r.Level == store.ShareLevelEdit {
				out[i].Level = store.ShareLevelEdit
			}
			out[i].CanDownload = out[i].CanDownload || r.CanDownload
			out[i].CanShare = out[i].CanShare || r.CanShare
			continue
		}
		index[key] = len(out)
		out = append(out, r)
	}
	return out, nil
}

// Principal is somebody (or something) that reaches a node by inheritance:
// a person, a group, or a share link.
type Principal struct {
	Kind string // user | group | link
	ID   pgtype.UUID
	Name string
}

// MoveImpact is the access delta a move would cause.
type MoveImpact struct {
	Losing  []Principal
	Gaining []Principal
}

// MoveImpact answers "who gains and who loses access if this node moves
// there" without moving anything.
//
// Access is inherited, so a move rehangs the node under a different chain of
// owners and grants. Drive's answer to the same question is a warning on the
// move dialog, and the reason it matters here is sharper: a member may move a
// file they created out of a colleague's folder, which is the documented way
// to take something back — but it should not be possible to do that, or the
// reverse, without being told who it affects.
//
// Grants on the node itself and its own creator travel with it, so principals
// reaching it that way appear on neither list. A zero destID means the
// caller's top level, which inherits nothing.
func (s *Service) MoveImpact(ctx context.Context, node store.Node, destID pgtype.UUID) (MoveImpact, error) {
	withSelf, err := s.chainPrincipals(ctx, node.ID)
	if err != nil {
		return MoveImpact{}, err
	}
	above, err := s.chainPrincipals(ctx, node.ParentID)
	if err != nil {
		return MoveImpact{}, err
	}
	dest, err := s.chainPrincipals(ctx, destID)
	if err != nil {
		return MoveImpact{}, err
	}
	// What the node carries on its own: everything it grants minus what it
	// only inherits.
	self := make(map[string]Principal, len(withSelf))
	for k, p := range withSelf {
		if _, inherited := above[k]; !inherited {
			self[k] = p
		}
	}
	out := MoveImpact{}
	for k, p := range above {
		if _, keeps := dest[k]; keeps {
			continue
		}
		if _, keeps := self[k]; keeps {
			continue
		}
		out.Losing = append(out.Losing, p)
	}
	for k, p := range dest {
		if _, had := above[k]; had {
			continue
		}
		if _, had := self[k]; had {
			continue
		}
		out.Gaining = append(out.Gaining, p)
	}
	sortPrincipals(out.Losing)
	sortPrincipals(out.Gaining)
	return out, nil
}

// chainPrincipals keys the query's rows so the three sets can be differenced.
// A zero id is the top level of a space: nothing is inherited there.
func (s *Service) chainPrincipals(ctx context.Context, rootID pgtype.UUID) (map[string]Principal, error) {
	out := map[string]Principal{}
	if !rootID.Valid {
		return out, nil
	}
	rows, err := s.q.ListChainPrincipals(ctx, rootID)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		p := Principal{Kind: r.Kind, ID: r.PrincipalID, Name: r.Name}
		out[r.Kind+":"+fmt.Sprintf("%x", r.PrincipalID.Bytes)] = p
	}
	return out, nil
}

func sortPrincipals(ps []Principal) {
	sort.Slice(ps, func(i, j int) bool {
		if ps[i].Kind != ps[j].Kind {
			return ps[i].Kind < ps[j].Kind
		}
		return ps[i].Name < ps[j].Name
	})
}

// TrashRootsWithUserContent lists the tombstones in a space that bury live
// nodes userID created, with how many. Those rows appear in that user's trash
// even when they hold no capability on the trashed folder itself: their work
// is inside it, and a purge is coming.
func (s *Service) TrashRootsWithUserContent(ctx context.Context, spaceID, userID pgtype.UUID) (map[pgtype.UUID]int64, error) {
	rows, err := s.q.ListTrashRootsWithUserContent(ctx, store.ListTrashRootsWithUserContentParams{
		SpaceID: spaceID,
		UserID:  userID,
	})
	if err != nil {
		return nil, err
	}
	out := make(map[pgtype.UUID]int64, len(rows))
	for _, r := range rows {
		out[r.TombstoneID] = r.OwnCount
	}
	return out, nil
}

// OwnedCount reports how many live nodes under nodeID userID created. Used by
// the share dialog: revoking a grant leaves everything the grantee made behind,
// and the number is the honest way to say so.
func (s *Service) OwnedCount(ctx context.Context, nodeID, userID pgtype.UUID) (int64, error) {
	return s.q.CountOwnedInSubtree(ctx, store.CountOwnedInSubtreeParams{
		RootID: nodeID,
		UserID: userID,
	})
}

// EmptyTrash purges every trashed node in a space and returns how many it
// removed, plus whatever the purges refused to destroy. Nested tombstones
// disappear with their ancestor's cascade, so a child already gone by the time
// its turn comes is not an error.
func (s *Service) EmptyTrash(ctx context.Context, spaceID pgtype.UUID) (int, []RescuedNode, error) {
	ids, err := s.q.ListDeletedIDs(ctx, spaceID)
	if err != nil {
		return 0, nil, err
	}
	purged := 0
	var rescued []RescuedNode
	for _, id := range ids {
		saved, err := s.Purge(ctx, id)
		rescued = append(rescued, saved...)
		switch {
		case err == nil:
			purged++
		case errors.Is(err, ErrNotFound):
			// Already removed by an ancestor's cascade.
		default:
			return purged, rescued, err
		}
	}
	return purged, rescued, nil
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
