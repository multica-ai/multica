package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// Retrospectives (spec 4.7). Five triggers look at what the company already
// records and open a retrospective issue for the leader who owns the subject.
// A subject fires at most once per RetroWindow. The issue asks for a published
// postmortem or retrospective document and up to three improvement proposals.

// The trigger kinds retro_trigger accepts.
const (
	RetroTriggerInitiativeClosed = "initiative_closed"
	RetroTriggerInitiativeScaled = "initiative_scaled"
	RetroTriggerGateRejected     = "gate_rejected"
	RetroTriggerAgentFailures    = "agent_failures"
	RetroTriggerSquadRejections  = "squad_rejections"
	RetroTriggerBudgetHold       = "budget_hold"
	RetroTriggerStageSLA         = "stage_sla"
)

// Subject types retro_trigger accepts.
const (
	RetroSubjectInitiative = "initiative"
	RetroSubjectAgent      = "agent"
	RetroSubjectSquad      = "squad"
	RetroSubjectFloor      = "floor"
	RetroSubjectCompany    = "company"
)

const (
	// RetroWindow is the dedupe window: a subject fires once per window.
	RetroWindow = 14 * 24 * time.Hour
	// retroAgentFailureWindow is the span in which the same failure category
	// must repeat for an agent to be reviewed.
	retroAgentFailureWindow = 7 * 24 * time.Hour
	retroAgentFailureMin    = 3
	retroSquadRejectionRun  = 3
	retroGateRejectionMin   = 2
	retroSLABreachMin       = 2
	// retroStageHistory is how far back stage entries are read, so a visit that
	// started before the window still has its start.
	retroStageHistory = 120 * 24 * time.Hour
)

// ErrRetroNoLeader: neither the subject's leader nor the Secretary can take the
// retrospective. The trigger is not recorded, so the next run tries again.
var ErrRetroNoLeader = errors.New("no leader or secretary to take the retrospective")

// RetroCandidate is a subject that has met a trigger's condition now.
type RetroCandidate struct {
	Trigger     string
	SubjectType string
	// SubjectID is invalid for the company.
	SubjectID pgtype.UUID
	// Name names the subject in the issue title.
	Name string
	// LeaderSquadID is the squad whose leader takes the retrospective. Invalid
	// when the subject has none: the Secretary takes it then.
	LeaderSquadID pgtype.UUID
	// LeaderAgentID, when valid, takes the retrospective directly (a floor chief).
	LeaderAgentID pgtype.UUID
	// Evidence is what the trigger read, as the issue shows it and as the
	// retro_trigger row keeps it.
	Evidence []string
	// Facts are the machine-readable values of the evidence.
	Facts map[string]any
}

// RetroDueForSubject reports whether a subject may fire now: it has never fired,
// or its newest firing is at least RetroWindow old.
func RetroDueForSubject(last pgtype.Timestamptz, now time.Time) bool {
	return !last.Valid || now.Sub(last.Time) >= RetroWindow
}

// RetroRejectedInARow returns the squads whose three newest decided proposals
// are all rejections, and whose newest decision is no older than since.
func RetroRejectedInARow(rows []db.ListSquadRecentDecisionsRow, since time.Time) []pgtype.UUID {
	type run struct {
		id       pgtype.UUID
		rejected int
		total    int
		newest   time.Time
	}
	bySquad := map[string]*run{}
	order := []string{}
	for _, r := range rows {
		key := util.UUIDToString(r.SquadID)
		rr, ok := bySquad[key]
		if !ok {
			rr = &run{id: r.SquadID}
			bySquad[key] = rr
			order = append(order, key)
		}
		rr.total++
		if r.State == ProposalStateRejected {
			rr.rejected++
		}
		if rr.total == 1 && r.DecidedAt.Valid {
			rr.newest = r.DecidedAt.Time
		}
	}
	var out []pgtype.UUID
	for _, key := range order {
		rr := bySquad[key]
		if rr.total >= retroSquadRejectionRun && rr.rejected == retroSquadRejectionRun && !rr.newest.Before(since) {
			out = append(out, rr.id)
		}
	}
	return out
}

