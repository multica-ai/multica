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

	jobName, err := DispatchJob(context.Background(), k, "multica", r, task, "ghcr-pull", "pvc-name", ClaudeBrokerOptions{}, RepoCacheOptions{}, GitHubTokenOptions{}, nil)
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

	jobName, err := DispatchJob(context.Background(), k, "multica", r, task, "ghcr-pull", "pvc-name", cb, RepoCacheOptions{}, GitHubTokenOptions{}, nil)
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

// TestDispatchJob_WithGitHubToken asserts that, when GitHubTokenOptions.SecretName
// is set, DispatchJob adds a GH_TOKEN env var to the runtask container sourced
// from the named Secret. With SecretName empty, no GH_TOKEN must appear.
func TestDispatchJob_WithGitHubToken(t *testing.T) {
	k := fake.NewSimpleClientset()
	r := Registered{
		WorkspaceID: "ws-1", AgentName: "Lambda", Provider: "claude",
		Image:   "registry/multica-runtime-claude:v0.3.0-mk1",
		PVCSize: "5Gi",
	}
	task := daemon.Task{
		ID: "task-gh", IssueID: "iss-1", AgentID: "ag-1", WorkspaceID: r.WorkspaceID,
		RuntimeID: "rt-1",
	}
	gh := GitHubTokenOptions{SecretName: "multica-github-token", SecretKey: "token"}

	jobName, err := DispatchJob(context.Background(), k, "multica", r, task, "ghcr-pull", "pvc-name", ClaudeBrokerOptions{}, RepoCacheOptions{}, gh, nil)
	if err != nil {
		t.Fatal(err)
	}
	job, err := k.BatchV1().Jobs("multica").Get(context.Background(), jobName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Job missing: %v", err)
	}
	runtask := job.Spec.Template.Spec.Containers[0]
	var ghEnv *corev1.EnvVar
	for i := range runtask.Env {
		if runtask.Env[i].Name == "GH_TOKEN" {
			ghEnv = &runtask.Env[i]
		}
	}
	if ghEnv == nil {
		t.Fatalf("runtask missing GH_TOKEN env; env=%+v", runtask.Env)
	}
	if ghEnv.ValueFrom == nil || ghEnv.ValueFrom.SecretKeyRef == nil {
		t.Fatalf("GH_TOKEN must be sourced from secretKeyRef; got %+v", ghEnv)
	}
	ref := ghEnv.ValueFrom.SecretKeyRef
	if ref.Name != "multica-github-token" {
		t.Errorf("secretKeyRef.name = %q, want multica-github-token", ref.Name)
	}
	if ref.Key != "token" {
		t.Errorf("secretKeyRef.key = %q, want token", ref.Key)
	}
}

