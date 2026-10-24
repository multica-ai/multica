/**
 * Windows treats environment variable names case-insensitively, but plain
 * JS objects do not. Spreading `process.env` and then writing `env.PATH`
 * can leave both `Path` and `PATH` on the object; child processes may then
 * resolve the wrong one (e.g. a compat-bin-only PATH) and fail to find
 * agent CLIs. See #9250.
 */

export function resolveEnvPathKey(
  env: NodeJS.ProcessEnv,
  platform: NodeJS.Platform = process.platform,
): string {
  const existing = Object.keys(env).find((key) => key.toUpperCase() === "PATH");
  if (existing) return existing;
  return platform === "win32" ? "Path" : "PATH";
}

export function readEnvPath(env: NodeJS.ProcessEnv = process.env): string {
  const key = resolveEnvPathKey(env);
  const value = env[key];
  return typeof value === "string" ? value : "";
}

/**
 * Prepend `dir` onto a PATH string. Returns `pathValue` unchanged when `dir`
 * is missing, or when `pathValue` is empty/undefined — never degrade to
 * "alias directory only", which hides the real PATH on Windows (#9250).
 */
export function withAliasDirOnPath(
  pathValue: string | undefined,
  dir: string | undefined | null,
  platform: NodeJS.Platform = process.platform,
): string | undefined {
  if (!dir) return pathValue;
  if (pathValue === undefined || pathValue === "") return pathValue;

  const sep = platform === "win32" ? ";" : ":";
  const entries = pathValue.split(sep).filter(Boolean);
  if (entries[0] === dir) return pathValue;
  return [dir, ...entries.filter((entry) => entry !== dir)].join(sep);
}

/**
 * Collapse any Path/PATH key variants into a single key and optionally
 * prepend an alias directory. Safe to pass straight into child_process env.
 */
export function applyPathToEnv(
  env: NodeJS.ProcessEnv,
  options: {
    aliasDir?: string | null;
    platform?: NodeJS.Platform;
  } = {},
): NodeJS.ProcessEnv {
  const platform = options.platform ?? process.platform;
  const next: NodeJS.ProcessEnv = { ...env };

  let merged = "";
  for (const key of Object.keys(next)) {
    if (key.toUpperCase() !== "PATH") continue;
    const value = next[key];
    if (typeof value === "string" && value.length > merged.length) {
      merged = value;
    }
    delete next[key];
  }

  const pathKey = platform === "win32" ? "Path" : "PATH";
  const withAlias = withAliasDirOnPath(merged || undefined, options.aliasDir, platform);
  if (withAlias !== undefined && withAlias !== "") {
    next[pathKey] = withAlias;
  } else if (merged) {
    next[pathKey] = merged;
  }

  return next;
}
