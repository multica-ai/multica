# Fixed Repo Config Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add the PR1 API/data-model surface for agent fixed repo configuration without changing claim, lock, daemon, or UI behavior.

**Architecture:** Store fixed repo config on `agent`, expose it through existing agent create/update/get/list APIs, and validate that enabled fixed repo config only applies to local runtimes. Keep paths as JSONB string arrays and keep cleanup script persisted but not executed.

**Tech Stack:** Go 1.26, Chi handlers, sqlc/pgx, PostgreSQL migrations, TypeScript core types.

---

## Scope

This plan implements only PR1 from `docs/superpowers/specs/2026-05-24-fixed-repo-mode-design.md`.

Included:

- `agent` table fields for fixed repo config.
- sqlc query/model regeneration.
- Go request/response structs and validation.
- Handler tests for create/update/get/list behavior and validation.
- TypeScript core type additions.

Excluded:

- claim-time path locks.
- daemon fixed workdir execution.
- `multica repo checkout` fixed-mode rejection.
- UI settings.
- cleanup script execution.

## File Structure

- Create `server/migrations/108_agent_fixed_repo_config.up.sql`: add `agent` columns and constraints.
- Create `server/migrations/108_agent_fixed_repo_config.down.sql`: drop added constraints and columns.
- Modify `server/pkg/db/queries/agent.sql`: include new fields in `CreateAgent`, `UpdateAgent`, and add cleanup-script clear query.
- Regenerate `server/pkg/db/generated/models.go` and `server/pkg/db/generated/agent.sql.go` with `make sqlc`.
- Modify `server/internal/handler/agent.go`: add wire fields, parse JSONB path arrays, validate local-runtime fixed repo config, persist create/update fields, clear cleanup script on JSON null.
- Modify `server/internal/handler/agent_test.go`: add API tests.
- Modify `packages/core/types/agent.ts`: add optional fixed repo fields and request payload fields.

## Task 1: Failing Handler Tests

**Files:**
- Modify: `server/internal/handler/agent_test.go`

- [ ] **Step 1: Add tests that describe PR1 behavior**

Append these tests and helpers near the other `CreateAgent` / `GetAgent` tests:

```go
func createHandlerTestLocalRuntime(t *testing.T, name string) string {
	t.Helper()
	ctx := context.Background()

	var runtimeID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status,
			device_info, metadata, owner_id, last_seen_at
		)
		VALUES ($1, $2, $3, 'local', 'codex', 'online', $4, '{}'::jsonb, $5, now())
		RETURNING id
	`, testWorkspaceID, "fixed-repo-test-daemon-"+name, name, "fixed repo test runtime", testUserID).Scan(&runtimeID); err != nil {
		t.Fatalf("failed to create local runtime: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
	})
	return runtimeID
}

func TestCreateAgent_FixedRepoConfig_LocalRuntimePersistsAndReturns(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	runtimeID := createHandlerTestLocalRuntime(t, "fixed-repo-create-runtime")

	body := map[string]any{
		"name":                      "fixed-repo-create-agent",
		"description":               "fixed repo create",
		"runtime_id":                runtimeID,
		"visibility":                "private",
		"max_concurrent_tasks":      2,
		"fixed_repo_enabled":        true,
		"fixed_repo_paths":          []string{"/work/game-main", "/work/game-assets"},
		"fixed_repo_vcs_type":       "perforce",
		"fixed_repo_cleanup_script": "/work/game-main/scripts/cleanup.sh",
	}

	w := httptest.NewRecorder()
	testHandler.CreateAgent(w, newRequest(http.MethodPost, "/api/agents", body))
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateAgent: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp AgentResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, resp.ID)
	})
	if !resp.FixedRepoEnabled {
		t.Fatal("expected fixed_repo_enabled=true")
	}
	if got, want := resp.FixedRepoVcsType, "perforce"; got != want {
		t.Fatalf("fixed_repo_vcs_type = %q, want %q", got, want)
	}
	if resp.FixedRepoCleanupScript == nil || *resp.FixedRepoCleanupScript != "/work/game-main/scripts/cleanup.sh" {
		t.Fatalf("fixed_repo_cleanup_script = %#v", resp.FixedRepoCleanupScript)
	}
	if len(resp.FixedRepoPaths) != 2 || resp.FixedRepoPaths[0] != "/work/game-main" || resp.FixedRepoPaths[1] != "/work/game-assets" {
		t.Fatalf("fixed_repo_paths = %#v", resp.FixedRepoPaths)
	}
}

