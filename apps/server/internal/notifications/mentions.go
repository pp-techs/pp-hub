package notifications

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/fs"
	"github.com/nhatbui/pp-hub/apps/server/internal/mdoc"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// KindMentions is the job that notifies the people a new markdown version
// mentions and its previous version did not. It runs in cmd/server, next to
// the streams it delivers to.
const KindMentions = "notify:mentions"

const (
	mentionMaxAttempts = 5
	mentionPoll        = 2 * time.Second
	// maxMentionDocBytes matches the largest document the editor opens: a
	// bigger file was not written through it, and parsing it twice per
	// version is not worth a notification.
	maxMentionDocBytes = 16 << 20
	userMentionPrefix  = "user:"
)

// mentionJob is the notify:mentions payload. The field is not
// node_version_id on purpose: preview backfill treats any pending job naming a
// version that way as that version's render.
type mentionJob struct {
	VersionID string `json:"version_id"`
}

// EnqueueMentions schedules the mention diff for a new version. Only markdown
// holds mentions; any other version is ignored.
func (s *Service) EnqueueMentions(ctx context.Context, ver store.NodeVersion) error {
	if !fs.IsMarkdown(ver.Mime) {
		return nil
	}
	payload, err := json.Marshal(mentionJob{VersionID: uuidString(ver.ID)})
	if err != nil {
		return err
	}
	_, err = s.q.EnqueueJob(ctx, store.EnqueueJobParams{Kind: KindMentions, Payload: payload})
	return err
}

// Run claims and processes mention jobs until ctx ends. Stranded jobs are
// returned to the queue by the preview worker's reaper, which is not gated on
// kind.
func (s *Service) Run(ctx context.Context) {
	ticker := time.NewTicker(mentionPoll)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.drainMentions(ctx)
		}
	}
}

func (s *Service) drainMentions(ctx context.Context) {
	jobs, err := s.q.ClaimJobs(ctx, store.ClaimJobsParams{Kinds: []string{KindMentions}, Lim: 10})
	if err != nil {
		if !isNoRows(err) {
			s.log.WarnContext(ctx, "claim mention jobs", "err", err)
		}
		return
	}
	for _, job := range jobs {
		err := s.processMentionJob(ctx, job.Payload)
		if err == nil {
			_ = s.q.CompleteJob(ctx, job.ID)
			continue
		}
		s.log.WarnContext(ctx, "mention job failed", "attempt", job.Attempts, "err", err)
		msg := pgtype.Text{String: err.Error(), Valid: true}
		if job.Attempts >= mentionMaxAttempts {
			_ = s.q.FailJob(ctx, store.FailJobParams{ID: job.ID, LastError: msg})
		} else {
			_ = s.q.RequeueJob(ctx, store.RequeueJobParams{ID: job.ID, LastError: msg})
		}
	}
}

func (s *Service) processMentionJob(ctx context.Context, payload []byte) error {
	var p mentionJob
	if err := json.Unmarshal(payload, &p); err != nil {
		return fmt.Errorf("bad payload: %w", err)
	}
	var id pgtype.UUID
	if err := id.Scan(p.VersionID); err != nil {
		return fmt.Errorf("bad version id: %w", err)
	}
	return s.NotifyVersionMentions(ctx, id)
}

