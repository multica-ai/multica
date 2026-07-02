package notification

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// RegisterBuiltinRules adds R1-R5 to the RuleSet.
func RegisterBuiltinRules(rs *RuleSet) {
	rs.Add(ruleR5DispatchCallback())
	rs.Add(ruleR2ReviewComplete())
	rs.Add(ruleR4StageComplete())
	rs.Add(ruleR3BlockingLifted())
	rs.Add(ruleR1ChildToParent())
}

// ---- helpers shared by rules ----

func resolveQueries(ctx *RuleContext) *db.Queries {
	q, _ := ctx.Queries.(*db.Queries)
	return q
}

func parseIssueMetadata(raw []byte) map[string]any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil || out == nil {
		return map[string]any{}
	}
	return out
}

func uuidToString(id pgtype.UUID) string { return util.UUIDToString(id) }

func textToPtr(t pgtype.Text) *string {
	if !t.Valid {
		return nil
	}
	return &t.String
}

func uuidToPtr(id pgtype.UUID) *string {
	if !id.Valid {
		return nil
	}
	s := util.UUIDToString(id)
	return &s
}

// parsePayloadIssue extracts the standard issue-update fields from an
// event payload. Returns zero values when the payload shape doesn't match.
func parsePayloadIssue(payload any) (issueID, parentIssueID, oldStatus, newStatus, assigneeType, assigneeID string) {
	m, ok := payload.(map[string]any)
	if !ok {
		return
	}
	if v, ok := m["issue_id"].(string); ok {
		issueID = v
	}
	if v, ok := m["parent_issue_id"].(string); ok {
		parentIssueID = v
	}
	if v, ok := m["old_status"].(string); ok {
		oldStatus = v
	}
	if v, ok := m["new_status"].(string); ok {
		newStatus = v
	}
	if v, ok := m["issue_assignee_type"].(string); ok {
		assigneeType = v
	}
	if v, ok := m["issue_assignee_id"].(string); ok {
		assigneeID = v
	}
	return
}

// parseCommentPayload extracts comment-related fields from a
// comment:created event.
func parseCommentPayload(payload any) (commentID, issueID, authorType string) {
	m, ok := payload.(map[string]any)
	if !ok {
		return
	}
	if cmt, ok := m["comment"].(map[string]any); ok {
		if v, ok := cmt["id"].(string); ok {
			commentID = v
		}
		if v, ok := cmt["issue_id"].(string); ok {
			issueID = v
		}
		if v, ok := cmt["author_type"].(string); ok {
			authorType = v
		}
	}
	return
}

// parseIssuePayload extracts the issue from an event payload (for
// issue:updated / issue:created events).
func parseIssuePayload(payload any) map[string]any {
	m, ok := payload.(map[string]any)
	if !ok {
		return nil
	}
	if issue, ok := m["issue"].(map[string]any); ok {
		return issue
	}
	return m
}

// ---------------------------------------------------------------------------
// R5 — Dispatch Contract Callback (priority 10)
// ---------------------------------------------------------------------------