// RetroStageBreachVisits counts the stage visits of one initiative that ran past
// their SLA inside the window. entries are the initiative's stage entries,
// oldest first. A visit runs from its entry to the next entry, or to now for
// the current stage. A stage with no SLA never breaches.
func RetroStageBreachVisits(entries []RetroStageEntry, slaDays map[string]int, now, windowStart time.Time) int {
	breaches := 0
	for i, e := range entries {
		sla := slaDays[e.Stage]
		if sla <= 0 {
			continue
		}
		leave := now
		if i+1 < len(entries) {
			leave = entries[i+1].At
		}
		deadline := e.At.Add(time.Duration(sla) * 24 * time.Hour)
		if deadline.Before(leave) && leave.After(windowStart) && deadline.Before(now) {
			breaches++
		}
	}
	return breaches
}

// RetroStageEntry is one entry into a stage of an initiative.
type RetroStageEntry struct {
	Stage string
	At    time.Time
}

// RetroTriggerLabel is the words an issue uses for a trigger.
func RetroTriggerLabel(trigger string) string {
	switch trigger {
	case RetroTriggerInitiativeClosed:
		return "an initiative was closed"
	case RetroTriggerInitiativeScaled:
		return "an initiative was scaled to a follow-on"
	case RetroTriggerGateRejected:
		return "a launch gate was rejected twice"
	case RetroTriggerAgentFailures:
		return "an agent failed three times in one category in a week"
	case RetroTriggerSquadRejections:
		return "a squad's proposals were rejected three times in a row"
	case RetroTriggerBudgetHold:
		return "a budget hold occurred"
	case RetroTriggerStageSLA:
		return "a stage's SLA was breached twice"
	}
	return trigger
}

// RetroIssueBody is the retrospective issue's description. The headings are the
// template the spec names (what happened, evidence, root cause, what to change),
// and the deliverable and its commands follow them.
func RetroIssueBody(c RetroCandidate) string {
	var b strings.Builder
	fmt.Fprintf(&b, "This retrospective opened because %s: %s.\n\n", RetroTriggerLabel(c.Trigger), c.Name)
	b.WriteString("## What happened\n\nWrite what happened, in order, with dates. Name the initiative, agent, squad or scope.\n\n")
	b.WriteString("## Evidence\n\n")
	for _, line := range c.Evidence {
		b.WriteString("- " + line + "\n")
	}
	b.WriteString("\n## Root cause\n\nName the cause. Mark an inference as an inference and say what you verified.\n\n")
	b.WriteString("## What to change\n\nUp to three improvements, each an instruction change, a charter change or a runbook. ")
	b.WriteString("File each as a proposal with `multica proposal create`, with the evidence and the exact change.\n\n")
	b.WriteString("## Deliverable\n\n")
	b.WriteString("1. Search what the company already knows: `multica knowledge search \"<topic>\" --kind postmortem`.\n")
	b.WriteString("2. Publish the postmortem, or the retrospective, as a knowledge document: check its headings with `multica knowledge template postmortem`, then `multica knowledge add --kind postmortem --title \"...\" --file <file>` and publish it.\n")
	b.WriteString("3. File the improvement proposals, at most three.\n\n")
	b.WriteString("Close this issue when the document is published and the proposals are filed.\n")
	return b.String()
}

// RetroService evaluates the triggers and opens retrospective issues.
type RetroService struct {
	Queries *db.Queries
	Issues  *IssueService
	// Now is the clock; nil means time.Now. Tests pin it.
	Now func() time.Time
}

func (s *RetroService) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// EvaluateRetros runs the five triggers for every workspace and opens one
// retrospective issue per subject that fires. It returns how many fired. A
// failure in one workspace is logged and the pass goes on.
func (s *RetroService) EvaluateRetros(ctx context.Context) (int, error) {
	workspaces, err := s.Queries.ListRetroWorkspaceIDs(ctx)
	if err != nil {
		return 0, fmt.Errorf("list workspaces: %w", err)
	}
	fired := 0
	var failures []error
	for _, id := range workspaces {
		n, err := s.EvaluateWorkspace(ctx, id)
		fired += n
		if err != nil {
			slog.Warn("retro: evaluation failed for a workspace", "workspace_id", util.UUIDToString(id), "error", err)
			failures = append(failures, err)
		}
	}
	return fired, errors.Join(failures...)
}

