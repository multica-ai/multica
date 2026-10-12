package wecom

// inbox_enrichment.go — the change-detail body that buildInboxMarkdown does
// not see for status_changed / assignee_changed / unassigned / issue_assigned.
//
// InboxTypeLabels in strings.go already gives each of these types a category
// label ("状态变更" / "Status changed"), and buildInboxMarkdown wraps it around
// the issue title as the card header. A category label without the delta,
// though, reads as "something changed" with no idea what actually changed —
// exactly the gap the user reports. The server DOES publish the delta: the
// inbox item's `details` JSON carries `{"from": prevStatus, "to": newStatus}`
// for status changes (notification_listeners.go:793-796) and prev/new
// assignee IDs for assignee changes (notification_listeners.go:735-747). The
// in-app inbox renders both via its own detail UI
// (packages/views/inbox/components/inbox-detail-label.tsx). The WeCom card
// was ignoring that field.
//
// This file fills the gap: synthesise a one-line body
// (状态: todo -> done, 指派人: 张三 -> 李四) from `details` for those types,
// and hand the enriched item to buildInboxMarkdown. The body line lives in
// the same field as member-authored text — same truncation, same
// breakMemberLinks safety — so no new format surface is introduced, and the
// existing buildInboxMarkdown tests (which construct status_changed items
// with a manually-crafted body) keep passing unchanged because we never
// override a non-empty body.

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/multica-ai/multica/server/internal/util"
)

// enrichInboxItemBody synthesises the change-detail body for inbox
// notifications whose body would otherwise be empty.
//
// Returns "" in every case where the caller should leave the body alone:
// the item already has a body, the type is not one this adapter knows how
// to render as a detail line, the details JSON is missing or empty, or the
// delta fields it expects are all empty. The caller copies the result into
// item["body"] only when it is non-empty.
//
// Never returns an error. A name the DB can't resolve, an unparseable
// details JSON, an unrecognised type — all degrade to the previous shape
// (empty body, title-only card). A missing detail line is a cosmetic loss;
// the notification still arrives, and the in-app inbox still shows the
// full delta. Dropping the whole push over a lookup miss would be the
// wrong trade.
func enrichInboxItemBody(ctx context.Context, q languageLookup, item map[string]any, c copyPack) string {
	if inboxItemBody(item) != "" {
		return "" // member-authored body wins
	}
	typeStr, _ := item["type"].(string)
	label := c.changeFieldLabel(typeStr)
	if label == "" {
		return ""
	}

	from, to, ok := extractDelta(item, typeStr)
	if !ok {
		return ""
	}

	// Assignee deltas carry user UUIDs, not display names. Translate them
	// through the DB so the card shows "张三" instead of a 36-character hex
	// string. Status deltas are already human-readable status keys
	// ("todo", "in_review", "done") and are passed through as-is — the same
	// keys the in-app inbox renders, and the same ones a status-catalog
	// translation would have to look up by, so localising here would only
	// restate what the frontend already does.
	if typeStr == "assignee_changed" || typeStr == "unassigned" || typeStr == "issue_assigned" {
		from = resolveAssigneeName(ctx, q, from, c.UnassignedValue)
		to = resolveAssigneeName(ctx, q, to, c.UnassignedValue)
	}

	if from == "" && to == "" {
		return ""
	}

	return formatChangeLine(label, from, to)
}

// extractDelta parses the details JSON for a change-type notification and
// returns the (from, to) pair. `details` on an inbox item is either a JSON
// byte slice (the shape notification_listeners.go writes via json.Marshal)
// or absent, and both are handled.
//
// The JSON shape is per-type — the writer in notification_listeners.go
// chooses the field names, so this reads them:
//
//	status_changed:          {"from": "todo", "to": "done"}
//	assignee_changed:        {"prev_assignee_id": "...", "new_assignee_id": "..."}
//	unassigned:              {"prev_assignee_id": "..."}
//	issue_assigned:          {"new_assignee_id": "..."}
//
// ok=false means "nothing useful was here", not "there was an error": a
// null/empty/{} details, an unparseable payload, or a shape with neither
// from nor to. The caller treats ok=false the same as "no detail line".
func extractDelta(item map[string]any, typeStr string) (from, to string, ok bool) {
	raw, hasRaw := item["details"].(json.RawMessage)
	if !hasRaw || len(raw) == 0 {
		return "", "", false
	}
	s := string(raw)
	if s == "null" || s == "{}" {
		return "", "", false
	}

	// The details map is written by notification_listeners.go as
	// map[string]string, so json.Unmarshal into map[string]string is the
	// shape that never fails on the field type. A different type — added
	// by a future writer that didn't read the existing comment on
	// detailsMap — fails here and reads as "no delta", which is the safe
	// failure mode (the card still shows the title).
	var d map[string]string
	if err := json.Unmarshal(raw, &d); err != nil {
		return "", "", false
	}

	switch typeStr {
	case "status_changed":
		return d["from"], d["to"], true
	case "assignee_changed", "unassigned", "issue_assigned":
		return d["prev_assignee_id"], d["new_assignee_id"], true
	}
	return "", "", false
}

// resolveAssigneeName turns a user UUID into the member's display name.
// Missing IDs, unparseable UUIDs, and DB lookup failures all fall back to
// the `fallback` string (typically "未指派" / "Unassigned") rather than
// error. A nameless card is a cosmetic issue, but a dropped notification is
// a lost one, and this is the wrong seam to fail hard over.
//
// q is nil-tolerant on purpose: tests and single-node deployments that
// haven't wired the query seam still get a card, just without names.
func resolveAssigneeName(ctx context.Context, q languageLookup, idStr, fallback string) string {
	idStr = strings.TrimSpace(idStr)
	if idStr == "" || q == nil {
		return fallback
	}
	id, err := util.ParseUUID(idStr)
	if err != nil || !id.Valid {
		return fallback
	}
	u, err := q.GetUser(ctx, id)
	if err != nil {
		return fallback
	}
	if name := strings.TrimSpace(u.Name); name != "" {
		return name
	}
	return fallback
}

// formatChangeLine composes the body line `label: from -> to`. The literal
// punctuation (": " and " -> ") is not Chinese, so it does not trip the
// language lint in language_lint_test.go — that only flags Han characters.
// Keeping it as a literal rather than a copyPack field is deliberate: it
// is the punctuation of the language, not the copy, and neither locale
// reads it differently.
func formatChangeLine(label, from, to string) string {
	if from == "" && to == "" {
		return ""
	}
	return label + ": " + from + " -> " + to
}

// enrichInboxItem returns an item map with the change-detail body filled in
// when it applies, else the item unchanged. It uses a shallow copy of the
// original map so the event payload is not mutated — other subscribers to
// inbox:new (the WebSocket broadcaster in cmd/server/listeners.go, in
// particular) read the same map concurrently and must see the original
// shape. The in-app inbox has its own detail rendering from `details` and
// would not want WeCom's body synthesis leaking through the WS payload.
//
// The copy is shallow: it shares the values of the other fields (title,
// issue_id, details, ...) with the original. That is correct — we are only
// replacing body, and the other values are read-only downstream. The copy
// is allocated only when enrichment actually applies; the common case
// (non-change type, or non-empty body) returns the original map.
func enrichInboxItem(ctx context.Context, q languageLookup, item map[string]any, c copyPack) map[string]any {
	body := enrichInboxItemBody(ctx, q, item, c)
	if body == "" {
		return item
	}
	out := make(map[string]any, len(item)+1)
	for k, v := range item {
		out[k] = v
	}
	out["body"] = body
	return out
}
