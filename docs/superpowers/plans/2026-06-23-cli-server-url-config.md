# CLI Server URL Config Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a dedicated `cli_server_url` runtime config field so the CLI setup commands shown in the "Add a computer" dialog (and related onboarding/landing CLI sections) point to the backend path (`/multica-backend`) instead of the web path (`/multica-web`).

**Architecture:** The backend's `/api/config` endpoint currently returns a single `server_url` used by the web app. We add a new optional `cli_server_url` field that the frontend uses when generating copy-paste CLI commands, falling back to `server_url` when absent. This keeps the web app's API/WS routing unchanged while letting operators specify a separate public backend address for CLI/daemon registration.

**Tech Stack:** Go (Chi handler), TypeScript/React (Zustand config store, `@multica/core` API client, `@multica/views` components, `apps/web` landing component).

---

## File Structure

- `server/internal/handler/config.go` — add `CliServerURL` to `AppConfig`; read `CLI_SERVER_URL` env var with fallback to `SERVER_URL`.
- `server/internal/handler/config_test.go` — add assertions for `CliServerURL` behavior.
- `packages/core/config/index.ts` — add `cliServerUrl` state + setter.
- `packages/core/api/client.ts` — add `cli_server_url` to `getConfig()` return type.
- `packages/core/platform/auth-initializer.tsx` — hydrate `cliServerUrl` from config response.
- `packages/views/runtimes/components/connect-remote-dialog.tsx` — use `cliServerUrl` for the token command.
- `packages/views/onboarding/steps/cli-install-instructions.tsx` — use `cliServerUrl` for the token command.
- `apps/web/features/landing/components/download/cli-section.tsx` — use `cliServerUrl` for the token command.

---

## Task 1: Backend — expose `cli_server_url` in `/api/config`

**Files:**
- Modify: `server/internal/handler/config.go`
- Test: `server/internal/handler/config_test.go`

- [ ] **Step 1: Write the failing test**

In `server/internal/handler/config_test.go`, add a new test that asserts `CliServerURL` is returned when set and falls back to `ServerURL` when unset.

```go
func TestGetConfigCliServerURL(t *testing.T) {
	origStorage := testHandler.Storage
	testHandler.Storage = &mockStorage{}
	defer func() { testHandler.Storage = origStorage }()

	t.Run("uses CLI_SERVER_URL when set", func(t *testing.T) {
		t.Setenv("SERVER_URL", "https://zgsmtest.cn:30443/multica-web")
		t.Setenv("CLI_SERVER_URL", "https://zgsmtest.cn:30443/multica-backend")

		req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
		w := httptest.NewRecorder()
		testHandler.GetConfig(w, req)

		var cfg AppConfig
		if err := json.Unmarshal(w.Body.Bytes(), &cfg); err != nil {
			t.Fatalf("decode config: %v", err)
		}
		if cfg.ServerURL != "https://zgsmtest.cn:30443/multica-web" {
			t.Fatalf("server_url: want web URL, got %q", cfg.ServerURL)
		}
		if cfg.CliServerURL != "https://zgsmtest.cn:30443/multica-backend" {
			t.Fatalf("cli_server_url: want backend URL, got %q", cfg.CliServerURL)
		}
	})

	t.Run("falls back to SERVER_URL when CLI_SERVER_URL unset", func(t *testing.T) {
		t.Setenv("SERVER_URL", "https://api.example.com")
		t.Setenv("CLI_SERVER_URL", "")

		req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
		w := httptest.NewRecorder()
		testHandler.GetConfig(w, req)

		var cfg AppConfig
		if err := json.Unmarshal(w.Body.Bytes(), &cfg); err != nil {
			t.Fatalf("decode config: %v", err)
		}
		if cfg.CliServerURL != "https://api.example.com" {
			t.Fatalf("cli_server_url: want fallback, got %q", cfg.CliServerURL)
		}
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd server && go test ./internal/handler/ -run TestGetConfigCliServerURL -v`

