import { app, ipcMain, shell } from "electron";
import { execFile, spawn, type ChildProcessWithoutNullStreams } from "child_process";
import { randomBytes } from "crypto";
import { createWriteStream, existsSync, type WriteStream } from "fs";
import { access, cp, mkdir, readFile, writeFile } from "fs/promises";
import { join } from "path";
import { promisify } from "util";
import type {
  TaobaoBridgeAgentEnvironment,
  TaobaoBridgeConfigInput,
  TaobaoBridgePublicConfig,
  TaobaoBridgeStatus,
  TaobaoWorkflowAssetInstallResult,
} from "../shared/taobao-bridge-types";

const execFileAsync = promisify(execFile);

const DEFAULT_PORT = 8090;
const SKILL_KEY = "taobao-order-ops";
const STATUS_TIMEOUT_MS = 1_500;
const START_HEALTH_TIMEOUT_MS = 30_000;
const POLL_INTERVAL_MS = 750;

const ORDERED_ENV_KEYS = [
  "APP_NAME",
  "ENV",
  "HOST",
  "PORT",
  "TAOBAO_EVENT_SECRET",
  "ORDER_BRIDGE_API_TOKEN",
  "REQUIRE_ORDER_BRIDGE_API_AUTH",
  "CORS_ALLOW_ORIGINS",
  "MULTICA_AUTOPILOT_WEBHOOK_URL",
  "ORDER_BRIDGE_BASE_URL",
  "ORDER_API_BASE_URL",
  "ORDER_API_TOKEN",
  "ORDER_API_GET_ORDER_PATH",
  "ORDER_API_TIMEOUT_SECONDS",
  "ORDER_API_AUTH_HEADER",
  "ORDER_API_AUTH_SCHEME",
  "ORDER_API_WRITE_THROUGH",
  "DEDUPE_DB_PATH",
  "ALLOW_PLAIN_RECEIVER_INFO",
  "STORE_PLAIN_RECEIVER_IN_ACTION_LOG",
  "ALLOW_HIGH_RISK_ACTIONS",
  "REMOTE_AREA_KEYWORDS",
  "UNSUPPORTED_AREA_KEYWORDS",
];

type EnvMap = Record<string, string>;

interface RuntimePaths {
  bundledRoot: string;
  userRoot: string;
  runtimeDir: string;
  venvDir: string;
  venvPython: string;
  envPath: string;
  dataDir: string;
  logDir: string;
  logPath: string;
  skillDir: string;
}

let bridgeProcess: ChildProcessWithoutNullStreams | null = null;
let bridgeLogStream: WriteStream | null = null;
let currentStatus: TaobaoBridgeStatus = { state: "stopped" };
let operationChain: Promise<unknown> = Promise.resolve();

function paths(): RuntimePaths {
  const userRoot = join(app.getPath("userData"), "taobao-order-worker");
  const runtimeDir = join(userRoot, "runtime");
  const venvDir = join(userRoot, ".venv");
  return {
    bundledRoot: join(__dirname, "../../resources/taobao-order-worker-v1").replace(
      "app.asar",
      "app.asar.unpacked",
    ),
    userRoot,
    runtimeDir,
    venvDir,
    venvPython:
      process.platform === "win32"
        ? join(venvDir, "Scripts", "python.exe")
        : join(venvDir, "bin", "python"),
    envPath: join(userRoot, ".env"),
    dataDir: join(userRoot, "data"),
    logDir: join(userRoot, "logs"),
    logPath: join(userRoot, "logs", "bridge.log"),
    skillDir: join(app.getPath("home"), ".agents", "skills", SKILL_KEY),
  };
}

function setStatus(status: TaobaoBridgeStatus): TaobaoBridgeStatus {
  currentStatus = status;
  return status;
}

function serializeOperation<T>(fn: () => Promise<T>): Promise<T> {
  const next = operationChain.then(fn, fn);
  operationChain = next.catch(() => undefined);
  return next;
}

