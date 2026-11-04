package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// The governance log (spec 4.5). Every action that changes how the company is
// governed writes one row. Writers pass their own query handle, so a row written
// inside a transaction commits or rolls back with the action it records.

// Governance event kinds. The log accepts only these.
const (
	GovernanceKindPolicyAutoApprove = "policy_auto_approve"
	GovernanceKindPolicyCreated     = "policy_created"
	GovernanceKindPolicyRevoked     = "policy_revoked"
	GovernanceKindBudgetSet         = "budget_set"
	GovernanceKindBudgetExtended    = "budget_extended"
	GovernanceKindBudgetHold        = "budget_hold"
	GovernanceKindBudgetRelease     = "budget_release"
	GovernanceKindAutonomyChanged   = "autonomy_changed"
	GovernanceKindOrgChangeApplied  = "org_change_applied"
	GovernanceKindOrgChangeReverted = "org_change_reverted"
	GovernanceKindAutoPublish       = "auto_publish"
	GovernanceKindInitiativeForced  = "initiative_forced"
	GovernanceKindCompanyPaused     = "company_paused"
	GovernanceKindCompanyResumed    = "company_resumed"
	GovernanceKindGoalActivated     = "goal_activated"
	GovernanceKindGoalImported      = "goal_imported"
	GovernanceKindReviewPublished   = "review_published"
)

// Actor types the log records. A policy acts as policy:<id>, a system writer
// (a sweep, a job) has no actor id.
const (
	GovernanceActorMember = "member"
	GovernanceActorAgent  = "agent"
	GovernanceActorSystem = "system"
	GovernanceActorPolicy = "policy"
)

// GovernanceSummaryMax is the longest summary the log stores, in characters.
const GovernanceSummaryMax = 500

// governanceKinds is the set of kinds the log accepts.
var governanceKinds = map[string]bool{
	GovernanceKindPolicyAutoApprove: true,
	GovernanceKindPolicyCreated:     true,
	GovernanceKindPolicyRevoked:     true,
	GovernanceKindBudgetSet:         true,
	GovernanceKindBudgetExtended:    true,
	GovernanceKindBudgetHold:        true,
	GovernanceKindBudgetRelease:     true,
	GovernanceKindAutonomyChanged:   true,
	GovernanceKindOrgChangeApplied:  true,
	GovernanceKindOrgChangeReverted: true,
	GovernanceKindAutoPublish:       true,
	GovernanceKindInitiativeForced:  true,
	GovernanceKindCompanyPaused:     true,
	GovernanceKindCompanyResumed:    true,
	GovernanceKindGoalActivated:     true,
	GovernanceKindGoalImported:      true,
	GovernanceKindReviewPublished:   true,
}

// GovernanceKinds lists every kind the log accepts, for filters and the CLI.
func GovernanceKinds() []string {
	out := make([]string, 0, len(governanceKinds))
	for _, k := range []string{
		GovernanceKindPolicyAutoApprove, GovernanceKindPolicyCreated, GovernanceKindPolicyRevoked,
		GovernanceKindBudgetSet, GovernanceKindBudgetExtended, GovernanceKindBudgetHold, GovernanceKindBudgetRelease,
		GovernanceKindAutonomyChanged, GovernanceKindOrgChangeApplied, GovernanceKindOrgChangeReverted,
		GovernanceKindAutoPublish, GovernanceKindInitiativeForced, GovernanceKindCompanyPaused,
		GovernanceKindCompanyResumed, GovernanceKindGoalActivated, GovernanceKindReviewPublished,
	} {
		if governanceKinds[k] {
			out = append(out, k)
		}
	}
	return out
}

// ErrGovernanceKindUnknown: the kind is not one the log accepts.
var ErrGovernanceKindUnknown = errors.New("unknown governance event kind")

// ErrGovernanceEventInvalid: a field the log needs is missing or unknown.
var ErrGovernanceEventInvalid = errors.New("invalid governance event")

