package preview

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// DoneJobRetention is how long a finished job row is kept before the sweep
// deletes it. Failed rows are never purged: admin stats and metrics read them.
const DoneJobRetention = 7 * 24 * time.Hour

const doneJobPurgeBatch = 5000

// PurgeDoneJobs deletes 'done' jobs created before the cutoff, in batches, and
// returns how many went. It stops at the first error.
func PurgeDoneJobs(ctx context.Context, q *store.Queries, before time.Time) (int64, error) {
	var total int64
	for {
		n, err := q.DeleteDoneJobsBefore(ctx, store.DeleteDoneJobsBeforeParams{
			Before: pgtype.Timestamptz{Time: before, Valid: true},
			Lim:    doneJobPurgeBatch,
		})
		if err != nil {
			return total, err
		}
		total += n
		if n < doneJobPurgeBatch {
			return total, nil
		}
	}
}