func TestCreateAgent_FixedRepoConfig_RejectsCloudRuntime(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	body := map[string]any{
		"name":                 "fixed-repo-cloud-agent",
		"runtime_id":           testRuntimeID,
		"fixed_repo_enabled":   true,
		"fixed_repo_paths":     []string{"/work/repo"},
		"fixed_repo_vcs_type":  "git",
		"max_concurrent_tasks": 1,
	}

	w := httptest.NewRecorder()
	testHandler.CreateAgent(w, newRequest(http.MethodPost, "/api/agents", body))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("CreateAgent with cloud runtime: expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateAgent_FixedRepoConfig_CanSetAndClearCleanupScript(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	runtimeID := createHandlerTestLocalRuntime(t, "fixed-repo-update-runtime")

	createBody := map[string]any{
		"name":                 "fixed-repo-update-agent",
		"runtime_id":           runtimeID,
		"visibility":           "private",
		"max_concurrent_tasks": 1,
	}
	createW := httptest.NewRecorder()
	testHandler.CreateAgent(createW, newRequest(http.MethodPost, "/api/agents", createBody))
	if createW.Code != http.StatusCreated {
		t.Fatalf("CreateAgent: expected 201, got %d: %s", createW.Code, createW.Body.String())
	}
	var created AgentResponse
	if err := json.NewDecoder(createW.Body).Decode(&created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, created.ID)
	})

	updateBody := map[string]any{
		"fixed_repo_enabled":        true,
		"fixed_repo_paths":          []string{"/fixed/main"},
		"fixed_repo_vcs_type":       "git",
		"fixed_repo_cleanup_script": "/fixed/main/cleanup.sh",
	}
	updateReq := withURLParam(newRequest(http.MethodPut, "/api/agents/"+created.ID, updateBody), "id", created.ID)
	updateW := httptest.NewRecorder()
	testHandler.UpdateAgent(updateW, updateReq)
	if updateW.Code != http.StatusOK {
		t.Fatalf("UpdateAgent set fixed repo: expected 200, got %d: %s", updateW.Code, updateW.Body.String())
	}
	var updated AgentResponse
	if err := json.NewDecoder(updateW.Body).Decode(&updated); err != nil {
		t.Fatalf("decode update response: %v", err)
	}
	if !updated.FixedRepoEnabled || len(updated.FixedRepoPaths) != 1 || updated.FixedRepoPaths[0] != "/fixed/main" {
		t.Fatalf("unexpected fixed repo update response: %+v", updated)
	}
	if updated.FixedRepoCleanupScript == nil || *updated.FixedRepoCleanupScript != "/fixed/main/cleanup.sh" {
		t.Fatalf("fixed_repo_cleanup_script after set = %#v", updated.FixedRepoCleanupScript)
	}

	clearBody := map[string]any{"fixed_repo_cleanup_script": nil}
	clearReq := withURLParam(newRequest(http.MethodPut, "/api/agents/"+created.ID, clearBody), "id", created.ID)
	clearW := httptest.NewRecorder()
	testHandler.UpdateAgent(clearW, clearReq)
	if clearW.Code != http.StatusOK {
		t.Fatalf("UpdateAgent clear cleanup script: expected 200, got %d: %s", clearW.Code, clearW.Body.String())
	}
	var cleared AgentResponse
	if err := json.NewDecoder(clearW.Body).Decode(&cleared); err != nil {
		t.Fatalf("decode clear response: %v", err)
	}
	if cleared.FixedRepoCleanupScript != nil {
		t.Fatalf("expected cleanup script cleared, got %#v", cleared.FixedRepoCleanupScript)
	}
}

