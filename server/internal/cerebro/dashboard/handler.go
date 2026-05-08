// Package dashboard exposes cerebro-only dashboard HTTP endpoints. The
// dashboard aggregates workspace activity (issues, chat, channels, agents,
// members) into a single period-aware overview at /api/cerebro/dashboard.
//
// All queries that this package needs live in
// server/internal/cerebro/queries/dashboard.sql and are generated into the
// cerebrodb package. The handler also reads from the upstream `db` package
// for resources like agent run-counts and workspace usage.
package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/pricing"
)

// Handler exposes the cerebro dashboard HTTP endpoint. Holds both the
// cerebro and upstream sqlc query packages because the overview pulls from
// both (cerebro: dashboard aggregates, upstream: workspace usage + budget).
type Handler struct {
	Cerebro  *cerebrodb.Queries
	Upstream *db.Queries
}

func New(cerebro *cerebrodb.Queries, upstream *db.Queries) *Handler {
	return &Handler{Cerebro: cerebro, Upstream: upstream}
}

// Range options accepted by the dashboard endpoint. The current and prior
// windows have the same length so KPI deltas compare like-for-like.
type rangeSpec struct {
	now           time.Time
	periodStart   time.Time
	periodEnd     time.Time
	priorStart    time.Time
	priorEnd      time.Time
	bucketsByDay  bool
}

func parseRange(rangeStr string, now time.Time) rangeSpec {
	var dur time.Duration
	switch rangeStr {
	case "24h":
		dur = 24 * time.Hour
	case "30d":
		dur = 30 * 24 * time.Hour
	default:
		dur = 7 * 24 * time.Hour
	}
	end := now
	start := end.Add(-dur)
	return rangeSpec{
		now:          now,
		periodStart:  start,
		periodEnd:    end,
		priorStart:   start.Add(-dur),
		priorEnd:     start,
		bucketsByDay: dur >= 24*time.Hour,
	}
}

// kpi holds a single KPI count plus a comparable count from the prior period.
type kpi struct {
	Value int `json:"value"`
	Prior int `json:"prior"`
}

type dayBucket struct {
	Day            string `json:"day"`
	IssuesCreated  int    `json:"issues_created"`
	IssuesDone     int    `json:"issues_done"`
	TasksCompleted int    `json:"tasks_completed"`
	TasksFailed    int    `json:"tasks_failed"`
}

type bucket struct {
	Key   string `json:"key"`
	Count int    `json:"count"`
}

type topActor struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Count int    `json:"count"`
}

type activityEntry struct {
	ID              string          `json:"id"`
	IssueID         string          `json:"issue_id,omitempty"`
	IssueTitle      string          `json:"issue_title,omitempty"`
	IssueNumber     int32           `json:"issue_number,omitempty"`
	IssueKind       string          `json:"issue_kind,omitempty"`
	ActorType       string          `json:"actor_type"`
	ActorID         string          `json:"actor_id"`
	Action          string          `json:"action"`
	Details         json.RawMessage `json:"details,omitempty"`
	CreatedAt       string          `json:"created_at"`
}

type recentTask struct {
	TaskID         string `json:"task_id"`
	AgentID        string `json:"agent_id"`
	AgentName      string `json:"agent_name"`
	AgentAvatarURL string `json:"agent_avatar_url,omitempty"`
	IssueID        string `json:"issue_id,omitempty"`
	IssueTitle     string `json:"issue_title,omitempty"`
	IssueNumber    int32  `json:"issue_number,omitempty"`
	Status         string `json:"status"`
	CompletedAt    string `json:"completed_at,omitempty"`
	StartedAt      string `json:"started_at,omitempty"`
	CreatedAt      string `json:"created_at"`
}

