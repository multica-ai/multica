package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// The digest is the Secretary's view of the company: facts computed by SQL and
// bounded in size, so the Secretary never scans the raw board. These constants
// are the bounds.
const (
	// DigestStaleAfter is how long an in-progress issue may go without
	// activity before the digest calls it stale.
	DigestStaleAfter = 72 * time.Hour
	// DigestMaxSquads is how many squads are reported in full; the rest are
	// ranked and cut with a count.
	DigestMaxSquads = 20
	// DigestMaxQuiet is how many quiet squads are named; the rest are counted.
	DigestMaxQuiet = 50
	// DigestRecentDoneLimit is how many completed issues a squad lists.
	DigestRecentDoneLimit = 5
	// DigestProposalListLimit is how many pending proposals a squad lists; the
	// squad's pending_proposal_count carries the total.
	DigestProposalListLimit = 10
	// DigestWorkspaceListLimit bounds each workspace-level list.
	DigestWorkspaceListLimit = 10
	// DigestMaxWindow is the longest span `since` may reach back.
	DigestMaxWindow = 35 * 24 * time.Hour
	// DigestDefaultWindow applies when the caller gives no `since`.
	DigestDefaultWindow = 24 * time.Hour
	// DigestKnowledgeStaleAfter is how long a published knowledge document may
	// go without a republish before the digest lists it for re-verification.
	DigestKnowledgeStaleAfter = 90 * 24 * time.Hour
	// DigestKnowledgeListLimit bounds the stale documents listed; the count
	// carries the total.
	DigestKnowledgeListLimit = 10
	// DigestSilentAfter is how long a chartered squad may go without activity
	// before the digest reports it as silent.
	DigestSilentAfter = 7 * 24 * time.Hour
	// DigestMaxRoutines bounds the routines a squad's charter section lists; the
	// routine_count carries the total.
	DigestMaxRoutines = 10
)

// ErrDigestWindow reports a `since` the digest refuses.
type ErrDigestWindow struct{ Message string }

func (e *ErrDigestWindow) Error() string { return e.Message }

// DigestIssueRef is an issue the digest points at. Mention is the markdown
// link the Secretary pastes into a brief.
type DigestIssueRef struct {
	ID         string `json:"id"`
	Identifier string `json:"identifier"`
	Title      string `json:"title"`
	Mention    string `json:"mention"`
}

type DigestProposalRef struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// DigestInquiry is a question the Secretary put to a squad leader on the day or
// the day before (UTC). Answered is set when the squad has replied on the issue,
// and AnswerExcerpt then carries the first characters of the reply. The JSON keys
// are question, answered and answer_excerpt; the Secretary reads them without
// searching.
type DigestInquiry struct {
	Question      string  `json:"question"`
	Answered      bool    `json:"answered"`
	AnswerExcerpt *string `json:"answer_excerpt"`
}

// DigestRoutine is one of a chartered squad's routines with its derived state.
type DigestRoutine struct {
	AutopilotID    string `json:"autopilot_id"`
	Title          string `json:"title"`
	Purpose        string `json:"purpose"`
	ExpectedOutput string `json:"expected_output"`
	Required       bool   `json:"required"`
	State          string `json:"state"`
}

// DigestSquadCharter is a chartered squad's section: its routine states and
// whether it has gone quiet. The routine states are facts; the Secretary asks
// the leader about a fix (see the brief's blockers).
type DigestSquadCharter struct {
	// Silent is set when nothing has happened in the squad for more than
	// DigestSilentAfter. The charter's own save counts as activity.
	Silent       bool            `json:"silent"`
	RoutineCount int             `json:"routine_count"`
	Routines     []DigestRoutine `json:"routines"`
}

// DigestRoutineRef is a required routine the workspace is not delivering.
type DigestRoutineRef struct {
	AutopilotID string `json:"autopilot_id"`
	SquadID     string `json:"squad_id"`
	SquadName   string `json:"squad_name"`
	Title       string `json:"title"`
	State       string `json:"state"`
}

type DigestRoutineRefList struct {
	Count int                `json:"count"`
	Items []DigestRoutineRef `json:"items"`
}

// DigestSquad is one squad reported in full.
type DigestSquad struct {
	ID      string  `json:"id"`
	Name    string  `json:"name"`
	FloorID *string `json:"floor_id"`
	// Counts. Done is since the digest's `since`; the others are the state now
	// (stale and blocked are subsets of what is in flight).
	Done           int `json:"done"`
	InProgress     int `json:"in_progress"`
	Blocked        int `json:"blocked"`
	Stale          int `json:"stale"`
	FailedRuns     int `json:"failed_runs"`
	LeaderNoAction int `json:"leader_no_action"`
	// PendingProposalCount is the total; PendingProposals lists the newest.
	PendingProposalCount int                 `json:"pending_proposal_count"`
	PendingProposals     []DigestProposalRef `json:"pending_proposals"`
	RecentDone           []DigestIssueRef    `json:"recent_done"`
	// Charter is nil when the squad has no charter.
	Charter *DigestSquadCharter `json:"charter"`

	// routinesOffTrack counts the squad's required routines not on time. It
	// ranks the squad and keeps it in full; the list is in the workspace section.
	routinesOffTrack int
}

// DigestQuietSquad is the single line a squad with nothing new gets.
type DigestQuietSquad struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	InProgress int    `json:"in_progress"`
}

type DigestQuiet struct {
	Count  int                `json:"count"`
	Squads []DigestQuietSquad `json:"squads"`
}

type DigestRuntimeRef struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Provider string `json:"provider"`
}

type DigestAgentRef struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	FailedRuns int    `json:"failed_runs"`
}

type DigestSquadRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type DigestRuntimeList struct {
	Count int                `json:"count"`
	Items []DigestRuntimeRef `json:"items"`
}

type DigestAgentList struct {
	Count int              `json:"count"`
	Items []DigestAgentRef `json:"items"`
}

type DigestSquadList struct {
	Count int              `json:"count"`
	Items []DigestSquadRef `json:"items"`
}

