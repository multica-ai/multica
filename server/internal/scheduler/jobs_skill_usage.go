package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/pkg/agent"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const JobNameSkillUsageProcessor = "skill_usage_processor"

const skillUsageBatchSize = 100

// SkillUsageProcessorJob returns the JobSpec that drives async skill usage
// statistics. Every 2 minutes it scans newly-completed tasks for
// tool_use/read messages whose path matches a workspace skill's SKILL.md,
// then upserts a skill_usage_event row per (skill, task) pair.
//
//	cadence:               2m
//	catch_up_mode:         latest_only
//	run_timeout:           10m
//	stale_timeout:         15m
//	heartbeat_interval:    30s
//	max_attempts:          3
//	allow_stale_reentry:   true
func SkillUsageProcessorJob(pool *pgxpool.Pool, queries *db.Queries) JobSpec {
	return JobSpec{
		Name:              JobNameSkillUsageProcessor,
		Cadence:           2 * time.Minute,
		ScheduleDelay:     2 * time.Minute,
		CatchUpMode:       CatchUpLatestOnly,
		CatchUpWindow:     24 * time.Hour,
		RunTimeout:        10 * time.Minute,
		StaleTimeout:      15 * time.Minute,
		HeartbeatInterval: 30 * time.Second,
		AllowStaleReentry: true,
		MaxAttempts:       3,
		RetryBackoff: []time.Duration{
			1 * time.Minute,
			5 * time.Minute,
		},
		Scopes:  StaticScopes(ScopeGlobal),
		Handler: makeSkillUsageHandler(pool, queries),
	}
}

func makeSkillUsageHandler(pool *pgxpool.Pool, queries *db.Queries) Handler {
	return func(ctx context.Context, in HandlerInput) (HandlerResult, error) {
		// Read cursor (last_completed_at timestamp). NULL = first run.
		var lastCompletedAt pgtype.Timestamptz
		if err := pool.QueryRow(ctx, `SELECT last_completed_at FROM skill_usage_process_cursor WHERE id = 1`).Scan(&lastCompletedAt); err != nil {
			return HandlerResult{}, fmt.Errorf("read skill usage cursor: %w", err)
		}
		if !lastCompletedAt.Valid {
			// Use epoch so completed_at > 'epoch' matches all completed tasks.
			lastCompletedAt = pgtype.Timestamptz{Time: time.Unix(0, 0), Valid: true}
		}

		var totalEvents int64
		workspaceSkillCache := make(map[[16]byte][]db.Skill)

		for {
			// Use raw SQL with completed_at ordering — UUID v4 is random, so
			// "WHERE id > $1 ORDER BY id" would skip tasks whose UUID sorts
			// before the cursor even though they were created later. Completed_at
			// also correctly handles long-running tasks that finish after a
			// shorter task that was created later (created_at ordering would
			// skip them).
			rows, err := pool.Query(ctx, `
				SELECT atq.id, atq.runtime_id, atq.issue_id, ar.workspace_id, ar.provider
				FROM agent_task_queue atq
				JOIN agent_runtime ar ON atq.runtime_id = ar.id
				WHERE atq.status IN ('completed', 'failed')
				  AND atq.completed_at > $1
				ORDER BY atq.completed_at, atq.id
				LIMIT $2
			`, lastCompletedAt, skillUsageBatchSize)
			if err != nil {
				return HandlerResult{}, fmt.Errorf("list unprocessed tasks: %w", err)
			}

			var tasks []db.ListUnprocessedCompletedTasksRow
			for rows.Next() {
				var t db.ListUnprocessedCompletedTasksRow
				if err := rows.Scan(&t.ID, &t.RuntimeID, &t.IssueID, &t.WorkspaceID, &t.Provider); err != nil {
					rows.Close()
					return HandlerResult{}, fmt.Errorf("scan task: %w", err)
				}
				tasks = append(tasks, t)
			}
			rows.Close()
			if len(tasks) == 0 {
				break
			}

			for _, task := range tasks {
				events, err := processTaskSkillUsage(ctx, queries, task, workspaceSkillCache)
				if err != nil {
					slog.Warn("skill_usage: failed to process task",
						"task_id", task.ID, "error", err)
				}
				totalEvents += events
			}

			// Update cursor to the last task's completed_at for the next batch.
			lastTask := tasks[len(tasks)-1]
			var completedAt pgtype.Timestamptz
			if err := pool.QueryRow(ctx, `SELECT completed_at FROM agent_task_queue WHERE id = $1`, lastTask.ID).Scan(&completedAt); err != nil {
				return HandlerResult{}, fmt.Errorf("read task completed_at: %w", err)
			}
			if completedAt.Valid {
				lastCompletedAt = completedAt
			} else {
				// Fallback: should not happen for completed/failed tasks.
				slog.Warn("skill_usage: task has NULL completed_at, using created_at as fallback",
					"task_id", lastTask.ID)
				var createdAt pgtype.Timestamptz
				if err := pool.QueryRow(ctx, `SELECT created_at FROM agent_task_queue WHERE id = $1`, lastTask.ID).Scan(&createdAt); err != nil {
					return HandlerResult{}, fmt.Errorf("read task created_at: %w", err)
				}
				if createdAt.Valid {
					lastCompletedAt = createdAt
				}
			}

			if in.Heartbeat != nil {
				_ = in.Heartbeat(ctx)
			}

			if len(tasks) < skillUsageBatchSize {
				break
			}
		}

		if _, err := pool.Exec(ctx, `UPDATE skill_usage_process_cursor SET last_completed_at = $1, updated_at = now() WHERE id = 1`, lastCompletedAt); err != nil {
			return HandlerResult{}, fmt.Errorf("update skill usage cursor: %w", err)
		}

		return HandlerResult{
			RowsAffected: totalEvents,
			Result: map[string]any{
				"last_completed_at": lastCompletedAt.Time,
			},
		}, nil
	}
}