// EvaluateWorkspace runs the five triggers for one workspace.
func (s *RetroService) EvaluateWorkspace(ctx context.Context, workspaceID pgtype.UUID) (int, error) {
	now := s.now()
	candidates, err := s.Candidates(ctx, workspaceID, now)
	if err != nil {
		return 0, err
	}
	// A close's durable request (migration 889) is evaluated first, whether or not
	// the initiative is inside the scan's window. fire dedupes against the trigger
	// log, so a request that a scan already fired fires nothing twice.
	pending, err := s.Queries.ListPendingRetroRequests(ctx, workspaceID)
	if err != nil {
		return 0, fmt.Errorf("list retro requests: %w", err)
	}
	var requested []RetroCandidate
	for _, r := range pending {
		ini, err := s.initiativeByID(ctx, workspaceID, util.UUIDToString(r.SubjectID))
		if err != nil {
			return 0, err
		}
		if !ini.ID.Valid {
			continue
		}
		requested = append(requested, closedInitiativeCandidate(ini))
	}
	fired := 0
	for _, c := range append(requested, candidates...) {
		ok, err := s.fire(ctx, workspaceID, c, now)
		if err != nil {
			return fired, err
		}
		if ok {
			fired++
		}
	}
	for _, r := range pending {
		if err := s.Queries.MarkRetroRequestProcessed(ctx, db.MarkRetroRequestProcessedParams{ID: r.ID, WorkspaceID: workspaceID}); err != nil {
			return fired, fmt.Errorf("mark retro request: %w", err)
		}
	}
	return fired, nil
}

// closedInitiativeCandidate is the initiative_closed trigger for one initiative.
func closedInitiativeCandidate(ini db.Initiative) RetroCandidate {
	return RetroCandidate{
		Trigger:       RetroTriggerInitiativeClosed,
		SubjectType:   RetroSubjectInitiative,
		SubjectID:     ini.ID,
		Name:          fmt.Sprintf("INI-%d %s", ini.Seq, ini.Title),
		LeaderSquadID: ini.OwnerSquadID,
		Evidence: []string{
			fmt.Sprintf("Initiative INI-%d closed on %s.", ini.Seq, ini.ClosedAt.Time.UTC().Format("2006-01-02")),
			"Reason given: " + ini.ClosedReason,
		},
		Facts: map[string]any{"status": ini.Status, "closed_reason": ini.ClosedReason},
	}
}

// Candidates reads the five triggers' sources and returns every subject whose
// condition holds now, before the window check. Order is stable.
func (s *RetroService) Candidates(ctx context.Context, workspaceID pgtype.UUID, now time.Time) ([]RetroCandidate, error) {
	var out []RetroCandidate
	add := func(c []RetroCandidate, err error) error {
		if err != nil {
			return err
		}
		out = append(out, c...)
		return nil
	}
	if err := add(s.initiativeClosedCandidates(ctx, workspaceID, now)); err != nil {
		return nil, err
	}
	if err := add(s.initiativeScaledCandidates(ctx, workspaceID, now)); err != nil {
		return nil, err
	}
	if err := add(s.gateRejectedCandidates(ctx, workspaceID, now)); err != nil {
		return nil, err
	}
	if err := add(s.agentFailureCandidates(ctx, workspaceID, now)); err != nil {
		return nil, err
	}
	if err := add(s.squadRejectionCandidates(ctx, workspaceID, now)); err != nil {
		return nil, err
	}
	if err := add(s.budgetHoldCandidates(ctx, workspaceID, now)); err != nil {
		return nil, err
	}
	if err := add(s.stageSLACandidates(ctx, workspaceID, now)); err != nil {
		return nil, err
	}
	return out, nil
}