// DigestWorkspace is the workspace-level section.
type DigestWorkspace struct {
	Runs       int `json:"runs"`
	FailedRuns int `json:"failed_runs"`
	// Tokens are all usage; ReportedCostUSD only counts runs whose provider
	// reported a cost.
	InputTokens     int64             `json:"input_tokens"`
	OutputTokens    int64             `json:"output_tokens"`
	ReportedCostUSD float64           `json:"reported_cost_usd"`
	OfflineRuntimes DigestRuntimeList `json:"offline_runtimes"`
	AgentsFailed    DigestAgentList   `json:"agents_with_failed_runs"`
	UnseatedSquads  DigestSquadList   `json:"unseated_squads"`
	// RoutinesNotOnTime lists the required routines whose state is not on_time,
	// for the brief's blockers and risks. Count is the total.
	RoutinesNotOnTime DigestRoutineRefList `json:"routines_not_on_time"`
}

// DigestKnowledgeDocRef is a published knowledge document the digest flags.
type DigestKnowledgeDocRef struct {
	ID            string `json:"id"`
	Title         string `json:"title"`
	KnowledgeBase string `json:"knowledge_base"`
	PublishedAt   string `json:"published_at"`
}

type DigestKnowledgeStaleList struct {
	Count int                     `json:"count"`
	Items []DigestKnowledgeDocRef `json:"items"`
}

// DigestKnowledge is the company knowledge section. It is empty, and not an
// error, when the workspace does not have ready knowledge.
type DigestKnowledge struct {
	// StalePublished lists published documents not republished for
	// DigestKnowledgeStaleAfter, oldest first.
	StalePublished DigestKnowledgeStaleList `json:"stale_published"`
	// PendingDrafts counts drafts awaiting publishing across the bases.
	PendingDrafts int `json:"pending_drafts"`
	// Retrieval is the latest eval run against the previous one. Nil until a
	// run is recorded. A fall beyond EvalDropThreshold carries a risk line.
	Retrieval *DigestRetrieval `json:"retrieval"`
	// AutoPublished lists the documents Multica published without a person
	// pressing publish since the window opened (spec 3.3). The brief's FYI names
	// each one. Count is the total, Items is capped at DigestAutoPublishMax.
	AutoPublished DigestAutoPublishList `json:"auto_published"`
}

// DigestAutoPublishMax bounds the automatic publishes the digest lists, newest first.
const DigestAutoPublishMax = 20

// DigestAutoPublishList is the automatic publishes in the window.
type DigestAutoPublishList struct {
	Count int                 `json:"count"`
	Items []DigestAutoPublish `json:"items"`
}

// DigestAutoPublish is one document published automatically: by an
// auto_after_review verdict of another squad, or by an auto policy.
type DigestAutoPublish struct {
	LinkID        string `json:"link_id"`
	Title         string `json:"title"`
	KnowledgeBase string `json:"knowledge_base"`
	Kind          string `json:"kind"`
	Mode          string `json:"mode"`
	AuthorSquadID string `json:"author_squad_id"`
	CriticSquadID string `json:"critic_squad_id"`
	PublishedAt   string `json:"published_at"`
}

// Digest is the whole response of GET /api/company/digest.
type Digest struct {
	Since     string          `json:"since"`
	Until     string          `json:"until"`
	Workspace DigestWorkspace `json:"workspace"`
	// Squads are ordered by what needs attention first, at most DigestMaxSquads.
	Squads []DigestSquad `json:"squads"`
	// OmittedSquadCount is how many squads with news were ranked below the cut.
	OmittedSquadCount int             `json:"omitted_squad_count"`
	Quiet             DigestQuiet     `json:"quiet"`
	Knowledge         DigestKnowledge `json:"knowledge"`
	// Inquiries are the Secretary's questions to squad leaders asked today or
	// yesterday (UTC), newest first. The daily cap keeps the list short.
	Inquiries []DigestInquiry `json:"inquiries"`
	// Pipeline lists each initiative that is active or operating (spec 3.7).
	Pipeline []DigestInitiative `json:"pipeline"`
	// OverdueRequests are squad requests the receiving squad has not answered
	// within its response_hours (spec 3.5).
	OverdueRequests []DigestOverdueRequest `json:"overdue_requests"`
	// People lists the reviews published since the window opened, with the
	// organisation changes they recommended (spec 4.6).
	People DigestPeople `json:"people"`
	// AutoApproved are proposals a standing approval decided in the window. The
	// brief's FYI names each one as "Auto-approved by policy" (spec 4.4).
	AutoApproved []DigestAutoApproval `json:"auto_approved"`
	// Budget is the brief's Budget section: budgets at or above their soft line
	// and the holds in force (spec 4.2).
	Budget DigestBudget `json:"budget"`
	// Mode is the source the Secretary reads: squads, or floors when the company
	// is large (spec 4.8). In floors mode Squads is empty and Floors carries each
	// floor's latest brief and whether a squad on it needs the boss.
	Mode   string        `json:"mode"`
	Scope  DigestScope   `json:"scope"`
	Floors []DigestFloor `json:"floors"`
	// Goals are the active goals the brief reports: those whose state changed
	// since the last brief and those at risk or off track (spec 4.1).
	Goals []DigestGoal `json:"goals"`
	// SystemRisks are risk lines about the system itself: a stuck scheduled job
	// (spec 4.9).
	SystemRisks []string `json:"system_risks"`
}

// DigestScope says whose digest this is: the company's, or one floor's.
//
// The counts are the totals inside the scope. A floor digest counts only the
// floor's squads, the initiatives those squads own, and the budgets and goals
// attached to the floor, its squads or those initiatives. The company's usage
// totals, knowledge, inquiries, reviews and runtimes belong to no floor: a floor
// digest reports the usage totals as zero, and the other sections as the company's.
type DigestScope struct {
	Type    string            `json:"type"`
	FloorID *string           `json:"floor_id"`
	Counts  DigestScopeCounts `json:"counts"`
}

// DigestScopeCounts are the totals of a scope.
type DigestScopeCounts struct {
	Squads           int `json:"squads"`
	Initiatives      int `json:"initiatives"`
	PendingDecisions int `json:"pending_decisions"`
	Budgets          int `json:"budgets"`
	Goals            int `json:"goals"`
}

