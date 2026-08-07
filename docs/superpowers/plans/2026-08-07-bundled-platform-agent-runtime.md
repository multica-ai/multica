# Bundled Platform Agent Runtime Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship `platform-agent-cli` as a first-class, bundled Multica Desktop runtime that is discovered as `provider=platform-agent-cli`, appears in the existing runtime picker, and executes a real task through the CLI's Codex-compatible app-server protocol.

**Architecture:** Keep the Platform Agent CLI source and release pipeline outside the Multica repository. Multica's package step selects and verifies one prebuilt CLI artifact for the current Electron target, Desktop injects its unpacked absolute path into every daemon/probe child, the daemon registers an independent built-in provider, and a provider-specific backend reuses only the app-server transport/lifecycle while disabling Codex-owned configuration and model policy.

**Tech Stack:** Go 1.26.1, Electron, TypeScript 5.9, Node.js 22 ESM scripts, pnpm/Turborepo, Vitest, Go tests, electron-builder.

## Global Constraints

- The Multica repository must not contain Platform Agent CLI source or tracked CLI binaries.
- The external CLI remains at `/Users/zxx/Documents/技术学习/platform-agent-cli/bin/platform-agent-cli` for the current macOS arm64 smoke test.
- The provider key is exactly `platform-agent-cli`; never register it as `codex` and never fall back to `codex` after a failure.
- `platform-agent-cli` is built in by Desktop discovery. Do not add it to `agent.SupportedTypes`, the Runtime Profile protocol-family database constraint, or Runtime Profile administration UI.
- The first implementation supports no Multica-side model, thinking-level, service-tier, custom-argument, or MCP injection. The runtime owns those decisions.
- Every installer contains exactly one target-compatible Platform Agent CLI binary, named `platform-agent-cli` on macOS/Linux and `platform-agent-cli.exe` on Windows.
- Default tests may only inspect a test-created executable. The external mock may run only behind the existing `agentintegration` build tag and `MULTICA_RUN_REAL_AGENT_SMOKE=1`.
- Use `apply_patch` for source edits, English code comments, `gofmt` for Go, and atomic conventional commits.
- Do not add a database migration or API endpoint. Existing string-valued provider fields and runtime registration APIs are sufficient.

---

### Task 1: Add a provider-neutral app-server policy and independent backend

**Files:**

- Create: `server/pkg/agent/app_server_policy.go`
- Create: `server/pkg/agent/platform_agent.go`
- Create: `server/pkg/agent/platform_agent_test.go`
- Modify: `server/pkg/agent/agent.go`
- Modify: `server/pkg/agent/agent_test.go`
- Modify: `server/pkg/agent/codex.go`
- Modify: `server/pkg/agent/models.go`
- Modify: `server/pkg/agent/version.go`
- Modify: `server/pkg/agent/platform_cli_integration_test.go`

- [ ] **Step 1: Write failing provider contract tests**

Add table-driven tests that pin the public identity and capability contract:

```go
func TestPlatformAgentProviderContract(t *testing.T) {
	backend, err := New("platform-agent-cli", Config{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := backend.(*platformAgentBackend); !ok {
		t.Fatalf("New(platform-agent-cli) returned %T", backend)
	}
	if IsSupportedType("platform-agent-cli") {
		t.Fatal("built-in platform runtime must not enter the custom profile whitelist")
	}
	if got := LaunchHeader("platform-agent-cli"); got != "platform-agent-cli app-server" {
		t.Fatalf("LaunchHeader = %q", got)
	}
	if ModelSelectionSupported("platform-agent-cli") {
		t.Fatal("platform runtime owns model selection")
	}
}

func TestPlatformAgentMinimumVersion(t *testing.T) {
	if err := CheckMinVersion("platform-agent-cli", "platform-agent-cli 0.1.0"); err != nil {
		t.Fatal(err)
	}
	if err := CheckMinVersion("platform-agent-cli", "platform-agent-cli 0.0.9"); err == nil {
		t.Fatal("expected below-minimum error")
	}
}
```

Also assert `ListModels(context.Background(), "platform-agent-cli", "")` returns an empty, non-fallback catalog without resolving an executable.

- [ ] **Step 2: Run the narrow Go tests and confirm RED**

