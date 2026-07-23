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
