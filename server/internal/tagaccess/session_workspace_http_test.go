package tagaccess_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/tagaccess"
)

func TestSessionWorkspaceHTTPIngressAcceptsExactVIBESFixtureAndRejectsTamper(t *testing.T) {
	fixture := loadSessionWorkspaceFixture(t)
	key, _ := base64.StdEncoding.DecodeString(fixture.TestKeyBase64)
	mac, _ := base64.StdEncoding.DecodeString(fixture.MACBase64)
	fixture.UnsignedEnvelope.Authentication = tagaccess.AuthorityEnvelopeAuthentication{KeyID: fixture.KeyID, MAC: mac}
	access, err := tagaccess.NewAuthenticatedAccess(
		tagaccess.NewMemoryStore(), fixedClock{now: time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)},
		map[string][]byte{fixture.KeyID: key}, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	ingress, err := tagaccess.NewHTTPIngress(access)
	if err != nil {
		t.Fatal(err)
	}
	deliver := func(envelope tagaccess.SessionWorkspaceSupersededEnvelope) *httptest.ResponseRecorder {
		body, err := json.Marshal(envelope)
		if err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest(http.MethodPost, "/internal/tag-authority/session-workspace-supersessions", bytes.NewReader(body))
		response := httptest.NewRecorder()
		ingress.SessionWorkspace(response, request)
		return response
	}
	accepted := deliver(fixture.UnsignedEnvelope)
	if accepted.Code != http.StatusOK {
		t.Fatalf("exact fixture status = %d, body = %s", accepted.Code, accepted.Body.String())
	}
	var receipt struct {
		Source string                                 `json:"source"`
		Apply  tagaccess.SessionWorkspaceApplyReceipt `json:"apply"`
	}
	if err := json.Unmarshal(accepted.Body.Bytes(), &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.Source != "session_workspace" || receipt.Apply.PayloadDigest != fixture.PayloadSHA256 || receipt.Apply.Result != tagaccess.ApplyGap {
		t.Fatalf("receipt = %#v", receipt)
	}
	tampered := fixture.UnsignedEnvelope
	tampered.Delivery.NewWorkspaceID = "workspace-attacker"
	denied := deliver(tampered)
	if denied.Code != http.StatusUnauthorized {
		t.Fatalf("tampered fixture status = %d, body = %s", denied.Code, denied.Body.String())
	}
}