func TestUpdateAgent_FixedRepoConfig_RejectsEmptyEnabledPaths(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	runtimeID := createHandlerTestLocalRuntime(t, "fixed-repo-empty-paths-runtime")

	createBody := map[string]any{
		"name":                 "fixed-repo-empty-paths-agent",
		"runtime_id":           runtimeID,
		"visibility":           "private",
		"max_concurrent_tasks": 1,
	}
	createW := httptest.NewRecorder()
	testHandler.CreateAgent(createW, newRequest(http.MethodPost, "/api/agents", createBody))
	if createW.Code != http.StatusCreated {
		t.Fatalf("CreateAgent: expected 201, got %d: %s", createW.Code, createW.Body.String())
	}
	var created AgentResponse
	if err := json.NewDecoder(createW.Body).Decode(&created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, created.ID)
	})

	updateBody := map[string]any{
		"fixed_repo_enabled": true,
		"fixed_repo_paths":   []string{},
	}
	updateReq := withURLParam(newRequest(http.MethodPut, "/api/agents/"+created.ID, updateBody), "id", created.ID)
	w := httptest.NewRecorder()
	testHandler.UpdateAgent(w, updateReq)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("UpdateAgent empty paths: expected 400, got %d: %s", w.Code, w.Body.String())
	}
}
```

- [ ] **Step 2: Run tests and verify RED**

Run:

```bash
cd server
go test ./internal/handler -run 'TestCreateAgent_FixedRepoConfig|TestUpdateAgent_FixedRepoConfig' -count=1
```

Expected: compile failure mentioning missing `AgentResponse.FixedRepoEnabled`, `AgentResponse.FixedRepoPaths`, `AgentResponse.FixedRepoVcsType`, or `AgentResponse.FixedRepoCleanupScript`.

## Task 2: Agent Schema And sqlc Surface

**Files:**
- Create: `server/migrations/108_agent_fixed_repo_config.up.sql`
- Create: `server/migrations/108_agent_fixed_repo_config.down.sql`
- Modify: `server/pkg/db/queries/agent.sql`
- Generated: `server/pkg/db/generated/models.go`
- Generated: `server/pkg/db/generated/agent.sql.go`

- [ ] **Step 1: Add migration**

`server/migrations/108_agent_fixed_repo_config.up.sql`:

```sql
ALTER TABLE agent
    ADD COLUMN fixed_repo_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN fixed_repo_paths JSONB NOT NULL DEFAULT '[]',
    ADD COLUMN fixed_repo_vcs_type TEXT NOT NULL DEFAULT 'git',
    ADD COLUMN fixed_repo_cleanup_script TEXT;

ALTER TABLE agent
    ADD CONSTRAINT agent_fixed_repo_paths_array_check
        CHECK (jsonb_typeof(fixed_repo_paths) = 'array'),
    ADD CONSTRAINT agent_fixed_repo_vcs_type_check
        CHECK (fixed_repo_vcs_type IN ('git', 'perforce', 'none', 'custom'));
```

`server/migrations/108_agent_fixed_repo_config.down.sql`:

```sql
ALTER TABLE agent DROP CONSTRAINT IF EXISTS agent_fixed_repo_vcs_type_check;
ALTER TABLE agent DROP CONSTRAINT IF EXISTS agent_fixed_repo_paths_array_check;

ALTER TABLE agent
    DROP COLUMN IF EXISTS fixed_repo_cleanup_script,
    DROP COLUMN IF EXISTS fixed_repo_vcs_type,
    DROP COLUMN IF EXISTS fixed_repo_paths,
    DROP COLUMN IF EXISTS fixed_repo_enabled;
