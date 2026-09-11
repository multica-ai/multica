package handler

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type metadataMutationResponse struct {
	Metadata      map[string]any `json:"metadata"`
	IssueRevision int64          `json:"issue_revision"`
}

type metadataRowState struct {
	ctid           string
	xmin           string
	metadata       string
	revision       int64
	updatedAt      string
	lastActivityAt string
}

func readMetadataRowState(t *testing.T, issueID string) metadataRowState {
	t.Helper()
	var state metadataRowState
	dbfx.QueryRow(t, `
		SELECT ctid::text, xmin::text, metadata, revision,
		       updated_at::text, COALESCE(last_activity_at::text, '<null>')
		FROM issue WHERE id = $1
	`, issueID).Scan(
		&state.ctid,
		&state.xmin,
		&state.metadata,
		&state.revision,
		&state.updatedAt,
		&state.lastActivityAt,
	)
	return state
}

func mutationRequest(method, issueID, key string, value any) *http.Request {
	return testutil.WithURLParams(
		newRequest(method, "/api/issues/"+issueID+"/metadata/"+key, value),
		"id", issueID,
		"key", key,
	)
}

func handlerWithMetadataEventCounter(counter *atomic.Int64) *Handler {
	h := *testHandler
	h.Bus = events.New()
	h.Bus.Subscribe(protocol.EventIssueMetadataChanged, func(events.Event) {
		counter.Add(1)
	})
	return &h
}

func TestIssueMetadataNoopPreservesRowAndSuppressesEvents(t *testing.T) {
	issueID := dbfx.Issue(t, "metadata no-op row preservation", testutil.Cols{
		"metadata":         testutil.Raw(`'{"state":"ready"}'::jsonb`),
		"revision":         7,
		"last_activity_at": testutil.Raw("now() - interval '1 minute'"),
	})
	before := readMetadataRowState(t, issueID)
	var eventCount atomic.Int64
	h := handlerWithMetadataEventCounter(&eventCount)

	cases := []struct {
		name    string
		handler http.HandlerFunc
		request *http.Request
	}{
		{"set same value", h.SetIssueMetadataKey, mutationRequest(http.MethodPut, issueID, "state", map[string]any{"value": "ready"})},
		{"delete missing key", h.DeleteIssueMetadataKey, mutationRequest(http.MethodDelete, issueID, "missing", nil)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var body metadataMutationResponse
			testutil.Call(t, tc.handler, tc.request).Want(http.StatusOK).JSON(&body)
			if body.IssueRevision != before.revision || body.Metadata["state"] != "ready" {
				t.Fatalf("response = %+v, want current metadata at revision %d", body, before.revision)
			}
			if after := readMetadataRowState(t, issueID); after != before {
				t.Fatalf("no-op changed row state:\n before=%+v\n after=%+v", before, after)
			}
		})
	}
	if got := eventCount.Load(); got != 0 {
		t.Fatalf("events after no-op mutations = %d, want 0", got)
	}
}

func TestIssueMetadataWaitingNoopReturnsCommittedSnapshot(t *testing.T) {
	issueID := dbfx.Issue(t, "metadata waiting no-op snapshot")
	before := readMetadataRowState(t, issueID)
	holder, err := testPool.Begin(context.Background())
	if err != nil {
		t.Fatalf("begin holder transaction: %v", err)
	}
	defer holder.Rollback(context.Background())
	holderPID := holderBackendPID(t, context.Background(), holder)
	if _, err := db.New(holder).SetIssueMetadataKey(context.Background(), db.SetIssueMetadataKeyParams{
		ID:          parseUUID(issueID),
		WorkspaceID: parseUUID(testWorkspaceID),
		Key:         "state",
		Value:       []byte(`"committed"`),
	}); err != nil {
		t.Fatalf("set metadata in holder transaction: %v", err)
	}

	var eventCount atomic.Int64
	h := handlerWithMetadataEventCounter(&eventCount)
	response := make(chan *testutil.Response, 1)
	go func() {
		response <- testutil.Call(t, h.SetIssueMetadataKey,
			mutationRequest(http.MethodPut, issueID, "state", map[string]any{"value": "committed"}))
	}()
	if !waitForWaiterBlockedBy(t, holderPID, 10*time.Second) {
		_ = holder.Rollback(context.Background())
		<-response
		t.Fatalf("metadata set did not wait behind pid %d", holderPID)
	}
	if err := holder.Commit(context.Background()); err != nil {
		t.Fatalf("commit holder transaction: %v", err)
	}

	var body metadataMutationResponse
	(<-response).Want(http.StatusOK).JSON(&body)
	if body.IssueRevision != before.revision+1 || body.Metadata["state"] != "committed" {
		t.Fatalf("response = %+v, want committed snapshot at revision %d", body, before.revision+1)
	}
	if got := eventCount.Load(); got != 0 {
		t.Fatalf("events after waiting no-op = %d, want 0", got)
	}
}
