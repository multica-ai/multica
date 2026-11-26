package handler

import (
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/projectaccess"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Per-member usage on the workspace dashboard.
//
//   GET /api/dashboard/usage/by-member     per-member token usage + run count
//   GET /api/dashboard/usage/member-tasks  one member's most recent runs
//
// "Member" is the run's accountable human (agent_task_queue.accountable_user_id),
// so a shared agent's spend splits across the people it worked for. Both
// endpoints take the same ?days= / ?project_id= / ?squad_id= / ?tz= as
// usage/by-agent, on its exact-N window and under the plan's history limit
// (dashboardCutoff with trimDays 1), so the member rows add up to the Cost and
// Tokens KPIs. They read raw task_usage, which has no rollup lag: within a few
// minutes of new usage the member rows can run ahead of the hourly-rollup
// totals beside them, never behind.
//
// Access control:
//   - owner/admin see every member, plus the unattributed bucket;
//   - anyone else sees only their own row. The restriction is applied in SQL
//     (only_user_id), and member-tasks for anyone else, or for the unattributed
//     bucket, is a 403;
//   - by-member has no agent dimension, so it names no agent; member-tasks
//     names each run's agent and folds the ones the caller may not see onto
//     restrictedAgentsRowID, like the per-agent rollups;
//   - chat sessions are creator-only (admins included), so a chat run's title
//     and session id are only returned to the chat's creator;
//   - private projects (MUL-8472): a project_id the caller cannot see is
//     answered like a missing project, and member-tasks keeps the usage of a
//     run about a project the caller cannot see but drops its issue.

// unattributedMemberID marks usage whose run has no accountable human. Not a
// UUID, so it can never collide with a real user id.
const unattributedMemberID = "__unattributed__"

// memberTaskLimit caps member-tasks; the panel lists recent runs, not history.
const memberTaskLimit = 20

// DashboardMemberUsage is one (provider, model) usage aggregate, with the same
// authoritative/uncosted cost split as DashboardUsageByAgentResponse.
type DashboardMemberUsage struct {
	Provider                 string `json:"provider"`
	Model                    string `json:"model"`
	InputTokens              int64  `json:"input_tokens"`
	OutputTokens             int64  `json:"output_tokens"`
	CacheReadTokens          int64  `json:"cache_read_tokens"`
	CacheWriteTokens         int64  `json:"cache_write_tokens"`
	CostUSDTicks             int64  `json:"cost_usd_ticks"`
	UncostedInputTokens      int64  `json:"uncosted_input_tokens"`
	UncostedOutputTokens     int64  `json:"uncosted_output_tokens"`
	UncostedCacheReadTokens  int64  `json:"uncosted_cache_read_tokens"`
	UncostedCacheWriteTokens int64  `json:"uncosted_cache_write_tokens"`
}

// DashboardUsageByMemberResponse is one member's usage in the window. UserID
// is unattributedMemberID for runs nobody is accountable for. TaskCount is the
// number of distinct runs with usage; a run that used two models counts once.
type DashboardUsageByMemberResponse struct {
	UserID    string                 `json:"user_id"`
	TaskCount int32                  `json:"task_count"`
	Usage     []DashboardMemberUsage `json:"usage"`
}

func memberIDOf(u pgtype.UUID) string {
	if !u.Valid {
		return unattributedMemberID
	}
	return uuidToString(u)
}

// dashboardMemberScope returns the only_user_id filter for the caller: NULL
// (everyone) for owner/admin, the caller's own user id for anyone else.
func dashboardMemberScope(member db.Member) (onlyUser pgtype.UUID, privileged bool) {
	if roleAllowed(member.Role, "owner", "admin") {
		return pgtype.UUID{}, true
	}
	return member.UserID, false
}

// GetDashboardUsageByMember returns per-member token aggregates and run counts.
func (h *Handler) GetDashboardUsageByMember(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	projectID, ok := parseProjectIDParam(w, r)
	if !ok {
		return
	}
	squadScope, squadID, ok := parseSquadScopeParam(w, r)
	if !ok {
		return
	}
	if !h.dashboardProjectVisible(w, r, workspaceID, projectID) {
		return
	}
	tz := h.resolveViewingTZ(r)
	window := h.dashboardCutoff(w, r, workspaceID, tz, 1)
	sinceHour := usageHourCutoff(window.since)
	onlyUser, _ := dashboardMemberScope(member)
	wsUUID := parseUUID(workspaceID)

	usageRows, err := h.Queries.ListDashboardUsageByMember(r.Context(), db.ListDashboardUsageByMemberParams{
		WorkspaceID: wsUUID,
		SinceHour:   sinceHour,
		ProjectID:   projectID,
		SquadScope:  squadScope,
		SquadID:     squadID,
		OnlyUserID:  onlyUser,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list usage by member")
		return
	}
	countRows, err := h.Queries.ListDashboardTaskCountByMember(r.Context(), db.ListDashboardTaskCountByMemberParams{
		WorkspaceID: wsUUID,
		SinceHour:   sinceHour,
		ProjectID:   projectID,
		SquadScope:  squadScope,
		SquadID:     squadID,
		OnlyUserID:  onlyUser,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list usage by member")
		return
	}

	resp := make([]DashboardUsageByMemberResponse, 0)
	index := make(map[string]int)
	rowFor := func(userID string) *DashboardUsageByMemberResponse {
		i, seen := index[userID]
		if !seen {
			i = len(resp)
			index[userID] = i
			resp = append(resp, DashboardUsageByMemberResponse{UserID: userID, Usage: []DashboardMemberUsage{}})
		}
		return &resp[i]
	}
	for _, row := range usageRows {
		m := rowFor(memberIDOf(row.AccountableUserID))
		m.Usage = append(m.Usage, DashboardMemberUsage{
			Provider:                 row.Provider,
			Model:                    row.Model,
			InputTokens:              row.InputTokens,
			OutputTokens:             row.OutputTokens,
			CacheReadTokens:          row.CacheReadTokens,
			CacheWriteTokens:         row.CacheWriteTokens,
			CostUSDTicks:             row.CostUsdTicks,
			UncostedInputTokens:      row.UncostedInputTokens,
			UncostedOutputTokens:     row.UncostedOutputTokens,
			UncostedCacheReadTokens:  row.UncostedCacheReadTokens,
			UncostedCacheWriteTokens: row.UncostedCacheWriteTokens,
		})
	}
	for _, row := range countRows {
		rowFor(memberIDOf(row.AccountableUserID)).TaskCount = row.TaskCount
	}
	writeJSON(w, http.StatusOK, resp)
}

// DashboardMemberTask is one recent run. Kind is "issue", "chat" or "other".
// The issue fields are set for an issue run the caller may see; ChatSessionID
// and ChatTitle only for the chat's own creator.
type DashboardMemberTask struct {
	TaskID          string                 `json:"task_id"`
	AgentID         string                 `json:"agent_id"`
	Status          string                 `json:"status"`
	Kind            string                 `json:"kind"`
	IssueID         *string                `json:"issue_id"`
	IssueIdentifier *string                `json:"issue_identifier"`
	IssueTitle      *string                `json:"issue_title"`
	ChatSessionID   *string                `json:"chat_session_id"`
	ChatTitle       *string                `json:"chat_title"`
	LastUsedAt      string                 `json:"last_used_at"`
	Usage           []DashboardMemberUsage `json:"usage"`
}

// GetDashboardMemberTasks returns one member's most recent runs with usage in
// the window. ?user_id= is a user id or unattributedMemberID.
func (h *Handler) GetDashboardMemberTasks(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	projectID, ok := parseProjectIDParam(w, r)
	if !ok {
		return
	}
	squadScope, squadID, ok := parseSquadScopeParam(w, r)
	if !ok {
		return
	}
	rawUser := r.URL.Query().Get("user_id")
	if rawUser == "" {
		writeError(w, http.StatusBadRequest, "user_id is required")
		return
	}
	unattributed := rawUser == unattributedMemberID
	var userID pgtype.UUID
	if !unattributed {
		if userID, ok = parseUUIDOrBadRequest(w, rawUser, "user_id"); !ok {
			return
		}
	}
	// Anyone below admin may only read their own runs: never someone else's,
	// and never the unattributed bucket, which is everyone's leftovers.
	if self, privileged := dashboardMemberScope(member); !privileged && (unattributed || userID != self) {
		writeError(w, http.StatusForbidden, "you can only view your own usage")
		return
	}

	// One lookup serves both private-project rules: the project_id filter is
	// answered like dashboardProjectVisible, and the same set decides which
	// runs lose their issue below.
	hidden, ok := h.requestHiddenProjects(w, r, workspaceID)
	if !ok {
		return
	}
	if !hidden.CanSeeProject(projectID) {
		writeJSON(w, http.StatusOK, []struct{}{})
		return
	}
	h.writeDashboardTasks(w, r, workspaceID, member, hidden, db.ListDashboardMemberTaskUsageParams{
		ProjectID:    projectID,
		SquadScope:   squadScope,
		SquadID:      squadID,
		Unattributed: unattributed,
		UserID:       userID,
	})
}

// writeDashboardTasks lists the most recent runs matching params (member-tasks
// and project-tasks) and applies the per-run rules: agents the caller may not
// see fold onto restrictedAgentsRowID, a run about a private project the
// caller cannot see loses its issue, and a chat is named only to its creator.
// It fills in the workspace, window and limit itself.
func (h *Handler) writeDashboardTasks(
	w http.ResponseWriter,
	r *http.Request,
	workspaceID string,
	member db.Member,
	hidden projectaccess.Hidden,
	params db.ListDashboardMemberTaskUsageParams,
) {
	restricted, ok := h.dashboardRestrictedAgents(w, r, workspaceID, member.Role)
	if !ok {
		return
	}
	tz := h.resolveViewingTZ(r)
	window := h.dashboardCutoff(w, r, workspaceID, tz, 1)

	params.WorkspaceID = parseUUID(workspaceID)
	params.SinceHour = usageHourCutoff(window.since)
	params.TaskLimit = memberTaskLimit
	rows, err := h.Queries.ListDashboardMemberTaskUsage(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list recent runs")
		return
	}
	hiddenTasks, err := dashboardHiddenProjectTasks(r, h.Queries, hidden, rows)
	if err != nil {
		if writeQueryCancellation(w, r, err) {
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to resolve project access")
		return
	}

	var issuePrefix string
	prefixLoaded := false
	resp := make([]DashboardMemberTask, 0)
	index := make(map[pgtype.UUID]int)
	for _, row := range rows {
		i, seen := index[row.TaskID]
		if !seen {
			task := DashboardMemberTask{
				TaskID:     uuidToString(row.TaskID),
				AgentID:    uuidToString(row.AgentID),
				Status:     row.Status,
				Kind:       "other",
				LastUsedAt: timestampToString(row.LastUsedAt),
				Usage:      []DashboardMemberUsage{},
			}
			if _, hide := restricted[task.AgentID]; hide {
				task.AgentID = restrictedAgentsRowID
			}
			switch {
			case hiddenTasks[row.TaskID]:
				// About a private project the caller cannot see: the run keeps
				// its usage, so the member's total does not shrink, but not
				// what it was about.
			case row.IssueID.Valid:
				task.Kind = "issue"
				task.IssueID = uuidToPtr(row.IssueID)
				if !prefixLoaded {
					issuePrefix = h.getIssuePrefix(r.Context(), parseUUID(workspaceID))
					prefixLoaded = true
				}
				task.IssueIdentifier = inboxIssueIdentifier(issuePrefix, row.IssueNumber)
				if row.IssueTitle.Valid {
					title := row.IssueTitle.String
					task.IssueTitle = &title
				}
			case row.ChatSessionID.Valid:
				task.Kind = "chat"
				if row.ChatCreatorID.Valid && row.ChatCreatorID == member.UserID {
					task.ChatSessionID = uuidToPtr(row.ChatSessionID)
					if row.ChatTitle.Valid {
						title := row.ChatTitle.String
						task.ChatTitle = &title
					}
				}
			}
			i = len(resp)
			index[row.TaskID] = i
			resp = append(resp, task)
		}
		resp[i].Usage = append(resp[i].Usage, DashboardMemberUsage{
			Provider:                 row.Provider,
			Model:                    row.Model,
			InputTokens:              row.InputTokens,
			OutputTokens:             row.OutputTokens,
			CacheReadTokens:          row.CacheReadTokens,
			CacheWriteTokens:         row.CacheWriteTokens,
			CostUSDTicks:             row.CostUsdTicks,
			UncostedInputTokens:      row.UncostedInputTokens,
			UncostedOutputTokens:     row.UncostedOutputTokens,
			UncostedCacheReadTokens:  row.UncostedCacheReadTokens,
			UncostedCacheWriteTokens: row.UncostedCacheWriteTokens,
		})
	}
	writeJSON(w, http.StatusOK, resp)
}

// dashboardHiddenProjectTasks returns the runs in rows that are about a
// private project the caller cannot see, by the rule requireTaskVisible uses
// (projectaccess.TasksProjects: the run's issue, autopilot run or quick-create
// target). The rows carry every field TasksProjects reads, so no run is read
// again. Callers that see every project skip the lookups.
func dashboardHiddenProjectTasks(
	r *http.Request,
	q projectaccess.TaskQuerier,
	hidden projectaccess.Hidden,
	rows []db.ListDashboardMemberTaskUsageRow,
) (map[pgtype.UUID]bool, error) {
	if hidden.Empty() {
		return nil, nil
	}
	seen := make(map[pgtype.UUID]struct{})
	tasks := make([]db.AgentTaskQueue, 0, len(rows))
	for _, row := range rows {
		if _, dup := seen[row.TaskID]; dup {
			continue
		}
		seen[row.TaskID] = struct{}{}
		tasks = append(tasks, db.AgentTaskQueue{
			ID:             row.TaskID,
			IssueID:        row.IssueID,
			ChatSessionID:  row.ChatSessionID,
			AutopilotRunID: row.AutopilotRunID,
			Context:        row.Context,
		})
	}
	projects, err := projectaccess.TasksProjects(r.Context(), q, tasks)
	if err != nil {
		return nil, err
	}
	out := make(map[pgtype.UUID]bool)
	for i, task := range tasks {
		if !hidden.CanSeeProjects(projects[i]) {
			out[task.ID] = true
		}
	}
	return out, nil
}