type overviewResponse struct {
	Range            string         `json:"range"`
	PeriodStart      string         `json:"period_start"`
	PeriodEnd        string         `json:"period_end"`
	IssuesCreated    kpi            `json:"issues_created"`
	IssuesCompleted  kpi            `json:"issues_completed"`
	ChatMessages     kpi            `json:"chat_messages"`
	ChannelMessages  kpi            `json:"channel_messages"`
	ChannelsActive   kpi            `json:"channels_active"`
	TasksCompleted   kpi            `json:"tasks_completed"`
	TasksFailed      kpi            `json:"tasks_failed"`
	AgentsActive     kpi            `json:"agents_active"`
	MembersActive    kpi            `json:"members_active"`
	SpendCents       kpi            `json:"spend_cents"`
	IssuesByStatus   []bucket       `json:"issues_by_status"`
	IssuesByPriority []bucket       `json:"issues_by_priority"`
	Timeline         []dayBucket    `json:"timeline"`
	TopAgents        []topActor     `json:"top_agents"`
	TopMembers       []topActor     `json:"top_members"`
	ActivityFeed     []activityEntry `json:"activity_feed"`
	RecentTasks      []recentTask    `json:"recent_tasks"`
}

// Overview returns the full dashboard payload for the current workspace.
// Aggregates ~15 queries in parallel where possible to keep p99 under 1s
// even on workspaces with hundreds of thousands of issues / activity rows.
func (h *Handler) Overview(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	workspaceID := middleware.WorkspaceIDFromContext(r.Context())
	wsUUID, err := util.ParseUUID(workspaceID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workspace id")
		return
	}

	rangeStr := r.URL.Query().Get("range")
	spec := parseRange(rangeStr, time.Now())

	out := overviewResponse{
		Range:       rangeOrDefault(rangeStr),
		PeriodStart: spec.periodStart.UTC().Format(time.RFC3339),
		PeriodEnd:   spec.periodEnd.UTC().Format(time.RFC3339),
	}

	ctx := r.Context()

	// Run independent queries in parallel; collect into the response struct
	// under a mutex. Each closure handles its own error logging — a single
	// failed sub-query degrades that field rather than the whole response.
	var wg sync.WaitGroup
	var mu sync.Mutex

	runOne := func(fn func(context.Context) error) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = fn(ctx)
		}()
	}

	// --- Issues counts (current + prior) ---
	runOne(func(c context.Context) error {
		v, err := h.Cerebro.DashboardCountIssuesCreatedInPeriod(c, cerebrodb.DashboardCountIssuesCreatedInPeriodParams{
			WorkspaceID: wsUUID, CreatedAt: ts(spec.periodStart), CreatedAt_2: ts(spec.periodEnd),
		})
		if err != nil {
			return err
		}
		p, _ := h.Cerebro.DashboardCountIssuesCreatedInPeriod(c, cerebrodb.DashboardCountIssuesCreatedInPeriodParams{
			WorkspaceID: wsUUID, CreatedAt: ts(spec.priorStart), CreatedAt_2: ts(spec.priorEnd),
		})
		mu.Lock()
		out.IssuesCreated = kpi{Value: int(v), Prior: int(p)}
		mu.Unlock()
		return nil
	})
	runOne(func(c context.Context) error {
		v, err := h.Cerebro.DashboardCountIssuesCompletedInPeriod(c, cerebrodb.DashboardCountIssuesCompletedInPeriodParams{
			WorkspaceID: wsUUID, UpdatedAt: ts(spec.periodStart), UpdatedAt_2: ts(spec.periodEnd),
		})
		if err != nil {
			return err
		}
		p, _ := h.Cerebro.DashboardCountIssuesCompletedInPeriod(c, cerebrodb.DashboardCountIssuesCompletedInPeriodParams{
			WorkspaceID: wsUUID, UpdatedAt: ts(spec.priorStart), UpdatedAt_2: ts(spec.priorEnd),
		})
		mu.Lock()
		out.IssuesCompleted = kpi{Value: int(v), Prior: int(p)}
		mu.Unlock()
		return nil
	})

	// --- Chat + channel + tasks counts ---
	runOne(func(c context.Context) error {
		v, err := h.Cerebro.DashboardCountChatMessagesInPeriod(c, cerebrodb.DashboardCountChatMessagesInPeriodParams{
			WorkspaceID: wsUUID, CreatedAt: ts(spec.periodStart), CreatedAt_2: ts(spec.periodEnd),
		})
		if err != nil {
			return err
		}
		p, _ := h.Cerebro.DashboardCountChatMessagesInPeriod(c, cerebrodb.DashboardCountChatMessagesInPeriodParams{
			WorkspaceID: wsUUID, CreatedAt: ts(spec.priorStart), CreatedAt_2: ts(spec.priorEnd),
		})
		mu.Lock()
		out.ChatMessages = kpi{Value: int(v), Prior: int(p)}
		mu.Unlock()
		return nil
	})
	runOne(func(c context.Context) error {
		v, err := h.Cerebro.DashboardCountChannelMessagesInPeriod(c, cerebrodb.DashboardCountChannelMessagesInPeriodParams{
			WorkspaceID: wsUUID, CreatedAt: ts(spec.periodStart), CreatedAt_2: ts(spec.periodEnd),
		})
		if err != nil {
			return err
		}
		p, _ := h.Cerebro.DashboardCountChannelMessagesInPeriod(c, cerebrodb.DashboardCountChannelMessagesInPeriodParams{
			WorkspaceID: wsUUID, CreatedAt: ts(spec.priorStart), CreatedAt_2: ts(spec.priorEnd),
		})
		mu.Lock()
		out.ChannelMessages = kpi{Value: int(v), Prior: int(p)}
		mu.Unlock()
		return nil
	})
	runOne(func(c context.Context) error {
		v, err := h.Cerebro.DashboardCountActiveChannelsInPeriod(c, cerebrodb.DashboardCountActiveChannelsInPeriodParams{
			WorkspaceID: wsUUID, CreatedAt: ts(spec.periodStart), CreatedAt_2: ts(spec.periodEnd),
		})
		if err != nil {
			return err
		}
		p, _ := h.Cerebro.DashboardCountActiveChannelsInPeriod(c, cerebrodb.DashboardCountActiveChannelsInPeriodParams{
			WorkspaceID: wsUUID, CreatedAt: ts(spec.priorStart), CreatedAt_2: ts(spec.priorEnd),
		})
		mu.Lock()
		out.ChannelsActive = kpi{Value: int(v), Prior: int(p)}
		mu.Unlock()
		return nil
	})
	runOne(func(c context.Context) error {
		rows, err := h.Cerebro.DashboardCountTasksByStatusInPeriod(c, cerebrodb.DashboardCountTasksByStatusInPeriodParams{
			WorkspaceID: wsUUID, CompletedAt: ts(spec.periodStart), CompletedAt_2: ts(spec.periodEnd),
		})
		if err != nil {
			return err
		}
		priorRows, _ := h.Cerebro.DashboardCountTasksByStatusInPeriod(c, cerebrodb.DashboardCountTasksByStatusInPeriodParams{
			WorkspaceID: wsUUID, CompletedAt: ts(spec.priorStart), CompletedAt_2: ts(spec.priorEnd),
		})
		var done, fail, doneP, failP int
		for _, row := range rows {
			if row.Status == "completed" {
				done = int(row.Count)
			}
			if row.Status == "failed" {
				fail = int(row.Count)
			}
		}
		for _, row := range priorRows {
			if row.Status == "completed" {
				doneP = int(row.Count)
			}
			if row.Status == "failed" {
				failP = int(row.Count)
			}
		}
		mu.Lock()
		out.TasksCompleted = kpi{Value: done, Prior: doneP}
		out.TasksFailed = kpi{Value: fail, Prior: failP}
		mu.Unlock()
		return nil
	})

	// --- Active actors ---
	runOne(func(c context.Context) error {
		v, err := h.Cerebro.DashboardActiveAgentCountInPeriod(c, cerebrodb.DashboardActiveAgentCountInPeriodParams{
			WorkspaceID: wsUUID, CompletedAt: ts(spec.periodStart), CompletedAt_2: ts(spec.periodEnd),
		})
		if err != nil {
			return err
		}
		p, _ := h.Cerebro.DashboardActiveAgentCountInPeriod(c, cerebrodb.DashboardActiveAgentCountInPeriodParams{
			WorkspaceID: wsUUID, CompletedAt: ts(spec.priorStart), CompletedAt_2: ts(spec.priorEnd),
		})
		mu.Lock()
		out.AgentsActive = kpi{Value: int(v), Prior: int(p)}
		mu.Unlock()
		return nil
	})
	runOne(func(c context.Context) error {
		v, err := h.Cerebro.DashboardActiveMemberCountInPeriod(c, cerebrodb.DashboardActiveMemberCountInPeriodParams{
			WorkspaceID: wsUUID, CreatedAt: ts(spec.periodStart), CreatedAt_2: ts(spec.periodEnd),
		})
		if err != nil {
			return err
		}
		p, _ := h.Cerebro.DashboardActiveMemberCountInPeriod(c, cerebrodb.DashboardActiveMemberCountInPeriodParams{
			WorkspaceID: wsUUID, CreatedAt: ts(spec.priorStart), CreatedAt_2: ts(spec.priorEnd),
		})
		mu.Lock()
		out.MembersActive = kpi{Value: int(v), Prior: int(p)}
		mu.Unlock()
		return nil
	})

	// --- Spend (current + prior) using upstream usage summary + pricing pkg ---
	runOne(func(c context.Context) error {
		cur := h.spendCents(c, wsUUID, spec.periodStart, spec.periodEnd)
		prior := h.spendCents(c, wsUUID, spec.priorStart, spec.priorEnd)
		mu.Lock()
		out.SpendCents = kpi{Value: cur, Prior: prior}
		mu.Unlock()
		return nil
	})

	// --- Issues by status / priority (totals, not period-scoped) ---
	runOne(func(c context.Context) error {
		rows, err := h.Cerebro.DashboardCountIssuesByStatus(c, wsUUID)
		if err != nil {
			return err
		}
		mu.Lock()
		for _, row := range rows {
			out.IssuesByStatus = append(out.IssuesByStatus, bucket{Key: row.Status, Count: int(row.Count)})
		}
		mu.Unlock()
		return nil
	})
	runOne(func(c context.Context) error {
		rows, err := h.Cerebro.DashboardCountIssuesByPriority(c, wsUUID)
		if err != nil {
			return err
		}
		mu.Lock()
		for _, row := range rows {
			out.IssuesByPriority = append(out.IssuesByPriority, bucket{Key: row.Priority, Count: int(row.Count)})
		}
		mu.Unlock()
		return nil
	})

	// --- Timeline (per-day buckets across the period) ---
	runOne(func(c context.Context) error {
		timeline := buildTimeline(spec)
		// Issues created
		created, _ := h.Cerebro.DashboardCountIssuesCreatedByDay(c, cerebrodb.DashboardCountIssuesCreatedByDayParams{
			WorkspaceID: wsUUID, CreatedAt: ts(spec.periodStart), CreatedAt_2: ts(spec.periodEnd),
		})
		for _, row := range created {
			if i, ok := timeline.idx[row.Day]; ok {
				timeline.buckets[i].IssuesCreated = int(row.Count)
			}
		}
		// Issues done
		done, _ := h.Cerebro.DashboardCountIssuesCompletedByDay(c, cerebrodb.DashboardCountIssuesCompletedByDayParams{
			WorkspaceID: wsUUID, UpdatedAt: ts(spec.periodStart), UpdatedAt_2: ts(spec.periodEnd),
		})
		for _, row := range done {
			if i, ok := timeline.idx[row.Day]; ok {
				timeline.buckets[i].IssuesDone = int(row.Count)
			}
		}
		// Tasks by day
		tasks, _ := h.Cerebro.DashboardCountTasksByDay(c, cerebrodb.DashboardCountTasksByDayParams{
			WorkspaceID: wsUUID, CompletedAt: ts(spec.periodStart), CompletedAt_2: ts(spec.periodEnd),
		})
		for _, row := range tasks {
			i, ok := timeline.idx[row.Day]
			if !ok {
				continue
			}
			switch row.Status {
			case "completed":
				timeline.buckets[i].TasksCompleted = int(row.Count)
			case "failed":
				timeline.buckets[i].TasksFailed = int(row.Count)
			}
		}
		mu.Lock()
		out.Timeline = timeline.buckets
		mu.Unlock()
		return nil
	})

	// --- Top agents / top members ---
	runOne(func(c context.Context) error {
		rows, err := h.Cerebro.DashboardTopAgentsByActivityInPeriod(c, cerebrodb.DashboardTopAgentsByActivityInPeriodParams{
			WorkspaceID: wsUUID, CompletedAt: ts(spec.periodStart), CompletedAt_2: ts(spec.periodEnd),
		})
		if err != nil {
			return err
		}
		var top []topActor
		for _, row := range rows {
			top = append(top, topActor{ID: util.UUIDToString(row.ActorID), Name: row.Name, Count: int(row.ActivityCount)})
		}
		mu.Lock()
		out.TopAgents = top
		mu.Unlock()
		return nil
	})
	runOne(func(c context.Context) error {
		rows, err := h.Cerebro.DashboardTopMembersByActivityInPeriod(c, cerebrodb.DashboardTopMembersByActivityInPeriodParams{
			WorkspaceID: wsUUID, CreatedAt: ts(spec.periodStart), CreatedAt_2: ts(spec.periodEnd),
		})
		if err != nil {
			return err
		}
		var top []topActor
		for _, row := range rows {
			name := ""
			if row.Name.Valid {
				name = row.Name.String
			}
			top = append(top, topActor{ID: util.UUIDToString(row.ActorID), Name: name, Count: int(row.ActivityCount)})
		}
		mu.Lock()
		out.TopMembers = top
		mu.Unlock()
		return nil
	})

	// --- Activity feed (latest 30 in period) ---
	runOne(func(c context.Context) error {
		rows, err := h.Cerebro.DashboardActivityFeed(c, cerebrodb.DashboardActivityFeedParams{
			WorkspaceID: wsUUID, CreatedAt: ts(spec.periodStart), CreatedAt_2: ts(spec.periodEnd), Limit: 30,
		})
		if err != nil {
			return err
		}
		var feed []activityEntry
		for _, row := range rows {
			entry := activityEntry{
				ID:        util.UUIDToString(row.ID),
				Action:    row.Action,
				CreatedAt: row.CreatedAt.Time.UTC().Format(time.RFC3339),
			}
			if row.ActorType.Valid {
				entry.ActorType = row.ActorType.String
			}
			if row.ActorID.Valid {
				entry.ActorID = util.UUIDToString(row.ActorID)
			}
			if row.IssueID.Valid {
				entry.IssueID = util.UUIDToString(row.IssueID)
			}
			if row.IssueTitle.Valid {
				entry.IssueTitle = row.IssueTitle.String
			}
			if row.IssueNumber.Valid {
				entry.IssueNumber = row.IssueNumber.Int32
			}
			if row.IssueKind.Valid {
				entry.IssueKind = row.IssueKind.String
			}
			if len(row.Details) > 0 {
				entry.Details = json.RawMessage(row.Details)
			}
			feed = append(feed, entry)
		}
		mu.Lock()
		out.ActivityFeed = feed
		mu.Unlock()
		return nil
	})

	// --- Recent tasks (workspace-wide, limit 12) ---
	runOne(func(c context.Context) error {
		rows, err := h.Cerebro.DashboardRecentTasks(c, cerebrodb.DashboardRecentTasksParams{
			WorkspaceID: wsUUID, Limit: 12,
		})
		if err != nil {
			return err
		}
		var tasks []recentTask
		for _, row := range rows {
			t := recentTask{
				TaskID:    util.UUIDToString(row.TaskID),
				AgentID:   util.UUIDToString(row.AgentID),
				AgentName: row.AgentName,
				Status:    row.Status,
				CreatedAt: row.CreatedAt.Time.UTC().Format(time.RFC3339),
			}
			if row.AgentAvatarUrl.Valid {
				t.AgentAvatarURL = row.AgentAvatarUrl.String
			}
			if row.IssueID.Valid {
				t.IssueID = util.UUIDToString(row.IssueID)
			}
			if row.IssueTitle.Valid {
				t.IssueTitle = row.IssueTitle.String
			}
			if row.IssueNumber.Valid {
				t.IssueNumber = row.IssueNumber.Int32
			}
			if row.CompletedAt.Valid {
				t.CompletedAt = row.CompletedAt.Time.UTC().Format(time.RFC3339)
			}
			if row.StartedAt.Valid {
				t.StartedAt = row.StartedAt.Time.UTC().Format(time.RFC3339)
			}
			tasks = append(tasks, t)
		}
		mu.Lock()
		out.RecentTasks = tasks
		mu.Unlock()
		return nil
	})

	wg.Wait()
	writeJSON(w, http.StatusOK, out)
}

