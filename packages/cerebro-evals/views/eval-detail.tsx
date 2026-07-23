"use client";

import { useMemo, useState, type ReactNode } from "react";
import { ArrowLeft, Copy, Pencil, Trash2 } from "lucide-react";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@multica/ui/components/ui/tabs";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@multica/ui/components/ui/table";
import { formFromEval } from "./form";
import { describeGrader, describeThreshold, nextTransitions, versionsForKey } from "./lifecycle";
import { RunDetail } from "./run-detail";
import { ConfirmDelete } from "./confirm-delete";
import { EvalScheduleCard } from "./eval-schedule-card";
import { formatCost, formatDuration } from "../format";
import type { CerebroEval, EvalBinding, EvalRun, EvalSchedule, EvalScheduleInput, EvalStatus } from "../types";

export type EvalDetailTab = "overview" | "tasks" | "runs" | "connections" | "settings";

interface WorkflowRef { id: string; name: string }

// EvalDetail is the single-eval detail screen (FIR-3496 Fase 1): five tabs
// (Overview / Tasks / Runs / Connections / Settings) over one version, with the
// draft → active → paused → retired lifecycle and the version history. It is a
// presentational component — the page owns the queries and mutations and wires
// every action through callbacks.
export function EvalDetail({
  item, versions, initialTab = "overview", runs, runsLoading, runsError,
  selectedRunId, selectedRun, onSelectRun, bindings, workflows,
  onEdit, onDuplicate, onStatusChange, statusPending, statusError, onDelete,
  onRunNow, runPending, runError,
  schedule, scheduleLoading, schedulePending, scheduleError, onSaveSchedule, onOpenHooks,
  onClose, onOpenVersion, onOpenEvidence,
}: {
  item: CerebroEval;
  versions: CerebroEval[];
  initialTab?: EvalDetailTab;
  runs: EvalRun[];
  runsLoading: boolean;
  runsError: boolean;
  selectedRunId: string;
  selectedRun: EvalRun | null;
  onSelectRun: (runId: string) => void;
  bindings: EvalBinding[];
  workflows: WorkflowRef[];
  onEdit: () => void;
  onDuplicate: () => void;
  onStatusChange: (to: EvalStatus) => void;
  statusPending: boolean;
  statusError: boolean;
  onDelete: () => void;
  onRunNow: () => void;
  runPending: boolean;
  runError: boolean;
  schedule: EvalSchedule | null;
  scheduleLoading: boolean;
  schedulePending: boolean;
  scheduleError: boolean;
  onSaveSchedule: (input: EvalScheduleInput | null) => void;
  onOpenHooks: () => void;
  onClose: () => void;
  onOpenVersion: (evalId: string) => void;
  onOpenEvidence?: (artifactId: string) => void;
}) {
  const [tab, setTab] = useState<EvalDetailTab>(initialTab);
  const tasks = useMemo(() => formFromEval(item).tasks, [item]);
  const siblings = useMemo(() => versionsForKey(versions, item.key), [versions, item.key]);
  const connections = useMemo(() => bindings.filter((b) => b.eval_id === item.id), [bindings, item.id]);
  const workflowName = useMemo(() => new Map(workflows.map((w) => [w.id, w.name])), [workflows]);
  const transitions = nextTransitions(item.status);

  return (
    <div className="flex flex-col gap-4">
      {/* Both rows wrap: on a phone the title, badges and up to four actions
          cannot share one line, and without wrapping the actions were squeezed
          off the edge. */}
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="flex min-w-0 items-center gap-3">
          <Button size="icon-sm" variant="ghost" aria-label="Back to catalog" onClick={onClose}><ArrowLeft /></Button>
          <div className="min-w-0">
            <div className="flex flex-wrap items-center gap-2">
              <h2 className="text-sm font-semibold">{item.title}</h2>
              <Badge variant={item.status === "active" ? "default" : item.status === "retired" ? "outline" : "secondary"}>{item.status}</Badge>
              <span className="font-mono text-xs text-muted-foreground">{item.version}</span>
            </div>
            <p className="truncate text-xs text-muted-foreground">{item.key}</p>
          </div>
        </div>
        {/* Run now is the one action that belongs next to the title. Editing and
            the lifecycle moves live in Settings, so each has exactly one home. */}
        <div className="flex flex-wrap items-center gap-2">
          <Button size="sm" disabled={runPending || item.status !== "active"} onClick={onRunNow}>{runPending ? "Running…" : "Run now"}</Button>
        </div>
      </div>
      {statusError && <p className="text-sm text-destructive">Failed to change status.</p>}
      {runError && <p className="text-sm text-destructive">Failed to run eval. Check that it has tasks, a grader, and an available target.</p>}

      <Tabs value={tab} onValueChange={(value) => setTab(value as EvalDetailTab)}>
        <TabsList className="no-scrollbar flex h-auto w-full flex-nowrap justify-start gap-1 overflow-x-auto sm:w-fit">
          <TabsTrigger value="overview">Overview</TabsTrigger>
          <TabsTrigger value="tasks">Tasks · {tasks.length}</TabsTrigger>
          <TabsTrigger value="runs">Runs</TabsTrigger>
          <TabsTrigger value="connections">Connections · {connections.length}</TabsTrigger>
          <TabsTrigger value="settings">Settings</TabsTrigger>
        </TabsList>

        <TabsContent value="overview" className="mt-4 flex flex-col gap-4">
          <div className="grid gap-3 md:grid-cols-2">
            <Fact label="Objective" value={item.objective} span />
            <Fact label="Target" value={`${String(item.target.kind ?? "—")} · ${String(item.target.locator ?? "")} · ${String(item.target.ref ?? "")}`} />
            <Fact label="Source" value={String(item.source.path ?? item.source.repository ?? "Not pinned")} />
            <Fact label="Grader" value={describeGrader(item)} />
            <Fact label="Pass rule" value={describeThreshold(item)} />
            <Fact label="Created" value={new Date(item.created_at).toLocaleString()} />
            <Fact label="Updated" value={new Date(item.updated_at).toLocaleString()} />
          </div>
          <VersionHistory siblings={siblings} currentId={item.id} onOpenVersion={onOpenVersion} />
        </TabsContent>

        <TabsContent value="tasks" className="mt-4 flex flex-col gap-2">
          {tasks.length === 0 && <p className="text-xs text-muted-foreground">No tasks yet. Edit the eval to add situations and expected answers.</p>}
          {tasks.map((task, i) => (
            <div key={task.id} className="flex flex-col gap-1 rounded-md border bg-background/50 p-3">
              <div className="flex items-center justify-between gap-2">
                <span className="text-xs font-medium">Task {i + 1}</span>
                {task.critical && <Badge variant="outline">Critical</Badge>}
              </div>
              <Fact label="Situation" value={task.situation} />
              <Fact label="Expected" value={task.expected} />
            </div>
          ))}
        </TabsContent>

        <TabsContent value="runs" className="mt-4 flex flex-col gap-3">
          {runsError && <p className="text-sm text-destructive">Failed to load runs.</p>}
          <Table>
            <TableHeader><TableRow><TableHead>Status</TableHead><TableHead>Target version</TableHead><TableHead>Issue</TableHead><TableHead>Cost</TableHead><TableHead>Latency</TableHead><TableHead>Created</TableHead></TableRow></TableHeader>
            <TableBody>
              {runs.map((run) => (
                <TableRow key={run.id} className={`cursor-pointer${run.id === selectedRunId ? " bg-muted/50" : ""}`} onClick={() => onSelectRun(run.id === selectedRunId ? "" : run.id)}>
                  <TableCell><Badge variant={run.status === "passed" ? "default" : "secondary"}>{run.status}</Badge></TableCell>
                  <TableCell className="font-mono text-xs">{run.target_version || "—"}</TableCell>
                  <TableCell className="font-mono text-xs">{run.issue_key || (run.issue_id ? run.issue_id.slice(0, 8) : "—")}</TableCell>
                  <TableCell className="text-xs">{formatCost(run.cost_cents)}</TableCell>
                  <TableCell className="text-xs">{formatDuration(run.latency_ms)}</TableCell>
                  <TableCell className="text-xs">{new Date(run.created_at).toLocaleString()}</TableCell>
                </TableRow>
              ))}
              {runs.length === 0 && !runsLoading && <TableRow><TableCell colSpan={6} className="py-8 text-center text-sm text-muted-foreground">No runs recorded for this eval.</TableCell></TableRow>}
            </TableBody>
          </Table>
          {selectedRun && <RunDetail run={selectedRun} onClose={() => onSelectRun("")} onOpenEvidence={onOpenEvidence} />}
        </TabsContent>

        <TabsContent value="connections" className="mt-4 flex flex-col gap-2">
          <p className="text-xs text-muted-foreground">Workflow gates that run this eval. A blocking gate holds the workflow until the latest issue-specific run passes. Bind and unbind gates from the catalog.</p>
          <div className="flex flex-col divide-y rounded-md border">
            {connections.map((binding) => (
              <div key={binding.id} className="flex items-center justify-between gap-3 p-3">
                <div>
                  <span className="text-sm font-medium">{workflowName.get(binding.workflow_id) ?? `Workflow ${binding.workflow_id.slice(0, 8)}`}</span>
                  <div className="text-xs text-muted-foreground">{binding.phase} · {binding.blocking ? "Blocking" : "Advisory"}</div>
                </div>
                <Badge variant={binding.blocking ? "default" : "secondary"}>{binding.blocking ? "Blocking" : "Advisory"}</Badge>
              </div>
            ))}
            {connections.length === 0 && <p className="p-4 text-sm text-muted-foreground">Not bound to any workflow.</p>}
          </div>
        </TabsContent>

        <TabsContent value="settings" className="mt-4 flex flex-col gap-4">
          <EvalScheduleCard schedule={schedule} loading={scheduleLoading} pending={schedulePending} error={scheduleError} onSave={onSaveSchedule} onOpenHooks={onOpenHooks} />
          <div className="flex flex-col gap-2 rounded-md border p-3">
            <h3 className="text-xs font-semibold">Lifecycle</h3>
            <p className="text-[11px] text-muted-foreground">A draft is activated when it is ready to gate work; pause it to take it out of use without losing it; retire it when the version is done.</p>
            <div className="flex flex-wrap gap-2">
              {transitions.length === 0 && <p className="text-xs text-muted-foreground">This version is retired — the end of its lifecycle.</p>}
              {transitions.map((t) => (t.confirm ? (
                <ConfirmDelete
                  key={t.to}
                  trigger={<Button size="sm" variant="outline" disabled={statusPending}>{t.label}</Button>}
                  title={`Retire ${item.version}?`}
                  description={`"${item.title} · ${item.version}" will stop being used as a gate. Retiring ends this version — it cannot be reactivated.`}
                  actionLabel="Retire" onConfirm={() => onStatusChange(t.to)} pending={statusPending}
                />
              ) : (
                <Button key={t.to} size="sm" disabled={statusPending} onClick={() => onStatusChange(t.to)}>{t.label}</Button>
              )))}
            </div>
          </div>
          <div className="flex flex-col gap-2 rounded-md border p-3">
            <h3 className="text-xs font-semibold">Manage</h3>
            <div className="flex flex-wrap gap-2">
              <Button size="sm" variant="outline" onClick={onEdit}><Pencil className="size-4" />Edit</Button>
              <Button size="sm" variant="outline" onClick={onDuplicate}><Copy className="size-4" />Duplicate as new version</Button>
              <ConfirmDelete
                trigger={<Button size="sm" variant="outline"><Trash2 className="size-4 text-destructive" />Delete</Button>}
                title="Delete eval?"
                description={`This permanently removes "${item.title} · ${item.version}". It will stop being used as a gate. This cannot be undone.`}
                actionLabel="Delete" onConfirm={onDelete}
              />
            </div>
          </div>
        </TabsContent>
      </Tabs>
    </div>
  );
}