// fire opens the retrospective for a candidate unless its subject fired inside
// the window. It returns false when the subject is still inside the window or no
// leader can take the issue.
func (s *RetroService) fire(ctx context.Context, workspaceID pgtype.UUID, c RetroCandidate, now time.Time) (bool, error) {
	last, err := s.Queries.LatestRetroTriggerForSubject(ctx, db.LatestRetroTriggerForSubjectParams{
		WorkspaceID: workspaceID, TriggerKind: c.Trigger, SubjectType: c.SubjectType,
		SubjectID: c.SubjectID,
	})
	var lastAt pgtype.Timestamptz
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return false, fmt.Errorf("latest retro trigger: %w", err)
	default:
		lastAt = last.FiredAt
	}
	if !RetroDueForSubject(lastAt, now) {
		return false, nil
	}

	assignee, err := s.assignee(ctx, workspaceID, c)
	if err != nil {
		return false, err
	}
	creator, err := s.creator(ctx, workspaceID)
	if err != nil {
		return false, err
	}
	res, err := s.Issues.Create(ctx, IssueCreateParams{
		WorkspaceID:  workspaceID,
		Title:        fmt.Sprintf("Retrospective: %s", c.Name),
		Description:  pgtype.Text{String: RetroIssueBody(c), Valid: true},
		Status:       "todo",
		Priority:     "none",
		AssigneeType: pgtype.Text{String: "agent", Valid: true},
		AssigneeID:   assignee,
		CreatorType:  "member",
		CreatorID:    creator,
	}, IssueCreateOpts{})
	if err != nil {
		return false, fmt.Errorf("create retrospective issue: %w", err)
	}

	evidence, err := json.Marshal(map[string]any{"lines": c.Evidence, "facts": c.Facts})
	if err != nil {
		return false, fmt.Errorf("encode retro evidence: %w", err)
	}
	if _, err := s.Queries.InsertRetroTrigger(ctx, db.InsertRetroTriggerParams{
		ID:          dbid.NewV7(),
		WorkspaceID: workspaceID,
		TriggerKind: c.Trigger,
		SubjectType: c.SubjectType,
		SubjectID:   c.SubjectID,
		IssueID:     res.Issue.ID,
		Evidence:    evidence,
		FiredAt:     pgtype.Timestamptz{Time: now, Valid: true},
	}); err != nil {
		return false, fmt.Errorf("record retro trigger: %w", err)
	}
	return true, nil
}

// assignee picks the agent that takes the retrospective: the candidate's agent,
// else its squad's leader, else the Secretary. No taker is an error.
func (s *RetroService) assignee(ctx context.Context, workspaceID pgtype.UUID, c RetroCandidate) (pgtype.UUID, error) {
	if c.LeaderAgentID.Valid {
		return c.LeaderAgentID, nil
	}
	if c.LeaderSquadID.Valid {
		sq, err := s.Queries.GetSquadLeaderForRetro(ctx, db.GetSquadLeaderForRetroParams{ID: c.LeaderSquadID, WorkspaceID: workspaceID})
		if err == nil && sq.LeaderID.Valid {
			return sq.LeaderID, nil
		}
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return pgtype.UUID{}, fmt.Errorf("load squad leader: %w", err)
		}
	}
	ws, err := s.Queries.GetWorkspace(ctx, workspaceID)
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("load workspace: %w", err)
	}
	if ws.SecretaryAgentID.Valid {
		return ws.SecretaryAgentID, nil
	}
	return pgtype.UUID{}, ErrRetroNoLeader
}

// creator is the workspace owner the issue is filed for. Retrospectives are
// opened by the system on the boss's behalf.
func (s *RetroService) creator(ctx context.Context, workspaceID pgtype.UUID) (pgtype.UUID, error) {
	owners, err := s.Queries.ListWorkspaceOwnerUserIDs(ctx, workspaceID)
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("list owners: %w", err)
	}
	if len(owners) == 0 {
		return pgtype.UUID{}, ErrRetroNoLeader
	}
	return owners[0], nil
}

