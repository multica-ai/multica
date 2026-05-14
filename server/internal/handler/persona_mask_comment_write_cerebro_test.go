// JEH-1188: unit tests for the comment-write redaction paths
// (CreateComment / UpdateComment) and the new Classification field on
// CommentResponse. Tests live in-package to reuse the stubMaskInvoker
// + stubAuditWriter pattern from persona_mask_resources_cerebro_test.go.

package handler

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestCommentToResponse_ExposesClassification_DefaultInternal(t *testing.T) {
	// No-Classification row → response carries "internal".
	c := db.Comment{Content: "hi", AuthorType: "member"}
	resp := commentToResponse(c, nil, nil)
	if resp.Classification != "internal" {
		t.Errorf("default classification should normalise to %q, got %q", "internal", resp.Classification)
	}
}

func TestCommentToResponse_ExposesClassification_PiiPassesThrough(t *testing.T) {
	c := db.Comment{Content: "private", AuthorType: "member", Classification: "pii"}
	resp := commentToResponse(c, nil, nil)
	if resp.Classification != "pii" {
		t.Errorf("pii classification must pass through to response, got %q", resp.Classification)
	}
}

func TestCommentFieldClasses_ReturnsNilForInternalRow(t *testing.T) {
	if commentFieldClasses("internal") != nil {
		t.Errorf("internal rows carry no field-level pii signal; want nil map")
	}
	if commentFieldClasses("public") != nil {
		t.Errorf("public rows carry no field-level pii signal; want nil map")
	}
}

func TestCommentFieldClasses_PiiRowTagsContent(t *testing.T) {
	m := commentFieldClasses("pii")
	if m == nil || m[personaFieldContent] != "pii" {
		t.Errorf("pii row must tag content with its classification, got %v", m)
	}
}

func TestMaskSingleCommentForCaller_DenyZeroesAndWrites404(t *testing.T) {
	h := &Handler{
		PersonaMask: &stubMaskInvoker{decisions: []PersonaMaskDecision{
			{Allowed: false, Reason: "no read grant"},
		}},
		PersonaMaskAudit: &stubAuditWriter{},
	}
	w := httptest.NewRecorder()
	c := db.Comment{
		ID:             pgtype.UUID{Bytes: [16]byte{1}, Valid: true},
		WorkspaceID:    pgtype.UUID{Bytes: [16]byte{2}, Valid: true},
		Classification: "pii",
		Content:        "private",
	}
	resp := commentToResponse(c, nil, nil)
	ok := h.maskSingleCommentForCaller(w, newReqWithUser("u-member"), c, &resp)
	if ok {
		t.Fatalf("deny must return false so caller exits")
	}
	if w.Code != 404 {
		t.Errorf("deny must write 404 (consistent with comment-list deny-drop semantics), got %d", w.Code)
	}
}

func TestMaskSingleCommentForCaller_AllowWithContentMasked(t *testing.T) {
	h := &Handler{
		PersonaMask: &stubMaskInvoker{decisions: []PersonaMaskDecision{
			{Allowed: true, MaskedFields: []string{"content"}, DecisionID: "d-1"},
		}},
		PersonaMaskAudit: &stubAuditWriter{},
	}
	w := httptest.NewRecorder()
	c := db.Comment{
		ID:             pgtype.UUID{Bytes: [16]byte{1}, Valid: true},
		WorkspaceID:    pgtype.UUID{Bytes: [16]byte{2}, Valid: true},
		Classification: "pii",
		Content:        "private",
	}
	resp := commentToResponse(c, nil, nil)
	ok := h.maskSingleCommentForCaller(w, newReqWithUser("u-member"), c, &resp)
	if !ok {
		t.Fatalf("allow outcome must return true")
	}
	if resp.Content != nil {
		t.Errorf("content should be nil on mask (serialises as `null` per JEH-1215), got %q", *resp.Content)
	}
	if resp.Classification != "pii" {
		t.Errorf("classification field stays exposed so UI can render the badge, got %q", resp.Classification)
	}
}

// JEH-1215 regression: a masked comment must serialise as
// `"content": null`, not `"content": ""`. The previous `string`-typed
// field forced the empty-string overload, which the UI cannot
// distinguish from a legitimately empty body. This test pins the
// JSON-on-the-wire shape so a future revert of `*string` → `string`
// trips immediately.
func TestApplyCommentMask_JSONSerialisesContentAsNull(t *testing.T) {
	c := db.Comment{Content: "private", Classification: "pii"}
	resp := commentToResponse(c, nil, nil)
	applyCommentMask(&resp, []string{"content"})

	out, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	body := string(out)
	if !strings.Contains(body, `"content":null`) {
		t.Errorf("masked comment must serialise content as null (JEH-1215), got %s", body)
	}
	if strings.Contains(body, `"content":""`) {
		t.Errorf("masked comment must NOT serialise content as empty string (JEH-1215), got %s", body)
	}
}

// JEH-1215 regression: an unmasked comment must round-trip its content
// unchanged. Pins that the *string field doesn't accidentally null
// non-redacted rows.
func TestCommentToResponse_UnmaskedContentRoundTripsAsString(t *testing.T) {
	c := db.Comment{Content: "hello world", Classification: "internal"}
	resp := commentToResponse(c, nil, nil)
	if resp.Content == nil {
		t.Fatalf("unmasked content must not be nil")
	}
	if *resp.Content != "hello world" {
		t.Errorf("content must round-trip unchanged, got %q", *resp.Content)
	}
	out, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	if !strings.Contains(string(out), `"content":"hello world"`) {
		t.Errorf("unmasked comment must serialise content as the original string, got %s", string(out))
	}
}
