// JEH-1186: unit tests for the per-row user mask helper used by the
// auth/me + login response paths.
//
// Lives in-package so we can use the stub PersonaMaskInvoker that
// persona_mask_members_cerebro_test.go and persona_mask_resources_cerebro_test.go
// already define. The tests cover the four scenarios called out in the
// acceptance criteria:
//   - persona not configured  → full payload preserved (pre-JEH-1186 baseline)
//   - allow + no mask         → email present (pii-grant actor)
//   - allow + email mask      → email blanked (internal-grant actor)
//   - deny                    → email blanked (defensive zero on mis-config)

package handler

import (
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func newUserRow(id, email string) db.User {
	uid, _ := pgtype.UUID{}, false
	_ = uid
	row := db.User{Email: email, Name: "Test"}
	// db.User.ID is pgtype.UUID; the helper only reads it via uuidToString
	// for the persona resource_id, so any deterministic value works. Build
	// one from a fixed 16-byte pattern.
	var raw [16]byte
	for i := range raw {
		raw[i] = byte(i)
	}
	row.ID = pgtype.UUID{Bytes: raw, Valid: true}
	_ = id
	return row
}

func TestMaskUserForCaller_NoInvoker(t *testing.T) {
	h := &Handler{}
	resp := UserResponse{ID: "u1", Email: "mia@example.com", Name: "Mia"}
	h.maskUserForCaller(httptest.NewRequest("GET", "/me", nil), newUserRow("u1", "mia@example.com"), &resp)
	if resp.Email != "mia@example.com" {
		t.Fatalf("no invoker should preserve email, got %q", resp.Email)
	}
}

func TestMaskUserForCaller_AllowWithoutMask_PreservesEmail(t *testing.T) {
	audit := &stubAuditWriter{}
	h := &Handler{
		PersonaMask:      &stubMaskInvoker{decisions: []PersonaMaskDecision{{Allowed: true}}},
		PersonaMaskAudit: audit,
	}
	resp := UserResponse{ID: "u1", Email: "mia@example.com", Name: "Mia"}
	h.maskUserForCaller(newReqWithUser("u1"), newUserRow("u1", "mia@example.com"), &resp)
	if resp.Email != "mia@example.com" {
		t.Errorf("pii-grant actor should see email, got %q", resp.Email)
	}
	if len(audit.events) != 0 {
		t.Errorf("allow-with-no-mask should not write to audit ledger, got %d events", len(audit.events))
	}
}

func TestMaskUserForCaller_AllowWithEmailMasked(t *testing.T) {
	audit := &stubAuditWriter{}
	h := &Handler{
		PersonaMask: &stubMaskInvoker{decisions: []PersonaMaskDecision{
			{Allowed: true, MaskedFields: []string{"email"}, DecisionID: "d-1"},
		}},
		PersonaMaskAudit: audit,
	}
	resp := UserResponse{ID: "u1", Email: "mia@example.com", Name: "Mia"}
	h.maskUserForCaller(newReqWithUser("u1"), newUserRow("u1", "mia@example.com"), &resp)
	if resp.Email != "" {
		t.Errorf("internal-grant actor should see email blanked, got %q", resp.Email)
	}
	if resp.Name != "Mia" {
		t.Errorf("name must survive — only email is pii on UserResponse today, got %q", resp.Name)
	}
	if len(audit.events) != 1 || audit.events[0].Decision != "mask" {
		t.Errorf("mask outcome must write one audit row with decision=mask, got %+v", audit.events)
	}
	if audit.events[0].ResourceKind != personaKindUser {
		t.Errorf("audit row must record resource kind %q, got %q", personaKindUser, audit.events[0].ResourceKind)
	}
}

func TestMaskUserForCaller_DenyZeroesEmailDefensively(t *testing.T) {
	audit := &stubAuditWriter{}
	h := &Handler{
		PersonaMask: &stubMaskInvoker{decisions: []PersonaMaskDecision{
			{Allowed: false, Reason: "no read grant"},
		}},
		PersonaMaskAudit: audit,
	}
	resp := UserResponse{ID: "u1", Email: "mia@example.com", Name: "Mia"}
	h.maskUserForCaller(newReqWithUser("u1"), newUserRow("u1", "mia@example.com"), &resp)
	if resp.Email != "" {
		t.Errorf("deny on self-read should zero email defensively, got %q", resp.Email)
	}
	if resp.Name != "Mia" {
		t.Errorf("deny must not strip non-pii fields on self-read, got %q", resp.Name)
	}
	if len(audit.events) != 1 || audit.events[0].Decision != "deny" {
		t.Errorf("deny outcome must write one audit row with decision=deny, got %+v", audit.events)
	}
}