func ruleR5DispatchCallback() *Rule {
	return &Rule{
		ID:          "r5_dispatch_callback",
		Description: "When a dispatched sub-issue reaches terminal state, execute the dispatch contract callback.",
		Priority:    10,
		Cooldown:    0, // contract callbacks always fire
		Enabled:     true,
		Match: func(ctx *RuleContext, ev events.Event) bool {
			if ev.Type != "issue:updated" {
				return false
			}
			_, parentID, _, newStatus, _, _ := parsePayloadIssue(ev.Payload)
			if parentID == "" {
				return false
			}
			if newStatus != "done" && newStatus != "cancelled" {
				return false
			}
			// Check for dispatch_contract_id in the source issue's metadata
			q := resolveQueries(ctx)
			if q == nil {
				return false
			}
			issueID := ""
			if m, ok := ev.Payload.(map[string]any); ok {
				if v, ok := m["issue_id"].(string); ok {
					issueID = v
				}
			}
			if issueID == "" {
				return false
			}
			issue, err := q.GetIssue(ctx.Ctx, parseUUID(issueID))
			if err != nil {
				return false
			}
			meta := parseIssueMetadata(issue.Metadata)
			_, hasContract := meta["dispatch_contract_id"]
			return hasContract
		},
		BuildActions: func(ctx *RuleContext, ev events.Event) []Action {
			var actions []Action
			issueID, _, _, newStatus, _, _ := parsePayloadIssue(ev.Payload)
			if issueID == "" {
				return actions
			}
			q := resolveQueries(ctx)
			if q == nil {
				return actions
			}
			issue, err := q.GetIssue(ctx.Ctx, parseUUID(issueID))
			if err != nil {
				return actions
			}
			meta := parseIssueMetadata(issue.Metadata)
			contractID, ok := meta["dispatch_contract_id"].(string)
			if !ok {
				return actions
			}
			_ = contractID // Future: load DispatchContract from DB

			// Determine callback target from metadata
			callbackTarget := ""
			if v, ok := meta["callback_target"].(string); ok {
				callbackTarget = v
			}
			if callbackTarget == "" && issue.ParentIssueID.Valid {
				callbackTarget = uuidToString(issue.ParentIssueID)
			}
			if callbackTarget == "" {
				return actions
			}

			parent, err := q.GetIssue(ctx.Ctx, parseUUID(callbackTarget))
			if err != nil {
				return actions
			}

			prefix := ""
			if issue.WorkspaceID.Valid {
				p, _ := q.GetWorkspace(ctx.Ctx, issue.WorkspaceID)
				if p.IssuePrefix != "" {
					prefix = p.IssuePrefix
				}
			}
			childKey := prefix + "-" + strconv.Itoa(int(issue.Number))

			content := fmt.Sprintf(
				"📋 **派发任务完成通知**\n\n"+
					"派发任务 [%s](mention://issue/%s) 已进入 **%s** 状态。\n\n"+
					"请查看子任务产出并推进相关工作。",
				childKey, issueID, newStatus,
			)

			actions = append(actions, Action{
				Kind:         ActionPostComment,
				TargetIssueID: callbackTarget,
				Template:      content,
				TemplateVars:  nil,
			})

			// @mention parent assignee if agent
			if parent.AssigneeType.Valid && parent.AssigneeType.String == "agent" && parent.AssigneeID.Valid {
				actions = append(actions, Action{
					Kind:         ActionMentionAgent,
					TargetIssueID: callbackTarget,
					AgentID:       uuidToString(parent.AssigneeID),
				})
			}

			return actions
		},
	}
}

// ---------------------------------------------------------------------------
// R2 — Review Complete → Reviewed Party Notification (priority 20)
// ---------------------------------------------------------------------------

func ruleR2ReviewComplete() *Rule {
	return &Rule{
		ID:          "r2_review_complete",
		Description: "When an issue with review_target metadata enters in_review, notify the reviewed party.",
		Priority:    20,
		Cooldown:    600 * time.Second,
		Enabled:     true,
		Match: func(ctx *RuleContext, ev events.Event) bool {
			if ev.Type != "issue:updated" {
				return false
			}
			_, _, _, newStatus, _, _ := parsePayloadIssue(ev.Payload)
			if newStatus != "in_review" {
				return false
			}
			issueID := ""
			if m, ok := ev.Payload.(map[string]any); ok {
				if v, ok := m["issue_id"].(string); ok {
					issueID = v
				}
			}
			if issueID == "" {
				return false
			}
			q := resolveQueries(ctx)
			if q == nil {
				return false
			}
			issue, err := q.GetIssue(ctx.Ctx, parseUUID(issueID))
			if err != nil {
				return false
			}
			meta := parseIssueMetadata(issue.Metadata)
			_, hasTarget := meta["review_target"]
			return hasTarget
		},
		BuildActions: func(ctx *RuleContext, ev events.Event) []Action {
			var actions []Action
			issueID, _, _, _, _, _ := parsePayloadIssue(ev.Payload)
			if issueID == "" {
				return actions
			}
			q := resolveQueries(ctx)
			if q == nil {
				return actions
			}
			issue, err := q.GetIssue(ctx.Ctx, parseUUID(issueID))
			if err != nil {
				return actions
			}
			meta := parseIssueMetadata(issue.Metadata)
			reviewTarget, ok := meta["review_target"].(string)
			if !ok || reviewTarget == "" {
				return actions
			}

			reviewScore := ""
			if v, ok := meta["review_score"]; ok {
				reviewScore = fmt.Sprintf("%v", v)
			}
			reviewFindings := ""
			if v, ok := meta["review_findings"]; ok {
				reviewFindings = fmt.Sprintf("%v", v)
			}

			prefix := ""
			if issue.WorkspaceID.Valid {
				p, _ := q.GetWorkspace(ctx.Ctx, issue.WorkspaceID)
				if p.IssuePrefix != "" {
					prefix = p.IssuePrefix
				}
			}
			sourceKey := prefix + "-" + strconv.Itoa(int(issue.Number))

			content := fmt.Sprintf(
				"✅ **审查完成通知**\n\n"+
					"对 [%s](mention://issue/%s) 的审查已完成。\n",
				sourceKey, issueID,
			)
			if reviewScore != "" {
				content += fmt.Sprintf("**评分**：%s\n", reviewScore)
			}
			if reviewFindings != "" {
				content += fmt.Sprintf("**关键发现**：%s\n", reviewFindings)
			}

			actions = append(actions, Action{
				Kind:         ActionPostComment,
				TargetIssueID: reviewTarget,
				Template:      content,
			})

			// @mention review target assignee
			targetIssue, err := q.GetIssue(ctx.Ctx, parseUUID(reviewTarget))
			if err == nil && targetIssue.AssigneeType.Valid && targetIssue.AssigneeType.String == "agent" && targetIssue.AssigneeID.Valid {
				actions = append(actions, Action{
					Kind:         ActionMentionAgent,
					TargetIssueID: reviewTarget,
					AgentID:       uuidToString(targetIssue.AssigneeID),
				})
			}

			return actions
		},
	}
}

