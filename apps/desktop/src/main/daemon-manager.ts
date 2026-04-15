import { app, ipcMain, BrowserWindow } from "electron";
import { execFile } from "child_process";
import {
  readFile,
  writeFile,
  mkdir,
  rm,
  open,
  stat,
} from "fs/promises";
import {
  existsSync,
  watchFile,
  unwatchFile,
  type StatsListener,
} from "fs";
import { join } from "path";
import { homedir } from "os";
import type { DaemonStatus, DaemonPrefs } from "../shared/daemon-types";
import { ensureManagedCli, managedCliPath } from "./cli-bootstrap";
import { decideVersionAction } from "./version-decision";

const DEFAULT_HEALTH_PORT = 19514;
const POLL_INTERVAL_MS = 5_000;
const PREFS_PATH = join(homedir(), ".multica", "desktop_prefs.json");
const LOG_TAIL_RETRY_MS = 2_000;
const LOG_TAIL_MAX_RETRIES = 5;

const DEFAULT_PREFS: DaemonPrefs = { autoStart: true, autoStop: false };

interface ActiveProfile {
  name: string; // "" = default profile
  port: number;
}

let statusPollTimer: ReturnType<typeof setInterval> | null = null;
let logTailWatcher: { path: string; listener: StatsListener } | null = null;
let currentState: DaemonStatus["state"] = "installing_cli";
let getMainWindow: () => BrowserWindow | null = () => null;
let operationInProgress = false;
let cachedCliBinary: string | null | undefined = undefined;
let cliResolvePromise: Promise<string | null> | null = null;
let cachedCliBinaryVersion: string | null | undefined = undefined;
// Set when a CLI version mismatch was detected but the running daemon is
// busy executing tasks. The poll loop retries the check on each tick and
// fires the restart once active_task_count drops to 0.
let pendingVersionRestart = false;
let targetApiBaseUrl: string | null = null;
let activeProfile: ActiveProfile | null = null;

// Serialize all writes to any profile config file. Multiple paths
// (syncToken, resolveActiveProfile, clearToken, watch/unwatch handlers)
// may try to write concurrently; chaining them avoids interleaved writes
// corrupting the JSON.
let configWriteChain: Promise<void> = Promise.resolve();

// Keep the Go impl in sync: server/cmd/multica/cmd_daemon.go healthPortForProfile.
function healthPortForProfile(profile: string): number {
  if (!profile) return DEFAULT_HEALTH_PORT;
  let sum = 0;
  for (const b of Buffer.from(profile, "utf-8")) sum += b;
  return DEFAULT_HEALTH_PORT + 1 + (sum % 1000);
}

function profileDir(profile: string): string {
  return profile
    ? join(homedir(), ".multica", "profiles", profile)
    : join(homedir(), ".multica");
}

function profileConfigPath(profile: string): string {
  return join(profileDir(profile), "config.json");
}

function profileLogPath(profile: string): string {
  return join(profileDir(profile), "daemon.log");
}

// Sidecar file that records which Multica user the cached PAT in config.json
// was minted for. The Go CLI/daemon never read or write this file, so it
// survives Go-side config rewrites. Used to detect user switches and mint a
// fresh PAT instead of reusing a token that belongs to a previous user.
function profileUserIdPath(profile: string): string {
  return join(profileDir(profile), ".desktop-user-id");
}

async function readProfileUserId(profile: string): Promise<string | null> {
  try {
    const raw = await readFile(profileUserIdPath(profile), "utf-8");
    const trimmed = raw.trim();
    return trimmed || null;
  } catch {
    return null;
  }
}

async function writeProfileUserId(
  profile: string,
  userId: string,
): Promise<void> {
  await mkdir(profileDir(profile), { recursive: true });
  await writeFile(profileUserIdPath(profile), userId, "utf-8");
}

async function removeProfileUserId(profile: string): Promise<void> {
  try {
    await rm(profileUserIdPath(profile));
  } catch {
    // Already gone — nothing to do.
  }
}

function normalizeUrl(u: string): string {
  if (!u) return "";
  try {
    const parsed = new URL(u);
    return `${parsed.protocol}//${parsed.host}`.toLowerCase();
  } catch {
    return u.replace(/\/+$/, "").toLowerCase();
  }
}