```

- [ ] **Step 2: Update sqlc queries**

In `server/pkg/db/queries/agent.sql`, update `CreateAgent` column list to include:

```sql
fixed_repo_enabled, fixed_repo_paths, fixed_repo_vcs_type, fixed_repo_cleanup_script
```

and add matching values:

```sql
$17, $18, $19, $20
```

In `UpdateAgent`, add:

```sql
fixed_repo_enabled = COALESCE(sqlc.narg('fixed_repo_enabled'), fixed_repo_enabled),
fixed_repo_paths = COALESCE(sqlc.narg('fixed_repo_paths'), fixed_repo_paths),
fixed_repo_vcs_type = COALESCE(sqlc.narg('fixed_repo_vcs_type'), fixed_repo_vcs_type),
fixed_repo_cleanup_script = COALESCE(sqlc.narg('fixed_repo_cleanup_script'), fixed_repo_cleanup_script),
```

before `updated_at = now()`.

Add this query after `ClearAgentMcpConfig`:

```sql
-- name: ClearAgentFixedRepoCleanupScript :one
UPDATE agent SET fixed_repo_cleanup_script = NULL, updated_at = now()
WHERE id = $1
RETURNING *;
```

- [ ] **Step 3: Regenerate sqlc**

Run:

```bash
make sqlc
```

Expected: generated `db.Agent` has:

```go
FixedRepoEnabled       bool        `json:"fixed_repo_enabled"`
FixedRepoPaths         []byte      `json:"fixed_repo_paths"`
FixedRepoVcsType       string      `json:"fixed_repo_vcs_type"`
FixedRepoCleanupScript pgtype.Text `json:"fixed_repo_cleanup_script"`
```

- [ ] **Step 4: Run tests and verify RED changed**

Run:

```bash
cd server
go test ./internal/handler -run 'TestCreateAgent_FixedRepoConfig|TestUpdateAgent_FixedRepoConfig' -count=1
```

Expected: compile failure now moves to handler fields/methods, or runtime failure because handler does not yet populate fixed repo fields.

## Task 3: Handler Request/Response And Validation

**Files:**
- Modify: `server/internal/handler/agent.go`
- Test: `server/internal/handler/agent_test.go`

- [ ] **Step 1: Add wire fields and helpers**

In `server/internal/handler/agent.go`, add constants and helpers near `maxAgentDescriptionLength`:

```go
const (
	maxFixedRepoPaths              = 16
	maxFixedRepoPathLength         = 4096
	maxFixedRepoCleanupScriptBytes = 4096
	defaultFixedRepoVcsType        = "git"
)

func isKnownFixedRepoVcsType(v string) bool {
	switch v {
	case "git", "perforce", "none", "custom":
		return true
	default:
		return false
	}
}
```

Add helper functions near the other agent helpers:

```go
func decodeStringSliceJSON(raw []byte) []string {
	if len(raw) == 0 {
		return []string{}
	}
	var out []string
	if err := json.Unmarshal(raw, &out); err != nil {
		slog.Warn("failed to unmarshal fixed_repo_paths", "error", err)
		return []string{}
	}
	if out == nil {
		return []string{}
	}
	return out
}

func encodeStringSliceJSON(values []string) []byte {
	data, err := json.Marshal(values)
	if err != nil {
		return []byte("[]")
	}
	return data
}

func normalizeFixedRepoPaths(paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	return out
}

func validateFixedRepoConfig(enabled bool, paths []string, vcsType string, cleanupScript *string, runtimeMode string) error {
	if vcsType == "" {
		vcsType = defaultFixedRepoVcsType
	}
	if !isKnownFixedRepoVcsType(vcsType) {
		return fmt.Errorf("fixed_repo_vcs_type %q is not supported", vcsType)
	}
	if len(paths) > maxFixedRepoPaths {
		return fmt.Errorf("fixed_repo_paths must contain %d paths or fewer", maxFixedRepoPaths)
	}
	for _, p := range paths {
		if utf8.RuneCountInString(p) > maxFixedRepoPathLength {
			return fmt.Errorf("fixed_repo_paths entries must be %d characters or fewer", maxFixedRepoPathLength)
		}
	}
	if cleanupScript != nil && utf8.RuneCountInString(*cleanupScript) > maxFixedRepoCleanupScriptBytes {
		return fmt.Errorf("fixed_repo_cleanup_script must be %d characters or fewer", maxFixedRepoCleanupScriptBytes)
	}
	if !enabled {
		return nil
	}
	if runtimeMode != "local" {
		return fmt.Errorf("fixed_repo_enabled requires a local runtime")
	}
	if len(paths) == 0 {
		return fmt.Errorf("fixed_repo_paths is required when fixed_repo_enabled is true")
	}
	return nil
}
```

Ensure `strings` is imported.

- [ ] **Step 2: Add response fields**

In `AgentResponse` add:

```go
FixedRepoEnabled       bool     `json:"fixed_repo_enabled"`
FixedRepoPaths         []string `json:"fixed_repo_paths"`
FixedRepoVcsType       string   `json:"fixed_repo_vcs_type"`
FixedRepoCleanupScript *string  `json:"fixed_repo_cleanup_script"`
```

In `agentToResponse`, set:

```go
fixedRepoVcsType := a.FixedRepoVcsType
if fixedRepoVcsType == "" {
	fixedRepoVcsType = defaultFixedRepoVcsType
}

