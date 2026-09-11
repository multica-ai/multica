package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
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
	issueID := dbfx.Issue(t, "metadata no-op row preservation")
	var eventCount atomic.Int64
	h := handlerWithMetadataEventCounter(&eventCount)

	var changed metadataMutationResponse
	testutil.Call(t, h.SetIssueMetadataKey,
		mutationRequest(http.MethodPut, issueID, "state", map[string]any{"value": "ready"}),
	).Want(http.StatusOK).JSON(&changed)
	if got := eventCount.Load(); got != 1 {
		t.Fatalf("events after changed set = %d, want 1", got)
	}
	afterSet := readMetadataRowState(t, issueID)

	var duplicateSet metadataMutationResponse
	testutil.Call(t, h.SetIssueMetadataKey,
		mutationRequest(http.MethodPut, issueID, "state", map[string]any{"value": "ready"}),
	).Want(http.StatusOK).JSON(&duplicateSet)
	if duplicateSet.IssueRevision != changed.IssueRevision || duplicateSet.Metadata["state"] != "ready" {
		t.Fatalf("duplicate set response = %+v, want committed metadata/revision %+v", duplicateSet, changed)
	}
	if got := readMetadataRowState(t, issueID); got != afterSet {
		t.Fatalf("duplicate set changed row state:\n before=%+v\n after=%+v", afterSet, got)
	}
	if got := eventCount.Load(); got != 1 {
		t.Fatalf("events after duplicate set = %d, want 1", got)
	}

	var missingDelete metadataMutationResponse
	testutil.Call(t, h.DeleteIssueMetadataKey,
		mutationRequest(http.MethodDelete, issueID, "missing", nil),
	).Want(http.StatusOK).JSON(&missingDelete)
	if missingDelete.IssueRevision != changed.IssueRevision || missingDelete.Metadata["state"] != "ready" {
		t.Fatalf("missing-key delete response = %+v, want current state", missingDelete)
	}
	if got := readMetadataRowState(t, issueID); got != afterSet {
		t.Fatalf("missing-key delete changed row state:\n before=%+v\n after=%+v", afterSet, got)
	}
	if got := eventCount.Load(); got != 1 {
		t.Fatalf("events after missing-key delete = %d, want 1", got)
	}

	var changedDelete metadataMutationResponse
	testutil.Call(t, h.DeleteIssueMetadataKey,
		mutationRequest(http.MethodDelete, issueID, "state", nil),
	).Want(http.StatusOK).JSON(&changedDelete)
	if _, ok := changedDelete.Metadata["state"]; ok {
		t.Fatalf("changed delete retained state key: %+v", changedDelete.Metadata)
	}
	if changedDelete.IssueRevision != changed.IssueRevision+1 {
		t.Fatalf("changed delete revision = %d, want %d", changedDelete.IssueRevision, changed.IssueRevision+1)
	}
	afterDelete := readMetadataRowState(t, issueID)

	testutil.Call(t, h.DeleteIssueMetadataKey,
		mutationRequest(http.MethodDelete, issueID, "state", nil),
	).Want(http.StatusOK)
	if got := readMetadataRowState(t, issueID); got != afterDelete {
		t.Fatalf("duplicate delete changed row state:\n before=%+v\n after=%+v", afterDelete, got)
	}
	if got := eventCount.Load(); got != 2 {
		t.Fatalf("events after changed and duplicate delete = %d, want 2", got)
	}
}

func TestIssueMetadataConcurrentIdenticalSetsMutateAndPublishOnce(t *testing.T) {
	issueID := dbfx.Issue(t, "metadata concurrent identical sets")
	before := readMetadataRowState(t, issueID)
	var eventCount atomic.Int64
	h := handlerWithMetadataEventCounter(&eventCount)

	const workers = 20
	start := make(chan struct{})
	results := make(chan metadataMutationResponse, workers)
	errs := make(chan string, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			resp := testutil.Call(t, h.SetIssueMetadataKey,
				mutationRequest(http.MethodPut, issueID, "state", map[string]any{"value": "ready"}),
			)
			if resp.Code != http.StatusOK {
				errs <- fmt.Sprintf("status=%d body=%s", resp.Code, resp.Body.String())
				return
			}
			var body metadataMutationResponse
			if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
				errs <- fmt.Sprintf("decode: %v", err)
				return
			}
			results <- body
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	close(errs)
	for errText := range errs {
		t.Error(errText)
	}
	for body := range results {
		if body.IssueRevision != before.revision+1 || body.Metadata["state"] != "ready" {
			t.Errorf("concurrent response = %+v, want revision %d and committed value", body, before.revision+1)
		}
	}

	after := readMetadataRowState(t, issueID)
	if after.revision != before.revision+1 {
		t.Fatalf("revision after identical sets = %d, want %d", after.revision, before.revision+1)
	}
	if got := eventCount.Load(); got != 1 {
		t.Fatalf("events after identical sets = %d, want 1", got)
	}
}

