// autopilot_anomaly_report scans active scheduled autopilots and writes a
// durable inbox alert when the latest run was skipped/failed or when a
// schedule has gone materially overdue without producing a new run.
//
// The command is intentionally one-shot so it can be driven by an external
// cron on godfinns-cpu. It does not depend on the autopilot workers
// themselves, which is the whole point of catching a broken dispatcher.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

const (
	autopilotAnomalyType       = "autopilot_anomaly"
	autopilotAnomalySeverity   = "attention"
	autopilotAnomalyStaleSlack = 5 * time.Minute
)

const autopilotAnomalyReportQuery = `
SELECT
  a.id::text,
  a.workspace_id::text,
  a.title,
  a.created_by_type,
  a.created_by_id::text,
  COALESCE(latest.status, '')::text,
  COALESCE(latest.failure_reason, '')::text,
  COALESCE(latest.reason_code, '')::text,
  schedule.next_run_at,
  schedule.cron_expression,
  schedule.timezone
FROM autopilot a
JOIN LATERAL (
  SELECT t.next_run_at, t.cron_expression, t.timezone
  FROM autopilot_trigger t
  WHERE t.autopilot_id = a.id
    AND t.enabled
    AND t.kind = 'schedule'
  ORDER BY t.next_run_at ASC NULLS LAST, t.created_at ASC, t.id ASC
  LIMIT 1
) schedule ON true
LEFT JOIN LATERAL (
  SELECT r.status, r.failure_reason, r.reason_code
  FROM autopilot_run r
  WHERE r.autopilot_id = a.id
  ORDER BY r.triggered_at DESC, r.created_at DESC, r.id DESC
  LIMIT 1
) latest ON true
WHERE a.status = 'active'
ORDER BY a.workspace_id ASC, a.created_at DESC, a.id ASC
`

const autopilotAnomalyInboxQuery = `
SELECT id::text, workspace_id::text, recipient_type, recipient_id::text, details
FROM inbox_item
WHERE archived = false AND type = $1
`

const autopilotAnomalyInboxInsert = `
INSERT INTO inbox_item (
    id, workspace_id, recipient_type, recipient_id,
    type, severity, issue_id, title, body,
    actor_type, actor_id, details
) VALUES (
    $1, $2, $3, $4,
    $5, $6, $7, $8, $9,
    $10, $11, $12::jsonb
)
`

const autopilotAnomalyInboxArchive = `
UPDATE inbox_item SET archived = true
WHERE id = $1
`

type autopilotAnomalyCandidate struct {
	ID             string
	WorkspaceID    string
	Title          string
	CreatedByType  string
	CreatedByID    string
	LastRunStatus  string
	LastReason     string
	LastReasonCode string
	NextRunAt      pgtype.Timestamptz
	CronExpression pgtype.Text
	Timezone       pgtype.Text
}

type autopilotAnomalyAlert struct {
	ID            string
	WorkspaceID   string
	RecipientType string
	RecipientID   string
	AutopilotID   string
	AnomalyKind   string
}

type inboxAlertRow struct {
	ID            string
	WorkspaceID   string
	RecipientType string
	RecipientID   string
	Details       []byte
}

type recipientRef struct {
	Type string
	ID   string
}

type alertEvaluation struct {
	Kind    string
	Title   string
	Body    string
	Details []byte
}

