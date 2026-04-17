# GitLab Issues Integration — Phase 1: Foundation — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Land the plumbing that lets a workspace admin paste a GitLab service PAT + project identifier, have the server validate both against gitlab.com, and persist the encrypted credentials. No issue read/write wiring yet; webhooks, sync, and cache schema are Phase 2.

**Architecture:** One new migration adds two tables (`workspace_gitlab_connection` and `user_gitlab_connection`). A new `server/pkg/secrets/` package provides AES-256-GCM encryption for PATs. A new `server/pkg/gitlab/` HTTP client library calls `GET /user` and `GET /projects/:id` to validate credentials before persisting. Three new HTTP endpoints (`POST`/`GET`/`DELETE /api/workspaces/{id}/gitlab/connect`) drive the connect flow, gated behind a new `MULTICA_GITLAB_ENABLED` env flag. The frontend adds a workspace settings page (`packages/views/workspace/settings/gitlab/`) with shared hooks in `packages/core/gitlab/`, wired into both web and desktop.

**Tech Stack:**
- Go 1.26, Chi router, `pgx/v5`, `sqlc`, golang-migrate (existing stack)
- `crypto/aes` + `crypto/cipher` (stdlib) for AES-GCM
- TypeScript, React, TanStack Query, Zustand, Vitest, Testing Library, Next.js (web), Electron + react-router (desktop) (existing stack)

**Design spec:** `docs/superpowers/specs/2026-04-17-gitlab-issues-integration-design.md`

---

## File Structure

### New files (backend)

| Path | Responsibility |
|---|---|
| `server/pkg/secrets/secrets.go` | AES-256-GCM `Cipher` struct with `Encrypt`/`Decrypt`; `Load()` reads `MULTICA_SECRETS_KEY`. |
| `server/pkg/secrets/secrets_test.go` | Unit tests — round-trip, tamper detection, bad key size. |
| `server/pkg/gitlab/client.go` | HTTP client: `Client` struct, `do()` method, auth header injection, error parsing. |
| `server/pkg/gitlab/client_test.go` | Tests for request construction + error handling against `httptest.Server`. |
| `server/pkg/gitlab/user.go` | `CurrentUser(ctx, token) (*User, error)` method. |
| `server/pkg/gitlab/user_test.go` | Tests. |
| `server/pkg/gitlab/project.go` | `GetProject(ctx, token, idOrPath string) (*Project, error)` method. |
| `server/pkg/gitlab/project_test.go` | Tests. |
| `server/pkg/gitlab/errors.go` | Typed errors: `ErrUnauthorized`, `ErrNotFound`, `ErrForbidden`. |
| `server/migrations/049_gitlab_connection.up.sql` | Creates both connection tables. |
| `server/migrations/049_gitlab_connection.down.sql` | Drops them. |
| `server/pkg/db/queries/gitlab_connection.sql` | sqlc queries — `CreateWorkspaceGitlabConnection`, `GetWorkspaceGitlabConnection`, `DeleteWorkspaceGitlabConnection`, plus the user-connection trio for Phase 3 consumers. |
| `server/internal/handler/gitlab_connection.go` | Three handler methods: `ConnectGitlabWorkspace`, `GetGitlabWorkspaceConnection`, `DisconnectGitlabWorkspace`. |
| `server/internal/handler/gitlab_connection_test.go` | Handler tests against the real DB (same pattern as other `*_test.go` in that package). |

### New files (frontend)

| Path | Responsibility |
|---|---|
| `packages/core/gitlab/api.ts` | API client calls: `connectWorkspace`, `disconnectWorkspace`, `getWorkspaceConnection`. |
| `packages/core/gitlab/queries.ts` | TanStack Query keys + `useWorkspaceGitlabConnection(wsId)` hook. |
| `packages/core/gitlab/mutations.ts` | `useConnectWorkspaceGitlabMutation`, `useDisconnectWorkspaceGitlabMutation`. |
| `packages/core/gitlab/types.ts` | Shared types: `GitlabConnection`, `ConnectGitlabInput`. |
| `packages/core/gitlab/api.test.ts` | Vitest coverage for api call shapes. |
| `packages/core/gitlab/mutations.test.ts` | Vitest coverage for mutation behavior. |
| `packages/views/workspace/settings/gitlab/connect-gitlab-page.tsx` | Shared view component (not-connected / connecting / connected / error states). |
| `packages/views/workspace/settings/gitlab/connect-gitlab-page.test.tsx` | Testing Library coverage. |
| `apps/web/app/[workspaceSlug]/(dashboard)/settings/gitlab/page.tsx` | Next.js route wrapper. |
| `apps/desktop/src/renderer/src/pages/settings-gitlab.tsx` | Desktop route wrapper (1:1 with the web wrapper). |

### Modified files

| Path | Change |
|---|---|
| `Makefile` | Add `generate-secrets-key` helper; call it from `setup` if `MULTICA_SECRETS_KEY` missing in `.env`. |
| `server/cmd/server/main.go` | Warn if `MULTICA_SECRETS_KEY` missing (pattern matches `JWT_SECRET`). Load cipher and inject into `Handler`. |
| `server/internal/handler/handler.go` | `Handler` struct gains `SecretsCipher *secrets.Cipher` + `GitlabClient *gitlab.Client` + `GitlabEnabled bool`. `New(...)` signature grows to accept them. |
| `server/cmd/server/router.go` | Mount the three new `/api/workspaces/{id}/gitlab/connect` endpoints under the existing workspace-membership middleware, gated by `h.GitlabEnabled`. |
| `packages/views/workspace/settings/` (nav) | Add "GitLab" entry — exact file identified in Task 14 by searching the existing settings nav. |
| `apps/desktop/src/renderer/src/routes.tsx` | Register the desktop route. |
| `.env.example` | Add `MULTICA_SECRETS_KEY` and `MULTICA_GITLAB_ENABLED` with doc comments. |

---

## Task 1: Secrets package — AES-256-GCM cipher

**Files:**
- Create: `server/pkg/secrets/secrets.go`
- Create: `server/pkg/secrets/secrets_test.go`

- [ ] **Step 1.1: Write failing tests**

Create `server/pkg/secrets/secrets_test.go` with this exact content:

```go
package secrets

import (
	"bytes"
	"encoding/base64"
	"testing"
)

func testKey(t *testing.T) []byte {
	t.Helper()
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	return key
}

func TestCipher_EncryptDecryptRoundTrip(t *testing.T) {
	c, err := NewCipher(testKey(t))
	if err != nil {
		t.Fatalf("NewCipher: %v", err)
	}
	plaintext := []byte("glpat-xxxxxxxxxxxxxxxxxxxx")
	ciphertext, err := c.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if bytes.Equal(ciphertext, plaintext) {
		t.Fatal("ciphertext equals plaintext")
	}
	got, err := c.Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Fatalf("round-trip mismatch: got %q want %q", got, plaintext)
	}
}

func TestCipher_EncryptProducesFreshNonce(t *testing.T) {
	c, err := NewCipher(testKey(t))
	if err != nil {
		t.Fatalf("NewCipher: %v", err)
	}
	plaintext := []byte("same plaintext")
	a, err := c.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	b, err := c.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if bytes.Equal(a, b) {
		t.Fatal("expected different ciphertexts due to fresh nonce")
	}
}

func TestCipher_DecryptTamperedFails(t *testing.T) {
	c, err := NewCipher(testKey(t))
	if err != nil {
		t.Fatalf("NewCipher: %v", err)
	}
	ciphertext, err := c.Encrypt([]byte("plaintext"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	// Flip one byte in the tag region.
	ciphertext[len(ciphertext)-1] ^= 0xff
	if _, err := c.Decrypt(ciphertext); err == nil {
		t.Fatal("expected decrypt to fail on tampered ciphertext")
	}
}

func TestCipher_RejectsWrongKeySize(t *testing.T) {
	if _, err := NewCipher(make([]byte, 16)); err == nil {
		t.Fatal("expected NewCipher to reject 16-byte key")
	}
	if _, err := NewCipher(make([]byte, 64)); err == nil {
		t.Fatal("expected NewCipher to reject 64-byte key")
	}
}

func TestLoadFromEnv_Success(t *testing.T) {
	key := testKey(t)
	t.Setenv("MULTICA_SECRETS_KEY", base64.StdEncoding.EncodeToString(key))
	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	ciphertext, err := c.Encrypt([]byte("x"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if _, err := c.Decrypt(ciphertext); err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
}

func TestLoadFromEnv_Missing(t *testing.T) {
	t.Setenv("MULTICA_SECRETS_KEY", "")
	if _, err := Load(); err == nil {
		t.Fatal("expected Load to fail when env var missing")
	}
}

func TestLoadFromEnv_WrongSize(t *testing.T) {
	t.Setenv("MULTICA_SECRETS_KEY", base64.StdEncoding.EncodeToString([]byte("too-short")))
	if _, err := Load(); err == nil {
		t.Fatal("expected Load to fail on wrong-size key")
	}
}
```

- [ ] **Step 1.2: Run tests — expect build failure**

Run: `cd /Users/jimmy.mills/Developer/multica/server && go test ./pkg/secrets/ -run TestCipher -v`
Expected: compilation error — `secrets` package does not exist yet.

- [ ] **Step 1.3: Implement the cipher**

Create `server/pkg/secrets/secrets.go`:

