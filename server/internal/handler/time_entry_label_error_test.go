package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestTimeEntryLabelMutationErrorResponse(t *testing.T) {
	testCases := []struct {
		name           string
		err            error
		wantStatusCode int
		wantMessage    string
	}{
		{
			name:           "invalid label id is a client error",
			err:            newTimeEntryLabelValidationError("invalid label_id: bad-id"),
			wantStatusCode: 400,
			wantMessage:    "invalid label_id: bad-id",
		},
		{
			name:           "missing label is a client error",
			err:            newTimeEntryLabelValidationError("label not found: 123"),
			wantStatusCode: 400,
			wantMessage:    "label not found: 123",
		},
		{
			name:           "database failures stay server errors",
			err:            context.Canceled,
			wantStatusCode: 500,
			wantMessage:    "failed to update time entry labels",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			gotStatusCode, gotMessage := timeEntryLabelMutationErrorResponse(testCase.err)
			if gotStatusCode != testCase.wantStatusCode {
				t.Fatalf("status = %d, want %d", gotStatusCode, testCase.wantStatusCode)
			}
			if gotMessage != testCase.wantMessage {
				t.Fatalf("message = %q, want %q", gotMessage, testCase.wantMessage)
			}
		})
	}
}

type failingTxStarter struct {
	err error
}

func (f failingTxStarter) Begin(ctx context.Context) (pgx.Tx, error) {
	return nil, f.err
}

func TestSetTimeEntryLabelsReturnsServerErrorForMutationFailure(t *testing.T) {
	ensureTimeEntryLabelSchema(t)

	start := time.Now().UTC().Truncate(time.Second)

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/time-entries", map[string]any{
		"start_time": start.Format(time.RFC3339),
	})
	testHandler.CreateTimeEntry(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateTimeEntry: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var entry TimeEntryResponse
	if err := json.NewDecoder(w.Body).Decode(&entry); err != nil {
		t.Fatalf("decode created entry: %v", err)
	}

	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM time_entry WHERE id = $1`, entry.ID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM running_timer WHERE user_id = $1`, testUserID)
	})

	failingHandler := &Handler{
		Queries:   testHandler.Queries,
		TxStarter: failingTxStarter{err: errors.New("boom")},
	}

	w = httptest.NewRecorder()
	req = newRequest("PUT", "/api/time-entries/"+entry.ID+"/labels", map[string]any{
		"label_ids": []string{},
	})
	req = withURLParam(req, "entry_id", entry.ID)
	failingHandler.SetTimeEntryLabels(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("SetTimeEntryLabels: expected 500, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if resp["error"] != "failed to update time entry labels" {
		t.Fatalf("error message = %q, want %q", resp["error"], "failed to update time entry labels")
	}
}
