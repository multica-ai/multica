# Tool Cache + Per-Issue PVC Auto-GC (Plan F) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Two controller-side improvements on top of Plan E:
1. **Tool cache** — a shared RWX PVC mounted at `/caches` on every Job pod, pre-organized for `npm`, `pip`, `cargo`, `go`. Reuses dependency downloads across tasks even when their workdir PVCs are different.
2. **Auto-GC of per-issue PVCs** — the controller periodically asks Multica `GET /api/daemon/issues/{id}/gc-check`; PVCs for issues that have been `done` or `cancelled` for longer than the grace period get deleted. Reopened issues lose the deletion candidacy automatically.

**Architecture:** No new binaries. Both changes are additions to the existing `multica-k8s-controller` plus one new chart template (the tool-cache PVC). The controller's `DispatchJob` gains a tool-cache mount + env vars. A new `SweepGCIssuePVCs` function runs on a periodic ticker alongside the existing failed-Job sweep.

**Tech stack:** Same as Plan E. No new dependencies.

**Source spec:** `docs/superpowers/specs/2026-05-20-multica-k8s-design.md` — §5.6 (per-issue PVCs, lifecycle), §5.7 (tool cache), §5.3 step 8 (issue lifecycle GC).

**Builds on:** Plan E (controller, per-issue PVCs, Job dispatch). The controller image tag bumps; everything else is unchanged.

---

## Key facts established by code reading (do not re-investigate)

