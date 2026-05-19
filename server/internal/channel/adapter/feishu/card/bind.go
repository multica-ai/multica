// Package card — bind.go renders the "绑定 Multica 账号" prompt card.
//
// Responsibilities:
//   - Map a BindPromptData (one-time URL + TTL) to a Feishu interactive
//     card guiding an unbound user through the workspace-binding flow.
//   - Embed the TTL into the user-visible expiry hint via the caller-
//     supplied TTLMinutes — no hardcoded duration in the package.
//
// Boundaries:
//   - No token issuance / no URL generation — BindPromptData is built by
//     the caller (binding-token Service from STA-6); this file only
//     formats the prompt.
//   - No I/O. Pure rendering.
package card

import "fmt"

// BindPromptData holds the fields needed to render a bind prompt card.
type BindPromptData struct {
	// URL is the one-time binding link delivered via private chat.
	URL string
	// TTLMinutes is how long the URL stays valid; surfaced in the user-
	// visible expiry hint. The caller (STA-6 binding token Service) is
	// the source of truth — when a config change moves the TTL, this
	// field moves with it instead of drifting out of sync.
	TTLMinutes int
}

// BindPromptCard builds a Feishu interactive card that guides the user
// through the workspace binding flow. Delivered via private chat after
// an unbound user @-mentions the bot in a group.
func BindPromptCard(d BindPromptData) *Card {
	c := NewCard("绑定 Multica 账号", "orange")

	c.AddMarkdown("你还没有绑定 Multica 账号。点击下方按钮完成绑定，绑定后即可在群里使用 Bot 功能。")

	ttl := d.TTLMinutes
	if ttl <= 0 {
		// Defensive fallback: an upstream wiring bug should still
		// produce a sensible card rather than "链接 0 分钟内有效".
		ttl = 10
	}
	c.AddMarkdown(fmt.Sprintf("**链接 %d 分钟内有效，仅可使用一次。**", ttl))

	if d.URL != "" {
		c.AddHR()
		c.AddButton("立即绑定", d.URL)
	}

	return c
}
