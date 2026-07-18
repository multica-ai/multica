import { buildWriteInput, formFromEval } from "./form";
import type { CerebroEval, EvalStatus, EvalWriteInput } from "../types";

// lifecycle.ts holds the pure logic behind the eval detail screen (FIR-3496
// Fase 1): the allowed status moves, the write payload that flips only the
// status, the version-history grouping, and the one-line grader/threshold
// summaries. Keeping it out of the React component lets the state machine and
// the JSON round-trip be unit tested without a DOM, and reuses the tested
// form → write mapping so a status flip can never silently rewrite the tasks,
// grader, or threshold the runner reads.

export interface EvalTransition {
  to: EvalStatus;
  label: string;
  // confirm marks a move that ends a version and should ask before firing.
  confirm?: boolean;
}

// nextTransitions is the lifecycle state machine: a draft is activated; an
// active eval is paused or retired; a paused eval is resumed or retired.
// Activate/pause/resume are reversible one-click flips, retire ends the version
// and is confirmed. Retired is terminal.
export function nextTransitions(status: EvalStatus): EvalTransition[] {
  switch (status) {
    case "draft":
      return [{ to: "active", label: "Activate" }];
    case "active":
      return [{ to: "paused", label: "Pause" }, { to: "retired", label: "Retire", confirm: true }];
    case "paused":
      return [{ to: "active", label: "Resume" }, { to: "retired", label: "Retire", confirm: true }];
    case "retired":
      return [];
  }
}

// statusInput builds the write payload that flips only the status, carrying
// every other field — tasks, grader, threshold, target, source, runner, owner —
// through unchanged by round-tripping the eval back through the tested
// form → write mapping. buildWriteInput reads the status from its base argument,
// so a base with the new status is all it takes.
export function statusInput(item: CerebroEval, to: EvalStatus): EvalWriteInput {
  return buildWriteInput(formFromEval(item), { ...item, status: to });
}

// versionsForKey returns every eval sharing this key, newest first, so the
// detail screen can show one version's siblings as its version history.
export function versionsForKey(evals: CerebroEval[], key: string): CerebroEval[] {
  return evals
    .filter((item) => item.key === key)
    .sort((a, b) => b.created_at.localeCompare(a.created_at) || b.version.localeCompare(a.version));
}

// describeGrader / describeThreshold render the parsed grader and pass rule as
// one readable line for the Overview tab, reusing formFromEval so the reading
// matches exactly what the edit form and the runner see.
export function describeGrader(item: CerebroEval): string {
  const form = formFromEval(item);
  if (form.graderType === "hard_rule") return `Hard rule · ${form.graderMatch}`;
  return form.graderModel ? `AI judge · ${form.graderModel}` : "AI judge";
}

export function describeThreshold(item: CerebroEval): string {
  const form = formFromEval(item);
  const critical = form.requireAllCritical ? " · every critical task must pass" : "";
  return `Pass rate ≥ ${form.passRate}%${critical}`;
}