function urlsMatch(a: string, b: string): boolean {
  const na = normalizeUrl(a);
  const nb = normalizeUrl(b);
  return na.length > 0 && na === nb;
}

function sendStatus(status: DaemonStatus): void {
  const win = getMainWindow();
  win?.webContents.send("daemon:status", status);
}

interface HealthPayload {
  status?: string;
  pid?: number;
  uptime?: string;
  daemon_id?: string;
  device_name?: string;
  server_url?: string;
  cli_version?: string;
  active_task_count?: number;
  agents?: string[];
  workspaces?: unknown[];
}

async function fetchHealthAtPort(
  port: number,
): Promise<HealthPayload | null> {
  try {
    const controller = new AbortController();
    const timeout = setTimeout(() => controller.abort(), 2_000);
    const res = await fetch(`http://127.0.0.1:${port}/health`, {
      signal: controller.signal,
    });
    clearTimeout(timeout);
    if (!res.ok) return null;
    return (await res.json()) as HealthPayload;
  } catch {
    return null;
  }
}

// Desktop owns a dedicated CLI profile named after the target API host, so it
// never reads or writes the user's hand-configured profiles. Profile dir:
//   ~/.multica/profiles/desktop-<host>/
function deriveProfileName(targetUrl: string): string {
  try {
    const url = new URL(targetUrl);
    const host = url.host.replace(/:/g, "-").toLowerCase();
    return `desktop-${host}`;
  } catch {
    return "desktop";
  }
}

async function readProfileConfig(
  profile: string,
): Promise<Record<string, unknown>> {
  try {
    const raw = await readFile(profileConfigPath(profile), "utf-8");
    const parsed = JSON.parse(raw);
    return parsed && typeof parsed === "object" ? parsed : {};
  } catch {
    return {};
  }
}

async function writeProfileConfig(
  profile: string,
  cfg: Record<string, unknown>,
): Promise<void> {
  const op = async () => {
    await mkdir(profileDir(profile), { recursive: true });
    await writeFile(
      profileConfigPath(profile),
      JSON.stringify(cfg, null, 2),
      "utf-8",
    );
  };
  const next = configWriteChain.catch(() => {}).then(op);
  configWriteChain = next.catch(() => {});
  return next;
}

/**
 * Returns the Desktop-owned profile for the current target API URL. Creates
 * the profile's config.json on demand with `server_url` pinned to the target.
 *
 * This function never falls back to the default profile, and never touches a
 * profile whose name doesn't start with `desktop-`, so the user's manually
 * configured CLI profiles are untouched.
 */
async function resolveActiveProfile(): Promise<ActiveProfile> {
  const target = targetApiBaseUrl;
  if (!target) return { name: "", port: DEFAULT_HEALTH_PORT };

  const name = deriveProfileName(target);
  const cfg = await readProfileConfig(name);

  if (cfg.server_url !== target) {
    cfg.server_url = target;
    await writeProfileConfig(name, cfg);
    console.log(`[daemon] initialized profile "${name}" → ${target}`);
  }

  return { name, port: healthPortForProfile(name) };
}

async function ensureActiveProfile(): Promise<ActiveProfile> {
  if (activeProfile) return activeProfile;
  activeProfile = await resolveActiveProfile();
  return activeProfile;
}

function invalidateActiveProfile(): void {
  activeProfile = null;
}

async function fetchHealth(): Promise<DaemonStatus> {
  // While the CLI is being downloaded or has permanently failed, short-circuit
  // polling — there's nothing to probe yet and /health calls would just return
  // "stopped", which would overwrite the correct setup state in the UI.
  if (currentState === "installing_cli" || currentState === "cli_not_found") {
    return { state: currentState };
  }

  const active = await ensureActiveProfile();
  const data = await fetchHealthAtPort(active.port);

  if (!data || data.status !== "running") {
    return {
      state: currentState === "starting" ? "starting" : "stopped",
      profile: active.name,
    };
  }

  // Safety: if we have a target URL and the daemon on our port reports a
  // different server_url, it's not "our" daemon — drop it and re-resolve.
  if (
    targetApiBaseUrl &&
    data.server_url &&
    !urlsMatch(data.server_url, targetApiBaseUrl)
  ) {
    invalidateActiveProfile();
    return { state: "stopped" };
  }

  return {
    state: "running",
    pid: data.pid,
    uptime: data.uptime,
    daemonId: data.daemon_id,
    deviceName: data.device_name,
    agents: data.agents ?? [],
    workspaceCount: Array.isArray(data.workspaces)
      ? data.workspaces.length
      : 0,
    profile: active.name,
    serverUrl: data.server_url,
  };
}

