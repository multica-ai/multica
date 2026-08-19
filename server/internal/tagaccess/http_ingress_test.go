package tagaccess_test

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/tagaccess"
)

func TestHTTPIngressDeliversSerializedWorkspaceEnvelope(t *testing.T) {
	key := []byte("vibes-authority-test-key-32-bytes-minimum")
	access, err := tagaccess.NewAuthenticatedAccess(
		tagaccess.NewMemoryStore(), fixedClock{now: time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)},
		map[string][]byte{"vibes-primary": key}, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := tagaccess.NewHTTPIngress(access)
	if err != nil {
		t.Fatal(err)
	}
	envelope := tagaccess.AuthorityEnvelope{
		SchemaVersion: 1, DeliveryID: "vibes-outbox-http-1", CorrelationID: "correlation-http-1",
		Delivery: tagaccess.ProjectionDelivery{
			Kind: tagaccess.DeliverySnapshot, BaselineAuthorityVersion: 5,
			AuthorityAssertionID: "snapshot-vibes-outbox-http-1",
			Projections: []tagaccess.ProjectionEvent{{
				EventID: "vibes-outbox-http-1", VIBESUserID: "vibes-user-1", WorkspaceID: "vibes-workspace-1",
				Role: tagaccess.RoleOwner, Status: tagaccess.StatusActive, AccountEpoch: 7,
				MembershipGeneration: 3, AuthorityVersion: 5,
			}},
		},
		ConnectionCloseTargets: []tagaccess.ConnectionCloseTarget{},
		Authentication:         tagaccess.AuthorityEnvelopeAuthentication{KeyID: "vibes-primary"},
	}
	payload, err := tagaccess.CanonicalAuthorityEnvelope(envelope)
	if err != nil {
		t.Fatal(err)
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(payload)
	envelope.Authentication.MAC = mac.Sum(nil)
	body, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/internal/tag-authority/workspace-projections", bytes.NewReader(body))
	response := httptest.NewRecorder()
	handler.Workspace(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var responseBody struct {
		Source string `json:"source"`
		tagaccess.TwoStageReceipt
	}
	if err := json.NewDecoder(response.Body).Decode(&responseBody); err != nil {
		t.Fatal(err)
	}
	if responseBody.Source != "workspace" {
		t.Fatalf("source = %q, want workspace", responseBody.Source)
	}
	receipt := responseBody.TwoStageReceipt
	if receipt.Apply.DeliveryID != envelope.DeliveryID || receipt.Apply.AuthorityVersion != 5 || receipt.Apply.Result != tagaccess.ApplyApplied {
		t.Fatalf("receipt = %#v", receipt)
	}
	if receipt.ConnectionClose.Status != tagaccess.ConnectionCloseNotRequired {
		t.Fatalf("close stage = %#v", receipt.ConnectionClose)
	}

	envelope.Authentication.MAC[0] ^= 0xff
	forged, _ := json.Marshal(envelope)
	request = httptest.NewRequest(http.MethodPost, "/internal/tag-authority/workspace-projections", bytes.NewReader(forged))
	response = httptest.NewRecorder()
	handler.Workspace(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("forged status = %d, want 401", response.Code)
	}
}

func TestHTTPIngressRejectsUnknownFieldsAndMultipleDocuments(t *testing.T) {
	key := []byte("vibes-authority-test-key-32-bytes-minimum")
	access, err := tagaccess.NewAuthenticatedAccess(tagaccess.NewMemoryStore(), tagaccess.SystemClock{}, map[string][]byte{"vibes-primary": key}, nil)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := tagaccess.NewHTTPIngress(access)
	if err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{`{"unknown":true}`, `{}` + "\n" + `{}`} {
		request := httptest.NewRequest(http.MethodPost, "/internal/tag-authority/workspace-projections", bytes.NewBufferString(body))
		response := httptest.NewRecorder()
		handler.Workspace(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("body %q status = %d, want 400", body, response.Code)
		}
	}
}
