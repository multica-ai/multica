// CEREBRO-PATCH(agent-comment-tag-split): TECH-2961 — cerebro-zone helper file.
//
// Splits the routing key used by EventCommentCreated when the comment is
// authored by an agent, so the notification settings expose three rows:
//
//   - agent_comment_no_tag      — agent monologue, default off in inbox
//   - agent_comment_member_tag  — agent flagged a human, default on in inbox
//   - agent_comment_agent_tag   — agent handed off to another agent, default on
//
// Member-authored comments are unaffected and keep the upstream "new_comment"
// routing key. See notification_routing.go for the per-channel defaults.
package main

// pickAgentCommentRoutingKey returns the routing key for a comment-created
// event. For agent-authored comments we look at the mention set: a member
// (or @all) mention wins over an agent mention, and an agent mention wins
// over no mention at all. Member-authored comments always use "new_comment"
// — they are not split by tag, so muting agent chatter never silences a
// teammate's reply.
func pickAgentCommentRoutingKey(authorType string, mentions []mention) string {
	if authorType != "agent" {
		return "new_comment"
	}
	hasMemberish := false
	hasAgent := false
	for _, m := range mentions {
		switch m.Type {
		case "member", "all":
			hasMemberish = true
		case "agent":
			hasAgent = true
		}
	}
	switch {
	case hasMemberish:
		return "agent_comment_member_tag"
	case hasAgent:
		return "agent_comment_agent_tag"
	default:
		return "agent_comment_no_tag"
	}
}
