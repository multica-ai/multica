package notification

import (
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Detector is a pure event-driven anomaly detector. It reacts to specific
// events and optionally performs corrective actions. No timers, no cron.
type Detector struct {
	ID          string
	Description string
	// Match returns true when this detector's check should run.
	Match func(ctx *RuleContext, ev events.Event) bool
	// Check runs the detection logic and returns corrective actions.
	Check func(ctx *RuleContext, ev events.Event) []Action
}

// DetectorSet holds all registered detectors and per-detector cooldown state.
type DetectorSet struct {
	detectors []*Detector
	mu        sync.Mutex
	cooldowns map[string]time.Time // key: "detectorID:targetID"
}

// NewDetectorSet creates an empty DetectorSet.
func NewDetectorSet() *DetectorSet {
	return &DetectorSet{
		cooldowns: make(map[string]time.Time),
	}
}

// Add registers a detector.
func (ds *DetectorSet) Add(d *Detector) {
	ds.detectors = append(ds.detectors, d)
}

// Detectors returns the registered detectors.
func (ds *DetectorSet) Detectors() []*Detector {
	return ds.detectors
}

// IsCoolingDown checks whether the given (detectorID, targetID) is within cooldown.
func (ds *DetectorSet) IsCoolingDown(detectorID, targetID string, cooldown time.Duration) bool {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	key := detectorID + ":" + targetID
	last, ok := ds.cooldowns[key]
	if !ok {
		return false
	}
	return time.Since(last) < cooldown
}

// MarkCooled sets the cooldown timestamp for (detectorID, targetID).
func (ds *DetectorSet) MarkCooled(detectorID, targetID string) {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	ds.cooldowns[detectorID+":"+targetID] = time.Now()
}

// RegisterBuiltinDetectors adds D1-D6 to the DetectorSet.
func RegisterBuiltinDetectors(ds *DetectorSet) {
	ds.Add(detectorD1Deadlock())
	ds.Add(detectorD4Orphan())
	ds.Add(detectorD2Timeout())
	ds.Add(detectorD3Stale())
	ds.Add(detectorD5CrossSquadSilence())
	ds.Add(detectorD6LoopBreak())
}

// ---------------------------------------------------------------------------
// D1 — Parent-Child Deadlock Detector
// ---------------------------------------------------------------------------

func detectorD1Deadlock() *Detector {
	return &Detector{
		ID:          "d1_deadlock",
		Description: "Detects when all children of a non-terminal parent are done but the parent hasn't progressed.",
		Match: func(ctx *RuleContext, ev events.Event) bool {
			if ev.Type != "issue:updated" {
				return false
			}
			_, parentID, _, newStatus, _, _ := parsePayloadIssue(ev.Payload)
			if parentID == "" {
				return false
			}
			return newStatus == "done" || newStatus == "cancelled"
		},
		Check: func(ctx *RuleContext, ev events.Event) []Action {
			var actions []Action
			_, parentID, _, _, _, _ := parsePayloadIssue(ev.Payload)
			if parentID == "" {
				return actions
			}
			q := resolveQueries(ctx)
			if q == nil {
				return actions
			}

			parentUUID := parseUUID(parentID)
			parent, err := q.GetIssue(ctx.Ctx, parentUUID)
			if err != nil {
				return actions
			}
			// Only check non-terminal parents
			if parent.Status == "done" || parent.Status == "cancelled" {
				return actions
			}

			children, err := q.ListChildIssues(ctx.Ctx, parentUUID)
			if err != nil {
				return actions
			}
			allDone := true
			for _, c := range children {
				if c.Status != "done" && c.Status != "cancelled" {
					allDone = false
					break
				}
			}
			if !allDone || len(children) == 0 {
				return actions
			}

			// Check if parent has acknowledged any child in recent comments
			comments, err := q.ListCommentsForIssue(ctx.Ctx, db.ListCommentsForIssueParams{
				IssueID:     parentUUID,
				WorkspaceID: parent.WorkspaceID,
				Limit:       20,
			})
			if err != nil {
				return actions
			}
			acknowledged := false
			for _, c := range comments {
				for _, child := range children {
					if containsStr(c.Content, uuidToStrP(child.ID)) {
						acknowledged = true
						break
					}
				}
				if acknowledged {
					break
				}
			}
			if acknowledged {
				return actions
			}

			// Deadlock detected
			var childNames []string
			for _, c := range children {
				childNames = append(childNames, uuidToString(c.ID))
			}

			content := fmt.Sprintf(
				"⚠️ **死锁检测**：所有子任务已完成，但父任务似乎未感知。\n\n"+
					"已完成子任务：%s\n\n"+
					"建议：检查子任务产出，推进父任务至下一阶段。",
				joinStrs(childNames, ", "),
			)

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

			actions = append(actions, Action{
				Kind:         ActionSetMetadata,
				TargetIssueID: parentID,
				MetaKey:       "deadlock_detected_at",
				MetaValue:     time.Now().UTC().Format(time.RFC3339),
			})

			return actions
		},
	}
}

// ---------------------------------------------------------------------------
// D4 — Orphan Notification Detector
// ---------------------------------------------------------------------------

func detectorD4Orphan() *Detector {
	return &Detector{
		ID:          "d4_orphan",
		Description: "Detects when a child finishes but the parent seems unaware (orphaned notification).",
		Match: func(ctx *RuleContext, ev events.Event) bool {
			if ev.Type != "issue:updated" {
				return false
			}
			_, parentID, _, newStatus, _, _ := parsePayloadIssue(ev.Payload)
			if parentID == "" {
				return false
			}
			return newStatus == "done" || newStatus == "cancelled"
		},
		Check: func(ctx *RuleContext, ev events.Event) []Action {
			var actions []Action
			childID, parentID, _, _, _, _ := parsePayloadIssue(ev.Payload)
			if parentID == "" || childID == "" {
				return actions
			}
			q := resolveQueries(ctx)
			if q == nil {
				return actions
			}

			parentUUID := parseUUID(parentID)
			parent, err := q.GetIssue(ctx.Ctx, parentUUID)
			if err != nil {
				return actions
			}
			if parent.Status == "done" || parent.Status == "cancelled" {
				return actions
			}

			// Check if parent has any comment referencing this child in last 20 comments
			comments, err := q.ListCommentsForIssue(ctx.Ctx, db.ListCommentsForIssueParams{
				IssueID:     parentUUID,
				WorkspaceID: parent.WorkspaceID,
				Limit:       20,
			})
			if err != nil {
				return actions
			}
			for _, c := range comments {
				if containsStr(c.Content, childID) {
					return actions // parent has acknowledged
				}
			}

			// Check last comment age
			if len(comments) > 0 {
				lastCommentTime := comments[0].CreatedAt.Time
				if time.Since(lastCommentTime) < 1*time.Hour {
					// Too recent, give parent time to respond
					return actions
				}
			}

			content := fmt.Sprintf(
				"💡 子任务 %s 已完成。请查看其产出并考虑推进父任务。",
				childID,
			)
			actions = append(actions, Action{
				Kind:         ActionPostComment,
				TargetIssueID: parentID,
				Template:      content,
				// No @mention — lightweight reminder only
			})

			return actions
		},
	}
}

// ---------------------------------------------------------------------------
// D2 — Dispatch Timeout Detector (event-piggyback)
// ---------------------------------------------------------------------------

func detectorD2Timeout() *Detector {
	return &Detector{
		ID:          "d2_timeout",
		Description: "Detects dispatch contracts that have been pending too long (event-piggyback: checked on any child_terminal).",
		Match: func(ctx *RuleContext, ev events.Event) bool {
			if ev.Type != "issue:updated" {
				return false
			}
			_, _, _, newStatus, _, _ := parsePayloadIssue(ev.Payload)
			return newStatus == "done" || newStatus == "cancelled"
		},
		Check: func(ctx *RuleContext, ev events.Event) []Action {
			// D2 is a placeholder — full implementation requires
			// the DispatchContract table (Phase 3). For now it is a no-op.
			// When the contract table exists, this detector will:
			// 1. Scan all pending contracts
			// 2. Check if any have exceeded 8h since creation
			// 3. Issue escalation actions for timed-out contracts
			slog.Debug("d2_timeout: placeholder check (contract table not yet available)")
			return nil
		},
	}
}

// ---------------------------------------------------------------------------
// D3 — Stale Review Detector (event-piggyback)
// ---------------------------------------------------------------------------

func detectorD3Stale() *Detector {
	return &Detector{
		ID:          "d3_stale",
		Description: "Detects in_review issues that have been stale for too long (event-piggyback: checked on any status_changed).",
		Match: func(ctx *RuleContext, ev events.Event) bool {
			return ev.Type == "issue:updated"
		},
		Check: func(ctx *RuleContext, ev events.Event) []Action {
			var actions []Action
			q := resolveQueries(ctx)
			if q == nil {
				return actions
			}

			// D3 is a scan-heavy detector. For now, we only scan when we have
			// workspace context from the triggering event.
			wsID := parseWorkspaceID(ev)
			if !wsID.Valid {
				return actions
			}

			inReviewIssues, err := q.ListIssues(ctx.Ctx, db.ListIssuesParams{
				WorkspaceID: wsID,
				Status:      pgtype.Text{String: "in_review", Valid: true},
				Limit:       200,
			})
			if err != nil {
				slog.Warn("d3_stale: failed to list in_review issues", "error", err)
				return actions
			}

			for _, issue := range inReviewIssues {
				meta := parseIssueMetadata(issue.Metadata)
				posture, _ := meta["interaction_posture"].(string)

				// Stale review: in_review with submitted_for_review posture > 24h
				if posture == "submitted_for_review" || posture == "" {
					if issue.UpdatedAt.Valid && time.Since(issue.UpdatedAt.Time) > 24*time.Hour {
						content := "⏰ **审查提醒**：此 issue 已处于审查状态超过 24 小时。请审查方尽快处理。"
						actions = append(actions, Action{
							Kind:          ActionPostComment,
							TargetIssueID: uuidToString(issue.ID),
							Template:      content,
						})
						if issue.AssigneeType.Valid && issue.AssigneeType.String == "agent" && issue.AssigneeID.Valid {
							actions = append(actions, Action{
								Kind:          ActionMentionAgent,
								TargetIssueID: uuidToString(issue.ID),
								AgentID:       uuidToString(issue.AssigneeID),
							})
						}
					}
				}

				// Severe stale: review_completed > 6h without advancement
				if posture == "review_completed" {
					if issue.UpdatedAt.Valid && time.Since(issue.UpdatedAt.Time) > 6*time.Hour {
						reviewTarget, _ := meta["review_target"].(string)
						if reviewTarget != "" {
							content := fmt.Sprintf(
								"🚨 **严重陈旧审查**：此 issue 审查已完成超过 6 小时但尚未推进。审查目标：%s",
								reviewTarget,
							)
							actions = append(actions, Action{
								Kind:          ActionPostComment,
								TargetIssueID: uuidToString(issue.ID),
								Template:      content,
							})
							// Notify the review target's assignee
							targetIssue, err := q.GetIssue(ctx.Ctx, parseUUID(reviewTarget))
							if err == nil && targetIssue.AssigneeType.Valid &&
								targetIssue.AssigneeType.String == "agent" && targetIssue.AssigneeID.Valid {
								actions = append(actions, Action{
									Kind:          ActionPostComment,
									TargetIssueID: reviewTarget,
									Template:      "🚨 审查方已完成对相关 issue 的审查，请及时查看并推进。",
								})
								actions = append(actions, Action{
									Kind:          ActionMentionAgent,
									TargetIssueID: reviewTarget,
									AgentID:       uuidToString(targetIssue.AssigneeID),
								})
							}
						}
					}
				}
			}

			return actions
		},
	}
}

// ---------------------------------------------------------------------------
// D5 — Cross-Squad Silence Detector (event-piggyback)
// ---------------------------------------------------------------------------

func detectorD5CrossSquadSilence() *Detector {
	return &Detector{
		ID:          "d5_cross_squad_silence",
		Description: "Detects when cross-squad communication has gone silent for too long (event-piggyback).",
		Match: func(ctx *RuleContext, ev events.Event) bool {
			// Trigger on any cross-squad relevant event
			return ev.Type == "issue:updated" || ev.Type == "comment:created"
		},
		Check: func(ctx *RuleContext, ev events.Event) []Action {
			// Placeholder — full implementation requires:
			// 1. Identifying cross-squad issue pairs
			// 2. Checking last cross-squad comment timestamp
			// 3. Issuing summary comment if blocked child detected
			return nil
		},
	}
}

// ---------------------------------------------------------------------------
// D6 — Loop Break Detector (event-piggyback)
// ---------------------------------------------------------------------------

func detectorD6LoopBreak() *Detector {
	return &Detector{
		ID:          "d6_loop_break",
		Description: "Detects when a dispatch callback has been fulfilled but the from_issue hasn't progressed (event-piggyback).",
		Match: func(ctx *RuleContext, ev events.Event) bool {
			return ev.Type == "issue:updated" || ev.Type == "comment:created"
		},
		Check: func(ctx *RuleContext, ev events.Event) []Action {
			// Placeholder — full implementation requires DispatchContract table
			return nil
		},
	}
}

// ---- utility functions ----

func uuidToStrP(id pgtype.UUID) string {
	if !id.Valid {
		return ""
	}
	return uuidToString(id)
}

func containsStr(haystack, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) &&
		(haystack == needle ||
			(len(haystack) > len(needle) && indexOfSubstr(haystack, needle) >= 0))
}

func indexOfSubstr(s, sub string) int {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func joinStrs(ss []string, sep string) string {
	if len(ss) == 0 {
		return ""
	}
	result := ss[0]
	for i := 1; i < len(ss); i++ {
		result += sep + ss[i]
	}
	return result
}