- **GC-check endpoint:** `GET /api/daemon/issues/{id}/gc-check` → `IssueGCStatus{Status string, UpdatedAt time.Time}`. Confirmed in `server/internal/daemon/client.go:313–325`. Already exposed via `daemon.Client.GetIssueGCCheck(ctx, issueID)`.
- **Terminal statuses:** `"done"` and `"cancelled"` (matching the daemon's GC logic in `daemon/gc.go`). Anything else means the issue is still active.
- **404 from gc-check** means the issue was deleted entirely — treat as immediately deletable.
- **Per-issue PVC labels** (added in Plan E Task 5): `multica.ai/workspace-id`, `multica.ai/agent-id`, `multica.ai/issue-id`, `multica.ai/runtime-id`, `app.kubernetes.io/managed-by=multica-k8s-controller`. We label-select on the last one to scope the sweep.
- **Tool caches are race-tolerant by design:** npm/pip/cargo/go use lockfiles or atomic moves on cache writes. No coordination needed across concurrent Jobs sharing one RWX PVC.
- **Job spec from Plan E (`jobs.go:DispatchJob`)** already declares volumes/mounts in named slots — extending it for an extra volume + 4 env vars is additive, no restructuring.

---

## File structure

### Modified by this plan

```
server/cmd/multica-k8s-controller/
├── config.go                    # +ToolCachePVCName, +GCGracePeriod fields
├── config_test.go               # +test for the two new fields
├── jobs.go                      # +toolCachePVC param, +mount + env vars in DispatchJob
├── jobs_test.go                 # +assertion the Job mounts /caches
├── dispatcher.go                # +pass toolCachePVC through to DispatchJob
├── dispatcher_test.go           # (regenerated assertions for the new signature)
├── watcher.go                   # +SweepGCIssuePVCs
├── watcher_test.go              # +tests for the GC sweep
└── main.go                      # +periodic ticker for GC sweep

packaging/helm/multica/values.yaml                          # +runtime.controller.toolCache, +runtime.controller.gc
packaging/helm/multica/templates/runtime/
├── tool-cache-pvc.yaml          # CREATE: RWX PVC, gated on mode==controller
└── controller-configmap.yaml    # +toolCachePVCName, +gcGracePeriod into runtime.yaml

packaging/README.md              # +tool cache + auto-GC sections
~/kube/apps/multica/values.yaml  # +tool-cache size + storage class, optional gc tuning
```

### Reused unchanged

- `RegisterAll`, `RunHeartbeatLoop`, `DispatchOnce`, `EnsurePVC`, `SweepFailedJobs`, the runtime image, all three worker Secrets, RBAC.

---

## Prerequisites

1. Plans A, C, D, E executed: controller running, per-task Jobs spawning, per-issue PVCs accumulating.
2. Cluster has an **RWX** storage class (the tool cache requires it). Confirm:
   ```bash
   kubectl get storageclass
   ```
   Identify the one that supports `ReadWriteMany`. (You said earlier you have RWX CSI available — pin the name.)
3. Go toolchain 1.26+, `docker login ghcr.io` done, `GHCR_PAT` exported.
4. Pick a fresh image tag for this plan. Export:
   ```bash
   export TAG=v0.3.1-mk1
   ```

---

## Task 1: Controller config — tool cache + GC settings

**Files:**
- Modify: `server/cmd/multica-k8s-controller/config.go`
- Modify: `server/cmd/multica-k8s-controller/config_test.go`

- [ ] **Step 1: Extend the test fixture**

In `server/cmd/multica-k8s-controller/config_test.go`, find `TestLoadConfig_FromEnvAndFile`. After the existing assertions, add:

```go
	if got.ToolCachePVCName != "multica-tool-cache" {
		t.Errorf("ToolCachePVCName default = %q", got.ToolCachePVCName)
	}
	if got.GCGracePeriod != 24*time.Hour {
		t.Errorf("GCGracePeriod default = %v", got.GCGracePeriod)
	}
```

Then add a new test exercising overrides:

```go
func TestLoadConfig_GCAndToolCacheOverrides(t *testing.T) {
	cfgDir := t.TempDir()
	cfgYAML := []byte(`
workspaces:
  - id: 11111111-1111-1111-1111-111111111111
    provider: claude
    agentName: Lambda
    runtimeImage: ghcr.io/chrissnell/multica-runtime-claude:v0.3.1-mk1
    pvcSize: 5Gi
toolCachePVCName: cache-xl
gcGracePeriod: 1h
`)
	if err := os.WriteFile(filepath.Join(cfgDir, "runtime.yaml"), cfgYAML, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MULTICA_SERVER_URL", "http://x.invalid")
	t.Setenv("MULTICA_TOKEN", "tk")
	t.Setenv("POD_NAMESPACE", "multica")
	t.Setenv("CONTROLLER_CONFIG_DIR", cfgDir)

	got, err := LoadConfig()
	if err != nil { t.Fatal(err) }
	if got.ToolCachePVCName != "cache-xl" {
		t.Errorf("ToolCachePVCName not overridden: %q", got.ToolCachePVCName)
	}
	if got.GCGracePeriod != time.Hour {
		t.Errorf("GCGracePeriod not overridden: %v", got.GCGracePeriod)
	}
}
```

- [ ] **Step 2: Verify the new test fails**

```bash
cd /Users/cjs/dev/multica/server
go test ./cmd/multica-k8s-controller/ -run TestLoadConfig -v 2>&1 | tail -10
```

Expected: FAIL — the existing test's new assertions don't compile (`got.ToolCachePVCName undefined`).

- [ ] **Step 3: Add the fields and defaults**

In `server/cmd/multica-k8s-controller/config.go`, extend the `Config` struct:

```go
type Config struct {
	ServerBaseURL string
	Token         string
	Namespace     string

	Workspaces      []WorkspaceConfig `yaml:"workspaces"`
	ImagePullSecret string            `yaml:"imagePullSecret"`

	// New in Plan F:
	ToolCachePVCName string        `yaml:"toolCachePVCName"`
	GCGracePeriod    time.Duration `yaml:"gcGracePeriod"`

	PollInterval      time.Duration
	HeartbeatInterval time.Duration

	DaemonIDPrefix string
	DeviceName     string
}
```

And in `LoadConfig`, set the defaults after parsing the YAML (before the final `return cfg, nil`):

```go
	if cfg.ToolCachePVCName == "" {
		cfg.ToolCachePVCName = "multica-tool-cache"
	}
	if cfg.GCGracePeriod == 0 {
		cfg.GCGracePeriod = 24 * time.Hour
	}
```

`yaml.v3` parses Go `time.Duration` from strings like `"24h"` or `"1h"` natively.

- [ ] **Step 4: Tests pass**

```bash
go test ./cmd/multica-k8s-controller/ -run TestLoadConfig -v 2>&1 | tail -10
```

Expected: both tests PASS.

- [ ] **Step 5: Commit**

```bash
cd /Users/cjs/dev/multica
git add server/cmd/multica-k8s-controller/config.go server/cmd/multica-k8s-controller/config_test.go
git commit -m "feat(controller): config — tool cache PVC name + GC grace period"
```

---

## Task 2: `DispatchJob` mounts `/caches` and exports cache env vars

**Files:**
- Modify: `server/cmd/multica-k8s-controller/jobs.go`
- Modify: `server/cmd/multica-k8s-controller/jobs_test.go`

- [ ] **Step 1: Update the test**

In `server/cmd/multica-k8s-controller/jobs_test.go`, find `TestCreateJob_AndPayloadConfigMap`. Update the `DispatchJob` call signature to include a tool-cache PVC name argument — pass `"multica-tool-cache"`. Then after the existing Job-exists assertion, add:

```go
	// Tool cache: volume present + env vars set on the runtask container.
	pod := job.Spec.Template.Spec
	var tcMount *corev1.VolumeMount
	for _, m := range pod.Containers[0].VolumeMounts {
		if m.Name == "tool-cache" {
			m2 := m
			tcMount = &m2
		}
	}
	if tcMount == nil || tcMount.MountPath != "/caches" {
		t.Fatalf("tool-cache mount missing or wrong path: %+v", pod.Containers[0].VolumeMounts)
	}
	var tcVol *corev1.Volume
	for _, v := range pod.Volumes {
		if v.Name == "tool-cache" {
			v2 := v
			tcVol = &v2
		}
	}
	if tcVol == nil || tcVol.PersistentVolumeClaim == nil || tcVol.PersistentVolumeClaim.ClaimName != "multica-tool-cache" {
		t.Fatalf("tool-cache volume missing or wrong PVC: %+v", tcVol)
	}
	wantEnv := map[string]string{
		"npm_config_cache":  "/caches/npm",
		"PIP_CACHE_DIR":     "/caches/pip",
		"CARGO_HOME":        "/caches/cargo",
		"GOMODCACHE":        "/caches/go-mod",
	}
	gotEnv := map[string]string{}
	for _, e := range pod.Containers[0].Env {
		gotEnv[e.Name] = e.Value
	}
	for k, v := range wantEnv {
		if gotEnv[k] != v {
			t.Errorf("env %s=%q, want %q", k, gotEnv[k], v)
		}
	}
```

- [ ] **Step 2: Update the test for `TestEnsurePVC*` callers if they call DispatchJob**

(Most likely they don't — `EnsurePVC` is independent of `DispatchJob`. Skim the test file to confirm; if any other test calls `DispatchJob`, add `"multica-tool-cache"` to its arg list.)

- [ ] **Step 3: Verify the test fails to compile**

```bash
go test ./cmd/multica-k8s-controller/ -run TestCreateJob -v 2>&1 | tail -10
```

Expected: FAIL — `not enough arguments in call to DispatchJob`.

- [ ] **Step 4: Update `DispatchJob` signature + body**

In `server/cmd/multica-k8s-controller/jobs.go`:

1. Add a parameter to the signature:

```go
func DispatchJob(ctx context.Context, k kubernetes.Interface, namespace string, r Registered, t daemon.Task, imagePullSecret, pvc, toolCachePVC string) (string, error) {
```

2. In the container's `VolumeMounts` slice, add after the existing `work` mount:

```go
							{Name: "tool-cache", MountPath: "/caches"},
```

3. In the container's `Env` slice, append:

```go
							{Name: "npm_config_cache", Value: "/caches/npm"},
							{Name: "PIP_CACHE_DIR", Value: "/caches/pip"},
							{Name: "CARGO_HOME", Value: "/caches/cargo"},
							{Name: "GOMODCACHE", Value: "/caches/go-mod"},
```

4. In the pod's `Volumes` slice, add after the existing `work` volume:

```go
						{Name: "tool-cache", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: toolCachePVC}}},
```

- [ ] **Step 5: Update the dispatcher to pass the new arg**

In `server/cmd/multica-k8s-controller/dispatcher.go`, `DispatchOnce`:

1. Add the parameter to the function signature:

```go
func DispatchOnce(ctx context.Context, cli *daemon.Client, k kubernetes.Interface, namespace, imagePullSecret, toolCachePVC string, r Registered) (bool, error) {
```

2. Pass it through to `DispatchJob`:

```go
	if _, err := DispatchJob(ctx, k, namespace, r, *task, imagePullSecret, pvc, toolCachePVC); err != nil {
```

- [ ] **Step 6: Update the dispatcher test signature**

In `server/cmd/multica-k8s-controller/dispatcher_test.go`, `TestDispatchOnce_CreatesPVCAndJobForClaimedTask`, both `DispatchOnce` calls need the new arg:

```go
	dispatched, err := DispatchOnce(context.Background(), cli, k, "multica", "ghcr-pull", "multica-tool-cache", r)
```

(Twice — for the first claim and the second-call no-task verification.)

- [ ] **Step 7: Run all controller tests**

```bash
go test ./cmd/multica-k8s-controller/ 2>&1 | tail -15
```

Expected: all PASS.

- [ ] **Step 8: Commit**

```bash
git add server/cmd/multica-k8s-controller/jobs.go server/cmd/multica-k8s-controller/jobs_test.go \
        server/cmd/multica-k8s-controller/dispatcher.go server/cmd/multica-k8s-controller/dispatcher_test.go
git commit -m "feat(controller): mount tool cache + npm/pip/cargo/go env vars on Jobs"
```

---

## Task 3: Wire `toolCachePVC` through `main.go`

**Files:**
- Modify: `server/cmd/multica-k8s-controller/main.go`

- [ ] **Step 1: Update the dispatch call**

In `main.go`'s poll loop, the call to `DispatchOnce` currently passes 6 args. Add `cfg.ToolCachePVCName`:

```go
				dispatched, err := DispatchOnce(ctx, cli, k, cfg.Namespace, cfg.ImagePullSecret, cfg.ToolCachePVCName, r)
```

- [ ] **Step 2: Build the binary**

```bash
cd /Users/cjs/dev/multica/server
go build ./cmd/multica-k8s-controller
```

Expected: clean build.

- [ ] **Step 3: Commit**

```bash
git add server/cmd/multica-k8s-controller/main.go
git commit -m "feat(controller): main loop passes tool cache PVC to dispatcher"
```

---

## Task 4: `SweepGCIssuePVCs` — controller-side per-issue PVC GC

**Files:**
- Modify: `server/cmd/multica-k8s-controller/watcher.go`
- Modify: `server/cmd/multica-k8s-controller/watcher_test.go`

- [ ] **Step 1: Write tests for three cases**

Append to `server/cmd/multica-k8s-controller/watcher_test.go`:

```go
import (
	// add to existing imports:
	"encoding/json"
	"time"
)

// helper: stub the gc-check API
type gcCheckBehavior struct{ status string; updated time.Time; notFound bool }

func gcCheckServer(t *testing.T, byIssue map[string]gcCheckBehavior) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// path: /api/daemon/issues/{id}/gc-check
		parts := strings.Split(r.URL.Path, "/")
		// .../issues/{id}/gc-check
		var id string
		for i, p := range parts {
			if p == "issues" && i+1 < len(parts) {
				id = parts[i+1]
				break
			}
		}
		b, ok := byIssue[id]
		if !ok || b.notFound {
			http.NotFound(w, r); return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":     b.status,
			"updated_at": b.updated.Format(time.RFC3339),
		})
	}))
}

func makePVC(name, issueID string) *corev1.PersistentVolumeClaim {
	return &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: name, Namespace: "multica",
			Labels: map[string]string{
				labelManagedBy: managedByValue,
				labelIssueID:   issueID,
			},
		},
	}
}

func TestSweepGCIssuePVCs_DeletesOldDone(t *testing.T) {
	srv := gcCheckServer(t, map[string]gcCheckBehavior{
		"iss-old": {status: "done", updated: time.Now().Add(-48 * time.Hour)},
	})
	defer srv.Close()
	cli := daemon.NewClient(srv.URL); cli.SetToken("tk")

	k := fake.NewSimpleClientset(makePVC("wd-x", "iss-old"))
	if err := SweepGCIssuePVCs(context.Background(), cli, k, "multica", 24*time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := k.CoreV1().PersistentVolumeClaims("multica").Get(context.Background(), "wd-x", metav1.GetOptions{}); err == nil {
		t.Errorf("expected wd-x deleted")
	}
}

func TestSweepGCIssuePVCs_KeepsRecentDone(t *testing.T) {
	srv := gcCheckServer(t, map[string]gcCheckBehavior{
		"iss-fresh": {status: "done", updated: time.Now().Add(-1 * time.Hour)},
	})
	defer srv.Close()
	cli := daemon.NewClient(srv.URL); cli.SetToken("tk")

	k := fake.NewSimpleClientset(makePVC("wd-fresh", "iss-fresh"))
	if err := SweepGCIssuePVCs(context.Background(), cli, k, "multica", 24*time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := k.CoreV1().PersistentVolumeClaims("multica").Get(context.Background(), "wd-fresh", metav1.GetOptions{}); err != nil {
		t.Errorf("expected wd-fresh kept (grace not elapsed), got: %v", err)
	}
}

func TestSweepGCIssuePVCs_KeepsActiveIssue(t *testing.T) {
	srv := gcCheckServer(t, map[string]gcCheckBehavior{
		"iss-active": {status: "in_progress", updated: time.Now()},
	})
	defer srv.Close()
	cli := daemon.NewClient(srv.URL); cli.SetToken("tk")

	k := fake.NewSimpleClientset(makePVC("wd-active", "iss-active"))
	if err := SweepGCIssuePVCs(context.Background(), cli, k, "multica", 24*time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := k.CoreV1().PersistentVolumeClaims("multica").Get(context.Background(), "wd-active", metav1.GetOptions{}); err != nil {
		t.Errorf("expected wd-active kept (issue still active): %v", err)
	}
}

func TestSweepGCIssuePVCs_DeletesOn404(t *testing.T) {
	srv := gcCheckServer(t, map[string]gcCheckBehavior{
		"iss-gone": {notFound: true},
	})
	defer srv.Close()
	cli := daemon.NewClient(srv.URL); cli.SetToken("tk")

	k := fake.NewSimpleClientset(makePVC("wd-gone", "iss-gone"))
	if err := SweepGCIssuePVCs(context.Background(), cli, k, "multica", 24*time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := k.CoreV1().PersistentVolumeClaims("multica").Get(context.Background(), "wd-gone", metav1.GetOptions{}); err == nil {
		t.Errorf("expected wd-gone deleted on issue 404")
	}
}
```

You may need to add `"strings"` to imports.

- [ ] **Step 2: Verify the tests fail**

```bash
go test ./cmd/multica-k8s-controller/ -run TestSweepGCIssuePVCs -v 2>&1 | tail -10
```

Expected: FAIL — `SweepGCIssuePVCs` undefined.

- [ ] **Step 3: Implement `SweepGCIssuePVCs`**

Append to `server/cmd/multica-k8s-controller/watcher.go`:

```go
import (
	// add to existing imports:
	"strings"
	"time"
)

// SweepGCIssuePVCs deletes per-issue workdir PVCs whose backing issue has been
// `done` or `cancelled` for longer than gracePeriod. PVCs for active issues
// are left alone; reopening an issue thus automatically retains its PVC.
// PVCs whose issues return 404 are deleted (the issue was hard-deleted).
//
// Selector is the same as SweepFailedJobs: `multica.ai/issue-id` label and
// app.kubernetes.io/managed-by = multica-k8s-controller.
func SweepGCIssuePVCs(ctx context.Context, cli *daemon.Client, k kubernetes.Interface, namespace string, gracePeriod time.Duration) error {
	pvcs, err := k.CoreV1().PersistentVolumeClaims(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelManagedBy + "=" + managedByValue,
	})
	if err != nil {
		return fmt.Errorf("list pvcs: %w", err)
	}
	now := time.Now()
	for _, pvc := range pvcs.Items {
		issueID := pvc.Labels[labelIssueID]
		if issueID == "" {
			continue
		}
		gc, err := cli.GetIssueGCCheck(ctx, issueID)
		if err != nil {
			if isNotFoundErr(err) {
				_ = k.CoreV1().PersistentVolumeClaims(namespace).Delete(ctx, pvc.Name, metav1.DeleteOptions{})
			}
			// Other errors (network blip etc.) — skip; next sweep will retry.
			continue
		}
		terminal := gc.Status == "done" || gc.Status == "cancelled"
		if !terminal {
			continue
		}
		if now.Sub(gc.UpdatedAt) < gracePeriod {
			continue
		}
		_ = k.CoreV1().PersistentVolumeClaims(namespace).Delete(ctx, pvc.Name, metav1.DeleteOptions{})
	}
	return nil
}

// isNotFoundErr returns true when the daemon Client's getJSON wrapped a 404.
// The wrapped error string contains "404" — Client.getJSON does not preserve
// a typed sentinel today, so we string-check. Update this when daemon.Client
// gains a typed NotFound.
func isNotFoundErr(err error) bool {
	if err == nil { return false }
	return strings.Contains(err.Error(), "404") || strings.Contains(strings.ToLower(err.Error()), "not found")
}
```

**Note**: if `daemon.Client.GetIssueGCCheck` already returns a typed not-found error, simplify `isNotFoundErr` accordingly. Check by running:

```bash
grep -nE "ErrNotFound|StatusCode|http.StatusNotFound" server/internal/daemon/client.go | head
```

If a typed sentinel exists, use `errors.Is(err, daemon.ErrNotFound)` instead of the string sniff.

- [ ] **Step 4: Run the tests**

```bash
go test ./cmd/multica-k8s-controller/ -run TestSweepGCIssuePVCs -v 2>&1 | tail -15
```

Expected: all 4 PASS.

- [ ] **Step 5: Run full controller tests**

```bash
go test ./cmd/multica-k8s-controller/ 2>&1 | tail -10
```

Expected: all PASS.

- [ ] **Step 6: Commit**

```bash
git add server/cmd/multica-k8s-controller/watcher.go server/cmd/multica-k8s-controller/watcher_test.go
git commit -m "feat(controller): SweepGCIssuePVCs — delete per-issue PVCs after grace period"
```

---

## Task 5: Wire the GC sweep into the main loop

**Files:**
- Modify: `server/cmd/multica-k8s-controller/main.go`

- [ ] **Step 1: Add a periodic ticker for the new sweep**

In `main.go`, after the existing `sweepTicker` block (the one calling `SweepFailedJobs`), add a peer block:

```go
	// Per-issue PVC GC sweep — every 30 minutes.
	gcTicker := time.NewTicker(30 * time.Minute)
	defer gcTicker.Stop()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-gcTicker.C:
				if err := SweepGCIssuePVCs(ctx, cli, k, cfg.Namespace, cfg.GCGracePeriod); err != nil {
					logger.Warn("sweep gc pvcs", "error", err)
				}
			}
		}
	}()
```

30 minutes balances responsiveness against API load. The default grace period is 24h, so being late by 30 minutes is fine.

- [ ] **Step 2: Build the binary**

```bash
cd /Users/cjs/dev/multica/server
go build ./cmd/multica-k8s-controller
```

Expected: clean build.

- [ ] **Step 3: Full Go suite regression**

```bash
go vet ./...
go test ./... 2>&1 | tail -10
```

Expected: clean vet, all pass.

- [ ] **Step 4: Commit**

```bash
git add server/cmd/multica-k8s-controller/main.go
git commit -m "feat(controller): periodic per-issue PVC GC sweep"
```

---

## Task 6: Chart values for tool cache + GC tuning

**Files:**
- Modify: `packaging/helm/multica/values.yaml`

- [ ] **Step 1: Extend `runtime.controller`**

In `packaging/helm/multica/values.yaml`, inside the `runtime.controller:` block, add:

```yaml
    toolCache:
      enabled: true
      storageClass: ""        # RWX storage class
      size: 30Gi
      pvcName: multica-tool-cache

    gc:
      gracePeriod: 24h
```

- [ ] **Step 2: Commit**

```bash
cd /Users/cjs/dev/multica
git add packaging/helm/multica/values.yaml
git commit -m "feat(helm): runtime.controller.toolCache + runtime.controller.gc values"
```

---

## Task 7: Tool-cache PVC chart template

**Files:**
- Create: `packaging/helm/multica/templates/runtime/tool-cache-pvc.yaml`

- [ ] **Step 1: Write the template**

Create the file with:

```yaml
{{- if and .Values.runtime.enabled (eq .Values.runtime.mode "controller") .Values.runtime.controller.toolCache.enabled }}
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: {{ .Values.runtime.controller.toolCache.pvcName }}
  namespace: {{ .Release.Namespace }}
  labels:
    {{- include "multica.componentLabels" (dict "name" "tool-cache" "ctx" .) | nindent 4 }}
  annotations:
    "helm.sh/resource-policy": keep
spec:
  accessModes: ["ReadWriteMany"]
  {{- with .Values.runtime.controller.toolCache.storageClass }}
  storageClassName: {{ . | quote }}
  {{- end }}
  resources:
    requests:
      storage: {{ .Values.runtime.controller.toolCache.size }}
{{- end }}
```

The `helm.sh/resource-policy: keep` annotation prevents `helm uninstall` from deleting a populated cache.

- [ ] **Step 2: Render**

```bash
helm template multica packaging/helm/multica/ \
  --set hostname=multica.chrissnell.com \
  --set image.registry=ghcr.io/chrissnell \
  --set image.tag="$TAG" \
  --set runtime.enabled=true --set runtime.mode=controller \
  --set runtime.workspaceId=00000000-0000-0000-0000-000000000000 \
  -s templates/runtime/tool-cache-pvc.yaml
```

Expected: a PVC named `multica-tool-cache`, `accessModes: ReadWriteMany`, `storage: 30Gi`.

- [ ] **Step 3: Verify it's gated off in daemon mode**

```bash
helm template multica packaging/helm/multica/ \
  --set hostname=multica.chrissnell.com \
  --set image.registry=ghcr.io/chrissnell \
  --set image.tag="$TAG" \
  --set runtime.enabled=true --set runtime.mode=daemon \
  --set runtime.workspaceId=foo \
  -s templates/runtime/tool-cache-pvc.yaml
```

Expected: empty.

- [ ] **Step 4: Commit**

```bash
git add packaging/helm/multica/templates/runtime/tool-cache-pvc.yaml
git commit -m "feat(helm): tool-cache RWX PVC for controller mode"
```

---

## Task 8: Surface tool cache + GC settings in the controller ConfigMap

**Files:**
- Modify: `packaging/helm/multica/templates/runtime/controller-configmap.yaml`

The controller's `runtime.yaml` (loaded from the ConfigMap) must now include `toolCachePVCName` and `gcGracePeriod`.

- [ ] **Step 1: Extend the template**

In `packaging/helm/multica/templates/runtime/controller-configmap.yaml`, after the existing `imagePullSecret:` line in the `data:` block, add:

```yaml
    toolCachePVCName: {{ .Values.runtime.controller.toolCache.pvcName | quote }}
    gcGracePeriod: {{ .Values.runtime.controller.gc.gracePeriod | quote }}
```

(Both inside the `runtime.yaml: |` heredoc.)

- [ ] **Step 2: Render and check both fields appear**

```bash
helm template multica packaging/helm/multica/ \
  --set hostname=multica.chrissnell.com \
  --set image.registry=ghcr.io/chrissnell \
  --set image.tag="$TAG" \
  --set runtime.enabled=true --set runtime.mode=controller \
  --set runtime.workspaceId=00000000-0000-0000-0000-000000000000 \
  -s templates/runtime/controller-configmap.yaml
```

Expected: the rendered ConfigMap's `runtime.yaml` contains both `toolCachePVCName: "multica-tool-cache"` and `gcGracePeriod: "24h"`.

- [ ] **Step 3: Commit**

```bash
git add packaging/helm/multica/templates/runtime/controller-configmap.yaml
git commit -m "feat(helm): controller ConfigMap surfaces toolCache + gc settings"
```

---

## Task 9: Rebuild and push controller image

**Files:** none (image push).

- [ ] **Step 1: Build and push**

```bash
cd /Users/cjs/dev/multica
./packaging/scripts/build-images.sh --tag "$TAG" controller
```

Expected: pushes `ghcr.io/chrissnell/multica-controller:v0.3.1-mk1`.

- [ ] **Step 2: Verify the tag**

```bash
curl -s -H "Authorization: Bearer $(echo $GHCR_PAT | base64)" \
  https://ghcr.io/v2/chrissnell/multica-controller/tags/list | head -c 400; echo
```

Expected: tag list includes `v0.3.1-mk1`.

(No need to rebuild the runtime image — the worker doesn't need any code changes; the cache env vars come from the Job spec.)

---

## Task 10: Update override values and deploy

**Files:**
- Modify: `~/kube/apps/multica/values.yaml`

- [ ] **Step 1: Bump tag + set RWX storage class**

In `~/kube/apps/multica/values.yaml`:

```yaml
image:
  tag: v0.3.1-mk1            # bumped

runtime:
  # ... existing keys ...
  controller:
    # ... existing keys (replicaCount, image, etc) ...
    toolCache:
      enabled: true
      storageClass: "your-rwx-storage-class"   # the one you noted in prereqs
      size: 30Gi
    gc:
      gracePeriod: 24h
```

- [ ] **Step 2: Deploy**

```bash
cd /Users/cjs/dev/multica
helm upgrade --install multica packaging/helm/multica/ \
  --namespace multica \
  -f ~/kube/apps/multica/values.yaml
```

Expected: release upgraded.

- [ ] **Step 3: Confirm the tool-cache PVC binds**

```bash
kubectl -n multica get pvc multica-tool-cache
```

Expected: STATUS `Bound`, ACCESS MODES `RWX`, size 30Gi.

- [ ] **Step 4: Confirm the controller pod rolls and loads the new config**

```bash
kubectl -n multica rollout status deploy/multica-controller
kubectl -n multica logs deploy/multica-controller --tail=30 | grep -E "registered|polling"
```

Expected: rollout succeeds; logs show the new image, runtimes re-registered.

---

## Task 11: End-to-end — assign a task and confirm tool-cache mount

**Files:** none.

- [ ] **Step 1: Assign a task that uses npm (or pip, etc.)**

In the web UI, create an issue against a Node-using repo (e.g. "Add a small change to <some Node project>"). Assign it.

- [ ] **Step 2: While the Job is running, exec into it and inspect mounts**

```bash
JOB_POD=$(kubectl -n multica get pod -l app.kubernetes.io/managed-by=multica-k8s-controller --field-selector=status.phase=Running -o name | head -1)
kubectl -n multica exec "$JOB_POD" -- ls -la /caches
kubectl -n multica exec "$JOB_POD" -- printenv | grep -E "cache|CACHE|CARGO|GOMOD"
```

Expected:
- `/caches` exists and is writable.
- Env vars set: `npm_config_cache=/caches/npm`, `PIP_CACHE_DIR=/caches/pip`, `CARGO_HOME=/caches/cargo`, `GOMODCACHE=/caches/go-mod`.

(If the Job has already completed, assign another and try again — Jobs are short-lived.)

- [ ] **Step 3: Run a second npm-heavy task and time it**

The second task to a npm-using repo should be noticeably faster than the first (cached `node_modules` lookups). Hard to measure precisely; subjective speedup is the bar.

- [ ] **Step 4: No commit** — verification only.

---

## Task 12: End-to-end — auto-GC of a closed issue's PVC

**Files:** none.

For an honest test of auto-GC, the simplest approach is to **temporarily lower the grace period** so you can verify the sweep without waiting 24 hours.

- [ ] **Step 1: Temporarily set grace to 60s**

Edit `~/kube/apps/multica/values.yaml`:

```yaml
    gc:
      gracePeriod: 60s
```

`helm upgrade --install`. The controller pod rolls.

- [ ] **Step 2: Identify a closed issue's PVC**

Close one of your test issues in the web UI (mark status `done`). Note the issue's UUID and the PVC name:

```bash
kubectl -n multica get pvc -l app.kubernetes.io/managed-by=multica-k8s-controller
# pick the PVC whose multica.ai/issue-id label matches your closed issue
kubectl -n multica get pvc <pvc-name> -o jsonpath='{.metadata.labels}'; echo
```

- [ ] **Step 3: Wait for the next sweep (up to 30 min)**

```bash
kubectl -n multica logs deploy/multica-controller -f | grep -E "sweep|delete|gc"
```

You can manually trigger a faster sweep by deleting and recreating the controller pod (each start runs a sweep more eagerly only if you also lower the sweep ticker — without code change, you'll wait up to 30 min):

```bash
kubectl -n multica delete pod -l app.kubernetes.io/component=controller
```

Within ~60s of the controller coming back up and running its first sweep, the PVC for the closed issue should be gone.

- [ ] **Step 4: Confirm**

```bash
kubectl -n multica get pvc <pvc-name>
```

Expected: `NotFound`.

- [ ] **Step 5: Reset grace period to a sensible production value**

Edit `~/kube/apps/multica/values.yaml`:

```yaml
    gc:
      gracePeriod: 24h
```

`helm upgrade --install` again.

- [ ] **Step 6: No commit** — verification only.

---

## Task 13: Documentation

**Files:**
- Modify: `packaging/README.md`

- [ ] **Step 1: Add tool cache + GC sections**

Append to `packaging/README.md`:

```markdown
## Tool cache (Plan F)

A single RWX PVC mounted at `/caches` on every Job pod. Pre-organized:

| Path           | Env var          | Used by |
|----------------|------------------|---------|
| /caches/npm    | `npm_config_cache` | npm, pnpm, yarn |
| /caches/pip    | `PIP_CACHE_DIR`  | pip |
| /caches/cargo  | `CARGO_HOME`     | cargo |
| /caches/go-mod | `GOMODCACHE`     | go |

Race-tolerant by design (npm/pip/cargo/go use lockfiles or atomic moves). To
size or disable, see `runtime.controller.toolCache` in values.

## Per-issue PVC auto-GC (Plan F)

The controller sweeps controller-managed PVCs every 30 minutes and asks
Multica for each issue's status via `GET /api/daemon/issues/{id}/gc-check`.
PVCs are deleted when their issue has been `done` or `cancelled` for longer
than `runtime.controller.gc.gracePeriod` (default 24h). Reopened issues lose
deletion candidacy automatically — the PVC is kept and reused.

Issues hard-deleted in Multica return 404 from the gc-check endpoint; their
PVCs are deleted immediately.
```

- [ ] **Step 2: Commit**

```bash
git add packaging/README.md
git commit -m "docs(packaging): tool cache + per-issue PVC auto-GC"
```

---

## Task 14: Final review

**Files:** none.

- [ ] **Step 1: git log + branch state**

```bash
cd /Users/cjs/dev/multica
git status
git log --oneline main..HEAD
```

Expected: clean tree; ~10 commits on this branch.

- [ ] **Step 2: Confirm controller is healthy**

```bash
kubectl -n multica get deploy,pvc -l app.kubernetes.io/managed-by=multica-k8s-controller
kubectl -n multica logs deploy/multica-controller --tail=10
```

Expected: controller Running; tool-cache PVC Bound; logs show normal poll/heartbeat without errors.

- [ ] **Step 3: No commit** — Plan F done.

---

## End state of Plan F

- Worker pods mount a shared `/caches` RWX PVC with npm/pip/cargo/go caches preconfigured. Dependency-heavy tasks reuse downloads across runs.
- Per-issue PVCs auto-delete 24h after their issue closes (configurable). Reopened issues retain their PVC. Hard-deleted issues' PVCs delete immediately.
- The controller's poll + heartbeat + failed-Job sweep loops now have a sibling GC sweep, all driven from the same main loop.

## What's next (Plan G)

The **repo-cache server** (spec §5.5): a small Go binary running `git daemon` over an RWX PVC. Sub-second clones for in-cluster Jobs, single-writer to avoid cross-pod git lockfile races, decoupling from origin during transient GitHub outages. The controller's Job spec adds a `MULTICA_REPO_CACHE_URL` env that the worker can hit via `multica repo checkout` (or directly via `git clone git://multica-repo-cache.multica.svc/…`).

Plan G is ~18 tasks (new Go binary, new image, new chart templates, controller-side integration). Written after Plan F is verified.

Plan H then bundles the bootstrap helpers (interactive bootstrap Job, token-rotator CronJob, tool-cache-gc CronJob) — pure operational polish, mostly chart.
