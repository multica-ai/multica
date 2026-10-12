package wecom

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// fakeUsers is a languageLookup with a name-by-UUID table for the tests.
// GetUser returns a db.User{ID, Name} for an id it has seen, and pgx.ErrNoRows
// for anything else, which is the failure mode the enrichment treats as
// "fall back to UnassignedValue".
type fakeUsers struct {
	names map[string]string
}

func (f fakeUsers) GetChannelUserBindingByUserID(_ context.Context, _ db.GetChannelUserBindingByUserIDParams) (db.ChannelUserBinding, error) {
	return db.ChannelUserBinding{}, nil
}

func (f fakeUsers) GetUser(_ context.Context, id pgtype.UUID) (db.User, error) {
	name, ok := f.names[util.UUIDToString(id)]
	if !ok {
		return db.User{}, nil // zero value reads as "no name" — the fallback
	}
	return db.User{ID: id, Name: name}, nil
}

// detailJSON marshals a string->string map into the json.RawMessage shape
// notification_listeners.go puts on item["details"]. A nil m produces "null",
// which the enrichment treats as "no delta" — that is the shape an inbox item
// without details actually carries.
func detailJSON(t *testing.T, m map[string]string) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal details: %v", err)
	}
	return b
}

func TestEnrichInboxItemBody_StatusChanged(t *testing.T) {
	item := map[string]any{
		"type":    "status_changed",
		"details": detailJSON(t, map[string]string{"from": "todo", "to": "done"}),
	}
	got := enrichInboxItemBody(context.Background(), nil, item, copyFor(LocaleZhHans))
	want := "状态: todo -> done"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}

	// English pack picks up its own label.
	got = enrichInboxItemBody(context.Background(), nil, item, copyFor(LocaleEn))
	want = "Status: todo -> done"
	if got != want {
		t.Fatalf("en: got %q, want %q", got, want)
	}
}

func TestEnrichInboxItemBody_AssigneeChanged(t *testing.T) {
	prevID := mustUUID("11111111-1111-1111-1111-111111111111")
	newID := mustUUID("22222222-2222-2222-2222-222222222222")
	q := fakeUsers{names: map[string]string{
		util.UUIDToString(prevID): "张三",
		util.UUIDToString(newID):  "李四",
	}}
	item := map[string]any{
		"type": "assignee_changed",
		"details": detailJSON(t, map[string]string{
			"prev_assignee_id": util.UUIDToString(prevID),
			"new_assignee_id":  util.UUIDToString(newID),
		}),
	}
	got := enrichInboxItemBody(context.Background(), q, item, copyFor(LocaleZhHans))
	want := "指派人: 张三 -> 李四"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestEnrichInboxItemBody_UnassignedFallsBackToUnassignedValue(t *testing.T) {
	prevID := mustUUID("11111111-1111-1111-1111-111111111111")
	q := fakeUsers{names: map[string]string{
		util.UUIDToString(prevID): "张三",
	}}
	item := map[string]any{
		"type": "unassigned",
		"details": detailJSON(t, map[string]string{
			"prev_assignee_id": util.UUIDToString(prevID),
		}),
	}
	got := enrichInboxItemBody(context.Background(), q, item, copyFor(LocaleZhHans))
	want := "指派人: 张三 -> 未指派"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}

	got = enrichInboxItemBody(context.Background(), q, item, copyFor(LocaleEn))
	want = "Assignee: 张三 -> Unassigned"
	if got != want {
		t.Fatalf("en: got %q, want %q", got, want)
	}
}