function findCliOnPath(): string | null {
  const candidates = process.platform === "win32" ? ["multica.exe"] : ["multica"];
  const paths = (process.env["PATH"] ?? "").split(
    process.platform === "win32" ? ";" : ":",
  );
  if (process.platform === "darwin") {
    paths.push("/opt/homebrew/bin", "/usr/local/bin");
  }
  for (const name of candidates) {
    for (const dir of paths) {
      const full = join(dir, name);
      if (existsSync(full)) return full;
    }
  }
  return null;
}

/**
 * Returns the path to the CLI binary bundled inside the Desktop app.
 *
 * - Dev (`electron-vite dev`): `app.getAppPath()` → `apps/desktop`, resolving
 *   to `apps/desktop/resources/bin/multica`. `bundle-cli.mjs` populates this
 *   before dev starts, so iterating on Go changes is "make build → restart".
 * - Packaged: `app.getAppPath()` → `<Multica.app>/Contents/Resources/app.asar`.
 *   electron-builder's `asarUnpack: resources/**` extracts the binary to
 *   `app.asar.unpacked/`, so we swap the path segment to execute it.
 */
function bundledCliPath(): string {
  const binName = process.platform === "win32" ? "multica.exe" : "multica";
  return join(app.getAppPath(), "resources", "bin", binName).replace(
    "app.asar",
    "app.asar.unpacked",
  );
}

/**
 * Returns a usable `multica` binary path. Priority:
 *   1. Cached result from a previous successful resolve.
 *   2. Bundled binary shipped with the Desktop app (`bundle-cli.mjs`).
 *   3. Managed binary already installed in userData (`managedCliPath`).
 *   4. Download + install latest release into userData.
 *   5. `multica` on PATH (dev convenience / user-installed via brew).
 * Returns `null` only when all of the above fail.
 *
 * Bundled is preferred so Desktop iterates in lockstep with Go changes in
 * the same repo — avoids the 404 / stale-API problem when the Desktop's
 * TS side is ahead of the last published CLI release.
 *
 * This function is idempotent and safe to call concurrently — in-flight
 * installs are de-duplicated via `cliResolvePromise`.
 */
async function resolveCliBinary(): Promise<string | null> {
  if (cachedCliBinary !== undefined) return cachedCliBinary;
  if (cliResolvePromise) return cliResolvePromise;

  cliResolvePromise = (async () => {
    const bundled = bundledCliPath();
    if (existsSync(bundled)) {
      console.log(`[daemon] using bundled CLI at ${bundled}`);
      cachedCliBinary = bundled;
      return bundled;
    }

    const managed = managedCliPath();
    if (existsSync(managed)) {
      cachedCliBinary = managed;
      return managed;
    }

    try {
      const installed = await ensureManagedCli();
      cachedCliBinary = installed;
      return installed;
    } catch (err) {
      console.warn("[daemon] CLI auto-install failed, falling back to PATH:", err);
      const onPath = findCliOnPath();
      cachedCliBinary = onPath;
      return onPath;
    }
  })();

  try {
    return await cliResolvePromise;
  } finally {
    cliResolvePromise = null;
  }
}

/**
 * Reads the version of the currently resolved CLI binary by invoking
 * `multica version --output json`. Cached for the process lifetime — the
 * bundled binary doesn't change after `bundle-cli.mjs` runs at dev/build time.
 * Returns null on any failure (unknown `go` at bundle time, broken binary,
 * etc.) so callers can fail open.
 */
