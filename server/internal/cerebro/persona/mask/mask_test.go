package mask

import (
	"context"
	"errors"
	"testing"

	persona "github.com/hvejsel/firtal-persona/sdk/go"
)

// fakeClient is a CheckClient that captures the args and returns canned
// results. Lets us assert what the mask layer sent to persona and what
// it did with the response.
type fakeClient struct {
	resp    *persona.CheckResult
	err     error
	gotKind string
	gotID   string
	gotCls  string
	gotMap  map[string]string
}

func (f *fakeClient) CheckResourceWithMasking(
	_ context.Context,
	_, _, kind, id, classification string,
	fieldClassifications map[string]string,
	_ map[string]any,
) (*persona.CheckResult, error) {
	f.gotKind = kind
	f.gotID = id
	f.gotCls = classification
	f.gotMap = fieldClassifications
	return f.resp, f.err
}

func TestDecide_NoClient_FullAccess(t *testing.T) {
	c := NewChecker(nil, nil)
	d := c.Decide(context.Background(), "actor", "read", KindMember, "id", "internal", FieldClassifications(KindMember))
	if !d.Allowed {
		t.Fatalf("nil client should yield full access, got Allowed=false")
	}
	if len(d.MaskedFields) != 0 {
		t.Errorf("nil client should produce no mask, got %v", d.MaskedFields)
	}
}

func TestDecide_DefaultClassification(t *testing.T) {
	f := &fakeClient{resp: &persona.CheckResult{Allowed: true}}
	c := NewChecker(f, nil)
	// Empty rowClassification must promote to DefaultClassification.
	c.Decide(context.Background(), "actor", "read", KindMember, "id", "", nil)
	if f.gotCls != DefaultClassification {
		t.Errorf("want default %q sent to persona, got %q", DefaultClassification, f.gotCls)
	}
}

func TestDecide_PassesFieldClassifications(t *testing.T) {
	f := &fakeClient{resp: &persona.CheckResult{Allowed: true, MaskedFields: []string{"email"}}}
	c := NewChecker(f, nil)
	d := c.Decide(context.Background(), "actor", "read", KindMember, "id", "internal", FieldClassifications(KindMember))
	if f.gotMap == nil || f.gotMap["email"] != "pii" {
		t.Errorf("expected field map to carry email=pii, got %v", f.gotMap)
	}
	if len(d.MaskedFields) != 1 || d.MaskedFields[0] != "email" {
		t.Errorf("expected MaskedFields=[email], got %v", d.MaskedFields)
	}
}

func TestDecide_FailClosed_OnError(t *testing.T) {
	f := &fakeClient{err: errors.New("persona unreachable")}
	c := NewChecker(f, nil)
	d := c.Decide(context.Background(), "actor", "read", KindMember, "id", "internal", nil)
	if d.Allowed {
		t.Fatalf("expected fail-closed on transport error, got Allowed=true")
	}
}

func TestFieldClassifications_StaticMap(t *testing.T) {
	m := FieldClassifications(KindMember)
	if m["email"] != "pii" || m["phone"] != "pii" {
		t.Errorf("member field map missing pii labels, got %v", m)
	}

	// Mutating the returned map must not affect the constant.
	m["email"] = "public"
	again := FieldClassifications(KindMember)
	if again["email"] != "pii" {
		t.Errorf("returned map should be defensive copy; constant got mutated to %q", again["email"])
	}

	if FieldClassifications(KindAttachment) != nil {
		t.Errorf("kinds without per-field labels should return nil, got %v", FieldClassifications(KindAttachment))
	}
}

func TestApplyToJSON_RedactsListedFields(t *testing.T) {
	payload := map[string]any{
		"id":    "m1",
		"name":  "Mia",
		"email": "mia@example.com",
	}
	ApplyToJSON(payload, []string{"email"})
	if payload["email"] != nil {
		t.Errorf("email should be nil after redaction, got %v", payload["email"])
	}
	if payload["name"] != "Mia" {
		t.Errorf("non-listed fields must remain intact, got name=%v", payload["name"])
	}
	// Unknown field: no-op.
	ApplyToJSON(payload, []string{"address"})
	if _, ok := payload["address"]; ok {
		t.Errorf("missing field must not be inserted as nil, got %v", payload)
	}
}
