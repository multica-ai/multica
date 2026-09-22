"use client";

import { useQuery } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { toast } from "sonner";
import { workflowRunListOptions, workflowRunDetailOptions, workflowRunEventsOptions, workflowRunNodeOptions, workflowWorkItemOptions, useCancelWorkflowRun, useRetryWorkflowNode, useSubmitWorkflowWorkItem, type WorkflowRun } from "@multica/core/workflows";
import { useWorkspacePaths } from "@multica/core/paths";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Sheet, SheetContent, SheetHeader, SheetTitle } from "@multica/ui/components/ui/sheet";
import { AppLink } from "../navigation";
import { RichContent } from "../rich-content";
import { useT } from "../i18n";

type WorkItemField = {
  key: string;
  label?: string;
  type?: string;
  required?: boolean;
  max_length?: number;
  placeholder?: string;
};

function workItemFields(snapshot: Record<string, unknown>): WorkItemField[] {
  const raw = snapshot.fields;
  if (!Array.isArray(raw)) return [];
  return raw.flatMap((value) => {
    if (!value || typeof value !== "object" || Array.isArray(value)) return [];
    const field = value as Record<string, unknown>;
    return typeof field.key === "string" && field.key.trim() ? [{
      key: field.key,
      label: typeof field.label === "string" ? field.label : undefined,
      type: typeof field.type === "string" ? field.type : "text",
      required: field.required === true,
      max_length: typeof field.max_length === "number" ? field.max_length : undefined,
      placeholder: typeof field.placeholder === "string" ? field.placeholder : undefined,
    }] : [];
  });
}

function isStructuredWorkItemField(type: string): boolean {
  return type === "json" || type === "array" || type === "attachment" || type === "attachments";
}

export function WorkflowRunPanel({ wsId, workflowId, open, onOpenChange, selectedRunId, onSelect }: { wsId: string; workflowId: string; open: boolean; onOpenChange: (open: boolean) => void; selectedRunId: string | null; onSelect: (id: string | null) => void }) {
  const { t } = useT("workflows");
  const runs = useQuery({ ...workflowRunListOptions(wsId, workflowId), enabled: open, refetchInterval: open ? 3_000 : false });
  const detail = useQuery({ ...workflowRunDetailOptions(wsId, workflowId, selectedRunId ?? ""), enabled: open && !!selectedRunId });
  return <Sheet open={open} onOpenChange={onOpenChange}><SheetContent className="w-full overflow-y-auto sm:max-w-xl"><SheetHeader><SheetTitle>{t(($) => $.history)}</SheetTitle></SheetHeader>
    <div className="space-y-4 px-4 pb-6">
      {runs.isError && <div role="alert">{runs.error.message}<Button onClick={() => runs.refetch()}>{t(($) => $.retry)}</Button></div>}
      {!runs.isPending && !runs.data?.length && <p className="text-caption text-muted-foreground">{t(($) => $.no_runs)}</p>}
      <div className="flex flex-wrap gap-2">{runs.data?.map((run) => <Button key={run.id} variant={selectedRunId === run.id ? "secondary" : "outline"} size="sm" onClick={() => onSelect(run.id)}><span>{new Date(run.createdAt).toLocaleString()}</span><RunStatus status={run.status} /></Button>)}</div>
      {detail.isError && <p role="alert">{detail.error.message}</p>}
      {detail.data && <WorkflowRunDetail wsId={wsId} workflowId={workflowId} run={detail.data} />}
    </div>
  </SheetContent></Sheet>;
}

export function RunStatus({ status }: { status: string }) {
  const { t } = useT("workflows");
  const known = ["pending", "ready", "queued", "running", "waiting", "waiting_human", "retry_wait", "blocked", "succeeded", "failed", "cancelled", "skipped", "cancelling"];
  return <span className="rounded-sm bg-muted px-1.5 py-0.5 text-micro font-medium">{known.includes(status) ? t(($) => $.status[status as keyof typeof $.status]) : status}</span>;
}