async function getCliBinaryVersion(): Promise<string | null> {
  if (cachedCliBinaryVersion !== undefined) return cachedCliBinaryVersion;
  const bin = await resolveCliBinary();
  if (!bin) {
    cachedCliBinaryVersion = null;
    return null;
  }
  try {
    const stdout = await new Promise<string>((resolve, reject) => {
      execFile(
        bin,
        ["version", "--output", "json"],
        { timeout: 5_000 },
        (err, out) => {
          if (err) reject(err);
          else resolve(out);
        },
      );
    });
    const parsed = JSON.parse(stdout) as { version?: string };
    cachedCliBinaryVersion = parsed.version ?? null;
  } catch (err) {
    console.warn("[daemon] failed to read CLI binary version:", err);
    cachedCliBinaryVersion = null;
  }
  return cachedCliBinaryVersion;
}

/**
 * Compares the running daemon's `cli_version` against the CLI binary we
 * would use to spawn a new one, and restarts only when safe. The decision
 * logic itself is in `version-decision.ts` (pure, unit-tested); this
 * wrapper handles the async plumbing and side effects.
 *
 * Restart is only fired when ALL of:
 *   - a daemon is actually running on the active profile's port
 *   - both sides report a version and the strings differ
 *   - `active_task_count` is 0 (no in-flight agent work would be killed)
 *
 * On a confirmed mismatch while the daemon is busy, `pendingVersionRestart`
 * is set; the poll loop retries this function on each 5s tick and will fire
 * the restart as soon as the daemon drains.
 */
async function ensureRunningDaemonVersionMatches(): Promise<
  "restarted" | "deferred" | "ok" | "not_running"
> {
  const active = await ensureActiveProfile();
  const running = await fetchHealthAtPort(active.port);
  const bundled = await getCliBinaryVersion();
  const action = decideVersionAction(bundled, running);

  switch (action) {
    case "not_running":
      pendingVersionRestart = false;
      return "not_running";
    case "ok":
      pendingVersionRestart = false;
      return "ok";
    case "defer": {
      if (!pendingVersionRestart) {
        const activeTasks = running?.active_task_count ?? 0;
        console.log(
          `[daemon] CLI version mismatch (bundled=${bundled} running=${running?.cli_version}); deferring restart until ${activeTasks} active task(s) finish`,
        );
      }
      pendingVersionRestart = true;
      return "deferred";
    }
    case "restart":
      console.log(
        `[daemon] CLI version mismatch (bundled=${bundled} running=${running?.cli_version}) — restarting daemon`,
      );
      pendingVersionRestart = false;
      await restartDaemon();
      return "restarted";
  }
}

/**
 * Exchange the user's JWT for a long-lived PAT via POST /api/tokens. The
 * daemon needs a PAT (or `mul_` / `mdt_` token) because JWTs expire in 30
 * days and signatures are tied to a specific backend instance.
 */
async function mintPat(jwt: string): Promise<string> {
  if (!targetApiBaseUrl) {
    throw new Error("mint PAT: target API URL not set");
  }
  const url = `${targetApiBaseUrl.replace(/\/+$/, "")}/api/tokens`;
  const res = await fetch(url, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      Authorization: `Bearer ${jwt}`,
    },
    // Omit expires_in_days → server treats as null → non-expiring PAT.
    body: JSON.stringify({ name: "Multica Desktop" }),
  });
  if (!res.ok) {
    const body = await res.text().catch(() => "");
    throw new Error(`mint PAT failed: ${res.status} ${res.statusText} ${body}`);
  }
  const data = (await res.json()) as { token?: unknown };
  if (typeof data.token !== "string" || !data.token.startsWith("mul_")) {
    throw new Error("mint PAT: response missing token");
  }
  return data.token;
}

/**
 * Ensure the active profile's config.json has a usable token for the daemon.
 *
 * - Input from the renderer is the user's JWT (from localStorage) plus the
 *   current user's id, so we can detect session changes.
 * - If the profile already has a cached PAT (`mul_...`) AND the sidecar user
 *   id matches the caller, reuse it — minting fresh on every launch would
 *   accumulate garbage in the user's tokens page.
 * - On user mismatch (or first run) call POST /api/tokens with the JWT to
 *   mint a fresh PAT, overwriting any stale cached PAT. This is the critical
 *   path: without it, a previous user's PAT would be used by a new session.
 * - If the caller happens to pass a PAT directly, write it through.
 * - When we mint fresh and a daemon is already running, restart it so the
 *   new credentials take effect (the Go daemon reads config at startup).
 */
