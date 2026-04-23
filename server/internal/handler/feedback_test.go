package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

func TestCreateFeedbackHappyPath(t *testing.T) {
	clearFeedbackForTestUser(t)

	req := newRequest("POST", "/api/feedback", CreateFeedbackRequest{
		Message: "Love the product, dark mode flashes on startup",
	})
	w := httptest.NewRecorder()
	testHandler.CreateFeedback(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp FeedbackResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.ID == "" {
		t.Fatal("expected feedback id in response")
	}
}

func TestCreateFeedbackEmptyMessage(t *testing.T) {
	req := newRequest("POST", "/api/feedback", CreateFeedbackRequest{Message: "   "})
	w := httptest.NewRecorder()
	testHandler.CreateFeedback(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateFeedbackRateLimit(t *testing.T) {
	clearFeedbackForTestUser(t)

	for i := 0; i < feedbackHourlyRateLimit; i++ {
		req := newRequest("POST", "/api/feedback", CreateFeedbackRequest{
			Message: "feedback #" + strconv.Itoa(i),
		})
		w := httptest.NewRecorder()
		testHandler.CreateFeedback(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("iteration %d: expected 201, got %d: %s", i, w.Code, w.Body.String())
		}
	}
	req := newRequest("POST", "/api/feedback", CreateFeedbackRequest{Message: "one too many"})
	w := httptest.NewRecorder()
	testHandler.CreateFeedback(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d: %s", w.Code, w.Body.String())
	}
}

// clearFeedbackForTestUser wipes all feedback rows for the shared test user
// at both test start (fresh state) and test end (via t.Cleanup), so tests
// in this file don't interfere with each other or with the hourly rate-limit
// window when run in sequence.
func clearFeedbackForTestUser(t *testing.T) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(), `DELETE FROM feedback WHERE user_id = $1`, parseUUID(testUserID)); err != nil {
		t.Fatalf("clear feedback: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM feedback WHERE user_id = $1`, parseUUID(testUserID))
	})
}