func TestEnrichInboxItemBody_IssueAssignedFallsBackToUnassignedValue(t *testing.T) {
	newID := mustUUID("22222222-2222-2222-2222-222222222222")
	q := fakeUsers{names: map[string]string{
		util.UUIDToString(newID): "李四",
	}}
	item := map[string]any{
		"type": "issue_assigned",
		"details": detailJSON(t, map[string]string{
			"new_assignee_id": util.UUIDToString(newID),
		}),
	}
	got := enrichInboxItemBody(context.Background(), q, item, copyFor(LocaleZhHans))
	want := "指派人: 未指派 -> 李四"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestEnrichInboxItemBody_PreservesMemberAuthoredBody(t *testing.T) {
	// A body that arrived non-empty must win over any enrichment, whatever
	// the type or details. This is the invariant that keeps the existing
	// buildInboxMarkdown tests (which construct status_changed items with a
	// manually-crafted body) passing unchanged.
	item := map[string]any{
		"type":    "status_changed",
		"body":    "from: todo\nto: done",
		"details": detailJSON(t, map[string]string{"from": "todo", "to": "done"}),
	}
	if got := enrichInboxItemBody(context.Background(), nil, item, copyFor(LocaleZhHans)); got != "" {
		t.Fatalf("enrichment must not override a member-authored body, got %q", got)
	}

	// Pointer body is treated the same way.
	body := "手动写的"
	item = map[string]any{
		"type": "status_changed",
		"body": &body,
	}
	if got := enrichInboxItemBody(context.Background(), nil, item, copyFor(LocaleZhHans)); got != "" {
		t.Fatalf("enrichment must not override a pointer body, got %q", got)
	}
}

func TestEnrichInboxItemBody_MissingDetailsReturnsEmpty(t *testing.T) {
	item := map[string]any{"type": "status_changed"}
	if got := enrichInboxItemBody(context.Background(), nil, item, copyFor(LocaleZhHans)); got != "" {
		t.Fatalf("got %q, want empty when details is missing", got)
	}

	// Empty map is the shape notification_listeners.go writes when the
	// caller passed emptyDetails.
	item = map[string]any{
		"type":    "status_changed",
		"details": json.RawMessage(`{}`),
	}
	if got := enrichInboxItemBody(context.Background(), nil, item, copyFor(LocaleZhHans)); got != "" {
		t.Fatalf("got %q, want empty when details is {}", got)
	}

	// null details.
	item = map[string]any{
		"type":    "status_changed",
		"details": json.RawMessage(`null`),
	}
	if got := enrichInboxItemBody(context.Background(), nil, item, copyFor(LocaleZhHans)); got != "" {
		t.Fatalf("got %q, want empty when details is null", got)
	}
}

func TestEnrichInboxItemBody_UnknownTypeReturnsEmpty(t *testing.T) {
	item := map[string]any{
		"type":    "mentioned",
		"details": detailJSON(t, map[string]string{"from": "a", "to": "b"}),
	}
	if got := enrichInboxItemBody(context.Background(), nil, item, copyFor(LocaleZhHans)); got != "" {
		t.Fatalf("got %q, want empty for a type this adapter does not render as a detail line", got)
	}
}

func TestEnrichInboxItemBody_UnresolvableAssigneeFallsBack(t *testing.T) {
	// A UUID the DB cannot resolve (deleted user, DB error, or an id that
	// is not a UUID at all) falls back to UnassignedValue rather than
	// erroring. The card still goes out.
	prevID := mustUUID("11111111-1111-1111-1111-111111111111")
	q := fakeUsers{names: map[string]string{}} // no names at all
	item := map[string]any{
		"type": "assignee_changed",
		"details": detailJSON(t, map[string]string{
			"prev_assignee_id": util.UUIDToString(prevID),
			"new_assignee_id":  "33333333-3333-3333-3333-333333333333",
		}),
	}
	got := enrichInboxItemBody(context.Background(), q, item, copyFor(LocaleZhHans))
	want := "指派人: 未指派 -> 未指派"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestEnrichInboxItemBody_NilQueryStillRendersStatusDelta(t *testing.T) {
	// Status deltas do not need a DB lookup, so a nil lookup still
	// produces a body. Assignee deltas degrade to the fallback.
	item := map[string]any{
		"type":    "status_changed",
		"details": detailJSON(t, map[string]string{"from": "todo", "to": "done"}),
	}
	got := enrichInboxItemBody(context.Background(), nil, item, copyFor(LocaleZhHans))
	want := "状态: todo -> done"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestEnrichInboxItem_ItemCopyNotMutated(t *testing.T) {
	prevID := mustUUID("11111111-1111-1111-1111-111111111111")
	newID := mustUUID("22222222-2222-2222-2222-222222222222")
	q := fakeUsers{names: map[string]string{
		util.UUIDToString(prevID): "张三",
		util.UUIDToString(newID):  "李四",
	}}
	original := map[string]any{
		"type":    "assignee_changed",
		"title":   "登录页 500 错误",
		"details": detailJSON(t, map[string]string{
			"prev_assignee_id": util.UUIDToString(prevID),
			"new_assignee_id":  util.UUIDToString(newID),
		}),
	}
	enriched := enrichInboxItem(context.Background(), q, original, copyFor(LocaleZhHans))

	// The original must not gain a body field — mutating it would leak
	// WeCom's synthesis into the WebSocket broadcaster's payload.
	if body, present := original["body"]; present {
		t.Fatalf("original item must not gain a body field, got %q", body)
	}
	if body, _ := enriched["body"].(string); body != "指派人: 张三 -> 李四" {
		t.Fatalf("enriched body got %q", body)
	}
	if enriched["title"] != "登录页 500 错误" {
		t.Fatalf("other fields must be preserved: %v", enriched["title"])
	}

	// No enrichment: the original item is returned unchanged — no body field,
	// same title. We do not assert pointer equality (Go forbids map ==), so
	// the assertion is that the enriched map has no body and matches the
	// original's shape.
	same := enrichInboxItem(context.Background(), q, map[string]any{
		"type": "mentioned", "title": "hi",
	}, copyFor(LocaleZhHans))
	if body, present := same["body"]; present {
		t.Fatalf("non-applicable enrichment must not add a body, got %q", body)
	}
	if same["title"] != "hi" {
		t.Fatalf("non-applicable enrichment must preserve title, got %v", same["title"])
	}
}

func TestEnrichInboxItem_FallsBackWhenEnrichmentYieldsEmpty(t *testing.T) {
	// Missing details: enrichInboxItemBody returns "", enrichInboxItem
	// must return the original item unchanged — no copy with an empty body.
	item := map[string]any{"type": "status_changed", "title": "hi"}
	enriched := enrichInboxItem(context.Background(), nil, item, copyFor(LocaleZhHans))
	if body, present := enriched["body"]; present {
		t.Fatalf("enrichInboxItem must not add a body when enrichment yields empty, got %q", body)
	}
	if enriched["title"] != "hi" {
		t.Fatalf("title must survive, got %v", enriched["title"])
	}
}