async function syncToken(
  tokenFromRenderer: string,
  userId: string,
): Promise<void> {
  const active = await ensureActiveProfile();
  const config = await readProfileConfig(active.name);
  const previousUserId = await readProfileUserId(active.name);
  const userChanged = Boolean(previousUserId) && previousUserId !== userId;
  const sameUserWithCachedPat =
    !userChanged &&
    previousUserId === userId &&
    typeof config.token === "string" &&
    config.token.startsWith("mul_");

  let finalToken: string;
  if (tokenFromRenderer.startsWith("mul_")) {
    finalToken = tokenFromRenderer;
  } else if (sameUserWithCachedPat) {
    finalToken = config.token as string;
  } else {
    try {
      finalToken = await mintPat(tokenFromRenderer);
      console.log(
        `[daemon] minted PAT for profile "${active.name}" (user_changed=${userChanged})`,
      );
    } catch (err) {
      console.error("[daemon] failed to mint PAT:", err);
      throw err;
    }
  }

  config.token = finalToken;
  if (targetApiBaseUrl) config.server_url = targetApiBaseUrl;
  await writeProfileConfig(active.name, config);
  await writeProfileUserId(active.name, userId);

  // If we just rotated credentials onto a running daemon, restart it so the
  // in-memory token in the Go process matches the new config.
  if (userChanged) {
    try {
      const existing = await fetchHealthAtPort(active.port);
      if (existing?.status === "running") {
        console.log(
          "[daemon] user switched — restarting daemon with new credentials",
        );
        void restartDaemon();
      }
    } catch (err) {
      console.warn("[daemon] restart-on-user-switch failed:", err);
    }
  }
}

async function loadPrefs(): Promise<DaemonPrefs> {
  try {
    const raw = await readFile(PREFS_PATH, "utf-8");
    const parsed = JSON.parse(raw);
    return { ...DEFAULT_PREFS, ...parsed };
  } catch {
    return { ...DEFAULT_PREFS };
  }
}

async function savePrefs(prefs: DaemonPrefs): Promise<void> {
  const dir = join(homedir(), ".multica");
  await mkdir(dir, { recursive: true });
  await writeFile(PREFS_PATH, JSON.stringify(prefs, null, 2), "utf-8");
}

async function clearToken(): Promise<void> {
  const active = await ensureActiveProfile();
  const config = await readProfileConfig(active.name);
  if ("token" in config) {
    delete config.token;
    await writeProfileConfig(active.name, config);
  }
  // Always drop the sidecar so a subsequent syncToken from any user is
  // treated as a fresh mint, not a reuse of a stale cached PAT.
  await removeProfileUserId(active.name);
}

async function withGuard<T>(fn: () => Promise<T>): Promise<T | { success: false; error: string }> {
  if (operationInProgress) {
    return { success: false, error: "Another daemon operation is in progress" };
  }
  operationInProgress = true;
  try {
    return await fn();
  } finally {
    operationInProgress = false;
  }
}

function profileArgs(active: ActiveProfile): string[] {
  return active.name ? ["--profile", active.name] : [];
}

// Env passed to every CLI child so the daemon process knows it was spawned
// by the Desktop app. The server uses this to mark runtimes as managed and
// hide CLI self-update UI.
const DESKTOP_SPAWN_ENV = {
  ...process.env,
  MULTICA_LAUNCHED_BY: "desktop",
};

async function startDaemon(): Promise<{ success: boolean; error?: string }> {
  const bin = await resolveCliBinary();
  if (!bin) return { success: false, error: "multica CLI is not installed" };

  const active = await ensureActiveProfile();
  const existing = await fetchHealthAtPort(active.port);
  if (existing?.status === "running") {
    pollOnce();
    return { success: true };
  }

  currentState = "starting";
  sendStatus({ state: "starting" });

  const args = ["daemon", "start", ...profileArgs(active)];

  return new Promise((resolve) => {
    execFile(
      bin,
      args,
      { timeout: 20_000, env: DESKTOP_SPAWN_ENV },
      (err) => {
        if (err) {
          currentState = "stopped";
          sendStatus({ state: "stopped" });
          resolve({ success: false, error: err.message });
          return;
        }
        // Stay in "starting" until pollOnce confirms /health — the CLI
        // returning 0 only means the supervisor was spawned, not that the
        // daemon process is already listening.
        pollOnce();
        resolve({ success: true });
      },
    );
  });
}

