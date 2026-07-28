/**
 * Workpad plan parsing — mobile-owned pure copy of the web logic in
 * packages/cerebro-artifacts/core/workpad.ts (FIR-3659, FIR-3765). Mobile
 * cannot import @multica/cerebro-artifacts (it is outside the @multica/core
 * types / pure-functions sharing whitelist — apps/mobile/CLAUDE.md), so the
 * parsing lives here. Keep it in lockstep with the web source: same step
 * regex, same phase grouping, so mobile and web show the same phases and the
 * same done/total counts for one plan.
 */

// WorkpadArtifact is the subset of an Artifact the Workpad reads. Mirrors the
// fields of @multica/core/types Artifact that the panel needs.
export interface WorkpadArtifact {
  id: string;
  kind: string;
  title: string | null;
  body: string | null;
  updated_at: string;
}

export interface WorkpadItem {
  text: string;
  done: boolean;
}

// A phase groups consecutive checklist steps under a markdown heading. `title`
// is null for steps that appear before the first heading (an ungrouped lead).
export interface WorkpadPhase {
  title: string | null;
  items: WorkpadItem[];
}

export interface WorkpadProgress {
  done: number;
  total: number;
}

// selectPlanArtifact picks THE plan for an issue: the single kind:"plan"
// artifact coupled to it. If several exist the most recently updated wins, so
// the panel is deterministic. Returns null when the issue has no plan.
export function selectPlanArtifact(
  artifacts: WorkpadArtifact[] | undefined | null,
): WorkpadArtifact | null {
  if (!artifacts || artifacts.length === 0) return null;
  const plans = artifacts.filter((a) => a.kind === "plan");
  if (plans.length === 0) return null;
  return plans.reduce((latest, cur) =>
    cur.updated_at > latest.updated_at ? cur : latest,
  );
}

const CHECKLIST_LINE = /^\s*[-*]\s+\[( |x|X)\]\s+(.*\S)\s*$/;
const HEADING_LINE = /^\s{0,3}(#{1,6})\s+(.*\S?)\s*$/;

// parseWorkpadPhases groups the plan's steps by the markdown headings above
// them: each heading opens a new phase, and every checklist line below it joins
// that phase until the next heading. Steps before the first heading form a
// leading phase with `title: null`. Phases carrying no steps (a bare `# Plan`
// title line, or two headings in a row) are dropped. Flat order is preserved,
// so `phases.flatMap(p => p.items)` equals `parseWorkpadChecklist(body)`.
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

// namedPhases returns only the phases worth offering as a filter: those with a
// heading title. Fewer than two → the panel renders the plan flat, no filter.
export function namedPhases(phases: WorkpadPhase[]): WorkpadPhase[] {
  return phases.filter((p) => p.title !== null);
}

export function workpadProgress(items: WorkpadItem[]): WorkpadProgress {
  return { done: items.filter((i) => i.done).length, total: items.length };
}

// phaseStatus mirrors packages/cerebro-artifacts/core/workpad.ts (FIR-3765) so a
// phase shows the same circular status icon on both clients.
export function phaseStatus({
  done,
  total,
}: WorkpadProgress): "todo" | "in_progress" | "done" {
  if (total > 0 && done >= total) return "done";
  if (done > 0) return "in_progress";
  return "todo";
}