// DigestFloor is one floor's line in a floors-mode digest.
type DigestFloor struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	ChiefAgentID *string `json:"chief_agent_id"`
	SquadCount   int     `json:"squad_count"`
	// Flagged is set when a squad on the floor needs the boss's attention: a
	// pending proposal, blocked or failed work, stale work, an off-track routine
	// or silence. The Secretary drills into a flagged floor with floor_id.
	Flagged bool `json:"flagged"`
	// Brief is the floor's latest brief in the last day and a half; nil when the
	// chief has not published one.
	Brief *DigestFloorBrief `json:"brief"`
}

// DigestFloorBrief is a floor chief's brief as the company digest carries it.
type DigestFloorBrief struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	CreatedAt string `json:"created_at"`
	Body      string `json:"body"`
}

// DigestInitiative is one initiative's place in its pipeline. Stalled is set
// when the stage has run past its sla_days; it is a flag, not a move.
type DigestInitiative struct {
	ID                    string   `json:"id"`
	Identifier            string   `json:"identifier"`
	Title                 string   `json:"title"`
	StageKey              string   `json:"stage_key"`
	StageName             string   `json:"stage_name"`
	OwnerSquadID          *string  `json:"owner_squad_id"`
	Status                string   `json:"status"`
	DaysInStage           int      `json:"days_in_stage"`
	SLADays               int      `json:"sla_days"`
	Stalled               bool     `json:"stalled"`
	ArtifactsRequired     int      `json:"artifacts_required"`
	ArtifactsPresent      int      `json:"artifacts_present"`
	Blockers              []string `json:"blockers"`
	NextGate              string   `json:"next_gate"`
	PendingGateProposalID *string  `json:"pending_gate_proposal_id"`
}

// DigestAutoApprovalMax bounds the auto-approvals the digest lists, newest first.
const DigestAutoApprovalMax = 20

// DigestAutoApproval is one proposal a standing approval approved.
type DigestAutoApproval struct {
	ProposalID string `json:"proposal_id"`
	Identifier string `json:"identifier"`
	Title      string `json:"title"`
	SquadID    string `json:"squad_id"`
	PolicyID   string `json:"policy_id"`
	PolicyName string `json:"policy_name"`
	DecidedAt  string `json:"decided_at"`
}

// DigestOverdueRequest is a squad request past the receiving squad's response time.
type DigestOverdueRequest struct {
	IssueID      string `json:"issue_id"`
	Identifier   string `json:"identifier"`
	Title        string `json:"title"`
	SquadID      string `json:"squad_id"`
	HoursOverdue int    `json:"hours_overdue"`
}

// DigestService computes the digest.
type DigestService struct {
	Queries *db.Queries
	// Now is the clock; nil means time.Now. Tests pin it.
	Now func() time.Time
	// FloorID, when valid, scopes the digest to one floor's squads (a floor
	// chief's digest). Invalid means the whole company.
	FloorID pgtype.UUID
	// Mode is squads or floors (see ResolveDigestMode). Empty means squads.
	Mode string
}

func NewDigestService(q *db.Queries) *DigestService { return &DigestService{Queries: q} }

func (s *DigestService) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// ParseDigestSince reads the `since` query value: an RFC 3339 timestamp, or
// empty for the default window.
func ParseDigestSince(raw string, now time.Time) (time.Time, error) {
	if strings.TrimSpace(raw) == "" {
		return now.Add(-DigestDefaultWindow), nil
	}
	since, err := time.Parse(time.RFC3339, strings.TrimSpace(raw))
	if err != nil {
		return time.Time{}, &ErrDigestWindow{Message: "since must be an RFC 3339 timestamp, for example 2026-05-01T08:00:00Z"}
	}
	if since.After(now) {
		return time.Time{}, &ErrDigestWindow{Message: "since must not be in the future"}
	}
	if now.Sub(since) > DigestMaxWindow {
		return time.Time{}, &ErrDigestWindow{Message: fmt.Sprintf("since must be within the last %d days", int(DigestMaxWindow.Hours()/24))}
	}
	return since, nil
}

// quiet reports whether a squad has nothing worth the boss's attention. A squad
// with work merely in flight is quiet: it still gets its in-progress count on
// its single line. A squad with an off-track routine or gone silent is not.
func (q DigestSquad) quiet() bool {
	return q.Done == 0 && q.Blocked == 0 && q.Stale == 0 && q.FailedRuns == 0 &&
		q.LeaderNoAction == 0 && q.PendingProposalCount == 0 &&
		q.routinesOffTrack == 0 && !q.silent()
}

func (q DigestSquad) silent() bool { return q.Charter != nil && q.Charter.Silent }

func digestBoolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// attentionLess orders squads by what needs the boss first: pending proposals
// (only the boss can clear them), then blocked work, failed runs, stale work,
// leaders that did nothing, routines off track, silence, then plain activity.
// Ties break on name and id so the order is stable.
func attentionLess(a, b DigestSquad) bool {
	keys := [][2]int{
		{a.PendingProposalCount, b.PendingProposalCount},
		{a.Blocked, b.Blocked},
		{a.FailedRuns, b.FailedRuns},
		{a.Stale, b.Stale},
		{a.LeaderNoAction, b.LeaderNoAction},
		{a.routinesOffTrack, b.routinesOffTrack},
		{digestBoolInt(a.silent()), digestBoolInt(b.silent())},
		{a.Done + a.InProgress, b.Done + b.InProgress},
	}
	for _, k := range keys {
		if k[0] != k[1] {
			return k[0] > k[1]
		}
	}
	if a.Name != b.Name {
		return a.Name < b.Name
	}
	return a.ID < b.ID
}

