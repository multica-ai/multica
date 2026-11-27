// Command Agent `runtime_config` schema (GH #6202).
//
// Stored under `agent.runtime_config` as JSONB for agents bound to the
// built-in `command` runtime. Mirrors `server/pkg/agent/command.go`
// (`CommandRuntimeConfig`).

export const COMMAND_PROVIDER = "command";

export const COMMAND_ENV_PLACEHOLDERS = [
  "${MULTICA_BASE_BRANCH}",
  "${MULTICA_WORK_BRANCH}",
  "${MULTICA_BASE_COMMIT}",
  "${MULTICA_WORK_DIR}",
  "${MULTICA_ISSUE_IDENTIFIER}",
] as const;

export interface CommandRuntimeConfig {
  command?: string;
  args?: string[];
  workingDir?: string;
  timeoutSeconds?: number;
}

export function isValidCommandToken(command: string): boolean {
  const trimmed = command.trim();
  return trimmed.length > 0 && !/[\s\0]/.test(trimmed);
}

export function commandExecutable(raw: unknown): string {
  return parseCommandRuntimeConfig(raw).command ?? "";
}

/**
 * Splits a command argument string into argv tokens, supporting single and
 * double quotes as well as whitespace/newline separation.
 */
export function parseCommandArgsInput(input: string): string[] {
  const out: string[] = [];
  let current = "";
  let quote: '"' | "'" | null = null;
  let inToken = false;

  for (let i = 0; i < input.length; i += 1) {
    const ch = input[i]!;
    if (ch === "\0") continue;
    if (quote) {
      if (ch === quote) {
        quote = null;
      } else if (ch === "\\" && quote === '"' && i + 1 < input.length) {
        i += 1;
        current += input[i]!;
      } else {
        current += ch;
      }
      inToken = true;
      continue;
    }
    if (ch === '"' || ch === "'") {
      quote = ch;
      inToken = true;
      continue;
    }
    if (/\s/.test(ch)) {
      if (inToken) {
        out.push(current);
        current = "";
        inToken = false;
      }
      continue;
    }
    current += ch;
    inToken = true;
  }
  if (inToken) {
    out.push(current);
  }
  return out;
}

export function formatCommandArgsInput(args: string[] | undefined): string {
  if (!args || args.length === 0) return "";
  return args
    .map((arg) => (/\s/.test(arg) ? JSON.stringify(arg) : arg))
    .join(" ");
}

export function parseCommandRuntimeConfig(raw: unknown): CommandRuntimeConfig {
  if (!raw || typeof raw !== "object" || Array.isArray(raw)) {
    return {};
  }
  const obj = raw as Record<string, unknown>;
  const out: CommandRuntimeConfig = {};

  if (typeof obj.command === "string" && obj.command.trim() !== "") {
    out.command = obj.command.trim();
  }
  if (Array.isArray(obj.args)) {
    const args = obj.args
      .filter((item): item is string => typeof item === "string")
      .map((item) => item.trim())
      .filter(Boolean);
    if (args.length > 0) {
      out.args = args;
    }
  }
  if (typeof obj.working_dir === "string" && obj.working_dir.trim() !== "") {
    out.workingDir = obj.working_dir.trim();
  }
  if (
    typeof obj.timeout_seconds === "number" &&
    Number.isInteger(obj.timeout_seconds) &&
    obj.timeout_seconds > 0
  ) {
    out.timeoutSeconds = obj.timeout_seconds;
  }
  return out;
}

export function serializeCommandRuntimeConfig(
  cfg: CommandRuntimeConfig,
): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  if (typeof cfg.command === "string" && cfg.command.trim() !== "") {
    out.command = cfg.command.trim();
  }
  if (Array.isArray(cfg.args)) {
    const args = cfg.args.map((item) => item.trim()).filter(Boolean);
    if (args.length > 0) {
      out.args = args;
    }
  }
  if (typeof cfg.workingDir === "string" && cfg.workingDir.trim() !== "") {
    out.working_dir = cfg.workingDir.trim();
  }
  if (
    typeof cfg.timeoutSeconds === "number" &&
    Number.isInteger(cfg.timeoutSeconds) &&
    cfg.timeoutSeconds > 0
  ) {
    out.timeout_seconds = cfg.timeoutSeconds;
  }
  return out;
}

export function commandRuntimeConfigEquals(
  a: CommandRuntimeConfig,
  b: CommandRuntimeConfig,
): boolean {
  return (
    JSON.stringify(serializeCommandRuntimeConfig(a)) ===
    JSON.stringify(serializeCommandRuntimeConfig(b))
  );
}