export function WorkflowRunDetail({ wsId, workflowId, run }: { wsId: string; workflowId: string; run: WorkflowRun }) {
  const { t } = useT("workflows");
  const paths = useWorkspacePaths();
  const cancel = useCancelWorkflowRun(wsId, workflowId);
  const retry = useRetryWorkflowNode(wsId, workflowId);
  const events = useQuery(workflowRunEventsOptions(wsId, workflowId, run.id));
  const [expandedNodeId, setExpandedNodeId] = useState<string | null>(null);
  const active = ["queued", "running", "waiting", "blocked", "cancelling"].includes(run.status);
  async function action(fn: () => Promise<unknown>) { try { await fn(); } catch (error) { toast.error(error instanceof Error ? error.message : t(($) => $.error)); } }
  return <div className="space-y-5">
    <div className="flex items-center gap-2"><RunStatus status={run.status} />{active && <Button size="sm" variant="outline" disabled={cancel.isPending} onClick={() => action(() => cancel.mutateAsync({ runId: run.id, expectedStateRevision: run.stateRevision }))}>{t(($) => $.cancel_run)}</Button>}{run.issueId && <AppLink href={paths.issueDetail(run.issueId)} className="text-caption text-primary underline">{t(($) => $.view_task)}</AppLink>}</div>
    {run.error && <p role="alert" className="text-caption text-destructive">{run.error}</p>}
    <div className="rounded-lg border p-3"><h3 className="mb-2 text-body font-medium">{t(($) => $.run_input)}</h3><p className="whitespace-pre-wrap break-words text-caption">{run.input}</p></div>
    {run.nodes?.map((node) => <article key={node.nodeId} className="space-y-3 rounded-xl border p-4">
      <header className="flex flex-wrap items-center gap-2"><h3 className="min-w-0 flex-1 truncate text-body font-semibold">{run.graph.nodes.find((entry) => entry.id === node.nodeId)?.label ?? node.nodeId}</h3><RunStatus status={node.status} /></header>
      <p className="text-micro text-muted-foreground">{t(($) => $.attempt, { count: node.attempt })}</p>
      {node.error && <p role="alert" className="whitespace-pre-wrap break-words text-caption text-destructive">{node.error}</p>}
      {node.output ? <div className="min-w-0 overflow-x-auto"><RichContent content={node.output} /></div> : <p className="text-caption text-muted-foreground">{t(($) => $.no_output)}</p>}
      {node.workItemId && node.status === "waiting_human" && <WorkflowWorkItemCard wsId={wsId} workflowId={workflowId} run={run} nodeId={node.nodeId} itemId={node.workItemId} />}
      <div className="flex flex-wrap items-center gap-3">{node.issueId && <AppLink href={paths.issueDetail(node.issueId)} className="text-caption text-primary underline">{t(($) => $.view_task)}</AppLink>}{node.status === "failed" && run.status !== "cancelled" && <Button size="sm" variant="outline" disabled={retry.isPending} onClick={() => action(() => retry.mutateAsync({ runId: run.id, nodeId: node.nodeId, expectedStateRevision: run.stateRevision }))}>{t(($) => $.retry_node)}</Button>}<Button size="sm" variant="ghost" onClick={() => setExpandedNodeId((current) => current === node.nodeId ? null : node.nodeId)}>{expandedNodeId === node.nodeId ? t(($) => $.hide_node_details) : t(($) => $.node_details)}</Button></div>
      {expandedNodeId === node.nodeId && <WorkflowNodeDetail wsId={wsId} workflowId={workflowId} runId={run.id} nodeId={node.nodeId} />}
    </article>)}
    {run.output && <section className="min-w-0 overflow-x-auto rounded-xl border p-4"><h3 className="mb-3 text-body font-semibold">{t(($) => $.output)}</h3><RichContent content={run.output} /></section>}
    <section className="rounded-xl border p-4"><h3 className="mb-3 text-body font-semibold">{t(($) => $.run_events)}</h3>{events.isError && <p className="text-caption text-destructive" role="alert">{events.error.message}</p>}{!events.isPending && !events.data?.events.length && <p className="text-caption text-muted-foreground">{t(($) => $.no_events)}</p>}<ol className="space-y-2">{events.data?.events.map((event) => <li key={event.id} className="flex gap-3 text-caption"><span className="w-8 shrink-0 text-right text-micro text-muted-foreground">#{event.sequence}</span><span className="min-w-0 flex-1"><span className="font-medium">{event.eventType}</span><span className="ml-2 text-micro text-muted-foreground">{new Date(event.createdAt).toLocaleString()}</span>{typeof event.payload.status === "string" && <span className="ml-2 text-micro text-muted-foreground">{event.payload.status}</span>}</span></li>)}</ol></section>
  </div>;
}

function WorkflowNodeDetail({ wsId, workflowId, runId, nodeId }: { wsId: string; workflowId: string; runId: string; nodeId: string }) {
  const { t } = useT("workflows");
  const detail = useQuery(workflowRunNodeOptions(wsId, workflowId, runId, nodeId));
  if (detail.isPending) return <p className="rounded-lg bg-muted p-3 text-caption" role="status">{t(($) => $.node_detail_loading)}</p>;
  if (detail.isError || !detail.data) return <p className="rounded-lg border border-destructive/40 p-3 text-caption text-destructive" role="alert">{detail.error?.message ?? t(($) => $.error)}</p>;
  return <div className="space-y-3 rounded-lg bg-muted/40 p-3">
    {detail.data.definition.instructions && <p className="whitespace-pre-wrap break-words text-caption">{detail.data.definition.instructions}</p>}
    <div><h4 className="mb-2 text-caption font-semibold">{t(($) => $.attempts)}</h4><ol className="space-y-2">{detail.data.attempts.map((attempt, index) => <li key={`${attempt.activationId ?? "attempt"}-${index}`} className="flex flex-wrap items-center gap-2 text-micro"><RunStatus status={attempt.status} /><span>{t(($) => $.attempt, { count: attempt.attempt })}</span>{attempt.error && <span className="text-destructive">{attempt.error}</span>}</li>)}</ol></div>
    {detail.data.outputs.length > 0 && <div><h4 className="mb-2 text-caption font-semibold">{t(($) => $.node_outputs)}</h4><div className="space-y-2">{detail.data.outputs.map((output) => <pre key={output.id} className="max-h-48 overflow-auto rounded-sm border bg-background p-2 text-micro">{JSON.stringify(output.values, null, 2)}</pre>)}</div></div>}
  </div>;
}

function WorkflowWorkItemCard({ wsId, workflowId, run, nodeId, itemId }: { wsId: string; workflowId: string; run: WorkflowRun; nodeId: string; itemId: string }) {
  const { t } = useT("workflows");
  const item = useQuery(workflowWorkItemOptions(wsId, workflowId, run.id, itemId));
  const submit = useSubmitWorkflowWorkItem(wsId, workflowId, run.id, itemId);
  const [values, setValues] = useState("{}");
  const [fieldValues, setFieldValues] = useState<Record<string, unknown>>({});
  const [initializedItemId, setInitializedItemId] = useState<string | null>(null);
  const [feedback, setFeedback] = useState("");
  const [error, setError] = useState<string | null>(null);
  useEffect(() => {
    if (!item.data || item.data.status !== "open" || initializedItemId === item.data.id) return;
    setFieldValues(item.data.outputValues ?? {});
    setInitializedItemId(item.data.id);
  }, [initializedItemId, item.data]);
  if (item.isPending) return <p className="rounded-lg bg-muted p-3 text-caption" role="status">{t(($) => $.work_item_loading)}</p>;
  if (item.isError || !item.data) return <p className="rounded-lg border border-destructive/40 p-3 text-caption text-destructive" role="alert">{item.error?.message ?? t(($) => $.error)}</p>;
  const workItem = item.data;
  if (workItem.status !== "open") return null;
  const node = run.graph.nodes.find((entry) => entry.id === nodeId);
  const isReview = workItem.kind === "review" || node?.type === "human_review";
  const fields = workItemFields(workItem.formSnapshot);

  function parseSubmissionValues(): Record<string, unknown> | null {
    if (fields.length === 0) {
      try {
        const candidate = JSON.parse(values);
        if (candidate && typeof candidate === "object" && !Array.isArray(candidate)) return candidate as Record<string, unknown>;
      } catch {
        // The caller renders the localized validation message.
      }
      return null;
    }
    const parsed: Record<string, unknown> = {};
    for (const field of fields) {
      const type = field.type ?? "text";
      const value = fieldValues[field.key];
      if (value === undefined || value === null || value === "") {
        if (field.required) return null;
        continue;
      }
      if (isStructuredWorkItemField(type) && typeof value === "string") {
        try {
          parsed[field.key] = JSON.parse(value);
        } catch {
          return null;
        }
      } else {
        parsed[field.key] = value;
      }
    }
    return parsed;
  }

  async function decide(action: "submit" | "approve" | "rework") {
    setError(null);
    const parsed = parseSubmissionValues();
    if (!parsed) {
      setError(fields.length > 0 ? t(($) => $.work_item_field_invalid) : t(($) => $.work_item_values_invalid));
      return;
    }
    if (action === "rework" && !feedback.trim()) { setError(t(($) => $.work_item_feedback_required)); return; }
    try {
      await submit.mutateAsync({ expectedStateRevision: run.stateRevision ?? 0, expectedItemVersion: workItem.version, idempotencyKey: crypto.randomUUID(), action, values: parsed, feedback });
    } catch (cause) { setError(cause instanceof Error ? cause.message : t(($) => $.error)); }
  }
  return <div className="space-y-3 rounded-lg border border-primary/30 bg-primary/5 p-3">
    <div><h4 className="text-body font-medium">{isReview ? t(($) => $.review_work_item) : t(($) => $.task_work_item)}</h4><p className="text-micro text-muted-foreground">{t(($) => $.work_item_version, { version: workItem.version })}</p></div>
    {fields.length > 0 ? <div className="space-y-3"><p className="text-caption font-medium">{t(($) => $.work_item_form)}</p>{fields.map((field) => {
      const type = field.type ?? "text";
      const label = `${field.label ?? field.key}${field.required ? " *" : ""}`;
      if (type === "boolean") return <label key={field.key} className="flex items-center gap-2 text-caption"><input type="checkbox" checked={fieldValues[field.key] === true} onChange={(event) => setFieldValues((current) => ({ ...current, [field.key]: event.target.checked }))} />{label}</label>;
      if (isStructuredWorkItemField(type)) return <label key={field.key} className="block space-y-1"><span className="text-caption font-medium">{label}</span><textarea className="min-h-20 w-full rounded-md border bg-background p-2 font-mono text-caption" value={typeof fieldValues[field.key] === "string" ? String(fieldValues[field.key]) : fieldValues[field.key] == null ? "" : JSON.stringify(fieldValues[field.key], null, 2)} onChange={(event) => setFieldValues((current) => ({ ...current, [field.key]: event.target.value }))} placeholder={field.placeholder} aria-label={label} /></label>;
      const inputType = type === "number" || type === "integer" ? "number" : "text";
      return <label key={field.key} className="block space-y-1"><span className="text-caption font-medium">{label}</span><Input type={inputType} value={fieldValues[field.key] == null ? "" : String(fieldValues[field.key])} onChange={(event) => setFieldValues((current) => ({ ...current, [field.key]: inputType === "number" ? (event.target.value === "" ? undefined : Number(event.target.value)) : event.target.value }))} maxLength={field.max_length} placeholder={field.placeholder} aria-label={label} /></label>;
    })}</div> : <label className="block space-y-1"><span className="text-caption font-medium">{t(($) => $.work_item_values)}</span><textarea className="min-h-24 w-full rounded-md border bg-background p-2 font-mono text-caption" value={values} onChange={(event) => setValues(event.target.value)} aria-label={t(($) => $.work_item_values)} /></label>}
    <label className="block space-y-1"><span className="text-caption font-medium">{t(($) => $.work_item_feedback)}</span><textarea className="min-h-16 w-full rounded-md border bg-background p-2 text-caption" value={feedback} onChange={(event) => setFeedback(event.target.value)} aria-label={t(($) => $.work_item_feedback)} /></label>
    {error && <p className="text-caption text-destructive" role="alert">{error}</p>}
    <div className="flex flex-wrap gap-2"><Button size="sm" disabled={submit.isPending} onClick={() => void decide(isReview ? "approve" : "submit")}>{isReview ? t(($) => $.approve_work_item) : t(($) => $.submit_work_item)}</Button>{isReview && <Button size="sm" variant="outline" disabled={submit.isPending} onClick={() => void decide("rework")}>{t(($) => $.rework_work_item)}</Button>}</div>
  </div>;
}
