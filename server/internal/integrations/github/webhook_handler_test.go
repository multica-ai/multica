package github

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ----------------------------- HMAC -----------------------------

func TestVerifySignatureValid(t *testing.T) {
	secret := "topsecret"
	body := []byte(`{"hello":"world"}`)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	header := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if !verifySignature(header, secret, body) {
		t.Fatalf("expected valid signature to verify")
	}
}

func TestVerifySignatureWrongSecret(t *testing.T) {
	body := []byte(`{}`)
	mac := hmac.New(sha256.New, []byte("a"))
	mac.Write(body)
	header := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if verifySignature(header, "b", body) {
		t.Fatalf("expected wrong secret to fail")
	}
}

func TestVerifySignatureMissingPrefix(t *testing.T) {
	if verifySignature("deadbeef", "x", []byte("a")) {
		t.Fatalf("expected missing prefix to fail")
	}
}

func TestVerifySignatureBadHex(t *testing.T) {
	if verifySignature("sha256=zzz", "x", []byte("a")) {
		t.Fatalf("expected bad hex to fail")
	}
}

// ----------------------------- HTTP early-exit paths -----------------------------

func TestServeHTTPIgnoresUnknownEvent(t *testing.T) {
	h := &WebhookHandler{Secret: ""}
	body := `{"action":"created"}`
	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/github", strings.NewReader(body))
	req.Header.Set("X-GitHub-Delivery", "delivery-1")
	req.Header.Set("X-GitHub-Event", "ping")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"action":"ignored"`) {
		t.Fatalf("body = %s, want ignored action", w.Body.String())
	}
}

func TestServeHTTPRejectsBadSignature(t *testing.T) {
	h := &WebhookHandler{Secret: "topsecret"}
	body := `{"action":"opened"}`
	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/github", strings.NewReader(body))
	req.Header.Set("X-GitHub-Delivery", "d2")
	req.Header.Set("X-GitHub-Event", "pull_request")
	req.Header.Set("X-Hub-Signature-256", "sha256=00")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

func TestServeHTTPRejectsMissingHeaders(t *testing.T) {
	h := &WebhookHandler{Secret: ""}
	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/github", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestServeHTTPRejectsBadPayload(t *testing.T) {
	h := &WebhookHandler{Secret: ""}
	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/github", strings.NewReader(`not json`))
	req.Header.Set("X-GitHub-Delivery", "d3")
	req.Header.Set("X-GitHub-Event", "pull_request")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

// ----------------------------- splitRepo -----------------------------

func TestSplitRepoOK(t *testing.T) {
	o, r, ok := splitRepo("zeyad-farrag/multica")
	if !ok || o != "zeyad-farrag" || r != "multica" {
		t.Fatalf("split = %s, %s, %v", o, r, ok)
	}
}

func TestSplitRepoInvalid(t *testing.T) {
	if _, _, ok := splitRepo("noslash"); ok {
		t.Fatalf("expected invalid")
	}
	if _, _, ok := splitRepo("/onlyright"); ok {
		t.Fatalf("expected invalid")
	}
	if _, _, ok := splitRepo("onlyleft/"); ok {
		t.Fatalf("expected invalid")
	}
}

// ----------------------------- Predicate -----------------------------

type fakePredicateClient struct {
	reviews []Review
	threads []ReviewThread
}

func (f fakePredicateClient) ListReviews(_ context.Context, _, _ string, _ int) ([]Review, error) {
	return f.reviews, nil
}
func (f fakePredicateClient) ListReviewThreads(_ context.Context, _, _ string, _ int) ([]ReviewThread, error) {
	return f.threads, nil
}

func newReview(login, state string) Review {
	r := Review{State: state}
	r.User.Login = login
	return r
}

func TestPredicateAllClean(t *testing.T) {
	c := fakePredicateClient{
		reviews: []Review{newReview("coderabbitai[bot]", "APPROVED")},
		threads: []ReviewThread{{IsResolved: true, Author: "coderabbitai[bot]"}},
	}
	noOpen, noUnresolved, err := EvaluatePredicate(context.Background(), c, "o", "r", 1, "coderabbitai[bot]")
	if err != nil {
		t.Fatal(err)
	}
	if !noOpen || !noUnresolved {
		t.Fatalf("expected both true, got %v %v", noOpen, noUnresolved)
	}
}

func TestPredicateOpenChangesRequest(t *testing.T) {
	c := fakePredicateClient{
		reviews: []Review{newReview("coderabbitai[bot]", "CHANGES_REQUESTED")},
	}
	noOpen, _, _ := EvaluatePredicate(context.Background(), c, "o", "r", 1, "coderabbitai[bot]")
	if noOpen {
		t.Fatalf("expected NoOpenCRChangesRequest=false")
	}
}

func TestPredicateChangesThenDismissed(t *testing.T) {
	// Latest CR-bot review wins. CHANGES_REQUESTED then DISMISSED → no open.
	c := fakePredicateClient{
		reviews: []Review{
			newReview("coderabbitai[bot]", "CHANGES_REQUESTED"),
			newReview("coderabbitai[bot]", "DISMISSED"),
		},
	}
	noOpen, _, _ := EvaluatePredicate(context.Background(), c, "o", "r", 1, "coderabbitai[bot]")
	if !noOpen {
		t.Fatalf("expected NoOpenCRChangesRequest=true (latest is DISMISSED)")
	}
}

func TestPredicateUnresolvedThread(t *testing.T) {
	c := fakePredicateClient{
		threads: []ReviewThread{{IsResolved: false, Author: "coderabbitai[bot]"}},
	}
	_, noUnresolved, _ := EvaluatePredicate(context.Background(), c, "o", "r", 1, "coderabbitai[bot]")
	if noUnresolved {
		t.Fatalf("expected NoUnresolvedCRThreads=false")
	}
}

func TestPredicateIgnoresHumanReviews(t *testing.T) {
	c := fakePredicateClient{
		reviews: []Review{newReview("alice", "CHANGES_REQUESTED")},
		threads: []ReviewThread{{IsResolved: false, Author: "alice"}},
	}
	noOpen, noUnresolved, _ := EvaluatePredicate(context.Background(), c, "o", "r", 1, "coderabbitai[bot]")
	if !noOpen || !noUnresolved {
		t.Fatalf("expected both true (human-only), got %v %v", noOpen, noUnresolved)
	}
}