func main() {
	logger.Init()
	if err := run(); err != nil {
		slog.Error("autopilot anomaly report failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}

	currentAlerts, err := loadOpenAlerts(ctx, pool)
	if err != nil {
		return err
	}

	candidates, err := loadCandidates(ctx, pool)
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	seen := make(map[string]bool, len(currentAlerts))
	createdCount := 0
	reportedCount := 0

	for _, candidate := range candidates {
		alert, err := evaluateCandidate(candidate, now)
		if err != nil {
			slog.Warn("autopilot anomaly report: evaluate candidate failed",
				"autopilot_id", candidate.ID,
				"workspace_id", candidate.WorkspaceID,
				"error", err,
			)
			continue
		}
		if alert == nil {
			continue
		}

		recipients := resolveRecipients(ctx, pool, candidate)
		for _, recipient := range recipients {
			key := alertKey(candidate.WorkspaceID, recipient.Type, recipient.ID, candidate.ID, alert.Kind)
			seen[key] = true

			created, err := syncAlert(ctx, pool, candidate, recipient, alert, currentAlerts, key)
			if err != nil {
				slog.Warn("autopilot anomaly report: sync alert failed",
					"autopilot_id", candidate.ID,
					"workspace_id", candidate.WorkspaceID,
					"recipient_type", recipient.Type,
					"recipient_id", recipient.ID,
					"error", err,
				)
				continue
			}
			if created {
				createdCount++
			}
			reportedCount++
			fmt.Println(alert.summaryLine(candidate, recipient))
		}
	}

	archivedCount := 0
	for key, row := range currentAlerts {
		if seen[key] {
			continue
		}
		if err := archiveAlert(ctx, pool, row.ID); err != nil {
			slog.Warn("autopilot anomaly report: archive stale alert failed",
				"alert_id", row.ID,
				"error", err,
			)
			continue
		}
		archivedCount++
	}

	if reportedCount > 0 || archivedCount > 0 {
		fmt.Printf("autopilot anomaly report: created=%d archived=%d active=%d\n", createdCount, archivedCount, reportedCount)
	}
	return nil
}

func loadCandidates(ctx context.Context, pool *pgxpool.Pool) ([]autopilotAnomalyCandidate, error) {
	rows, err := pool.Query(ctx, autopilotAnomalyReportQuery)
	if err != nil {
		return nil, fmt.Errorf("query autopilot anomalies: %w", err)
	}
	defer rows.Close()

	var out []autopilotAnomalyCandidate
	for rows.Next() {
		var row autopilotAnomalyCandidate
		if err := rows.Scan(
			&row.ID,
			&row.WorkspaceID,
			&row.Title,
			&row.CreatedByType,
			&row.CreatedByID,
			&row.LastRunStatus,
			&row.LastReason,
			&row.LastReasonCode,
			&row.NextRunAt,
			&row.CronExpression,
			&row.Timezone,
		); err != nil {
			return nil, fmt.Errorf("scan autopilot anomaly candidate: %w", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate autopilot anomaly candidates: %w", err)
	}
	return out, nil
}

func loadOpenAlerts(ctx context.Context, pool *pgxpool.Pool) (map[string]autopilotAnomalyAlert, error) {
	rows, err := pool.Query(ctx, autopilotAnomalyInboxQuery, autopilotAnomalyType)
	if err != nil {
		return nil, fmt.Errorf("query open autopilot anomaly alerts: %w", err)
	}
	defer rows.Close()

	out := make(map[string]autopilotAnomalyAlert)
	for rows.Next() {
		var row inboxAlertRow
		if err := rows.Scan(&row.ID, &row.WorkspaceID, &row.RecipientType, &row.RecipientID, &row.Details); err != nil {
			return nil, fmt.Errorf("scan open autopilot anomaly alert: %w", err)
		}
		alert, ok := decodeAlert(row)
		if !ok {
			continue
		}
		out[alertKey(alert.WorkspaceID, alert.RecipientType, alert.RecipientID, alert.AutopilotID, alert.AnomalyKind)] = alert
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate open autopilot anomaly alerts: %w", err)
	}
	return out, nil
}

func decodeAlert(row inboxAlertRow) (autopilotAnomalyAlert, bool) {
	type alertDetails struct {
		AutopilotID string `json:"autopilot_id"`
		AnomalyKind string `json:"anomaly_kind"`
	}
	var parsed alertDetails
	if err := json.Unmarshal(row.Details, &parsed); err != nil {
		return autopilotAnomalyAlert{}, false
	}
	if parsed.AutopilotID == "" || parsed.AnomalyKind == "" {
		return autopilotAnomalyAlert{}, false
	}
	return autopilotAnomalyAlert{
		ID:            row.ID,
		WorkspaceID:   row.WorkspaceID,
		RecipientType: row.RecipientType,
		RecipientID:   row.RecipientID,
		AutopilotID:   parsed.AutopilotID,
		AnomalyKind:   parsed.AnomalyKind,
	}, true
}

func evaluateCandidate(c autopilotAnomalyCandidate, now time.Time) (*alertEvaluation, error) {
	status := strings.TrimSpace(c.LastRunStatus)
	switch status {
	case "skipped", "failed":
		return newRunAnomalyEvaluation(c, status), nil
	case "issue_created", "running", "completed", "cancelled":
		return nil, nil
	case "":
		// Keep going to the stale-schedule fallback below.
	default:
		// Unknown statuses should not crash the report. Surface them so the
		// operator sees that the status vocabulary drifted.
		return newRunAnomalyEvaluation(c, "unknown_status"), nil
	}

	if !c.NextRunAt.Valid || !c.CronExpression.Valid || !c.Timezone.Valid {
		return nil, nil
	}

	interval, err := scheduleInterval(c.CronExpression.String, c.Timezone.String, c.NextRunAt.Time.UTC())
	if err != nil {
		return nil, err
	}
	threshold := c.NextRunAt.Time.UTC().Add(interval + autopilotAnomalyStaleSlack)
	if now.After(threshold) {
		return newStaleAnomalyEvaluation(c, interval), nil
	}
	return nil, nil
}

func newRunAnomalyEvaluation(c autopilotAnomalyCandidate, status string) *alertEvaluation {
	title := fmt.Sprintf("Autopilot alert: %s", c.Title)
	reason := strings.TrimSpace(c.LastReason)
	if reason == "" {
		reason = strings.TrimSpace(c.LastReasonCode)
	}
	if reason == "" {
		reason = status
	}

	body := fmt.Sprintf(
		"The latest run for this active autopilot is %s, so the schedule can look healthy while nothing useful is being produced. Check the most recent run and fix the underlying dispatch problem.",
		status,
	)
	if reason != "" {
		body = fmt.Sprintf("%s Last reason: %s.", body, reason)
	}
	details, _ := json.Marshal(map[string]any{
		"autopilot_id":    c.ID,
		"autopilot_title": c.Title,
		"anomaly_kind":    status,
		"last_run_status": status,
		"last_reason":     reason,
		"next_run_at":     timestampOrEmpty(c.NextRunAt),
		"cron_expression": textOrEmpty(c.CronExpression),
		"timezone":        textOrEmpty(c.Timezone),
	})
	return &alertEvaluation{
		Kind:    status,
		Title:   title,
		Body:    body,
		Details: details,
	}
}

func newStaleAnomalyEvaluation(c autopilotAnomalyCandidate, interval time.Duration) *alertEvaluation {
	title := fmt.Sprintf("Autopilot alert: %s", c.Title)
	body := fmt.Sprintf(
		"This active autopilot has not produced a run for longer than one cron interval. The next scheduled fire was %s in %s and the cadence is about %s.",
		timestampOrEmpty(c.NextRunAt), textOrEmpty(c.Timezone), interval.Round(time.Minute),
	)
	details, _ := json.Marshal(map[string]any{
		"autopilot_id":              c.ID,
		"autopilot_title":           c.Title,
		"anomaly_kind":              "stale",
		"last_run_status":           "",
		"next_run_at":               timestampOrEmpty(c.NextRunAt),
		"cron_expression":           textOrEmpty(c.CronExpression),
		"timezone":                  textOrEmpty(c.Timezone),
		"expected_interval_seconds": int64(interval.Seconds()),
	})
	return &alertEvaluation{
		Kind:    "stale",
		Title:   title,
		Body:    body,
		Details: details,
	}
}

func syncAlert(
	ctx context.Context,
	pool *pgxpool.Pool,
	candidate autopilotAnomalyCandidate,
	recipient recipientRef,
	alert *alertEvaluation,
	current map[string]autopilotAnomalyAlert,
	key string,
) (bool, error) {
	if _, ok := current[key]; ok {
		return false, nil
	}

	if len(alert.Details) == 0 {
		alert.Details = []byte("{}")
	}

	_, err := pool.Exec(ctx, autopilotAnomalyInboxInsert,
		dbid.NewV7(),
		util.MustParseUUID(candidate.WorkspaceID),
		recipient.Type,
		util.MustParseUUID(recipient.ID),
		autopilotAnomalyType,
		autopilotAnomalySeverity,
		pgtype.UUID{},
		alert.Title,
		alert.Body,
		"system",
		pgtype.UUID{},
		string(alert.Details),
	)
	if err != nil {
		return false, err
	}
	return true, nil
}

func archiveAlert(ctx context.Context, pool *pgxpool.Pool, id string) error {
	_, err := pool.Exec(ctx, autopilotAnomalyInboxArchive, util.MustParseUUID(id))
	return err
}

func resolveRecipients(ctx context.Context, pool *pgxpool.Pool, c autopilotAnomalyCandidate) []recipientRef {
	switch c.CreatedByType {
	case "member":
		return []recipientRef{{Type: "member", ID: c.CreatedByID}}
	case "agent":
		var owner pgtype.UUID
		if err := pool.QueryRow(ctx, `SELECT owner_id FROM agent WHERE id = $1`, util.MustParseUUID(c.CreatedByID)).Scan(&owner); err != nil {
			return nil
		}
		if !owner.Valid {
			return nil
		}
		var memberID string
		if err := pool.QueryRow(ctx, `
SELECT user_id::text
FROM member
WHERE workspace_id = $1 AND user_id = $2
`, util.MustParseUUID(c.WorkspaceID), owner).Scan(&memberID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil
			}
			return nil
		}
		return []recipientRef{{Type: "member", ID: memberID}}
	default:
		return nil
	}
}

func scheduleInterval(cronExpr, timezone string, after time.Time) (time.Duration, error) {
	occurrences, err := service.NextOccurrencesAfterUTC(cronExpr, timezone, after, 1)
	if err != nil {
		return 0, err
	}
	if len(occurrences) == 0 {
		return 0, fmt.Errorf("no future occurrence for cron %q in timezone %q", cronExpr, timezone)
	}
	interval := occurrences[0].Sub(after.UTC())
	if interval <= 0 {
		return 0, fmt.Errorf("non-positive cron interval for %q in %q", cronExpr, timezone)
	}
	return interval, nil
}

func timestampOrEmpty(ts pgtype.Timestamptz) string {
	if !ts.Valid {
		return ""
	}
	return ts.Time.UTC().Format(time.RFC3339)
}

func textOrEmpty(v pgtype.Text) string {
	if !v.Valid {
		return ""
	}
	return v.String
}

func alertKey(workspaceID, recipientType, recipientID, autopilotID, anomalyKind string) string {
	return strings.Join([]string{workspaceID, recipientType, recipientID, autopilotID, anomalyKind}, ":")
}

func (a *alertEvaluation) summaryLine(candidate autopilotAnomalyCandidate, recipient recipientRef) string {
	return fmt.Sprintf(
		"autopilot anomaly: workspace=%s autopilot=%s recipient=%s/%s kind=%s status=%s",
		candidate.WorkspaceID, candidate.ID, recipient.Type, recipient.ID, a.Kind, candidate.LastRunStatus,
	)
}