```go
// Package secrets provides AES-256-GCM encryption for small values (PATs, OAuth tokens).
// Ciphertext format: [12-byte nonce][ciphertext][16-byte GCM tag] (the Seal output already
// appends the tag, so the on-disk format is simply [nonce][Seal(output)]).
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
)

const keySize = 32 // AES-256

// Cipher encrypts and decrypts small byte slices with AES-GCM.
// Construct via NewCipher (direct key) or Load (from MULTICA_SECRETS_KEY env var).
type Cipher struct {
	aead cipher.AEAD
}

// NewCipher constructs a Cipher from a 32-byte key. Returns an error for any other size.
func NewCipher(key []byte) (*Cipher, error) {
	if len(key) != keySize {
		return nil, fmt.Errorf("secrets: key must be %d bytes, got %d", keySize, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("secrets: aes.NewCipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("secrets: cipher.NewGCM: %w", err)
	}
	return &Cipher{aead: aead}, nil
}

// Encrypt returns [nonce || ciphertext || tag].
func (c *Cipher) Encrypt(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("secrets: read nonce: %w", err)
	}
	return c.aead.Seal(nonce, nonce, plaintext, nil), nil
}

// Decrypt expects the output format from Encrypt.
func (c *Cipher) Decrypt(ciphertext []byte) ([]byte, error) {
	ns := c.aead.NonceSize()
	if len(ciphertext) < ns+c.aead.Overhead() {
		return nil, errors.New("secrets: ciphertext too short")
	}
	nonce, body := ciphertext[:ns], ciphertext[ns:]
	return c.aead.Open(nil, nonce, body, nil)
}

// Load reads MULTICA_SECRETS_KEY (base64-encoded 32 bytes) and returns a Cipher.
func Load() (*Cipher, error) {
	raw := os.Getenv("MULTICA_SECRETS_KEY")
	if raw == "" {
		return nil, errors.New("secrets: MULTICA_SECRETS_KEY is not set")
	}
	key, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("secrets: MULTICA_SECRETS_KEY must be base64: %w", err)
	}
	return NewCipher(key)
}
```

- [ ] **Step 1.4: Run tests — expect pass**

Run: `cd /Users/jimmy.mills/Developer/multica/server && go test ./pkg/secrets/ -v`
Expected: All tests PASS.

- [ ] **Step 1.5: Commit**

```bash
cd /Users/jimmy.mills/Developer/multica
git add server/pkg/secrets/
git commit -m "feat(secrets): add AES-256-GCM cipher with env-based key loading"
```

---

## Task 2: Makefile — auto-generate dev secrets key on `make setup`

**Files:**
- Modify: `Makefile`
- Modify: `.env.example`

- [ ] **Step 2.1: Read the current setup target**

Run: `grep -n -A 20 "^setup:" /Users/jimmy.mills/Developer/multica/Makefile`

Note the setup target's layout so your addition fits the existing style.

- [ ] **Step 2.2: Add a `generate-secrets-key` helper and wire it into setup**

Edit `Makefile` — add this target near the other `generate-*` / `db-*` helpers (pick the nearest cluster):

```make
generate-secrets-key:
	@if ! grep -q '^MULTICA_SECRETS_KEY=' .env 2>/dev/null; then \
		key=$$(head -c 32 /dev/urandom | base64) ; \
		echo "MULTICA_SECRETS_KEY=$$key" >> .env ; \
		echo "generated MULTICA_SECRETS_KEY in .env" ; \
	fi
```

Then extend the `setup:` target so `generate-secrets-key` runs before server start. Exact placement depends on the current target body — if `setup` calls `env-copy` or similar, put `generate-secrets-key` immediately after that and before migrations.

- [ ] **Step 2.3: Document in `.env.example`**

Append to `.env.example`:

```
# Base64-encoded 32-byte key used by server/pkg/secrets to encrypt stored
# credentials (GitLab PATs and similar). Generate with:
#   head -c 32 /dev/urandom | base64
# `make setup` auto-generates this into .env if missing.
MULTICA_SECRETS_KEY=

# Feature flag: when true, /api/workspaces/{id}/gitlab/* endpoints are served
# and the GitLab settings page appears in the UI.
MULTICA_GITLAB_ENABLED=false
```

- [ ] **Step 2.4: Verify by running generate-secrets-key in a temp env**

Run:
```bash
cd /tmp && rm -f .env && make -f /Users/jimmy.mills/Developer/multica/Makefile generate-secrets-key && grep MULTICA_SECRETS_KEY .env
```

Expected: file contains `MULTICA_SECRETS_KEY=<base64>`. Delete the temp file.

- [ ] **Step 2.5: Commit**

```bash
cd /Users/jimmy.mills/Developer/multica
git add Makefile .env.example
git commit -m "chore(setup): auto-generate dev MULTICA_SECRETS_KEY + document env flags"
```

---

## Task 3: GitLab client — HTTP scaffolding and typed errors

**Files:**
- Create: `server/pkg/gitlab/client.go`
- Create: `server/pkg/gitlab/errors.go`
- Create: `server/pkg/gitlab/client_test.go`

- [ ] **Step 3.1: Write failing test**

Create `server/pkg/gitlab/client_test.go`:

```go
package gitlab

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClient_SendsPrivateTokenHeader(t *testing.T) {
	var gotToken string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.Header.Get("PRIVATE-TOKEN")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, srv.Client())
	var out map[string]any
	if err := c.get(context.Background(), "glpat-abc", "/ping", &out); err != nil {
		t.Fatalf("get: %v", err)
	}
	if gotToken != "glpat-abc" {
		t.Fatalf("PRIVATE-TOKEN header = %q, want %q", gotToken, "glpat-abc")
	}
}

func TestClient_Parses401AsErrUnauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"message":"401 Unauthorized"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, srv.Client())
	err := c.get(context.Background(), "tok", "/x", nil)
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("err = %v, want ErrUnauthorized", err)
	}
}

func TestClient_Parses404AsErrNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"404 Not Found"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, srv.Client())
	err := c.get(context.Background(), "tok", "/x", nil)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}
```

- [ ] **Step 3.2: Run tests — expect build failure**

Run: `cd /Users/jimmy.mills/Developer/multica/server && go test ./pkg/gitlab/ -v`
Expected: compilation error — package `gitlab` does not exist.

- [ ] **Step 3.3: Create `server/pkg/gitlab/errors.go`**

```go
package gitlab

import "errors"

// Sentinel errors that callers match via errors.Is.
var (
	ErrUnauthorized = errors.New("gitlab: unauthorized (401)")
	ErrForbidden    = errors.New("gitlab: forbidden (403)")
	ErrNotFound     = errors.New("gitlab: not found (404)")
)

// APIError wraps non-classified non-2xx responses so callers see the HTTP status + message.
type APIError struct {
	StatusCode int
	Message    string
}

func (e *APIError) Error() string { return e.Message }
```

- [ ] **Step 3.4: Create `server/pkg/gitlab/client.go`**

```go
// Package gitlab is a small client library for the GitLab REST API v4.
// Tokens are passed per call (never stored on the Client) so the caller can use
// per-user or per-workspace tokens interchangeably.
package gitlab

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

const DefaultBaseURL = "https://gitlab.com"

// Client performs HTTP calls to a GitLab instance.
type Client struct {
	baseURL string
	http    *http.Client
}

// NewClient constructs a Client with a given base URL and http.Client.
// Pass http.DefaultClient (or a timeout-bounded one) for production use.
func NewClient(baseURL string, hc *http.Client) *Client {
	if hc == nil {
		hc = http.DefaultClient
	}
	return &Client{baseURL: baseURL, http: hc}
}

func (c *Client) get(ctx context.Context, token, path string, out any) error {
	return c.do(ctx, http.MethodGet, token, path, nil, out)
}

func (c *Client) do(ctx context.Context, method, token, path string, body, out any) error {
	var reqBody io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("gitlab: marshal body: %w", err)
		}
		reqBody = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+"/api/v4"+path, reqBody)
	if err != nil {
		return fmt.Errorf("gitlab: build request: %w", err)
	}
	req.Header.Set("PRIVATE-TOKEN", token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("gitlab: http do: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if out == nil {
			io.Copy(io.Discard, resp.Body)
			return nil
		}
		return json.NewDecoder(resp.Body).Decode(out)
	}

	// Non-2xx: classify.
	respBody, _ := io.ReadAll(resp.Body)
	var parsed struct {
		Message any `json:"message"`
	}
	_ = json.Unmarshal(respBody, &parsed)
	msg := fmt.Sprintf("%v", parsed.Message)
	if msg == "<nil>" || msg == "" {
		msg = string(respBody)
	}

	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return fmt.Errorf("%w: %s", ErrUnauthorized, msg)
	case http.StatusForbidden:
		return fmt.Errorf("%w: %s", ErrForbidden, msg)
	case http.StatusNotFound:
		return fmt.Errorf("%w: %s", ErrNotFound, msg)
	default:
		return &APIError{StatusCode: resp.StatusCode, Message: msg}
	}
}
```

Note: the `fmt.Errorf("%w: …")` wrapping is what makes `errors.Is(err, ErrUnauthorized)` succeed.

- [ ] **Step 3.5: Run tests — expect pass**

Run: `cd /Users/jimmy.mills/Developer/multica/server && go test ./pkg/gitlab/ -v`
Expected: All three tests PASS.