// processTaskSkillUsage scans a single task's messages for read tool calls
// that match workspace skill paths, upserting usage events.
func processTaskSkillUsage(
	ctx context.Context,
	queries *db.Queries,
	task db.ListUnprocessedCompletedTasksRow,
	cache map[[16]byte][]db.Skill,
) (int64, error) {
	messages, err := queries.ListTaskMessages(ctx, task.ID)
	if err != nil {
		return 0, fmt.Errorf("list task messages: %w", err)
	}

	// Load (or reuse cached) workspace skills.
	var skills []db.Skill
	if cached, ok := cache[task.WorkspaceID.Bytes]; ok {
		skills = cached
	} else {
		skills, err = queries.ListSkillsByWorkspace(ctx, task.WorkspaceID)
		if err != nil {
			return 0, fmt.Errorf("list workspace skills: %w", err)
		}
		cache[task.WorkspaceID.Bytes] = skills
	}
	if len(skills) == 0 {
		return 0, nil
	}

	skillDir := skillDirRelativePath(task.Provider)
	if skillDir == "" {
		return 0, nil
	}

	// Build a map of skill suffix → skill for this workspace.
	suffixMap := make(map[string]pgtype.UUID, len(skills))
	// Also build a name map for direct skill invocations (tool_use/skill).
	nameMap := make(map[string]pgtype.UUID, len(skills))
	for _, s := range skills {
		suffix := skillDir + "/" + sanitizeSkillName(s.Name) + "/SKILL.md"
		suffixMap[suffix] = s.ID
		nameMap[strings.ToLower(strings.TrimSpace(s.Name))] = s.ID
	}

	var events int64
	for _, msg := range messages {
		if msg.Type != "tool_use" {
			continue
		}
		if !msg.Tool.Valid {
			continue
		}

		switch msg.Tool.String {
		case "read":
			events += matchSkillByReadPath(ctx, msg, suffixMap, queries, task)
		case "skill":
			events += matchSkillByName(ctx, msg, nameMap, queries, task)
		}
	}

	return events, nil
}

