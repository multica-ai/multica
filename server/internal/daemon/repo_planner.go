package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
)

// repoPlan is the outcome of a single planning attempt.
//
//   - chosen != nil, needsUser == false → auto-assigned or high-confidence pick
//   - chosen == nil, needsUser == true  → emit a clarification card and pause
//   - chosen == nil, needsUser == false → no repos registered, let the agent run
//     against whatever workspace-level files exist (preserves pre-planner behavior)
type repoPlan struct {
	chosen     *RepoData
	confidence float32
	reason     string
	candidates []RepoData
	needsUser  bool
}

// planRepo decides which workspace repo a chat task should target.
//
// v1 is intentionally heuristic-only — no LLM call on the hot path. Adding a
// small completion here is cheap (<1s) but requires picking a provider and
// handling its auth in the daemon, so we defer that until the shape of the
// chip/clarification UX has proven itself on the heuristic cases:
//
//   - 0 repos: nothing to pick; skip planning entirely (empty chosen + no user prompt)
//   - 1 repo:  unambiguous auto-assign, high confidence
//   - 2+ repos: bail to the user; we do not want to quietly guess wrong
//
// Issue/autopilot tasks are passed through unchanged because they already
// carry their own repo context elsewhere.
func planRepo(task Task) repoPlan {
	if task.ChatSessionID == "" {
		return repoPlan{}
	}
	repos := task.Repos
	switch len(repos) {
	case 0:
		return repoPlan{}
	case 1:
		r := repos[0]
		return repoPlan{
			chosen:     &r,
			confidence: 1.0,
			reason:     "workspace has a single repo",
		}
	default:
		return repoPlan{
			candidates: repos,
			needsUser:  true,
		}
	}
}

// emitPlanAndMaybePause writes the planner's decision back to the server as a
// task_message (so the chat UI can render the chip / clarification card) and
// optionally parks the task in 'awaiting_user' status. Returns `paused=true`
// when the caller should return from handleTask without executing the agent.
func (d *Daemon) emitPlanAndMaybePause(ctx context.Context, task Task, plan repoPlan, taskLog *slog.Logger) (paused bool) {
	if plan.needsUser {
		candidates := make([]map[string]any, 0, len(plan.candidates))
		for _, c := range plan.candidates {
			candidates = append(candidates, map[string]any{
				"url":         c.URL,
				"description": c.Description,
			})
		}
		msg := []TaskMessageData{{
			// Seq 0 is fine here: task_message has no uniqueness constraint on
			// (task_id, seq) in the current schema, and the planner always
			// writes before any agent output starts.
			Seq:  0,
			Type: "repo_clarification",
			Input: map[string]any{
				"question":   buildClarificationQuestion(task),
				"candidates": candidates,
			},
		}}
		if err := d.client.ReportTaskMessages(ctx, task.ID, msg); err != nil {
			taskLog.Warn("emit repo_clarification failed", "error", err)
			// Don't pause on failure: falling through to normal execution is
			// safer than leaving the task stuck awaiting a card the user
			// never got.
			return false
		}
		if err := d.client.SetTaskAwaitingUser(ctx, task.ID, 0); err != nil {
			taskLog.Warn("set awaiting_user failed; proceeding without pause", "error", err)
			return false
		}
		taskLog.Info("task paused for repo clarification", "candidates", len(plan.candidates))
		return true
	}

	if plan.chosen != nil {
		msg := []TaskMessageData{{
			Seq:  0,
			Type: "repo_plan",
			Input: map[string]any{
				"repo_url":         plan.chosen.URL,
				"repo_description": plan.chosen.Description,
				"confidence":       plan.confidence,
				"reason":           plan.reason,
			},
		}}
		if err := d.client.ReportTaskMessages(ctx, task.ID, msg); err != nil {
			taskLog.Warn("emit repo_plan failed (non-fatal)", "error", err)
		}
	}
	return false
}

// filterReposForTarget narrows a repo list to only the chosen target.
//
// If targetURL is empty (no planner decision yet, or non-chat task), returns
// repos unchanged. If the target URL is not found in repos (e.g. workspace
// repo list changed after task was queued), falls back to the full list
// rather than leaving the agent with zero repos.
func filterReposForTarget(repos []RepoData, targetURL string) []RepoData {
	if targetURL == "" {
		return repos
	}
	for _, r := range repos {
		if r.URL == targetURL {
			return []RepoData{r}
		}
	}
	return repos
}

func buildClarificationQuestion(task Task) string {
	snippet := strings.TrimSpace(task.ChatMessage)
	if snippet == "" {
		return "需要你确认一下仓库"
	}
	if len(snippet) > 40 {
		snippet = snippet[:40] + "…"
	}
	return fmt.Sprintf("这条消息有多个候选仓库，想在哪个仓库执行？\n— %s", snippet)
}
