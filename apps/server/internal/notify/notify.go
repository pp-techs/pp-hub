package notify

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/nhatbui/pp-hub/apps/server/internal/mail"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
	"github.com/nhatbui/pp-hub/apps/server/internal/sysconfig"
	"github.com/nhatbui/pp-hub/apps/server/internal/users"
)

const KindEmail = "mail:send"
const maxAttempts = 5

type Recipient struct {
	ID       pgtype.UUID
	Email    string
	Settings []byte // users.settings, for locale and the opt-out
}

type Share struct {
	Granter     store.User
	Node        store.Node
	Level       store.ShareLevel
	CanDownload bool
	Group       string // group name; "" for a direct share
	// External: the recipients are outside the node's space, so the space's
	// own routes are not theirs to open; the link goes through /open/.
	External   bool
	Recipients []Recipient
}

// Invite is a share waiting for an address with no account.
type Invite struct {
	Granter     store.User
	Node        store.Node
	Level       store.ShareLevel
	CanDownload bool
	Email       string
}

type emailJob struct {
	To      string `json:"to"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

type Notifier struct {
	q           *store.Queries
	mail        *mail.Sender
	sys         *sysconfig.Service
	externalURL string
}

func New(q *store.Queries, m *mail.Sender, sys *sysconfig.Service, externalURL string) *Notifier {
	return &Notifier{
		q:           q,
		mail:        m,
		sys:         sys,
		externalURL: externalURL,
	}
}

func uuidString(u pgtype.UUID) string {
	v, _ := u.Value()
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// Shared enqueues one email per recipient. Nil-safe and never returns an
// error: the share already succeeded, so a failure here is logged, not surfaced.
func (n *Notifier) Shared(ctx context.Context, s Share) {
	if n == nil || n.mail == nil || !n.mail.Enabled() || len(s.Recipients) == 0 {
		return
	}

	space, err := n.q.GetSpace(ctx, s.Node.SpaceID)
	if err != nil {
		slog.WarnContext(ctx, "share notification skipped", "err", err)
		return
	}
	instance := n.instance()
	granterName := displayName(s.Granter)

	spaceID := uuidString(s.Node.SpaceID)
	nodeID := uuidString(s.Node.ID)

	link := n.externalURL + "/drive/" + spaceID + "/shared"
	if s.Node.Kind == store.NodeKindDir {
		link = n.externalURL + "/drive/" + spaceID + "?dir=" + nodeID
	}
	if s.External {
		link = n.externalURL + "/open/" + nodeID
	}

	for _, r := range s.Recipients {
		if r.Email == "" {
			continue
		}
		prefs := users.DecodePreferences(r.Settings)
		if !prefs.WantsShareEmail() {
			continue
		}

		subject, body := render(localeOf(prefs.Locale), msgData{
			GranterName:  granterName,
			GranterEmail: s.Granter.Email,
			NodeName:     s.Node.Name,
			IsDir:        s.Node.Kind == store.NodeKindDir,
			Group:        s.Group,
			SpaceName:    spaceLabel(space),
			Instance:     instance,
			Level:        s.Level,
			CanDownload:  s.CanDownload,
			Link:         link,
		})

		payload, err := json.Marshal(emailJob{
			To:      r.Email,
			Subject: subject,
			Body:    body,
		})
		if err != nil {
			slog.WarnContext(ctx, "failed to marshal email job", "err", err)
			continue
		}

		if _, err := n.q.EnqueueJob(ctx, store.EnqueueJobParams{Kind: KindEmail, Payload: payload}); err != nil {
			slog.WarnContext(ctx, "failed to enqueue email job", "err", err)
			continue
		}
	}
}

// Invited mails an address with no account that something was shared with
// it. Nil-safe and never returns an error, like Shared. The recipient has no
// preferences yet, so the mail is in the granter's language — the likelier
// guess for somebody they chose to invite.
func (n *Notifier) Invited(ctx context.Context, inv Invite) {
	if n == nil || n.mail == nil || !n.mail.Enabled() || inv.Email == "" {
		return
	}
	space, err := n.q.GetSpace(ctx, inv.Node.SpaceID)
	if err != nil {
		slog.WarnContext(ctx, "invitation email skipped", "err", err)
		return
	}
	subject, body := renderInvite(localeOf(users.DecodePreferences(inv.Granter.Settings).Locale), msgData{
		GranterName:  displayName(inv.Granter),
		GranterEmail: inv.Granter.Email,
		NodeName:     inv.Node.Name,
		IsDir:        inv.Node.Kind == store.NodeKindDir,
		SpaceName:    spaceLabel(space),
		Instance:     n.instance(),
		Level:        inv.Level,
		CanDownload:  inv.CanDownload,
		Link:         n.externalURL + "/open/" + uuidString(inv.Node.ID),
	})
	payload, err := json.Marshal(emailJob{To: inv.Email, Subject: subject, Body: body})
	if err != nil {
		slog.WarnContext(ctx, "failed to marshal email job", "err", err)
		return
	}
	if _, err := n.q.EnqueueJob(ctx, store.EnqueueJobParams{Kind: KindEmail, Payload: payload}); err != nil {
		slog.WarnContext(ctx, "failed to enqueue email job", "err", err)
	}
}

func (n *Notifier) instance() string {
	if n.sys != nil {
		return n.sys.InstanceName(sysconfig.DefaultTenantID)
	}
	return "pp-hub"
}

func displayName(u store.User) string {
	if u.DisplayName != "" {
		return u.DisplayName
	}
	if u.Name != "" {
		return u.Name
	}
	return u.Email
}

// spaceLabel is how a mail names where the item lives. A personal space is
// named after its owner, who is already the granter in the sentence, so it
// is left out rather than read as "in Jane Doe".
func spaceLabel(sp store.Space) string {
	if sp.Kind == "personal" {
		return ""
	}
	return sp.Name
}

// SecurityAlert is one alert as the watcher raised it.
type SecurityAlert struct {
	IP              string
	Attempts        int64
	Window          time.Duration
	FirstAt, LastAt time.Time
	Codes           []string
}

// SecurityAlert mails every live instance admin. Nil-safe and never returns
// an error, like Shared. There is no per-person opt-out: the share-email
// preference is about colleagues' activity, not the instance's security, and
// alerts are turned off instance-wide by setting the threshold to 0.
func (n *Notifier) SecurityAlert(ctx context.Context, a SecurityAlert) {
	if n == nil || n.mail == nil || !n.mail.Enabled() {
		return
	}
	rows, err := n.q.ListInstanceAdminRecipients(ctx)
	if err != nil {
		slog.WarnContext(ctx, "security alert email skipped", "err", err)
		return
	}
	instance := "pp-hub"
	if n.sys != nil {
		instance = n.sys.InstanceName(sysconfig.DefaultTenantID)
	}
	const layout = "2006-01-02 15:04:05"
	d := alertData{
		Instance:      instance,
		IP:            a.IP,
		Attempts:      a.Attempts,
		WindowMinutes: int(a.Window / time.Minute),
		FirstAt:       a.FirstAt.UTC().Format(layout),
		LastAt:        a.LastAt.UTC().Format(layout),
		Codes:         strings.Join(a.Codes, ", "),
		Link:          n.externalURL + "/admin/security",
	}
	for _, row := range rows {
		subject, body := renderAlert(localeOf(users.DecodePreferences(row.Settings).Locale), d)
		payload, err := json.Marshal(emailJob{To: row.Email, Subject: subject, Body: body})
		if err != nil {
			slog.WarnContext(ctx, "failed to marshal email job", "err", err)
			continue
		}
		if _, err := n.q.EnqueueJob(ctx, store.EnqueueJobParams{Kind: KindEmail, Payload: payload}); err != nil {
			slog.WarnContext(ctx, "failed to enqueue email job", "err", err)
		}
	}
}

func (n *Notifier) Run(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n.drain(ctx)
		}
	}
}

func (n *Notifier) drain(ctx context.Context) {
	// Do not reap. preview.Worker.reap in the same process is deliberately
	// not gated on kind and already reclaims stranded mail:send rows.
	if n == nil || n.mail == nil || !n.mail.Enabled() {
		return
	}

	jobs, err := n.q.ClaimJobs(ctx, store.ClaimJobsParams{Kinds: []string{KindEmail}, Lim: 10})
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			slog.WarnContext(ctx, "claim mail jobs", "err", err)
		}
		return
	}

	for _, job := range jobs {
		var ej emailJob
		if err := json.Unmarshal(job.Payload, &ej); err != nil {
			_ = n.q.FailJob(ctx, store.FailJobParams{
				ID:        job.ID,
				LastError: pgtype.Text{String: err.Error(), Valid: true},
			})
			continue
		}

		sendCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		err = n.mail.Send(sendCtx, mail.Message{
			To:      ej.To,
			Subject: ej.Subject,
			Body:    ej.Body,
		})
		cancel()

		if err != nil {
			slog.WarnContext(ctx, "mail job failed", "attempt", job.Attempts, "err", err)
			if job.Attempts >= maxAttempts {
				_ = n.q.FailJob(ctx, store.FailJobParams{
					ID:        job.ID,
					LastError: pgtype.Text{String: err.Error(), Valid: true},
				})
			} else {
				_ = n.q.RequeueJob(ctx, store.RequeueJobParams{
					ID:        job.ID,
					LastError: pgtype.Text{String: err.Error(), Valid: true},
				})
			}
			continue
		}

		_ = n.q.CompleteJob(ctx, job.ID)
	}
}
