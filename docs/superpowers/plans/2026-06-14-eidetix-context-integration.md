# Eidetix Shared-Context Integration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Wire the partner-hosted Eidetix knowledge-graph MCP into Multica's agent runtime so that agents working a project's issues share one read/write memory — server-side, with no daemon or CLI release.

**Architecture:** A per-project config row (`eidetix_project_config`) holds an encrypted Bearer token and endpoint. The task-claim handler, after resolving the issue's project, decrypts the token and merges an `eidetix` MCP server entry into the claim-response copy of `mcp_config` (the single chokepoint that already reaches every provider backend), then appends a conditionally-shipped `multica-eidetix` loop skill. Everything is fail-open: Eidetix never blocks or fails a task. Config surface is CLI-only, gated to workspace owner/admin, audited.

**Tech Stack:** Go (Chi router, sqlc/pgx, cobra CLI, `embed.FS` skills, AES-256-GCM via `internal/util/secretbox`), PostgreSQL.

**Design spec:** `docs/superpowers/specs/2026-06-13-eidetix-context-design.md` (read it before starting; this plan implements components C1–C5 + the provider-verification and doc-upkeep tasks).

**Secrets warning:** The two real Eidetix tokens (Marketing / Support) are secrets. They must NEVER be written into source, tests, fixtures, the spec, the plan, git, or any log line. Tests use throwaway fake tokens. The only place a real token lives is the encrypted DB column, set by the operator via `--token-stdin`.

---

## File Structure

**New files:**
- `server/migrations/120_eidetix_project_config.up.sql` / `.down.sql` — the config table.
- `server/pkg/db/queries/eidetix.sql` — sqlc queries (get / upsert / delete / set-enabled).
- `server/internal/handler/eidetix_merge.go` — pure MCP-merge logic (no DB, no HTTP).
- `server/internal/handler/eidetix_merge_test.go` — unit tests for the merge logic.
- `server/internal/handler/eidetix.go` — REST handlers (set/show/clear/enable-disable) + the claim-time apply method.
- `server/internal/handler/eidetix_test.go` — handler + claim-integration tests (DB-backed).
- `server/cmd/multica/cmd_project_eidetix.go` — `multica project eidetix` subcommands.
- `server/cmd/multica/cmd_project_eidetix_test.go` — CLI flag-resolution unit tests.
- `server/internal/service/eidetix_skill.go` — `EidetixLoopSkill()` accessor + its own embed.
- `server/internal/service/eidetix_skill_test.go` — conformance test for the loop skill.
- `server/internal/service/eidetix_skill/multica-eidetix/SKILL.md` — the loop skill body.
- `server/internal/service/eidetix_skill/multica-eidetix/references/eidetix-tools-source-map.md` — source-traced tool contract.