// matchSkillByReadPath checks if a tool_use/read message's path matches a
// skill's SKILL.md file and upserts the usage event.
func matchSkillByReadPath(
	ctx context.Context,
	msg db.TaskMessage,
	suffixMap map[string]pgtype.UUID,
	queries *db.Queries,
	task db.ListUnprocessedCompletedTasksRow,
) int64 {
	path, ok := extractReadPath(msg.Input)
	if !ok || path == "" {
		return 0
	}
	for suffix, skillID := range suffixMap {
		if strings.HasSuffix(path, suffix) {
			if err := queries.UpsertSkillUsageEvent(ctx, db.UpsertSkillUsageEventParams{
				SkillID:     skillID,
				TaskID:      task.ID,
				IssueID:     task.IssueID,
				WorkspaceID: task.WorkspaceID,
			}); err != nil {
				slog.Warn("skill_usage: upsert failed",
					"skill_id", skillID, "task_id", task.ID, "error", err)
				return 0
			}
			return 1
		}
	}
	return 0
}

// matchSkillByName matches a tool_use/skill message's skill name against
// workspace skills. The input JSON is expected to have a "name" field.
func matchSkillByName(
	ctx context.Context,
	msg db.TaskMessage,
	nameMap map[string]pgtype.UUID,
	queries *db.Queries,
	task db.ListUnprocessedCompletedTasksRow,
) int64 {
	var v struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(msg.Input, &v); err != nil || v.Name == "" {
		return 0
	}
	skillID, ok := nameMap[strings.ToLower(strings.TrimSpace(v.Name))]
	if !ok {
		return 0
	}
	if err := queries.UpsertSkillUsageEvent(ctx, db.UpsertSkillUsageEventParams{
		SkillID:     skillID,
		TaskID:      task.ID,
		IssueID:     task.IssueID,
		WorkspaceID: task.WorkspaceID,
	}); err != nil {
		slog.Warn("skill_usage: upsert failed",
			"skill_id", skillID, "task_id", task.ID, "error", err)
		return 0
	}
	return 1
}

// extractReadPath parses the JSONB input of a read tool_use message and
// returns the "path" field.
func extractReadPath(input []byte) (string, bool) {
	if len(input) == 0 {
		return "", false
	}
	var v struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(input, &v); err != nil {
		return "", false
	}
	return v.Path, true
}

// skillDirRelativePath returns the provider-native skills directory as a
// relative path (without workDir prefix), mirroring the logic in
// daemon/execenv/context.go:skillsDirPath. This is duplicated here because
// importing the daemon package would create an import cycle.
func skillDirRelativePath(provider string) string {
	if desc, ok := agent.BuiltinRuntimeByID(provider); ok {
		return desc.SkillsDir
	}
	switch provider {
	case "claude":
		return ".claude/skills"
	case "codebuddy":
		return ".codebuddy/skills"
	case "copilot":
		return ".github/skills"
	case "opencode":
		return ".opencode/skills"
	case "deveco":
		return ".deveco/skills"
	case "openclaw":
		return "skills"
	case "pi":
		return ".pi/skills"
	case "cursor":
		return ".cursor/skills"
	case "kimi":
		return ".kimi/skills"
	case "reasonix":
		return ".reasonix/skills"
	case "dsh":
		return ".dsh/skills"
	case "kiro":
		return ".kiro/skills"
	case "qoder", "qoderclicn":
		return ".qoder/skills"
	case "qwen":
		return ".qwen/skills"
	case "qwenpaw":
		return "skill_pool"
	case "mcode":
		return ".minimax/skills"
	case "traecli":
		return ".traecli/skills"
	case "antigravity":
		return ".agents/skills"
	case "grok":
		return ".grok/skills"
	default:
		return ".agent_context/skills"
	}
}

var nonAlphaNum = regexp.MustCompile(`[^a-z0-9]+`)

// sanitizeSkillName mirrors daemon/execenv/context.go:sanitizeSkillName.
func sanitizeSkillName(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = nonAlphaNum.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		s = "skill"
	}
	return s
}

func uuidToText(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		u.Bytes[0:4], u.Bytes[4:6], u.Bytes[6:8], u.Bytes[8:10], u.Bytes[10:16])
}
