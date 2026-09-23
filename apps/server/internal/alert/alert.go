// Package alert watches the security log for patterns worth waking someone
// for, and raises them: a security_alert row, a Prometheus counter and an
// email to the instance's admins.
//
// It lives apart from internal/audit so that audit stays a leaf the request
// path can import freely; this package needs settings, mail and metrics too.
//
// One rule ships: a burst of failed sign-ins from one address. Failed
// sign-ins mostly carry no user — the subject is the error code — so a
// per-account rule would have nothing to key on.
package alert

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/audit"
	"github.com/nhatbui/pp-hub/apps/server/internal/notify"
	"github.com/nhatbui/pp-hub/apps/server/internal/obs"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
	"github.com/nhatbui/pp-hub/apps/server/internal/sysconfig"
)

// RuleFailedLoginBurst is the subject of an alert raised for a run of failed
// sign-ins from one address.
const RuleFailedLoginBurst = "login_failed_burst"

// scanInterval bounds how late an alert can be.
const scanInterval = time.Minute

type Watcher struct {
	q       *store.Queries
	audit   *audit.Service
	sys     *sysconfig.Service
	notify  *notify.Notifier
	metrics *obs.AlertMetrics
	now     func() time.Time
}

func NewWatcher(q *store.Queries, auditSvc *audit.Service, sys *sysconfig.Service, notifier *notify.Notifier, metrics *obs.AlertMetrics) *Watcher {
	return &Watcher{q: q, audit: auditSvc, sys: sys, notify: notifier, metrics: metrics, now: time.Now}
}

// Run scans every scanInterval until ctx is done.
func (w *Watcher) Run(ctx context.Context) {
	t := time.NewTicker(scanInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if _, err := w.Scan(ctx); err != nil {
				slog.WarnContext(ctx, "security alert scan failed", "err", err)
			}
		}
	}
}

// Scan raises an alert for every address over the failed sign-in threshold
// within the window that has not already been alerted on within it, and
// returns how many it raised. An address under sustained attack therefore
// alerts once per window, not once per scan.
//
// Across replicas the check-then-record is not atomic, so two replicas
// scanning the same minute can both raise the same alert. A duplicate email
// is the cheaper failure than a missed one.
func (w *Watcher) Scan(ctx context.Context) (int, error) {
	threshold := w.sys.FailedLoginAlertThreshold(sysconfig.DefaultTenantID)
	if threshold == 0 {
		return 0, nil
	}
	window := w.sys.FailedLoginAlertWindow(sysconfig.DefaultTenantID)
	since := pgtype.Timestamptz{Time: w.now().Add(-window), Valid: true}

	bursts, err := w.q.FailedLoginBursts(ctx, store.FailedLoginBurstsParams{Since: since, Threshold: int64(threshold)})
	if err != nil || len(bursts) == 0 {
		return 0, err
	}
	alerted, err := w.q.RecentSecurityAlertIPs(ctx, store.RecentSecurityAlertIPsParams{Rule: RuleFailedLoginBurst, Since: since})
	if err != nil {
		return 0, err
	}
	done := make(map[string]struct{}, len(alerted))
	for _, ip := range alerted {
		done[ip] = struct{}{}
	}

	raised := 0
	for _, b := range bursts {
		if _, ok := done[b.Ip]; ok {
			continue
		}
		w.audit.RecordSystem(ctx, audit.Event{
			Kind:    audit.KindSecurityAlert,
			Subject: RuleFailedLoginBurst,
			Detail: map[string]any{
				"rule":           RuleFailedLoginBurst,
				"attempts":       b.Attempts,
				"window_minutes": int(window / time.Minute),
				"first_at":       b.FirstAt.Time.UTC().Format(time.RFC3339),
				"last_at":        b.LastAt.Time.UTC().Format(time.RFC3339),
				"codes":          b.Codes,
			},
		}, b.Ip)
		w.metrics.Raised(RuleFailedLoginBurst)
		w.notify.SecurityAlert(ctx, notify.SecurityAlert{
			IP:       b.Ip,
			Attempts: b.Attempts,
			Window:   window,
			FirstAt:  b.FirstAt.Time,
			LastAt:   b.LastAt.Time,
			Codes:    b.Codes,
		})
		slog.WarnContext(ctx, "security alert raised",
			"rule", RuleFailedLoginBurst, "ip", b.Ip, "attempts", b.Attempts)
		raised++
	}
	return raised, nil
}
