package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/nhatbui/pp-hub/apps/server/internal/collab"
	"github.com/nhatbui/pp-hub/apps/server/internal/fs"
	"github.com/nhatbui/pp-hub/apps/server/internal/share"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

var (
	errAgentEditForbidden = errors.New("agent edit permission changed")
	errAgentEditLocked    = errors.New("agent edit lock changed")
)

// Recheck after blob ingestion, using the version transaction's connection.
// Node/ancestor locks stabilize the location used by the capability resolver;
// locking our editor lock prevents its replacement until the version commits.
func (s *Server) agentCommitCheck(userID pgtype.UUID, initial store.Node, token string, markdown bool) fs.CommitCheck {
	return func(ctx context.Context, q *store.Queries, current store.Node) error {
		if current.TeamRootID != initial.TeamRootID || current.CreatedBy != initial.CreatedBy {
			return fs.ErrStaleVersion
		}
		resolver := share.NewService(q)
		resolver.SetExternalSharing(s.share.ExternalSharingEnabled)
		caps, err := resolver.EffectiveCapsForNode(ctx, current, userID)
		if err != nil {
			return err
		}
		if !caps.Edit {
			return errAgentEditForbidden
		}
		lock, err := q.GetLockForUpdate(ctx, current.ID)
		if errors.Is(err, pgx.ErrNoRows) {
			if markdown {
				return nil
			}
			return errAgentEditLocked
		}
		if err != nil {
			return err
		}
		if markdown && collab.IsLockToken(lock.LockToken) || !markdown && lock.LockToken == token {
			return nil
		}
		return errAgentEditLocked
	}
}

func agentEditProblem(w http.ResponseWriter, err error) bool {
	switch {
	case errors.Is(err, errAgentEditForbidden):
		writeProblem(w, http.StatusForbidden, "Forbidden", "insufficient permission")
	case errors.Is(err, errAgentEditLocked):
		writeProblem(w, http.StatusConflict, "Conflict", "the edit lock changed; read the file again")
	default:
		return false
	}
	return true
}
