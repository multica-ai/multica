import type { TimelineItem } from "./build-timeline";
import { unwrapToolOutput } from "./trace-event-presenter";

// Derives a run's live plan from the checklist tools agents call, so the run
// transcript can show progress. Two shapes feed it:
// - Codex `update_plan` (recorded by the daemon from turn/plan/updated) and
//   Claude's legacy `TodoWrite` carry the whole plan every time: the last one
//   wins.
// - Claude's `TaskCreate`/`TaskUpdate` are incremental: a create's id exists
//   only in its result, an update names that id. `TaskList` answers with the
//   whole list, which is how a resumed session recovers steps created in an
//   earlier run. An update for an id this run never saw is dropped.
// Tool names are matched exactly: `Task` (sub-agent launch) is unrelated.

export type PlanStepStatus = "pending" | "in_progress" | "completed";

export interface PlanStep {
  id?: string;
  text: string;
  status: PlanStepStatus;
}

export interface RunPlan {
  steps: PlanStep[];
  completed: number;
  total: number;
  /** The step being worked on, or the next pending one. */
  current?: PlanStep;
}

function normalizeStatus(raw: unknown): PlanStepStatus | null {
  switch (raw) {
    case "pending":
      return "pending";
    case "in_progress":
    case "inProgress":
      return "in_progress";
    case "completed":
      return "completed";
    default:
      return null;
  }
}

function snapshotSteps(entries: unknown, textKey: string): PlanStep[] | null {
  if (!Array.isArray(entries)) return null;
  const steps: PlanStep[] = [];
  for (const entry of entries) {
    if (typeof entry !== "object" || entry === null) continue;
    const record = entry as Record<string, unknown>;
    const text = record[textKey];
    if (typeof text !== "string" || !text.trim()) continue;
    steps.push({ text: text.trim(), status: normalizeStatus(record.status) ?? "pending" });
  }
  return steps;
}

/**
 * A Claude tool result is stored as the raw JSON of its content: usually a
 * quoted string, sometimes an array of text blocks.
 */
function resultText(output: string | undefined): string {
  const raw = (output ?? "").trim();
  if (raw.startsWith("[")) {
    try {
      const blocks: unknown = JSON.parse(raw);
      if (Array.isArray(blocks)) {
        return blocks
          .map((block) =>
            typeof block === "object" && block !== null && typeof (block as { text?: unknown }).text === "string"
              ? (block as { text: string }).text
              : "",
          )
          .join("\n")
          .trim();
      }
    } catch {
      // Not JSON: fall through to the plain-string reading.
    }
  }
  return unwrapToolOutput(raw).trim();
}

const CREATED_RE = /^Task #(\S+) created successfully/;
const UPDATED_RE = /^Updated task #(\S+)/;
const LIST_LINE_RE = /^#(\S+) \[(\w+)\] (.+)$/;

function parseTaskList(text: string): PlanStep[] | null {
  if (text === "No tasks found") return [];
  const steps: PlanStep[] = [];
  for (const line of text.split("\n")) {
    const match = LIST_LINE_RE.exec(line.trim());
    if (!match) continue;
    const status = normalizeStatus(match[2]);
    if (!status) continue;
    // TaskList may append owner/blocker notes after the subject.
    const subject = match[3]!.replace(/\s+\((?:owner|blocked by)[^)]*\)\s*$/i, "").trim();
    steps.push({ id: match[1], text: subject, status });
  }
  return steps.length > 0 ? steps : null;
}

/** The latest plan the run published, or null when it never published one. */
export function buildRunPlan(items: TimelineItem[]): RunPlan | null {
  let steps: PlanStep[] | null = null;
  const pendingCalls = new Map<string, TimelineItem>();

  const sorted = [...items].sort((a, b) => a.seq - b.seq);
  for (const item of sorted) {
    if (item.type === "tool_use") {
      if (item.tool === "update_plan") {
        steps = snapshotSteps(item.input?.plan, "step") ?? steps;
      } else if (item.tool === "TodoWrite") {
        steps = snapshotSteps(item.input?.todos, "content") ?? steps;
      } else if (
        (item.tool === "TaskCreate" || item.tool === "TaskUpdate" || item.tool === "TaskList") &&
        item.callId
      ) {
        pendingCalls.set(item.callId, item);
      }
      continue;
    }
    if (item.type !== "tool_result" || !item.callId) continue;
    const call = pendingCalls.get(item.callId);
    if (!call) continue;
    pendingCalls.delete(item.callId);
    const output = resultText(item.output);

    if (call.tool === "TaskList") {
      steps = parseTaskList(output) ?? steps;
    } else if (call.tool === "TaskCreate") {
      const match = CREATED_RE.exec(output);
      const subject = call.input?.subject;
      if (!match || typeof subject !== "string" || !subject.trim()) continue;
      const id = match[1]!;
      steps = [...(steps ?? []).filter((step) => step.id !== id), { id, text: subject.trim(), status: "pending" }];
    } else if (call.tool === "TaskUpdate") {
      const match = UPDATED_RE.exec(output);
      const id = typeof call.input?.taskId === "string" ? call.input.taskId : match?.[1];
      if (!match || !id || !steps) continue;
      const input = call.input ?? {};
      if (input.status === "deleted") {
        steps = steps.filter((step) => step.id !== id);
        continue;
      }
      const status = normalizeStatus(input.status);
      const subject = typeof input.subject === "string" && input.subject.trim() ? input.subject.trim() : null;
      steps = steps.map((step) =>
        step.id === id ? { ...step, status: status ?? step.status, text: subject ?? step.text } : step,
      );
    }
  }

  if (!steps || steps.length === 0) return null;
  const completed = steps.filter((step) => step.status === "completed").length;
  const current =
    steps.find((step) => step.status === "in_progress") ?? steps.find((step) => step.status === "pending");
  return { steps, completed, total: steps.length, current };
}
