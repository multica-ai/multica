package evals

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// inboxTypeEvalDrift is the inbox_item.type for an eval drift alert. A string,
// not a server enum; the inbox renders unknown types from title/body.
const inboxTypeEvalDrift = "eval_drift"

// driftInterval is the daily cadence for the drift alarm.
const driftInterval = 24 * time.Hour

// DriftSweeper alerts workspace owners/admins when an active eval drifts: its
// newest run failed, or its newest target version's pass-rate regressed against
// the previous version. Warn-only — it never blocks anything. Modeled on
// driftwatch/sweeper.go. Gated behind CEREBRO_EVAL_DRIFT_ENABLED (default OFF).
type DriftSweeper struct {
	store      *Store
	recipients OwnerAdminLister
	inbox      InboxWriter
	bus        *events.Bus
	now        func() time.Time
}

// NewDriftSweeper builds a DriftSweeper. bus may be nil (alerts are still
// written, just not broadcast live).
func NewDriftSweeper(store *Store, recipients OwnerAdminLister, inbox InboxWriter, bus *events.Bus) *DriftSweeper {
	return &DriftSweeper{store: store, recipients: recipients, inbox: inbox, bus: bus, now: time.Now}
}

// Run blocks on ctx and ticks daily. Returns immediately when the drift feature
// is disabled, so an unset CEREBRO_EVAL_DRIFT_ENABLED costs nothing.
func (d *DriftSweeper) Run(ctx context.Context, interval time.Duration) {
	if !evalDriftEnabled() {
		return
	}
	if interval <= 0 {
		interval = driftInterval
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if !evalDriftEnabled() {
				continue
			}
			if err := d.Tick(ctx); err != nil && !errors.Is(err, context.Canceled) {
				slog.Warn("cerebro eval drift sweep failed", "error", err)
			}
		}
	}
}

// Tick runs one pass over every active eval, alerting on any that drifted.
func (d *DriftSweeper) Tick(ctx context.Context) error {
	evals, err := d.store.ListActiveEvals(ctx)
	if err != nil {
		return fmt.Errorf("list active evals: %w", err)
	}
	for _, eval := range evals {
		reason, drifted, err := d.assess(ctx, eval)
		if err != nil {
			slog.Warn("cerebro eval drift: assess failed", "eval_id", eval.ID.String(), "error", err)
			continue
		}
		if !drifted {
			continue
		}
		recipients, err := d.recipients.ListCerebroWorkspaceOwnerAdmins(ctx, pgUUID(eval.WorkspaceID))
		if err != nil {
			slog.Warn("cerebro eval drift: list owner/admins failed", "workspace_id", eval.WorkspaceID.String(), "error", err)
			continue
		}
		if len(recipients) == 0 {
			continue
		}
		d.alert(ctx, eval, reason, recipients)
	}
	return nil
}

// assess decides whether an eval drifted and returns a human-readable reason.
// Two signals, checked in order: the newest run failed/errored, or the newest
// target version's pass-rate regressed against the previous version's.
func (d *DriftSweeper) assess(ctx context.Context, eval Eval) (string, bool, error) {
	status, ok, err := d.store.LatestRunStatusForEval(ctx, eval.WorkspaceID, eval.ID)
	if err != nil {
		return "", false, err
	}
	if ok && (status == "failed" || status == "error") {
		return fmt.Sprintf("its newest run %s", status), true, nil
	}

	rates, err := d.store.PassRateByTargetVersion(ctx, eval.WorkspaceID, eval.ID)
	if err != nil {
		return "", false, err
	}
	if len(rates) >= 2 && rates[0].Rate() < rates[1].Rate() {
		return fmt.Sprintf("its pass rate dropped from %.0f%% (%s) to %.0f%% (%s)",
			rates[1].Rate()*100, versionLabel(rates[1].TargetVersion),
			rates[0].Rate()*100, versionLabel(rates[0].TargetVersion)), true, nil
	}
	return "", false, nil
}

// alert writes one inbox card per owner/admin recipient and broadcasts each.
func (d *DriftSweeper) alert(ctx context.Context, eval Eval, reason string, recipients []pgtype.UUID) {
	label := evalTitleOrKey(eval)
	title := fmt.Sprintf("Eval drift: %s", label)
	body := fmt.Sprintf("The eval %q shows drift — %s. Review its recent runs.", label, reason)
	details, _ := json.Marshal(map[string]any{
		"eval_id":    eval.ID.String(),
		"eval_key":   eval.Key,
		"eval_title": eval.Title,
		"reason":     reason,
		"kind":       "eval_drift",
	})
	for _, userID := range recipients {
		item, err := d.inbox.CreateInboxItem(ctx, db.CreateInboxItemParams{
			WorkspaceID:   pgUUID(eval.WorkspaceID),
			RecipientType: "member",
			RecipientID:   userID,
			Type:          inboxTypeEvalDrift,
			Severity:      "attention",
			IssueID:       pgtype.UUID{},
			Title:         title,
			Body:          pgtype.Text{String: body, Valid: true},
			ActorType:     pgtype.Text{String: "system", Valid: true},
			ActorID:       pgtype.UUID{},
			Details:       details,
			Route:         "inbox",
		})
		if err != nil {
			slog.Warn("cerebro eval drift: inbox write failed",
				"eval_id", eval.ID.String(), "recipient_id", userID.String(), "error", err)
			continue
		}
		publishInboxNew(d.bus, item)
	}
}

func evalTitleOrKey(eval Eval) string {
	if eval.Title != "" {
		return eval.Title
	}
	return eval.Key
}

func versionLabel(v string) string {
	if v == "" {
		return "unversioned"
	}
	return v
}
