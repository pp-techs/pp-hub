package fs

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/blob"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// OpenVersion returns one of nodeID's versions and a reader for its bytes. A
// version of another node is ErrNotFound: the node is what was authorized.
func (s *Service) OpenVersion(ctx context.Context, nodeID, versionID pgtype.UUID) (store.NodeVersion, io.ReadCloser, error) {
	ver, err := s.q.GetVersion(ctx, versionID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && ver.NodeID != nodeID) {
		return store.NodeVersion{}, nil, ErrNotFound
	}
	if err != nil {
		return store.NodeVersion{}, nil, err
	}
	rc, err := s.blobs.Get(ctx, ver.BlobHash)
	if err != nil {
		if errors.Is(err, blob.ErrNotExist) {
			return store.NodeVersion{}, nil, fmt.Errorf("blob %s missing on disk", ver.BlobHash[:12])
		}
		return store.NodeVersion{}, nil, err
	}
	return ver, rc, nil
}
