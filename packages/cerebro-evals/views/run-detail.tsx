import { AlertTriangle, ExternalLink } from "lucide-react";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { parseRunReport, passLabel } from "../report";
import type { EvalCaseReport, EvalRun } from "../types";

// RunDetail is the "See why" screen (FIR-3496 Fase 3): open one eval run and see,
// per task, what was asked, expected, produced, and the grader's reason for the
// pass/fail — plus the run's overall verdict and a link to its evidence. It is a
// pure presentational component; the page wires the evidence navigation.
export function RunDetail({ run, onClose, onOpenEvidence }: {
  run: EvalRun;
  onClose: () => void;
  onOpenEvidence?: (artifactId: string) => void;
}) {
  const report = parseRunReport(run.results);
  const passed = run.status === "passed";

  return (
    <section className="flex flex-col gap-4 rounded-lg border p-4">
      <div className="flex items-start justify-between gap-3">
        <div className="flex flex-col gap-1">
          <div className="flex items-center gap-2">
            <h2 className="text-sm font-semibold">See why</h2>
            <Badge variant={passed ? "default" : "destructive"}>{run.status}</Badge>
          </div>
          <p className="text-xs text-muted-foreground">Per-task detail for this run — the answer, the grader's reason, and the verdict.</p>
        </div>
        <div className="flex items-center gap-2">
          {run.evidence_artifact_id && onOpenEvidence && (
            <Button size="sm" variant="outline" onClick={() => onOpenEvidence(run.evidence_artifact_id as string)}>
              <ExternalLink className="size-4" />Evidence
            </Button>
          )}
          <Button size="sm" variant="ghost" onClick={onClose}>Close</Button>
        </div>
      </div>

      {report ? (
        <>
          <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
            <Summary label="Verdict" value={report.outcome.status} />
            <Summary label="Passed" value={`${report.outcome.passed}/${report.outcome.total} · ${pct(report.outcome.pass_rate)}`} />
            <Summary label="Pass rate needed" value={`${pct(report.outcome.min_pass_rate)} · ${report.outcome.threshold_met ? "met" : "not met"}`} />
            <Summary label="Critical tasks" value={report.outcome.critical_total === 0 ? "none" : `${report.outcome.critical_failed} failed · ${report.outcome.critical_rule_met ? "rule met" : "rule broken"}`} />
            <Summary label="Cost" value={`${report.cost_cents}¢`} />
            <Summary label="Latency" value={`${report.latency_ms} ms`} />
          </div>

          <div className="flex flex-col gap-2">
            {report.cases.length === 0 && <p className="text-xs text-muted-foreground">This run scored zero tasks, so it cannot be a trustworthy pass.</p>}
            {report.cases.map((task, i) => <CaseCard key={task.case_id || i} task={task} index={i} />)}
          </div>
        </>
      ) : (
        <p className="text-xs text-muted-foreground">No per-task detail recorded for this run. Runs created before the eval engine landed only have a summary verdict.</p>
      )}
    </section>
  );
}

function CaseCard({ task, index }: { task: EvalCaseReport; index: number }) {
  const label = passLabel(task.passed, task.error);
  const variant = task.error ? "destructive" : task.passed ? "default" : "destructive";
  return (
    <div className="flex flex-col gap-2 rounded-md border bg-background/50 p-3">
      <div className="flex items-center justify-between gap-2">
        <div className="flex items-center gap-2">
          <span className="text-xs font-medium">{task.case_id || `Task ${index + 1}`}</span>
          {task.critical && <Badge variant="outline">Critical</Badge>}
        </div>
        <Badge variant={variant}>{label}</Badge>
      </div>
      <Line label="Situation" value={task.situation} />
      <Line label="Expected" value={task.expected} />
      {task.error ? (
        <div className="flex items-start gap-1.5 text-xs text-destructive">
          <AlertTriangle className="mt-0.5 size-3.5 shrink-0" />
          <span>{task.error}</span>
        </div>
      ) : (
        <Line label="Produced" value={task.produced} />
      )}
      {task.reason && <Line label="Why" value={task.reason} />}
    </div>
  );
}

function Summary({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex flex-col gap-0.5 rounded-md border p-2">
      <span className="text-[11px] text-muted-foreground">{label}</span>
      <span className="text-xs font-medium">{value}</span>
    </div>
  );
}

function Line({ label, value }: { label: string; value: string }) {
  return (
    <div className="grid grid-cols-[80px_1fr] gap-2 text-xs">
      <span className="text-muted-foreground">{label}</span>
      <span className="whitespace-pre-wrap break-words">{value || "—"}</span>
    </div>
  );
}

function pct(rate: number): string {
  return `${Math.round(rate * 100)}%`;
}