FixedRepoEnabled:       a.FixedRepoEnabled,
FixedRepoPaths:         decodeStringSliceJSON(a.FixedRepoPaths),
FixedRepoVcsType:       fixedRepoVcsType,
FixedRepoCleanupScript: textToPtr(a.FixedRepoCleanupScript),
```

- [ ] **Step 3: Add request fields and create handling**

Add to `CreateAgentRequest`:

```go
FixedRepoEnabled       bool     `json:"fixed_repo_enabled"`
FixedRepoPaths         []string `json:"fixed_repo_paths"`
FixedRepoVcsType       string   `json:"fixed_repo_vcs_type"`
FixedRepoCleanupScript *string  `json:"fixed_repo_cleanup_script"`
```

In `CreateAgent`, after runtime load and thinking validation, normalize and validate:

```go
fixedRepoPaths := normalizeFixedRepoPaths(req.FixedRepoPaths)
fixedRepoVcsType := req.FixedRepoVcsType
if fixedRepoVcsType == "" {
	fixedRepoVcsType = defaultFixedRepoVcsType
}
if err := validateFixedRepoConfig(req.FixedRepoEnabled, fixedRepoPaths, fixedRepoVcsType, req.FixedRepoCleanupScript, runtime.RuntimeMode); err != nil {
	writeError(w, http.StatusBadRequest, err.Error())
	return
}
```

Set `CreateAgentParams`:

```go
FixedRepoEnabled:       req.FixedRepoEnabled,
FixedRepoPaths:         encodeStringSliceJSON(fixedRepoPaths),
FixedRepoVcsType:       fixedRepoVcsType,
FixedRepoCleanupScript: ptrToText(req.FixedRepoCleanupScript),
```

- [ ] **Step 4: Add update handling**

Add to `UpdateAgentRequest`:

```go
FixedRepoEnabled       *bool     `json:"fixed_repo_enabled"`
FixedRepoPaths         *[]string `json:"fixed_repo_paths"`
FixedRepoVcsType       *string   `json:"fixed_repo_vcs_type"`
FixedRepoCleanupScript *string   `json:"fixed_repo_cleanup_script"`
```

In `UpdateAgent`, maintain target runtime mode:

```go
targetRuntimeMode := existing.RuntimeMode
```

When `req.RuntimeID != nil`, after loading `runtime`, set:

```go
targetRuntimeMode = runtime.RuntimeMode
```

Before `h.Queries.UpdateAgent`, compute the effective fixed repo config:

```go
nextFixedRepoEnabled := existing.FixedRepoEnabled
if req.FixedRepoEnabled != nil {
	nextFixedRepoEnabled = *req.FixedRepoEnabled
	params.FixedRepoEnabled = pgtype.Bool{Bool: *req.FixedRepoEnabled, Valid: true}
}

nextFixedRepoPaths := decodeStringSliceJSON(existing.FixedRepoPaths)
if req.FixedRepoPaths != nil {
	nextFixedRepoPaths = normalizeFixedRepoPaths(*req.FixedRepoPaths)
	params.FixedRepoPaths = encodeStringSliceJSON(nextFixedRepoPaths)
}

nextFixedRepoVcsType := existing.FixedRepoVcsType
if nextFixedRepoVcsType == "" {
	nextFixedRepoVcsType = defaultFixedRepoVcsType
}
if req.FixedRepoVcsType != nil {
	nextFixedRepoVcsType = *req.FixedRepoVcsType
	if nextFixedRepoVcsType == "" {
		nextFixedRepoVcsType = defaultFixedRepoVcsType
	}
	params.FixedRepoVcsType = pgtype.Text{String: nextFixedRepoVcsType, Valid: true}
}

nextCleanupScript := textToPtr(existing.FixedRepoCleanupScript)
rawCleanupScript, hasCleanupScript := rawFields["fixed_repo_cleanup_script"]
shouldClearFixedRepoCleanupScript := hasCleanupScript && bytes.Equal(bytes.TrimSpace(rawCleanupScript), []byte("null"))
if shouldClearFixedRepoCleanupScript {
	nextCleanupScript = nil
} else if req.FixedRepoCleanupScript != nil {
	nextCleanupScript = req.FixedRepoCleanupScript
	params.FixedRepoCleanupScript = pgtype.Text{String: *req.FixedRepoCleanupScript, Valid: true}
}

