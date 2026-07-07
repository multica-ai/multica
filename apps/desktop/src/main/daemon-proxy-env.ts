import { execFileSync } from "child_process";

const PROXY_READ_TIMEOUT_MS = 2_000;
const PROXY_READ_MAX_BUFFER = 64 * 1024;

interface ParsedMacProxySettings {
  httpProxy?: string;
  httpsProxy?: string;
  noProxy?: string;
}

function envValue(env: NodeJS.ProcessEnv, key: string): string {
  return typeof env[key] === "string" ? env[key].trim() : "";
}

function proxyURL(host: string | undefined, port: string | undefined): string {
  const cleanHost = (host ?? "").trim();
  const cleanPort = (port ?? "").trim();
  if (!cleanHost || !cleanPort) return "";
  if (/^[a-z][a-z0-9+.-]*:\/\//i.test(cleanHost)) return cleanHost;
  const formattedHost =
    cleanHost.includes(":") && !cleanHost.startsWith("[")
      ? `[${cleanHost}]`
      : cleanHost;
  return `http://${formattedHost}:${cleanPort}`;
}

function readMacSystemProxy(): string {
  try {
    return execFileSync("/usr/sbin/scutil", ["--proxy"], {
      encoding: "utf8",
      timeout: PROXY_READ_TIMEOUT_MS,
      maxBuffer: PROXY_READ_MAX_BUFFER,
      stdio: ["ignore", "pipe", "ignore"],
    });
  } catch {
    return "";
  }
}

function parseMacSystemProxy(raw: string): ParsedMacProxySettings {
  const values: Record<string, string> = {};
  const exceptions: string[] = [];
  let inExceptions = false;

  for (const line of raw.split(/\r?\n/)) {
    const trimmed = line.trim();
    if (!trimmed) continue;

    if (/^ExceptionsList\s*:\s*<array>/.test(trimmed)) {
      inExceptions = true;
      continue;
    }

    if (inExceptions) {
      if (trimmed === "}") {
        inExceptions = false;
        continue;
      }
      const exception = trimmed.match(/^\d+\s*:\s*(.+)$/);
      if (exception?.[1]) exceptions.push(exception[1].trim());
      continue;
    }

    const pair = trimmed.match(/^([A-Za-z0-9]+)\s*:\s*(.*)$/);
    if (pair?.[1]) values[pair[1]] = (pair[2] ?? "").trim();
  }

  const noProxy = exceptions.slice();
  if (values.ExcludeSimpleHostnames === "1") noProxy.push("localhost");

  return {
    httpProxy:
      values.HTTPEnable === "1"
        ? proxyURL(values.HTTPProxy, values.HTTPPort)
        : undefined,
    httpsProxy:
      values.HTTPSEnable === "1"
        ? proxyURL(values.HTTPSProxy, values.HTTPSPort)
        : undefined,
    noProxy: noProxy.length > 0 ? Array.from(new Set(noProxy)).join(",") : undefined,
  };
}

function setProxyPair(
  env: NodeJS.ProcessEnv,
  upperKey: string,
  lowerKey: string,
  fallbackValue: string | undefined,
): boolean {
  const upper = envValue(env, upperKey);
  const lower = envValue(env, lowerKey);

  if (upper && !lower) {
    env[lowerKey] = upper;
    return true;
  }
  if (!upper && lower) {
    env[upperKey] = lower;
    return true;
  }
  if (!upper && !lower && fallbackValue) {
    env[upperKey] = fallbackValue;
    env[lowerKey] = fallbackValue;
    return true;
  }
  return false;
}

function addMacSystemProxyEnv(
  env: NodeJS.ProcessEnv,
  readProxy: () => string,
): string[] {
  const settings = parseMacSystemProxy(readProxy());
  const applied: string[] = [];

  if (setProxyPair(env, "HTTP_PROXY", "http_proxy", settings.httpProxy)) {
    applied.push("HTTP_PROXY");
  }
  if (setProxyPair(env, "HTTPS_PROXY", "https_proxy", settings.httpsProxy)) {
    applied.push("HTTPS_PROXY");
  }
  if (setProxyPair(env, "NO_PROXY", "no_proxy", settings.noProxy)) {
    applied.push("NO_PROXY");
  }

  return applied;
}

// Env passed to every CLI child so the daemon process knows it was spawned
// by the Desktop app. On macOS, GUI-launched apps may not inherit the shell's
// proxy variables, so fill missing proxy env from System Settings at spawn time.
// Computed lazily so it picks up the PATH fix applied by fix-path in main/index.ts.
export function desktopSpawnEnv(
  baseEnv: NodeJS.ProcessEnv = process.env,
  platform: NodeJS.Platform = process.platform,
  readProxy: () => string = readMacSystemProxy,
): NodeJS.ProcessEnv {
  const env = { ...baseEnv, MULTICA_LAUNCHED_BY: "desktop" };

  if (platform === "darwin") {
    const applied = addMacSystemProxyEnv(env, readProxy);
    if (applied.length > 0) {
      console.log(
        `[daemon] inherited macOS system proxy env for daemon (${applied.join(", ")})`,
      );
    }
  }

  return env;
}
