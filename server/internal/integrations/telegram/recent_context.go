package telegram

import (
	"fmt"
	"strings"
	"sync"
)

// This file holds the Telegram counterpart of Lark's <recent_context>
// prefetch (lark/inbound_enricher.go, MUL-3084): when a member addresses the
// bot in a group (@-mention or a reply to one of its messages), the
// surrounding conversation is inlined ahead of their instruction so the agent
// sees more than the single addressed line.
//
// Lark can fetch that window on demand (im/v1/messages.list). The Telegram
// Bot API has no history endpoint — getUpdates is consume-once — so the
// polling loop keeps a small in-memory ring of the group messages it has
// already seen, per chat (and per forum topic), and reads the window back
// from that ring when an addressed message arrives. The ring is scoped to
// one installation's polling loop and lives only as long as the loop does: a
// restart starts empty, which is the accepted trade-off for keeping this out
// of the database.
//
// Retention contract: a non-addressed group message never creates a session
// or a turn on its own (MUL-2671). It is held in this in-memory ring, and once
// an addressed turn pulls it into its <recent_context> block it is persisted
// with that turn — the rewritten Text becomes the turn's
// chat_message.content, exactly as Lark's enricher documents for its own
// block. What the ring can hold depends on what Telegram delivers: with Group
// Privacy on, only commands, explicit @-mentions and replies to the bot; with
// it off, or for a bot that is a group admin, every group message.

// DefaultRecentContextSize is the window the production wiring uses: the
// number of preceding group messages kept per chat and inlined on an
// @-mention. Mirrors lark.DefaultRecentContextSize.
const DefaultRecentContextSize = 10

const (
	// maxRecentContextChats bounds the number of distinct chat/topic rings one
	// installation keeps; the least recently updated ring is evicted past it.
	maxRecentContextChats = 256
	// maxRecentContextTextRunes truncates one buffered message so a pasted wall
	// of text cannot dominate the prompt or the buffer.
	maxRecentContextTextRunes = 1000
)

// recentEntry is one buffered group message.
type recentEntry struct {
	MessageID int64
	Sender    string
	Text      string
}

// recentContextSource is what the inbound translation needs from the buffer:
// the preceding messages of one chat/topic, minus the ids the caller already
// renders elsewhere (the trigger itself and its quoted parent).
type recentContextSource interface {
	Snapshot(chatID, threadID int64, exclude ...int64) []recentEntry
}

// recentContextBuffer keeps the last N human group messages per chat/topic.
// Safe for concurrent use; the polling loop is single-goroutine but tests and
// future fan-out should not depend on that.
type recentContextBuffer struct {
	size int

	mu    sync.Mutex
	rings map[recentKey]*recentRing
	clock uint64
}

type recentKey struct {
	chatID   int64
	threadID int64
}

type recentRing struct {
	entries []recentEntry
	touched uint64
}

// newRecentContextBuffer returns nil when size <= 0, which disables the
// feature entirely (nil receivers are safe on every method).
func newRecentContextBuffer(size int) *recentContextBuffer {
	if size <= 0 {
		return nil
	}
	return &recentContextBuffer{size: size, rings: map[recentKey]*recentRing{}}
}

// Record appends one message to its chat/topic ring, dropping the oldest
// entry past the window and the least recently updated ring past the chat
// cap.
func (b *recentContextBuffer) Record(chatID, threadID int64, e recentEntry) {
	if b == nil {
		return
	}
	e.Text = truncateRunes(e.Text, maxRecentContextTextRunes)
	b.mu.Lock()
	defer b.mu.Unlock()
	b.clock++
	key := recentKey{chatID: chatID, threadID: threadID}
	ring, ok := b.rings[key]
	if !ok {
		if len(b.rings) >= maxRecentContextChats {
			b.evictOldestLocked()
		}
		ring = &recentRing{}
		b.rings[key] = ring
	}
	ring.touched = b.clock
	ring.entries = append(ring.entries, e)
	if extra := len(ring.entries) - b.size; extra > 0 {
		ring.entries = append([]recentEntry(nil), ring.entries[extra:]...)
	}
}

// Snapshot returns the ring's entries oldest-first, minus excluded ids. The
// returned slice is a copy the caller may keep.
func (b *recentContextBuffer) Snapshot(chatID, threadID int64, exclude ...int64) []recentEntry {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	ring, ok := b.rings[recentKey{chatID: chatID, threadID: threadID}]
	if !ok {
		return nil
	}
	skip := make(map[int64]bool, len(exclude))
	for _, id := range exclude {
		skip[id] = true
	}
	out := make([]recentEntry, 0, len(ring.entries))
	for _, e := range ring.entries {
		if skip[e.MessageID] {
			continue
		}
		out = append(out, e)
	}
	return out
}

func (b *recentContextBuffer) evictOldestLocked() {
	var (
		oldestKey recentKey
		oldest    uint64
		found     bool
	)
	for k, r := range b.rings {
		if !found || r.touched < oldest {
			oldestKey, oldest, found = k, r.touched, true
		}
	}
	if found {
		delete(b.rings, oldestKey)
	}
}

// recentEntryFromMessage renders one Telegram message the way a group member
// saw it: raw text or caption (bot mentions included), or a typed placeholder
// for media without a caption. Never called for bot senders — the polling
// loop drops those before recording, and the bot's own replies never arrive
// through getUpdates anyway.
func recentEntryFromMessage(m *Message) recentEntry {
	text := m.Text
	if text == "" {
		text = m.Caption
	}
	if strings.TrimSpace(text) == "" {
		text = fmt.Sprintf("[%s message]", classifyMessage(m))
	}
	sender := "Unknown user"
	if m.From != nil {
		if name := senderDisplayName(m.From); name != "" {
			sender = name
		}
	}
	return recentEntry{MessageID: m.MessageID, Sender: sender, Text: text}
}

// renderRecentContextBlock formats the window exactly like Lark's enricher:
// one "[<speaker>]: <text>" line per message, oldest-first, so an agent
// prompt reads the same across channels. Callers pass a non-empty slice.
func renderRecentContextBlock(entries []recentEntry) string {
	lines := make([]string, 0, len(entries))
	for _, e := range entries {
		lines = append(lines, fmt.Sprintf("[%s]: %s", e.Sender, e.Text))
	}
	return fmt.Sprintf("<recent_context count=\"%d\">\n%s\n</recent_context>",
		len(entries), strings.Join(lines, "\n"))
}

func truncateRunes(s string, max int) string {
	if max <= 0 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "…"
}