async function exists(path: string): Promise<boolean> {
  try {
    await access(path);
    return true;
  } catch {
    return false;
  }
}

function randomSecret(): string {
  return randomBytes(32).toString("hex");
}

function parseEnv(content: string): EnvMap {
  const env: EnvMap = {};
  for (const line of content.split(/\r?\n/)) {
    const trimmed = line.trim();
    if (!trimmed || trimmed.startsWith("#")) continue;
    const index = trimmed.indexOf("=");
    if (index <= 0) continue;
    const key = trimmed.slice(0, index).trim();
    let value = trimmed.slice(index + 1).trim();
    if (
      (value.startsWith('"') && value.endsWith('"')) ||
      (value.startsWith("'") && value.endsWith("'"))
    ) {
      value = value.slice(1, -1);
    }
    env[key] = value;
  }
  return env;
}

function serializeEnv(env: EnvMap): string {
  const keys = [
    ...ORDERED_ENV_KEYS,
    ...Object.keys(env).filter((key) => !ORDERED_ENV_KEYS.includes(key)).sort(),
  ];
  const uniqueKeys = [...new Set(keys)].filter((key) => env[key] !== undefined);
  return `${uniqueKeys.map((key) => `${key}=${env[key] ?? ""}`).join("\n")}\n`;
}

async function readEnv(): Promise<EnvMap> {
  const { envPath } = paths();
  try {
    return parseEnv(await readFile(envPath, "utf8"));
  } catch {
    return {};
  }
}

async function writeEnv(env: EnvMap): Promise<void> {
  const { userRoot, envPath } = paths();
  await mkdir(userRoot, { recursive: true });
  await writeFile(envPath, serializeEnv(env), { mode: 0o600 });
}

function numberFromEnv(value: string | undefined, fallback: number): number {
  const parsed = Number(value);
  if (!Number.isFinite(parsed)) return fallback;
  return Math.max(1, Math.min(65535, Math.floor(parsed)));
}

function boolFromEnv(value: string | undefined, fallback: boolean): boolean {
  if (value === undefined || value === "") return fallback;
  return ["1", "true", "yes", "on"].includes(value.toLowerCase());
}

function applyDefaults(input: EnvMap): EnvMap {
  const p = paths();
  const port = numberFromEnv(input.PORT, DEFAULT_PORT);
  return {
    ...input,
    APP_NAME: input.APP_NAME || "taobao-order-worker-v1",
    ENV: input.ENV || "dev",
    HOST: "127.0.0.1",
    PORT: String(port),
    TAOBAO_EVENT_SECRET: input.TAOBAO_EVENT_SECRET || randomSecret(),
    ORDER_BRIDGE_API_TOKEN: input.ORDER_BRIDGE_API_TOKEN || randomSecret(),
    REQUIRE_ORDER_BRIDGE_API_AUTH: "true",
    CORS_ALLOW_ORIGINS:
      input.CORS_ALLOW_ORIGINS ||
      "http://localhost:3000,http://localhost:3001,http://localhost:8080",
    MULTICA_AUTOPILOT_WEBHOOK_URL: input.MULTICA_AUTOPILOT_WEBHOOK_URL || "",
    ORDER_BRIDGE_BASE_URL: `http://127.0.0.1:${port}`,
    ORDER_API_BASE_URL: input.ORDER_API_BASE_URL || "",
    ORDER_API_TOKEN: input.ORDER_API_TOKEN || "",
    ORDER_API_GET_ORDER_PATH: input.ORDER_API_GET_ORDER_PATH || "/api/orders/{tid}",
    ORDER_API_TIMEOUT_SECONDS: input.ORDER_API_TIMEOUT_SECONDS || "10",
    ORDER_API_AUTH_HEADER: input.ORDER_API_AUTH_HEADER || "Authorization",
    ORDER_API_AUTH_SCHEME: input.ORDER_API_AUTH_SCHEME || "Bearer",
    ORDER_API_WRITE_THROUGH: String(boolFromEnv(input.ORDER_API_WRITE_THROUGH, false)),
    DEDUPE_DB_PATH: join(p.dataDir, "order_worker.sqlite3"),
    ALLOW_PLAIN_RECEIVER_INFO: String(boolFromEnv(input.ALLOW_PLAIN_RECEIVER_INFO, true)),
    STORE_PLAIN_RECEIVER_IN_ACTION_LOG: "false",
    ALLOW_HIGH_RISK_ACTIONS: "false",
    REMOTE_AREA_KEYWORDS:
      input.REMOTE_AREA_KEYWORDS || "新疆,西藏,内蒙古,青海,宁夏,甘肃",
    UNSUPPORTED_AREA_KEYWORDS:
      input.UNSUPPORTED_AREA_KEYWORDS || "香港,澳门,台湾,海外",
  };
}