- [ ] **Step 3.6: Commit**

```bash
cd /Users/jimmy.mills/Developer/multica
git add server/pkg/gitlab/
git commit -m "feat(gitlab): add HTTP client scaffolding with typed errors"
```

---

## Task 4: GitLab client — `CurrentUser` method

**Files:**
- Create: `server/pkg/gitlab/user.go`
- Create: `server/pkg/gitlab/user_test.go`

- [ ] **Step 4.1: Write failing test**

Create `server/pkg/gitlab/user_test.go`:

```go
package gitlab

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCurrentUser_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v4/user" {
			t.Errorf("path = %q, want /api/v4/user", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id": 42, "username": "alice", "name": "Alice A", "avatar_url": "https://x/avatar.png"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, srv.Client())
	u, err := c.CurrentUser(context.Background(), "tok")
	if err != nil {
		t.Fatalf("CurrentUser: %v", err)
	}
	if u.ID != 42 || u.Username != "alice" || u.Name != "Alice A" {
		t.Fatalf("unexpected user: %+v", u)
	}
}
```

- [ ] **Step 4.2: Run test — expect build failure**

Run: `cd /Users/jimmy.mills/Developer/multica/server && go test ./pkg/gitlab/ -run TestCurrentUser -v`
Expected: compilation error — `CurrentUser` undefined.

- [ ] **Step 4.3: Implement**

Create `server/pkg/gitlab/user.go`:

```go
package gitlab

import "context"

// User mirrors the subset of GET /api/v4/user we care about.
type User struct {
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatar_url"`
}

// CurrentUser returns the user the given token is authenticated as.
func (c *Client) CurrentUser(ctx context.Context, token string) (*User, error) {
	var u User
	if err := c.get(ctx, token, "/user", &u); err != nil {
		return nil, err
	}
	return &u, nil
}
```

- [ ] **Step 4.4: Run tests — expect pass**

Run: `cd /Users/jimmy.mills/Developer/multica/server && go test ./pkg/gitlab/ -v`
Expected: All tests PASS.

- [ ] **Step 4.5: Commit**

```bash
cd /Users/jimmy.mills/Developer/multica
git add server/pkg/gitlab/user.go server/pkg/gitlab/user_test.go
git commit -m "feat(gitlab): add CurrentUser method"
```

---

## Task 5: GitLab client — `GetProject` method

**Files:**
- Create: `server/pkg/gitlab/project.go`
- Create: `server/pkg/gitlab/project_test.go`

- [ ] **Step 5.1: Write failing test**

Create `server/pkg/gitlab/project_test.go`:

```go
package gitlab

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetProject_ByNumericID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v4/projects/123" {
			t.Errorf("path = %q, want /api/v4/projects/123", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id": 123, "path_with_namespace": "group/app"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, srv.Client())
	p, err := c.GetProject(context.Background(), "tok", "123")
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	if p.ID != 123 || p.PathWithNamespace != "group/app" {
		t.Fatalf("unexpected project: %+v", p)
	}
}

func TestGetProject_ByPathIsURLEncoded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// URL path delivered by httptest already decodes %2F back to /, so the
		// handler sees /api/v4/projects/group/app. We check RequestURI for the
		// raw form instead.
		if r.RequestURI != "/api/v4/projects/group%2Fapp" {
			t.Errorf("request URI = %q, want /api/v4/projects/group%%2Fapp", r.RequestURI)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id": 7, "path_with_namespace": "group/app"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, srv.Client())
	p, err := c.GetProject(context.Background(), "tok", "group/app")
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	if p.ID != 7 {
		t.Fatalf("id = %d, want 7", p.ID)
	}
}

func TestGetProject_404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message": "404 Project Not Found"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, srv.Client())
	_, err := c.GetProject(context.Background(), "tok", "missing")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}
```

- [ ] **Step 5.2: Run test — expect build failure**

Run: `cd /Users/jimmy.mills/Developer/multica/server && go test ./pkg/gitlab/ -run TestGetProject -v`
Expected: compilation error — `GetProject` undefined.

- [ ] **Step 5.3: Implement**

Create `server/pkg/gitlab/project.go`:

```go
package gitlab

import (
	"context"
	"net/url"
	"strconv"
	"strings"
)

// Project mirrors the subset of GET /api/v4/projects/:id we care about.
type Project struct {
	ID                int64  `json:"id"`
	Name              string `json:"name"`
	PathWithNamespace string `json:"path_with_namespace"`
	WebURL            string `json:"web_url"`
	Description       string `json:"description"`
}

// GetProject looks up a project by numeric ID or by URL-encoded path ("group/project").
func (c *Client) GetProject(ctx context.Context, token, idOrPath string) (*Project, error) {
	// Numeric → use as-is; path → URL-encode slashes (GitLab convention).
	ref := idOrPath
	if _, err := strconv.ParseInt(idOrPath, 10, 64); err != nil {
		ref = url.PathEscape(strings.TrimPrefix(idOrPath, "/"))
	}
	var p Project
	if err := c.get(ctx, token, "/projects/"+ref, &p); err != nil {
		return nil, err
	}
	return &p, nil
}
```

- [ ] **Step 5.4: Run tests — expect pass**

Run: `cd /Users/jimmy.mills/Developer/multica/server && go test ./pkg/gitlab/ -v`
Expected: All tests PASS.

- [ ] **Step 5.5: Commit**

```bash
cd /Users/jimmy.mills/Developer/multica
git add server/pkg/gitlab/project.go server/pkg/gitlab/project_test.go
git commit -m "feat(gitlab): add GetProject method (numeric ID or path)"
```

---

## Task 6: Database migration — connection tables

**Files:**
- Create: `server/migrations/049_gitlab_connection.up.sql`
- Create: `server/migrations/049_gitlab_connection.down.sql`

- [ ] **Step 6.1: Verify the next migration number**

Run: `ls /Users/jimmy.mills/Developer/multica/server/migrations | sort | tail -5`

Confirm the highest prefix is `048_`. If another migration beat you to `049_`, bump to `050_` (and use that number throughout the rest of this task).

- [ ] **Step 6.2: Create `server/migrations/049_gitlab_connection.up.sql`**

```sql
-- GitLab integration: per-workspace and per-user connection records.
-- Phase 1 only creates the tables + indices; webhook/sync columns are left
-- nullable because Phase 2 populates them during initial sync.

CREATE TABLE IF NOT EXISTS workspace_gitlab_connection (
    workspace_id UUID PRIMARY KEY REFERENCES workspace(id) ON DELETE CASCADE,
    gitlab_project_id BIGINT NOT NULL,
    gitlab_project_path TEXT NOT NULL,
    service_token_encrypted BYTEA NOT NULL,
    service_token_user_id BIGINT NOT NULL,
    webhook_secret TEXT,
    webhook_gitlab_id BIGINT,
    last_sync_cursor TIMESTAMPTZ,
    connection_status TEXT NOT NULL DEFAULT 'connected'
        CHECK (connection_status IN ('connecting', 'connected', 'error')),
    status_message TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_workspace_gitlab_connection_project
    ON workspace_gitlab_connection(gitlab_project_id);

CREATE TABLE IF NOT EXISTS user_gitlab_connection (
    user_id UUID NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    gitlab_user_id BIGINT NOT NULL,
    gitlab_username TEXT NOT NULL,
    pat_encrypted BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, workspace_id)
);

CREATE INDEX IF NOT EXISTS idx_user_gitlab_connection_workspace
    ON user_gitlab_connection(workspace_id);
```

- [ ] **Step 6.3: Verify the `workspace` / `user` table names**

Run: `grep -l "CREATE TABLE" /Users/jimmy.mills/Developer/multica/server/migrations/001_init.up.sql | xargs grep -n "CREATE TABLE" | head -20`

The FKs in Step 6.2 assume tables are named `workspace` and `"user"` (singular, `user` quoted because it's a reserved keyword). If either is pluralized in `001_init.up.sql`, fix the FK references in the migration before continuing.

- [ ] **Step 6.4: Create `server/migrations/049_gitlab_connection.down.sql`**

```sql
DROP TABLE IF EXISTS user_gitlab_connection;
DROP TABLE IF EXISTS workspace_gitlab_connection;
```

- [ ] **Step 6.5: Run the migration**

Run: `cd /Users/jimmy.mills/Developer/multica && make migrate-up`
Expected: migration applies without error. Verify with:
```bash
docker exec -i multica-postgres psql -U multica -d multica -c "\d workspace_gitlab_connection"
docker exec -i multica-postgres psql -U multica -d multica -c "\d user_gitlab_connection"
```
(Your container/DB name may differ — match what `make db-up` produced.)

- [ ] **Step 6.6: Roll back and re-apply to verify both directions**

```bash
cd /Users/jimmy.mills/Developer/multica
make migrate-down
make migrate-up
```

Expected: both complete cleanly.

- [ ] **Step 6.7: Commit**

```bash
git add server/migrations/049_gitlab_connection.up.sql server/migrations/049_gitlab_connection.down.sql
git commit -m "feat(db): migration for gitlab connection tables"
```

---

## Task 7: sqlc queries for GitLab connection

**Files:**
- Create: `server/pkg/db/queries/gitlab_connection.sql`
- Regenerate: `server/pkg/db/generated/` (via `make sqlc`)

- [ ] **Step 7.1: Create the query file**

Create `server/pkg/db/queries/gitlab_connection.sql`:

```sql
-- name: CreateWorkspaceGitlabConnection :one
INSERT INTO workspace_gitlab_connection (
    workspace_id,
    gitlab_project_id,
    gitlab_project_path,
    service_token_encrypted,
    service_token_user_id,
    connection_status
)
VALUES ($1, $2, $3, $4, $5, 'connected')
RETURNING *;

