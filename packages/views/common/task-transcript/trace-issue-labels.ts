import type { TraceEvent } from "./trace-event-presenter";

const UUID = "[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}";
const UUID_VALUE = new RegExp(`^${UUID}$`, "i");
// Only these positional arguments identify issues. In particular --thread and
// --parent identify comments, and must never be sent to the issue resolver.
const ISSUE_COMMAND = new RegExp(
  `\\bmultica\\s+issue\\s+(?:get|update|comment\\s+(?:list|add))\\s+["']?(${UUID})(?![\\w-])`,
  "gi",
);

export function collectTraceIssueIds(issueId: string, events: readonly TraceEvent[]): string[] {
  const ids = new Set<string>();
  const add = (id: unknown) => {
    if (typeof id === "string" && UUID_VALUE.test(id)) ids.add(id.toLowerCase());
  };
  add(issueId);
  for (const event of events) {
    if (event.type !== "tool_use" || !event.input) continue;
    add(event.input.issue_id);
    for (const key of ["command", "cmd"]) {
      const command = event.input[key];
      if (typeof command !== "string") continue;
      for (const match of command.matchAll(ISSUE_COMMAND)) add(match[1]);
    }
  }
  return [...ids];
}

/** Display-only: never rewrite stored events, expanded evidence, or clipboard text. */
export function replaceTraceIssueIds(text: string, labels: ReadonlyMap<string, string>): string {
  return text.replace(new RegExp(`(?<![\\w-])${UUID}(?![\\w-])`, "gi"),
    (id) => labels.get(id.toLowerCase()) ?? id);
}