// rankSquads splits squads into the ones reported in full (ranked, capped), the
// count cut below the cap, and the quiet ones.
func rankSquads(all []DigestSquad) (full []DigestSquad, omitted int, quiet DigestQuiet) {
	var loud []DigestSquad
	var quietSquads []DigestSquad
	for _, sq := range all {
		if sq.quiet() {
			quietSquads = append(quietSquads, sq)
		} else {
			loud = append(loud, sq)
		}
	}
	sort.SliceStable(loud, func(i, j int) bool { return attentionLess(loud[i], loud[j]) })
	sort.SliceStable(quietSquads, func(i, j int) bool {
		if quietSquads[i].Name != quietSquads[j].Name {
			return quietSquads[i].Name < quietSquads[j].Name
		}
		return quietSquads[i].ID < quietSquads[j].ID
	})

	full = loud
	if len(full) > DigestMaxSquads {
		omitted = len(full) - DigestMaxSquads
		full = full[:DigestMaxSquads]
	}
	quiet = DigestQuiet{Count: len(quietSquads), Squads: []DigestQuietSquad{}}
	for i, sq := range quietSquads {
		if i == DigestMaxQuiet {
			break
		}
		quiet.Squads = append(quiet.Squads, DigestQuietSquad{ID: sq.ID, Name: sq.Name, InProgress: sq.InProgress})
	}
	return full, omitted, quiet
}

// digestScope is the set of objects a digest counts. The company digest counts
// everything. A floor digest counts only the floor's squads (sc.squads), the
// initiatives those squads own (sc.initiatives), and the budgets and goals
// attached to the floor, to one of its squads, or to one of those initiatives.
type digestScope struct {
	floor       bool
	floorID     pgtype.UUID
	squads      map[string]bool
	squadIDs    []pgtype.UUID
	initiatives map[string]bool
}

// keeps reports whether an object with this scope type and id belongs to the
// digest's scope. A company object belongs to the company digest only.
func (sc digestScope) keeps(scopeType string, scopeID pgtype.UUID) bool {
	if !sc.floor {
		return true
	}
	id := util.UUIDToString(scopeID)
	switch scopeType {
	case ScopeFloor:
		return scopeID == sc.floorID
	case ScopeSquad:
		return sc.squads[id]
	case ScopeInitiative:
		return sc.initiatives[id]
	}
	return false
}

// digestScopeOf reads the floor's squads and initiatives for a floor digest. The
// company digest returns the zero scope, which keeps everything.
func (s *DigestService) digestScopeOf(ctx context.Context, workspaceID pgtype.UUID, all []DigestSquad) (digestScope, error) {
	if !s.FloorID.Valid {
		return digestScope{}, nil
	}
	sc := digestScope{floor: true, floorID: s.FloorID, squads: map[string]bool{}, initiatives: map[string]bool{}}
	for _, sq := range all {
		sc.squads[sq.ID] = true
		sc.squadIDs = append(sc.squadIDs, util.MustParseUUID(sq.ID))
	}
	list, err := s.Queries.ListInitiatives(ctx, db.ListInitiativesParams{WorkspaceID: workspaceID})
	if err != nil {
		return digestScope{}, fmt.Errorf("digest floor initiatives: %w", err)
	}
	for _, ini := range list {
		if ini.OwnerSquadID.Valid && sc.squads[util.UUIDToString(ini.OwnerSquadID)] {
			sc.initiatives[util.UUIDToString(ini.ID)] = true
		}
	}
	return sc, nil
}