-- name: GetWorkspaceGitlabConnection :one
SELECT * FROM workspace_gitlab_connection
WHERE workspace_id = $1;

-- name: DeleteWorkspaceGitlabConnection :exec
DELETE FROM workspace_gitlab_connection
WHERE workspace_id = $1;

-- name: UpsertUserGitlabConnection :one
INSERT INTO user_gitlab_connection (
    user_id,
    workspace_id,
    gitlab_user_id,
    gitlab_username,
    pat_encrypted
)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (user_id, workspace_id) DO UPDATE SET
    gitlab_user_id = EXCLUDED.gitlab_user_id,
    gitlab_username = EXCLUDED.gitlab_username,
    pat_encrypted = EXCLUDED.pat_encrypted
RETURNING *;

-- name: GetUserGitlabConnection :one
SELECT * FROM user_gitlab_connection
WHERE user_id = $1 AND workspace_id = $2;

-- name: DeleteUserGitlabConnection :exec
DELETE FROM user_gitlab_connection
WHERE user_id = $1 AND workspace_id = $2;
```

- [ ] **Step 7.2: Regenerate sqlc code**

Run: `cd /Users/jimmy.mills/Developer/multica && make sqlc`
Expected: no errors. New methods appear in `server/pkg/db/generated/`.

- [ ] **Step 7.3: Verify generated code compiles**

Run: `cd /Users/jimmy.mills/Developer/multica/server && go build ./...`
Expected: builds cleanly.

- [ ] **Step 7.4: Commit**

```bash
cd /Users/jimmy.mills/Developer/multica
git add server/pkg/db/queries/gitlab_connection.sql server/pkg/db/generated/
git commit -m "feat(db): sqlc queries for gitlab connection tables"
```

---

## Task 8: Handler — `Handler` struct gains cipher, GitLab client, feature flag

**Files:**
- Modify: `server/internal/handler/handler.go`
- Modify: `server/cmd/server/main.go`
- Modify: `server/cmd/server/router.go` (only the `NewRouter` signature)

- [ ] **Step 8.1: Extend the `Handler` struct**

Open `server/internal/handler/handler.go`. Add imports:

```go
import (
	// …existing imports…
	"github.com/multica-ai/multica/server/pkg/gitlab"
	"github.com/multica-ai/multica/server/pkg/secrets"
)
```

Add fields to the `Handler` struct (between `CFSigner` and the closing brace):

```go
	Secrets       *secrets.Cipher
	Gitlab        *gitlab.Client
	GitlabEnabled bool
```

Extend the `New(...)` constructor signature to take these:

```go
func New(
	queries *db.Queries,
	txStarter txStarter,
	hub *realtime.Hub,
	bus *events.Bus,
	emailService *service.EmailService,
	store storage.Storage,
	cfSigner *auth.CloudFrontSigner,
	secretsCipher *secrets.Cipher,
	gitlabClient *gitlab.Client,
	gitlabEnabled bool,
) *Handler {
	// …existing body…
	return &Handler{
		// …existing fields…
		Secrets:       secretsCipher,
		Gitlab:        gitlabClient,
		GitlabEnabled: gitlabEnabled,
	}
}
```

- [ ] **Step 8.2: Update `cmd/server/main.go` to load secrets + gitlab client**

Add import:

```go
	"net/http"
	"time"
	"github.com/multica-ai/multica/server/pkg/gitlab"
	"github.com/multica-ai/multica/server/pkg/secrets"
```

(`net/http` and `time` likely already imported — skip duplicates.)

After the `RESEND_API_KEY` warn block, add:

```go
	secretsCipher, err := secrets.Load()
	if err != nil {
		slog.Warn("MULTICA_SECRETS_KEY not configured; generating an ephemeral dev key. Set MULTICA_SECRETS_KEY for production.", "error", err)
		// Generate an in-memory key for dev so startup doesn't fail.
		ephemeral := make([]byte, 32)
		if _, err := rand.Read(ephemeral); err != nil {
			slog.Error("failed to generate ephemeral secrets key", "error", err)
			os.Exit(1)
		}
		secretsCipher, _ = secrets.NewCipher(ephemeral)
	}

	gitlabEnabled := os.Getenv("MULTICA_GITLAB_ENABLED") == "true"
	gitlabClient := gitlab.NewClient(gitlab.DefaultBaseURL, &http.Client{Timeout: 30 * time.Second})
```

Import `crypto/rand` (alias to avoid shadowing `math/rand` if already present).

- [ ] **Step 8.3: Update every `NewRouter` / `handler.New` call site**

Run: `grep -rn "handler.New(" /Users/jimmy.mills/Developer/multica/server`

For each hit, thread through the three new args. In `server/cmd/server/router.go`, update the `NewRouter` signature to accept them and pass them to `handler.New`.

- [ ] **Step 8.4: Fix test fixtures**

Run: `grep -rn "handler.New(" /Users/jimmy.mills/Developer/multica/server`

In `server/internal/handler/handler_test.go` (`TestMain`), update the `testHandler = New(...)` call to pass `nil, nil, false` for the three new arguments — handler tests don't exercise the cipher or gitlab client yet.

- [ ] **Step 8.5: Verify build + tests**

Run:
```bash
cd /Users/jimmy.mills/Developer/multica/server
go build ./...
go test ./internal/handler/ -run TestMain -v
```
Expected: build passes; existing tests still pass.

- [ ] **Step 8.6: Commit**

```bash
cd /Users/jimmy.mills/Developer/multica
git add server/internal/handler/handler.go server/cmd/server/main.go server/cmd/server/router.go server/internal/handler/handler_test.go
git commit -m "feat(server): wire secrets cipher + gitlab client + feature flag into Handler"
```

---

## Task 9: Handler — `POST /api/workspaces/{id}/gitlab/connect`

**Files:**
- Create: `server/internal/handler/gitlab_connection.go`
- Create: `server/internal/handler/gitlab_connection_test.go`

- [ ] **Step 9.1: Write failing test**

Create `server/internal/handler/gitlab_connection_test.go`:

```go
package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/realtime"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/pkg/gitlab"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/secrets"
)

// buildHandlerWithGitlab returns a Handler whose GitLab client points at the given fake server URL.
func buildHandlerWithGitlab(t *testing.T, fakeGitlabURL string) *Handler {
	t.Helper()
	key := make([]byte, 32)
	cipher, err := secrets.NewCipher(key)
	if err != nil {
		t.Fatalf("NewCipher: %v", err)
	}
	client := gitlab.NewClient(fakeGitlabURL, http.DefaultClient)
	hub := realtime.NewHub()
	go hub.Run()
	bus := events.New()
	emailSvc := service.NewEmailService()
	return New(
		db.New(testPool), testPool, hub, bus, emailSvc, nil, nil,
		cipher, client, true,
	)
}