Run:

```bash
cd server
/Users/zxx/Documents/技术学习/.tools/go1.26.1/bin/go test ./pkg/agent -run 'TestPlatformAgent' -count=1
```

Expected: `New` reports an unknown agent type and the launch/version/model assertions fail.

- [ ] **Step 3: Introduce the shared app-server policy boundary**

Define a private policy that distinguishes protocol behavior from Codex vendor behavior:

```go
type appServerPolicy struct {
	provider                   string
	defaultExecutable          string
	manageCodexConfig          bool
	allowCodexLaunchOverrides  bool
	allowCodexTurnOverrides    bool
	allowCodexStartupRetry     bool
}

var codexAppServerPolicy = appServerPolicy{
	provider:                  "codex",
	defaultExecutable:         "codex",
	manageCodexConfig:         true,
	allowCodexLaunchOverrides: true,
	allowCodexTurnOverrides:   true,
	allowCodexStartupRetry:    true,
}

var platformAgentAppServerPolicy = appServerPolicy{
	provider:          "platform-agent-cli",
	defaultExecutable: "platform-agent-cli",
}
```

Extend `codexBackend` with an optional policy and preserve the historical zero-value behavior for existing direct tests:

```go
type codexBackend struct {
	cfg    Config
	policy *appServerPolicy
}

func (b *codexBackend) appServerPolicy() appServerPolicy {
	if b.policy == nil {
		return codexAppServerPolicy
	}
	return *b.policy
}
```

Update the app-server execution path so the policy owns:

- default executable and user-visible provider name;
- whether `CODEX_HOME/config.toml` is read or written;
- whether `ExtraArgs`, `CustomArgs`, MCP configuration and Fast mode alter launch arguments;
- whether model, thinking level and service tier enter thread/resume/thread-start/turn-start payloads;
- whether the Codex model-catalog startup retry runs;
- provider names in executable, startup, handshake and process-exit errors.

Keep Codex protocol method names (`initialize`, `thread/start`, `thread/resume`, `turn/start`) and event parsing shared. Do not duplicate `codex.go` into a second protocol implementation.

- [ ] **Step 4: Add the independent Platform Agent backend**

Create a wrapper with its own concrete type:

```go
type platformAgentBackend struct {
	transport *codexBackend
}

func newPlatformAgentBackend(cfg Config) *platformAgentBackend {
	cfg.CodexVersion = ""
	cfg.Env = cloneStringMap(cfg.Env)
	delete(cfg.Env, "CODEX_HOME")
	return &platformAgentBackend{
		transport: &codexBackend{cfg: cfg, policy: &platformAgentAppServerPolicy},
	}
}

func (b *platformAgentBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	opts.Model = ""
	opts.ThinkingLevel = ""
	opts.ServiceTier = ""
	opts.ExtraArgs = nil
	opts.CustomArgs = nil
	opts.McpConfig = nil
	return b.transport.Execute(ctx, prompt, opts)
}
```

Use a small private map-clone helper so removing `CODEX_HOME` cannot mutate the daemon's caller-owned environment. Add a unit test that passes every disabled option and asserts the sanitized value, preferably through a pure `platformAgentExecOptions` helper.

- [ ] **Step 5: Register provider metadata without changing Runtime Profiles**

Update `agent.New`, its supported-type error text and package comment to include the built-in provider. Add:

```go
case "platform-agent-cli":
	return newPlatformAgentBackend(cfg), nil
```

Add the following metadata:

```go
// launchHeaders
"platform-agent-cli": "platform-agent-cli app-server",

// MinVersions
"platform-agent-cli": "0.1.0",
```

In `ListModels`, return `Catalog{Models: []Model{}}` without shelling out. In `ModelSelectionSupported`, return `false`. Leave `SupportedTypes` unchanged and keep a regression assertion for that exclusion.

- [ ] **Step 6: Convert the real protocol smoke to the new provider**

Rename the test to `TestPlatformAgentCLIRealRuntime` and replace:

```go
backend, err := New("platform-agent-cli", Config{
	ExecutablePath: path,
	CLIVersion:     version,
	Logger:         logger,
})
```