if err := validateFixedRepoConfig(nextFixedRepoEnabled, nextFixedRepoPaths, nextFixedRepoVcsType, nextCleanupScript, targetRuntimeMode); err != nil {
	writeError(w, http.StatusBadRequest, err.Error())
	return
}
```

After the existing `ClearAgentMcpConfig` and `ClearAgentThinkingLevel` blocks, add:

```go
if shouldClearFixedRepoCleanupScript {
	updated, err = h.Queries.ClearAgentFixedRepoCleanupScript(r.Context(), updated.ID)
	if err != nil {
		slog.Warn("clear agent fixed_repo_cleanup_script failed", append(logger.RequestAttrs(r), "error", err, "agent_id", id)...)
		writeError(w, http.StatusInternalServerError, "failed to clear fixed_repo_cleanup_script: "+err.Error())
		return
	}
}
```

- [ ] **Step 5: Run focused Go tests and verify GREEN**

Run:

```bash
cd server
go test ./internal/handler -run 'TestCreateAgent_FixedRepoConfig|TestUpdateAgent_FixedRepoConfig' -count=1
```

Expected: PASS.

## Task 4: TypeScript Core Types

**Files:**
- Modify: `packages/core/types/agent.ts`

- [ ] **Step 1: Add TS type fields**

Add near `AgentRuntimeMode`:

```ts
export type FixedRepoVcsType = "git" | "perforce" | "none" | "custom";
```

Add optional fields to `Agent`:

```ts
  /** Fixed local worktree pool for daemon-executed tasks. Omitted by older backends. */
  fixed_repo_enabled?: boolean;
  /** Local paths on the daemon host. Omitted by older backends. */
  fixed_repo_paths?: string[];
  /** VCS hint for fixed repo tasks. Defaults to "git" when omitted. */
  fixed_repo_vcs_type?: FixedRepoVcsType;
  /** Persisted for future cleanup support; not executed by v1 daemon. */
  fixed_repo_cleanup_script?: string | null;
```

Add optional fields to `CreateAgentRequest`:

```ts
  fixed_repo_enabled?: boolean;
  fixed_repo_paths?: string[];
  fixed_repo_vcs_type?: FixedRepoVcsType;
  fixed_repo_cleanup_script?: string | null;
```

Add optional fields to `UpdateAgentRequest`:

```ts
  fixed_repo_enabled?: boolean;
  fixed_repo_paths?: string[];
  fixed_repo_vcs_type?: FixedRepoVcsType;
  fixed_repo_cleanup_script?: string | null;
```

- [ ] **Step 2: Run typecheck**

Run:

```bash
pnpm typecheck
```

Expected: PASS.

## Task 5: PR1 Verification And Commit

**Files:**
- All PR1 files above.

- [ ] **Step 1: Run backend focused verification**

Run:

```bash
cd server
go test ./internal/handler -run 'TestCreateAgent_FixedRepoConfig|TestUpdateAgent_FixedRepoConfig' -count=1
```

Expected: PASS.

- [ ] **Step 2: Run compile-only server check**

Run:

```bash
cd server
go test ./internal/service ./internal/handler -run '^$' -count=1
```

Expected: PASS or report any pre-existing database fixture failure separately if the local DB is not migrated.

- [ ] **Step 3: Run TS typecheck**

Run:

```bash
pnpm typecheck
```

Expected: PASS.

- [ ] **Step 4: Inspect diff**

Run:

```bash
git diff --stat
git diff --check
```

Expected: no whitespace errors; diff only touches PR1 files.

- [ ] **Step 5: Commit PR1**

Run:

```bash
git add server/migrations/108_agent_fixed_repo_config.up.sql \
  server/migrations/108_agent_fixed_repo_config.down.sql \
  server/pkg/db/queries/agent.sql \
  server/pkg/db/generated/models.go \
  server/pkg/db/generated/agent.sql.go \
  server/internal/handler/agent.go \
  server/internal/handler/agent_test.go \
  packages/core/types/agent.ts
git commit -m "feat(agents): add fixed repo config"
```

Expected: commit succeeds. This branch is then ready for PR1.

## Self-Review

- Spec coverage: this plan covers PR1 config model/API/types and intentionally excludes lock, daemon, CLI, UI, and cleanup execution.
- Placeholder scan: no open placeholders or unspecified test expectations.
- Type consistency: Go/JSON/TS field names consistently use `fixed_repo_enabled`, `fixed_repo_paths`, `fixed_repo_vcs_type`, and `fixed_repo_cleanup_script`.
