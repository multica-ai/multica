package qoderruntime

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// squadLeader holds what a squad-leader run needs to deliver its turn. The
// Squad Operating Protocol tells a leader to delegate with a mention comment
// and record its evaluation through the Multica CLI; a managed Qoder session
// has no CLI, so the agent states both in its final message and the bridge
// posts them with the task credential. Only a comment created through the
// comments API triggers the mentioned members.
type squadLeader struct {
	IssueID          string `json:"issue_id"`
	TriggerCommentID string `json:"trigger_comment_id,omitempty"`
	Commented        bool   `json:"commented,omitempty"`
	Recorded         bool   `json:"recorded,omitempty"`
	// Persist write intent before each POST. Neither endpoint deduplicates an
	// ambiguous response, so it must never cause an automatic replay.
	CommentSubmitted    bool `json:"comment_submitted,omitempty"`
	EvaluationSubmitted bool `json:"evaluation_submitted,omitempty"`
	// The task credential is never persisted; after a restart it is gone and
	// the run fails instead of delivering the turn.
	token string
}

const leaderBridgeNote = `
Squad leader on Qoder Cloud Agents: this session has no Multica CLI. Never run ` + "`multica`" + ` commands; the bridge performs the delegation comment and the evaluation record from the Squad Operating Protocol for you. Do not change code or open pull requests. End your turn with a final message in exactly this form:

OUTCOME: action | no_action | failed
REASON: <one short line>
<the comment body to post on the issue, using the exact mention markdown from the Squad Roster; leave it empty for no_action>
`

var leaderOutcomeLine = regexp.MustCompile(`(?im)^\s*\**OUTCOME\**\s*:\s*\**\s*(action|no_action|failed)\b.*$`)
var leaderReasonLine = regexp.MustCompile(`(?im)^\s*\**REASON\**\s*:\s*\**\s*(.*)$`)

// parseLeaderOutput reads the contract in leaderBridgeNote. Output without an
// OUTCOME line is treated as an action whose whole text is the comment, so a
// leader that ignored the format still delegates.
func parseLeaderOutput(output string) (outcome, reason, comment string) {
	loc := leaderOutcomeLine.FindStringSubmatchIndex(output)
	if loc == nil {
		return "action", "", strings.TrimSpace(output)
	}
	outcome = strings.ToLower(output[loc[2]:loc[3]])
	rest := output[loc[1]:]
	if m := leaderReasonLine.FindStringSubmatchIndex(rest); m != nil && strings.TrimSpace(rest[:m[0]]) == "" {
		reason = strings.TrimSpace(strings.Trim(rest[m[2]:m[3]], "*"))
		rest = rest[m[1]:]
	}
	comment = strings.TrimSpace(rest)
	if outcome == "no_action" {
		comment = ""
	}
	return outcome, reason, comment
}

// deliverLeader posts the delegation comment and records its actual dispatch
// result before completing. Only a definite rate-limit refusal permits retry.
func (b *Bridge) deliverLeader(ctx context.Context, r *run) error {
	l := r.Leader
	if l.Recorded {
		return nil
	}
	fail := func(reason string) error {
		r.Outcome, r.Failure = "failed", reason
		return b.persist()
	}
	if l.token == "" {
		if l.CommentSubmitted || l.Commented {
			return fail("the bridge restarted during squad leader delivery; check the issue comments and activity before retrying, because members may already be running")
		}
		return fail("the bridge restarted before the squad leader turn was delivered; trigger the leader again")
	}
	taskAPI := *b.multica
	taskAPI.token = l.token
	outcome, reason, comment := parseLeaderOutput(r.Output)
	if outcome == "action" && comment == "" && r.Failure == "" {
		r.Failure = "the squad leader reported action without a delegation comment"
	}
	const uncertainComment = "squad leader comment delivery could not be confirmed; check the issue comments before retrying, because members may already be running"
	if l.CommentSubmitted && !l.Commented && r.Failure == "" {
		r.Failure = uncertainComment
	}
	if !l.Commented && comment != "" && r.Failure == "" {
		if err := ctx.Err(); err != nil {
			return err
		}
		body := map[string]any{"content": comment, "type": "comment"}
		if l.TriggerCommentID != "" {
			body["parent_id"] = l.TriggerCommentID
		}
		l.CommentSubmitted = true
		if err := b.persist(); err != nil {
			return err
		}
		var posted struct {
			TriggerOutcomes []struct {
				TargetID   string `json:"target_id"`
				Status     string `json:"status"`
				ReasonCode string `json:"reason_code"`
			} `json:"trigger_outcomes"`
		}
		if err := taskAPI.call(ctx, http.MethodPost, "/api/issues/"+url.PathEscape(l.IssueID)+"/comments", body, &posted); err != nil {
			if rateLimited(err) {
				l.CommentSubmitted = false
				if persistErr := b.persist(); persistErr != nil {
					return persistErr
				}
				return err
			}
			if definitelyRejected(err) {
				r.Failure = "Multica rejected the squad leader comment: " + err.Error()
			} else {
				r.Failure = uncertainComment
			}
		} else {
			l.Commented = true
			var blocked []string
			for _, target := range posted.TriggerOutcomes {
				if target.Status == "blocked" {
					blocked = append(blocked, fmt.Sprintf("%s (%s)", target.TargetID, target.ReasonCode))
				}
			}
			if len(blocked) > 0 {
				r.Failure = "squad delegation blocked for " + strings.Join(blocked, ", ") + "; other targets may already be running, so do not repeat the delegation"
			}
		}
		if err := b.persist(); err != nil {
			return err
		}
	}
	if r.Failure != "" {
		outcome, reason = "failed", r.Failure
	}
	if reason == "" {
		reason = fmt.Sprintf("Recorded by the Qoder Cloud Agents bridge (%s)", outcome)
	}
	if l.EvaluationSubmitted {
		return fail("squad evaluation delivery could not be confirmed; check the issue activity before retrying and do not repeat the delegation")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	l.EvaluationSubmitted = true
	if err := b.persist(); err != nil {
		return err
	}
	err := taskAPI.call(ctx, http.MethodPost, "/api/issues/"+url.PathEscape(l.IssueID)+"/squad-evaluated", map[string]string{"outcome": outcome, "reason": reason}, nil)
	if err != nil {
		if rateLimited(err) {
			l.EvaluationSubmitted = false
			if persistErr := b.persist(); persistErr != nil {
				return persistErr
			}
			return err
		}
		failure := "squad evaluation delivery could not be confirmed; check the issue activity before retrying and do not repeat the delegation"
		if definitelyRejected(err) {
			failure = "Multica rejected the squad leader evaluation: " + err.Error()
		}
		if r.Failure != "" {
			failure = r.Failure + "; " + failure
		}
		return fail(failure)
	}
	l.Recorded = true
	if outcome == "failed" {
		r.Outcome, r.Failure = "failed", reason
	}
	return b.persist()
}