// ---------------------------------------------------------------------------
// R4 — Stage Complete → Parent Advancement (priority 30)
// ---------------------------------------------------------------------------

func ruleR4StageComplete() *Rule {
	return &Rule{
		ID:          "r4_stage_complete",
		Description: "When all children in a stage enter terminal state, notify the parent assignee.",
		Priority:    30,
		Cooldown:    300 * time.Second,
		Enabled:     true,
		Match: func(ctx *RuleContext, ev events.Event) bool {
			// R4 is triggered by child_terminal events (synthesized by the
			// engine from issue:updated with child done/cancelled statuses).
			if ev.Type != "notification:child_terminal" {
				return false
			}
			m, ok := ev.Payload.(map[string]any)
			if !ok {
				return false
			}
			parentID, _ := m["parent_issue_id"].(string)
			return parentID != ""
		},
		BuildActions: func(ctx *RuleContext, ev events.Event) []Action {
			var actions []Action
			m, ok := ev.Payload.(map[string]any)
			if !ok {
				return actions
			}
			childID, _ := m["child_id"].(string)
			parentID, _ := m["parent_issue_id"].(string)
			if parentID == "" {
				return actions
			}
			q := resolveQueries(ctx)
			if q == nil {
				return actions
			}

			// Check if ALL children of the parent are terminal
			parentUUID := parseUUID(parentID)
			children, err := q.ListChildIssues(ctx.Ctx, parentUUID)
			if err != nil {
				return actions
			}
			allDone := true
			doneCount := 0
			totalCount := len(children)
			var doneIDs []string
			for _, c := range children {
				if c.Status == "done" || c.Status == "cancelled" {
					doneCount++
					doneIDs = append(doneIDs, uuidToString(c.ID))
				} else {
					allDone = false
				}
			}
			if !allDone {
				return actions
			}

			parent, err := q.GetIssue(ctx.Ctx, parentUUID)
			if err != nil {
				return actions
			}

			prefix := ""
			if parent.WorkspaceID.Valid {
				p, _ := q.GetWorkspace(ctx.Ctx, parent.WorkspaceID)
				if p.IssuePrefix != "" {
					prefix = p.IssuePrefix
				}
			}
			parentKey := prefix + "-" + strconv.Itoa(int(parent.Number))

			content := fmt.Sprintf(
				"📊 **Stage 完成通知**\n\n"+
					"父任务 [%s](mention://issue/%s) 下的所有子任务已完成（%d/%d）。\n\n"+
					"已完成子任务：%s\n\n"+
					"请推进父任务至下一阶段。",
				parentKey, parentID, doneCount, totalCount,
				strings.Join(doneIDs, ", "),
			)
			_ = childID // used in template but not directly here for R4

			actions = append(actions, Action{
				Kind:         ActionPostComment,
				TargetIssueID: parentID,
				Template:      content,
			})

			if parent.AssigneeType.Valid && parent.AssigneeType.String == "agent" && parent.AssigneeID.Valid {
				actions = append(actions, Action{
					Kind:         ActionMentionAgent,
					TargetIssueID: parentID,
					AgentID:       uuidToString(parent.AssigneeID),
				})
			}

			return actions
		},
	}
}

