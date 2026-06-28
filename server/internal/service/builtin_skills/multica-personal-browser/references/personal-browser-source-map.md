# personal-browser source map

Evidence layer for `SKILL.md` (FIR-2037). Every contract the skill states is
traced to a current `file:line` here. Re-confirm with the verification commands
at the bottom before relying on an exact line.

## CLI surface — `multica cerebro-browser`

| Behavior | File:line |
|---|---|
| Parent command `cerebro-browser` | `server/cmd/multica/cerebro_browser.go` (`cerebroBrowserCmd`) |
| Subcommands `open` / `snapshot` / `click` / `fill` / `navigate` / `sessions` / `logout` / `clear-cookies` | `server/cmd/multica/cerebro_browser.go` (`init`) |
| `open` launches the desktop app if needed, waits for the sidecar, then requests the Browser tab | `server/cmd/multica/cerebro_browser.go` (`runCerebroBrowserOpen`, `launchDesktopApp`, `sidecarReady`) |
| Desktop app identity for launch (`appId` / `productName`) | `server/cmd/multica/cerebro_browser.go` (`desktopAppBundleID`, `desktopAppName`) ↔ `apps/desktop/electron-builder.yml` |
| `--session` persistent flag | `server/cmd/multica/cerebro_browser.go` (`PersistentFlags().String("session", …)`) |
| Registered on the root command | `server/cmd/multica/main.go` (`rootCmd.AddCommand(cerebroBrowserCmd)`, marked `CEREBRO-PATCH(cerebro-browser-cli)`) |
| Refuses without the grant env `MULTICA_PERSONAL_BROWSER` | `server/cmd/multica/cerebro_browser.go` (`callCerebroBrowser`) |
| Reads sidecar `~/.multica/cerebro-browser-control.json` for port + token | `server/cmd/multica/cerebro_browser.go` (`cerebroBrowserSidecarPath`) |
| POSTs `http://127.0.0.1:<port>/agent/<action>` with `Authorization: Bearer <token>` | `server/cmd/multica/cerebro_browser.go` (`callCerebroBrowser`) |

## Capability gate — `tools:personal-browser`

The decision is split: a claim-time FEATURE gate (is the browser reachable at
all) and the authoritative PER-ACTION gate (may this agent drive this host now).

| Behavior | File:line |
|---|---|
| Capability declared in the Claude tool set (mirrors to `tools:personal-browser`) | `server/internal/cerebro/capabilities/discovery.go` (`providerRegistry["claude"].Tools`, `"personal-browser"`) |
| Capability key constant | `server/internal/handler/daemon_personal_browser_cerebro.go` (`personalBrowserToolKey`) |
| Grant env constant `MULTICA_PERSONAL_BROWSER` | `server/internal/handler/daemon_personal_browser_cerebro.go` (`personalBrowserGrantEnv`) |
| Claim-time env tracks the **feature flag** `cerebro_browser` (not the capability — a host-conditioned grant can't be evaluated with no host) | `server/internal/handler/daemon_personal_browser_cerebro.go` (`personalBrowserFeatureEnabled`, `withPersonalBrowserEnv`) |
| Claim-time call site | `server/internal/handler/daemon.go` (`ClaimTaskByRuntime`, marked `CEREBRO-PATCH(personal-browser-gate)`) |

### Per-action authoritative gate (all-layer + conditions + site-limits)

| Behavior | File:line |
|---|---|
| Endpoint `POST /api/cerebro/personal-browser/authorize` (agent `mat_` token) | `server/cmd/server/router.go` (marked `CEREBRO-PATCH(personal-browser-authorize-route)`) |
| Resolves the full chain with **Base = Deny + `RequestContext{Host}`** so a host-allowlist condition bites (FIR-1609) | `server/internal/handler/personal_browser_authorize_cerebro.go` (`AuthorizePersonalBrowser`) |
| Agent identity from server-set `X-Agent-ID` / `X-Workspace-ID` (auth middleware, `X-Actor-Source: task_token`) | `server/internal/middleware/auth.go`; `personal_browser_authorize_cerebro.go` |
| Central audit (which agent, which host, decision) | `server/internal/handler/personal_browser_authorize_cerebro.go` (`auditPersonalBrowser`) |
| Desktop control server authorizes every action against the endpoint (fails closed) | `apps/desktop/src/main/cerebro-browser-control-server.ts` (`authorize`, `Route.hostFor`) |
| Server base URL the desktop authorizes against | `apps/desktop/src/main/daemon-manager.ts` (`getTargetApiBaseUrl`) |
| CLI forwards the agent token (`MULTICA_TOKEN`) so the desktop can authorize as the agent | `server/cmd/multica/cerebro_browser.go` (`callCerebroBrowser`) |

## Desktop control server + pane

| Behavior | File:line |
|---|---|
| Loopback control server (127.0.0.1, bearer token, audit) | `apps/desktop/src/main/cerebro-browser-control-server.ts` |
| Writes the 0600 sidecar `cerebro-browser-control.json` | `apps/desktop/src/main/cerebro-browser-control-server.ts` (`ensureCerebroBrowserControlServer`) |
| `/agent/open-tab` route (background-loads url, then asks the renderer to show the tab) | `apps/desktop/src/main/cerebro-browser-control-server.ts` (`buildRoutes`) |
| Control server started at app startup (flag-on), not only on manual tab open | `apps/desktop/src/main/cerebro-browser-pane.ts` (`cerebro-browser:ensure-control-server`) ← `apps/desktop/src/renderer/src/cerebro/use-cerebro-browser-bridge.ts` |
| Pane focuses the window + asks renderer to open the Browser tab | `apps/desktop/src/main/cerebro-browser-pane.ts` (`requestOpenTab`) |
| Renderer opens the Browser route when the agent asks | `apps/desktop/src/renderer/src/cerebro/use-cerebro-browser-bridge.ts` (`onOpenTab`) |
| Audit log `~/.multica/logs/cerebro-browser-audit.log` (never logs typed values) | `apps/desktop/src/main/cerebro-browser-control-server.ts` (`audit`, `/agent/fill` handler) |
| Pane drives the same logged-in view over CDP | `apps/desktop/src/main/cerebro-browser-pane.ts` (`agentSnapshot` / `agentClick` / `agentFill` / `agentNavigate`) |
| Per-session isolated partition `persist:cerebro-browser[-<id>]` | `apps/desktop/src/main/cerebro-browser-pane.ts` (`partitionFor`) |
| logout / clear-cookies wipe only the personal-browser partition | `apps/desktop/src/main/cerebro-browser-pane.ts` (`logout`, `clearCookies`) |
| Feature flag `cerebro_browser` gates the pane (renderer-side) | `packages/cerebro-feature-flags/registry.ts` |

## Verification commands

```bash
# CLI surface and gate (no app needed):
multica cerebro-browser --help
multica cerebro-browser snapshot            # → "not granted" without the capability

# Capability + claim wiring:
grep -n "personal-browser" server/internal/cerebro/capabilities/discovery.go
grep -n "withPersonalBrowserEnv\|personalBrowserToolKey" server/internal/handler/daemon_personal_browser_cerebro.go
grep -n "personal-browser-gate" server/internal/handler/daemon.go

# Desktop transport:
grep -n "ensureCerebroBrowserControlServer\|/agent/" apps/desktop/src/main/cerebro-browser-control-server.ts
```