**Modified files:**
- `server/internal/handler/handler.go` — add `EidetixSecrets *secretbox.Box` field to `Handler`.
- `server/cmd/server/router.go` — load `MULTICA_EIDETIX_SECRET_KEY`; register `/api/projects/{id}/eidetix` routes.
- `server/internal/handler/daemon.go` — call `applyEidetixToClaim` after project resolution (~`:1224`).
- `server/cmd/multica/main.go` — register the new subcommand group (via `projectCmd.AddCommand`, in `cmd_project_eidetix.go`'s `init()`).
- `server/internal/service/builtin_skills/multica-projects-and-resources/references/projects-and-resources-source-map.md` — one-line pointer to the new admin command (doc-upkeep rule).

---

## Decisions locked for this plan (read once)

1. **Dedicated encryption key `MULTICA_EIDETIX_SECRET_KEY`** (base64 32-byte, same `secretbox` helper Lark uses). A dedicated key keeps Eidetix independent of whether Lark is configured and gives it its own blast radius. If the key is unset, `h.EidetixSecrets == nil` and the claim handler fails open (no Eidetix) — exactly like Lark's 503-when-unset posture.
2. **Routes:** `GET` (show), `PUT` (set/upsert, requires token), `DELETE` (clear), `PATCH` (toggle `enabled`) under `/api/projects/{id}/eidetix`.
3. **The loop skill lives OUTSIDE `builtin_skills/`** (which ships to every agent) in its own `eidetix_skill/` embed, exposed via `TaskService.EidetixLoopSkill()`, appended only in the enabled branch of the claim handler.
4. **Append the skill whenever the project's Eidetix config is enabled and the token decrypts** — even if the managed server was not merged because a user-defined `eidetix` server already existed (an `eidetix` server is present either way, so the loop guidance is valid).
5. **The `multica project eidetix` command is admin-facing, not agent-facing.** It is NOT added to the agent-runtime body of `multica-projects-and-resources`; only a one-line source-map pointer is added, satisfying the doc-upkeep rule without polluting every agent's context with a config command agents never run.

---

### Task 1: Data model — `eidetix_project_config` table + sqlc queries

**Files:**
- Create: `server/migrations/120_eidetix_project_config.up.sql`
- Create: `server/migrations/120_eidetix_project_config.down.sql`
- Create: `server/pkg/db/queries/eidetix.sql`
- Generated (do not hand-edit): `server/pkg/db/generated/eidetix.sql.go`, `server/pkg/db/generated/models.go`

- [ ] **Step 1: Write the up migration**

Create `server/migrations/120_eidetix_project_config.up.sql`:

```sql
-- Per-project binding to a partner Eidetix knowledge graph. One row per
-- project. The token selects the graph on Eidetix's side; we store it
-- application-encrypted (secretbox/AES-256-GCM), so a DB dump leaks ciphertext
-- only. enabled is a soft switch so an operator can pause Eidetix for a project
-- without losing the token.
CREATE TABLE eidetix_project_config (
    project_id       UUID PRIMARY KEY REFERENCES project(id) ON DELETE CASCADE,
    enabled          BOOLEAN NOT NULL DEFAULT true,
    endpoint_url     TEXT NOT NULL DEFAULT 'https://eidetix.nodeops.xyz/mcp/sse',
    -- Ciphertext of the Eidetix Bearer token. Application-layer secretbox.
    -- DB never sees plaintext.
    token_encrypted  BYTEA NOT NULL,
    -- Human label only ("Marketing" / "Support"). NEVER the token.
    graph_label      TEXT,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

> NOTE: Confirm the projects table name. The project loader uses `GetProjectInWorkspace` and the resource migrations reference the projects table — verify whether it is `project` or `projects` with: `grep -rn "REFERENCES project" server/migrations/ | head`. Use whichever the existing FKs use. The codebase model type is `db.Project` and the table in `project_resource` migrations is the authority; match it exactly here.

- [ ] **Step 2: Write the down migration**

Create `server/migrations/120_eidetix_project_config.down.sql`:

```sql
DROP TABLE IF EXISTS eidetix_project_config;
```

- [ ] **Step 3: Write the sqlc queries**

Create `server/pkg/db/queries/eidetix.sql`:

```sql
-- name: GetEidetixConfigForProject :one
SELECT * FROM eidetix_project_config WHERE project_id = $1;

-- name: UpsertEidetixProjectConfig :one
INSERT INTO eidetix_project_config (
    project_id, enabled, endpoint_url, token_encrypted, graph_label
) VALUES (
    $1, sqlc.arg('enabled'), sqlc.arg('endpoint_url'), sqlc.arg('token_encrypted'), sqlc.narg('graph_label')
)
ON CONFLICT (project_id) DO UPDATE SET
    enabled         = EXCLUDED.enabled,
    endpoint_url    = EXCLUDED.endpoint_url,
    token_encrypted = EXCLUDED.token_encrypted,
    graph_label     = EXCLUDED.graph_label,
    updated_at      = now()
RETURNING *;

-- name: SetEidetixProjectEnabled :one
UPDATE eidetix_project_config
SET enabled = $2, updated_at = now()
WHERE project_id = $1
RETURNING *;

-- name: DeleteEidetixProjectConfig :exec
DELETE FROM eidetix_project_config WHERE project_id = $1;
```

- [ ] **Step 4: Regenerate sqlc and run the migration**

Run:
```bash
make sqlc && make migrate-up
```
Expected: `sqlc generate` writes `server/pkg/db/generated/eidetix.sql.go` and an `EidetixProjectConfig` struct in `models.go`; `migrate-up` applies `120_*`. No errors.

- [ ] **Step 5: Verify the generated types and that the backend compiles**

Run:
```bash
cd server && grep -n "type EidetixProjectConfig struct" pkg/db/generated/models.go && grep -n "func (q \*Queries) GetEidetixConfigForProject" pkg/db/generated/eidetix.sql.go && go build ./...
```
Expected: the struct exists with fields `ProjectID pgtype.UUID`, `Enabled bool`, `EndpointUrl string`, `TokenEncrypted []byte`, `GraphLabel pgtype.Text`, `CreatedAt`/`UpdatedAt pgtype.Timestamptz`; the four query methods exist; `go build` succeeds.

- [ ] **Step 6: Commit**

```bash
git add server/migrations/120_eidetix_project_config.up.sql server/migrations/120_eidetix_project_config.down.sql server/pkg/db/queries/eidetix.sql server/pkg/db/generated/
git commit -m "feat(eidetix): add eidetix_project_config table and queries"
```

---

### Task 2: Pure MCP-merge logic (no DB, no HTTP)

This is the testable core of C4. It builds the `eidetix` server entry and merges it into a Claude-style `mcp_config` without clobbering a user-defined server of the same name.

**Files:**
- Create: `server/internal/handler/eidetix_merge.go`
- Test: `server/internal/handler/eidetix_merge_test.go`

- [ ] **Step 1: Write the failing test**

Create `server/internal/handler/eidetix_merge_test.go`:

```go
package handler

import (
	"encoding/json"
	"testing"
)

func TestMergeEidetixServer_EmptyConfig(t *testing.T) {
	merged, added, err := mergeEidetixServer(nil, "https://eidetix.example/mcp/sse", "tok-abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !added {
		t.Fatalf("expected added=true on empty config")
	}

	var got struct {
		McpServers map[string]struct {
			URL       string            `json:"url"`
			Transport string            `json:"transport"`
			Headers   map[string]string `json:"headers"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(merged, &got); err != nil {
		t.Fatalf("merged is not valid JSON: %v", err)
	}
	e, ok := got.McpServers["eidetix"]
	if !ok {
		t.Fatalf("eidetix server not present, got %s", merged)
	}
	if e.URL != "https://eidetix.example/mcp/sse" {
		t.Errorf("url = %q, want the endpoint", e.URL)
	}
	if e.Transport != "streamable-http" {
		t.Errorf("transport = %q, want streamable-http", e.Transport)
	}
	if e.Headers["Authorization"] != "Bearer tok-abc" {
		t.Errorf("Authorization = %q, want Bearer tok-abc", e.Headers["Authorization"])
	}
}

func TestMergeEidetixServer_PreservesExistingServers(t *testing.T) {
	existing := json.RawMessage(`{"mcpServers":{"github":{"command":"gh-mcp"}}}`)
	merged, added, err := mergeEidetixServer(existing, "https://e/mcp/sse", "tok")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !added {
		t.Fatalf("expected added=true")
	}
	var got struct {
		McpServers map[string]json.RawMessage `json:"mcpServers"`
	}
	if err := json.Unmarshal(merged, &got); err != nil {
		t.Fatalf("merged not valid JSON: %v", err)
	}
	if _, ok := got.McpServers["github"]; !ok {
		t.Errorf("existing github server was dropped: %s", merged)
	}
	if _, ok := got.McpServers["eidetix"]; !ok {
		t.Errorf("eidetix server not added: %s", merged)
	}
}

func TestMergeEidetixServer_DoesNotClobberUserDefined(t *testing.T) {
	existing := json.RawMessage(`{"mcpServers":{"eidetix":{"url":"https://user/sse","transport":"streamable-http"}}}`)
	merged, added, err := mergeEidetixServer(existing, "https://managed/mcp/sse", "managed-tok")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if added {
		t.Fatalf("expected added=false when a user-defined eidetix server exists")
	}
	// The returned config must be byte-identical to the input (no mutation).
	if string(merged) != string(existing) {
		t.Errorf("user config was mutated:\n got %s\nwant %s", merged, existing)
	}
}

func TestMergeEidetixServer_MalformedExistingReturnsError(t *testing.T) {
	_, _, err := mergeEidetixServer(json.RawMessage(`{not json`), "https://e/sse", "tok")
	if err == nil {
		t.Fatalf("expected an error on malformed existing config")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd server && go test ./internal/handler/ -run TestMergeEidetixServer`
Expected: FAIL — `undefined: mergeEidetixServer`.

- [ ] **Step 3: Write the implementation**

Create `server/internal/handler/eidetix_merge.go`:

```go
package handler

import "encoding/json"

// eidetixServerName is the reserved key for the managed Eidetix MCP server in
// an agent's mcp_config. A user-defined server with this exact name is left
// untouched (the operator is presumed to know what they are doing).
const eidetixServerName = "eidetix"

// buildEidetixServerEntry returns the Claude-style MCP server entry for the
// remote Eidetix SSE endpoint. transport "streamable-http" + a url makes it a
// remote HTTP/SSE server (as opposed to a stdio `command` server).
func buildEidetixServerEntry(endpointURL, token string) map[string]any {
	return map[string]any{
		"url":       endpointURL,
		"transport": "streamable-http",
		"headers": map[string]any{
			"Authorization": "Bearer " + token,
		},
	}
}

// mergeEidetixServer merges the managed eidetix server into an existing
// Claude-style mcp_config (`{"mcpServers": {...}}`). It returns the merged
// config and whether the server was added.
//
//   - existing == nil/empty  → a fresh {"mcpServers":{"eidetix":...}}
//   - existing has no eidetix → eidetix added, all other servers preserved
//   - existing has an eidetix → NOT clobbered; returns existing unchanged, added=false
//   - existing is malformed   → error (caller fails open and proceeds unchanged)
func mergeEidetixServer(existing json.RawMessage, endpointURL, token string) (json.RawMessage, bool, error) {
	root := map[string]json.RawMessage{}
	if len(existing) > 0 {
		if err := json.Unmarshal(existing, &root); err != nil {
			return nil, false, err
		}
	}

	servers := map[string]json.RawMessage{}
	if raw, ok := root["mcpServers"]; ok && len(raw) > 0 {
		if err := json.Unmarshal(raw, &servers); err != nil {
			return nil, false, err
		}
	}

	if _, exists := servers[eidetixServerName]; exists {
		// User-defined server of the same name — do not clobber.
		return existing, false, nil
	}

	entryBytes, err := json.Marshal(buildEidetixServerEntry(endpointURL, token))
	if err != nil {
		return nil, false, err
	}
	servers[eidetixServerName] = entryBytes

	serversBytes, err := json.Marshal(servers)
	if err != nil {
		return nil, false, err
	}
	root["mcpServers"] = serversBytes

	out, err := json.Marshal(root)
	if err != nil {
		return nil, false, err
	}
	return out, true, nil
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd server && go test ./internal/handler/ -run TestMergeEidetixServer -v`
Expected: PASS for all four sub-tests.

- [ ] **Step 5: Commit**

```bash
git add server/internal/handler/eidetix_merge.go server/internal/handler/eidetix_merge_test.go
git commit -m "feat(eidetix): pure mcp_config merge logic for the eidetix server entry"
```

---

### Task 3: Encryption key wiring on the Handler

**Files:**
- Modify: `server/internal/handler/handler.go` (add a field to the `Handler` struct)
- Modify: `server/cmd/server/router.go` (load `MULTICA_EIDETIX_SECRET_KEY` ~`:188`, alongside the Lark block)
- Test: `server/internal/handler/eidetix_test.go` (round-trip)

- [ ] **Step 1: Add the field to the Handler struct**

In `server/internal/handler/handler.go`, find the `Handler` struct (the one carrying `LarkInstallations`, `Queries`, `TaskService`, etc.) and add:

```go
	// EidetixSecrets decrypts per-project Eidetix tokens at claim time. nil
	// when MULTICA_EIDETIX_SECRET_KEY is unset — in which case Eidetix is
	// disabled platform-wide and the claim handler fails open (no eidetix
	// server, no loop skill).
	EidetixSecrets *secretbox.Box
```

Add the import `"github.com/multica-ai/multica/server/internal/util/secretbox"` to `handler.go` if not already present.

- [ ] **Step 2: Load the key in router.go**

In `server/cmd/server/router.go`, immediately after the Lark `if larkKey, err := secretbox.LoadKey("MULTICA_LARK_SECRET_KEY"); ...` block (ends ~`:230`), add:

```go
	// Eidetix integration. Only wired when MULTICA_EIDETIX_SECRET_KEY is set
	// (base64-encoded 32-byte key). Without it, per-project Eidetix tokens
	// cannot be decrypted, so the claim handler fails open and no agent gets
	// the eidetix MCP server. This is the platform-wide off switch.
	if eidetixKey, err := secretbox.LoadKey("MULTICA_EIDETIX_SECRET_KEY"); err == nil {
		box, err := secretbox.New(eidetixKey)
		if err != nil {
			slog.Error("eidetix: secretbox.New failed; eidetix integration disabled", "error", err)
		} else {
			h.EidetixSecrets = box
			slog.Info("eidetix integration enabled")
		}
	}
```

- [ ] **Step 3: Write the failing round-trip test**

Create `server/internal/handler/eidetix_test.go` with an initial round-trip test (more tests are added in later tasks):

```go
package handler

import (
	"testing"

	"github.com/multica-ai/multica/server/internal/util/secretbox"
)

// newTestEidetixBox builds a secretbox with a throwaway in-test key. NEVER use
// a real Eidetix token in tests; "fake-token" below is not a secret.
func newTestEidetixBox(t *testing.T) *secretbox.Box {
	t.Helper()
	key := make([]byte, secretbox.KeySize)
	for i := range key {
		key[i] = byte(i + 1)
	}
	box, err := secretbox.New(key)
	if err != nil {
		t.Fatalf("secretbox.New: %v", err)
	}
	return box
}

func TestEidetixTokenRoundTrip(t *testing.T) {
	box := newTestEidetixBox(t)
	const plain = "fake-token-not-a-secret"

	sealed, err := box.Seal([]byte(plain))
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if string(sealed) == plain {
		t.Fatalf("sealed bytes must not equal plaintext")
	}
	opened, err := box.Open(sealed)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if string(opened) != plain {
		t.Errorf("round-trip = %q, want %q", opened, plain)
	}
}
```

> If `secretbox.KeySize` is not an exported const, check the package: `grep -n "KeySize\|const " server/internal/util/secretbox/secretbox.go`. If it is unexported, hardcode `key := make([]byte, 32)`.

- [ ] **Step 4: Run the test**

Run: `cd server && go test ./internal/handler/ -run TestEidetixTokenRoundTrip -v && go build ./...`
Expected: PASS, and the whole backend compiles (confirming the struct field + router wiring are valid).

- [ ] **Step 5: Commit**

```bash
git add server/internal/handler/handler.go server/cmd/server/router.go server/internal/handler/eidetix_test.go
git commit -m "feat(eidetix): wire MULTICA_EIDETIX_SECRET_KEY secretbox onto the handler"
```

---

### Task 4: REST handlers + routes (admin config surface)

Owner/admin-gated, audited writes, token never returned. Mirrors the `agent_env.go` authorize → write → audit pattern, and reuses `resolveProjectID`/`loadProjectForResource` conventions.

**Files:**
- Modify: `server/internal/handler/eidetix.go` (created here — REST handlers)
- Modify: `server/cmd/server/router.go` (route registration)
- Test: `server/internal/handler/eidetix_test.go` (append handler tests)

- [ ] **Step 1: Write the failing handler tests**

Append to `server/internal/handler/eidetix_test.go`. These use the existing handler test DB harness (`testPool`, `testWorkspaceID`, and the auth/request helpers used across `*_test.go` in this package). Inspect `server/internal/handler/agent_env_test.go` and `daemon_test.go` for the exact request-builder helper names (e.g. how an owner-authenticated request is constructed) and mirror them.

```go
func insertTestProject(t *testing.T, title string) string {
	t.Helper()
	var id string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO project (workspace_id, title, status)
		VALUES ($1, $2, 'in_progress') RETURNING id
	`, testWorkspaceID, title).Scan(&id); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, id) })
	return id
}