func (s *RetroService) initiativeClosedCandidates(ctx context.Context, workspaceID pgtype.UUID, now time.Time) ([]RetroCandidate, error) {
	since := now.Add(-RetroWindow)
	closed, err := s.Queries.ListInitiativesClosedSince(ctx, db.ListInitiativesClosedSinceParams{
		WorkspaceID: workspaceID, Since: pgtype.Timestamptz{Time: since, Valid: true},
	})
	if err != nil {
		return nil, fmt.Errorf("closed initiatives: %w", err)
	}
	out := make([]RetroCandidate, 0, len(closed))
	for _, ini := range closed {
		out = append(out, RetroCandidate{
			Trigger:       RetroTriggerInitiativeClosed,
			SubjectType:   RetroSubjectInitiative,
			SubjectID:     ini.ID,
			Name:          fmt.Sprintf("INI-%d %s", ini.Seq, ini.Title),
			LeaderSquadID: ini.OwnerSquadID,
			Evidence: []string{
				fmt.Sprintf("Initiative INI-%d closed on %s.", ini.Seq, ini.ClosedAt.Time.UTC().Format("2006-01-02")),
				"Reason given: " + ini.ClosedReason,
			},
			Facts: map[string]any{"status": ini.Status, "closed_reason": ini.ClosedReason},
		})
	}
	return out, nil
}

// initiativeScaledCandidates are the initiatives that scaled a follow-on in the
// window. The scaled initiative keeps operating, so its retrospective is about
// the operate stage that led to the scale, and its leader is the operate owner.
func (s *RetroService) initiativeScaledCandidates(ctx context.Context, workspaceID pgtype.UUID, now time.Time) ([]RetroCandidate, error) {
	since := now.Add(-RetroWindow)
	scaled, err := s.Queries.ListInitiativesScaledSince(ctx, db.ListInitiativesScaledSinceParams{
		WorkspaceID: workspaceID, Since: pgtype.Timestamptz{Time: since, Valid: true},
	})
	if err != nil {
		return nil, fmt.Errorf("scaled initiatives: %w", err)
	}
	out := make([]RetroCandidate, 0, len(scaled))
	for _, ini := range scaled {
		out = append(out, RetroCandidate{
			Trigger:       RetroTriggerInitiativeScaled,
			SubjectType:   RetroSubjectInitiative,
			SubjectID:     ini.ID,
			Name:          fmt.Sprintf("INI-%d %s", ini.Seq, ini.Title),
			LeaderSquadID: ini.OwnerSquadID,
			Evidence: []string{
				fmt.Sprintf("Initiative INI-%d scaled a follow-on at an operate review.", ini.Seq),
			},
			Facts: map[string]any{"status": ini.Status, "stage": ini.StageKey},
		})
	}
	return out, nil
}