Remove `CodexVersion`, change failure copy from “Codex backend” to “Platform Agent backend”, and change the session assertion text to “app-server thread session ID”. Keep the exact expected Mock output and the prohibition on tool events.

- [ ] **Step 7: Run unit, regression and authorized real smoke tests**

Run:

```bash
cd server
/Users/zxx/Documents/技术学习/.tools/go1.26.1/bin/go test ./pkg/agent -run 'TestPlatformAgent|TestNewCodex|TestCodex' -count=1
MULTICA_RUN_REAL_AGENT_SMOKE=1 \
MULTICA_PLATFORM_AGENT_CLI_PATH=/Users/zxx/Documents/技术学习/platform-agent-cli/bin/platform-agent-cli \
/Users/zxx/Documents/技术学习/.tools/go1.26.1/bin/go test -tags=agentintegration ./pkg/agent -run TestPlatformAgentCLIRealRuntime -count=1 -v
```

Expected: the external binary reports `platform-agent-cli 0.1.0`, streams at least one text event, returns a non-empty session ID, and returns `Mock Runtime 已收到任务：multica source integration smoke`.

- [ ] **Step 8: Commit the backend boundary**

```bash
git add server/pkg/agent
git commit -m "feat(agent): add platform agent runtime backend"
```

---

### Task 2: Register the built-in daemon provider and execution context

**Files:**

- Create: `server/internal/daemon/agents_probe_platform_test.go`
- Modify: `server/internal/daemon/agents_probe.go`
- Modify: `server/internal/daemon/config.go`
- Modify: `server/internal/daemon/daemon.go`
- Modify: `server/internal/daemon/daemon_test.go`
- Modify: `server/internal/daemon/execenv/runtime_config.go`
- Modify: `server/internal/daemon/execenv/runtime_config_test.go`
- Modify: `scripts/agent-cli-command-names.txt`

- [ ] **Step 1: Write failing daemon discovery and identity tests**

Create a test-owned executable and pin it through the new environment variable:

```go
func TestProbeAgentCLIsPlatformAgentPinnedPath(t *testing.T) {
	path := fakeExecutable(t, "platform-agent-cli")
	t.Setenv("PATH", "")
	t.Setenv("MULTICA_PLATFORM_AGENT_CLI_PATH", path)

	agents := probeAgentCLIs()
	entry, ok := agents["platform-agent-cli"]
	if !ok {
		t.Fatal("platform-agent-cli was not discovered")
	}
	if entry.Path != path || entry.Command != path || entry.Model != "" {
		t.Fatalf("unexpected entry: %+v", entry)
	}
}
```

Follow the existing Qoder test's login-shell stub/reset pattern so no ambient CLI path affects the result. Add tests for:

- `providerDisplayName("platform-agent-cli") == "Platform Agent CLI"`;
- `runtimeConfigPath(workDir, "platform-agent-cli") == <workDir>/AGENTS.md`;
- `providerNeedsInlineSystemPrompt("platform-agent-cli") == false`, because app-server loads the task's `AGENTS.md` through the working directory;
- the default command guard contains `platform-agent-cli`.

- [ ] **Step 2: Run the daemon guards and confirm RED**

Run:

```bash
cd server
/Users/zxx/Documents/技术学习/.tools/go1.26.1/bin/go test ./internal/daemon ./internal/daemon/execenv \
  -run 'TestProbeAgentCLIsPlatform|TestPlatformAgent|TestDefaultAgentCommandNamesCoversAllProbes|TestAgentCLIGuardCoversDefaultCommands' \
  -count=1
```

Expected: provider discovery, display name, config path, and command-list assertions fail.

- [ ] **Step 3: Add the provider to built-in discovery**

Add this probe with no model environment variable:

```go
if e, ok := probe("MULTICA_PLATFORM_AGENT_CLI_PATH", "platform-agent-cli", ""); ok {
	agents["platform-agent-cli"] = e
}
```

Update:

- `Config.Agents`' provider comment;
- the `LoadConfig` “no agent CLI found” error list;
- `defaultAgentCommandNames`;
- `scripts/agent-cli-command-names.txt`.

Do not add an args field to daemon `Config`; platform launch arguments are intentionally unsupported in this phase.

- [ ] **Step 4: Add display and task-context mapping**

Add:

