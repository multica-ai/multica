package main

import (
	"context"
	"encoding/json"
	"strings"
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

	jobName, err := DispatchJob(context.Background(), k, "multica", r, task, "ghcr-pull", "pvc-name", ClaudeBrokerOptions{}, RepoCacheOptions{})
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

	// In legacy mode, the claude-auth init container + claude-oauth-secret
	// volume MUST be present (this is the behavior broker mode replaces).
	pod := job.Spec.Template.Spec
	if len(pod.InitContainers) != 1 || pod.InitContainers[0].Name != "claude-auth" {
		t.Errorf("legacy mode missing claude-auth init container: %+v", pod.InitContainers)
	}
	if !hasVolume(pod.Volumes, "claude-oauth-secret") {
		t.Errorf("legacy mode missing claude-oauth-secret volume")
	}
}

func TestCreateJob_BrokerMode(t *testing.T) {
	k := fake.NewSimpleClientset()
	r := Registered{
		WorkspaceID: "ws-1", AgentName: "Lambda", Provider: "claude",
		Image:   "registry/multica-runtime-claude:v0.3.0-mk1",
		PVCSize: "5Gi",
	}
	task := daemon.Task{
		ID: "task-broker", IssueID: "iss-1", AgentID: "ag-1", WorkspaceID: r.WorkspaceID,
		RuntimeID: "rt-1",
	}
	cb := ClaudeBrokerOptions{Enabled: true, AccessTokenSecret: "multica-claude-broker-access-token", SecretKey: "access_token"}

	jobName, err := DispatchJob(context.Background(), k, "multica", r, task, "ghcr-pull", "pvc-name", cb, RepoCacheOptions{})
	if err != nil {
		t.Fatal(err)
	}
	job, err := k.BatchV1().Jobs("multica").Get(context.Background(), jobName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Job missing: %v", err)
	}
	pod := job.Spec.Template.Spec

	// (a) Legacy auth path must be GONE.
	if len(pod.InitContainers) != 0 {
		t.Errorf("broker mode must not have init containers; got %+v", pod.InitContainers)
	}
	if hasVolume(pod.Volumes, "claude-oauth-secret") {
		t.Errorf("broker mode must not mount the legacy claude-oauth-secret volume")
	}

	// (b) No legacy apiKeyHelper artifacts — apiKeyHelper is x-api-key auth and
	//     OAuth tokens get rejected on that path. The fix injects
	//     CLAUDE_CODE_OAUTH_TOKEN via secretKeyRef instead.
	if hasVolume(pod.Volumes, "claude-broker-client") {
		t.Errorf("broker mode must not mount the apiKeyHelper client CM (pivot to env-via-secretKeyRef)")
	}

	// (c) runtask env must include CLAUDE_CODE_OAUTH_TOKEN sourced from
	//     the broker's access-token Secret.
	runtask := pod.Containers[0]
	var tokenEnv *corev1.EnvVar
	for i := range runtask.Env {
		if runtask.Env[i].Name == "CLAUDE_CODE_OAUTH_TOKEN" {
			tokenEnv = &runtask.Env[i]
		}
	}
	if tokenEnv == nil {
		t.Fatalf("runtask missing CLAUDE_CODE_OAUTH_TOKEN env; env=%+v", runtask.Env)
	}
	if tokenEnv.ValueFrom == nil || tokenEnv.ValueFrom.SecretKeyRef == nil {
		t.Fatalf("CLAUDE_CODE_OAUTH_TOKEN must be sourced from secretKeyRef; got %+v", tokenEnv)
	}
	ref := tokenEnv.ValueFrom.SecretKeyRef
	if ref.Name != "multica-claude-broker-access-token" {
		t.Errorf("secretKeyRef.name = %q, want multica-claude-broker-access-token", ref.Name)
	}
	if ref.Key != "access_token" {
		t.Errorf("secretKeyRef.key = %q, want access_token", ref.Key)
	}

	// (d) The deprecated CLAUDE_CODE_API_KEY_HELPER_TTL_MS env must NOT be set.
	for _, e := range runtask.Env {
		if e.Name == "CLAUDE_CODE_API_KEY_HELPER_TTL_MS" {
			t.Errorf("CLAUDE_CODE_API_KEY_HELPER_TTL_MS still set; apiKeyHelper path is deprecated")
		}
	}
}

func hasVolume(vs []corev1.Volume, name string) bool {
	for _, v := range vs {
		if v.Name == name {
			return true
		}
	}
	return false
}

func hasVolumeMount(ms []corev1.VolumeMount, name string) *corev1.VolumeMount {
	for i := range ms {
		if ms[i].Name == name {
			return &ms[i]
		}
	}
	return nil
}