// Compute builds the digest for the workspace since the given time.
// issuePrefix is the workspace's issue prefix, for identifiers and mentions.
func (s *DigestService) Compute(ctx context.Context, workspaceID pgtype.UUID, since time.Time, issuePrefix string) (Digest, error) {
	now := s.now()
	sinceTS := pgtype.Timestamptz{Time: since, Valid: true}

	rows, err := s.Queries.DigestSquadStats(ctx, db.DigestSquadStatsParams{
		WorkspaceID: workspaceID,
		Since:       sinceTS,
		StaleBefore: pgtype.Timestamptz{Time: now.Add(-DigestStaleAfter), Valid: true},
		Now:         pgtype.Timestamptz{Time: now, Valid: true},
	})
	if err != nil {
		return Digest{}, fmt.Errorf("digest squad stats: %w", err)
	}
	all := make([]DigestSquad, 0, len(rows))
	for _, r := range rows {
		if s.FloorID.Valid && (!r.FloorID.Valid || r.FloorID != s.FloorID) {
			continue
		}
		all = append(all, DigestSquad{
			ID:                   util.UUIDToString(r.SquadID),
			Name:                 r.Name,
			FloorID:              util.UUIDToPtr(r.FloorID),
			Done:                 int(r.DoneCount),
			InProgress:           int(r.InProgressCount),
			Blocked:              int(r.BlockedCount),
			Stale:                int(r.StaleCount),
			FailedRuns:           int(r.FailedRunsCount),
			LeaderNoAction:       int(r.LeaderNoActionCount),
			PendingProposalCount: int(r.PendingProposalCount),
			PendingProposals:     []DigestProposalRef{},
			RecentDone:           []DigestIssueRef{},
		})
	}

	offTrack, err := s.attachRoutines(ctx, workspaceID, now, all)
	if err != nil {
		return Digest{}, err
	}

	squads, omitted, quiet := rankSquads(all)
	floorsMode := s.Mode == DigestModeFloors && !s.FloorID.Valid
	if floorsMode {
		omitted += len(squads)
		squads = []DigestSquad{}
		quiet.Squads = []DigestQuietSquad{}
	}

	// Details only for the squads reported in full.
	if len(squads) > 0 {
		ids := make([]pgtype.UUID, len(squads))
		index := make(map[string]*DigestSquad, len(squads))
		for i := range squads {
			ids[i] = util.MustParseUUID(squads[i].ID)
			index[squads[i].ID] = &squads[i]
		}
		proposals, err := s.Queries.DigestPendingProposals(ctx, db.DigestPendingProposalsParams{
			WorkspaceID: workspaceID, SquadIds: ids, PerSquadLimit: DigestProposalListLimit,
			Now: pgtype.Timestamptz{Time: now, Valid: true},
		})
		if err != nil {
			return Digest{}, fmt.Errorf("digest pending proposals: %w", err)
		}
		for _, p := range proposals {
			if sq := index[util.UUIDToString(p.SquadID)]; sq != nil {
				sq.PendingProposals = append(sq.PendingProposals, DigestProposalRef{ID: util.UUIDToString(p.ProposalID), Title: p.Title})
			}
		}
		done, err := s.Queries.DigestRecentDoneIssues(ctx, db.DigestRecentDoneIssuesParams{
			WorkspaceID: workspaceID, SquadIds: ids, Since: sinceTS, PerSquadLimit: DigestRecentDoneLimit,
		})
		if err != nil {
			return Digest{}, fmt.Errorf("digest recent done issues: %w", err)
		}
		for _, d := range done {
			if sq := index[util.UUIDToString(d.SquadID)]; sq != nil {
				sq.RecentDone = append(sq.RecentDone, digestIssueRef(issuePrefix, util.UUIDToString(d.IssueID), d.Number, d.Title))
			}
		}
	}

	sc, err := s.digestScopeOf(ctx, workspaceID, all)
	if err != nil {
		return Digest{}, err
	}
	ws, err := s.computeWorkspace(ctx, workspaceID, sinceTS, sc)
	if err != nil {
		return Digest{}, err
	}
	ws.RoutinesNotOnTime = offTrack
	know, err := s.computeKnowledge(ctx, workspaceID, now, since)
	if err != nil {
		return Digest{}, err
	}
	inquiries, err := s.computeInquiries(ctx, workspaceID, now)
	if err != nil {
		return Digest{}, err
	}
	pipeline, err := s.computePipeline(ctx, workspaceID, now)
	if err != nil {
		return Digest{}, err
	}
	goals, goalCount, err := s.computeGoals(ctx, workspaceID, since, now, sc)
	if err != nil {
		return Digest{}, err
	}
	overdue, err := s.computeOverdueRequests(ctx, workspaceID, now, issuePrefix)
	if err != nil {
		return Digest{}, err
	}
	people, err := s.computePeople(ctx, workspaceID, sinceTS)
	if err != nil {
		return Digest{}, err
	}
	autoApproved, err := s.computeAutoApproved(ctx, workspaceID, since, issuePrefix, sc)
	if err != nil {
		return Digest{}, err
	}

	if sc.floor {
		routinesOffTrack := 0
		for _, sq := range all {
			routinesOffTrack += sq.routinesOffTrack
		}
		pipeline = filterPipelineToSquads(pipeline, sc.squads)
		overdue = filterOverdueToSquads(overdue, sc.squads)
		ws.RoutinesNotOnTime = floorRoutineRefs(ws.RoutinesNotOnTime, sc.squads, routinesOffTrack)
	}
	scope := DigestScope{Type: "company"}
	if sc.floor {
		scope = DigestScope{Type: "floor", FloorID: util.UUIDToPtr(s.FloorID)}
	}
	floors := []DigestFloor{}
	if floorsMode {
		floors, err = s.computeFloors(ctx, workspaceID, now, all)
		if err != nil {
			return Digest{}, err
		}
	}
	risks := s.systemRisks(ctx, now)

	mode := DigestModeSquads
	if floorsMode {
		mode = DigestModeFloors
	}
	budget, budgetCount, err := s.computeBudget(ctx, workspaceID, now, sc)
	if err != nil {
		return Digest{}, err
	}
	pendingDecisions := 0
	for _, sq := range all {
		pendingDecisions += sq.PendingProposalCount
	}
	scope.Counts = DigestScopeCounts{
		Squads: len(all), Initiatives: len(pipeline), PendingDecisions: pendingDecisions,
		Budgets: budgetCount, Goals: goalCount,
	}
	return Digest{
		Since:             since.UTC().Format(time.RFC3339),
		Until:             now.UTC().Format(time.RFC3339),
		Workspace:         ws,
		Squads:            squads,
		OmittedSquadCount: omitted,
		Quiet:             quiet,
		Knowledge:         know,
		Inquiries:         inquiries,
		Pipeline:          pipeline,
		OverdueRequests:   overdue,
		People:            people,
		AutoApproved:      autoApproved,
		Budget:            budget,
		Mode:              mode,
		Scope:             scope,
		Floors:            floors,
		Goals:             goals,
		SystemRisks:       risks,
	}, nil
}

// computePipeline reports every active or operating initiative: its stage, how
// long it has been there, whether that is past the stage's SLA, what it is
// waiting for, and the gate it is waiting at, if any.
func (s *DigestService) computePipeline(ctx context.Context, workspaceID pgtype.UUID, now time.Time) ([]DigestInitiative, error) {
	list, err := s.Queries.ListInitiatives(ctx, db.ListInitiativesParams{WorkspaceID: workspaceID})
	if err != nil {
		return nil, fmt.Errorf("digest initiatives: %w", err)
	}
	out := []DigestInitiative{}
	if len(list) == 0 {
		return out, nil
	}
	pipelines, err := s.Queries.ListPipelines(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("digest pipelines: %w", err)
	}
	stagesByPipeline := make(map[pgtype.UUID][]PipelineStage, len(pipelines))
	for _, p := range pipelines {
		var stages []PipelineStage
		if err := decodeStages(p.Stages, &stages); err != nil {
			return nil, err
		}
		stagesByPipeline[p.ID] = stages
	}
	pending := map[string]pgtype.UUID{}
	gates, err := s.Queries.ListPendingGateProposalsForWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("digest pending gates: %w", err)
	}
	for _, g := range gates {
		if _, ok := pending[g.InitiativeID]; !ok {
			pending[g.InitiativeID] = g.ProposalID
		}
	}
	artifacts, err := s.Queries.ListInitiativeArtifactsForWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("digest artifacts: %w", err)
	}
	byInitiative := map[string][]db.InitiativeArtifact{}
	for _, a := range artifacts {
		key := util.UUIDToString(a.InitiativeID)
		byInitiative[key] = append(byInitiative[key], a)
	}

	for _, ini := range list {
		if ini.Status != InitiativeStatusActive && ini.Status != InitiativeStatusOperating {
			continue
		}
		stages := stagesByPipeline[ini.PipelineID]
		idx := stageIndex(stages, ini.StageKey)
		if idx < 0 {
			continue
		}
		stage := stages[idx]
		days := int(now.Sub(ini.StageEnteredAt.Time).Hours() / 24)
		item := DigestInitiative{
			ID:           util.UUIDToString(ini.ID),
			Identifier:   fmt.Sprintf("INI-%d", ini.Seq),
			Title:        ini.Title,
			StageKey:     stage.Key,
			StageName:    stage.Name,
			OwnerSquadID: util.UUIDToPtr(ini.OwnerSquadID),
			Status:       ini.Status,
			DaysInStage:  days,
			SLADays:      stage.SLADays,
			Stalled:      stage.SLADays > 0 && days > stage.SLADays,
			Blockers:     []string{},
		}
		for _, a := range byInitiative[item.ID] {
			if a.StageKey != stage.Key || !a.Required {
				continue
			}
			item.ArtifactsRequired++
			if artifactSatisfied(a) {
				item.ArtifactsPresent++
			} else {
				item.Blockers = append(item.Blockers, "missing "+a.Kind)
			}
		}
		if stage.Exit.Mode == PipelineExitGate {
			item.NextGate = stage.Exit.Criteria
		}
		if id, ok := pending[item.ID]; ok {
			pid := util.UUIDToString(id)
			item.PendingGateProposalID = &pid
		}
		if item.Stalled {
			item.Blockers = append(item.Blockers, fmt.Sprintf("stalled: %d days in %s, SLA %d", days, stage.Name, stage.SLADays))
		}
		out = append(out, item)
	}
	return out, nil
}