```go
"platform-agent-cli": "Platform Agent CLI",
```

to `runtimeDisplayNameOverrides`. Add `platform-agent-cli` to the `AGENTS.md` branch of `runtimeConfigPath` so normal Multica runtime instructions remain available through the app-server working directory. Leave it on the default `.agent_context/skills` sidecar path; platform-owned Skill/Extension registration is outside this phase.

Confirm every Codex-only daemon branch remains guarded by the exact comparison `provider == "codex"`, including Codex session storage, `CODEX_HOME`, sandbox, repository checkout mode, model discovery, login and update behavior.

- [ ] **Step 5: Run daemon and execution-environment tests**

Run:

```bash
cd server
/Users/zxx/Documents/技术学习/.tools/go1.26.1/bin/go test ./internal/daemon ./internal/daemon/execenv -count=1
```

- [ ] **Step 6: Commit daemon registration**

```bash
git add server/internal/daemon server/internal/daemon/execenv scripts/agent-cli-command-names.txt
git commit -m "feat(daemon): register platform agent runtime"
```

---

### Task 3: Stage verified per-target CLI artifacts during Desktop builds

**Files:**

- Create: `apps/desktop/scripts/bundle-platform-agent-cli.mjs`
- Create: `apps/desktop/scripts/bundle-platform-agent-cli.test.mjs`
- Modify: `apps/desktop/scripts/bundle-cli.mjs`
- Modify: `apps/desktop/scripts/package.mjs`
- Modify: `apps/desktop/scripts/package.test.mjs`
- Modify: `apps/desktop/scripts/dev.mjs`
- Modify: `apps/desktop/package.json`

- [ ] **Step 1: Write failing artifact-contract tests**

Test these exported functions:

```js
platformAgentArtifactName(version, targetPlatform, targetArch)
platformAgentBinaryName(targetPlatform)
parseChecksumManifest(raw)
stageReleasePlatformAgent(options)
stageDevPlatformAgent(options)
```

Pin all six mappings:

| Electron target | Expected artifact |
| --- | --- |
| `darwin/x64` | `platform-agent-cli_0.1.0_darwin_amd64` |
| `darwin/arm64` | `platform-agent-cli_0.1.0_darwin_arm64` |
| `linux/x64` | `platform-agent-cli_0.1.0_linux_amd64` |
| `linux/arm64` | `platform-agent-cli_0.1.0_linux_arm64` |
| `win32/x64` | `platform-agent-cli_0.1.0_windows_amd64.exe` |
| `win32/arm64` | `platform-agent-cli_0.1.0_windows_arm64.exe` |

Use `mkdtemp`, test-created files and `createHash("sha256")`. Cover:

- valid checksum copies exactly one selected artifact;
- missing artifact, missing checksum entry, malformed checksum and mismatch all reject;
- staging removes both stale Platform binary names but preserves sibling `multica`/`multica.exe`;
- development without `PLATFORM_AGENT_CLI_DEV_BINARY` removes stale Platform binaries and succeeds;
- a configured but missing development binary fails instead of silently keeping a stale copy.

- [ ] **Step 2: Run the script tests and confirm RED**

Run:

```bash
pnpm --filter @multica/desktop exec vitest run scripts/bundle-platform-agent-cli.test.mjs scripts/package.test.mjs
```

Expected: the new module is missing and the packaging invocation assertion fails.

- [ ] **Step 3: Implement strict release and tolerant development staging**

Use the following public contract in the ESM module:

```js
export function platformAgentArtifactName(version, targetPlatform, targetArch) {}
export function platformAgentBinaryName(targetPlatform) {}
export function parseChecksumManifest(raw) {}
export async function stageReleasePlatformAgent(options) {}
export async function stageDevPlatformAgent(options) {}
```

Release mode must:

1. require non-empty `PLATFORM_AGENT_CLI_VERSION` and absolute `PLATFORM_AGENT_CLI_ARTIFACT_DIR`;
2. accept only `darwin|linux|win32` and `x64|arm64`;
3. parse standard lowercase `sha256sum` lines (`<64 hex><two spaces><filename>`);
4. verify the selected file before copying;
5. remove `platform-agent-cli` and `platform-agent-cli.exe` from the destination first;
6. copy to the canonical bundled name and set mode `0755` on non-Windows targets.

