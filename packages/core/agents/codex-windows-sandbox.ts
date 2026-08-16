import type { RuntimeDevice } from "../types";

export const CODEX_WINDOWS_SANDBOX_ARGS = [
  "-c",
  'windows.sandbox="unelevated"',
] as const;

type RuntimeDescriptor = Pick<RuntimeDevice, "provider" | "metadata">;

const windowsSandboxConfigKey = /^\s*windows\s*\.\s*sandbox\s*=/;

function normalizeArgToken(token: string): string {
  if (
    token.length >= 2 &&
    ((token.startsWith("'") && token.endsWith("'")) ||
      (token.startsWith('"') && token.endsWith('"')))
  ) {
    return token.slice(1, -1);
  }
  return token;
}

function hasWindowsSandboxOverride(args: readonly string[]): boolean {
  for (let index = 0; index < args.length; index += 1) {
    const arg = normalizeArgToken(args[index] ?? "");
    const separator = arg.indexOf("=");
    let flag = arg;
    let value = "";

    if (separator > 0) {
      flag = arg.slice(0, separator);
      value = normalizeArgToken(arg.slice(separator + 1));
    }

    if (flag !== "-c" && flag !== "--config") {
      continue;
    }
    if (separator <= 0) {
      index += 1;
      value = normalizeArgToken(args[index] ?? "");
    }
    if (windowsSandboxConfigKey.test(value)) {
      return true;
    }
  }
  return false;
}

function isWindowsCodexRuntime(
  runtime: RuntimeDescriptor | null | undefined,
): boolean {
  const os = runtime?.metadata?.os;
  return (
    runtime?.provider?.trim().toLowerCase() === "codex" &&
    typeof os === "string" &&
    os.trim().toLowerCase() === "windows"
  );
}

function withoutManagedSandboxPrefix(args: readonly string[]): string[] {
  if (
    args[0] === CODEX_WINDOWS_SANDBOX_ARGS[0] &&
    args[1] === CODEX_WINDOWS_SANDBOX_ARGS[1]
  ) {
    return args.slice(CODEX_WINDOWS_SANDBOX_ARGS.length);
  }
  return [...args];
}

function runtimeArgsOwnWindowsSandbox(
  runtime: RuntimeDescriptor | null | undefined,
): boolean {
  return runtime?.metadata?.codex_windows_sandbox_arg_configured === true;
}

/**
 * Mirrors the daemon's managed Codex prefix for persisted arguments and UI
 * previews. Runtime metadata carries only whether fixed arguments already own
 * the setting; the daemon independently uses its own GOOS and effective argv
 * before spawning the process.
 */
export function ensureCodexWindowsSandboxArgs(
  customArgs: readonly string[],
  runtime: RuntimeDescriptor | null | undefined,
): string[] {
  const result = withoutManagedSandboxPrefix(customArgs);
  if (
    !isWindowsCodexRuntime(runtime) ||
    runtimeArgsOwnWindowsSandbox(runtime) ||
    hasWindowsSandboxOverride(result)
  ) {
    return result;
  }
  return [...CODEX_WINDOWS_SANDBOX_ARGS, ...result];
}