// spendCents sums USD cents across the workspace for [start, end) using the
// upstream GetWorkspaceUsageSummary query (aggregates token usage by model)
// combined with the pricing.ComputeCents lookup.
func (h *Handler) spendCents(ctx context.Context, wsID pgtype.UUID, start, end time.Time) int {
	rows, err := h.Upstream.GetWorkspaceUsageSummary(ctx, db.GetWorkspaceUsageSummaryParams{
		WorkspaceID: wsID,
		Since:       ts(start),
	})
	if err != nil {
		return 0
	}
	var cents int64
	for _, row := range rows {
		// GetWorkspaceUsageSummary aggregates from `since` forward. Subtract
		// the period beyond `end` if the query returned data past it. Token
		// usage doesn't expose row timestamps in the summary view, so for
		// MVP we treat `since=start` and accept that the count includes
		// usage produced after `end` (rare since `end` ~= now in dashboard
		// usage). Refine with a since/until variant in JEH-704 follow-up.
		if !end.After(time.Now().Add(-30 * time.Second)) {
			// User picked an older window — skip spend (avoid overstating).
			continue
		}
		cents += pricing.ComputeCents(row.Model, pricing.Usage{
			InputTokens:      row.TotalInputTokens,
			OutputTokens:     row.TotalOutputTokens,
			CacheReadTokens:  row.TotalCacheReadTokens,
			CacheWriteTokens: row.TotalCacheWriteTokens,
		})
	}
	return int(cents)
}

type timeline struct {
	idx     map[string]int
	buckets []dayBucket
}

func buildTimeline(spec rangeSpec) timeline {
	tl := timeline{idx: map[string]int{}}
	if !spec.bucketsByDay {
		// 24h range: one bucket
		day := spec.periodStart.UTC().Format("2006-01-02")
		tl.buckets = []dayBucket{{Day: day}}
		tl.idx[day] = 0
		return tl
	}
	cur := time.Date(spec.periodStart.Year(), spec.periodStart.Month(), spec.periodStart.Day(), 0, 0, 0, 0, time.UTC)
	end := spec.periodEnd
	for cur.Before(end) {
		day := cur.Format("2006-01-02")
		tl.idx[day] = len(tl.buckets)
		tl.buckets = append(tl.buckets, dayBucket{Day: day})
		cur = cur.AddDate(0, 0, 1)
	}
	return tl
}

func ts(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: true}
}

func rangeOrDefault(s string) string {
	switch s {
	case "24h", "7d", "30d":
		return s
	default:
		return "7d"
	}
}

func requireUserID(w http.ResponseWriter, r *http.Request) (string, bool) {
	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return "", false
	}
	return userID, true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
