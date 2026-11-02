package service

import (
	"context"
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
}

// Digest is the whole response of GET /api/office/digest.
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
}

// DigestService computes the digest.
type DigestService struct {
	Queries *db.Queries
	// Now is the clock; nil means time.Now. Tests pin it.
	Now func() time.Time
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

// Compute builds the digest for the workspace since the given time.
// issuePrefix is the workspace's issue prefix, for identifiers and mentions.
func (s *DigestService) Compute(ctx context.Context, workspaceID pgtype.UUID, since time.Time, issuePrefix string) (Digest, error) {
	now := s.now()
	sinceTS := pgtype.Timestamptz{Time: since, Valid: true}

	rows, err := s.Queries.DigestSquadStats(ctx, db.DigestSquadStatsParams{
		WorkspaceID: workspaceID,
		Since:       sinceTS,
		StaleBefore: pgtype.Timestamptz{Time: now.Add(-DigestStaleAfter), Valid: true},
	})
	if err != nil {
		return Digest{}, fmt.Errorf("digest squad stats: %w", err)
	}
	all := make([]DigestSquad, 0, len(rows))
	for _, r := range rows {
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

	ws, err := s.computeWorkspace(ctx, workspaceID, sinceTS)
	if err != nil {
		return Digest{}, err
	}
	ws.RoutinesNotOnTime = offTrack
	know, err := s.computeKnowledge(ctx, workspaceID, now)
	if err != nil {
		return Digest{}, err
	}

	return Digest{
		Since:             since.UTC().Format(time.RFC3339),
		Until:             now.UTC().Format(time.RFC3339),
		Workspace:         ws,
		Squads:            squads,
		OmittedSquadCount: omitted,
		Quiet:             quiet,
		Knowledge:         know,
	}, nil
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
func (s *DigestService) computeKnowledge(ctx context.Context, workspaceID pgtype.UUID, now time.Time) (DigestKnowledge, error) {
	know := DigestKnowledge{StalePublished: DigestKnowledgeStaleList{Items: []DigestKnowledgeDocRef{}}}

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

	drafts, err := s.Queries.DigestKnowledgePendingDrafts(ctx, workspaceID)
	if err != nil {
		return know, fmt.Errorf("digest knowledge pending drafts: %w", err)
	}
	know.PendingDrafts = int(drafts)
	return know, nil
}

func digestIssueRef(prefix, id string, number int32, title string) DigestIssueRef {
	identifier := prefix + "-" + strconv.Itoa(int(number))
	return DigestIssueRef{
		ID: id, Identifier: identifier, Title: title,
		Mention: fmt.Sprintf("[%s](mention://issue/%s)", identifier, id),
	}
}

func (s *DigestService) computeWorkspace(ctx context.Context, workspaceID pgtype.UUID, since pgtype.Timestamptz) (DigestWorkspace, error) {
	ws := DigestWorkspace{
		OfflineRuntimes: DigestRuntimeList{Items: []DigestRuntimeRef{}},
		AgentsFailed:    DigestAgentList{Items: []DigestAgentRef{}},
		UnseatedSquads:  DigestSquadList{Items: []DigestSquadRef{}},
	}

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

	runtimes, err := s.Queries.DigestOfflineRuntimes(ctx, db.DigestOfflineRuntimesParams{WorkspaceID: workspaceID, RowLimit: DigestWorkspaceListLimit})
	if err != nil {
		return ws, fmt.Errorf("digest offline runtimes: %w", err)
	}
	for _, r := range runtimes {
		ws.OfflineRuntimes.Count = int(r.TotalCount)
		ws.OfflineRuntimes.Items = append(ws.OfflineRuntimes.Items, DigestRuntimeRef{ID: util.UUIDToString(r.ID), Name: r.Name, Provider: r.Provider})
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
