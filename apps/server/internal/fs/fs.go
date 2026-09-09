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
)

const maxTreeDepth = 100

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

func (s *Service) Delete(ctx context.Context, nodeID pgtype.UUID) error {
	return s.q.SoftDeleteNode(ctx, nodeID)
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
