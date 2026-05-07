// Loads connection config (API URL, auth token, default workspace) for
// the MCP server. Two sources, first-wins:
//
//  1. Environment variables — `MULTICA_API_URL`, `MULTICA_TOKEN`,
//     `MULTICA_WORKSPACE_ID`. Takes priority because operators may want
//     to point a single MCP install at a different workspace than the
//     CLI is configured for (e.g. one Claude Desktop, two workspaces).
//  2. `~/.multica/config.json` — the same file the multica CLI writes
//     on `multica login`. Lets users skip env-var setup entirely if
//     their CLI is already authenticated. The JSON has a WebSocket
//     `server_url` (e.g. `wss://api.example/ws`); we derive the HTTP
//     base by swapping the scheme and stripping the trailing `/ws`.
//
// Throws TokenRequiredError when neither source yields a token —
// without one every API call would 401 and the MCP tools would be
// useless.

import { homedir } from "node:os";
import { join } from "node:path";
import { readFileSync } from "node:fs";

export interface MulticaConfig {
  /** HTTP base URL, e.g. https://multica-api.example.com (no trailing slash). */
  apiUrl: string;
  /** Bearer token (`mul_...`). Sent as Authorization: Bearer <token>. */
  token: string;
  /** Default workspace UUID for tools that don't take an explicit override. */
  defaultWorkspaceId: string | null;
}

export class ConfigError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "ConfigError";
  }
}

interface RawCliConfig {
  server_url?: string;
  app_url?: string;
  token?: string;
  workspace_id?: string;
}

export interface LoadConfigOptions {
  /** Overrides for testing — when set, env + file lookups are skipped for that field. */
  apiUrl?: string;
  token?: string;
  workspaceId?: string;
  /** Override the location of ~/.multica/config.json (test seam). */
  cliConfigPath?: string;
  /** Mock `process.env` (test seam). Defaults to the real process.env. */
  env?: Record<string, string | undefined>;
}

const DEFAULT_CLI_CONFIG = join(homedir(), ".multica", "config.json");

export function loadConfig(opts: LoadConfigOptions = {}): MulticaConfig {
  const env = opts.env ?? process.env;
  const cliPath = opts.cliConfigPath ?? DEFAULT_CLI_CONFIG;

  // Best-effort read of the CLI config; missing/bad JSON is fine —
  // env vars or explicit overrides may still satisfy us.
  let cli: RawCliConfig = {};
  try {
    const raw = readFileSync(cliPath, "utf8");
    const parsed = JSON.parse(raw) as unknown;
    if (parsed && typeof parsed === "object") {
      cli = parsed as RawCliConfig;
    }
  } catch {
    // File missing or unreadable — fall through to env.
  }

  const apiUrl =
    opts.apiUrl ??
    env.MULTICA_API_URL ??
    deriveHttpBase(cli.server_url) ??
    "";

  const token = opts.token ?? env.MULTICA_TOKEN ?? cli.token ?? "";
  const defaultWorkspaceId =
    opts.workspaceId ?? env.MULTICA_WORKSPACE_ID ?? cli.workspace_id ?? null;

  if (!apiUrl) {
    throw new ConfigError(
      "Multica API URL is not configured. Set MULTICA_API_URL or run `multica login` to populate ~/.multica/config.json.",
    );
  }
  if (!token) {
    throw new ConfigError(
      "Multica auth token is not configured. Set MULTICA_TOKEN or run `multica login` to populate ~/.multica/config.json.",
    );
  }

  return {
    apiUrl: stripTrailingSlash(apiUrl),
    token,
    defaultWorkspaceId,
  };
}

// The CLI stores a WebSocket URL like `wss://api.example.com/ws`. The
// HTTP API lives at the same host with the matching scheme and no
// trailing `/ws`. Returns null on missing/bad input so the caller can
// fall through to env-only setups.
export function deriveHttpBase(serverUrl: string | undefined): string | null {
  if (!serverUrl) return null;
  let url = serverUrl.trim();
  if (!url) return null;
  if (url.startsWith("wss://")) url = "https://" + url.slice("wss://".length);
  else if (url.startsWith("ws://")) url = "http://" + url.slice("ws://".length);
  // Strip a trailing `/ws` (and optionally a `/`) so the CLI's WebSocket
  // suffix doesn't end up on every REST path.
  url = url.replace(/\/ws\/?$/, "");
  return stripTrailingSlash(url);
}

function stripTrailingSlash(s: string): string {
  return s.endsWith("/") ? s.slice(0, -1) : s;
}
