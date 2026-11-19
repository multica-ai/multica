import type { TaskMessagePayload } from "@multica/core/types";

export interface RunStep {
  id: string;
  label: string;
  done: boolean;
}

const DETAIL_KEYS = ["description", "command", "file_path", "path", "pattern", "query", "url", "prompt"] as const;

function basename(value: string): string {
  return /^[./~]|\//.test(value) && !/\s/.test(value) ? (value.split("/").filter(Boolean).pop() ?? value) : value;
}

/** "Read theme.ts", "Bash pnpm test": the tool and the one argument that says what it did. */
export function stepLabel(message: Pick<TaskMessagePayload, "tool" | "input">): string {
  const tool = message.tool ?? "Tool";
  const input = message.input ?? {};
  for (const key of DETAIL_KEYS) {
    const value = input[key];
    if (typeof value === "string" && value.trim()) {
      const detail = basename(value.trim().split("\n")[0]!);
      return `${tool} ${detail.length > 64 ? `${detail.slice(0, 63)}…` : detail}`;
    }
  }
  return tool;
}

/**
 * The tool calls of a run, oldest first. A call is done once its result
 * arrived, or once the run moved past it.
 */
export function runSteps(messages: readonly TaskMessagePayload[], live: boolean): RunStep[] {
  const results = new Set(messages.filter((m) => m.type === "tool_result" && m.call_id).map((m) => m.call_id));
  const calls = messages.filter((m) => m.type === "tool_use");
  return calls.map((call, index) => ({
    id: call.call_id ?? `${call.task_id}:${call.seq}`,
    label: stepLabel(call),
    done: !live || index < calls.length - 1 || (call.call_id ? results.has(call.call_id) : false),
  }));
}