// ---------------------------------------------------------------------------
// R3 — Blocking Lifted → Waiting Party Wake-up (priority 80)
// ---------------------------------------------------------------------------

func ruleR3BlockingLifted() *Rule {
	return &Rule{
		ID:          "r3_blocking_lifted",
		Description: "When an issue that others are waiting on unblocks, notify all waiting parties.",
		Priority:    80,
		Cooldown:    600 * time.Second,
		Enabled:     true,
		Match: func(ctx *RuleContext, ev events.Event) bool {
			if ev.Type != "issue:updated" {
				return false
			}
			issueID, _, oldStatus, newStatus, _, _ := parsePayloadIssue(ev.Payload)
			if oldStatus != "blocked" {
				return false
			}
			if newStatus == "blocked" {
				return false
			}
			return issueID != ""
		},
		BuildActions: func(ctx *RuleContext, ev events.Event) []Action {
			var actions []Action
			unblockedID, _, _, newStatus, _, _ := parsePayloadIssue(ev.Payload)
			if unblockedID == "" {
				return actions
			}
			q := resolveQueries(ctx)
			if q == nil {
				return actions
			}

			// Strategy: use metadata @> filter to find all issues whose
			// waiting_on_issue contains the unblocked issue ID.
			// The JSONB containment operator checks if the metadata
			// contains a waiting_on_issue key with the target ID.
			unblockedIssue, err := q.GetIssue(ctx.Ctx, parseUUID(unblockedID))
			if err != nil {
				return actions
			}

			waitingIssues, err := q.ListIssues(ctx.Ctx, db.ListIssuesParams{
				WorkspaceID:    unblockedIssue.WorkspaceID,
				MetadataFilter: []byte(fmt.Sprintf(`{"waiting_on_issue": "%s"}`, unblockedID)),
				Limit:          200,
			})
			if err != nil {
				slog.Warn("r3: metadata filter query failed", "error", err)
				return actions
			}

			prefix := ""
			if unblockedIssue.WorkspaceID.Valid {
				p, _ := q.GetWorkspace(ctx.Ctx, unblockedIssue.WorkspaceID)
				if p.IssuePrefix != "" {
					prefix = p.IssuePrefix
				}
			}
			sourceKey := prefix + "-" + strconv.Itoa(int(unblockedIssue.Number))

			for _, wi := range waitingIssues {
				meta := parseIssueMetadata(wi.Metadata)
				waitList, _ := meta["waiting_on_issue"].([]any)
				found := false
				for _, w := range waitList {
					if ws, ok := w.(string); ok && ws == unblockedID {
						found = true
						break
					}
				}
				if !found {
					continue
				}

				content := fmt.Sprintf(
					"🔓 **阻塞解除通知**\n\n"+
						"你所等待的 [%s](mention://issue/%s) 阻塞已解除（当前状态：**%s**）。\n\n"+
						"你可以继续推进相关工作了。",
					sourceKey, unblockedID, newStatus,
				)

				targetID := uuidToString(wi.ID)
				actions = append(actions, Action{
					Kind:         ActionPostComment,
					TargetIssueID: targetID,
					Template:      content,
				})

				if wi.AssigneeType.Valid && wi.AssigneeType.String == "agent" && wi.AssigneeID.Valid {
					actions = append(actions, Action{
						Kind:         ActionMentionAgent,
						TargetIssueID: targetID,
						AgentID:       uuidToString(wi.AssigneeID),
					})
				}

				// Remove unblockedID from waiting_on_issue
				newList := make([]string, 0, len(waitList))
				for _, w := range waitList {
					if ws, ok := w.(string); ok && ws != unblockedID {
						newList = append(newList, ws)
					}
				}
				if len(newList) == 0 {
					// All blockers cleared — auto-advance to in_progress
					actions = append(actions, Action{
						Kind:         ActionClearMetadata,
						TargetIssueID: targetID,
						MetaKey:       "waiting_on_issue",
					})
					actions = append(actions, Action{
						Kind:         ActionUpdateStatus,
						TargetIssueID: targetID,
						NewStatus:     "in_progress",
					})
				} else {
					actions = append(actions, Action{
						Kind:         ActionSetMetadata,
						TargetIssueID: targetID,
						MetaKey:       "waiting_on_issue",
						MetaValue:     newList,
					})
				}
			}

			return actions
		},
	}
}

