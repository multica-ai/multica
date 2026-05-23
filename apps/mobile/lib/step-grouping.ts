import type { TaskMessagePayload } from "@multica/core/types";

export type StepGroup =
  | { kind: "single"; item: TaskMessagePayload }
  | { kind: "group"; type: TaskMessagePayload["type"]; items: TaskMessagePayload[] };

export function groupConsecutiveSteps(
  items: TaskMessagePayload[],
): StepGroup[] {
  const out: StepGroup[] = [];
  for (const item of items) {
    const last = out[out.length - 1];
    if (last?.kind === "group" && last.type === item.type) {
      last.items.push(item);
    } else if (last?.kind === "single" && last.item.type === item.type) {
      out[out.length - 1] = {
        kind: "group",
        type: item.type,
        items: [last.item, item],
      };
    } else {
      out.push({ kind: "single", item });
    }
  }
  return out;
}

const LABELS: Record<string, (n: number) => string> = {
  thinking: (n) => `${n} thinking step${n === 1 ? "" : "s"}`,
  tool_use: (n) => `${n} tool call${n === 1 ? "" : "s"}`,
  tool_result: (n) => `${n} result${n === 1 ? "" : "s"}`,
  error: (n) => `${n} error${n === 1 ? "" : "s"}`,
};

export function getGroupLabel(type: TaskMessagePayload["type"], count: number): string {
  return (LABELS[type] ?? LABELS.error)(count);
}

export function getGroupIcon(
  type: TaskMessagePayload["type"],
): "bulb-outline" | "alert-circle" | "chevron-forward" {
  switch (type) {
    case "thinking":
      return "bulb-outline";
    case "error":
      return "alert-circle";
    default:
      return "chevron-forward";
  }
}