// GovernanceEvent is one entry to record.
type GovernanceEvent struct {
	WorkspaceID pgtype.UUID
	Kind        string
	ActorType   string
	ActorID     pgtype.UUID
	SubjectType string
	SubjectID   pgtype.UUID
	// Summary is one sentence for the reader. A summary longer than
	// GovernanceSummaryMax is cut, not refused: the entry belongs to an action
	// that is already happening, and a refused entry would block the action.
	Summary string
	Details map[string]any
}

// GovernanceLog writes and reads the governance log. It holds no state, so any
// service can construct one.
type GovernanceLog struct{}

// NewGovernanceLog returns the governance log writer and reader.
func NewGovernanceLog() *GovernanceLog { return &GovernanceLog{} }

// Record appends one entry through q, which may be bound to a transaction.
func (GovernanceLog) Record(ctx context.Context, q *db.Queries, ev GovernanceEvent) error {
	if !governanceKinds[ev.Kind] {
		return fmt.Errorf("%w: %q", ErrGovernanceKindUnknown, ev.Kind)
	}
	switch ev.ActorType {
	case GovernanceActorMember, GovernanceActorAgent, GovernanceActorSystem, GovernanceActorPolicy:
	default:
		return fmt.Errorf("%w: actor type %q", ErrGovernanceEventInvalid, ev.ActorType)
	}
	if !ev.WorkspaceID.Valid || ev.SubjectType == "" {
		return fmt.Errorf("%w: workspace and subject type are required", ErrGovernanceEventInvalid)
	}
	details := ev.Details
	if details == nil {
		details = map[string]any{}
	}
	raw, err := json.Marshal(details)
	if err != nil {
		return fmt.Errorf("%w: details: %v", ErrGovernanceEventInvalid, err)
	}
	if _, err := q.InsertGovernanceEvent(ctx, db.InsertGovernanceEventParams{
		ID:          dbid.NewV7(),
		WorkspaceID: ev.WorkspaceID,
		Kind:        ev.Kind,
		ActorType:   ev.ActorType,
		ActorID:     ev.ActorID,
		SubjectType: ev.SubjectType,
		SubjectID:   ev.SubjectID,
		Summary:     TruncateGovernanceSummary(ev.Summary),
		Details:     raw,
	}); err != nil {
		return fmt.Errorf("record governance event: %w", err)
	}
	return nil
}

// TruncateGovernanceSummary trims a summary and cuts it to GovernanceSummaryMax
// characters, ending a cut summary with an ellipsis.
func TruncateGovernanceSummary(s string) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) <= GovernanceSummaryMax {
		return s
	}
	runes := []rune(s)
	return string(runes[:GovernanceSummaryMax-1]) + "…"
}

// GovernanceFilter selects entries, newest first. Empty fields do not filter.
type GovernanceFilter struct {
	Kind        string
	ActorType   string
	SubjectType string
	SubjectID   pgtype.UUID
	// Cursor is the position after the previous page; empty for the first page.
	Cursor string
	// Limit is the page size. Zero means GovernancePageDefault; above
	// GovernancePageMax it is capped.
	Limit int32
}

const (
	GovernancePageDefault int32 = 50
	GovernancePageMax     int32 = 200
)

// GovernancePage is one page of the log.
type GovernancePage struct {
	Events []db.GovernanceEvent
	// NextCursor continues the listing; empty on the last page.
	NextCursor string
}

// ErrGovernanceCursorInvalid: the cursor was not one a previous page returned.
var ErrGovernanceCursorInvalid = errors.New("cursor is not valid")

// List reads one page of the workspace's log through q.
func (GovernanceLog) List(ctx context.Context, q *db.Queries, ws pgtype.UUID, f GovernanceFilter) (GovernancePage, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = GovernancePageDefault
	}
	if limit > GovernancePageMax {
		limit = GovernancePageMax
	}
	params := db.ListGovernanceEventsParams{
		WorkspaceID: ws,
		Kind:        textOrNullGovernance(f.Kind),
		ActorType:   textOrNullGovernance(f.ActorType),
		SubjectType: textOrNullGovernance(f.SubjectType),
		SubjectID:   f.SubjectID,
		RowLimit:    limit + 1,
	}
	if f.Cursor != "" {
		at, id, err := decodeGovernanceCursor(f.Cursor)
		if err != nil {
			return GovernancePage{}, err
		}
		params.BeforeCreatedAt = pgtype.Timestamptz{Time: at, Valid: true}
		params.BeforeID = id
	}
	rows, err := q.ListGovernanceEvents(ctx, params)
	if err != nil {
		return GovernancePage{}, fmt.Errorf("list governance events: %w", err)
	}
	page := GovernancePage{}
	if int32(len(rows)) > limit {
		rows = rows[:limit]
		last := rows[len(rows)-1]
		page.NextCursor = encodeGovernanceCursor(last.CreatedAt.Time, last.ID)
	}
	page.Events = rows
	return page, nil
}

