package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// Metrics (spec 4.1). A metric is a named series; a reading is one observed value
// of it at a scope. Built-in readings are computed nightly (metrics_builtin.go),
// manual readings are reported by a member or an agent, and webhook readings come
// from outside the product. All three land in metric_reading.

// Metric sources, as the metric table's CHECK allows them.
const (
	MetricSourceBuiltin = "builtin"
	MetricSourceManual  = "manual"
	MetricSourceWebhook = "webhook"
)

// Reading actor types, as the metric_reading CHECK allows them.
const (
	ReadingActorSystem  = "system"
	ReadingActorMember  = "member"
	ReadingActorAgent   = "agent"
	ReadingActorWebhook = "webhook"
)

// Scope types a metric or a goal may be attached to.
const (
	ScopeCompany    = "company"
	ScopeFloor      = "floor"
	ScopeSquad      = "squad"
	ScopeInitiative = "initiative"
)

// MetricRetentionDays is how long a reading is kept as written. Older readings
// are reduced to one per series per day (spec 4.9).
const MetricRetentionDays = 90

// metricReadingNote is the longest note a reading keeps, in characters.
const metricReadingNote = 500

// metricReadingPastWindow is how far back a reading may be dated. A reading
// cannot be dated later than a few minutes ahead, which allows for clock skew.
const (
	metricReadingPastWindow   = 365 * 24 * time.Hour
	metricReadingFutureWindow = 5 * time.Minute
)