Development mode must use only `PLATFORM_AGENT_CLI_DEV_BINARY`, copy it for the host platform, and remove stale staged Platform binaries when the variable is absent.

The module's CLI entry point uses `--release`, `--target-platform`, and `--target-arch`; it must be guarded by `pathToFileURL(process.argv[1]).href` so Vitest can import it without executing staging.

- [ ] **Step 4: Make the existing Multica CLI bundler sibling-safe**

Replace every whole-directory removal of `resources/bin` with removal of both Multica binary names only:

```js
await Promise.all([
  rm(join(destDir, "multica"), { force: true }),
  rm(join(destDir, "multica.exe"), { force: true }),
]);
```

Then create `destDir` without deleting it. This preserves the Platform binary while still preventing a cross-target package matrix from carrying both Multica executable suffixes.

- [ ] **Step 5: Wire development, build and release packaging**

Add scripts:

```json
{
  "bundle-platform-agent-cli": "node scripts/bundle-platform-agent-cli.mjs",
  "build": "pnpm run bundle-cli && pnpm run bundle-platform-agent-cli && electron-vite build"
}
```

In `dev.mjs`, run the Platform bundler after `bundle-cli.mjs` and before branding. In each `package.mjs` target iteration, run:

```js
execFileSync(process.execPath, [
  bundlePlatformAgentScript,
  "--release",
  "--target-platform",
  PLATFORM_CONFIG[target.platform].runtimePlatform,
  "--target-arch",
  target.arch,
], { stdio: "inherit", cwd: desktopRoot });
```

Invoke it after the matching Multica CLI build and before electron-builder. Extend `package.test.mjs` to assert one strict Platform staging call per build target.

- [ ] **Step 6: Run Desktop script tests**

Run:

```bash
pnpm --filter @multica/desktop exec vitest run scripts/bundle-platform-agent-cli.test.mjs scripts/package.test.mjs
```

- [ ] **Step 7: Commit artifact staging**

```bash
git add apps/desktop/scripts apps/desktop/package.json
git commit -m "feat(desktop): bundle platform agent runtime"
```

---

### Task 4: Inject the bundled executable path into every Desktop daemon child

**Files:**

- Create: `apps/desktop/src/main/platform-agent-runtime.ts`
- Create: `apps/desktop/src/main/platform-agent-runtime.test.ts`
- Modify: `apps/desktop/src/main/daemon-manager.ts`

- [ ] **Step 1: Write failing pure path/environment tests**

Cover:

- development app path → `resources/bin/platform-agent-cli`;
- packaged `app.asar` path → `app.asar.unpacked/resources/bin/platform-agent-cli`;
- Windows → `.exe`;
- existing bundled file overwrites an inherited `MULTICA_PLATFORM_AGENT_CLI_PATH`;
- missing bundled file deletes the inherited value from the child environment;
- the input environment object is never mutated.

Use dependency injection for the existence check; no test should inspect a real installed CLI.

- [ ] **Step 2: Run the focused test and confirm RED**

Run:

```bash
pnpm --filter @multica/desktop exec vitest run src/main/platform-agent-runtime.test.ts
```

Expected: module import fails.

- [ ] **Step 3: Implement the pure helper**

Use this interface:

```ts
export const PLATFORM_AGENT_CLI_PATH_ENV = "MULTICA_PLATFORM_AGENT_CLI_PATH";

export function bundledPlatformAgentPath(
  appPath: string,
  platform: NodeJS.Platform,
): string;

export function withBundledPlatformAgentPath(
  sourceEnv: NodeJS.ProcessEnv,
  appPath: string,
  platform: NodeJS.Platform,
  exists: (path: string) => boolean,
): NodeJS.ProcessEnv;
```

Clone `sourceEnv`, delete the inherited Platform path first, resolve the canonical bundled path, and re-add it only when `exists(path)` is true.

- [ ] **Step 4: Use the helper in the central child environment**

Update `desktopSpawnEnv()` only:

```ts
function desktopSpawnEnv(): NodeJS.ProcessEnv {
  return withBundledPlatformAgentPath(
    { ...process.env, MULTICA_LAUNCHED_BY: "desktop" },
    app.getAppPath(),
    process.platform,
    existsSync,
  );
}
```