Expected: FAIL — `CliServerURL` undefined or wrong value.

- [ ] **Step 3: Implement backend change**

Modify `server/internal/handler/config.go`:

1. Add the new field to `AppConfig`:

```go
	// CliServerURL is the public HTTP(S) address exposed to CLI setup commands.
	// Defaults to ServerURL so existing deployments keep working.
	CliServerURL string `json:"cli_server_url,omitempty"`
```

2. Populate it in `GetConfig` after the `ServerURL` block:

```go
	config.ServerURL = os.Getenv("SERVER_URL")
	if config.ServerURL == "" {
		config.ServerURL = os.Getenv("REMOTE_API_URL")
	}
	config.CliServerURL = os.Getenv("CLI_SERVER_URL")
	if config.CliServerURL == "" {
		config.CliServerURL = config.ServerURL
	}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd server && go test ./internal/handler/ -run TestGetConfigCliServerURL -v`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/internal/handler/config.go server/internal/handler/config_test.go
git commit -m "feat(config): expose cli_server_url for CLI setup commands"
```

---

## Task 2: Frontend core — add `cliServerUrl` to config store and API client

**Files:**
- Modify: `packages/core/config/index.ts`
- Modify: `packages/core/api/client.ts`
- Modify: `packages/core/platform/auth-initializer.tsx`

- [ ] **Step 1: Add `cliServerUrl` to config store**

Modify `packages/core/config/index.ts`:

```ts
interface ConfigState {
  cdnDomain: string;
  serverUrl: string;
  cliServerUrl: string;
  allowSignup: boolean;
  googleClientId: string;
  appEnv: string;
  casdoorEnabled: boolean;
  casdoorLoginUrl: string;
  setCdnDomain: (domain: string) => void;
  setServerUrl: (url: string) => void;
  setCliServerUrl: (url: string) => void;
  setAuthConfig: (config: {
    allowSignup: boolean;
    googleClientId?: string;
    appEnv?: string;
    casdoorEnabled?: boolean;
    casdoorLoginUrl?: string;
  }) => void;
}

export const configStore = createStore<ConfigState>((set) => ({
  cdnDomain: "",
  serverUrl: "",
  cliServerUrl: "",
  allowSignup: true,
  googleClientId: "",
  appEnv: "",
  casdoorEnabled: false,
  casdoorLoginUrl: "",
  setCdnDomain: (domain) => set({ cdnDomain: domain }),
  setServerUrl: (url) => set({ serverUrl: url }),
  setCliServerUrl: (url) => set({ cliServerUrl: url }),
  setAuthConfig: ({ allowSignup, googleClientId = "", appEnv = "", casdoorEnabled = false, casdoorLoginUrl = "" }) =>
    set({ allowSignup, googleClientId, appEnv, casdoorEnabled, casdoorLoginUrl }),
}));
```

- [ ] **Step 2: Update API client return type**

Modify `packages/core/api/client.ts` in `getConfig()`:

```ts
  async getConfig(): Promise<{
    cdn_domain: string;
    allow_signup: boolean;
    google_client_id?: string;
    app_env?: string;
    casdoor_enabled?: boolean;
    casdoor_login_url?: string;
    server_url?: string;
    cli_server_url?: string;
    posthog_key?: string;
    posthog_host?: string;
    analytics_environment?: string;
  }> {
    return this.fetch("/api/config");
  }
```

- [ ] **Step 3: Hydrate `cliServerUrl` from config response**

Modify `packages/core/platform/auth-initializer.tsx`:

```ts
        if (cfg.server_url) configStore.getState().setServerUrl(cfg.server_url);
        if (cfg.cli_server_url) configStore.getState().setCliServerUrl(cfg.cli_server_url);