// metricKeyPattern is the key format the metric table's CHECK enforces.
var metricKeyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_.]{0,63}$`)

var (
	// ErrMetricKeyInvalid: the key is not lower-case letters, digits, dots or underscores.
	ErrMetricKeyInvalid = errors.New("metric key must be 1 to 64 lower-case letters, digits, dots or underscores")
	// ErrMetricNotFound: no metric with that key in the workspace.
	ErrMetricNotFound = errors.New("metric not found")
	// ErrMetricBuiltinReadOnly: a built-in metric is computed, not reported.
	ErrMetricBuiltinReadOnly = errors.New("built-in metrics are computed by the company; they cannot be reported")
	// ErrMetricValueInvalid: the value is not a finite number.
	ErrMetricValueInvalid = errors.New("value must be a finite number")
	// ErrMetricScopeInvalid: the scope type or id does not name an object of this workspace.
	ErrMetricScopeInvalid = errors.New("scope does not name an object in this workspace")
	// ErrMetricObservedInvalid: observed_at is too far in the past or in the future.
	ErrMetricObservedInvalid = errors.New("observed_at is outside the range a reading may take")
	// ErrMetricNoteTooLong: the note is longer than the log keeps.
	ErrMetricNoteTooLong = errors.New("note is too long")
)

// BuiltinMetricDef is a metric the company computes. Direction says which way
// is better: a decrease is better for cost, decision time and stalled work.
type BuiltinMetricDef struct {
	Key         string
	Name        string
	Unit        string
	Direction   string
	Description string
}

// builtinMetricDefs lists the built-in metrics this build computes. The spec's
// delivery.cycle_time_days, delivery.rework_rate and knowledge metrics that need
// a source this build does not record are not listed: a series with no source
// would show invented numbers.
var builtinMetricDefs = []BuiltinMetricDef{
	{"delivery.issues_done", "Issues done", "issues", "increase", "Issues assigned to the scope that are done, counted the morning after the day they were last changed."},
	{"runs.count", "Runs finished", "runs", "increase", "Agent runs that completed or failed in the day."},
	{"runs.success_rate", "Run success rate", "%", "increase", "Completed runs as a share of finished runs in the day. No reading when no run finished."},
	{"runs.cost_usd", "Run cost", "USD", "decrease", "Provider-reported cost of the day's finished runs. Runs whose provider reports no cost add nothing."},
	{"proposals.acceptance_rate", "Proposal acceptance", "%", "increase", "Approved proposals as a share of those decided in the day. No reading when none was decided."},
	{"proposals.decision_hours", "Hours to decide a proposal", "hours", "decrease", "Mean hours from filing to decision for the proposals decided in the day."},
	{"routines.compliance_rate", "Routines on time", "%", "increase", "Required, active routines whose state is on time, as a share of those routines, now."},
	{"pipeline.initiatives_active", "Initiatives active", "initiatives", "increase", "Initiatives in the active or operate stage, now."},
	{"pipeline.stalled", "Initiatives stalled", "initiatives", "decrease", "Initiatives past their stage's SLA, now. An initiative reading is 1 when stalled and 0 when not."},
	{"pipeline.stage_days", "Days in stage", "days", "decrease", "Mean days the active initiatives have spent in their current stage, now. An initiative reading is its own days."},
	{"knowledge.docs_published", "Documents published", "documents", "increase", "Knowledge documents published in the day."},
	{"knowledge.fresh_ratio", "Fresh documents", "%", "increase", "Published documents republished within the last 90 days, as a share of all published documents, now."},
	{"knowledge.retrieval_hit_rate", "Retrieval hit rate", "%", "increase", "Hit rate of the latest knowledge retrieval evaluation run."},
	{"brief.usefulness", "Brief usefulness", "%", "increase", "Ratings of briefs that the boss marked useful, as a share of the day's ratings."},
}

// BuiltinMetricDefs returns the built-in metrics this build computes.
func BuiltinMetricDefs() []BuiltinMetricDef {
	out := make([]BuiltinMetricDef, len(builtinMetricDefs))
	copy(out, builtinMetricDefs)
	return out
}

// MetricService reads and writes metrics and readings.
type MetricService struct {
	Queries   *db.Queries
	TxStarter TxStarter
}

// NewMetricService builds the metric service over the database.
func NewMetricService(q *db.Queries, tx TxStarter) *MetricService {
	return &MetricService{Queries: q, TxStarter: tx}
}

// EnsureBuiltinMetrics seeds the built-in metric rows of a workspace. It is
// idempotent: a row already present is kept as it is.
func (s *MetricService) EnsureBuiltinMetrics(ctx context.Context, q *db.Queries, ws pgtype.UUID) error {
	for _, def := range builtinMetricDefs {
		if err := q.SeedBuiltinMetric(ctx, db.SeedBuiltinMetricParams{
			WorkspaceID: ws, Key: def.Key, Name: def.Name, Unit: def.Unit,
			Direction: def.Direction, Description: def.Description,
		}); err != nil {
			return fmt.Errorf("seed built-in metric %s: %w", def.Key, err)
		}
	}
	return nil
}

// ListMetrics returns the workspace's metrics, seeding the built-in ones first so
// they are listed before the first nightly run.
func (s *MetricService) ListMetrics(ctx context.Context, ws pgtype.UUID) ([]db.Metric, error) {
	if err := s.EnsureBuiltinMetrics(ctx, s.Queries, ws); err != nil {
		return nil, err
	}
	rows, err := s.Queries.ListMetricsInWorkspace(ctx, ws)
	if err != nil {
		return nil, fmt.Errorf("list metrics: %w", err)
	}
	return rows, nil
}

// ReadingInput is one reading to record: a manual report or a webhook delivery.
type ReadingInput struct {
	WorkspaceID pgtype.UUID
	Key         string
	// Source and the two metric fields below apply when the report creates the
	// metric. A manual report names the metric by key and gets a manual one.
	Source    string
	Name      string
	Unit      string
	Direction string
	ScopeType string
	ScopeID   pgtype.UUID
	Value     float64
	// ObservedAt zero means now.
	ObservedAt time.Time
	Note       string
	ActorType  string
	ActorID    pgtype.UUID
}

// RecordReading writes one manual or webhook reading. The metric is created by
// its first report; a built-in metric refuses reports.
func (s *MetricService) RecordReading(ctx context.Context, in ReadingInput, now time.Time) (db.MetricReading, error) {
	if !metricKeyPattern.MatchString(in.Key) {
		return db.MetricReading{}, ErrMetricKeyInvalid
	}
	if math.IsNaN(in.Value) || math.IsInf(in.Value, 0) {
		return db.MetricReading{}, ErrMetricValueInvalid
	}
	if len(in.Note) > metricReadingNote {
		return db.MetricReading{}, ErrMetricNoteTooLong
	}
	observed := in.ObservedAt
	if observed.IsZero() {
		observed = now
	}
	if observed.Before(now.Add(-metricReadingPastWindow)) || observed.After(now.Add(metricReadingFutureWindow)) {
		return db.MetricReading{}, ErrMetricObservedInvalid
	}
	if err := s.ValidateScope(ctx, s.Queries, in.WorkspaceID, in.ScopeType, in.ScopeID); err != nil {
		return db.MetricReading{}, err
	}
	metric, err := s.Queries.EnsureManualMetric(ctx, db.EnsureManualMetricParams{
		WorkspaceID: in.WorkspaceID, Key: in.Key, Name: nonEmpty(in.Name, in.Key), Unit: in.Unit,
		Source: nonEmpty(in.Source, MetricSourceManual), Direction: nonEmpty(in.Direction, "increase"),
	})
	if err != nil {
		return db.MetricReading{}, fmt.Errorf("ensure metric: %w", err)
	}
	if metric.Source == MetricSourceBuiltin {
		return db.MetricReading{}, ErrMetricBuiltinReadOnly
	}
	value, err := numericFromFloat(in.Value)
	if err != nil {
		return db.MetricReading{}, ErrMetricValueInvalid
	}
	row, err := s.Queries.InsertMetricReading(ctx, db.InsertMetricReadingParams{
		ID:          dbid.NewV7(),
		WorkspaceID: in.WorkspaceID,
		MetricKey:   in.Key,
		ScopeType:   in.ScopeType,
		ScopeID:     in.ScopeID,
		Value:       value,
		ObservedAt:  pgtype.Timestamptz{Time: observed, Valid: true},
		ActorType:   in.ActorType,
		ActorID:     in.ActorID,
		Note:        strings.TrimSpace(in.Note),
	})
	if err != nil {
		return db.MetricReading{}, fmt.Errorf("record reading: %w", err)
	}
	return row, nil
}

// ValidateScope checks that a scope names an object of the workspace: company
// takes no id, and floor, squad and initiative take an id that exists here.
func (s *MetricService) ValidateScope(ctx context.Context, q *db.Queries, ws pgtype.UUID, scopeType string, scopeID pgtype.UUID) error {
	switch scopeType {
	case ScopeCompany:
		if scopeID.Valid {
			return ErrMetricScopeInvalid
		}
		return nil
	case ScopeFloor, ScopeSquad, ScopeInitiative:
	default:
		return ErrMetricScopeInvalid
	}
	if !scopeID.Valid {
		return ErrMetricScopeInvalid
	}
	var err error
	switch scopeType {
	case ScopeFloor:
		_, err = q.GetCompanyFloorInWorkspace(ctx, db.GetCompanyFloorInWorkspaceParams{ID: scopeID, WorkspaceID: ws})
	case ScopeSquad:
		_, err = q.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{ID: scopeID, WorkspaceID: ws})
	case ScopeInitiative:
		_, err = q.GetInitiativeInWorkspace(ctx, db.GetInitiativeInWorkspaceParams{ID: scopeID, WorkspaceID: ws})
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrMetricScopeInvalid
	}
	if err != nil {
		return fmt.Errorf("check scope: %w", err)
	}
	return nil
}

// MetricSeriesParams selects one series for a read.
type MetricSeriesParams struct {
	WorkspaceID pgtype.UUID
	Key         string
	ScopeType   string
	ScopeID     pgtype.UUID
	Since       time.Time
	Until       time.Time
	Limit       int32
}

// ListReadings returns a series newest first.
func (s *MetricService) ListReadings(ctx context.Context, p MetricSeriesParams) ([]db.MetricReading, error) {
	if p.Limit <= 0 || p.Limit > 500 {
		p.Limit = 100
	}
	// An absent bound is open: the series from its first reading, to now.
	if p.Since.IsZero() {
		p.Since = time.Unix(0, 0).UTC()
	}
	if p.Until.IsZero() {
		p.Until = time.Now().Add(metricReadingFutureWindow)
	}
	rows, err := s.Queries.ListMetricReadingsForSeries(ctx, db.ListMetricReadingsForSeriesParams{
		WorkspaceID: p.WorkspaceID, MetricKey: p.Key, ScopeType: p.ScopeType, ScopeID: p.ScopeID,
		Since:    pgtype.Timestamptz{Time: p.Since, Valid: true},
		Until:    pgtype.Timestamptz{Time: p.Until, Valid: true},
		RowLimit: p.Limit,
	})
	if err != nil {
		return nil, fmt.Errorf("list readings: %w", err)
	}
	return rows, nil
}

// RetentionBatch is how many duplicate readings one retention pass removes.
const RetentionBatch = 5000

// retentionMaxPasses bounds one run of the retention job; the next night
// continues whatever remains.
const retentionMaxPasses = 40

// Downsample reduces readings older than the retention window to one per series
// per day, the newest of that day. It runs in batches and returns how many
// readings it removed.
func (s *MetricService) Downsample(ctx context.Context, now time.Time) (int64, error) {
	cutoff := pgtype.Timestamptz{Time: now.AddDate(0, 0, -MetricRetentionDays), Valid: true}
	var total int64
	for i := 0; i < retentionMaxPasses; i++ {
		n, err := s.Queries.DeleteDuplicateMetricReadingsBefore(ctx, db.DeleteDuplicateMetricReadingsBeforeParams{
			Cutoff: cutoff, RowLimit: RetentionBatch,
		})
		if err != nil {
			return total, fmt.Errorf("downsample readings: %w", err)
		}
		total += n
		if n < RetentionBatch {
			break
		}
	}
	return total, nil
}

// numericFromFloat converts a finite float to the numeric column's type.
func numericFromFloat(v float64) (pgtype.Numeric, error) {
	var n pgtype.Numeric
	if err := n.Scan(strconv.FormatFloat(v, 'f', -1, 64)); err != nil {
		return pgtype.Numeric{}, err
	}
	return n, nil
}

// floatFromNumeric reads a numeric column as a float, with ok false for NULL.
func floatFromNumeric(n pgtype.Numeric) (float64, bool) {
	if !n.Valid {
		return 0, false
	}
	f, err := n.Float64Value()
	if err != nil || !f.Valid {
		return 0, false
	}
	return f.Float64, true
}

func nonEmpty(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return strings.TrimSpace(s)
}
