package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/multica-ai/multica/server/internal/daemon"
)

func TestWatchFailedJobs_PostsFailTaskAndDeletes(t *testing.T) {
	var (
		mu    sync.Mutex
		fails []string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/fail") {
			mu.Lock()
			fails = append(fails, r.URL.Path)
			mu.Unlock()
		}
		_, _ = io.WriteString(w, "{}")
	}))
	defer srv.Close()
	cli := daemon.NewClient(srv.URL)
	cli.SetToken("tk")

	failed := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name: "task-failed1", Namespace: "multica",
			Labels: map[string]string{
				labelManagedBy: managedByValue,
				labelTaskID:    "tFAIL",
			},
		},
		Status: batchv1.JobStatus{Failed: 1, Conditions: []batchv1.JobCondition{{Type: batchv1.JobFailed, Status: "True"}}},
	}
	k := fake.NewSimpleClientset(failed)

	if err := SweepFailedJobs(context.Background(), cli, k, "multica"); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	n := len(fails)
	mu.Unlock()
	if n != 1 {
		t.Fatalf("expected 1 fail call, got %d", n)
	}

	// Job should be deleted post-sweep (so we don't repeatedly post FailTask).
	if _, err := k.BatchV1().Jobs("multica").Get(context.Background(), "task-failed1", metav1.GetOptions{}); err == nil {
		t.Errorf("expected Job to be deleted after fail")
	}
}
