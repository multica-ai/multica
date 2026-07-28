package handler

import (
	"errors"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/cerebro/internalbrowserqa"
)

func TestInternalBrowserJobStoreScopesAndCompletesJobs(t *testing.T) {
	store := newInternalBrowserJobStore()
	id, err := store.create("workspace-a", "agent-a", "registry")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := store.get(id, "workspace-a", "agent-b"); ok {
		t.Fatal("job was visible to another agent")
	}
	wantErr := errors.New("verification failed")
	store.complete(id, internalbrowserqa.Result{App: "registry"}, wantErr)
	job, ok := store.get(id, "workspace-a", "agent-a")
	if !ok || job.State != "complete" || job.Result.App != "registry" || !errors.Is(job.Err, wantErr) {
		t.Fatalf("completed job = %#v, ok = %v", job, ok)
	}
}

func TestInternalBrowserJobStoreExpiresJobs(t *testing.T) {
	store := newInternalBrowserJobStore()
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	id, err := store.create("workspace-a", "agent-a", "registry")
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(internalBrowserJobTTL + time.Second)
	if _, ok := store.get(id, "workspace-a", "agent-a"); ok {
		t.Fatal("expired job remained readable")
	}
}
