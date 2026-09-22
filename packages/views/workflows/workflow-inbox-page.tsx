"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { workflowRunDetailOptions, workflowWorkItemListOptions, useSubmitWorkflowWorkItem, type WorkflowWorkItem } from "@multica/core/workflows";
import { Button } from "@multica/ui/components/ui/button";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { AppLink } from "../navigation";
import { useT } from "../i18n";
import { RunStatus } from "./workflow-run-panel";

type WorkItemFilter = "open" | "closed" | "expired";

export function WorkflowInboxPage() {
  const { t } = useT("workflows");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const [status, setStatus] = useState<WorkItemFilter>("open");
  const items = useQuery(workflowWorkItemListOptions(wsId, status));
  return (
    <div className="flex h-full min-h-0 flex-col">
      <header className="flex flex-wrap items-center justify-between gap-3 border-b px-6 py-5">
        <div><h1 className="text-title font-semibold">{t(($) => $.work_items_title)}</h1><p className="mt-1 text-caption text-muted-foreground">{t(($) => $.work_items_subtitle)}</p></div>
        <div className="flex gap-1" role="group" aria-label={t(($) => $.status_filter)}>
          {(["open", "closed", "expired"] as const).map((value) => <Button key={value} size="sm" variant={status === value ? "secondary" : "outline"} onClick={() => setStatus(value)}>{t(($) => $.work_item_status[value])}</Button>)}
        </div>
      </header>
      <div className="min-h-0 flex-1 overflow-auto p-6">
        {items.isPending ? <p role="status">{t(($) => $.loading)}</p> : items.isError ? <div className="space-y-3" role="alert"><p>{items.error.message}</p><Button variant="outline" onClick={() => items.refetch()}>{t(($) => $.retry)}</Button></div> : !items.data?.length ? <p className="py-16 text-center text-caption text-muted-foreground">{t(($) => $.no_work_items)}</p> : <div className="mx-auto grid max-w-5xl gap-4">{items.data.map((item) => <WorkflowInboxItem key={item.id} item={item} wsId={wsId} paths={paths} />)}</div>}
      </div>
    </div>
  );
}

function WorkflowInboxItem({ item, wsId, paths }: { item: WorkflowWorkItem; wsId: string; paths: ReturnType<typeof useWorkspacePaths> }) {
  const { t } = useT("workflows");
  const run = useQuery(workflowRunDetailOptions(wsId, item.workflowId, item.runId));
  const submit = useSubmitWorkflowWorkItem(wsId, item.workflowId, item.runId, item.id);
  const [values, setValues] = useState("{}");
  const [feedback, setFeedback] = useState("");
  const [error, setError] = useState<string | null>(null);
  const isReview = item.kind === "review";

  async function decide(action: "submit" | "approve" | "rework") {
    setError(null);
    let parsed: Record<string, unknown>;
    try {
      const value: unknown = JSON.parse(values);
      if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error("values must be a JSON object");
      parsed = value as Record<string, unknown>;
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : t(($) => $.error));
      return;
    }
    if (action === "rework" && !feedback.trim()) {
      setError(t(($) => $.work_item_feedback_required));
      return;
    }
    try {
      await submit.mutateAsync({ expectedStateRevision: run.data?.stateRevision ?? 0, expectedItemVersion: item.version, idempotencyKey: crypto.randomUUID(), action, values: parsed, feedback });
    } catch (cause) {
      const message = cause instanceof Error ? cause.message : t(($) => $.error);
      setError(message);
      toast.error(message);
    }
  }

  return <article className="space-y-3 rounded-xl border bg-card p-5">
    <header className="flex flex-wrap items-start gap-3"><div className="min-w-0 flex-1"><h2 className="truncate text-body font-semibold">{item.kind === "review" ? t(($) => $.review_work_item) : t(($) => $.task_work_item)}</h2><p className="mt-1 text-micro text-muted-foreground">{t(($) => $.work_item_version, { version: item.version })} · {item.runId}</p></div><RunStatus status={item.status} /></header>
    <div className="flex flex-wrap gap-3 text-caption"><AppLink href={paths.workflowDetail(item.workflowId)} className="text-primary underline">{t(($) => $.view_workflow)}</AppLink>{item.dueAt && <span className="text-muted-foreground">{new Date(item.dueAt).toLocaleString()}</span>}</div>
    {item.status === "open" && <><label className="block space-y-1"><span className="text-caption font-medium">{t(($) => $.work_item_values)}</span><Textarea value={values} onChange={(event) => setValues(event.target.value)} aria-label={t(($) => $.work_item_values)} /></label><label className="block space-y-1"><span className="text-caption font-medium">{t(($) => $.work_item_feedback)}</span><Textarea value={feedback} onChange={(event) => setFeedback(event.target.value)} aria-label={t(($) => $.work_item_feedback)} /></label>{error && <p className="text-caption text-destructive" role="alert">{error}</p>}<div className="flex flex-wrap gap-2"><Button disabled={submit.isPending || run.isPending} onClick={() => void decide(isReview ? "approve" : "submit")}>{isReview ? t(($) => $.approve_work_item) : t(($) => $.submit_work_item)}</Button>{isReview && <Button variant="outline" disabled={submit.isPending || run.isPending} onClick={() => void decide("rework")}>{t(($) => $.rework_work_item)}</Button>}</div></>}
  </article>;
}