// TestDispatchJob_WithRepoCache asserts that, when RepoCacheOptions.Enabled,
// DispatchJob:
//   - creates a per-task gitconfig ConfigMap whose content rewrites every
//     workspace repo URL onto file:// paths under the cache mount, and
//   - adds both the repocache PVC (RO) and the gitconfig CM as mounts on the
//     runtask container.
func TestDispatchJob_WithRepoCache(t *testing.T) {
	k := fake.NewSimpleClientset()
	r := Registered{
		WorkspaceID: "ws-rc",
		AgentName:   "Lambda", Provider: "claude",
		Image:   "registry/multica-runtime-claude:dev",
		PVCSize: "5Gi",
	}
	task := daemon.Task{
		ID: "task-rc-1", IssueID: "iss-1", AgentID: "ag-1", WorkspaceID: r.WorkspaceID,
		RuntimeID: "rt-1",
		Repos: []daemon.RepoData{
			{URL: "https://github.com/chrissnell/graywolf.git"},
		},
	}
	rc := RepoCacheOptions{
		Enabled:   true,
		PVCName:   "multica-repocache-repos",
		MountPath: "/repos",
	}

	jobName, err := DispatchJob(context.Background(), k, "multica", r, task, "ghcr-pull", "pvc-name", ClaudeBrokerOptions{}, rc)
	if err != nil {
		t.Fatal(err)
	}

	// Per-task gitconfig CM exists and includes the expected insteadOf entry.
	gcCM, err := k.CoreV1().ConfigMaps("multica").Get(context.Background(), "task-"+task.ID[:8]+"-gitconfig", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("gitconfig CM missing: %v", err)
	}
	content := gcCM.Data[".gitconfig"]
	for _, want := range []string{
		`[url "file:///repos/ws-rc/github.com+chrissnell+graywolf.git"]`,
		"insteadOf = https://github.com/chrissnell/graywolf",
		"insteadOf = git@github.com:chrissnell/graywolf",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("gitconfig missing %q\ngot:\n%s", want, content)
		}
	}

	// Job spec: repocache PVC mounted RO, gitconfig CM subPath-mounted.
	job, err := k.BatchV1().Jobs("multica").Get(context.Background(), jobName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Job missing: %v", err)
	}
	pod := job.Spec.Template.Spec
	if !hasVolume(pod.Volumes, "repocache") {
		t.Errorf("missing repocache volume in pod.Volumes")
	}
	if !hasVolume(pod.Volumes, "gitconfig") {
		t.Errorf("missing gitconfig volume in pod.Volumes")
	}
	for _, v := range pod.Volumes {
		if v.Name == "repocache" {
			if v.PersistentVolumeClaim == nil || v.PersistentVolumeClaim.ClaimName != "multica-repocache-repos" {
				t.Errorf("repocache PVC wired wrong: %+v", v.PersistentVolumeClaim)
			}
			if !v.PersistentVolumeClaim.ReadOnly {
				t.Errorf("repocache PVC must be ReadOnly")
			}
		}
	}

	runtask := pod.Containers[0]
	rcMount := hasVolumeMount(runtask.VolumeMounts, "repocache")
	if rcMount == nil || rcMount.MountPath != "/repos" || !rcMount.ReadOnly {
		t.Errorf("repocache mount wrong: %+v", rcMount)
	}
	gcMount := hasVolumeMount(runtask.VolumeMounts, "gitconfig")
	if gcMount == nil || gcMount.MountPath != "/home/multica/.gitconfig" || gcMount.SubPath != ".gitconfig" {
		t.Errorf("gitconfig mount wrong: %+v", gcMount)
	}
}

// TestDispatchJob_RepoCacheDisabled is the explicit "fallback to direct origin
// clones" assertion called out in the plan's Task 19 sanity check.
func TestDispatchJob_RepoCacheDisabled(t *testing.T) {
	k := fake.NewSimpleClientset()
	r := Registered{
		WorkspaceID: "ws-rc", AgentName: "Lambda", Provider: "claude",
		Image:   "registry/multica-runtime-claude:dev",
		PVCSize: "5Gi",
	}
	task := daemon.Task{
		ID: "task-norc", IssueID: "iss-1", AgentID: "ag-1", WorkspaceID: r.WorkspaceID,
		RuntimeID: "rt-1",
		Repos:     []daemon.RepoData{{URL: "https://github.com/chrissnell/graywolf.git"}},
	}

	jobName, err := DispatchJob(context.Background(), k, "multica", r, task, "ghcr-pull", "pvc-name", ClaudeBrokerOptions{}, RepoCacheOptions{Enabled: false})
	if err != nil {
		t.Fatal(err)
	}

	// No gitconfig CM created.
	if _, err := k.CoreV1().ConfigMaps("multica").Get(context.Background(), "task-"+task.ID[:8]+"-gitconfig", metav1.GetOptions{}); err == nil {
		t.Errorf("gitconfig CM was created despite repocache disabled")
	}

	job, _ := k.BatchV1().Jobs("multica").Get(context.Background(), jobName, metav1.GetOptions{})
	pod := job.Spec.Template.Spec
	if hasVolume(pod.Volumes, "repocache") {
		t.Errorf("repocache volume present despite disabled")
	}
	if hasVolume(pod.Volumes, "gitconfig") {
		t.Errorf("gitconfig volume present despite disabled")
	}
}

