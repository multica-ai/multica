package main

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/multica-ai/multica/server/internal/daemon"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestEnsurePVC_CreatesWhenMissing(t *testing.T) {
	k := fake.NewSimpleClientset()
	r := Registered{
		WorkspaceID: "wsabcdef-1234-5678-9abc-def012345678",
		AgentName:   "Lambda", Provider: "claude",
		PVCSize: "5Gi",
	}
	task := daemon.Task{
		ID: "task-1", IssueID: "issabcdef-9999-aaaa-bbbb-cccccccccccc",
		AgentID:     "agabcdef-1111-2222-3333-444444444444",
		WorkspaceID: r.WorkspaceID,
	}

	name, err := EnsurePVC(context.Background(), k, "multica", r, task)
	if err != nil {
		t.Fatal(err)
	}

	got, err := k.CoreV1().PersistentVolumeClaims("multica").Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("PVC not created: %v", err)
	}
	if got.Labels["multica.ai/issue-id"] != task.IssueID {
		t.Errorf("missing issue-id label: %+v", got.Labels)
	}
	if got.Spec.AccessModes[0] != corev1.ReadWriteOnce {
		t.Errorf("wrong access mode: %v", got.Spec.AccessModes)
	}
}

func TestEnsurePVC_NoopWhenExists(t *testing.T) {
	r := Registered{WorkspaceID: "ws-1", AgentName: "Lambda", Provider: "claude", PVCSize: "5Gi"}
	task := daemon.Task{ID: "t1", IssueID: "iss-1", AgentID: "ag-1", WorkspaceID: r.WorkspaceID}
	name := pvcName(r, task)

	existing := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "multica"}}
	k := fake.NewSimpleClientset(existing)

	got, err := EnsurePVC(context.Background(), k, "multica", r, task)
	if err != nil {
		t.Fatal(err)
	}
	if got != name {
		t.Errorf("expected reuse %q, got %q", name, got)
	}
}

func TestCreateJob_AndPayloadConfigMap(t *testing.T) {
	k := fake.NewSimpleClientset()
	r := Registered{
		WorkspaceID: "ws-1", AgentName: "Lambda", Provider: "claude",
		Image:   "registry/multica-runtime-claude:v0.3.0-mk1",
		PVCSize: "5Gi",
	}
	task := daemon.Task{
		ID: "task-xyz", IssueID: "iss-1", AgentID: "ag-1", WorkspaceID: r.WorkspaceID,
		RuntimeID: "rt-1",
	}

	jobName, err := DispatchJob(context.Background(), k, "multica", r, task, "ghcr-pull", "pvc-name")
	if err != nil {
		t.Fatal(err)
	}

	// ConfigMap with payload
	cm, err := k.CoreV1().ConfigMaps("multica").Get(context.Background(), "task-"+task.ID, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("payload CM missing: %v", err)
	}
	var roundTrip daemon.Task
	if err := json.Unmarshal([]byte(cm.Data["task.json"]), &roundTrip); err != nil {
		t.Fatalf("payload not valid JSON: %v", err)
	}
	if roundTrip.ID != task.ID {
		t.Errorf("payload corrupted")
	}

	// Job exists
	job, err := k.BatchV1().Jobs("multica").Get(context.Background(), jobName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Job missing: %v", err)
	}
	if job.Labels["multica.ai/task-id"] != task.ID {
		t.Errorf("Job missing task-id label")
	}
}
