package runtime

import (
	"fmt"
	"strings"
)

// This file is the runtime behavior of the prune_tool_results cost saving
// (FIR-2325). When the saving is "on" for a workspace, the tool loop calls one
// of these helpers after each round so superseded tool-call output is dropped
// from the running transcript instead of being resent (and re-billed as input
// tokens) on every subsequent round. The most recent round's results are always
// kept verbatim — the model still needs them to take the next step; only
// earlier rounds, which the model rarely re-reads, are pruned.
//
// Before this, prune_tool_results only *counted* the tool-result surface for the
// dashboard and never changed a run — a toggle that did nothing when switched on.

const prunedToolResultPrefix = "[pruned superseded tool output:"

// prunedToolResultPlaceholder is left in place of a dropped tool result. It
// keeps the transcript well-formed (the tool_use/tool_result pairing the model
// expects is preserved) and records how many characters were removed.
func prunedToolResultPlaceholder(n int) string {
	return fmt.Sprintf("%s %d chars]", prunedToolResultPrefix, n)
}

// isPrunedToolResult reports whether a body has already been replaced by a
// placeholder, so repeated prune passes across rounds are idempotent.
func isPrunedToolResult(s string) bool {
	return strings.HasPrefix(s, prunedToolResultPrefix)
}

// pruneSupersededGatewayToolResults prunes the OpenAI-compatible transcript used
// by the chat-completions tool loops. Tool results are messages with role
// "tool"; the latest round's results follow the most recent assistant turn, so
// every "tool" message before that turn belongs to a superseded round and is
// replaced with a placeholder. Returns the number of characters removed.
func pruneSupersededGatewayToolResults(history []GatewayMessage) int64 {
	lastAssistant := -1
	for i := range history {
		if history[i].Role == "assistant" {
			lastAssistant = i
		}
	}
	if lastAssistant <= 0 {
		return 0
	}
	var removed int64
	for i := 0; i < lastAssistant; i++ {
		if history[i].Role != "tool" {
			continue
		}
		body := history[i].Content
		if body == "" || isPrunedToolResult(body) {
			continue
		}
		removed += int64(len(body))
		history[i].Content = prunedToolResultPlaceholder(len(body))
	}
	return removed
}

// pruneSupersededAnthropicToolResults prunes the Anthropic-native transcript.
// Tool results live as tool_result content blocks inside user-role turns; the
// latest round's results follow the most recent assistant turn, so any
// tool_result text in a user turn before that assistant turn is superseded.
// Returns the number of characters removed.
func pruneSupersededAnthropicToolResults(history []AnthropicMessage) int64 {
	lastAssistant := -1
	for i := range history {
		if history[i].Role == "assistant" {
			lastAssistant = i
		}
	}
	if lastAssistant <= 0 {
		return 0
	}
	var removed int64
	for i := 0; i < lastAssistant; i++ {
		if history[i].Role != "user" {
			continue
		}
		blocks, ok := history[i].Content.([]AnthropicContentBlock)
		if !ok {
			continue
		}
		for bi := range blocks {
			if blocks[bi].Type != "tool_result" {
				continue
			}
			removed += pruneAnthropicToolResultBlock(&blocks[bi])
		}
		history[i].Content = blocks
	}
	return removed
}

// pruneAnthropicToolResultBlock replaces the text of a single tool_result block
// with a placeholder, returning the characters removed. A tool_result block
// carries its body as nested text content blocks.
func pruneAnthropicToolResultBlock(b *AnthropicContentBlock) int64 {
	var removed int64
	for ci := range b.Content {
		if b.Content[ci].Type != "text" {
			continue
		}
		txt := b.Content[ci].Text
		if txt == "" || isPrunedToolResult(txt) {
			continue
		}
		removed += int64(len(txt))
		b.Content[ci].Text = prunedToolResultPlaceholder(len(txt))
	}
	return removed
}