// computeAutoApproved lists the proposals a standing approval approved since the
// window's start, newest first.
func (s *DigestService) computeAutoApproved(ctx context.Context, workspaceID pgtype.UUID, since time.Time, prefix string, sc digestScope) ([]DigestAutoApproval, error) {
	var squadIDs []pgtype.UUID
	if sc.floor {
		squadIDs = sc.squadIDs
	}
	rows, err := (&ApprovalPolicyService{Queries: s.Queries}).ListRecentAutoApprovals(ctx, workspaceID, since, DigestAutoApprovalMax, squadIDs)
	if err != nil {
		return nil, fmt.Errorf("digest auto-approvals: %w", err)
	}
	out := make([]DigestAutoApproval, 0, len(rows))
	for _, r := range rows {
		out = append(out, DigestAutoApproval{
			ProposalID: util.UUIDToString(r.ProposalID),
			Identifier: IssueIdentifier(prefix, r.IssueNumber),
			Title:      r.Title,
			SquadID:    util.UUIDToString(r.SquadID),
			PolicyID:   util.UUIDToString(r.PolicyID),
			PolicyName: r.PolicyName,
			DecidedAt:  r.DecidedAt.UTC().Format(time.RFC3339),
		})
	}
	return out, nil
}

// computeOverdueRequests lists squad requests older than the receiving squad's
// response_hours.
func (s *DigestService) computeOverdueRequests(ctx context.Context, workspaceID pgtype.UUID, now time.Time, prefix string) ([]DigestOverdueRequest, error) {
	rows, err := s.Queries.ListOverdueSquadRequests(ctx, db.ListOverdueSquadRequestsParams{
		WorkspaceID: workspaceID, Now: pgtype.Timestamptz{Time: now, Valid: true},
	})
	if err != nil {
		return nil, fmt.Errorf("digest overdue requests: %w", err)
	}
	out := make([]DigestOverdueRequest, 0, len(rows))
	for _, r := range rows {
		deadline := r.CreatedAt.Time.Add(time.Duration(r.ResponseHours) * time.Hour)
		out = append(out, DigestOverdueRequest{
			IssueID:      util.UUIDToString(r.IssueID),
			Identifier:   IssueIdentifier(prefix, r.Number),
			Title:        r.Title,
			SquadID:      util.UUIDToString(r.SquadID),
			HoursOverdue: int(now.Sub(deadline).Hours()),
		})
	}
	return out, nil
}

// computeInquiries lists the inquiries asked today or yesterday (UTC). The
// window is the day, not the digest's since: the Secretary reads an answer in the
// next digest after it arrives, whatever period that digest covers.
func (s *DigestService) computeInquiries(ctx context.Context, workspaceID pgtype.UUID, now time.Time) ([]DigestInquiry, error) {
	yesterday := briefInquiryDay(now).AddDate(0, 0, -1)
	rows, err := s.Queries.ListBriefInquiriesForDigest(ctx, db.ListBriefInquiriesForDigestParams{
		WorkspaceID: workspaceID, AskedOn: pgtype.Date{Time: yesterday, Valid: true},
	})
	if err != nil {
		return nil, fmt.Errorf("digest inquiries: %w", err)
	}
	out := make([]DigestInquiry, 0, len(rows))
	for _, row := range rows {
		item := DigestInquiry{Question: row.Question, Answered: row.AnsweredAt.Valid}
		if item.Answered && row.AnswerExcerpt.Valid {
			excerpt := row.AnswerExcerpt.String
			item.AnswerExcerpt = &excerpt
		}
		out = append(out, item)
	}
	return out, nil
}