// NotifyVersionMentions tells everyone a markdown version mentions (as
// `user:<id>`) that the version before it did not — once per person and
// version, and only those who can open the document, never its writer. A
// version or document that no longer exists has nobody left to tell.
func (s *Service) NotifyVersionMentions(ctx context.Context, versionID pgtype.UUID) error {
	ver, err := s.q.GetVersion(ctx, versionID)
	if isNoRows(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !fs.IsMarkdown(ver.Mime) {
		return nil
	}
	node, err := s.q.GetNode(ctx, ver.NodeID)
	if isNoRows(err) {
		return nil
	}
	if err != nil {
		return err
	}
	doc, ok, err := s.parseVersion(ctx, ver)
	if err != nil || !ok {
		return err
	}
	before := map[string]bool{}
	prev, err := s.q.GetPreviousVersion(ctx, store.GetPreviousVersionParams{NodeID: ver.NodeID, VersionNo: ver.VersionNo})
	switch {
	case isNoRows(err):
	case err != nil:
		return err
	case fs.IsMarkdown(prev.Mime):
		prevDoc, ok, err := s.parseVersion(ctx, prev)
		if err != nil {
			return err
		}
		if !ok {
			// The previous version is too large to diff against: treating
			// it as empty would notify everybody it already mentioned.
			return nil
		}
		for _, m := range mdoc.Mentions(prevDoc) {
			before[m] = true
		}
	}

	var added []pgtype.UUID
	for _, m := range mdoc.Mentions(doc) {
		if before[m] || !strings.HasPrefix(m, userMentionPrefix) {
			continue
		}
		var uid pgtype.UUID
		if uid.Scan(strings.TrimPrefix(m, userMentionPrefix)) != nil || uid == ver.CreatedBy {
			continue
		}
		added = append(added, uid)
	}
	recipients, err := s.Readers(ctx, node, added)
	if err != nil || len(recipients) == 0 {
		return err
	}

	excerpts := mentionExcerpts(doc)
	var ids []pgtype.UUID
	err = s.q.InTx(ctx, func(q *store.Queries) error {
		for _, uid := range recipients {
			rows, err := q.CreateVersionMentionNotification(ctx, store.CreateVersionMentionNotificationParams{
				UserID:    uid,
				ActorID:   ver.CreatedBy,
				NodeID:    node.ID,
				VersionID: ver.ID,
				Excerpt:   excerpts[userMentionPrefix+uuidString(uid)],
			})
			if err != nil {
				return err
			}
			ids = append(ids, rows...)
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.Deliver(ctx, ids)
	return nil
}

// parseVersion reads and parses a markdown version. ok is false for one too
// large to consider.
func (s *Service) parseVersion(ctx context.Context, ver store.NodeVersion) (mdoc.Node, bool, error) {
	if ver.Size > maxMentionDocBytes {
		return mdoc.Node{}, false, nil
	}
	rc, err := s.blobs.Get(ctx, ver.BlobHash)
	if err != nil {
		return mdoc.Node{}, false, fmt.Errorf("open version: %w", err)
	}
	defer rc.Close()
	src, err := io.ReadAll(io.LimitReader(rc, maxMentionDocBytes+1))
	if err != nil {
		return mdoc.Node{}, false, fmt.Errorf("read version: %w", err)
	}
	if len(src) > maxMentionDocBytes {
		return mdoc.Node{}, false, nil
	}
	parsed, err := mdoc.Parse(src)
	if err != nil {
		return mdoc.Node{}, false, fmt.Errorf("parse version: %w", err)
	}
	return parsed.Doc, true, nil
}

// mentionExcerpts maps each mention id to the text of the first block that
// mentions it, a mention reading as "@label".
func mentionExcerpts(doc mdoc.Node) map[string]string {
	out := map[string]string{}
	var walk func(n mdoc.Node)
	walk = func(n mdoc.Node) {
		if !hasInline(n) {
			for _, c := range n.Content {
				walk(c)
			}
			return
		}
		var text bytes.Buffer
		var ids []string
		for _, c := range n.Content {
			switch c.Type {
			case "text":
				text.WriteString(c.Text)
			case "mention":
				id, _ := c.Attrs["id"].(string)
				label, _ := c.Attrs["label"].(string)
				text.WriteString("@" + label)
				ids = append(ids, id)
			case "hardBreak":
				text.WriteByte(' ')
			}
		}
		excerpt := Excerpt(text.String())
		for _, id := range ids {
			if _, ok := out[id]; !ok {
				out[id] = excerpt
			}
		}
	}
	walk(doc)
	return out
}

// hasInline reports whether n is a textblock: a node holding text or mentions.
func hasInline(n mdoc.Node) bool {
	for _, c := range n.Content {
		if c.Type == "text" || c.Type == "mention" {
			return true
		}
	}
	return false
}