async function stopDaemon(): Promise<{ success: boolean; error?: string }> {
  const bin = await resolveCliBinary();
  if (!bin) return { success: false, error: "multica CLI is not installed" };

  const active = await ensureActiveProfile();
  currentState = "stopping";
  sendStatus({ state: "stopping" });

  const args = ["daemon", "stop", ...profileArgs(active)];

  return new Promise((resolve) => {
    execFile(bin, args, { timeout: 15_000 }, (err) => {
      if (err) {
        resolve({ success: false, error: err.message });
      } else {
        resolve({ success: true });
      }
      currentState = "stopped";
      sendStatus({ state: "stopped" });
    });
  });
}

async function restartDaemon(): Promise<{ success: boolean; error?: string }> {
  const stopResult = await stopDaemon();
  if (!stopResult.success) return stopResult;
  return startDaemon();
}

async function pollOnce(): Promise<void> {
  const status = await fetchHealth();
  currentState = status.state;
  sendStatus(status);
  // Retry a deferred version-mismatch restart once the daemon drains.
  if (pendingVersionRestart && status.state === "running") {
    void ensureRunningDaemonVersionMatches();
  }
}

function startPolling(): void {
  if (statusPollTimer) return;
  pollOnce();
  statusPollTimer = setInterval(pollOnce, POLL_INTERVAL_MS);
}

/**
 * Ensures the CLI binary is available, then transitions into the normal
 * stopped/running state machine. Called once at startup and again on
 * user-triggered `daemon:retry-install`.
 */
async function bootstrapCli(): Promise<void> {
  const bin = await resolveCliBinary();
  if (!bin) {
    currentState = "cli_not_found";
    sendStatus({ state: "cli_not_found" });
    return;
  }
  currentState = "stopped";
  sendStatus({ state: "stopped" });
  startPolling();
}

function stopPolling(): void {
  if (statusPollTimer) {
    clearInterval(statusPollTimer);
    statusPollTimer = null;
  }
}

const LOG_TAIL_INITIAL_WINDOW_BYTES = 32 * 1024;
const LOG_TAIL_INITIAL_LINES = 200;
const LOG_TAIL_POLL_MS = 500;

async function readLogRange(
  path: string,
  startAt: number,
  length: number,
): Promise<string> {
  const handle = await open(path, "r");
  try {
    const buffer = Buffer.alloc(length);
    const { bytesRead } = await handle.read(buffer, 0, length, startAt);
    return buffer.subarray(0, bytesRead).toString("utf-8");
  } finally {
    await handle.close();
  }
}

function sendLines(win: BrowserWindow, text: string): void {
  const lines = text.split("\n").filter((line) => line.length > 0);
  for (const line of lines) {
    win.webContents.send("daemon:log-line", line);
  }
}

// Cross-platform tail -f replacement: read the tail of the file once, then
// poll its stat with fs.watchFile and forward any new bytes since the last
// known offset. watchFile works on macOS, Linux, and Windows; spawn("tail")
// would silently fail on Windows.
function startLogTail(win: BrowserWindow, retryCount = 0): void {
  stopLogTail();

  void ensureActiveProfile().then(async (active) => {
    const logPath = profileLogPath(active.name);
    if (!existsSync(logPath)) {
      if (retryCount < LOG_TAIL_MAX_RETRIES) {
        setTimeout(() => startLogTail(win, retryCount + 1), LOG_TAIL_RETRY_MS);
      }
      return;
    }

    let position = 0;
    try {
      const initialStats = await stat(logPath);
      const windowBytes = Math.min(
        initialStats.size,
        LOG_TAIL_INITIAL_WINDOW_BYTES,
      );
      const startAt = initialStats.size - windowBytes;
      if (windowBytes > 0) {
        const text = await readLogRange(logPath, startAt, windowBytes);
        const lines = text
          .split("\n")
          .filter((line) => line.length > 0)
          .slice(-LOG_TAIL_INITIAL_LINES);
        for (const line of lines) {
          win.webContents.send("daemon:log-line", line);
        }
      }
      position = initialStats.size;
    } catch (err) {
      console.warn("[daemon] log tail initial read failed:", err);
      return;
    }

    const listener: StatsListener = (curr) => {
      const target = getMainWindow();
      if (!target) return;
      // File rotated/truncated — restart from the new beginning.
      if (curr.size < position) position = 0;
      if (curr.size === position) return;
      const from = position;
      const length = curr.size - from;
      position = curr.size;
      readLogRange(logPath, from, length)
        .then((text) => sendLines(target, text))
        .catch((err) => {
          console.warn("[daemon] log tail read failed:", err);
        });
    };

    watchFile(logPath, { interval: LOG_TAIL_POLL_MS }, listener);
    logTailWatcher = { path: logPath, listener };
  });
}