// TestDispatchJob_NoGitHubToken asserts that when GitHubTokenOptions.SecretName
// is empty, GH_TOKEN does not appear in the runtask env at all.
func TestDispatchJob_NoGitHubToken(t *testing.T) {
	k := fake.NewSimpleClientset()
	r := Registered{
		WorkspaceID: "ws-1", AgentName: "Lambda", Provider: "claude",
		Image:   "registry/multica-runtime-claude:v0.3.0-mk1",
		PVCSize: "5Gi",
	}
	task := daemon.Task{
		ID: "task-no-gh", IssueID: "iss-1", AgentID: "ag-1", WorkspaceID: r.WorkspaceID,
		RuntimeID: "rt-1",
	}

	jobName, err := DispatchJob(context.Background(), k, "multica", r, task, "ghcr-pull", "pvc-name", ClaudeBrokerOptions{}, RepoCacheOptions{}, GitHubTokenOptions{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	job, err := k.BatchV1().Jobs("multica").Get(context.Background(), jobName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Job missing: %v", err)
	}
	for _, e := range job.Spec.Template.Spec.Containers[0].Env {
		if e.Name == "GH_TOKEN" {
			t.Errorf("GH_TOKEN must not be set when SecretName is empty; got %+v", e)
		}
	}
}

// TestDispatchJob_WithWorkerExtraEnv asserts that each WorkerSecretEnvVar is
// injected into the runtask container as an env var sourced via secretKeyRef
// from the named Secret/key — the path that delivers the Cloudflare R2 creds
// (CLOUDFLARE_API_TOKEN, AWS_*) to wrangler/rclone.
func TestDispatchJob_WithWorkerExtraEnv(t *testing.T) {
	k := fake.NewSimpleClientset()
	r := Registered{
		WorkspaceID: "ws-1", AgentName: "Lambda", Provider: "claude",
		Image:   "registry/multica-runtime-claude:v0.3.0-mk1",
		PVCSize: "5Gi",
	}
	task := daemon.Task{
		ID: "task-cf", IssueID: "iss-1", AgentID: "ag-1", WorkspaceID: r.WorkspaceID,
		RuntimeID: "rt-1",
	}
	extraEnv := []WorkerSecretEnvVar{
		{Name: "CLOUDFLARE_API_TOKEN", SecretName: "multica-cloudflare", SecretKey: "api-token"},
		{Name: "AWS_ACCESS_KEY_ID", SecretName: "multica-cloudflare", SecretKey: "access-key-id"},
	}

	jobName, err := DispatchJob(context.Background(), k, "multica", r, task, "ghcr-pull", "pvc-name", ClaudeBrokerOptions{}, RepoCacheOptions{}, GitHubTokenOptions{}, extraEnv)
	if err != nil {
		t.Fatal(err)
	}
	job, err := k.BatchV1().Jobs("multica").Get(context.Background(), jobName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Job missing: %v", err)
	}
	env := job.Spec.Template.Spec.Containers[0].Env
	for _, want := range extraEnv {
		var got *corev1.EnvVar
		for i := range env {
			if env[i].Name == want.Name {
				got = &env[i]
			}
		}
		if got == nil {
			t.Fatalf("runtask missing %s env; env=%+v", want.Name, env)
		}
		if got.ValueFrom == nil || got.ValueFrom.SecretKeyRef == nil {
			t.Fatalf("%s must be sourced from secretKeyRef; got %+v", want.Name, got)
		}
		ref := got.ValueFrom.SecretKeyRef
		if ref.Name != want.SecretName || ref.Key != want.SecretKey {
			t.Errorf("%s secretKeyRef = %s/%s, want %s/%s", want.Name, ref.Name, ref.Key, want.SecretName, want.SecretKey)
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

// TestDispatchJob_PersistsClaudeProjects asserts that DispatchJob mounts the
// work PVC a second time, with SubPath "claude-projects", at the location
// where claude stores per-conversation jsonl files. Without this mount,
// /home/multica/.claude is an emptyDir that vanishes when the worker Pod
// ends, every follow-up task fails `claude --resume <session-id>`, and the
// daemon never recovers session continuity across tasks.
func TestDispatchJob_PersistsClaudeProjects(t *testing.T) {
	k := fake.NewSimpleClientset()
	r := Registered{
		WorkspaceID: "ws-1", AgentName: "Lambda", Provider: "claude",
		Image:   "registry/multica-runtime-claude:v0.3.0-mk1",
		PVCSize: "5Gi",
	}
	task := daemon.Task{
		ID: "task-resume", IssueID: "iss-1", AgentID: "ag-1", WorkspaceID: r.WorkspaceID,
		RuntimeID: "rt-1",
	}

	jobName, err := DispatchJob(context.Background(), k, "multica", r, task, "ghcr-pull", "pvc-name", ClaudeBrokerOptions{}, RepoCacheOptions{}, GitHubTokenOptions{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	job, err := k.BatchV1().Jobs("multica").Get(context.Background(), jobName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Job missing: %v", err)
	}
	runtask := job.Spec.Template.Spec.Containers[0]

	// Find both mounts of the work volume; one is the workdir at /work, the
	// other is the claude session-store reuse.
	var workMount, projectsMount *corev1.VolumeMount
	for i := range runtask.VolumeMounts {
		m := &runtask.VolumeMounts[i]
		if m.Name != "work" {
			continue
		}
		switch m.MountPath {
		case "/work":
			workMount = m
		case "/home/multica/.claude/projects":
			projectsMount = m
		}
	}
	if workMount == nil {
		t.Fatalf("work PVC must remain mounted at /work; mounts=%+v", runtask.VolumeMounts)
	}
	if workMount.SubPath != "" {
		t.Errorf("/work mount must have empty SubPath; got %q", workMount.SubPath)
	}
	if projectsMount == nil {
		t.Fatalf("work PVC must also be mounted at /home/multica/.claude/projects so session files persist across worker pods; mounts=%+v", runtask.VolumeMounts)
	}
	if projectsMount.SubPath != "claude-projects" {
		t.Errorf("projects mount SubPath = %q, want \"claude-projects\"", projectsMount.SubPath)
	}
	// The mount must be writable — claude writes its own jsonl files.
	if projectsMount.ReadOnly {
		t.Errorf("projects mount must not be ReadOnly; claude writes session files into it")
	}
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

	jobName, err := DispatchJob(context.Background(), k, "multica", r, task, "ghcr-pull", "pvc-name", ClaudeBrokerOptions{}, rc, GitHubTokenOptions{}, nil)
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
		// Push must route to SSH origin, NOT the read-only cache PVC.
		`[url "git@github.com:chrissnell/graywolf.git"]`,
		"pushInsteadOf = https://github.com/chrissnell/graywolf",
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

	// MULTICA_REPOCACHE_DIR tells the worker daemon that the bare cache
	// is mounted RO and externally managed, so it should swap in the
	// controller-mode /repo/checkout handler (Cache.CreateSharedClone).
	var sawEnv bool
	for _, e := range runtask.Env {
		if e.Name == "MULTICA_REPOCACHE_DIR" {
			sawEnv = true
			if e.Value != "/repos" {
				t.Errorf("MULTICA_REPOCACHE_DIR = %q, want /repos", e.Value)
			}
		}
	}
	if !sawEnv {
		t.Errorf("MULTICA_REPOCACHE_DIR env missing — worker daemon won't enter controller mode")
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

	jobName, err := DispatchJob(context.Background(), k, "multica", r, task, "ghcr-pull", "pvc-name", ClaudeBrokerOptions{}, RepoCacheOptions{Enabled: false}, GitHubTokenOptions{}, nil)
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
	for _, e := range pod.Containers[0].Env {
		if e.Name == "MULTICA_REPOCACHE_DIR" {
			t.Errorf("MULTICA_REPOCACHE_DIR leaked despite repocache disabled — worker daemon would enter controller mode without a cache mount")
		}
	}
}

// TestDispatchJob_WorkerServiceAccount asserts that Registered.ServiceAccountName
// (set per-workspace from the chart's runtime.worker.profiles lookup) is
// propagated to PodSpec.ServiceAccountName. This is what gives the projected
// SA token at /var/run/secrets/kubernetes.io/serviceaccount/ the rights bound
// by the chart's worker-rbac.yaml — without it, `kubectl` / `helm` from inside
// the pod see the namespace default SA (no permissions) and fail with 403.
func TestDispatchJob_WorkerServiceAccount(t *testing.T) {
	k := fake.NewSimpleClientset()
	r := Registered{
		WorkspaceID: "ws-sa", AgentName: "Lambda", Provider: "claude",
		Image:              "registry/multica-runtime-claude:dev",
		PVCSize:            "5Gi",
		ServiceAccountName: "multica-runtime-worker-k8s-admin",
	}
	task := daemon.Task{
		ID: "task-sa", IssueID: "iss-1", AgentID: "ag-1", WorkspaceID: r.WorkspaceID,
		RuntimeID: "rt-1",
	}

	jobName, err := DispatchJob(context.Background(), k, "multica", r, task, "ghcr-pull", "pvc-name", ClaudeBrokerOptions{}, RepoCacheOptions{}, GitHubTokenOptions{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	job, _ := k.BatchV1().Jobs("multica").Get(context.Background(), jobName, metav1.GetOptions{})
	if got := job.Spec.Template.Spec.ServiceAccountName; got != "multica-runtime-worker-k8s-admin" {
		t.Errorf("ServiceAccountName: got %q, want %q", got, "multica-runtime-worker-k8s-admin")
	}
}

// TestDispatchJob_NoWorkerServiceAccount asserts that an empty
// Registered.ServiceAccountName leaves PodSpec.ServiceAccountName empty —
// the pod falls back to the namespace `default` SA, i.e. no cluster API access.
// This is the deliberate "agent without K8S access" case: the workspace's
// runtime.workspaces[].workerProfile is unset.
func TestDispatchJob_NoWorkerServiceAccount(t *testing.T) {
	k := fake.NewSimpleClientset()
	r := Registered{
		WorkspaceID: "ws-nosa", AgentName: "Lambda", Provider: "claude",
		Image:   "registry/multica-runtime-claude:dev",
		PVCSize: "5Gi",
	}
	task := daemon.Task{
		ID: "task-nosa", IssueID: "iss-1", AgentID: "ag-1", WorkspaceID: r.WorkspaceID,
		RuntimeID: "rt-1",
	}

	jobName, err := DispatchJob(context.Background(), k, "multica", r, task, "ghcr-pull", "pvc-name", ClaudeBrokerOptions{}, RepoCacheOptions{}, GitHubTokenOptions{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	job, _ := k.BatchV1().Jobs("multica").Get(context.Background(), jobName, metav1.GetOptions{})
	if got := job.Spec.Template.Spec.ServiceAccountName; got != "" {
		t.Errorf("ServiceAccountName: got %q, want empty (default SA fallback)", got)
	}
}