async function ensureConfig(): Promise<EnvMap> {
  const env = applyDefaults(await readEnv());
  await writeEnv(env);
  return env;
}

function publicConfigFromEnv(env: EnvMap): TaobaoBridgePublicConfig {
  const p = paths();
  const port = numberFromEnv(env.PORT, DEFAULT_PORT);
  return {
    configured: Boolean(env.TAOBAO_EVENT_SECRET && env.ORDER_BRIDGE_API_TOKEN),
    port,
    baseUrl: env.ORDER_BRIDGE_BASE_URL || `http://127.0.0.1:${port}`,
    env: env.ENV || "dev",
    runtimeDir: p.runtimeDir,
    logPath: p.logPath,
    hasEventSecret: Boolean(env.TAOBAO_EVENT_SECRET),
    hasBridgeToken: Boolean(env.ORDER_BRIDGE_API_TOKEN),
    hasOrderApiToken: Boolean(env.ORDER_API_TOKEN),
    webhookConfigured: Boolean(env.MULTICA_AUTOPILOT_WEBHOOK_URL),
    orderApiBaseUrl: env.ORDER_API_BASE_URL || "",
    orderApiGetOrderPath: env.ORDER_API_GET_ORDER_PATH || "/api/orders/{tid}",
    orderApiAuthHeader: env.ORDER_API_AUTH_HEADER || "Authorization",
    orderApiAuthScheme: env.ORDER_API_AUTH_SCHEME || "Bearer",
    orderApiTimeoutSeconds: numberFromEnv(env.ORDER_API_TIMEOUT_SECONDS, 10),
    orderApiWriteThrough: boolFromEnv(env.ORDER_API_WRITE_THROUGH, false),
    allowPlainReceiverInfo: boolFromEnv(env.ALLOW_PLAIN_RECEIVER_INFO, true),
    remoteAreaKeywords: env.REMOTE_AREA_KEYWORDS || "",
    unsupportedAreaKeywords: env.UNSUPPORTED_AREA_KEYWORDS || "",
  };
}

function redact(text: string, env?: EnvMap): string {
  let output = text;
  for (const key of [
    "TAOBAO_EVENT_SECRET",
    "ORDER_BRIDGE_API_TOKEN",
    "ORDER_API_TOKEN",
    "PASSWORD",
    "COOKIE",
    "APP_SECRET",
  ]) {
    const value = env?.[key];
    if (value && value.length >= 4) {
      output = output.split(value).join("[redacted]");
    }
  }
  return output.replace(
    /\b(token|secret|password|cookie|app_secret)=([^\s]+)/gi,
    "$1=[redacted]",
  );
}

async function appendLog(line: string, env?: EnvMap): Promise<void> {
  const p = paths();
  await mkdir(p.logDir, { recursive: true });
  await writeFile(p.logPath, `${new Date().toISOString()} ${redact(line, env)}\n`, {
    flag: "a",
  });
}