func TestConnectGitlabWorkspace_Success(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v4/user":
			w.Write([]byte(`{"id": 555, "username": "svc-bot", "name": "Service Bot"}`))
		case "/api/v4/projects/42":
			w.Write([]byte(`{"id": 42, "path_with_namespace": "team/app"}`))
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer fake.Close()

	h := buildHandlerWithGitlab(t, fake.URL)
	// Reset connection state in case previous run left one.
	h.Queries.DeleteWorkspaceGitlabConnection(context.Background(), parseUUID(testWorkspaceID))

	body, _ := json.Marshal(map[string]string{
		"project": "42",
		"token":   "glpat-abc",
	})
	req := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/api/workspaces/%s/gitlab/connect", testWorkspaceID), bytes.NewReader(body))
	req.Header.Set("X-User-ID", testUserID)
	// chi URL params — route handlers read the id from the URL via chi.URLParam.
	req = req.WithContext(withWorkspaceURLParam(req.Context(), testWorkspaceID))
	rr := httptest.NewRecorder()

	h.ConnectGitlabWorkspace(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	var got map[string]any
	json.Unmarshal(rr.Body.Bytes(), &got)
	if got["gitlab_project_path"] != "team/app" {
		t.Errorf("gitlab_project_path = %v", got["gitlab_project_path"])
	}
	if got["service_token_encrypted"] != nil || got["pat_encrypted"] != nil {
		t.Errorf("response leaks token field: %+v", got)
	}
}

func TestConnectGitlabWorkspace_BadToken(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"message": "401 Unauthorized"}`))
	}))
	defer fake.Close()

	h := buildHandlerWithGitlab(t, fake.URL)
	h.Queries.DeleteWorkspaceGitlabConnection(context.Background(), parseUUID(testWorkspaceID))

	body, _ := json.Marshal(map[string]string{"project": "42", "token": "bad"})
	req := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/api/workspaces/%s/gitlab/connect", testWorkspaceID), bytes.NewReader(body))
	req.Header.Set("X-User-ID", testUserID)
	req = req.WithContext(withWorkspaceURLParam(req.Context(), testWorkspaceID))
	rr := httptest.NewRecorder()

	h.ConnectGitlabWorkspace(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body = %s", rr.Code, rr.Body.String())
	}
}
```

Note: The test uses a helper `withWorkspaceURLParam` to set the chi URL param on the request context. Define this helper at the top of `gitlab_connection_test.go` (or in a shared test helper file if the existing tests already have one — `grep` the test directory for similar patterns first):

```go
import (
	"context"
	"github.com/go-chi/chi/v5"
)

func withWorkspaceURLParam(ctx context.Context, id string) context.Context {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("workspaceID", id)
	return context.WithValue(ctx, chi.RouteCtxKey, rctx)
}
```

Put this helper once in `gitlab_connection_test.go`; Tasks 10 and 11 reuse it.

- [ ] **Step 9.2: Run tests — expect build failure**

Run: `cd /Users/jimmy.mills/Developer/multica/server && go test ./internal/handler/ -run TestConnectGitlab -v`
Expected: compilation error — `ConnectGitlabWorkspace` undefined.

- [ ] **Step 9.3: Implement the handler**

Create `server/internal/handler/gitlab_connection.go`:

```go
package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/pkg/gitlab"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type connectGitlabRequest struct {
	Project string `json:"project"` // numeric ID or "group/app" path
	Token   string `json:"token"`   // GitLab PAT (api scope)
}

type gitlabConnectionResponse struct {
	WorkspaceID         string `json:"workspace_id"`
	GitlabProjectID     int64  `json:"gitlab_project_id"`
	GitlabProjectPath   string `json:"gitlab_project_path"`
	ServiceTokenUserID  int64  `json:"service_token_user_id"`
	ServiceTokenUsername string `json:"service_token_username"`
	ConnectionStatus    string `json:"connection_status"`
	StatusMessage       string `json:"status_message,omitempty"`
}

// ConnectGitlabWorkspace validates a GitLab service PAT + project reference
// and persists an encrypted workspace_gitlab_connection row on success.
func (h *Handler) ConnectGitlabWorkspace(w http.ResponseWriter, r *http.Request) {
	if !h.GitlabEnabled {
		writeError(w, http.StatusNotFound, "gitlab integration disabled")
		return
	}
	workspaceID := chi.URLParam(r, "workspaceID")
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin"); !ok {
		return
	}

	var req connectGitlabRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Token == "" || req.Project == "" {
		writeError(w, http.StatusBadRequest, "project and token are required")
		return
	}

	// Validate token: who does it belong to?
	user, err := h.Gitlab.CurrentUser(r.Context(), req.Token)
	if err != nil {
		if errors.Is(err, gitlab.ErrUnauthorized) {
			writeError(w, http.StatusUnauthorized, "gitlab token is invalid")
			return
		}
		slog.Error("gitlab CurrentUser failed", "error", err)
		writeError(w, http.StatusBadGateway, "gitlab /user call failed")
		return
	}

	// Validate project: does the token have access?
	project, err := h.Gitlab.GetProject(r.Context(), req.Token, req.Project)
	if err != nil {
		switch {
		case errors.Is(err, gitlab.ErrNotFound):
			writeError(w, http.StatusNotFound, "gitlab project not found or token lacks access")
			return
		case errors.Is(err, gitlab.ErrForbidden):
			writeError(w, http.StatusForbidden, "gitlab token lacks api scope on project")
			return
		default:
			slog.Error("gitlab GetProject failed", "error", err)
			writeError(w, http.StatusBadGateway, "gitlab /projects call failed")
			return
		}
	}

	encrypted, err := h.Secrets.Encrypt([]byte(req.Token))
	if err != nil {
		slog.Error("encrypt gitlab token failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to encrypt token")
		return
	}

	row, err := h.Queries.CreateWorkspaceGitlabConnection(r.Context(), db.CreateWorkspaceGitlabConnectionParams{
		WorkspaceID:           parseUUID(workspaceID),
		GitlabProjectID:       project.ID,
		GitlabProjectPath:     project.PathWithNamespace,
		ServiceTokenEncrypted: encrypted,
		ServiceTokenUserID:    user.ID,
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "gitlab is already connected for this workspace")
			return
		}
		slog.Error("persist workspace_gitlab_connection failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to persist connection")
		return
	}

	writeJSON(w, http.StatusOK, gitlabConnectionResponse{
		WorkspaceID:          uuidToString(row.WorkspaceID),
		GitlabProjectID:      row.GitlabProjectID,
		GitlabProjectPath:    row.GitlabProjectPath,
		ServiceTokenUserID:   row.ServiceTokenUserID,
		ServiceTokenUsername: user.Username,
		ConnectionStatus:     row.ConnectionStatus,
	})
}
```

- [ ] **Step 9.4: Run tests**

Run: `cd /Users/jimmy.mills/Developer/multica/server && go test ./internal/handler/ -run TestConnectGitlab -v`
Expected: Both tests PASS.

- [ ] **Step 9.5: Commit**

```bash
cd /Users/jimmy.mills/Developer/multica
git add server/internal/handler/gitlab_connection.go server/internal/handler/gitlab_connection_test.go
git commit -m "feat(handler): POST workspace gitlab connect with token/project validation"
```

---

## Task 10: Handler — `GET /api/workspaces/{id}/gitlab/connect`

**Files:**
- Modify: `server/internal/handler/gitlab_connection.go`
- Modify: `server/internal/handler/gitlab_connection_test.go`

- [ ] **Step 10.1: Write failing test (append to `gitlab_connection_test.go`)**

```go
func TestGetGitlabWorkspaceConnection_Connected(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v4/user":
			w.Write([]byte(`{"id": 555, "username": "svc-bot"}`))
		case "/api/v4/projects/42":
			w.Write([]byte(`{"id": 42, "path_with_namespace": "team/app"}`))
		}
	}))
	defer fake.Close()

	h := buildHandlerWithGitlab(t, fake.URL)
	h.Queries.DeleteWorkspaceGitlabConnection(context.Background(), parseUUID(testWorkspaceID))

	// Set up one connection via the POST handler.
	body, _ := json.Marshal(map[string]string{"project": "42", "token": "glpat-abc"})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	req.Header.Set("X-User-ID", testUserID)
	req = req.WithContext(withWorkspaceURLParam(req.Context(), testWorkspaceID))
	h.ConnectGitlabWorkspace(httptest.NewRecorder(), req)

	// Now GET.
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.Header.Set("X-User-ID", testUserID)
	req2 = req2.WithContext(withWorkspaceURLParam(req2.Context(), testWorkspaceID))
	rr := httptest.NewRecorder()
	h.GetGitlabWorkspaceConnection(rr, req2)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rr.Code, rr.Body.String())
	}
	var got gitlabConnectionResponse
	json.Unmarshal(rr.Body.Bytes(), &got)
	if got.GitlabProjectPath != "team/app" {
		t.Errorf("got %+v", got)
	}
}

func TestGetGitlabWorkspaceConnection_NotConnected(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer fake.Close()
	h := buildHandlerWithGitlab(t, fake.URL)
	h.Queries.DeleteWorkspaceGitlabConnection(context.Background(), parseUUID(testWorkspaceID))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-User-ID", testUserID)
	req = req.WithContext(withWorkspaceURLParam(req.Context(), testWorkspaceID))
	rr := httptest.NewRecorder()
	h.GetGitlabWorkspaceConnection(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rr.Code)
	}
}
```

- [ ] **Step 10.2: Run test — expect build failure**

Run: `cd /Users/jimmy.mills/Developer/multica/server && go test ./internal/handler/ -run TestGetGitlab -v`
Expected: compilation error.

- [ ] **Step 10.3: Implement**

sqlc maps nullable `TEXT` columns to `pgtype.Text` (`{String string, Valid bool}`) with the `pgx/v5` driver that this repo uses. Append to `server/internal/handler/gitlab_connection.go`:

```go
// GetGitlabWorkspaceConnection returns sanitized connection status (never the token).
func (h *Handler) GetGitlabWorkspaceConnection(w http.ResponseWriter, r *http.Request) {
	if !h.GitlabEnabled {
		writeError(w, http.StatusNotFound, "gitlab integration disabled")
		return
	}
	workspaceID := chi.URLParam(r, "workspaceID")
	if _, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found"); !ok {
		return
	}
	row, err := h.Queries.GetWorkspaceGitlabConnection(r.Context(), parseUUID(workspaceID))
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "gitlab is not connected")
			return
		}
		slog.Error("read workspace_gitlab_connection failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to read connection")
		return
	}

	statusMessage := ""
	if row.StatusMessage.Valid {
		statusMessage = row.StatusMessage.String
	}
	writeJSON(w, http.StatusOK, gitlabConnectionResponse{
		WorkspaceID:        uuidToString(row.WorkspaceID),
		GitlabProjectID:    row.GitlabProjectID,
		GitlabProjectPath:  row.GitlabProjectPath,
		ServiceTokenUserID: row.ServiceTokenUserID,
		ConnectionStatus:   row.ConnectionStatus,
		StatusMessage:      statusMessage,
	})
}
```

- [ ] **Step 10.4: Verify generated struct matches**

Run: `grep -n -A 20 "type WorkspaceGitlabConnection struct" /Users/jimmy.mills/Developer/multica/server/pkg/db/generated/models.go`

Confirm `StatusMessage` is declared as `pgtype.Text`. If sqlc generated a different nullable-text wrapper, swap the `.Valid` / `.String` unwrap for whatever the generated type exposes (e.g. `sql.NullString.String` and `.Valid`). Same pattern — adjust only the field names.

- [ ] **Step 10.5: Run tests**

Run: `cd /Users/jimmy.mills/Developer/multica/server && go test ./internal/handler/ -run TestGetGitlab -v`
Expected: tests PASS.

- [ ] **Step 10.6: Commit**

```bash
cd /Users/jimmy.mills/Developer/multica
git add server/internal/handler/gitlab_connection.go server/internal/handler/gitlab_connection_test.go
git commit -m "feat(handler): GET workspace gitlab connection status"
```

---

## Task 11: Handler — `DELETE /api/workspaces/{id}/gitlab/connect`

**Files:**
- Modify: `server/internal/handler/gitlab_connection.go`
- Modify: `server/internal/handler/gitlab_connection_test.go`

- [ ] **Step 11.1: Write failing test (append)**

```go
func TestDisconnectGitlabWorkspace_Success(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v4/user":
			w.Write([]byte(`{"id": 1, "username": "svc"}`))
		case "/api/v4/projects/1":
			w.Write([]byte(`{"id": 1, "path_with_namespace": "g/a"}`))
		}
	}))
	defer fake.Close()

	h := buildHandlerWithGitlab(t, fake.URL)
	h.Queries.DeleteWorkspaceGitlabConnection(context.Background(), parseUUID(testWorkspaceID))

	// Create one to delete.
	body, _ := json.Marshal(map[string]string{"project": "1", "token": "glpat-x"})
	postReq := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	postReq.Header.Set("X-User-ID", testUserID)
	postReq = postReq.WithContext(withWorkspaceURLParam(postReq.Context(), testWorkspaceID))
	h.ConnectGitlabWorkspace(httptest.NewRecorder(), postReq)

	// DELETE.
	delReq := httptest.NewRequest(http.MethodDelete, "/", nil)
	delReq.Header.Set("X-User-ID", testUserID)
	delReq = delReq.WithContext(withWorkspaceURLParam(delReq.Context(), testWorkspaceID))
	rr := httptest.NewRecorder()
	h.DisconnectGitlabWorkspace(rr, delReq)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("status = %d body = %s", rr.Code, rr.Body.String())
	}

	// GET should now 404.
	getReq := httptest.NewRequest(http.MethodGet, "/", nil)
	getReq.Header.Set("X-User-ID", testUserID)
	getReq = getReq.WithContext(withWorkspaceURLParam(getReq.Context(), testWorkspaceID))
	rr2 := httptest.NewRecorder()
	h.GetGitlabWorkspaceConnection(rr2, getReq)
	if rr2.Code != http.StatusNotFound {
		t.Fatalf("after delete, GET should 404, got %d", rr2.Code)
	}
}
```

- [ ] **Step 11.2: Run test — expect build failure**

Run: `cd /Users/jimmy.mills/Developer/multica/server && go test ./internal/handler/ -run TestDisconnectGitlab -v`
Expected: compilation error.

- [ ] **Step 11.3: Implement**

Append to `server/internal/handler/gitlab_connection.go`:

```go
// DisconnectGitlabWorkspace removes the workspace's GitLab connection.
// Note: Phase 2 will extend this to also delete the webhook in GitLab.
func (h *Handler) DisconnectGitlabWorkspace(w http.ResponseWriter, r *http.Request) {
	if !h.GitlabEnabled {
		writeError(w, http.StatusNotFound, "gitlab integration disabled")
		return
	}
	workspaceID := chi.URLParam(r, "workspaceID")
	if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin"); !ok {
		return
	}
	if err := h.Queries.DeleteWorkspaceGitlabConnection(r.Context(), parseUUID(workspaceID)); err != nil {
		slog.Error("delete workspace_gitlab_connection failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to disconnect")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
```

- [ ] **Step 11.4: Run tests**

Run: `cd /Users/jimmy.mills/Developer/multica/server && go test ./internal/handler/ -run TestDisconnectGitlab -v`
Expected: PASS.

- [ ] **Step 11.5: Run the full handler test suite to ensure no regressions**

Run: `cd /Users/jimmy.mills/Developer/multica/server && go test ./internal/handler/ -v`
Expected: all existing tests still PASS.

- [ ] **Step 11.6: Commit**

```bash
cd /Users/jimmy.mills/Developer/multica
git add server/internal/handler/gitlab_connection.go server/internal/handler/gitlab_connection_test.go
git commit -m "feat(handler): DELETE workspace gitlab connection"
```

---

## Task 12: Router — mount the new endpoints

**Files:**
- Modify: `server/cmd/server/router.go`

- [ ] **Step 12.1: Find the workspace route group**

Run: `grep -n "api/workspaces" /Users/jimmy.mills/Developer/multica/server/cmd/server/router.go`

Locate the `/api/workspaces` route group. New endpoints mount underneath it at `/{workspaceID}/gitlab/connect`, scoped by the existing workspace-membership middleware.

- [ ] **Step 12.2: Add the routes**

Inside the workspace route group:

```go
r.Route("/{workspaceID}/gitlab", func(r chi.Router) {
    r.Post("/connect", h.ConnectGitlabWorkspace)
    r.Get("/connect", h.GetGitlabWorkspaceConnection)
    r.Delete("/connect", h.DisconnectGitlabWorkspace)
})
```

Place this inside whatever `Route("/workspaces", ...)` group currently exists, alongside other per-workspace sub-routes.

- [ ] **Step 12.3: Verify the build**

Run: `cd /Users/jimmy.mills/Developer/multica/server && go build ./...`
Expected: builds cleanly.

- [ ] **Step 12.4: Smoke test with curl**

Terminal 1:
```bash
cd /Users/jimmy.mills/Developer/multica
MULTICA_GITLAB_ENABLED=true make server
```

Terminal 2 — expect 401 (no auth session):
```bash
curl -i -X GET http://localhost:8080/api/workspaces/<some-id>/gitlab/connect
```

Stop the server.

- [ ] **Step 12.5: Commit**

```bash
cd /Users/jimmy.mills/Developer/multica
git add server/cmd/server/router.go
git commit -m "feat(server): mount /api/workspaces/{id}/gitlab/connect endpoints"
```

---

## Task 13: Frontend core — GitLab API client + queries + mutations

**Files:**
- Create: `packages/core/gitlab/types.ts`
- Create: `packages/core/gitlab/api.ts`
- Create: `packages/core/gitlab/queries.ts`
- Create: `packages/core/gitlab/mutations.ts`
- Create: `packages/core/gitlab/mutations.test.ts`

- [ ] **Step 13.1: Locate the existing api client module**

Run: `grep -rn "export const api" /Users/jimmy.mills/Developer/multica/packages/core | head -5`

Identify how existing domains (e.g. `packages/core/issues/api.ts`) define their api methods and what module they import the HTTP client from.

- [ ] **Step 13.2: Write types**

Create `packages/core/gitlab/types.ts`:

```ts
export interface GitlabConnection {
  workspace_id: string;
  gitlab_project_id: number;
  gitlab_project_path: string;
  service_token_user_id: number;
  service_token_username?: string;
  connection_status: "connecting" | "connected" | "error";
  status_message?: string;
}

export interface ConnectGitlabInput {
  project: string; // numeric id or "group/project"
  token: string;
}
```

- [ ] **Step 13.3: Write API methods**

Create `packages/core/gitlab/api.ts` (adapt the import path for the shared http client to match what `packages/core/issues/api.ts` uses):

```ts
import { apiFetch } from "../platform/api-client";
import type { ConnectGitlabInput, GitlabConnection } from "./types";

export const gitlabApi = {
  getWorkspaceConnection: (wsId: string): Promise<GitlabConnection> =>
    apiFetch(`/api/workspaces/${wsId}/gitlab/connect`, { method: "GET" }),

  connectWorkspace: (wsId: string, input: ConnectGitlabInput): Promise<GitlabConnection> =>
    apiFetch(`/api/workspaces/${wsId}/gitlab/connect`, {
      method: "POST",
      body: JSON.stringify(input),
    }),

  disconnectWorkspace: (wsId: string): Promise<void> =>
    apiFetch(`/api/workspaces/${wsId}/gitlab/connect`, { method: "DELETE" }),
};
```

If the existing `apiFetch` has a different signature (e.g. auto-serializes the body), match that pattern — check `packages/core/issues/api.ts` for the canonical example.

- [ ] **Step 13.4: Write query hook**

Create `packages/core/gitlab/queries.ts`:

```ts
import { useQuery, type UseQueryOptions } from "@tanstack/react-query";
import { gitlabApi } from "./api";
import type { GitlabConnection } from "./types";

export const gitlabKeys = {
  all: (wsId: string) => ["gitlab", "workspace", wsId] as const,
  connection: (wsId: string) => [...gitlabKeys.all(wsId), "connection"] as const,
};

export function workspaceGitlabConnectionOptions(wsId: string) {
  return {
    queryKey: gitlabKeys.connection(wsId),
    queryFn: () => gitlabApi.getWorkspaceConnection(wsId),
    // 404 is "not connected" — handled by the hook consumer, not a retry loop.
    retry: false,
  } satisfies UseQueryOptions<GitlabConnection>;
}

export function useWorkspaceGitlabConnection(wsId: string) {
  return useQuery(workspaceGitlabConnectionOptions(wsId));
}
```

- [ ] **Step 13.5: Write mutation hooks**

Create `packages/core/gitlab/mutations.ts`:

```ts
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { gitlabApi } from "./api";
import { gitlabKeys } from "./queries";
import type { ConnectGitlabInput } from "./types";

export function useConnectWorkspaceGitlabMutation(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: ConnectGitlabInput) => gitlabApi.connectWorkspace(wsId, input),
    onSuccess: (data) => {
      qc.setQueryData(gitlabKeys.connection(wsId), data);
    },
  });
}

export function useDisconnectWorkspaceGitlabMutation(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => gitlabApi.disconnectWorkspace(wsId),
    onSuccess: () => {
      qc.removeQueries({ queryKey: gitlabKeys.connection(wsId) });
    },
  });
}
```

- [ ] **Step 13.6: Write failing test**

Create `packages/core/gitlab/mutations.test.ts`:

```ts
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook, act } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useConnectWorkspaceGitlabMutation, useDisconnectWorkspaceGitlabMutation } from "./mutations";
import { gitlabApi } from "./api";

vi.mock("./api", () => ({
  gitlabApi: {
    getWorkspaceConnection: vi.fn(),
    connectWorkspace: vi.fn(),
    disconnectWorkspace: vi.fn(),
  },
}));

function wrapper() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return ({ children }: { children: React.ReactNode }) => (
    <QueryClientProvider client={qc}>{children}</QueryClientProvider>
  );
}

describe("gitlab mutations", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("connectWorkspace calls the api and caches the response", async () => {
    const connection = {
      workspace_id: "ws-1",
      gitlab_project_id: 1,
      gitlab_project_path: "g/a",
      service_token_user_id: 1,
      connection_status: "connected" as const,
    };
    (gitlabApi.connectWorkspace as any).mockResolvedValue(connection);

    const { result } = renderHook(() => useConnectWorkspaceGitlabMutation("ws-1"), { wrapper: wrapper() });
    await act(async () => {
      await result.current.mutateAsync({ project: "1", token: "t" });
    });
    expect(gitlabApi.connectWorkspace).toHaveBeenCalledWith("ws-1", { project: "1", token: "t" });
    expect(result.current.data).toEqual(connection);
  });

  it("disconnectWorkspace calls the api", async () => {
    (gitlabApi.disconnectWorkspace as any).mockResolvedValue(undefined);
    const { result } = renderHook(() => useDisconnectWorkspaceGitlabMutation("ws-1"), { wrapper: wrapper() });
    await act(async () => {
      await result.current.mutateAsync();
    });
    expect(gitlabApi.disconnectWorkspace).toHaveBeenCalledWith("ws-1");
  });
});
```

- [ ] **Step 13.7: Run tests**

Run: `cd /Users/jimmy.mills/Developer/multica && pnpm --filter @multica/core exec vitest run gitlab/mutations.test.ts`
Expected: PASS.

- [ ] **Step 13.8: Commit**

```bash
cd /Users/jimmy.mills/Developer/multica
git add packages/core/gitlab/
git commit -m "feat(core): add gitlab api client + query/mutation hooks"
```

---

## Task 14: Frontend view — `ConnectGitlabPage`

**Files:**
- Create: `packages/views/workspace/settings/gitlab/connect-gitlab-page.tsx`
- Create: `packages/views/workspace/settings/gitlab/connect-gitlab-page.test.tsx`

- [ ] **Step 14.1: Locate existing settings pages**

Run: `ls /Users/jimmy.mills/Developer/multica/packages/views/workspace/settings/ 2>/dev/null || find /Users/jimmy.mills/Developer/multica/packages/views -name "*settings*" | head -20`

Study the nearest existing settings page's structure and imports — match it exactly (headings, spacing, form components from `packages/ui/`).

- [ ] **Step 14.2: Write failing test**

Create `packages/views/workspace/settings/gitlab/connect-gitlab-page.test.tsx`:

```tsx
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { ConnectGitlabPage } from "./connect-gitlab-page";

vi.mock("@multica/core/gitlab/api", () => ({
  gitlabApi: {
    getWorkspaceConnection: vi.fn(),
    connectWorkspace: vi.fn(),
    disconnectWorkspace: vi.fn(),
  },
}));

import { gitlabApi } from "@multica/core/gitlab/api";

function renderPage(wsId = "ws-1") {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <ConnectGitlabPage workspaceId={wsId} />
    </QueryClientProvider>,
  );
}

describe("ConnectGitlabPage", () => {
  it("shows the connect form when not connected (404 from GET)", async () => {
    (gitlabApi.getWorkspaceConnection as any).mockRejectedValue({ status: 404 });
    renderPage();
    expect(await screen.findByRole("heading", { name: /connect gitlab/i })).toBeInTheDocument();
    expect(screen.getByLabelText(/project/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/token/i)).toBeInTheDocument();
  });

  it("submits the form and shows connected state", async () => {
    (gitlabApi.getWorkspaceConnection as any).mockRejectedValue({ status: 404 });
    (gitlabApi.connectWorkspace as any).mockResolvedValue({
      workspace_id: "ws-1",
      gitlab_project_id: 7,
      gitlab_project_path: "team/app",
      service_token_user_id: 1,
      connection_status: "connected",
    });
    renderPage();
    await userEvent.type(await screen.findByLabelText(/project/i), "team/app");
    await userEvent.type(screen.getByLabelText(/token/i), "glpat-abc");
    await userEvent.click(screen.getByRole("button", { name: /connect/i }));

    await waitFor(() => {
      expect(screen.getByText(/team\/app/)).toBeInTheDocument();
      expect(screen.getByRole("button", { name: /disconnect/i })).toBeInTheDocument();
    });
  });

  it("shows connected state when already connected", async () => {
    (gitlabApi.getWorkspaceConnection as any).mockResolvedValue({
      workspace_id: "ws-1",
      gitlab_project_id: 9,
      gitlab_project_path: "group/repo",
      service_token_user_id: 1,
      connection_status: "connected",
    });
    renderPage();
    expect(await screen.findByText(/group\/repo/)).toBeInTheDocument();
  });
});
```

- [ ] **Step 14.3: Implement**

Create `packages/views/workspace/settings/gitlab/connect-gitlab-page.tsx`:

```tsx
import {
  useWorkspaceGitlabConnection,
} from "@multica/core/gitlab/queries";
import {
  useConnectWorkspaceGitlabMutation,
  useDisconnectWorkspaceGitlabMutation,
} from "@multica/core/gitlab/mutations";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { useState, type FormEvent } from "react";

interface Props {
  workspaceId: string;
}

export function ConnectGitlabPage({ workspaceId }: Props) {
  const { data, error, isLoading } = useWorkspaceGitlabConnection(workspaceId);
  const connectMu = useConnectWorkspaceGitlabMutation(workspaceId);
  const disconnectMu = useDisconnectWorkspaceGitlabMutation(workspaceId);

  const [project, setProject] = useState("");
  const [token, setToken] = useState("");

  if (isLoading) {
    return <div className="text-muted-foreground">Loading…</div>;
  }

  // Treat 404 as "not connected". Any other error displays as inline error.
  const notConnected = !data && (error as { status?: number } | null)?.status === 404;
  const otherError = !data && !notConnected && error != null;

  if (data && data.connection_status === "connected") {
    return (
      <div className="space-y-4">
        <h2 className="text-xl font-semibold">GitLab</h2>
        <div className="rounded border p-4 space-y-2">
          <div>
            <span className="text-muted-foreground">Project: </span>
            <span className="font-medium">{data.gitlab_project_path}</span>
          </div>
          <div className="text-muted-foreground text-sm">
            Service account user id: {data.service_token_user_id}
          </div>
          <Button
            variant="destructive"
            disabled={disconnectMu.isPending}
            onClick={() => disconnectMu.mutate()}
          >
            {disconnectMu.isPending ? "Disconnecting…" : "Disconnect"}
          </Button>
        </div>
      </div>
    );
  }

  const handleSubmit = (e: FormEvent) => {
    e.preventDefault();
    connectMu.mutate({ project, token });
  };

  return (
    <form className="space-y-4" onSubmit={handleSubmit}>
      <h2 className="text-xl font-semibold">Connect GitLab</h2>
      {otherError ? (
        <div className="text-destructive text-sm">Failed to load connection status.</div>
      ) : null}
      <div className="space-y-2">
        <Label htmlFor="project">Project</Label>
        <Input
          id="project"
          value={project}
          onChange={(e) => setProject(e.target.value)}
          placeholder="group/project or numeric ID"
          required
        />
      </div>
      <div className="space-y-2">
        <Label htmlFor="token">Service access token</Label>
        <Input
          id="token"
          type="password"
          value={token}
          onChange={(e) => setToken(e.target.value)}
          placeholder="glpat-…"
          required
        />
      </div>
      {connectMu.isError ? (
        <div className="text-destructive text-sm">
          {(connectMu.error as Error).message || "Connection failed"}
        </div>
      ) : null}
      <Button type="submit" disabled={connectMu.isPending || !project || !token}>
        {connectMu.isPending ? "Connecting…" : "Connect"}
      </Button>
    </form>
  );
}
```

- [ ] **Step 14.4: Run tests**

Run: `cd /Users/jimmy.mills/Developer/multica && pnpm --filter @multica/views exec vitest run workspace/settings/gitlab/connect-gitlab-page.test.tsx`
Expected: PASS.

- [ ] **Step 14.5: Commit**

```bash
cd /Users/jimmy.mills/Developer/multica
git add packages/views/workspace/settings/gitlab/
git commit -m "feat(views): ConnectGitlabPage with form + connected states"
```

---

## Task 15: Web app — Next.js route wrapper

**Files:**
- Create: `apps/web/app/[workspaceSlug]/(dashboard)/settings/gitlab/page.tsx`

- [ ] **Step 15.1: Find the settings directory**

Run: `ls /Users/jimmy.mills/Developer/multica/apps/web/app/\[workspaceSlug\]/\(dashboard\)/settings/ 2>/dev/null`

Confirm the path exists. If settings aren't under `(dashboard)`, adjust the destination path to match the real location of existing settings pages.

- [ ] **Step 15.2: Create the route file**

```tsx
"use client";

import { useParams } from "next/navigation";
import { useWorkspaceIdFromSlug } from "@multica/web/platform/workspace";
import { ConnectGitlabPage } from "@multica/views/workspace/settings/gitlab/connect-gitlab-page";

export default function SettingsGitlabPage() {
  const { workspaceSlug } = useParams<{ workspaceSlug: string }>();
  const wsId = useWorkspaceIdFromSlug(workspaceSlug);
  if (!wsId) return null;
  return <ConnectGitlabPage workspaceId={wsId} />;
}
```

The hook name `useWorkspaceIdFromSlug` is what the apps/web platform layer typically exposes. If it's named differently, match the pattern used by another settings page (e.g. `settings/general/page.tsx`).

- [ ] **Step 15.3: Verify build**

Run: `cd /Users/jimmy.mills/Developer/multica && pnpm --filter @multica/web build`
Expected: build succeeds.

- [ ] **Step 15.4: Commit**

```bash
cd /Users/jimmy.mills/Developer/multica
git add apps/web/app/\[workspaceSlug\]/\(dashboard\)/settings/gitlab/page.tsx
git commit -m "feat(web): route /:ws/settings/gitlab to ConnectGitlabPage"
```

---

## Task 16: Desktop app — React Router route

**Files:**
- Modify: `apps/desktop/src/renderer/src/routes.tsx`
- Create: `apps/desktop/src/renderer/src/pages/settings-gitlab.tsx`

- [ ] **Step 16.1: Read existing desktop routes**

Run: `cat /Users/jimmy.mills/Developer/multica/apps/desktop/src/renderer/src/routes.tsx`

Identify the settings route pattern and where to add the new entry.

- [ ] **Step 16.2: Create page wrapper**

Create `apps/desktop/src/renderer/src/pages/settings-gitlab.tsx`:

```tsx
import { useWorkspaceId } from "@multica/core/platform/workspace";
import { ConnectGitlabPage } from "@multica/views/workspace/settings/gitlab/connect-gitlab-page";

export function SettingsGitlabPage() {
  const wsId = useWorkspaceId();
  return <ConnectGitlabPage workspaceId={wsId} />;
}
```

Match the hook name to the one other desktop settings pages use — adjust if the desktop uses a different import.

- [ ] **Step 16.3: Add the route**

Inside `routes.tsx`, add a `<Route path="settings/gitlab" element={<SettingsGitlabPage />} />` entry in the same group as other settings routes.

- [ ] **Step 16.4: Verify the desktop compiles**

Run: `cd /Users/jimmy.mills/Developer/multica && pnpm --filter @multica/desktop build`
Expected: compiles.

- [ ] **Step 16.5: Commit**

```bash
cd /Users/jimmy.mills/Developer/multica
git add apps/desktop/src/renderer/src/routes.tsx apps/desktop/src/renderer/src/pages/settings-gitlab.tsx
git commit -m "feat(desktop): route settings/gitlab to ConnectGitlabPage"
```

---

## Task 17: Settings nav — add "GitLab" entry (feature-flag gated)

**Why this task is structured this way:** `packages/views/` must not read framework-specific env vars (see CLAUDE.md — views is platform-agnostic). The feature flag lives in each app's platform layer; the nav component receives `showGitlab` as a prop.

**Files:**
- Modify: the settings nav component (identify in Step 17.1)
- Modify: the web settings layout (Next.js)
- Modify: the desktop settings layout (Electron)
- Modify: `apps/web/.env.example`
- Modify: `apps/desktop/.env.example`

- [ ] **Step 17.1: Locate the settings nav and its two app-layer consumers**

Run: `grep -rln "general\|members\|billing" /Users/jimmy.mills/Developer/multica/packages/views/workspace/settings/ 2>/dev/null | head`

Identify the nav component. Then find where each app renders settings chrome:

Run: `grep -rln "WorkspaceSettingsNav\|SettingsNav\|<SettingsLayout" /Users/jimmy.mills/Developer/multica/apps 2>/dev/null`

- [ ] **Step 17.2: Add a `showGitlab` prop to the nav component**

In the nav component, add a new prop:

```ts
interface Props {
  // …existing props…
  showGitlab?: boolean;
}
```

Render the existing entries as before, plus (conditionally) a new entry titled "GitLab" pointing at the relative path `settings/gitlab`. Match the visual style and component type (`AppLink` or whatever pattern the sibling entries use — do not invent a new one).

```tsx
{showGitlab ? (
  <NavEntry to="settings/gitlab" label="GitLab" />
) : null}
```

Replace `NavEntry` / `to` with whatever the existing entries use.

- [ ] **Step 17.3: Wire the flag in the web app**

In the web app's settings layout (identified in 17.1), pass `showGitlab` using a Next.js public env var:

```tsx
<WorkspaceSettingsNav
  // …existing props…
  showGitlab={process.env.NEXT_PUBLIC_GITLAB_ENABLED === "true"}
/>
```

Append to `apps/web/.env.example`:

```
# Feature flag: mirrors server-side MULTICA_GITLAB_ENABLED. When true the
# workspace settings nav shows a "GitLab" entry.
NEXT_PUBLIC_GITLAB_ENABLED=false
```

- [ ] **Step 17.4: Wire the flag in the desktop app**

In the desktop app's settings layout, pass `showGitlab` using Vite's client env var:

```tsx
<WorkspaceSettingsNav
  // …existing props…
  showGitlab={import.meta.env.VITE_GITLAB_ENABLED === "true"}
/>
```

Append to `apps/desktop/.env.example`:

```
# Feature flag: mirrors server-side MULTICA_GITLAB_ENABLED. When true the
# workspace settings nav shows a "GitLab" entry.
VITE_GITLAB_ENABLED=false
```

- [ ] **Step 17.5: Smoke test in web dev**

```bash
cd /Users/jimmy.mills/Developer/multica
NEXT_PUBLIC_GITLAB_ENABLED=true pnpm dev:web
```

Open a workspace → Settings → confirm the "GitLab" entry renders. Navigate to it — the connect form should render (clicking submit with an invalid token should return an error from your running backend, not a client-side crash).

Stop the dev server.

- [ ] **Step 17.6: Commit**

```bash
cd /Users/jimmy.mills/Developer/multica
git add packages/views/workspace/settings/ apps/web/.env.example apps/desktop/.env.example apps/web apps/desktop
git commit -m "feat(views+apps): gitlab settings nav entry behind per-app feature flag"
```

---

## Task 18: Full verification — `make check`

**Files:** (none — verification only)

- [ ] **Step 18.1: Run the full check pipeline**

Run: `cd /Users/jimmy.mills/Developer/multica && make check`

Expected: typecheck + vitest + go test + e2e all PASS.

- [ ] **Step 18.2: If a step fails**

Read the error output. The likely suspects:
- `pgtype.Text` unwrap in Task 10 didn't match the real sqlc output — read `server/pkg/db/generated/models.go` and adjust.
- Chi URL param name mismatch (`workspaceID` vs `workspaceId`) — check the existing router.go pattern.
- `apiFetch` helper signature differs — match the existing pattern in `packages/core/issues/api.ts`.

Fix, re-run `make check`. Do not mark Phase 1 complete until it's green.

- [ ] **Step 18.3: Final commit (if any fixes were needed)**

```bash
cd /Users/jimmy.mills/Developer/multica
git status
git add -p   # review changes
git commit -m "fix(gitlab-phase-1): <what you fixed>"
```

---

## Out of scope for Phase 1 (handled in later phases)

- Per-user PAT connect flow + UI banner (Phase 3).
- Webhook receiver endpoint + worker (Phase 2).
- Initial sync worker pulling issues/comments/labels (Phase 2).
- Reconciliation loop (Phase 2).
- Any issue read/write handler changes (Phase 2 for reads, Phase 3 for writes).
- Agent and autopilot re-pointing (Phase 4).
- Deleting the old issue-related tables and code (Phase 5).

## Definition of done

Phase 1 is complete when, on a clean checkout with `MULTICA_GITLAB_ENABLED=true` and `NEXT_PUBLIC_GITLAB_ENABLED=true`:

1. A workspace admin opens `Settings → GitLab`, sees the connect form.
2. They paste a valid GitLab PAT + project identifier and submit.
3. The server validates both against gitlab.com, encrypts the PAT, persists the row.
4. The page now shows the connected state with project path + disconnect button.
5. Clicking "Disconnect" removes the row; the form reappears.
6. `make check` passes cleanly.