// Count reports how many entries the filter matches. The cursor and the page
// size are ignored: this is the total a CSV export would write.
func (GovernanceLog) Count(ctx context.Context, q *db.Queries, ws pgtype.UUID, f GovernanceFilter) (int64, error) {
	n, err := q.CountGovernanceEvents(ctx, db.CountGovernanceEventsParams{
		WorkspaceID: ws,
		Kind:        textOrNullGovernance(f.Kind),
		ActorType:   textOrNullGovernance(f.ActorType),
		SubjectType: textOrNullGovernance(f.SubjectType),
		SubjectID:   f.SubjectID,
	})
	if err != nil {
		return 0, fmt.Errorf("count governance events: %w", err)
	}
	return n, nil
}

func textOrNullGovernance(s string) pgtype.Text {
	return pgtype.Text{String: s, Valid: s != ""}
}

// The cursor is opaque to clients: base64url of "<RFC3339Nano created_at>|<event id>".
func encodeGovernanceCursor(at time.Time, id pgtype.UUID) string {
	raw := at.UTC().Format(time.RFC3339Nano) + "|" + util.UUIDToString(id)
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeGovernanceCursor(s string) (time.Time, pgtype.UUID, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return time.Time{}, pgtype.UUID{}, ErrGovernanceCursorInvalid
	}
	atText, idText, ok := strings.Cut(string(raw), "|")
	if !ok {
		return time.Time{}, pgtype.UUID{}, ErrGovernanceCursorInvalid
	}
	at, err := time.Parse(time.RFC3339Nano, atText)
	if err != nil {
		return time.Time{}, pgtype.UUID{}, ErrGovernanceCursorInvalid
	}
	id, err := util.ParseUUID(idText)
	if err != nil {
		return time.Time{}, pgtype.UUID{}, ErrGovernanceCursorInvalid
	}
	return at, id, nil
}

// autoPublishGovernance is the GovernanceRecorder of the knowledge publish
// policy: each automatic publish becomes an auto_publish entry.
type autoPublishGovernance struct {
	q   *db.Queries
	log *GovernanceLog
}

// NewAutoPublishGovernance returns the recorder the knowledge service uses when
// none is wired. It writes through q.
func NewAutoPublishGovernance(q *db.Queries) GovernanceRecorder {
	return autoPublishGovernance{q: q, log: NewGovernanceLog()}
}

// RecordAutoPublish implements GovernanceRecorder.
func (g autoPublishGovernance) RecordAutoPublish(ctx context.Context, ev AutoPublishEvent) error {
	actorType := ev.ActorType
	if actorType == "" {
		actorType = GovernanceActorPolicy
	}
	return g.log.Record(ctx, g.q, GovernanceEvent{
		WorkspaceID: ev.WorkspaceID,
		Kind:        GovernanceKindAutoPublish,
		ActorType:   actorType,
		SubjectType: "knowledge_link",
		SubjectID:   ev.LinkID,
		Summary: fmt.Sprintf("Published a %s document in %s automatically (%s).",
			ev.Kind, ev.KnowledgeBase, ev.Mode),
		Details: map[string]any{
			"knowledge_base":  ev.KnowledgeBase,
			"kind":            ev.Kind,
			"mode":            ev.Mode,
			"author_squad_id": util.UUIDToString(ev.AuthorSquadID),
			"critic_squad_id": util.UUIDToString(ev.CriticSquadID),
		},
	})
}