async function ensureWorkflowAssets(): Promise<TaobaoWorkflowAssetInstallResult> {
  const p = paths();
  if (!existsSync(p.bundledRoot)) {
    throw new Error(`Bundled Taobao worker resource not found: ${p.bundledRoot}`);
  }
  await mkdir(p.userRoot, { recursive: true });
  await cp(p.bundledRoot, p.runtimeDir, {
    recursive: true,
    force: true,
    filter: (source) => !source.includes(`${p.bundledRoot}/.venv`),
  });
  await mkdir(join(app.getPath("home"), ".agents", "skills"), { recursive: true });
  await cp(join(p.runtimeDir, "multica"), p.skillDir, {
    recursive: true,
    force: true,
  });
  return { runtimeDir: p.runtimeDir, skillDir: p.skillDir, skillKey: SKILL_KEY };
}

async function findPython(): Promise<string | null> {
  for (const candidate of ["python3", "python"]) {
    try {
      await execFileAsync(candidate, ["--version"], { timeout: 5_000 });
      return candidate;
    } catch {
      // Try the next candidate.
    }
  }
  return null;
}

async function ensureVenv(python: string, env: EnvMap): Promise<void> {
  const p = paths();
  if (!(await exists(p.venvPython))) {
    setStatus(statusFromEnv(env, "installing", "Creating Python virtual environment"));
    await appendLog("Creating Python virtual environment", env);
    await execFileAsync(python, ["-m", "venv", p.venvDir], { timeout: 120_000 });
  }
  setStatus(statusFromEnv(env, "installing", "Installing Taobao bridge requirements"));
  await appendLog("Installing requirements.txt", env);
  await execFileAsync(p.venvPython, ["-m", "pip", "install", "-r", join(p.runtimeDir, "requirements.txt")], {
    cwd: p.runtimeDir,
    timeout: 180_000,
  });
}

function statusFromEnv(
  env: EnvMap,
  state: TaobaoBridgeStatus["state"],
  message?: string,
): TaobaoBridgeStatus {
  const p = paths();
  const port = numberFromEnv(env.PORT, DEFAULT_PORT);
  return {
    state,
    port,
    baseUrl: env.ORDER_BRIDGE_BASE_URL || `http://127.0.0.1:${port}`,
    runtimeDir: p.runtimeDir,
    logPath: p.logPath,
    ...(bridgeProcess?.pid ? { pid: bridgeProcess.pid } : {}),
    ...(message ? { message: redact(message, env) } : {}),
  };
}

async function probeHealth(env: EnvMap, timeoutMs = STATUS_TIMEOUT_MS): Promise<boolean> {
  const port = numberFromEnv(env.PORT, DEFAULT_PORT);
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), timeoutMs);
  try {
    const response = await fetch(`http://127.0.0.1:${port}/health`, {
      signal: controller.signal,
    });
    return response.ok;
  } catch {
    return false;
  } finally {
    clearTimeout(timeout);
  }
}

async function getStatus(): Promise<TaobaoBridgeStatus> {
  const env = await readEnv();
  if (!env.TAOBAO_EVENT_SECRET || !env.ORDER_BRIDGE_API_TOKEN) {
    return setStatus(statusFromEnv(applyDefaults(env), "not_configured"));
  }
  const merged = applyDefaults(env);
  if (await probeHealth(merged)) {
    return setStatus({
      ...statusFromEnv(merged, "running"),
      ...(bridgeProcess?.pid ? { pid: bridgeProcess.pid } : {}),
      startedAt: currentStatus.startedAt,
    });
  }
  if (bridgeProcess) {
    return setStatus(statusFromEnv(merged, "unhealthy", "Process is running but /health did not respond"));
  }
  return setStatus(statusFromEnv(merged, "stopped"));
}

