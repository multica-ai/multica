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

// deliverLeader posts the delegation comment and records the evaluation
// before the run completes. Each step is saved once it succeeds so a retry
// after a later failure does not post the comment twice.
func (b *Bridge) deliverLeader(ctx context.Context, r *run) error {
	l := r.Leader
	if l.token == "" {
		r.Outcome = "failed"
		r.Failure = "the bridge restarted before the squad leader turn was delivered; trigger the leader again"
		return b.persist()
	}
	taskAPI := *b.multica
	taskAPI.token = l.token
	outcome, reason, comment := parseLeaderOutput(r.Output)
	if !l.Commented && comment != "" {
		body := map[string]any{"content": comment, "type": "comment"}
		if l.TriggerCommentID != "" {
			body["parent_id"] = l.TriggerCommentID
		}
		if err := taskAPI.call(ctx, http.MethodPost, "/api/issues/"+url.PathEscape(l.IssueID)+"/comments", body, nil); err != nil {
			if definitelyRejected(err) {
				r.Outcome = "failed"
				r.Failure = "Multica rejected the squad leader comment: " + err.Error()
				return b.persist()
			}
			return err
		}
		l.Commented = true
		if err := b.persist(); err != nil {
			return err
		}
	}
	if !l.Recorded {
		if reason == "" {
			reason = fmt.Sprintf("Recorded by the Qoder Cloud Agents bridge (%s)", outcome)
		}
		err := taskAPI.call(ctx, http.MethodPost, "/api/issues/"+url.PathEscape(l.IssueID)+"/squad-evaluated", map[string]string{"outcome": outcome, "reason": reason}, nil)
		if err != nil && !definitelyRejected(err) {
			return err
		}
		// A rejected record must not fail a turn whose comment already
		// delegated; the completion fallback comment then keeps a record.
		l.Recorded = true
		if err := b.persist(); err != nil {
			return err
		}
	}
	return nil
}