// ---------------------------------------------------------------------------
// R1 — Child Complete → Parent Notification (priority 100)
// ---------------------------------------------------------------------------

func ruleR1ChildToParent() *Rule {
	return &Rule{
		ID:          "r1_child_to_parent",
		Description: "When a child issue reaches a significant state (done, in_review, cancelled), notify the parent.",
		Priority:    100,
		Cooldown:    300 * time.Second,
		Enabled:     true,
		Match: func(ctx *RuleContext, ev events.Event) bool {
			if ev.Type != "issue:updated" {
				return false
			}
			_, parentID, _, newStatus, _, _ := parsePayloadIssue(ev.Payload)
			if parentID == "" {
				return false
			}
			return newStatus == "done" || newStatus == "cancelled" || newStatus == "in_review"
		},
		BuildActions: func(ctx *RuleContext, ev events.Event) []Action {
			var actions []Action
			childID, parentID, _, newStatus, childAssigneeType, childAssigneeID := parsePayloadIssue(ev.Payload)
			if parentID == "" || childID == "" {
				return actions
			}
			q := resolveQueries(ctx)
			if q == nil {
				return actions
			}
			child, err := q.GetIssue(ctx.Ctx, parseUUID(childID))
			if err != nil {
				return actions
			}
			parent, err := q.GetIssue(ctx.Ctx, parseUUID(parentID))
			if err != nil {
				return actions
			}
			// Skip if parent is already terminal
			if parent.Status == "done" || parent.Status == "cancelled" {
				return actions
			}
			// Skip if parent has a human member assignee (per MUL-2538 pattern)
			if parent.AssigneeType.Valid && parent.AssigneeType.String == "member" {
				return actions
			}

			prefix := ""
			if child.WorkspaceID.Valid {
				p, _ := q.GetWorkspace(ctx.Ctx, child.WorkspaceID)
				if p.IssuePrefix != "" {
					prefix = p.IssuePrefix
				}
			}
			childKey := prefix + strconv.Itoa(int(child.Number))

			assigneeName := "未分配"
			if child.AssigneeType.Valid && child.AssigneeID.Valid {
				switch child.AssigneeType.String {
				case "agent":
					a, err := q.GetAgentInWorkspace(ctx.Ctx, db.GetAgentInWorkspaceParams{
						ID:          child.AssigneeID,
						WorkspaceID: child.WorkspaceID,
					})
					if err == nil {
						assigneeName = a.Name
					}
				case "squad":
					s, err := q.GetSquadInWorkspace(ctx.Ctx, db.GetSquadInWorkspaceParams{
						ID:          child.AssigneeID,
						WorkspaceID: child.WorkspaceID,
					})
					if err == nil {
						assigneeName = s.Name
					}
				}
			}

			statusLabel := map[string]string{
				"done":       "已完成",
				"cancelled":  "已取消",
				"in_review":  "审查中",
				"in_progress": "进行中",
				"blocked":    "阻塞中",
			}[newStatus]
			if statusLabel == "" {
				statusLabel = newStatus
			}

			content := fmt.Sprintf(
				"📌 **子任务状态更新**\n\n"+
					"子任务 [%s](mention://issue/%s) 状态变更为 **%s**。\n"+
					"处理方：%s",
				childKey, childID, statusLabel, assigneeName,
			)

			actions = append(actions, Action{
				Kind:         ActionPostComment,
				TargetIssueID: parentID,
				Template:      content,
			})

			// Update parent's children_progress metadata
			actions = append(actions, Action{
				Kind:         ActionSetMetadata,
				TargetIssueID: parentID,
				MetaKey:       "children_progress",
				MetaValue:     fmt.Sprintf("child %s → %s", childKey, newStatus),
			})

			// Cross-squad: mention parent assignee
			isCrossSquad := childAssigneeType == "squad" && childAssigneeID != "" &&
				(parent.AssigneeType.String != "squad" || uuidToString(parent.AssigneeID) != childAssigneeID)
			if isCrossSquad && parent.AssigneeType.Valid && parent.AssigneeType.String == "agent" && parent.AssigneeID.Valid {
				actions = append(actions, Action{
					Kind:         ActionMentionAgent,
					TargetIssueID: parentID,
					AgentID:       uuidToString(parent.AssigneeID),
				})
			}

			return actions
		},
	}
}

func parseUUID(s string) pgtype.UUID {
	var id pgtype.UUID
	_ = id.Scan(s)
	return id
}