async function saveConfig(input: TaobaoBridgeConfigInput): Promise<TaobaoBridgePublicConfig> {
  const existing = applyDefaults(await readEnv());
  const next: EnvMap = { ...existing };

  if (typeof input.port === "number" && Number.isFinite(input.port)) {
    next.PORT = String(Math.max(1, Math.min(65535, Math.floor(input.port))));
  }
  if (input.multicaAutopilotWebhookUrl !== undefined) {
    next.MULTICA_AUTOPILOT_WEBHOOK_URL = input.multicaAutopilotWebhookUrl.trim();
  }
  if (input.orderApiBaseUrl !== undefined) {
    next.ORDER_API_BASE_URL = input.orderApiBaseUrl.trim().replace(/\/+$/, "");
  }
  if (input.orderApiToken !== undefined && input.orderApiToken.trim()) {
    next.ORDER_API_TOKEN = input.orderApiToken.trim();
  }
  if (input.orderApiGetOrderPath !== undefined) {
    next.ORDER_API_GET_ORDER_PATH = input.orderApiGetOrderPath.trim() || "/api/orders/{tid}";
  }
  if (input.orderApiAuthHeader !== undefined) {
    next.ORDER_API_AUTH_HEADER = input.orderApiAuthHeader.trim() || "Authorization";
  }
  if (input.orderApiAuthScheme !== undefined) {
    next.ORDER_API_AUTH_SCHEME = input.orderApiAuthScheme.trim() || "Bearer";
  }
  if (typeof input.orderApiTimeoutSeconds === "number" && Number.isFinite(input.orderApiTimeoutSeconds)) {
    next.ORDER_API_TIMEOUT_SECONDS = String(Math.max(1, Math.floor(input.orderApiTimeoutSeconds)));
  }
  if (typeof input.orderApiWriteThrough === "boolean") {
    next.ORDER_API_WRITE_THROUGH = String(input.orderApiWriteThrough);
  }
  if (typeof input.allowPlainReceiverInfo === "boolean") {
    next.ALLOW_PLAIN_RECEIVER_INFO = String(input.allowPlainReceiverInfo);
  }
  if (input.remoteAreaKeywords !== undefined) {
    next.REMOTE_AREA_KEYWORDS = input.remoteAreaKeywords.trim();
  }
  if (input.unsupportedAreaKeywords !== undefined) {
    next.UNSUPPORTED_AREA_KEYWORDS = input.unsupportedAreaKeywords.trim();
  }

  const withDefaults = applyDefaults(next);
  await writeEnv(withDefaults);
  return publicConfigFromEnv(withDefaults);
}

