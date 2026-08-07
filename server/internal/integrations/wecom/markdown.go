package wecom

// markdown.go — keeping member-authored text from becoming markdown the bot
// appears to have written. Shared by the inbox card (inbox_message.go) and the
// /issue confirmation (replier.go), both of which splice someone else's words
// into a message that goes out under the bot's name.

import "strings"

// breakLinkAdjacency stops member-authored text from forming a markdown link
// in a message the bot signs. An issue titled
// "[click here](http://evil.example)" otherwise arrives as a working link
// inside a card the recipient has every reason to trust: it is delivered by
// the bot, and nothing in it marks which parts are quoted from a user.
//
// It separates rather than escapes. A link is only formed when "]" and "(" are
// adjacent — CommonMark requires the link text to be followed *immediately* by
// "(", and the naive `\[([^\]]+)\]\(([^)]+)\)` rewriters that stand in for a
// real parser require it too. Image syntax "![x](u)" needs the same adjacency,
// so it is covered by the same rule. One plain space between them is enough,
// and it is the only edit made: text that does not contain "](" comes back
// byte-identical, so the common "[Bug] 登录失败" title is untouched.
//
// Backslash escaping was tried first and is unusable here. On a live tenant
// "\[Bug\]" came back as an italic serif "Bug" with the brackets gone, which
// is how a display-math block renders — the mechanism is inferred from the
// rendering, no WeCom doc describes it — and the conversation-list preview,
// which renders no markdown at all, showed the backslashes raw. So this
// function must never emit a backslash: it would either be visible or pull
// member text into a math block, and an unpaired "[" in a title would open one
// that never closes.
//
// Two properties the callers rely on:
//
//   - The inserted space can never begin a line. A "]" always precedes it on
//     the same line, so it cannot open an indented code block or turn into a
//     trailing hard-break.
//   - It adds one rune per occurrence, so callers that budget a length cap
//     must call it before measuring, not after.
//
// What it does not cover. A bare URL is still auto-linkified by the client and
// nothing here can stop that, which is acceptable because a bare URL displays
// its own destination — the attack this closes is a label claiming to go
// somewhere it does not. A CommonMark reference link ("[label]: url" on its
// own line plus "[label]" elsewhere) reaches the same end without "](", but it
// needs a line the member starts with "[", so only a body can carry it, and
// whether this renderer resolves reference definitions at all is unverified.
func breakLinkAdjacency(s string) string {
	return strings.ReplaceAll(s, "](", "] (")
}