func (s *RetroService) gateRejectedCandidates(ctx context.Context, workspaceID pgtype.UUID, now time.Time) ([]RetroCandidate, error) {
	rows, err := s.Queries.ListRejectedLaunchGates(ctx, db.ListRejectedLaunchGatesParams{
		WorkspaceID: workspaceID, Since: pgtype.Timestamptz{Time: now.Add(-RetroWindow), Valid: true},
	})
	if err != nil {
		return nil, fmt.Errorf("rejected launch gates: %w", err)
	}
	counts := map[string]int{}
	for _, r := range rows {
		counts[r.InitiativeID]++
	}
	var ids []string
	for id, n := range counts {
		if n >= retroGateRejectionMin {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	var out []RetroCandidate
	for _, id := range ids {
		ini, err := s.initiativeByID(ctx, workspaceID, id)
		if err != nil {
			return nil, err
		}
		if !ini.ID.Valid {
			continue
		}
		out = append(out, RetroCandidate{
			Trigger:       RetroTriggerGateRejected,
			SubjectType:   RetroSubjectInitiative,
			SubjectID:     ini.ID,
			Name:          fmt.Sprintf("INI-%d %s", ini.Seq, ini.Title),
			LeaderSquadID: ini.OwnerSquadID,
			Evidence:      []string{fmt.Sprintf("The launch gate of INI-%d was rejected %d times in the window.", ini.Seq, counts[id])},
			Facts:         map[string]any{"rejections": counts[id]},
		})
	}
	return out, nil
}

// initiativeByID loads an initiative of the workspace. A row that is gone or
// names another workspace is skipped, not an error.
func (s *RetroService) initiativeByID(ctx context.Context, workspaceID pgtype.UUID, id string) (db.Initiative, error) {
	uid, err := util.ParseUUID(id)
	if err != nil {
		return db.Initiative{}, nil
	}
	ini, err := s.Queries.GetInitiativeInWorkspace(ctx, db.GetInitiativeInWorkspaceParams{ID: uid, WorkspaceID: workspaceID})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.Initiative{}, nil
	}
	if err != nil {
		return db.Initiative{}, fmt.Errorf("load initiative: %w", err)
	}
	return ini, nil
}

func (s *RetroService) agentFailureCandidates(ctx context.Context, workspaceID pgtype.UUID, now time.Time) ([]RetroCandidate, error) {
	rows, err := s.Queries.ListAgentFailureClusters(ctx, db.ListAgentFailureClustersParams{
		WorkspaceID: workspaceID,
		Since:       pgtype.Timestamptz{Time: now.Add(-retroAgentFailureWindow), Valid: true},
		RowMin:      retroAgentFailureMin,
	})
	if err != nil {
		return nil, fmt.Errorf("agent failure clusters: %w", err)
	}
	var out []RetroCandidate
	for _, r := range rows {
		agent, err := s.Queries.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: r.AgentID, WorkspaceID: workspaceID})
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("load agent: %w", err)
		}
		out = append(out, RetroCandidate{
			Trigger:       RetroTriggerAgentFailures,
			SubjectType:   RetroSubjectAgent,
			SubjectID:     r.AgentID,
			Name:          agent.Name,
			LeaderSquadID: agent.HomeSquadID,
			Evidence: []string{
				fmt.Sprintf("%d failed runs of category %q in the last 7 days.", r.Failures, r.FailureReason),
			},
			Facts: map[string]any{"failure_reason": r.FailureReason, "failures": r.Failures},
		})
	}
	return out, nil
}

func (s *RetroService) squadRejectionCandidates(ctx context.Context, workspaceID pgtype.UUID, now time.Time) ([]RetroCandidate, error) {
	rows, err := s.Queries.ListSquadRecentDecisions(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("squad decisions: %w", err)
	}
	var out []RetroCandidate
	for _, id := range RetroRejectedInARow(rows, now.Add(-RetroWindow)) {
		sq, err := s.Queries.GetSquadLeaderForRetro(ctx, db.GetSquadLeaderForRetroParams{ID: id, WorkspaceID: workspaceID})
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("load squad: %w", err)
		}
		out = append(out, RetroCandidate{
			Trigger:       RetroTriggerSquadRejections,
			SubjectType:   RetroSubjectSquad,
			SubjectID:     id,
			Name:          sq.Name,
			LeaderSquadID: id,
			Evidence:      []string{fmt.Sprintf("The last three proposals of %s were rejected.", sq.Name)},
			Facts:         map[string]any{"rejections_in_a_row": retroSquadRejectionRun},
		})
	}
	return out, nil
}

