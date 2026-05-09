package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// newWaitlistTestUser inserts a fresh user row, returns its id, and
// registers a cleanup. Uses the test pool directly so we don't depend
// on the handler-under-test for fixture setup.
func newWaitlistTestUser(t *testing.T, email string) string {
	t.Helper()
	ctx := context.Background()

	var userID string
	if err := testPool.QueryRow(ctx,
		`INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id`,
		"Waitlist Test", email,
	).Scan(&userID); err != nil {
		t.Fatalf("insert test user: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM "user" WHERE id = $1`, userID)
	})
	return userID
}

func newWaitlistRequest(userID string, body map[string]string) *http.Request {
	var buf bytes.Buffer
	if body != nil {
		json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(
		"POST", "/api/me/onboarding/cloud-waitlist", &buf,
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", userID)
	return req
}

func TestJoinCloudWaitlistRecordsEmailAndReason(t *testing.T) {
	userID := newWaitlistTestUser(t, "waitlist-ok@multica.ai")

	w := httptest.NewRecorder()
	req := newWaitlistRequest(userID, map[string]string{
		"email":  "Someone@Example.COM",
		"reason": "evaluating for our team",
	})
	testHandler.JoinCloudWaitlist(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var (
		waitlistEmail  *string
		waitlistReason *string
		onboardedAt    *time.Time
	)
	if err := testPool.QueryRow(context.Background(), `
		SELECT cloud_waitlist_email, cloud_waitlist_reason, onboarded_at
		  FROM "user" WHERE id = $1
	`, userID).Scan(&waitlistEmail, &waitlistReason, &onboardedAt); err != nil {
		t.Fatalf("lookup user: %v", err)
	}

	if waitlistEmail == nil || *waitlistEmail != "someone@example.com" {
		t.Fatalf("email not normalized/stored: %v", waitlistEmail)
	}
	if waitlistReason == nil || *waitlistReason != "evaluating for our team" {
		t.Fatalf("reason not stored: %v", waitlistReason)
	}
	// Waitlist is a pure side effect — onboarding is NOT marked
	// complete here. The user still has to pick a real Step 3
	// path (CLI / Skip) before onboarded_at gets set.
	if onboardedAt != nil {
		t.Fatalf("onboarded_at should stay NULL, got %v", *onboardedAt)
	}
}

func TestJoinCloudWaitlistAllowsEmptyReason(t *testing.T) {
	userID := newWaitlistTestUser(t, "waitlist-noreason@multica.ai")

	w := httptest.NewRecorder()
	req := newWaitlistRequest(userID, map[string]string{
		"email": "noreason@example.com",
	})
	testHandler.JoinCloudWaitlist(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var waitlistReason *string
	if err := testPool.QueryRow(context.Background(),
		`SELECT cloud_waitlist_reason FROM "user" WHERE id = $1`, userID,
	).Scan(&waitlistReason); err != nil {
		t.Fatalf("lookup user: %v", err)
	}
	if waitlistReason != nil {
		t.Fatalf("expected NULL reason, got %q", *waitlistReason)
	}
}

func TestJoinCloudWaitlistMissingEmailReturns400(t *testing.T) {
	userID := newWaitlistTestUser(t, "waitlist-missing@multica.ai")

	cases := []map[string]string{
		{},                        // empty body
		{"email": ""},             // blank
		{"email": "   "},          // whitespace only
		{"reason": "no email here"},
	}

	for i, body := range cases {
		w := httptest.NewRecorder()
		req := newWaitlistRequest(userID, body)
		testHandler.JoinCloudWaitlist(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("case %d: expected 400, got %d: %s", i, w.Code, w.Body.String())
		}
	}
}

func TestJoinCloudWaitlistRejectsOverlongReason(t *testing.T) {
	userID := newWaitlistTestUser(t, "waitlist-long@multica.ai")

	w := httptest.NewRecorder()
	req := newWaitlistRequest(userID, map[string]string{
		"email":  "long@example.com",
		"reason": strings.Repeat("x", cloudWaitlistReasonMaxLen+1),
	})
	testHandler.JoinCloudWaitlist(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for overlong reason, got %d", w.Code)
	}
}

func TestJoinCloudWaitlistSecondCallOverwrites(t *testing.T) {
	userID := newWaitlistTestUser(t, "waitlist-overwrite@multica.ai")

	// First submission.
	w := httptest.NewRecorder()
	req := newWaitlistRequest(userID, map[string]string{
		"email":  "first@example.com",
		"reason": "first",
	})
	testHandler.JoinCloudWaitlist(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("first call: expected 200, got %d", w.Code)
	}

	// Second submission with different values.
	w = httptest.NewRecorder()
	req = newWaitlistRequest(userID, map[string]string{
		"email":  "second@example.com",
		"reason": "changed my mind",
	})
	testHandler.JoinCloudWaitlist(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("second call: expected 200, got %d", w.Code)
	}

	var (
		waitlistEmail  string
		waitlistReason string
		onboardedAt    *time.Time
	)
	if err := testPool.QueryRow(context.Background(), `
		SELECT cloud_waitlist_email, cloud_waitlist_reason, onboarded_at
		  FROM "user" WHERE id = $1
	`, userID).Scan(&waitlistEmail, &waitlistReason, &onboardedAt); err != nil {
		t.Fatalf("lookup user: %v", err)
	}

	if waitlistEmail != "second@example.com" {
		t.Fatalf("second email should overwrite; got %q", waitlistEmail)
	}
	if waitlistReason != "changed my mind" {
		t.Fatalf("second reason should overwrite; got %q", waitlistReason)
	}
	// onboarded_at is never touched by the waitlist path — stays NULL
	// across any number of submissions.
	if onboardedAt != nil {
		t.Fatalf("onboarded_at should stay NULL, got %v", *onboardedAt)
	}
}
