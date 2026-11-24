package lark

import "strings"

// Native Feishu @-mentions, the outbound half of #8234. An agent's answer
// that opens with a literal "@Wang" is plain black text: it neither links to
// the member nor rings their phone, so in a busy group the person who asked
// never learns the answer arrived. A mention the platform recognises is the
// only construct that notifies.
//
// The mention is built from the open_id we persisted for the task's
// initiator, NEVER by pattern-matching an "@name" the model wrote. Display
// names are ambiguous (duplicates, nicknames, and a model that simply
// guesses wrong), and a mis-resolved name pings the wrong colleague — the
// exact failure #8234 asks us to avoid. Identity flows from the DB or the
// message goes out unmentioned.
//
// The two outbound wire shapes spell mentions differently, and using the
// wrong spelling leaves visible markup in the transcript:
//
//   - msg_type=text  → <at user_id="ou_x"></at>
//   - schema-2.0 card markdown → <at id=ou_x></at>
//
// Both render the member's current display name from the id, so the element
// body is left empty rather than carrying a name that could go stale.

// mentionSeparator trails the mention so the agent's first word does not
// collide with the rendered name. A trailing space (not a newline) keeps a
// one-line answer on one line.
const mentionSeparator = " "

// safeMentionOpenID guards the open_id before it is interpolated into text
// content or card JSON. Real Feishu ids are "ou_" + hex, so anything
// carrying quotes, angle brackets, or whitespace is not an id we should be
// embedding — returning "" makes the caller send without a mention instead
// of emitting broken markup or malformed card JSON.
func safeMentionOpenID(openID string) string {
	if openID == "" || strings.ContainsAny(openID, "<>\"'`\\ \t\r\n") {
		return ""
	}
	return openID
}

// prependTextMention returns body with a native mention of openID in front,
// for the msg_type=text path. An empty or unsafe openID returns body
// unchanged — an unmentioned answer is the correct degradation, a wrong or
// broken mention is not.
func prependTextMention(openID, body string) string {
	id := safeMentionOpenID(openID)
	if id == "" {
		return body
	}
	return `<at user_id="` + id + `"></at>` + mentionSeparator + body
}

// prependMarkdownMention is prependTextMention for the schema-2.0 card
// markdown element, which spells the same mention with `id=` and no quotes.
func prependMarkdownMention(openID, body string) string {
	id := safeMentionOpenID(openID)
	if id == "" {
		return body
	}
	return `<at id=` + id + `></at>` + mentionSeparator + body
}