func TestIssueMetadataConcurrentDistinctKeysArePreserved(t *testing.T) {
	issueID := dbfx.Issue(t, "metadata concurrent distinct keys")
	before := readMetadataRowState(t, issueID)
	var eventCount atomic.Int64
	h := handlerWithMetadataEventCounter(&eventCount)

	const workers = 20
	start := make(chan struct{})
	errs := make(chan string, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			key := fmt.Sprintf("key_%02d", i)
			resp := testutil.Call(t, h.SetIssueMetadataKey,
				mutationRequest(http.MethodPut, issueID, key, map[string]any{"value": i}),
			)
			if resp.Code != http.StatusOK {
				errs <- fmt.Sprintf("%s: status=%d body=%s", key, resp.Code, resp.Body.String())
			}
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for errText := range errs {
		t.Error(errText)
	}

	after := readMetadataRowState(t, issueID)
	if after.revision != before.revision+workers {
		t.Fatalf("revision after distinct sets = %d, want %d", after.revision, before.revision+workers)
	}
	var metadata map[string]any
	if err := json.Unmarshal([]byte(after.metadata), &metadata); err != nil {
		t.Fatalf("decode persisted metadata: %v", err)
	}
	for i := 0; i < workers; i++ {
		key := fmt.Sprintf("key_%02d", i)
		if got := metadata[key]; got != float64(i) {
			t.Errorf("metadata[%q] = %v, want %d", key, got, i)
		}
	}
	if got := eventCount.Load(); got != workers {
		t.Fatalf("events after distinct sets = %d, want %d", got, workers)
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
		req := mutationRequest(http.MethodPut, issueID, "state", map[string]any{"value": "committed"})
		response <- testutil.Call(t, h.SetIssueMetadataKey, req)
	}()

	if !waitForWaiterBlockedBy(t, holderPID, 10*time.Second) {
		_ = holder.Rollback(context.Background())
		<-response
		t.Fatalf("metadata set did not reach a row-lock wait behind pid %d", holderPID)
	}
	if err := holder.Commit(context.Background()); err != nil {
		t.Fatalf("commit holder transaction: %v", err)
	}

	var body metadataMutationResponse
	(<-response).Want(http.StatusOK).JSON(&body)
	if body.IssueRevision != before.revision+1 || body.Metadata["state"] != "committed" {
		t.Fatalf("waiting no-op response = %+v, want committed snapshot at revision %d", body, before.revision+1)
	}
	if got := eventCount.Load(); got != 0 {
		t.Fatalf("waiting no-op events = %d, want 0", got)
	}
}

func TestIssueMetadataConcurrentDeleteReturnsNotFound(t *testing.T) {
	issueID := dbfx.Issue(t, "metadata concurrent issue delete")
	holder, err := testPool.Begin(context.Background())
	if err != nil {
		t.Fatalf("begin holder transaction: %v", err)
	}
	defer holder.Rollback(context.Background())
	holderPID := holderBackendPID(t, context.Background(), holder)
	if _, err := holder.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID); err != nil {
		t.Fatalf("delete issue in holder transaction: %v", err)
	}

	var eventCount atomic.Int64
	h := handlerWithMetadataEventCounter(&eventCount)
	response := make(chan *testutil.Response, 1)
	go func() {
		req := mutationRequest(http.MethodPut, issueID, "state", map[string]any{"value": "ready"})
		response <- testutil.Call(t, h.SetIssueMetadataKey, req)
	}()

	if !waitForWaiterBlockedBy(t, holderPID, 10*time.Second) {
		_ = holder.Rollback(context.Background())
		<-response
		t.Fatalf("metadata set did not wait behind concurrent delete held by pid %d", holderPID)
	}
	if err := holder.Commit(context.Background()); err != nil {
		t.Fatalf("commit issue delete: %v", err)
	}
	(<-response).Want(http.StatusNotFound)
	if got := eventCount.Load(); got != 0 {
		t.Fatalf("events after concurrent issue delete = %d, want 0", got)
	}
}

func TestIssueMetadataMutationIsWorkspaceScoped(t *testing.T) {
	foreignWorkspaceID := dbfx.Workspace(t, "metadata foreign workspace", "metadata-foreign-workspace")
	foreignIssueID := dbfx.Issue(t, "metadata foreign issue", testutil.Cols{
		"workspace_id": foreignWorkspaceID,
	})

	_, err := testHandler.Queries.SetIssueMetadataKey(context.Background(), db.SetIssueMetadataKeyParams{
		ID:          parseUUID(foreignIssueID),
		WorkspaceID: parseUUID(testWorkspaceID),
		Key:         "state",
		Value:       []byte(`"ready"`),
	})
	if err != pgx.ErrNoRows {
		t.Fatalf("cross-workspace set error = %v, want pgx.ErrNoRows", err)
	}
	state := readMetadataRowState(t, foreignIssueID)
	if string(state.metadata) != "{}" {
		t.Fatalf("cross-workspace set mutated metadata: %s", state.metadata)
	}
}
