package handler

// wecom_web_test.go — both WeCom JSON endpoints decoded an unbounded body.
// encoding/json materializes a string value whole before Decode returns, so
// one authenticated caller POSTing a multi-gigabyte token cost the API server
// that much RSS per request.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/wecom"
)

// fakeRedeemer records whether the handler ever got as far as the service.
// A body the ceiling rejects must never reach it.
type fakeRedeemer struct{ calls int }

func (f *fakeRedeemer) RedeemAndBind(context.Context, string, pgtype.UUID) (wecom.RedeemedBindingToken, error) {
	f.calls++
	return wecom.RedeemedBindingToken{
		WorkspaceID:    pgtype.UUID{Bytes: [16]byte{1}, Valid: true},
		InstallationID: pgtype.UUID{Bytes: [16]byte{2}, Valid: true},
		WecomUserID:    "T-alex",
	}, nil
}

func redeemRequest(body string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/api/wecom/binding/redeem", strings.NewReader(body))
	// requestUserID reads this header and nothing else, so the whole test
	// runs without a database.
	r.Header.Set("X-User-ID", "00000000-0000-0000-0000-000000000001")
	return r
}

// TestRedeemWecomBindingTokenRefusesAnOversizedBody: the ceiling has to reject
// before the decoder allocates, and before the token reaches the service.
func TestRedeemWecomBindingTokenRefusesAnOversizedBody(t *testing.T) {
	fake := &fakeRedeemer{}
	h := &Handler{WecomBindingTokens: fake}

	huge := `{"token":"` + strings.Repeat("A", wecomBodyLimit*2) + `"}`
	w := httptest.NewRecorder()
	h.RedeemWecomBindingToken(w, redeemRequest(huge))

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	if fake.calls != 0 {
		t.Errorf("an oversized body reached the redeem service %d time(s), want 0", fake.calls)
	}
}

// TestRedeemWecomBindingTokenAcceptsAnOrdinaryBody guards the other side: a
// ceiling set too tight would break every real redemption, and this test is
// what would catch that.
func TestRedeemWecomBindingTokenAcceptsAnOrdinaryBody(t *testing.T) {
	fake := &fakeRedeemer{}
	h := &Handler{WecomBindingTokens: fake}

	w := httptest.NewRecorder()
	h.RedeemWecomBindingToken(w, redeemRequest(`{"token":"an-ordinary-32-byte-looking-token"}`))

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d (body: %s)", w.Code, http.StatusOK, w.Body.String())
	}
	if fake.calls != 1 {
		t.Errorf("redeem service called %d time(s), want 1", fake.calls)
	}
}
