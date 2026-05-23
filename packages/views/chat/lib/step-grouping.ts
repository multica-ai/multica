import type { ChatTimelineItem } from "@multica/core/chat";

export type StepGroup =
  | { kind: "single"; item: ChatTimelineItem }
  | { kind: "group"; type: ChatTimelineItem["type"]; items: ChatTimelineItem[] };

export function groupConsecutiveSteps(items: ChatTimelineItem[]): StepGroup[] {
  const out: StepGroup[] = [];
  for (const item of items) {
    if (item.type === "text") {
      out.push({ kind: "single", item });
      continue;
    }
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
