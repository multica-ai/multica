package channel

import "strings"

// Capability is a bitmask a Channel uses to DECLARE what it supports. It
// is declaration only: this package contains no degrade logic. A caller
// that wants to degrade output (rich card → plain text when CapRichCard
// is absent) reads the bitmask and decides for itself, so adding a new
// platform never forces a branch into the core. Capabilities() returns a
// Channel's fixed set; the zero value declares nothing.
type Capability uint64

const (
	// CapText — can deliver a plain text message. Every Channel is
	// expected to declare at least this.
	CapText Capability = 1 << iota
	// CapRichCard — can render a rich / interactive card (Lark
	// interactive card, Slack Block Kit, …).
	CapRichCard
	// CapThreadReply — can post a reply into a thread / topic.
	CapThreadReply
	// CapQuoteReply — can quote-reply to a specific message.
	CapQuoteReply
	// CapAttachment — can send and/or receive media attachments.
	CapAttachment
	// CapVoice — can handle voice / audio messages.
	CapVoice
	// CapTypingIndicator — can show a typing / "thinking" indicator.
	CapTypingIndicator
	// CapMessageEdit — can edit a message after it was sent (Lark card
	// patch, Slack chat.update, …).
	CapMessageEdit
)

// capabilityNames maps single-bit capabilities to a stable, lower-case
// name for String(). Order matches the bit order above so String() reads
// least-significant-bit first.
var capabilityNames = []struct {
	bit  Capability
	name string
}{
	{CapText, "text"},
	{CapRichCard, "rich_card"},
	{CapThreadReply, "thread_reply"},
	{CapQuoteReply, "quote_reply"},
	{CapAttachment, "attachment"},
	{CapVoice, "voice"},
	{CapTypingIndicator, "typing_indicator"},
	{CapMessageEdit, "message_edit"},
}

// Has reports whether c declares every capability in want. Has(0) is
// true (the empty requirement is always satisfied). Because want may be a
// combination of bits, this is an "includes all of" test, not "any of".
func (c Capability) Has(want Capability) bool {
	return c&want == want
}

// String renders the set bits as a "|"-joined list of names ("text|
// thread_reply"), "none" for the zero value, and appends any unknown
// high bits as a hex remainder so a forgotten name never silently
// vanishes from logs. It is for diagnostics only.
func (c Capability) String() string {
	if c == 0 {
		return "none"
	}
	var (
		parts     []string
		remaining = c
	)
	for _, cn := range capabilityNames {
		if remaining&cn.bit == cn.bit {
			parts = append(parts, cn.name)
			remaining &^= cn.bit
		}
	}
	if remaining != 0 {
		parts = append(parts, "0x"+strings.TrimLeft(hex(uint64(remaining)), "0"))
	}
	return strings.Join(parts, "|")
}

// hex formats v as a fixed lower-case hex string without importing fmt
// (keeps this leaf file dependency-free). Only used by String() for the
// rare unknown-bit remainder.
func hex(v uint64) string {
	const digits = "0123456789abcdef"
	var buf [16]byte
	for i := 15; i >= 0; i-- {
		buf[i] = digits[v&0xf]
		v >>= 4
	}
	return string(buf[:])
}