Because `probeLocalRuntimes()` and `startDaemon()` already call this function, one change covers both the onboarding probe process and the long-running daemon supervisor path. Do not set the variable globally on `process.env`.

- [ ] **Step 5: Run Desktop main-process tests and typecheck**

Run:

```bash
pnpm --filter @multica/desktop exec vitest run src/main/platform-agent-runtime.test.ts src/main/daemon-auth-probe.test.ts
pnpm --filter @multica/desktop typecheck:node
```

- [ ] **Step 6: Commit Desktop path injection**

```bash
git add apps/desktop/src/main/platform-agent-runtime.ts \
  apps/desktop/src/main/platform-agent-runtime.test.ts \
  apps/desktop/src/main/daemon-manager.ts
git commit -m "feat(desktop): inject bundled platform runtime path"
```

---

### Task 5: Present the real runtime in onboarding and provider UI

**Files:**

- Modify: `packages/views/runtimes/components/provider-logo.tsx`
- Modify: `packages/views/runtimes/components/provider-logo.test.tsx`
- Modify: `packages/views/onboarding/steps/step-runtime-connect.test.tsx`

- [ ] **Step 1: Write failing UI assertions**

Add a Provider Logo test that renders `provider="platform-agent-cli"` and asserts a dedicated SVG marker rather than the generic fallback:

```tsx
expect(
  container.querySelector('svg[data-provider-logo="platform-agent-cli"]'),
).not.toBeNull();
```

Add an onboarding fixture:

```ts
const platformRuntime = makeRuntime({
  id: "rt_platform",
  name: "Platform Agent CLI",
  provider: "platform-agent-cli",
  status: "online",
});
```

Set it as the real picker result and assert the visible name, the `1 agent runtime` count and enabled “Start with Mika” action. Do not add a static provider option to onboarding production code.

- [ ] **Step 2: Run the focused view tests and confirm RED**

Run:

```bash
pnpm --filter @multica/views exec vitest run \
  runtimes/components/provider-logo.test.tsx \
  onboarding/steps/step-runtime-connect.test.tsx
```

- [ ] **Step 3: Add a dedicated Platform Agent logo case**

Add a small inline SVG component using semantic `currentColor`, a unique `data-provider-logo="platform-agent-cli"` marker, and an explicit switch case:

```tsx
case "platform-agent-cli":
  return <PlatformAgentLogo className={className} />;
```

Do not add an external brand asset unless one is supplied by the Platform Agent CLI project.

- [ ] **Step 4: Run view tests and typecheck**

Run:

```bash
pnpm --filter @multica/views exec vitest run \
  runtimes/components/provider-logo.test.tsx \
  onboarding/steps/step-runtime-connect.test.tsx
pnpm --filter @multica/views typecheck
```

The model picker requires no production change: the daemon already returns `supported: false` from `ModelSelectionSupported`, and the existing UI renders “Managed by runtime”.

- [ ] **Step 5: Commit provider presentation**

```bash
git add packages/views/runtimes/components/provider-logo.tsx \
  packages/views/runtimes/components/provider-logo.test.tsx \
  packages/views/onboarding/steps/step-runtime-connect.test.tsx
git commit -m "feat(views): present platform agent runtime"
```

---

### Task 6: Build the current installer and complete the real Multica smoke test

**Files:**

- Verify only; do not add external artifacts to Git.

- [ ] **Step 1: Stage the current development binary**

Run:

```bash
PLATFORM_AGENT_CLI_DEV_BINARY=/Users/zxx/Documents/技术学习/platform-agent-cli/bin/platform-agent-cli \
pnpm --filter @multica/desktop run bundle-platform-agent-cli

test -x apps/desktop/resources/bin/platform-agent-cli
apps/desktop/resources/bin/platform-agent-cli --version
```

Expected version: `platform-agent-cli 0.1.0`.

- [ ] **Step 2: Run backend and frontend regression suites**

Run:

```bash
cd server
/Users/zxx/Documents/技术学习/.tools/go1.26.1/bin/go test ./pkg/agent ./internal/daemon ./internal/daemon/execenv -count=1
/Users/zxx/Documents/技术学习/.tools/go1.26.1/bin/go vet ./pkg/agent ./internal/daemon ./internal/daemon/execenv

cd /Users/zxx/Documents/技术学习/multica
pnpm --filter @multica/desktop test
pnpm --filter @multica/views test
pnpm --filter @multica/desktop typecheck
pnpm --filter @multica/views typecheck
```

- [ ] **Step 3: Re-run the external protocol test on the independent provider**

Run:

```bash
cd server
MULTICA_RUN_REAL_AGENT_SMOKE=1 \
MULTICA_PLATFORM_AGENT_CLI_PATH=/Users/zxx/Documents/技术学习/platform-agent-cli/bin/platform-agent-cli \
/Users/zxx/Documents/技术学习/.tools/go1.26.1/bin/go test -tags=agentintegration ./pkg/agent -run TestPlatformAgentCLIRealRuntime -count=1 -v
```

- [ ] **Step 4: Materialize a local release artifact and checksum outside Git**

Run:

```bash
PLATFORM_AGENT_ARTIFACT_DIR="$(mktemp -d)"
cp /Users/zxx/Documents/技术学习/platform-agent-cli/bin/platform-agent-cli \
  "$PLATFORM_AGENT_ARTIFACT_DIR/platform-agent-cli_0.1.0_darwin_arm64"
(
  cd "$PLATFORM_AGENT_ARTIFACT_DIR"
  shasum -a 256 platform-agent-cli_0.1.0_darwin_arm64 > checksums.txt
)
```

Keep the printed temporary directory path for the next step. Do not copy it into the repository.

- [ ] **Step 5: Build an actual macOS arm64 Desktop package**

Run with the temporary directory from Step 4:

```bash
PLATFORM_AGENT_CLI_VERSION=0.1.0 \
PLATFORM_AGENT_CLI_ARTIFACT_DIR="$PLATFORM_AGENT_ARTIFACT_DIR" \
CSC_IDENTITY_AUTO_DISCOVERY=false \
pnpm --filter @multica/desktop run package -- --mac --arm64 --publish never
```

Inspect the unpacked app and assert exactly one Platform binary:

```bash
find apps/desktop/dist -path '*resources/bin/platform-agent-cli*' -type f -print
```

Expected: one `platform-agent-cli` inside the macOS arm64 app resources and no `.exe` or second architecture artifact.

- [ ] **Step 6: Run the real Desktop/Daemon/onboarding task flow**

Launch the source Desktop with the development binary:

```bash
PLATFORM_AGENT_CLI_DEV_BINARY=/Users/zxx/Documents/技术学习/platform-agent-cli/bin/platform-agent-cli \
pnpm dev:desktop
```

In the running Multica client:

1. open the existing runtime selection step;
2. confirm `Platform Agent CLI` is reported by the daemon as online;
3. select that runtime for the test Agent;
4. submit `multica desktop platform runtime smoke`;
5. confirm the task completes and the result contains `Mock Runtime 已收到任务：multica desktop platform runtime smoke`;
6. inspect the daemon log and confirm the provider is `platform-agent-cli`, the command is the bundled absolute path plus `app-server --listen stdio://`, and there is no fallback launch of `codex`.

Also run the bundled Multica CLI's read-only check against the active profile:

```bash
apps/desktop/resources/bin/multica runtime list --output json
```

Expected: an online runtime whose `provider` is exactly `platform-agent-cli` and whose displayed name is `Platform Agent CLI`.

- [ ] **Step 7: Verify repository hygiene**

Run:

```bash
git diff --check
git status --short
git ls-files apps/desktop/resources/bin server/bin | rg 'platform-agent-cli' && exit 1 || true
git diff --name-only HEAD~5..HEAD | rg '(^|/)platform-agent-cli(\.exe)?$' && exit 1 || true
```

Expected: no tracked CLI binary or external CLI source, no whitespace errors, and only the intended source/tests/docs changes.

- [ ] **Step 8: Record verification evidence**

Add no new production commit unless verification uncovers a fix. In the final handoff, report:

- commits created;
- targeted and full test commands that passed;
- actual package path;
- runtime ID/provider observed in Multica;
- exact smoke task output;
- any cross-platform packages not built because their external artifacts were unavailable.
