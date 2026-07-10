import type { TaskMessagePayload } from "@multica/core/types/events";

const MESSAGE_TYPES = new Set<TaskMessagePayload["type"]>([
  "text",
  "thinking",
  "tool_use",
  "tool_result",
  "error",
]);

function safeString(value: unknown): string | undefined {
  if (value === null || value === undefined) return undefined;
  if (typeof value === "string") return value;
  if (typeof value === "number" || typeof value === "boolean" || typeof value === "bigint") {
    return String(value);
  }
  try {
    return JSON.stringify(value);
  } catch {
    return String(value);
  }
}

function safeRecord(value: unknown): Record<string, unknown> | undefined {
  if (!value || typeof value !== "object") return value === undefined ? undefined : { value };
  if (Array.isArray(value)) return { value };
  return value as Record<string, unknown>;
}

function safeType(value: unknown): TaskMessagePayload["type"] {
  return typeof value === "string" && MESSAGE_TYPES.has(value as TaskMessagePayload["type"])
    ? (value as TaskMessagePayload["type"])
    : "text";
}

function safeSeq(value: unknown, fallback: number): number {
  return typeof value === "number" && Number.isFinite(value) ? value : fallback;
}

export function normalizeTaskTranscriptMessages(
  messages: readonly TaskMessagePayload[],
): TaskMessagePayload[] {
  return messages.map((message, index) => {
    const raw = message as unknown as Record<string, unknown>;
    return {
      ...message,
      task_id: safeString(raw.task_id) ?? "",
      issue_id: safeString(raw.issue_id) ?? "",
      seq: safeSeq(raw.seq, index + 1),
      type: safeType(raw.type),
      tool: typeof raw.tool === "string" ? raw.tool : undefined,
      content: safeString(raw.content),
      input: safeRecord(raw.input),
      output: safeString(raw.output),
    };
  });
}

export function safeTaskText(value: unknown): string {
  return safeString(value)?.trim() ?? "";
}