func TestSetAndShowEidetixConfig_TokenNeverReturned(t *testing.T) {
	projectID := insertTestProject(t, "Eidetix Marketing")
	h := newTestHandler(t) // use this package's standard handler constructor
	h.EidetixSecrets = newTestEidetixBox(t)

	// PUT set
	setBody := map[string]any{
		"token":       "fake-token-not-a-secret",
		"graph_label": "Marketing",
	}
	rec := doOwnerRequest(t, h, http.MethodPut, "/api/projects/"+projectID+"/eidetix", setBody)
	if rec.Code != http.StatusOK {
		t.Fatalf("set: status = %d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "fake-token-not-a-secret") {
		t.Fatalf("set response leaked the token: %s", rec.Body.String())
	}

	// GET show
	rec = doOwnerRequest(t, h, http.MethodGet, "/api/projects/"+projectID+"/eidetix", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("show: status = %d body=%s", rec.Code, rec.Body.String())
	}
	var show map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &show); err != nil {
		t.Fatalf("show body not JSON: %v", err)
	}
	if show["configured"] != true {
		t.Errorf("configured = %v, want true", show["configured"])
	}
	if show["enabled"] != true {
		t.Errorf("enabled = %v, want true", show["enabled"])
	}
	if show["graph_label"] != "Marketing" {
		t.Errorf("graph_label = %v, want Marketing", show["graph_label"])
	}
	if _, present := show["token"]; present {
		t.Errorf("show response must never include a token field")
	}
	if strings.Contains(rec.Body.String(), "fake-token-not-a-secret") {
		t.Errorf("show response leaked the token")
	}
}