```

- [ ] **Step 4: Run TypeScript check for core**

Run: `pnpm --filter @multica/core typecheck`

Expected: PASS (no new errors).

- [ ] **Step 5: Commit**

```bash
git add packages/core/config/index.ts packages/core/api/client.ts packages/core/platform/auth-initializer.tsx
git commit -m "feat(config): add cliServerUrl to runtime config store"
```

---

## Task 3: Shared views — use `cliServerUrl` in CLI command generators

**Files:**
- Modify: `packages/views/runtimes/components/connect-remote-dialog.tsx`
- Modify: `packages/views/onboarding/steps/cli-install-instructions.tsx`

- [ ] **Step 1: Update connect-remote-dialog**

Modify `packages/views/runtimes/components/connect-remote-dialog.tsx`:

```ts
function InstructionsStep({ onClose }: { onClose: () => void }) {
  const { t } = useT("runtimes");
  const cliServerUrl = useConfigStore((s) => s.cliServerUrl);
  const serverUrl = useConfigStore((s) => s.serverUrl);
  const tokenCmd = makeTokenCmd(cliServerUrl || serverUrl);
  // ... rest unchanged
}
```

- [ ] **Step 2: Update cli-install-instructions**

Modify `packages/views/onboarding/steps/cli-install-instructions.tsx`:

```ts
export function CliInstallInstructions() {
  const { t } = useT("onboarding");
  const cliServerUrl = useConfigStore((s) => s.cliServerUrl);
  const serverUrl = useConfigStore((s) => s.serverUrl);
  const tokenCmd = makeTokenCmd(cliServerUrl || serverUrl);
  // ... rest unchanged
}
```

- [ ] **Step 3: Run TypeScript check for views**

Run: `pnpm --filter @multica/views typecheck`

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add packages/views/runtimes/components/connect-remote-dialog.tsx packages/views/onboarding/steps/cli-install-instructions.tsx
git commit -m "feat(views): use cliServerUrl in CLI setup instructions"
```

---

## Task 4: Web landing page — use `cliServerUrl` in CLI section

**Files:**
- Modify: `apps/web/features/landing/components/download/cli-section.tsx`

- [ ] **Step 1: Update landing CLI section**

Modify `apps/web/features/landing/components/download/cli-section.tsx`:

```ts
export function CliSection() {
  const { t } = useLocale();
  const d = t.download.cli;
  const cliServerUrl = useConfigStore((s) => s.cliServerUrl);
  const serverUrl = useConfigStore((s) => s.serverUrl);
  const tokenCmd = makeTokenCmd(cliServerUrl || serverUrl);
  // ... rest unchanged
}
```

- [ ] **Step 2: Run TypeScript check for web**

Run: `pnpm --filter @multica/web typecheck`

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add apps/web/features/landing/components/download/cli-section.tsx
git commit -m "feat(web): use cliServerUrl in landing CLI section"
```

---

## Task 5: Full verification

- [ ] **Step 1: Run the full verification pipeline**

Run: `make check`

Expected: typecheck, unit tests, and Go tests all pass.

- [ ] **Step 2: Commit any remaining changes**

If `make check` produced no changes, this step is a no-op.

---

## Deployment Note

To fix the displayed URL in the screenshot, set the environment variable:

```bash
CLI_SERVER_URL=https://zgsmtest.cn:30443/multica-backend
```

`SERVER_URL` can remain `https://zgsmtest.cn:30443/multica-web` if that URL is still used elsewhere by the web app.

---

## Self-Review

1. **Spec coverage:** The user's request is to change the displayed `server_url` in the "Add a computer" dialog from `/multica-web` to `/multica-backend`. Task 1 and Task 3 implement this by adding `cli_server_url` and using it in `connect-remote-dialog.tsx`. The related onboarding and landing CLI instructions are updated for consistency in Task 3 and Task 4.
2. **Placeholder scan:** No placeholders or vague steps.
3. **Type consistency:** Field names are consistent: Go `CliServerURL` / JSON `cli_server_url` / TS `cliServerUrl` / store setter `setCliServerUrl`.
