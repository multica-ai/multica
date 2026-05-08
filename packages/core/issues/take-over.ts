import type { AgentRuntime, AgentTask } from "../types";

// Providers whose CLI we know how to drive with a `--resume <session>` style
// invocation. Mirrors `resumeCommandForProvider` in
// server/cmd/multica/cmd_issue.go — keep the two lists in sync when adding a
// new provider.
export const TAKE_OVER_PROVIDERS = [
  "claude",
  "codex",
  "cursor",
  "gemini",
  "opencode",
  "copilot",
] as const;

export type TakeOverProvider = (typeof TAKE_OVER_PROVIDERS)[number];

export function isTakeOverProvider(p: string | undefined | null): p is TakeOverProvider {
  if (!p) return false;
  return (TAKE_OVER_PROVIDERS as readonly string[]).includes(p.toLowerCase());
}

// Subset of the agent_task.result payload we read for take-over. The Go CLI
// stores at least these two fields for completed local-runtime runs; the
// field is `unknown` on AgentTask because other shapes flow through the same
// column (chat tasks, autopilots, etc.).
interface TaskRunResult {
  session_id?: unknown;
  work_dir?: unknown;
}

function readResult(task: AgentTask): TaskRunResult {
  if (task.result && typeof task.result === "object") {
    return task.result as TaskRunResult;
  }
  return {};
}

function readSessionId(task: AgentTask): string | null {
  const v = readResult(task).session_id;
  return typeof v === "string" && v.length > 0 ? v : null;
}

/**
 * Walk tasks newest-first and return the first completed one that carries a
 * resumable session_id whose runtime maps to a supported provider. Mirrors
 * `pickResumableRun` + the provider mapping in cmd_issue.go.
 *
 * `tasks` does not need to be pre-sorted — this function sorts on a copy to
 * keep the function pure.
 */
export function findResumableTask(
  tasks: readonly AgentTask[],
  runtimes: readonly AgentRuntime[],
): { task: AgentTask; provider: TakeOverProvider } | null {
  const providerById = new Map(runtimes.map((r) => [r.id, r.provider]));

  const sorted = [...tasks].sort((a, b) => {
    const at = a.completed_at ?? a.created_at;
    const bt = b.completed_at ?? b.created_at;
    return new Date(bt).getTime() - new Date(at).getTime();
  });

  for (const task of sorted) {
    if (task.status !== "completed") continue;
    if (!readSessionId(task)) continue;
    const provider = providerById.get(task.runtime_id);
    if (!isTakeOverProvider(provider)) continue;
    return { task, provider };
  }
  return null;
}

/**
 * Predicate gating the desktop "Take Over Locally" menu item. Returns true
 * when the action would actually do something useful — at least one
 * completed run exists, its runtime is a supported provider, and we're
 * running in the desktop client.
 *
 * `clientType` is passed in (instead of read from `window`) so callers can
 * unit-test this without DOM globals.
 */
export function canTakeOverLocally(args: {
  tasks: readonly AgentTask[];
  runtimes: readonly AgentRuntime[];
  clientType: "desktop" | "web";
}): boolean {
  if (args.clientType !== "desktop") return false;
  return findResumableTask(args.tasks, args.runtimes) !== null;
}