func TestDisableThenClearEidetixConfig(t *testing.T) {
	projectID := insertTestProject(t, "Eidetix Toggle")
	h := newTestHandler(t)
	h.EidetixSecrets = newTestEidetixBox(t)

	doOwnerRequest(t, h, http.MethodPut, "/api/projects/"+projectID+"/eidetix",
		map[string]any{"token": "fake-token-not-a-secret"})

	// PATCH disable
	rec := doOwnerRequest(t, h, http.MethodPatch, "/api/projects/"+projectID+"/eidetix",
		map[string]any{"enabled": false})
	if rec.Code != http.StatusOK {
		t.Fatalf("disable: status = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = doOwnerRequest(t, h, http.MethodGet, "/api/projects/"+projectID+"/eidetix", nil)
	var show map[string]any
	json.Unmarshal(rec.Body.Bytes(), &show)
	if show["enabled"] != false {
		t.Errorf("after disable, enabled = %v, want false", show["enabled"])
	}

	// DELETE clear
	rec = doOwnerRequest(t, h, http.MethodDelete, "/api/projects/"+projectID+"/eidetix", nil)
	if rec.Code != http.StatusOK && rec.Code != http.StatusNoContent {
		t.Fatalf("clear: status = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = doOwnerRequest(t, h, http.MethodGet, "/api/projects/"+projectID+"/eidetix", nil)
	json.Unmarshal(rec.Body.Bytes(), &show)
	if show["configured"] != false {
		t.Errorf("after clear, configured = %v, want false", show["configured"])
	}
}
```

> The helpers `newTestHandler` and `doOwnerRequest` are placeholders for whatever this package already uses. Before writing them, grep the test files: `grep -rn "func newTestHandler\|httptest.NewRecorder\|requireWorkspaceRole" server/internal/handler/*_test.go | head`. Reuse the existing constructor + owner-auth request builder (e.g. the one in `agent_env_test.go` / `handler_test.go`). Do NOT invent a parallel harness — wire `h.Queries`, `h.TaskService`, router, and an owner member exactly as the existing tests do, then route the request through the same `chi` mux so `chi.URLParam(r, "id")` resolves.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd server && go test ./internal/handler/ -run 'TestSetAndShowEidetixConfig|TestDisableThenClearEidetixConfig'`
Expected: FAIL — handlers and routes don't exist yet (compile error or 404).

- [ ] **Step 3: Write the REST handlers**

Create `server/internal/handler/eidetix.go` (the apply-at-claim method is added in Task 6; this step adds only the REST surface):

```go
package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const defaultEidetixEndpoint = "https://eidetix.nodeops.xyz/mcp/sse"

// authorizeEidetixConfig resolves the project from the URL and enforces
// workspace owner/admin. Mirrors authorizeAgentEnv.
func (h *Handler) authorizeEidetixConfig(w http.ResponseWriter, r *http.Request) (db.Project, db.Member, bool) {
	project, ok := h.loadProjectForResource(w, r, chi.URLParam(r, "id"))
	if !ok {
		return db.Project{}, db.Member{}, false
	}
	member, ok := h.requireWorkspaceRole(w, r, uuidToString(project.WorkspaceID), "project not found", "owner", "admin")
	if !ok {
		return db.Project{}, db.Member{}, false
	}
	return project, member, true
}

type setEidetixRequest struct {
	Token      string  `json:"token"`
	EndpointURL string `json:"endpoint_url"`
	GraphLabel *string `json:"graph_label"`
	Enabled    *bool   `json:"enabled"`
}

type eidetixShowResponse struct {
	Configured  bool   `json:"configured"`
	Enabled     bool   `json:"enabled"`
	EndpointURL string `json:"endpoint_url,omitempty"`
	GraphLabel  string `json:"graph_label,omitempty"`
}

// SetEidetixConfig upserts the project's Eidetix binding. Requires a non-empty
// token. The token is encrypted and never echoed back.
func (h *Handler) SetEidetixConfig(w http.ResponseWriter, r *http.Request) {
	project, _, ok := h.authorizeEidetixConfig(w, r)
	if !ok {
		return
	}
	if h.EidetixSecrets == nil {
		writeError(w, http.StatusServiceUnavailable, "eidetix is not configured on this server (MULTICA_EIDETIX_SECRET_KEY unset)")
		return
	}

	var req setEidetixRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.Token) == "" {
		writeError(w, http.StatusBadRequest, "token is required")
		return
	}

	sealed, err := h.EidetixSecrets.Seal([]byte(req.Token))
	if err != nil {
		slog.Error("eidetix: seal token failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to encrypt token")
		return
	}

	endpoint := strings.TrimSpace(req.EndpointURL)
	if endpoint == "" {
		endpoint = defaultEidetixEndpoint
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	label := pgtype.Text{}
	if req.GraphLabel != nil && strings.TrimSpace(*req.GraphLabel) != "" {
		label = pgtype.Text{String: strings.TrimSpace(*req.GraphLabel), Valid: true}
	}

	cfg, err := h.Queries.UpsertEidetixProjectConfig(r.Context(), db.UpsertEidetixProjectConfigParams{
		ProjectID:      project.ID,
		Enabled:        enabled,
		EndpointUrl:    endpoint,
		TokenEncrypted: sealed,
		GraphLabel:     label,
	})
	if err != nil {
		slog.Error("eidetix: upsert config failed", "error", err, "project_id", uuidToString(project.ID))
		writeError(w, http.StatusInternalServerError, "failed to save eidetix config")
		return
	}

	writeJSON(w, http.StatusOK, eidetixShowResponse{
		Configured:  true,
		Enabled:     cfg.Enabled,
		EndpointURL: cfg.EndpointUrl,
		GraphLabel:  cfg.GraphLabel.String,
	})
}

// ShowEidetixConfig reports status WITHOUT the token.
func (h *Handler) ShowEidetixConfig(w http.ResponseWriter, r *http.Request) {
	project, _, ok := h.authorizeEidetixConfig(w, r)
	if !ok {
		return
	}
	cfg, err := h.Queries.GetEidetixConfigForProject(r.Context(), project.ID)
	if err != nil {
		// No row → not configured (not an error to the caller).
		writeJSON(w, http.StatusOK, eidetixShowResponse{Configured: false})
		return
	}
	writeJSON(w, http.StatusOK, eidetixShowResponse{
		Configured:  true,
		Enabled:     cfg.Enabled,
		EndpointURL: cfg.EndpointUrl,
		GraphLabel:  cfg.GraphLabel.String,
	})
}

type patchEidetixRequest struct {
	Enabled *bool `json:"enabled"`
}

// PatchEidetixConfig toggles the enabled flag on an existing config.
func (h *Handler) PatchEidetixConfig(w http.ResponseWriter, r *http.Request) {
	project, _, ok := h.authorizeEidetixConfig(w, r)
	if !ok {
		return
	}
	var req patchEidetixRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Enabled == nil {
		writeError(w, http.StatusBadRequest, "enabled (boolean) is required")
		return
	}
	cfg, err := h.Queries.SetEidetixProjectEnabled(r.Context(), db.SetEidetixProjectEnabledParams{
		ProjectID: project.ID,
		Enabled:   *req.Enabled,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "eidetix not configured for this project")
		return
	}
	writeJSON(w, http.StatusOK, eidetixShowResponse{
		Configured:  true,
		Enabled:     cfg.Enabled,
		EndpointURL: cfg.EndpointUrl,
		GraphLabel:  cfg.GraphLabel.String,
	})
}

// ClearEidetixConfig deletes the project's binding.
func (h *Handler) ClearEidetixConfig(w http.ResponseWriter, r *http.Request) {
	project, _, ok := h.authorizeEidetixConfig(w, r)
	if !ok {
		return
	}
	if err := h.Queries.DeleteEidetixProjectConfig(r.Context(), project.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clear eidetix config")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
```

> Verify helper names against the package: `uuidToString`, `writeJSON`, `writeError`, `requireWorkspaceRole`, `loadProjectForResource` all exist (confirmed in handler.go / project_resource.go / agent_env.go). If `loadProjectForResource` is unexported in `project_resource.go`, it is in the same package — fine to call. The `SetEidetixProjectEnabledParams`/`UpsertEidetixProjectConfigParams` field names (`EndpointUrl`, `GraphLabel`, `TokenEncrypted`) come from Task 1's generated code — confirm exact casing in `eidetix.sql.go`.

- [ ] **Step 4: Register the routes**

In `server/cmd/server/router.go`, inside the `r.Route("/api/projects", ...)` block (find it near the agents route block ~`:835`; if projects routes are nested under `/{id}`, add there), add:

```go
		r.Route("/{id}/eidetix", func(r chi.Router) {
			// Per-project Eidetix binding. Owner/admin only. The PUT body
			// carries the Bearer token, which is encrypted at rest and never
			// returned by GET. See internal/handler/eidetix.go.
			r.Get("/", h.ShowEidetixConfig)
			r.Put("/", h.SetEidetixConfig)
			r.Patch("/", h.PatchEidetixConfig)
			r.Delete("/", h.ClearEidetixConfig)
		})
```

> If `/api/projects/{id}` is already a nested `r.Route("/{id}", ...)`, register `r.Route("/eidetix", ...)` inside that instead, so the param name stays `id`. Match the existing project-resource route nesting (the resources sub-collection at `/api/projects/{id}/resources` is the exact precedent — find it and mirror its placement).

- [ ] **Step 5: Run the handler tests**

Run: `cd server && go test ./internal/handler/ -run 'TestSetAndShowEidetixConfig|TestDisableThenClearEidetixConfig' -v`
Expected: PASS. If they fail on harness helpers, fix the test helpers to match the package's existing patterns (not the handlers).

- [ ] **Step 6: Commit**

```bash
git add server/internal/handler/eidetix.go server/internal/handler/eidetix_test.go server/cmd/server/router.go
git commit -m "feat(eidetix): owner/admin REST handlers for per-project config"
```

---

### Task 5: CLI subcommands — `multica project eidetix`

**Files:**
- Create: `server/cmd/multica/cmd_project_eidetix.go`
- Test: `server/cmd/multica/cmd_project_eidetix_test.go`

- [ ] **Step 1: Write the failing test for secure token resolution**

Create `server/cmd/multica/cmd_project_eidetix_test.go`:

```go
package main

import (
	"strings"
	"testing"
)

func TestResolveEidetixToken_Inline(t *testing.T) {
	cmd := newEidetixSetCmdForTest()
	cmd.Flags().Set("token", "fake-token")
	tok, err := resolveEidetixToken(cmd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tok != "fake-token" {
		t.Errorf("token = %q, want fake-token", tok)
	}
}

func TestResolveEidetixToken_Stdin(t *testing.T) {
	cmd := newEidetixSetCmdForTest()
	cmd.Flags().Set("token-stdin", "true")
	cmd.SetIn(strings.NewReader("fake-token-from-stdin\n"))
	tok, err := resolveEidetixToken(cmd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tok != "fake-token-from-stdin" {
		t.Errorf("token = %q, want trimmed stdin value", tok)
	}
}

func TestResolveEidetixToken_MutuallyExclusive(t *testing.T) {
	cmd := newEidetixSetCmdForTest()
	cmd.Flags().Set("token", "a")
	cmd.Flags().Set("token-stdin", "true")
	if _, err := resolveEidetixToken(cmd); err == nil {
		t.Fatalf("expected mutual-exclusion error")
	}
}

func TestResolveEidetixToken_Missing(t *testing.T) {
	cmd := newEidetixSetCmdForTest()
	if _, err := resolveEidetixToken(cmd); err == nil {
		t.Fatalf("expected error when no token source provided")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd server && go test ./cmd/multica/ -run TestResolveEidetixToken`
Expected: FAIL — `undefined: resolveEidetixToken`, `newEidetixSetCmdForTest`.

- [ ] **Step 3: Implement the CLI subcommands**

Create `server/cmd/multica/cmd_project_eidetix.go`:

```go
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var projectEidetixCmd = &cobra.Command{
	Use:   "eidetix",
	Short: "Manage a project's Eidetix shared-context binding (workspace owner/admin only)",
}

var projectEidetixSetCmd = &cobra.Command{
	Use:   "set <project-id>",
	Short: "Bind a project to an Eidetix graph by Bearer token (upsert)",
	Args:  exactArgs(1),
	RunE:  runProjectEidetixSet,
}

var projectEidetixShowCmd = &cobra.Command{
	Use:   "show <project-id>",
	Short: "Show a project's Eidetix binding status (never prints the token)",
	Args:  exactArgs(1),
	RunE:  runProjectEidetixShow,
}

var projectEidetixClearCmd = &cobra.Command{
	Use:   "clear <project-id>",
	Short: "Remove a project's Eidetix binding",
	Args:  exactArgs(1),
	RunE:  runProjectEidetixClear,
}

var projectEidetixEnableCmd = &cobra.Command{
	Use:   "enable <project-id>",
	Short: "Enable Eidetix for a project (keeps the stored token)",
	Args:  exactArgs(1),
	RunE:  func(cmd *cobra.Command, args []string) error { return runProjectEidetixToggle(cmd, args, true) },
}

var projectEidetixDisableCmd = &cobra.Command{
	Use:   "disable <project-id>",
	Short: "Disable Eidetix for a project (keeps the stored token)",
	Args:  exactArgs(1),
	RunE:  func(cmd *cobra.Command, args []string) error { return runProjectEidetixToggle(cmd, args, false) },
}

func init() {
	projectCmd.AddCommand(projectEidetixCmd)
	projectEidetixCmd.AddCommand(projectEidetixSetCmd)
	projectEidetixCmd.AddCommand(projectEidetixShowCmd)
	projectEidetixCmd.AddCommand(projectEidetixClearCmd)
	projectEidetixCmd.AddCommand(projectEidetixEnableCmd)
	projectEidetixCmd.AddCommand(projectEidetixDisableCmd)

	registerEidetixSetFlags(projectEidetixSetCmd)
	projectEidetixShowCmd.Flags().String("output", "table", "Output format: table or json")
}

// registerEidetixSetFlags is shared with the test constructor so the flag set
// stays in one place.
func registerEidetixSetFlags(cmd *cobra.Command) {
	cmd.Flags().String("token", "", "Eidetix Bearer token (prefer --token-stdin so it never lands in shell history)")
	cmd.Flags().Bool("token-stdin", false, "Read the token from stdin")
	cmd.Flags().String("token-file", "", "Read the token from a file path")
	cmd.Flags().String("endpoint", "", "Override the Eidetix endpoint URL (defaults to the partner SSE URL)")
	cmd.Flags().String("label", "", "Human label for the graph (e.g. Marketing). Never the token.")
	cmd.Flags().String("output", "table", "Output format: table or json")
}

// resolveEidetixToken reads the token from exactly one of --token,
// --token-stdin, or --token-file. Mirrors resolveMcpConfig's secure-input
// pattern so the secret never has to appear on the command line.
func resolveEidetixToken(cmd *cobra.Command) (string, error) {
	inline := cmd.Flags().Changed("token")
	fromStdin, _ := cmd.Flags().GetBool("token-stdin")
	filePath, _ := cmd.Flags().GetString("token-file")
	fromFile := cmd.Flags().Changed("token-file")

	count := 0
	if inline {
		count++
	}
	if fromStdin {
		count++
	}
	if fromFile {
		count++
	}
	switch {
	case count == 0:
		return "", fmt.Errorf("a token is required: pass --token-stdin (recommended), --token-file, or --token")
	case count > 1:
		return "", fmt.Errorf("--token, --token-stdin, and --token-file are mutually exclusive; pick one")
	}

	var raw string
	switch {
	case inline:
		raw, _ = cmd.Flags().GetString("token")
	case fromStdin:
		buf, err := io.ReadAll(cmd.InOrStdin())
		if err != nil {
			return "", fmt.Errorf("read --token-stdin: %w", err)
		}
		raw = string(buf)
	case fromFile:
		buf, err := os.ReadFile(filePath)
		if err != nil {
			return "", fmt.Errorf("read --token-file: %w", err)
		}
		raw = string(buf)
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("token is empty")
	}
	return raw, nil
}

func runProjectEidetixSet(cmd *cobra.Command, args []string) error {
	token, err := resolveEidetixToken(cmd)
	if err != nil {
		return err
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	projectRef, err := resolveProjectID(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve project: %w", err)
	}

	body := map[string]any{"token": token}
	if v, _ := cmd.Flags().GetString("endpoint"); strings.TrimSpace(v) != "" {
		body["endpoint_url"] = strings.TrimSpace(v)
	}
	if v, _ := cmd.Flags().GetString("label"); strings.TrimSpace(v) != "" {
		body["graph_label"] = strings.TrimSpace(v)
	}

	var result map[string]any
	if err := client.PutJSON(ctx, "/api/projects/"+projectRef.ID+"/eidetix", body, &result); err != nil {
		return fmt.Errorf("set eidetix config: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Eidetix bound to project %s.\n", projectRef.Display)
	return printEidetixResult(cmd, result)
}

func runProjectEidetixShow(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	projectRef, err := resolveProjectID(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve project: %w", err)
	}
	var result map[string]any
	if err := client.GetJSON(ctx, "/api/projects/"+projectRef.ID+"/eidetix", &result); err != nil {
		return fmt.Errorf("show eidetix config: %w", err)
	}
	return printEidetixResult(cmd, result)
}

func runProjectEidetixClear(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	projectRef, err := resolveProjectID(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve project: %w", err)
	}
	if err := client.DeleteJSON(ctx, "/api/projects/"+projectRef.ID+"/eidetix"); err != nil {
		return fmt.Errorf("clear eidetix config: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Eidetix binding removed from project %s.\n", projectRef.Display)
	return nil
}

func runProjectEidetixToggle(cmd *cobra.Command, args []string, enabled bool) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	projectRef, err := resolveProjectID(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve project: %w", err)
	}
	var result map[string]any
	if err := client.PatchJSON(ctx, "/api/projects/"+projectRef.ID+"/eidetix", map[string]any{"enabled": enabled}, &result); err != nil {
		return fmt.Errorf("toggle eidetix config: %w", err)
	}
	state := "enabled"
	if !enabled {
		state = "disabled"
	}
	fmt.Fprintf(os.Stderr, "Eidetix %s for project %s.\n", state, projectRef.Display)
	return printEidetixResult(cmd, result)
}

func printEidetixResult(cmd *cobra.Command, result map[string]any) error {
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	headers := []string{"CONFIGURED", "ENABLED", "ENDPOINT", "LABEL"}
	rows := [][]string{{
		fmt.Sprintf("%v", result["configured"]),
		fmt.Sprintf("%v", result["enabled"]),
		strVal(result, "endpoint_url"),
		strVal(result, "graph_label"),
	}}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}
```

Also add the test constructor at the bottom of `cmd_project_eidetix_test.go` (kept in the test file, not shipped):

```go
func newEidetixSetCmdForTest() *cobra.Command {
	cmd := &cobra.Command{Use: "set"}
	registerEidetixSetFlags(cmd)
	return cmd
}
```
(Import `"github.com/spf13/cobra"` in the test file.)

- [ ] **Step 4: Confirm `PatchJSON` exists on the API client (or add it)**

Run: `grep -n "func (c \*APIClient) PatchJSON" server/internal/cli/client.go`
- If present: continue.
- If absent: add it by copying `PutJSON` and changing `http.MethodPut` → `http.MethodPatch`:

```go
func (c *APIClient) PatchJSON(ctx context.Context, path string, body any, out any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, c.BaseURL+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	c.setHeaders(req)
	resp, err := c.HTTPClient.Do(req)
	err = wrapTransport(req, err)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return newHTTPError(http.MethodPatch, path, resp)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
```

- [ ] **Step 5: Run the CLI tests and build the CLI**

Run: `cd server && go test ./cmd/multica/ -run TestResolveEidetixToken -v && go build ./cmd/multica/`
Expected: PASS; CLI builds.

- [ ] **Step 6: Commit**

```bash
git add server/cmd/multica/cmd_project_eidetix.go server/cmd/multica/cmd_project_eidetix_test.go server/internal/cli/client.go
git commit -m "feat(eidetix): multica project eidetix set/show/clear/enable/disable CLI"
```

---

### Task 6: Claim-handler integration — merge at claim time

Wires Task 2 (merge) + Task 1 (queries) + Task 3 (decrypt) + Task 7's skill accessor into the claim response. Fail-open throughout.

**Files:**
- Modify: `server/internal/handler/eidetix.go` (add `applyEidetixToClaim`)
- Modify: `server/internal/handler/daemon.go` (call it after project resolution, ~`:1224`)
- Test: `server/internal/handler/eidetix_test.go` (append claim-integration tests)

> This task depends on Task 7's `h.TaskService.EidetixLoopSkill()`. If executing strictly in order, implement Task 7 first OR stub the skill append behind a small interface; the recommended order is **Task 7 before Task 6**. The steps below assume `EidetixLoopSkill()` exists.

- [ ] **Step 1: Write the `applyEidetixToClaim` method**

Append to `server/internal/handler/eidetix.go`:

```go
// applyEidetixToClaim merges the project's Eidetix MCP server into the claim
// response and appends the loop skill — IF the project has an enabled config
// and the token decrypts. Every failure path is fail-open: it logs and returns
// without touching resp, so Eidetix can never block or fail a task.
//
// projectID is the resolved issue.project_id. resp.Agent must be non-nil.
func (h *Handler) applyEidetixToClaim(ctx context.Context, projectID pgtype.UUID, resp *AgentTaskResponse) {
	if h.EidetixSecrets == nil || resp == nil || resp.Agent == nil || !projectID.Valid {
		return
	}

	cfg, err := h.Queries.GetEidetixConfigForProject(ctx, projectID)
	if err != nil {
		return // no row → not configured. Expected, not an error.
	}
	if !cfg.Enabled {
		return
	}

	token, err := h.EidetixSecrets.Open(cfg.TokenEncrypted)
	if err != nil {
		slog.Error("eidetix: token decrypt failed; proceeding without eidetix",
			"error", err, "project_id", uuidToString(projectID))
		return
	}

	merged, added, err := mergeEidetixServer(resp.Agent.McpConfig, cfg.EndpointUrl, string(token))
	if err != nil {
		slog.Warn("eidetix: agent mcp_config malformed; proceeding without eidetix",
			"error", err, "project_id", uuidToString(projectID))
		return
	}
	if added {
		resp.Agent.McpConfig = merged
	} else {
		slog.Info("eidetix: agent already defines an 'eidetix' mcp server; leaving it untouched",
			"project_id", uuidToString(projectID))
	}

	// Append the loop skill whenever Eidetix is enabled and the token
	// decrypted — an eidetix server is present either way (managed or
	// user-defined), so the recall/ingest guidance is valid.
	resp.Agent.Skills = append(resp.Agent.Skills, h.TaskService.EidetixLoopSkill()...)
}
```

Add `"context"` and `"github.com/jackc/pgx/v5/pgtype"` to the imports if not already present.

- [ ] **Step 2: Call it from the claim handler**

In `server/internal/handler/daemon.go`, inside `ClaimTaskByRuntime`, locate the project-resolution block (~`:1219-1224`):

```go
		if issue.ProjectID.Valid {
			resp.ProjectID = uuidToString(issue.ProjectID)
			if proj, err := h.Queries.GetProject(r.Context(), issue.ProjectID); err == nil {
				resp.ProjectTitle = proj.Title
			}
```

Immediately after `resp.ProjectTitle = proj.Title` (still inside `if issue.ProjectID.Valid {`), add:

```go
			// Eidetix shared-context: if this project is bound to an enabled
			// Eidetix graph, merge its MCP server + the loop skill into the
			// claim response. Fail-open — never blocks the task.
			h.applyEidetixToClaim(r.Context(), issue.ProjectID, resp)
```

> Confirm the exact variable carrying the response is `resp` and that `resp.Agent` is already populated at this point in the function (Skills/McpConfig were assigned earlier ~`:1130-1141`, before issue/project resolution ~`:1189+`, so `resp.Agent` is non-nil here). If the response variable has a different name, use it.

- [ ] **Step 3: Write the failing claim-integration test**

Append to `server/internal/handler/eidetix_test.go`. Reuse the claim fixtures from `daemon_test.go` (`createClaimReclaimRuntime`, `createClaimReclaimAgentAndIssue`, `newDaemonTokenRequest`). Add a helper to attach the issue to a project + insert an enabled config:

```go
func insertEnabledEidetixConfig(t *testing.T, box *secretbox.Box, projectID, token string) {
	t.Helper()
	sealed, err := box.Seal([]byte(token))
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO eidetix_project_config (project_id, enabled, endpoint_url, token_encrypted, graph_label)
		VALUES ($1, true, 'https://eidetix.example/mcp/sse', $2, 'Marketing')
		ON CONFLICT (project_id) DO UPDATE SET enabled = true, token_encrypted = EXCLUDED.token_encrypted
	`, projectID, sealed); err != nil {
		t.Fatalf("insert eidetix config: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM eidetix_project_config WHERE project_id = $1`, projectID) })
}
```

Then the enabled-path test (adapt the claim-request construction and response decoding to match `daemon_test.go`'s existing claim test — find a test that calls `h.ClaimTaskByRuntime` and copy its setup):

```go
func TestClaim_EnabledEidetix_MergesServerAndSkill(t *testing.T) {
	ctx := context.Background()
	box := newTestEidetixBox(t)

	// Build runtime + agent + issue via the daemon_test fixtures, then attach
	// the issue to a project and bind an enabled Eidetix config.
	// (Pseudo-fixture calls — wire to the real helpers in daemon_test.go.)
	projectID := insertTestProject(t, "Eidetix Claim")
	runtimeID := createClaimReclaimRuntime(t, ctx, "eidetix-rt")
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "eidetix-claim")
	_ = agentID
	if _, err := testPool.Exec(ctx, `UPDATE issue SET project_id = $1 WHERE id = $2`, projectID, issueID); err != nil {
		t.Fatalf("attach issue to project: %v", err)
	}
	insertEnabledEidetixConfig(t, box, projectID, "fake-token-not-a-secret")

	h := newTestHandler(t)
	h.EidetixSecrets = box

	rec := httptest.NewRecorder()
	req := newDaemonTokenRequest(http.MethodPost, "/api/runtimes/"+runtimeID+"/claim", nil, testWorkspaceID, "")
	// Route through the chi mux with {runtimeID} bound, exactly as daemon_test.go does.
	routeClaim(t, h, rec, req, runtimeID)

	if rec.Code != http.StatusOK {
		t.Fatalf("claim status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp AgentTaskResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode claim resp: %v", err)
	}
	if resp.Agent == nil {
		t.Fatalf("claim returned no agent")
	}
	// Server merged
	var mc struct {
		McpServers map[string]json.RawMessage `json:"mcpServers"`
	}
	if err := json.Unmarshal(resp.Agent.McpConfig, &mc); err != nil {
		t.Fatalf("mcp_config not JSON: %s", resp.Agent.McpConfig)
	}
	if _, ok := mc.McpServers["eidetix"]; !ok {
		t.Errorf("eidetix server not merged into claim mcp_config: %s", resp.Agent.McpConfig)
	}
	// Skill appended
	found := false
	for _, s := range resp.Agent.Skills {
		if s.Name == "multica-eidetix" {
			found = true
		}
	}
	if !found {
		t.Errorf("multica-eidetix skill not appended to claim response")
	}
}

func TestClaim_DisabledEidetix_NoMergeNoSkill(t *testing.T) {
	ctx := context.Background()
	box := newTestEidetixBox(t)
	projectID := insertTestProject(t, "Eidetix Disabled")
	runtimeID := createClaimReclaimRuntime(t, ctx, "eidetix-rt-off")
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "eidetix-off")
	_ = agentID
	testPool.Exec(ctx, `UPDATE issue SET project_id = $1 WHERE id = $2`, projectID, issueID)
	insertEnabledEidetixConfig(t, box, projectID, "fake-token-not-a-secret")
	// Flip to disabled.
	testPool.Exec(ctx, `UPDATE eidetix_project_config SET enabled = false WHERE project_id = $1`, projectID)

	h := newTestHandler(t)
	h.EidetixSecrets = box

	rec := httptest.NewRecorder()
	req := newDaemonTokenRequest(http.MethodPost, "/api/runtimes/"+runtimeID+"/claim", nil, testWorkspaceID, "")
	routeClaim(t, h, rec, req, runtimeID)

	var resp AgentTaskResponse
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Agent != nil && len(resp.Agent.McpConfig) > 0 {
		var mc struct{ McpServers map[string]json.RawMessage `json:"mcpServers"` }
		json.Unmarshal(resp.Agent.McpConfig, &mc)
		if _, ok := mc.McpServers["eidetix"]; ok {
			t.Errorf("disabled config still merged eidetix server")
		}
	}
	if resp.Agent != nil {
		for _, s := range resp.Agent.Skills {
			if s.Name == "multica-eidetix" {
				t.Errorf("disabled config still appended the loop skill")
			}
		}
	}
}
```

> `routeClaim` is a placeholder for whatever the existing claim test uses to invoke the handler through the chi router with the runtime URL param bound. Find the real claim test in `daemon_test.go` (search for `ClaimTaskByRuntime`) and copy its request-routing exactly — including the URL path shape and param name (`runtimeID` vs `id`). Match `createClaimReclaimAgentAndIssue`'s real return signature. The intent of the two tests is fixed; the plumbing must match the existing harness.

- [ ] **Step 4: Run the integration tests**

Run: `cd server && go test ./internal/handler/ -run 'TestClaim_EnabledEidetix|TestClaim_DisabledEidetix' -v`
Expected: PASS. Enabled → eidetix server + skill present; disabled → neither.

- [ ] **Step 5: Run the full handler package test + build**

Run: `cd server && go build ./... && go test ./internal/handler/ -run Eidetix`
Expected: all Eidetix tests green.

- [ ] **Step 6: Commit**

```bash
git add server/internal/handler/eidetix.go server/internal/handler/daemon.go server/internal/handler/eidetix_test.go
git commit -m "feat(eidetix): merge eidetix server + loop skill into the task-claim response"
```

---

### Task 7: The `multica-eidetix` loop skill (conditionally shipped)

A new embedded skill OUTSIDE `builtin_skills/`, exposed via `TaskService.EidetixLoopSkill()`. **Implement this before Task 6** (Task 6 calls the accessor).

**Files:**
- Create: `server/internal/service/eidetix_skill/multica-eidetix/SKILL.md`
- Create: `server/internal/service/eidetix_skill/multica-eidetix/references/eidetix-tools-source-map.md`
- Create: `server/internal/service/eidetix_skill.go`
- Test: `server/internal/service/eidetix_skill_test.go`

- [ ] **Step 1: Write the skill body `SKILL.md`**

Create `server/internal/service/eidetix_skill/multica-eidetix/SKILL.md`:

```markdown
---
name: multica-eidetix
description: "Use when working any issue whose project is bound to an Eidetix shared-context graph. Teaches the read-before-acting and write-after-deciding loop against the eidetix MCP server so a team of agents shares one cited memory instead of each starting cold."
user-invocable: false
allowed-tools: mcp__eidetix__recall, mcp__eidetix__search, mcp__eidetix__get_graph, mcp__eidetix__get_graph_expanded, mcp__eidetix__get_content, mcp__eidetix__resolve_entities, mcp__eidetix__get_schema, mcp__eidetix__ingest_traces
---

# Eidetix shared context

This project is wired to an Eidetix knowledge graph through the `eidetix` MCP
server. Eidetix is the team's shared, cited memory: decisions, brand and voice
facts, campaign outcomes, and entity relationships that prior agents recorded.
You both **read** from it before acting and **write** to it after you learn
something durable. Treat it as a colleague's notes, not as ground truth — cite
what informs your work, and verify before you rely on a claim.

The `eidetix` tools only exist when the project is bound; if you do not see
them, this project has no shared graph and you work from the issue alone. Never
let an Eidetix call failure block the task — if a call errors, continue without
it.

## Before you act — recall first

1. `recall` the issue's topic in one shot — it returns a cited document plus the
   relevant graph sections. Start here for almost every issue.
2. `search` for narrower phrasings when `recall` is thin or you need a specific
   prior artifact.
3. `resolve_entities` for the people, tools, products, or campaigns named in the
   issue, to get their canonical graph identity before you reason about them.
4. `get_graph` / `get_graph_expanded` to widen from a resolved entity to its
   neighbours; `get_content` to read the full source behind a cited snippet.

Fold what you find into your plan and cite it in your work ("per the brand voice
note in shared context, …"). Do not re-derive what the graph already records.

## After meaningful work — ingest what's durable

When you make a decision, establish a fact, or produce an outcome that the
**next** agent would want to know:

1. Call `get_schema` FIRST — it tells you the trace shape Eidetix expects. Never
   call `ingest_traces` without it.
2. Call `ingest_traces` with the durable facts: decisions and their rationale,
   brand/voice/positioning facts, campaign results, canonical entity facts.

Ingest durable knowledge, not transients. Worth writing: "We standardized CTA
copy to X because Y." Not worth writing: "Ran a draft, will revise." Avoid
duplicating an entry the graph already holds — if `recall`/`search` already
surfaced the fact, do not re-ingest it.

## Boundaries

- The graph is shared across every agent on this project. Write facts, not
  half-finished scratch.
- Do not ingest secrets, credentials, or tokens.
- Eidetix is advisory. If it is unavailable, note that you proceeded without
  shared context and continue.

See `references/eidetix-tools-source-map.md` for the full tool contract.
```

- [ ] **Step 2: Write the source-map reference file**

Create `server/internal/service/eidetix_skill/multica-eidetix/references/eidetix-tools-source-map.md`:

```markdown
# Eidetix tools — source-traced contract

The source of truth for these tools is the partner-hosted Eidetix MCP server
(`https://eidetix.nodeops.xyz/mcp/sse`), not this repository. Multica injects
the `eidetix` MCP server into an agent's `mcp_config` at task-claim time when
the issue's project has an enabled Eidetix binding
(`server/internal/handler/eidetix.go` → `applyEidetixToClaim`). The Bearer token
selects the graph; there is no namespace parameter.

## Read tools

| Tool | Contract |
|------|----------|
| `recall` | One-shot cited recall for a topic. Returns a document plus the relevant graph sections. The default entry point. |
| `search` | Narrower query over recorded knowledge. Use when `recall` is thin. |
| `resolve_entities` | Maps names (people, tools, products, campaigns) to canonical graph entities. |
| `get_graph` | Returns the subgraph around an entity. |
| `get_graph_expanded` | Wider neighbourhood expansion from an entity. |
| `get_content` | Returns the full source behind a cited snippet. |

## Write tools

| Tool | Contract |
|------|----------|
| `get_schema` | Returns the trace schema. MUST be called before `ingest_traces`. |
| `ingest_traces` | Persists new observation traces (durable facts) into the graph. |

## Multica-side integration points

- Merge + skill injection: `server/internal/handler/eidetix.go`
  (`applyEidetixToClaim`, `mergeEidetixServer` in `eidetix_merge.go`).
- Per-project binding storage: `eidetix_project_config` table
  (`server/migrations/120_eidetix_project_config.up.sql`).
- Admin config CLI: `multica project eidetix set/show/clear/enable/disable`
  (`server/cmd/multica/cmd_project_eidetix.go`).
```

- [ ] **Step 3: Write the accessor with its own embed**

Create `server/internal/service/eidetix_skill.go`:

```go
package service

import (
	"embed"
	"io/fs"
	"path"
	"strings"
)

//go:embed eidetix_skill
var eidetixSkillFS embed.FS

const (
	eidetixSkillRoot = "eidetix_skill"
	eidetixSkillName = "multica-eidetix"
)

// EidetixLoopSkill returns the conditionally-shipped Eidetix read/write loop
// skill. It is deliberately NOT part of BuiltinSkills() (which ships to every
// agent): the claim handler appends it only when a project is bound to an
// enabled Eidetix graph. Returns nil if the skill fails to load (fail-open).
func (s *TaskService) EidetixLoopSkill() []AgentSkillData {
	skill, ok := loadEidetixSkill()
	if !ok {
		return nil
	}
	return []AgentSkillData{skill}
}

func loadEidetixSkill() (AgentSkillData, bool) {
	dir := path.Join(eidetixSkillRoot, eidetixSkillName)
	content, err := fs.ReadFile(eidetixSkillFS, path.Join(dir, "SKILL.md"))
	if err != nil {
		return AgentSkillData{}, false
	}
	skill := AgentSkillData{Name: eidetixSkillName, Content: string(content)}
	_ = fs.WalkDir(eidetixSkillFS, dir, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() {
			return walkErr
		}
		rel := strings.TrimPrefix(p, dir+"/")
		if rel == "SKILL.md" {
			return nil
		}
		data, readErr := fs.ReadFile(eidetixSkillFS, p)
		if readErr != nil {
			return nil
		}
		skill.Files = append(skill.Files, AgentSkillFileData{Path: rel, Content: string(data)})
		return nil
	})
	return skill, true
}
```

> Confirm the `AgentSkillData` / `AgentSkillFileData` field names against `server/internal/service/task.go` (they were reported as `Name`, `Content`, `Files`, and `Path`/`Content`). If the builtin loader sets `ID` or `Description`, mirror what `loadBuiltinSkill` does so the wire shape matches.

- [ ] **Step 4: Write the conformance test**

Create `server/internal/service/eidetix_skill_test.go`. It applies the same template invariants `TestBuiltinSkillsConformToTemplate` enforces, plus the contract-skill frontmatter shape, reusing this package's `splitFrontmatter` / `skillHasFile` helpers:

```go
package service

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestEidetixLoopSkillConformsToTemplate(t *testing.T) {
	var svc TaskService
	skills := svc.EidetixLoopSkill()
	if len(skills) != 1 {
		t.Fatalf("EidetixLoopSkill() returned %d skills, want 1", len(skills))
	}
	skill := skills[0]

	if skill.Name != "multica-eidetix" {
		t.Errorf("name = %q, want multica-eidetix", skill.Name)
	}
	if !strings.HasPrefix(skill.Name, "multica-") {
		t.Errorf("name %q must carry the multica- prefix", skill.Name)
	}

	fm, body, ok := splitFrontmatter(skill.Content)
	if !ok {
		t.Fatalf("SKILL.md must lead with a --- frontmatter block")
	}
	if strings.TrimSpace(fm["name"]) == "" {
		t.Errorf("frontmatter missing name")
	}
	desc := strings.TrimSpace(fm["description"])
	if desc == "" {
		t.Errorf("frontmatter missing description")
	}
	if len(desc) > maxDescriptionChars {
		t.Errorf("description is %d chars, over the %d cap", len(desc), maxDescriptionChars)
	}
	if n := strings.Count(body, "\n") + 1; n > maxSkillBodyLines {
		t.Errorf("SKILL.md body is %d lines, over the %d-line budget", n, maxSkillBodyLines)
	}
	if got := strings.TrimSpace(fm["user-invocable"]); got != "false" {
		t.Errorf("user-invocable = %q, want false (context-triggered platform skill)", got)
	}

	// Strict YAML frontmatter (the MUL-3100 guard): a Codex-style runtime drops
	// a skill whose frontmatter is not valid YAML 1.2.
	if !strings.HasPrefix(skill.Content, "---\n") {
		t.Fatalf("missing leading frontmatter delimiter")
	}
	rest := skill.Content[len("---\n"):]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		t.Fatalf("frontmatter has no closing delimiter")
	}
	var parsed map[string]any
	if err := yaml.Unmarshal([]byte(rest[:end]), &parsed); err != nil {
		t.Fatalf("frontmatter is not valid YAML: %v", err)
	}

	// Evals must not ride along as shipped files.
	for _, f := range skill.Files {
		lower := strings.ToLower(f.Path)
		if strings.Contains(lower, "eval") || strings.HasSuffix(lower, "_test.go") || strings.HasSuffix(lower, "_test.md") {
			t.Errorf("supporting file %q looks like an eval/test", f.Path)
		}
	}

	if !skillHasFile(skill, "references/eidetix-tools-source-map.md") {
		t.Errorf("missing supporting file references/eidetix-tools-source-map.md")
	}

	// The loop must teach both halves and name the gating write tool.
	mustContain := []string{"recall", "ingest_traces", "get_schema", "resolve_entities"}
	for _, want := range mustContain {
		if !strings.Contains(skill.Content, want) {
			t.Errorf("skill body missing %q", want)
		}
	}
}
```

- [ ] **Step 5: Run the skill test**

Run: `cd server && go test ./internal/service/ -run 'TestEidetixLoopSkillConformsToTemplate|TestBuiltinSkillsConformToTemplate' -v`
Expected: PASS — and crucially `TestBuiltinSkillsConformToTemplate` still passes (the eidetix skill is in its own embed, so it is NOT picked up by `loadBuiltinSkills()` and does not leak into every agent).

- [ ] **Step 6: Verify the skill is NOT in BuiltinSkills()**

Add to the same test file:

```go
func TestEidetixSkillNotInBuiltins(t *testing.T) {
	for _, s := range loadBuiltinSkills() {
		if s.Name == "multica-eidetix" {
			t.Fatalf("multica-eidetix must NOT be a built-in skill; it ships only to Eidetix-enabled projects")
		}
	}
}
```

Run: `cd server && go test ./internal/service/ -run TestEidetixSkillNotInBuiltins -v`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add server/internal/service/eidetix_skill.go server/internal/service/eidetix_skill_test.go server/internal/service/eidetix_skill/
git commit -m "feat(eidetix): conditionally-shipped multica-eidetix loop skill"
```

---

### Task 8: Documentation upkeep (doc-rule compliance)

**Files:**
- Modify: `server/internal/service/builtin_skills/multica-projects-and-resources/references/projects-and-resources-source-map.md`

- [ ] **Step 1: Add a source-map pointer to the new admin command**

Append to `server/internal/service/builtin_skills/multica-projects-and-resources/references/projects-and-resources-source-map.md`:

```markdown

## Project Eidetix binding (admin-only; not agent-facing)

`multica project eidetix set/show/clear/enable/disable` binds a project to a
partner Eidetix shared-context graph. This is a workspace owner/admin config
command — agents never run it — so it is intentionally **not** documented in
this skill's agent-facing body. Sources:

- CLI: `server/cmd/multica/cmd_project_eidetix.go`
- REST: `server/internal/handler/eidetix.go` (`/api/projects/{id}/eidetix`)
- Storage: `eidetix_project_config` (`server/migrations/120_eidetix_project_config.up.sql`)
- Claim-time injection: `applyEidetixToClaim` in `server/internal/handler/eidetix.go`
```

- [ ] **Step 2: Confirm the projects skill test still passes**

Run: `cd server && go test ./internal/service/ -run TestProjectsAndResourcesSkillCoversDurableContext -v`
Expected: PASS (we appended to the source-map file, not the SKILL.md body, so no `mustContain`/`mustNotContain` assertion is affected).

- [ ] **Step 3: Commit**

```bash
git add server/internal/service/builtin_skills/multica-projects-and-resources/references/projects-and-resources-source-map.md
git commit -m "docs(eidetix): note admin eidetix command in projects source map"
```

---

### Task 9: Provider-transport verification + rollout doc

Eidetix is a remote HTTP/SSE MCP server. A provider backend that only speaks
stdio MCP will reject the `url`-based `eidetix` entry. This task documents the
supported set and the verification procedure — it is a **documented manual
procedure**, not an automated test, because it exercises external provider
binaries against a live (or stub) SSE endpoint.

**Files:**
- Create: `docs/superpowers/runbooks/2026-06-14-eidetix-provider-verification.md`

- [ ] **Step 1: Write the verification runbook**

Create `docs/superpowers/runbooks/2026-06-14-eidetix-provider-verification.md`:

```markdown
# Eidetix provider-transport verification

Eidetix loads only if the provider backend supports a `url`-based (remote
HTTP/SSE) MCP server entry. Verify each backend a marketing agent will run on
BEFORE pointing real marketing work at it.

## Known status
- **Claude Code** — known-good (remote MCP via url/type).
- **OpenClaw** — known-good (partner demos `transport: streamable-http`).
- **Codex, Hermes, Gemini, kiro, kimi** — UNVERIFIED. A stdio-only backend will
  reject or ignore the entry. Do not assume graceful degradation — check.

## Procedure (per backend)
1. Bind a throwaway test project to a test Eidetix token:
   `printf '%s' "$TEST_TOKEN" | multica project eidetix set <project> --token-stdin --label Test`
2. Create an agent on the target provider; assign it an issue in that project.
3. In the agent run logs, confirm the `eidetix` MCP server initialized and that
   `recall`/`search` tools are listed as available.
4. Confirm the agent successfully calls `recall` (or that the tool is at least
   discoverable). A connection/tool-list error means the backend does not
   support the remote entry → mark UNSUPPORTED.
5. Record the result in the table below.

## Supported set (fill in during rollout)
| Backend | Remote MCP url entry loads? | Verified by | Date |
|---------|------------------------------|-------------|------|
| Claude Code | yes (known) | | |
| OpenClaw | yes (known) | | |
| Codex | ? | | |
| Hermes | ? | | |
| Gemini | ? | | |
| kiro | ? | | |
| kimi | ? | | |

## Operational rule
Marketing agents MUST run on a backend marked "yes". The per-project `enabled`
flag is the off switch if a binding misbehaves:
`multica project eidetix disable <project>`.
```

- [ ] **Step 2: Commit**

```bash
git add docs/superpowers/runbooks/2026-06-14-eidetix-provider-verification.md
git commit -m "docs(eidetix): provider-transport verification runbook"
```

---

### Task 10: Full verification pass

- [ ] **Step 1: Build everything**

Run: `cd server && go build ./...`
Expected: no errors.

- [ ] **Step 2: Run the full Go test suite for the touched packages**

Run:
```bash
cd server && go test ./internal/handler/ ./internal/service/ ./cmd/multica/
```
Expected: all green. (The handler/claim tests need the test PostgreSQL; ensure it is up per the repo's test DB setup.)

- [ ] **Step 3: Vet + format**

Run: `cd server && gofmt -l . | grep -v '^$' && go vet ./...`
Expected: `gofmt -l` prints nothing (no unformatted files); `go vet` is clean.

- [ ] **Step 4: Confirm no token strings leaked into the tree**

Run:
```bash
git grep -nE "aZQE6rQM|squhXpeXO8u" -- . ':(exclude)docs/superpowers/specs/*' || echo "clean: no real tokens in tree"
```
Expected: `clean: no real tokens in tree` (the real Marketing/Support token prefixes must appear nowhere).

---

## Self-Review

**1. Spec coverage**

| Spec component | Task(s) |
|---|---|
| C1 data model `eidetix_project_config` + sqlc | Task 1 |
| C2 token encryption (reuse secretbox) | Task 3 (key wiring), used in Tasks 4 & 6 |
| C3 CLI `multica project eidetix` + REST audited write | Task 5 (CLI), Task 4 (REST) |
| C4 claim-handler merge (fail-open, no-clobber, response-copy-only) | Task 2 (pure merge) + Task 6 (wiring) |
| C5 conditionally-shipped loop skill + accessor + conformance | Task 7 |
| Provider/transport constraint verification | Task 9 |
| Error handling (fail-open at claim, decrypt, malformed config) | Task 6 Step 1 + Task 2 tests |
| Testing matrix (enabled/disabled/decrypt-fail/preserve/no-clobber/skill conformance/crypto round-trip) | Tasks 2, 3, 6, 7 |
| Doc/contract upkeep | Task 8 |
| Rollout | Task 9 runbook |

Gap check: the spec's "decrypt failure → task still claims" is covered by `applyEidetixToClaim`'s fail-open `Open` error path (Task 6 Step 1); add an explicit decrypt-failure claim test if time permits (insert a config row whose `token_encrypted` is garbage bytes and assert the claim still 200s without an `eidetix` server) — noted here so it is not forgotten.

**2. Placeholder scan**

The plan flags every spot where a name must be confirmed against the real harness (`newTestHandler`, `doOwnerRequest`, `routeClaim`, `createClaimReclaimAgentAndIssue` signature, project table name `project` vs `projects`, `secretbox.KeySize`, `PatchJSON` presence, `AgentSkillData` fields). These are verification instructions, not unfilled implementation — every code block is complete. The engineer must reconcile test-harness helper names with the existing `*_test.go` files rather than inventing parallel ones.

**3. Type consistency**

- `mergeEidetixServer(existing json.RawMessage, endpointURL, token string) (json.RawMessage, bool, error)` — same signature in Task 2 (def), Task 6 (call).
- `applyEidetixToClaim(ctx context.Context, projectID pgtype.UUID, resp *AgentTaskResponse)` — defined Task 6 Step 1, called Task 6 Step 2.
- `EidetixLoopSkill() []AgentSkillData` — defined Task 7, called Task 6.
- Generated query/param names (`GetEidetixConfigForProject`, `UpsertEidetixProjectConfigParams{ProjectID,Enabled,EndpointUrl,TokenEncrypted,GraphLabel}`, `SetEidetixProjectEnabledParams{ProjectID,Enabled}`) — produced in Task 1, consumed in Tasks 4 & 6. Confirm exact casing from generated code in Task 1 Step 5 before consuming.
- Skill name string `"multica-eidetix"` — identical in the skill frontmatter, the accessor constant, and all test assertions.
```
