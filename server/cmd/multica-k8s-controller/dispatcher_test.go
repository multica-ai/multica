package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/multica-ai/multica/server/internal/daemon"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestDispatchOnce_CreatesPVCAndJobForClaimedTask(t *testing.T) {
	claimed := int32(0)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/daemon/runtimes/rt-1/tasks/claim" {
			if atomic.AddInt32(&claimed, 1) == 1 {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"task": daemon.Task{
						ID:          "task-A",
						RuntimeID:   "rt-1",
						IssueID:     "issA",
						AgentID:     "agA",
						WorkspaceID: "wsA",
						Agent:       &daemon.AgentData{Name: "Lambda"},
					},
				})
				return
			}
			_, _ = io.WriteString(w, "{}") // subsequent: no task
			return
		}
		_, _ = io.WriteString(w, "{}")
	}))
	defer srv.Close()

	cli := daemon.NewClient(srv.URL)
	cli.SetToken("tk")
	k := fake.NewSimpleClientset()
	r := Registered{
		WorkspaceID: "wsA", Provider: "claude", AgentName: "Lambda",
		RuntimeID: "rt-1", Image: "img:v", PVCSize: "5Gi",
	}

	dispatched, err := DispatchOnce(context.Background(), cli, k, "multica", "ghcr-pull", r, ClaudeBrokerOptions{}, RepoCacheOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !dispatched {
		t.Fatalf("expected dispatched=true on first claim")
	}

	// PVC and Job created
	jobs, err := k.BatchV1().Jobs("multica").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("listing jobs: %v", err)
	}
	if len(jobs.Items) != 1 {
		t.Fatalf("expected 1 Job, got %d", len(jobs.Items))
	}
	if jobs.Items[0].Labels[labelTaskID] != "task-A" {
		t.Errorf("Job missing correct task label")
	}

	// Second call: no task → no new Job
	if _, err := DispatchOnce(context.Background(), cli, k, "multica", "ghcr-pull", r, ClaudeBrokerOptions{}, RepoCacheOptions{}); err != nil {
		t.Fatal(err)
	}
	jobs2, _ := k.BatchV1().Jobs("multica").List(context.Background(), metav1.ListOptions{})
	if len(jobs2.Items) != 1 {
		t.Fatalf("a second claim attempt created a duplicate Job (now %d)", len(jobs2.Items))
	}
}