function VersionHistory({ siblings, currentId, onOpenVersion }: { siblings: CerebroEval[]; currentId: string; onOpenVersion: (evalId: string) => void }) {
  return (
    <div className="flex flex-col gap-2 rounded-md border p-3">
      <h3 className="text-xs font-semibold">Version history</h3>
      <div className="flex flex-col divide-y">
        {siblings.map((version) => (
          <div key={version.id} className="flex items-center justify-between gap-3 py-2">
            <div className="flex items-center gap-2">
              <span className="font-mono text-xs">{version.version}</span>
              <Badge variant={version.status === "active" ? "default" : version.status === "retired" ? "outline" : "secondary"}>{version.status}</Badge>
              {version.id === currentId && <span className="text-[11px] text-muted-foreground">viewing</span>}
            </div>
            <div className="flex items-center gap-3">
              <span className="text-[11px] text-muted-foreground">{new Date(version.created_at).toLocaleDateString()}</span>
              {version.id !== currentId && <Button size="sm" variant="ghost" onClick={() => onOpenVersion(version.id)}>Open</Button>}
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}

function Fact({ label, value, span }: { label: string; value: ReactNode; span?: boolean }) {
  return (
    <div className={`flex flex-col gap-0.5 rounded-md border p-2 ${span ? "md:col-span-2" : ""}`}>
      <span className="text-[11px] text-muted-foreground">{label}</span>
      <span className="text-xs font-medium whitespace-pre-wrap break-words">{value || "—"}</span>
    </div>
  );
}
