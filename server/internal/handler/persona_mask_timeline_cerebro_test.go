// JEH-1216: unit tests for the timeline embedded-comment mask helper.
// Mirrors persona_mask_comment_write_cerebro_test.go — same stubs, same
// shape, but the assertion target is TimelineEntry.Content (*string).

package handler

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestApplyCommentEntryMask_ContentSetToNil(t *testing.T) {
	body := "private"
	entry := TimelineEntry{Type: "comment", Content: &body}
	applyCommentEntryMask(&entry, []string{"content"})
	if entry.Content != nil {
		t.Errorf("masked content should be nil (matches *string omitempty contract), got %v", entry.Content)
	}
}

func TestMaskCommentEntriesForCaller_NoInvoker_PassThrough(t *testing.T) {
	h := &Handler{}
	body := "hi"
	entries := []TimelineEntry{{Type: "comment", Content: &body}}
	comments := []db.Comment{{ID: pgtype.UUID{Bytes: [16]byte{1}, Valid: true}, Content: "hi"}}
	out := h.maskCommentEntriesForCaller(newReqWithUser(""), db.Issue{}, comments, entries)
	if len(out) != 1 || out[0].Content == nil || *out[0].Content != "hi" {
		t.Errorf("no invoker must preserve entries unchanged, got %+v", out)
	}
}

func TestMaskCommentEntriesForCaller_AllowWithContentMasked(t *testing.T) {
	audit := &stubAuditWriter{}
	h := &Handler{
		PersonaMask: &stubMaskInvoker{decisions: []PersonaMaskDecision{
			{Allowed: true, MaskedFields: []string{"content"}, DecisionID: "d-1"},
		}},
		PersonaMaskAudit: audit,
	}
	body := "private"
	entries := []TimelineEntry{{Type: "comment", Content: &body}}
	comments := []db.Comment{{
		ID:             pgtype.UUID{Bytes: [16]byte{1}, Valid: true},
		WorkspaceID:    pgtype.UUID{Bytes: [16]byte{2}, Valid: true},
		Classification: "pii",
		Content:        "private",
	}}
	out := h.maskCommentEntriesForCaller(newReqWithUser("u1"), db.Issue{WorkspaceID: comments[0].WorkspaceID}, comments, entries)
	if len(out) != 1 {
		t.Fatalf("allow-with-mask must keep the entry, got %d entries", len(out))
	}
	if out[0].Content != nil {
		t.Errorf("masked content must be nil (omitempty drops it from JSON), got %v", out[0].Content)
	}
	if len(audit.events) != 1 || audit.events[0].Decision != "mask" {
		t.Errorf("expected one 'mask' audit event, got %+v", audit.events)
	}
}

func TestMaskCommentEntriesForCaller_DenyDropsEntry(t *testing.T) {
	audit := &stubAuditWriter{}
	h := &Handler{
		PersonaMask: &stubMaskInvoker{decisions: []PersonaMaskDecision{
			{Allowed: false, Reason: "no read grant"},
		}},
		PersonaMaskAudit: audit,
	}
	body := "private"
	entries := []TimelineEntry{{Type: "comment", Content: &body}}
	comments := []db.Comment{{
		ID:             pgtype.UUID{Bytes: [16]byte{1}, Valid: true},
		WorkspaceID:    pgtype.UUID{Bytes: [16]byte{2}, Valid: true},
		Classification: "pii",
		Content:        "private",
	}}
	out := h.maskCommentEntriesForCaller(newReqWithUser("u1"), db.Issue{WorkspaceID: comments[0].WorkspaceID}, comments, entries)
	if len(out) != 0 {
		t.Errorf("deny must drop the entry from the timeline (same semantics as ListComments), got %d entries", len(out))
	}
	if len(audit.events) != 1 || audit.events[0].Decision != "deny" {
		t.Errorf("expected one 'deny' audit event, got %+v", audit.events)
	}
}

func TestMaskCommentEntriesForCaller_MixedDecisionsKeepActivitiesInvariant(t *testing.T) {
	// Two comment rows: first allowed, second denied. The helper only
	// operates on comment entries, so any activity entries the caller
	// appended afterwards must remain untouched — guarded by the
	// "len(entries) == len(comments)" precondition: deny drops the
	// entire entry from the slice, mask zeros in-place.
	h := &Handler{
		PersonaMask: &stubMaskInvoker{decisions: []PersonaMaskDecision{
			{Allowed: true},                                 // row 0: keep, no mask
			{Allowed: true, MaskedFields: []string{"content"}}, // row 1: keep, content nil
			{Allowed: false},                                // row 2: drop
		}},
		PersonaMaskAudit: &stubAuditWriter{},
	}
	b0, b1, b2 := "first", "second", "third"
	entries := []TimelineEntry{
		{Type: "comment", Content: &b0},
		{Type: "comment", Content: &b1},
		{Type: "comment", Content: &b2},
	}
	comments := []db.Comment{
		{ID: pgtype.UUID{Bytes: [16]byte{1}, Valid: true}, Content: "first"},
		{ID: pgtype.UUID{Bytes: [16]byte{2}, Valid: true}, Content: "second", Classification: "pii"},
		{ID: pgtype.UUID{Bytes: [16]byte{3}, Valid: true}, Content: "third", Classification: "pii"},
	}
	out := h.maskCommentEntriesForCaller(newReqWithUser("u1"), db.Issue{}, comments, entries)
	if len(out) != 2 {
		t.Fatalf("two allowed entries should remain, got %d", len(out))
	}
	if out[0].Content == nil || *out[0].Content != "first" {
		t.Errorf("row 0 should keep its content untouched, got %v", out[0].Content)
	}
	if out[1].Content != nil {
		t.Errorf("row 1 should have content nil after mask, got %v", out[1].Content)
	}
}
