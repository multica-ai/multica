import type { CerebroEval, EvalStatus, EvalWriteInput } from "../types";

// form.ts holds the pure mapping between the catalog form and the API write
// shape. Keeping it out of the React component lets the in-app authoring —
// tasks, grader, and pass threshold — be unit tested without a DOM, and makes
// sure the JSON the form writes into datasets/graders/thresholds is exactly the
// shape the Go runner reads (server/internal/cerebro/evals/assets.go +
// runner/{aijudge,hardrule}.go). A mismatch there would let an eval "pass"
// against tasks the runner never actually parsed.

export interface DraftTask {
  id: string; situation: string; expected: string; critical: boolean;
}

export interface DraftForm {
  key: string; title: string; version: string; objective: string; targetKind: string;
  targetLocator: string; targetRef: string; sourceRepository: string; sourceCommit: string; sourcePath: string;
  // In-app exam content. tasks feed the datasets column; the grader + threshold
  // fields feed the graders and thresholds columns.
  tasks: DraftTask[];
  graderType: string;   // "ai_judge" | "hard_rule"
  graderRubric: string; // ai_judge: extra grading guidance
  graderModel: string;  // ai_judge: optional model override
  graderMatch: string;  // hard_rule: exact | contains | iexact | icontains
  passRate: string;     // percent, e.g. "80" — the share of tasks that must pass
  requireAllCritical: boolean;
}

export const GRADER_MATCHES = ["iexact", "exact", "icontains", "contains"] as const;

export const EMPTY_FORM: DraftForm = {
  key: "", title: "", version: "1.0.0", objective: "", targetKind: "workflow", targetLocator: "",
  targetRef: "main", sourceRepository: "https://github.com/firtal-group/firtal-evals", sourceCommit: "", sourcePath: "",
  tasks: [], graderType: "ai_judge", graderRubric: "", graderModel: "", graderMatch: "iexact",
  passRate: "100", requireAllCritical: true,
};

const str = (value: unknown): string => (value == null ? "" : String(value));

// clampPercent keeps the pass rate a whole percentage in [0,100]; a blank or
// unparseable value falls back to 100 (every task must pass) so an
// under-specified eval fails closed rather than passing by accident.
function clampPercent(value: string): number {
  const n = Number(value);
  if (!Number.isFinite(n)) return 100;
  return Math.min(100, Math.max(0, Math.round(n)));
}

// percentFromStored reads a pass_rate threshold value that may be stored as a
// fraction (0.8) or a percentage (80) — the runner accepts both — and shows it
// as a percentage in the form.
function percentFromStored(value: unknown): number {
  const n = Number(value);
  if (!Number.isFinite(n)) return 100;
  const percent = n <= 1 ? n * 100 : n;
  return Math.min(100, Math.max(0, Math.round(percent)));
}

// formFromEval prefills the catalog form from an existing eval so the same form
// drives editing. It parses the datasets/graders/thresholds JSONB back into the
// editable fields; fields the form does not expose stay on the eval and are
// re-attached by buildWriteInput.
export function formFromEval(item: CerebroEval): DraftForm {
  const rawTasks = Array.isArray(item.datasets) ? item.datasets : [];
  const tasks: DraftTask[] = rawTasks.map((raw, i) => {
    const t = (raw ?? {}) as Record<string, unknown>;
    return {
      id: str(t.id) || `task-${i + 1}`,
      situation: str(t.situation),
      expected: str(t.expected),
      critical: t.critical === true,
    };
  });

  const graders = Array.isArray(item.graders) ? item.graders : [];
  const grader = (graders[0] ?? {}) as Record<string, unknown>;
  const config = (grader.config ?? {}) as Record<string, unknown>;

  const thresholds = Array.isArray(item.thresholds) ? item.thresholds : [];
  let passRate = 100;
  let requireAllCritical = false;
  for (const raw of thresholds) {
    const e = (raw ?? {}) as Record<string, unknown>;
    if (e.metric === "pass_rate") passRate = percentFromStored(e.value);
    if (e.metric === "all_critical_pass") requireAllCritical = e.value === true;
  }

  return {
    key: item.key,
    title: item.title,
    version: item.version,
    objective: item.objective,
    targetKind: str(item.target.kind),
    targetLocator: str(item.target.locator),
    targetRef: str(item.target.ref),
    sourceRepository: str(item.source.repository),
    sourceCommit: str(item.source.commit),
    sourcePath: str(item.source.path),
    tasks,
    graderType: str(grader.type) || "ai_judge",
    graderRubric: str(config.rubric),
    graderModel: str(config.model),
    graderMatch: str(config.match) || "iexact",
    passRate: String(passRate),
    requireAllCritical,
  };
}

// buildGrader serializes the form's grader fields into the graders array the
// runner reads. Only the config keys the chosen grader type uses are written.
function buildGrader(form: DraftForm): Record<string, unknown> {
  if (form.graderType === "hard_rule") {
    return { id: "grader-1", type: "hard_rule", config: { match: form.graderMatch || "iexact" } };
  }
  const config: Record<string, unknown> = {};
  if (form.graderRubric.trim()) config.rubric = form.graderRubric.trim();
  if (form.graderModel.trim()) config.model = form.graderModel.trim();
  return { id: "grader-1", type: "ai_judge", config };
}

// buildWriteInput turns the form into an API write payload. It serializes the
// in-app tasks, grader, and threshold into the datasets/graders/thresholds
// columns in the runner's exact shape, dropping tasks with a blank situation
// (the runner skips those anyway). When base is given (an edit) it carries over
// the eval's runner config, owner, description, status, and any extra
// target/source keys the form does not expose; when base is omitted (a create)
// it uses fail-closed draft defaults.
export function buildWriteInput(form: DraftForm, base?: CerebroEval): EvalWriteInput {
  const status: EvalStatus = base?.status ?? "draft";
  const datasets = form.tasks
    .filter((t) => t.situation.trim() !== "")
    .map((t, i) => ({
      id: t.id || `task-${i + 1}`,
      situation: t.situation.trim(),
      expected: t.expected.trim(),
      critical: t.critical,
    }));
  const thresholds = [
    { metric: "pass_rate", operator: "gte", value: clampPercent(form.passRate) / 100 },
    { metric: "all_critical_pass", operator: "eq", value: form.requireAllCritical },
  ];
  return {
    key: form.key.trim(),
    version: form.version.trim(),
    title: form.title.trim(),
    description: base?.description ?? "",
    status,
    owner: base?.owner ?? { team: "Firtal AI" },
    objective: form.objective.trim(),
    target: { ...(base?.target ?? {}), kind: form.targetKind.trim(), locator: form.targetLocator.trim(), ref: form.targetRef.trim() },
    datasets,
    graders: [buildGrader(form)],
    thresholds,
    runner: base?.runner ?? {},
    source: { ...(base?.source ?? {}), repository: form.sourceRepository.trim(), commit: form.sourceCommit.trim(), path: form.sourcePath.trim() },
  };
}
