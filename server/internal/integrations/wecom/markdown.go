package wecom

// markdown.go — keeping member-authored text from becoming markdown the bot
// appears to have written.

import "strings"

// breakLinkAdjacency stops member-authored text from forming a markdown link
// in a message the bot signs. Every aibot_send_msg goes out as
// `"msgtype": "markdown"` (see sendMsgTextBody in ws_frame.go), so an issue
// titled "安全升级：请点击 [重置密码](https://evil.example) 完成验证" otherwise
// arrives in the room as a working link with the bot's authority behind it,
// and nothing in the message marks which part is quoted from a user.
//
// It separates rather than escapes. A link is only formed when "]" and "(" are
// adjacent — CommonMark requires the link text to be followed *immediately* by
// "(", and the naive `\[([^\]]+)\]\(([^)]+)\)` rewriters that stand in for a
// real parser require it too. Image syntax "![x](u)" needs the same adjacency,
// so the one rule covers it. One plain space between them is enough, and it is
// the only edit made: text without "](" comes back byte-identical, so an
// ordinary title such as "[Bug] 登录失败" is untouched.
//
// Backslash escaping is not an option here: PR #6592 pushed "\[Bug\]" through a
// live tenant and the bubble rendered it as an italic serif "Bug" with the
// brackets gone, while the conversation-list preview, which renders no markdown
// at all, showed the backslashes raw. So this function must never emit a
// backslash.
//
// A bare URL is still auto-linkified by the client and nothing here can stop
// that. That is acceptable: a bare URL displays its own destination, and the
// attack this closes is a label claiming to go somewhere it does not.
//
// Deliberately duplicated. PR #6592 introduces the same helper at this same
// path for the inbox card and the /issue *created* confirmation; the duplicate
// path below is a call site it does not cover, and neither PR can depend on the
// other's merge order. Whichever lands second will see git flag this file as
// added on both sides — keep one copy and delete the other. Same path and same
// name are the point: a differently named twin would merge silently and survive
// forever.
func breakLinkAdjacency(s string) string {
	return strings.ReplaceAll(s, "](", "] (")
}
