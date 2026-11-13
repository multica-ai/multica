import type { AgentTask, TaskMessagePayload } from "@multica/core/types";
import { redactSecrets } from "@multica/core/security/redact-secrets";
import { failureReasonKey } from "./failure-reason-label";

const UUID_PATTERN =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

export const RUN_TRANSCRIPT_PATHNAME =
  "/[workspace]/issue/[id]/run/[taskId]" as const;

export function runTranscriptRoute(
  workspace: string,
  issueId: string,
  taskId: string,
) {
  return {
    pathname: RUN_TRANSCRIPT_PATHNAME,
    params: { workspace, id: issueId, taskId },
  } as const;
}

export function isRunTranscriptTaskId(taskId: unknown): taskId is string {
  return typeof taskId === "string" && UUID_PATTERN.test(taskId);
}

export function isCancellableRunStatus(status: AgentTask["status"]): boolean {
  return (
    status === "queued" ||
    status === "deferred" ||
    status === "dispatched" ||
    status === "waiting_local_directory" ||
    status === "running"
  );
}

/** Match web transcript semantics without importing its UI or query layer. */
export function buildRunTranscript(
  messages: readonly TaskMessagePayload[],
): TaskMessagePayload[] {
  const items: TaskMessagePayload[] = [];
  for (const message of [...messages].sort((a, b) => a.seq - b.seq)) {
    const previous = items[items.length - 1];
    if (
      previous &&
      (message.type === "text" || message.type === "thinking") &&
      previous.type === message.type
    ) {
      previous.content = `${previous.content ?? ""}${message.content ?? ""}`;
      previous.created_at = message.created_at ?? previous.created_at;
    } else {
      items.push({ ...message });
    }
  }
  // Redact after coalescing: a credential may span two daemon flushes.
  return items.map((item) => ({
    ...item,
    content: item.content ? redactSecrets(item.content) : item.content,
    output: item.output ? redactSecrets(item.output) : item.output,
  }));
}

export type RunTranscriptEntry =
  | { kind: "message"; task_id: string; seq: number; message: TaskMessagePayload }
  | { kind: "tool"; task_id: string; seq: number; call?: TaskMessagePayload; result?: TaskMessagePayload };

/** Pair by opaque identity, never by completion order or a shared tool name. */
export function buildRunTranscriptEntries(messages: readonly TaskMessagePayload[]): RunTranscriptEntry[] {
  const entries: RunTranscriptEntry[] = [];
  const pending = new Map<string, Extract<RunTranscriptEntry, { kind: "tool" }>>();

  for (const message of buildRunTranscript(messages)) {
    // Missing IDs stay unpaired rather than guessing which parallel call won.
    const key = message.call_id ? `${message.task_id}:${message.call_id}` : null;
    if (message.type === "tool_use") {
      const entry = { kind: "tool" as const, task_id: message.task_id, seq: message.seq, call: message };
      entries.push(entry);
      if (key) pending.set(key, entry);
    } else if (message.type === "tool_result") {
      const call = key ? pending.get(key) : undefined;
      if (call && key) {
        call.result = message;
        pending.delete(key);
      } else {
        entries.push({ kind: "tool", task_id: message.task_id, seq: message.seq, result: message });
      }
    } else {
      entries.push({ kind: "message", task_id: message.task_id, seq: message.seq, message });
    }
  }
  return entries;
}

export function runToolContext(call: TaskMessagePayload | undefined): string | undefined {
  const input = call?.input;
  const context = [input?.command, input?.file_path, input?.path, input?.pattern, input?.query]
    .find((value): value is string => typeof value === "string" && value.length > 0);
  return context ? redactSecrets(context) : undefined;
}

export function getRunDiagnostics(
  task: Pick<AgentTask, "status" | "failure_reason" | "error">,
) {
  const reasonKey = task.status === "failed" ? failureReasonKey(task.failure_reason) : null;
  const error = task.error?.trim() ? redactSecrets(task.error) : null;
  return reasonKey || error ? { reasonKey, error } : null;
}