async function startBridge(): Promise<TaobaoBridgeStatus> {
  return serializeOperation(async () => {
    const existingStatus = await getStatus();
    if (existingStatus.state === "running" && bridgeProcess) return existingStatus;

    await ensureWorkflowAssets();
    const env = await ensureConfig();
    const python = await findPython();
    if (!python) {
      await appendLog("Python 3 was not found; bridge start skipped", env);
      return setStatus(statusFromEnv(env, "python_missing", "Python 3 is required to run the Taobao bridge"));
    }

    try {
      await ensureVenv(python, env);
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      await appendLog(`Bridge dependency install failed: ${message}`, env);
      return setStatus(statusFromEnv(env, "error", `Install failed: ${message}`));
    }

    const p = paths();
    await mkdir(p.logDir, { recursive: true });
    bridgeLogStream?.end();
    bridgeLogStream = createWriteStream(p.logPath, { flags: "a" });

    setStatus(statusFromEnv(env, "starting", "Starting Taobao order bridge"));
    await appendLog("Starting uvicorn bridge process", env);
    bridgeProcess = spawn(
      p.venvPython,
      [
        "-m",
        "uvicorn",
        "app:app",
        "--env-file",
        p.envPath,
        "--host",
        "127.0.0.1",
        "--port",
        String(numberFromEnv(env.PORT, DEFAULT_PORT)),
      ],
      {
        cwd: p.runtimeDir,
        env: { ...process.env, ...env },
      },
    );

    const startedAt = new Date().toISOString();
    bridgeProcess.stdout.on("data", (chunk: Buffer) => {
      bridgeLogStream?.write(redact(chunk.toString(), env));
    });
    bridgeProcess.stderr.on("data", (chunk: Buffer) => {
      bridgeLogStream?.write(redact(chunk.toString(), env));
    });
    bridgeProcess.on("exit", (code, signal) => {
      const message = `Bridge process exited code=${code ?? "null"} signal=${signal ?? "null"}`;
      bridgeLogStream?.write(`${new Date().toISOString()} ${message}\n`);
      bridgeLogStream?.end();
      bridgeLogStream = null;
      bridgeProcess = null;
      setStatus(statusFromEnv(env, code === 0 ? "stopped" : "error", message));
    });

    const start = Date.now();
    while (Date.now() - start < START_HEALTH_TIMEOUT_MS) {
      if (await probeHealth(env, POLL_INTERVAL_MS)) {
        return setStatus({
          ...statusFromEnv(env, "running"),
          pid: bridgeProcess.pid,
          startedAt,
        });
      }
      await new Promise((resolve) => setTimeout(resolve, POLL_INTERVAL_MS));
    }

    return setStatus(statusFromEnv(env, "unhealthy", "Started process, but /health did not respond"));
  });
}

async function stopBridge(): Promise<TaobaoBridgeStatus> {
  return serializeOperation(async () => {
    const env = applyDefaults(await readEnv());
    if (!bridgeProcess) return setStatus(statusFromEnv(env, "stopped"));
    const proc = bridgeProcess;
    await appendLog("Stopping uvicorn bridge process", env);
    proc.kill();
    bridgeProcess = null;
    bridgeLogStream?.end();
    bridgeLogStream = null;
    return setStatus(statusFromEnv(env, "stopped"));
  });
}

async function restartBridge(): Promise<TaobaoBridgeStatus> {
  await stopBridge();
  return startBridge();
}

async function openLogFile(): Promise<{ success: boolean; error?: string }> {
  const p = paths();
  await mkdir(p.logDir, { recursive: true });
  if (!(await exists(p.logPath))) await writeFile(p.logPath, "");
  const error = await shell.openPath(p.logPath);
  return error ? { success: false, error } : { success: true };
}

async function getAgentEnvironment(): Promise<TaobaoBridgeAgentEnvironment> {
  const env = await ensureConfig();
  return {
    ORDER_BRIDGE_BASE_URL: env.ORDER_BRIDGE_BASE_URL || `http://127.0.0.1:${DEFAULT_PORT}`,
    ORDER_BRIDGE_API_TOKEN: env.ORDER_BRIDGE_API_TOKEN,
  };
}

export function setupTaobaoBridgeManager(): void {
  ipcMain.handle("taobao-bridge:get-status", () => getStatus());
  ipcMain.handle("taobao-bridge:get-config", async () => publicConfigFromEnv(await ensureConfig()));
  ipcMain.handle("taobao-bridge:save-config", async (_event, input: TaobaoBridgeConfigInput) =>
    saveConfig(input ?? {}),
  );
  ipcMain.handle("taobao-bridge:start", () => startBridge());
  ipcMain.handle("taobao-bridge:stop", () => stopBridge());
  ipcMain.handle("taobao-bridge:restart", () => restartBridge());
  ipcMain.handle("taobao-bridge:open-log-file", () => openLogFile());
  ipcMain.handle("taobao-bridge:install-assets", () => ensureWorkflowAssets());
  ipcMain.handle("taobao-bridge:get-agent-environment", () => getAgentEnvironment());

  app.on("before-quit", () => {
    if (bridgeProcess) bridgeProcess.kill();
    bridgeLogStream?.end();
  });
}