function stopLogTail(): void {
  if (logTailWatcher) {
    unwatchFile(logTailWatcher.path, logTailWatcher.listener);
    logTailWatcher = null;
  }
}

export function setupDaemonManager(
  windowGetter: () => BrowserWindow | null,
): void {
  getMainWindow = windowGetter;

  ipcMain.handle("daemon:set-target-api-url", async (_e, url: string) => {
    const normalized = url || null;
    if (targetApiBaseUrl !== normalized) {
      console.log(`[daemon] target API URL set to ${normalized ?? "(none)"}`);
      targetApiBaseUrl = normalized;
      invalidateActiveProfile();
      await pollOnce();
    }
  });
  ipcMain.handle("daemon:start", () => withGuard(() => startDaemon()));
  ipcMain.handle("daemon:stop", () => withGuard(() => stopDaemon()));
  ipcMain.handle("daemon:restart", () => withGuard(() => restartDaemon()));
  ipcMain.handle("daemon:get-status", () => fetchHealth());
  ipcMain.handle(
    "daemon:sync-token",
    (_event, token: string, userId: string) => syncToken(token, userId),
  );
  ipcMain.handle("daemon:clear-token", () => clearToken());
  ipcMain.handle("daemon:is-cli-installed", async () => {
    const bin = await resolveCliBinary();
    return bin !== null;
  });
  ipcMain.handle("daemon:retry-install", async () => {
    cachedCliBinary = undefined;
    cliResolvePromise = null;
    // A retry-install may land a new CLI at a different version; drop the
    // cached version string so the next check re-reads the binary.
    cachedCliBinaryVersion = undefined;
    await bootstrapCli();
  });
  ipcMain.handle("daemon:get-prefs", () => loadPrefs());
  ipcMain.handle(
    "daemon:set-prefs",
    (_event, prefs: Partial<DaemonPrefs>) =>
      loadPrefs().then((cur) => {
        const merged = { ...cur, ...prefs };
        return savePrefs(merged).then(() => merged);
      }),
  );
  ipcMain.handle("daemon:auto-start", async () => {
    const prefs = await loadPrefs();
    if (!prefs.autoStart) return;
    const bin = await resolveCliBinary();
    if (!bin) return;
    const health = await fetchHealth();
    if (health.state === "running") {
      // Daemon is up but may be running an older CLI than the one we just
      // bundled. Restart it so the new binary actually takes effect.
      await ensureRunningDaemonVersionMatches();
      return;
    }
    await startDaemon();
  });

  ipcMain.on("daemon:start-log-stream", () => {
    const win = getMainWindow();
    if (win) startLogTail(win);
  });

  ipcMain.on("daemon:stop-log-stream", () => {
    stopLogTail();
  });

  // First-run CLI install kicks off here. Status bar shows "Setting up…"
  // until the managed binary is on disk (instant on subsequent launches).
  currentState = "installing_cli";
  sendStatus({ state: "installing_cli" });
  void bootstrapCli();

  let isQuitting = false;
  app.on("before-quit", (event) => {
    if (isQuitting) return;
    stopPolling();
    stopLogTail();

    loadPrefs().then(async (prefs) => {
      if (prefs.autoStop) {
        isQuitting = true;
        event.preventDefault();
        try {
          await stopDaemon();
        } catch {
          // Best-effort stop on quit
        }
        app.quit();
      }
    });
  });
}
