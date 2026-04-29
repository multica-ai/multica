import { clipboard, ipcMain } from "electron";
import { execFile } from "child_process";
import { resolveCliInvocation } from "./daemon-manager";

export interface TakeOverResult {
  ok: boolean;
  /** The shell command copied to the clipboard (e.g. `cd '...' && claude --resume sess_abc`). */
  command?: string;
  /** Workdir extracted from the `cd '...' && ` prefix, when present. */
  workDir?: string;
  /** Set on failure. Surfaced verbatim in the renderer toast. */
  error?: string;
}

const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
// MUL-123, ABC-1, etc. — the human-readable identifier accepted by the
// CLI alongside raw UUIDs.
const IDENTIFIER_RE = /^[A-Z][A-Z0-9]*-\d+$/;

function isAcceptableIssueId(id: unknown): id is string {
  if (typeof id !== "string") return false;
  return UUID_RE.test(id) || IDENTIFIER_RE.test(id);
}

// Pulls the path out of the `cd '<path>' && ...` prefix the CLI emits. The
// Go side single-quotes the path with `'\''` for embedded quotes; we just
// reverse the wrapping here for display purposes — the actual command is
// already on the clipboard.
function extractWorkDir(command: string): string | undefined {
  const match = command.match(/^cd '((?:[^']|'\\'')*)' && /);
  if (!match) return undefined;
  return match[1]!.replace(/'\\''/g, "'");
}

function runCli(
  bin: string,
  args: string[],
  env: NodeJS.ProcessEnv,
): Promise<{ stdout: string; stderr: string }> {
  return new Promise((resolve, reject) => {
    execFile(
      bin,
      args,
      { timeout: 15_000, env, maxBuffer: 1024 * 1024 },
      (err, stdout, stderr) => {
        if (err) {
          const e = err as NodeJS.ErrnoException & { stderr?: string };
          e.stderr = stderr;
          reject(e);
          return;
        }
        resolve({ stdout, stderr });
      },
    );
  });
}

async function takeOver(
  issueId: string,
  workspaceId: string,
): Promise<TakeOverResult> {
  if (!isAcceptableIssueId(issueId)) {
    return { ok: false, error: "Invalid issue id" };
  }
  if (!UUID_RE.test(workspaceId)) {
    return { ok: false, error: "Missing workspace id" };
  }

  const cli = await resolveCliInvocation();
  if (!cli) {
    return {
      ok: false,
      error: "multica CLI is not available. Try restarting the desktop app.",
    };
  }

  // The desktop profile config doesn't pin a workspace — the renderer is the
  // source of truth (the user can switch workspaces inside the app without
  // the CLI ever knowing). Pass --workspace-id explicitly so the API request
  // carries the right X-Workspace-* header.
  const args = [
    "issue",
    "take",
    issueId,
    "--print",
    "--workspace-id",
    workspaceId,
    ...cli.profileArgs,
  ];

  try {
    const { stdout } = await runCli(cli.bin, args, cli.env);
    const command = stdout.trim();
    if (!command) {
      return { ok: false, error: "CLI returned an empty resume command." };
    }
    clipboard.writeText(command);
    return { ok: true, command, workDir: extractWorkDir(command) };
  } catch (err) {
    const e = err as NodeJS.ErrnoException & { stderr?: string };
    // Prefer the CLI's stderr message — it carries the actionable detail
    // (e.g. "no completed run found", "unsupported provider").
    const detail = (e.stderr ?? "").trim().split("\n").pop()?.trim();
    return {
      ok: false,
      error: detail && detail.length > 0 ? detail : (e.message ?? "Unknown error"),
    };
  }
}

export function setupIssueTakeOver(): void {
  ipcMain.handle(
    "issue:takeOver",
    (_event, issueId: string, workspaceId: string) =>
      takeOver(issueId, workspaceId),
  );
}