// budgetHoldCandidates reads the budget holds the budget gate wrote to the
// governance log. A company or floor hold goes to the Secretary or the floor's
// chief; a squad or initiative hold goes to its leader.
func (s *RetroService) budgetHoldCandidates(ctx context.Context, workspaceID pgtype.UUID, now time.Time) ([]RetroCandidate, error) {
	holds, err := s.Queries.ListBudgetHoldsSince(ctx, db.ListBudgetHoldsSinceParams{
		WorkspaceID: workspaceID, Since: pgtype.Timestamptz{Time: now.Add(-RetroWindow), Valid: true},
	})
	if err != nil {
		return nil, fmt.Errorf("budget holds: %w", err)
	}
	type scopeKey struct{ kind, id string }
	seen := map[scopeKey]int{}
	var order []scopeKey
	for _, h := range holds {
		k := scopeKey{h.SubjectType, util.UUIDToString(h.SubjectID)}
		if seen[k] == 0 {
			order = append(order, k)
		}
		seen[k]++
	}
	var out []RetroCandidate
	for _, k := range order {
		c := RetroCandidate{
			Trigger:     RetroTriggerBudgetHold,
			SubjectType: k.kind,
			Evidence:    []string{fmt.Sprintf("%d budget hold(s) recorded for this %s in the window.", seen[k], k.kind)},
			Facts:       map[string]any{"holds": seen[k]},
		}
		switch k.kind {
		case RetroSubjectSquad:
			id, err := util.ParseUUID(k.id)
			if err != nil {
				continue
			}
			c.SubjectID, c.LeaderSquadID = id, id
			sq, err := s.Queries.GetSquadLeaderForRetro(ctx, db.GetSquadLeaderForRetroParams{ID: id, WorkspaceID: workspaceID})
			if err != nil {
				continue
			}
			c.Name = sq.Name
		case RetroSubjectInitiative:
			ini, err := s.initiativeByID(ctx, workspaceID, k.id)
			if err != nil || !ini.ID.Valid {
				continue
			}
			c.SubjectID, c.LeaderSquadID = ini.ID, ini.OwnerSquadID
			c.Name = fmt.Sprintf("INI-%d %s", ini.Seq, ini.Title)
		default:
			// A company or floor hold: the company's retrospective goes to the Secretary.
			c.SubjectType = RetroSubjectCompany
			c.Name = "the company's budget"
		}
		out = append(out, c)
	}
	return out, nil
}

func (s *RetroService) stageSLACandidates(ctx context.Context, workspaceID pgtype.UUID, now time.Time) ([]RetroCandidate, error) {
	entries, err := s.Queries.ListInitiativeStageEntriesSince(ctx, db.ListInitiativeStageEntriesSinceParams{
		WorkspaceID: workspaceID, Since: pgtype.Timestamptz{Time: now.Add(-retroStageHistory), Valid: true},
	})
	if err != nil {
		return nil, fmt.Errorf("stage entries: %w", err)
	}
	if len(entries) == 0 {
		return nil, nil
	}
	pipelines, err := s.Queries.ListPipelines(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list pipelines: %w", err)
	}
	slaByPipeline := map[string]map[string]int{}
	for _, p := range pipelines {
		var stages []PipelineStage
		if err := decodeStages(p.Stages, &stages); err != nil {
			return nil, err
		}
		m := map[string]int{}
		for _, st := range stages {
			m[st.Key] = st.SLADays
		}
		slaByPipeline[util.UUIDToString(p.ID)] = m
	}
	byInitiative := map[string][]RetroStageEntry{}
	var order []string
	for _, e := range entries {
		key := util.UUIDToString(e.InitiativeID)
		if _, ok := byInitiative[key]; !ok {
			order = append(order, key)
		}
		byInitiative[key] = append(byInitiative[key], RetroStageEntry{Stage: e.ToStage.String, At: e.CreatedAt.Time})
	}
	windowStart := now.Add(-RetroWindow)
	var out []RetroCandidate
	for _, key := range order {
		ini, err := s.initiativeByID(ctx, workspaceID, key)
		if err != nil {
			return nil, err
		}
		if !ini.ID.Valid || (ini.Status != InitiativeStatusActive && ini.Status != InitiativeStatusOperating) {
			continue
		}
		sla := slaByPipeline[util.UUIDToString(ini.PipelineID)]
		breaches := RetroStageBreachVisits(byInitiative[key], sla, now, windowStart)
		if breaches < retroSLABreachMin {
			continue
		}
		out = append(out, RetroCandidate{
			Trigger:       RetroTriggerStageSLA,
			SubjectType:   RetroSubjectInitiative,
			SubjectID:     ini.ID,
			Name:          fmt.Sprintf("INI-%d %s", ini.Seq, ini.Title),
			LeaderSquadID: ini.OwnerSquadID,
			Evidence:      []string{fmt.Sprintf("%d stage visits of INI-%d ran past their SLA in the last 14 days.", breaches, ini.Seq)},
			Facts:         map[string]any{"breaches": breaches},
		})
	}
	return out, nil
}