// attachRoutines derives every routine's state, gives each chartered squad its
// charter section, and returns the required routines not on time, for the
// workspace section. A routine of a squad that is not live (archived) is left out.
// A squad without a charter gets no section, but its routines still count.
func (s *DigestService) attachRoutines(ctx context.Context, workspaceID pgtype.UUID, now time.Time, squads []DigestSquad) (DigestRoutineRefList, error) {
	index := make(map[string]int, len(squads))
	for i := range squads {
		index[squads[i].ID] = i
	}

	states, err := RoutineComplianceStates(ctx, s.Queries, workspaceID, pgtype.UUID{}, now)
	if err != nil {
		return DigestRoutineRefList{}, fmt.Errorf("digest routine states: %w", err)
	}
	bySquad := make(map[int][]RoutineCompliance)
	var offTrack []DigestRoutineRef
	for _, rs := range states {
		i, ok := index[rs.SquadID]
		if !ok {
			continue
		}
		bySquad[i] = append(bySquad[i], rs)
		if rs.Required && rs.State != RoutineStateOnTime {
			squads[i].routinesOffTrack++
			offTrack = append(offTrack, DigestRoutineRef{
				AutopilotID: rs.AutopilotID, SquadID: rs.SquadID, SquadName: squads[i].Name,
				Title: rs.Title, State: rs.State,
			})
		}
	}
	sort.SliceStable(offTrack, func(i, j int) bool {
		a, b := offTrack[i], offTrack[j]
		if a.SquadName != b.SquadName {
			return a.SquadName < b.SquadName
		}
		if a.Title != b.Title {
			return a.Title < b.Title
		}
		return a.AutopilotID < b.AutopilotID
	})
	list := DigestRoutineRefList{Count: len(offTrack), Items: []DigestRoutineRef{}}
	if len(offTrack) > DigestWorkspaceListLimit {
		offTrack = offTrack[:DigestWorkspaceListLimit]
	}
	list.Items = append(list.Items, offTrack...)

	activity, err := s.Queries.DigestCharterActivity(ctx, workspaceID)
	if err != nil {
		return DigestRoutineRefList{}, fmt.Errorf("digest charter activity: %w", err)
	}
	for _, a := range activity {
		i, ok := index[util.UUIDToString(a.SquadID)]
		if !ok {
			continue
		}
		charter := &DigestSquadCharter{
			Silent:   now.Sub(a.LastActivityAt.Time) > DigestSilentAfter,
			Routines: []DigestRoutine{},
		}
		for _, rs := range bySquad[i] {
			charter.RoutineCount++
			if len(charter.Routines) < DigestMaxRoutines {
				charter.Routines = append(charter.Routines, DigestRoutine{
					AutopilotID: rs.AutopilotID, Title: rs.Title, Purpose: rs.Purpose,
					ExpectedOutput: rs.ExpectedOutput, Required: rs.Required, State: rs.State,
				})
			}
		}
		squads[i].Charter = charter
	}
	return list, nil
}

// computeKnowledge reads the knowledge registry only, never the sidecar: the
// digest is cheap and must answer while knowledge is offline. A workspace that
// is not ready has no rows in the joins, so the section comes back empty.
func (s *DigestService) computeKnowledge(ctx context.Context, workspaceID pgtype.UUID, now, since time.Time) (DigestKnowledge, error) {
	know := DigestKnowledge{
		StalePublished: DigestKnowledgeStaleList{Items: []DigestKnowledgeDocRef{}},
		AutoPublished:  DigestAutoPublishList{Items: []DigestAutoPublish{}},
	}

	stale, err := s.Queries.DigestKnowledgeStale(ctx, db.DigestKnowledgeStaleParams{
		WorkspaceID:     workspaceID,
		PublishedBefore: pgtype.Timestamptz{Time: now.Add(-DigestKnowledgeStaleAfter), Valid: true},
		RowLimit:        DigestKnowledgeListLimit,
	})
	if err != nil {
		return know, fmt.Errorf("digest knowledge stale documents: %w", err)
	}
	for _, d := range stale {
		know.StalePublished.Count = int(d.TotalCount)
		know.StalePublished.Items = append(know.StalePublished.Items, DigestKnowledgeDocRef{
			ID: util.UUIDToString(d.ID), Title: d.Title, KnowledgeBase: d.KnowledgeBase,
			PublishedAt: d.PublishedAt.Time.UTC().Format(time.RFC3339),
		})
	}

	autoRows, err := s.Queries.DigestAutoPublished(ctx, db.DigestAutoPublishedParams{
		WorkspaceID: workspaceID, Since: pgtype.Timestamptz{Time: since, Valid: true}, RowLimit: DigestAutoPublishMax,
	})
	if err != nil {
		return know, fmt.Errorf("digest automatic publishes: %w", err)
	}
	for _, r := range autoRows {
		know.AutoPublished.Count = int(r.TotalCount)
		item := DigestAutoPublish{
			LinkID: util.UUIDToString(r.LinkID), Title: r.Title,
			PublishedAt: r.CreatedAt.Time.UTC().Format(time.RFC3339),
		}
		var d struct {
			KnowledgeBase string `json:"knowledge_base"`
			Kind          string `json:"kind"`
			Mode          string `json:"mode"`
			AuthorSquad   string `json:"author_squad_id"`
			CriticSquad   string `json:"critic_squad_id"`
		}
		if err := json.Unmarshal(r.Details, &d); err != nil {
			return know, fmt.Errorf("digest automatic publish details: %w", err)
		}
		item.KnowledgeBase, item.Kind, item.Mode = d.KnowledgeBase, d.Kind, d.Mode
		item.AuthorSquadID, item.CriticSquadID = d.AuthorSquad, d.CriticSquad
		know.AutoPublished.Items = append(know.AutoPublished.Items, item)
	}

	drafts, err := s.Queries.DigestKnowledgePendingDrafts(ctx, workspaceID)
	if err != nil {
		return know, fmt.Errorf("digest knowledge pending drafts: %w", err)
	}
	know.PendingDrafts = int(drafts)

	runs, err := s.Queries.ListKnowledgeEvalRuns(ctx, db.ListKnowledgeEvalRunsParams{WorkspaceID: workspaceID, Limit: 2})
	if err != nil {
		return know, fmt.Errorf("digest knowledge eval runs: %w", err)
	}
	know.Retrieval = retrievalHealth(runs)
	return know, nil
}

func digestIssueRef(prefix, id string, number int32, title string) DigestIssueRef {
	identifier := prefix + "-" + strconv.Itoa(int(number))
	return DigestIssueRef{
		ID: id, Identifier: identifier, Title: title,
		Mention: fmt.Sprintf("[%s](mention://issue/%s)", identifier, id),
	}
}

