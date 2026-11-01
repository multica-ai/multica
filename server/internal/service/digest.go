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
}

// Digest is the whole response of GET /api/office/digest.
type Digest struct {
	Since     string          `json:"since"`
	Until     string          `json:"until"`
	Workspace DigestWorkspace `json:"workspace"`
	// Squads are ordered by what needs attention first, at most DigestMaxSquads.
	Squads []DigestSquad `json:"squads"`
	// OmittedSquadCount is how many squads with news were ranked below the cut.
	OmittedSquadCount int         `json:"omitted_squad_count"`
	Quiet             DigestQuiet `json:"quiet"`
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
// its single line.
func (q DigestSquad) quiet() bool {
	return q.Done == 0 && q.Blocked == 0 && q.Stale == 0 && q.FailedRuns == 0 &&
		q.LeaderNoAction == 0 && q.PendingProposalCount == 0
}

// attentionLess orders squads by what needs the boss first: pending proposals
// (only the boss can clear them), then blocked work, failed runs, stale work,
// leaders that did nothing, then plain activity. Ties break on name and id so
// the order is stable.
func attentionLess(a, b DigestSquad) bool {
	keys := [][2]int{
		{a.PendingProposalCount, b.PendingProposalCount},
		{a.Blocked, b.Blocked},
		{a.FailedRuns, b.FailedRuns},
		{a.Stale, b.Stale},
		{a.LeaderNoAction, b.LeaderNoAction},
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

	return Digest{
		Since:             since.UTC().Format(time.RFC3339),
		Until:             now.UTC().Format(time.RFC3339),
		Workspace:         ws,
		Squads:            squads,
		OmittedSquadCount: omitted,
		Quiet:             quiet,
	}, nil
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
