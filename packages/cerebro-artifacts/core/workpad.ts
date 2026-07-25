import type { Artifact } from "@multica/core/types";

// FIR-3659 — the Workpad is a first-class, auto-surfaced view of the issue's
// PLAN. The plan is a `kind: "plan"` artifact coupled to the issue (versioned
// like any note). The Workpad panel reads that plan and renders its checklist
// directly above the issue composer — it appears the moment a plan exists and
// disappears when there is none. No agent has to write anything into the
// description: the panel is driven purely by the presence of a plan on the
// issue.

export interface WorkpadItem {
  text: string;
  done: boolean;
}

// selectIssuePlan picks THE plan for an issue: the single `kind: "plan"`
// artifact coupled to it. If more than one exists (until the one-plan-per-issue
// guard is enforced everywhere) the most recently updated wins, so the panel is
// deterministic. Returns null when the issue has no plan — the caller renders
// nothing in that case.
export function selectIssuePlan(artifacts: Artifact[] | undefined | null): Artifact | null {
  if (!artifacts || artifacts.length === 0) return null;
  const plans = artifacts.filter((a) => a.kind === "plan");
  if (plans.length === 0) return null;
  return plans.reduce((latest, cur) =>
    cur.updated_at > latest.updated_at ? cur : latest,
  );
}

const CHECKLIST_LINE = /^\s*[-*]\s+\[( |x|X)\]\s+(.*\S)\s*$/;

// parseWorkpadChecklist extracts the plan's steps from its markdown body: every
// `- [ ]` / `- [x]` line becomes one item, in document order. Non-checklist
// lines (headings, prose) are ignored so a plan can carry context around its
// steps without polluting the panel.
export function parseWorkpadChecklist(body: string | undefined | null): WorkpadItem[] {
  if (!body) return [];
  const items: WorkpadItem[] = [];
  for (const line of body.replace(/\r\n/g, "\n").split("\n")) {
    const m = CHECKLIST_LINE.exec(line);
    if (!m) continue;
    const [, mark, text] = m;
    if (mark === undefined || text === undefined) continue;
    items.push({ done: mark.toLowerCase() === "x", text: text.trim() });
  }
  return items;
}

export interface WorkpadProgress {
  done: number;
  total: number;
}

export function workpadProgress(items: WorkpadItem[]): WorkpadProgress {
  return { done: items.filter((i) => i.done).length, total: items.length };
}

// phaseStatus maps a phase's progress to an issue-style status so the panel can
// show the same circular status icon per phase (FIR-3765): no steps done →
// "todo", some done → "in_progress", all done → "done". An empty phase reads as
// "todo". The literal union is assignable to the app's IssueStatus, so the
// shared StatusIcon renders it with the standard status colours.
export function phaseStatus({
  done,
  total,
}: WorkpadProgress): "todo" | "in_progress" | "done" {
  if (total > 0 && done >= total) return "done";
  if (done > 0) return "in_progress";
  return "todo";
}

// A phase groups consecutive checklist steps under a markdown heading, so a long
// plan reads as a handful of phases instead of one flat 17-step wall. `title` is
// null for steps that appear before the first heading (an ungrouped preamble).
export interface WorkpadPhase {
  title: string | null;
  items: WorkpadItem[];
}

const HEADING_LINE = /^\s{0,3}(#{1,6})\s+(.*\S?)\s*$/;

// parseWorkpadPhases groups the plan's steps by the markdown headings above
// them (FIR-3765): each `#`…`######` heading opens a new phase, and every
// checklist line below it joins that phase until the next heading. Steps before
// the first heading form a leading phase with `title: null`. Phases that carry
// no checklist steps (e.g. a bare `# Plan` title line, or two headings in a row)
// are dropped, so the result contains only phases the panel can actually show.
// The flat step order is preserved, so `phases.flatMap(p => p.items)` equals
// `parseWorkpadChecklist(body)`.
export function parseWorkpadPhases(body: string | undefined | null): WorkpadPhase[] {
  if (!body) return [];
  const phases: WorkpadPhase[] = [];
  let current: WorkpadPhase = { title: null, items: [] };
  const flush = () => {
    if (current.items.length > 0) phases.push(current);
  };
  for (const line of body.replace(/\r\n/g, "\n").split("\n")) {
    const heading = HEADING_LINE.exec(line);
    if (heading) {
      flush();
      current = { title: (heading[2] ?? "").trim() || "Untitled", items: [] };
      continue;
    }
    const m = CHECKLIST_LINE.exec(line);
    if (!m) continue;
    const [, mark, text] = m;
    if (mark === undefined || text === undefined) continue;
    current.items.push({ done: mark.toLowerCase() === "x", text: text.trim() });
  }
  flush();
  return phases;
}

// namedPhases returns only the phases that are worth offering as a filter in the
// Workpad: those with a heading title. When fewer than two named phases exist
// the panel renders the plan flat (no selector), matching the pre-phase view.
export function namedPhases(phases: WorkpadPhase[]): WorkpadPhase[] {
  return phases.filter((p) => p.title !== null);
}