func (s *DigestService) computeWorkspace(ctx context.Context, workspaceID pgtype.UUID, since pgtype.Timestamptz, sc digestScope) (DigestWorkspace, error) {
	ws := DigestWorkspace{
		OfflineRuntimes: DigestRuntimeList{Items: []DigestRuntimeRef{}},
		AgentsFailed:    DigestAgentList{Items: []DigestAgentRef{}},
		UnseatedSquads:  DigestSquadList{Items: []DigestSquadRef{}},
	}

	if sc.floor {
		runs, err := s.Queries.DigestFloorRuns(ctx, db.DigestFloorRunsParams{SquadIds: sc.squadIDs, Since: since})
		if err != nil {
			return ws, fmt.Errorf("digest floor runs: %w", err)
		}
		ws.Runs, ws.FailedRuns = int(runs.Runs), int(runs.FailedRuns)
	} else {
		runs, err := s.Queries.DigestWorkspaceRuns(ctx, db.DigestWorkspaceRunsParams{WorkspaceID: workspaceID, Since: since})
		if err != nil {
			return ws, fmt.Errorf("digest workspace runs: %w", err)
		}
		ws.Runs, ws.FailedRuns = int(runs.Runs), int(runs.FailedRuns)

		usage, err := s.Queries.DigestWorkspaceUsage(ctx, db.DigestWorkspaceUsageParams{WorkspaceID: workspaceID, Since: since})
		if err != nil {
			return ws, fmt.Errorf("digest workspace usage: %w", err)
		}
		ws.InputTokens, ws.OutputTokens = usage.InputTokens, usage.OutputTokens
		// Ticks are 1e-10 USD.
		ws.ReportedCostUSD = float64(usage.CostUsdTicks) / 1e10
	}

	runtimes, err := s.Queries.DigestOfflineRuntimes(ctx, db.DigestOfflineRuntimesParams{WorkspaceID: workspaceID, RowLimit: DigestWorkspaceListLimit})
	if err != nil {
		return ws, fmt.Errorf("digest offline runtimes: %w", err)
	}
	for _, r := range runtimes {
		ws.OfflineRuntimes.Count = int(r.TotalCount)
		ws.OfflineRuntimes.Items = append(ws.OfflineRuntimes.Items, DigestRuntimeRef{ID: util.UUIDToString(r.ID), Name: r.Name, Provider: r.Provider})
	}

	if sc.floor {
		agents, err := s.Queries.DigestFloorAgentsWithFailedRuns(ctx, db.DigestFloorAgentsWithFailedRunsParams{
			WorkspaceID: workspaceID, SquadIds: sc.squadIDs, Since: since, RowLimit: DigestWorkspaceListLimit,
		})
		if err != nil {
			return ws, fmt.Errorf("digest floor agents with failed runs: %w", err)
		}
		for _, a := range agents {
			ws.AgentsFailed.Count = int(a.TotalCount)
			ws.AgentsFailed.Items = append(ws.AgentsFailed.Items, DigestAgentRef{ID: util.UUIDToString(a.ID), Name: a.Name, FailedRuns: int(a.FailedRuns)})
		}
		// A squad on a floor has a seat, so the floor has no unseated squads.
		return ws, nil
	}

	agents, err := s.Queries.DigestAgentsWithFailedRuns(ctx, db.DigestAgentsWithFailedRunsParams{WorkspaceID: workspaceID, Since: since, RowLimit: DigestWorkspaceListLimit})
	if err != nil {
		return ws, fmt.Errorf("digest agents with failed runs: %w", err)
	}
	for _, a := range agents {
		ws.AgentsFailed.Count = int(a.TotalCount)
		ws.AgentsFailed.Items = append(ws.AgentsFailed.Items, DigestAgentRef{ID: util.UUIDToString(a.ID), Name: a.Name, FailedRuns: int(a.FailedRuns)})
	}

	unseated, err := s.Queries.DigestUnseatedSquads(ctx, db.DigestUnseatedSquadsParams{WorkspaceID: workspaceID, RowLimit: DigestWorkspaceListLimit})
	if err != nil {
		return ws, fmt.Errorf("digest unseated squads: %w", err)
	}
	for _, sq := range unseated {
		ws.UnseatedSquads.Count = int(sq.TotalCount)
		ws.UnseatedSquads.Items = append(ws.UnseatedSquads.Items, DigestSquadRef{ID: util.UUIDToString(sq.ID), Name: sq.Name})
	}
	return ws, nil
}

// DigestPeople is the brief's People section.
type DigestPeople struct {
	Count   int               `json:"count"`
	Reviews []DigestReviewRef `json:"reviews"`
}

// DigestReviewRef is one review in the People section: its subject, rating and
// period, and each recommendation with the proposal filed for it, if any.
type DigestReviewRef struct {
	ID              string                 `json:"id"`
	SubjectType     string                 `json:"subject_type"`
	SubjectID       string                 `json:"subject_id"`
	Rating          string                 `json:"rating"`
	PeriodStart     string                 `json:"period_start"`
	PeriodEnd       string                 `json:"period_end"`
	Recommendations []ReviewRecommendation `json:"recommendations"`
}

// DigestReviewsLimit bounds the reviews the People section lists.
const DigestReviewsLimit = 10

// computePeople lists the reviews published since the window opened, with their
// recommendations as stored.
func (s *DigestService) computePeople(ctx context.Context, workspaceID pgtype.UUID, since pgtype.Timestamptz) (DigestPeople, error) {
	rows, err := s.Queries.ListReviews(ctx, db.ListReviewsParams{
		WorkspaceID: workspaceID,
		Since:       since,
		RowLimit:    DigestReviewsLimit,
	})
	if err != nil {
		return DigestPeople{}, fmt.Errorf("digest reviews: %w", err)
	}
	out := DigestPeople{Count: len(rows), Reviews: make([]DigestReviewRef, 0, len(rows))}
	for _, r := range rows {
		recs := []ReviewRecommendation{}
		if len(r.Recommendations) > 0 {
			if err := json.Unmarshal(r.Recommendations, &recs); err != nil {
				return DigestPeople{}, fmt.Errorf("digest review recommendations: %w", err)
			}
		}
		out.Reviews = append(out.Reviews, DigestReviewRef{
			ID:              util.UUIDToString(r.ID),
			SubjectType:     r.SubjectType,
			SubjectID:       util.UUIDToString(r.SubjectID),
			Rating:          r.Rating,
			PeriodStart:     r.PeriodStart.Time.Format("2006-01-02"),
			PeriodEnd:       r.PeriodEnd.Time.Format("2006-01-02"),
			Recommendations: recs,
		})
	}
	return out, nil
}
