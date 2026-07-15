"use client";

import { useMemo, useState, type FormEvent, type ReactNode } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowLeft, FlaskConical, Plus, Trash2 } from "lucide-react";
import { useCurrentWorkspace } from "@multica/core/paths";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { cerebroWorkflowsListOptions } from "@multica/cerebro-workflows/core";
import { PageHeader } from "@multica/views/layout/page-header";
import { useNavigation } from "@multica/views/navigation";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@multica/ui/components/ui/table";
import { createEval, createEvalBinding, deleteEval, deleteEvalBinding } from "../api";
import { evalBindingsOptions, evalKeys, evalRunsOptions, evalsListOptions } from "../queries";
import type { CerebroEval } from "../types";

interface DraftForm {
  key: string; title: string; version: string; objective: string; targetKind: string;
  targetLocator: string; targetRef: string; sourceRepository: string; sourceCommit: string; sourcePath: string;
}

const EMPTY_FORM: DraftForm = {
  key: "", title: "", version: "1.0.0", objective: "", targetKind: "workflow", targetLocator: "",
  targetRef: "main", sourceRepository: "https://github.com/firtal-group/firtal-evals", sourceCommit: "", sourcePath: "",
};

export function EvalsPage() {
  const workflowsEnabled = useFeatureFlag("cerebro_workflows");
  const evalsEnabled = useFeatureFlag("cerebro_evals");
  const workspace = useCurrentWorkspace();
  const navigation = useNavigation();
  const queryClient = useQueryClient();
  const workspaceId = workspace?.id ?? "";
  const [search, setSearch] = useState("");
  const [showCreate, setShowCreate] = useState(false);
  const [form, setForm] = useState<DraftForm>(EMPTY_FORM);
  const [bindingEvalId, setBindingEvalId] = useState("");
  const [bindingWorkflowId, setBindingWorkflowId] = useState("");
  const [selectedEvalId, setSelectedEvalId] = useState("");

  const evalsQuery = useQuery(evalsListOptions(workspaceId));
  const workflowsQuery = useQuery(cerebroWorkflowsListOptions(workspaceId));
  const bindingsQuery = useQuery(evalBindingsOptions(workspaceId));
  const runsQuery = useQuery(evalRunsOptions(workspaceId, selectedEvalId));

  const createMutation = useMutation({
    mutationFn: () => createEval({
      key: form.key.trim(), version: form.version.trim(), title: form.title.trim(), description: "",
      status: "draft", owner: { team: "Firtal AI" }, objective: form.objective.trim(),
      target: { kind: form.targetKind, locator: form.targetLocator.trim(), ref: form.targetRef.trim() },
      datasets: [], graders: [], thresholds: [], runner: {},
      source: { repository: form.sourceRepository.trim(), commit: form.sourceCommit.trim(), path: form.sourcePath.trim() },
    }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: evalKeys.list(workspaceId) });
      setForm(EMPTY_FORM);
      setShowCreate(false);
    },
  });
  const deleteMutation = useMutation({ mutationFn: deleteEval, onSuccess: () => queryClient.invalidateQueries({ queryKey: evalKeys.list(workspaceId) }) });
  const bindMutation = useMutation({
    mutationFn: () => createEvalBinding({ workflow_id: bindingWorkflowId, eval_id: bindingEvalId, phase: "delivery", blocking: true }),
    onSuccess: () => { queryClient.invalidateQueries({ queryKey: evalKeys.bindings(workspaceId) }); setBindingEvalId(""); setBindingWorkflowId(""); },
  });
  const unbindMutation = useMutation({ mutationFn: deleteEvalBinding, onSuccess: () => queryClient.invalidateQueries({ queryKey: evalKeys.bindings(workspaceId) }) });

  const filtered = useMemo(() => {
    const needle = search.trim().toLowerCase();
    return (evalsQuery.data ?? []).filter((item) => !needle || [item.title, item.key, item.objective, String(item.target.kind ?? "")].some((value) => value.toLowerCase().includes(needle)));
  }, [evalsQuery.data, search]);

  if (!workflowsEnabled || !evalsEnabled) return null;
  if (!workspace) return <div className="flex h-full items-center justify-center text-sm text-muted-foreground">Loading workspace context…</div>;
  const submit = (event: FormEvent) => { event.preventDefault(); createMutation.mutate(); };

  return (
    <div className="flex h-full flex-col">
      <PageHeader className="justify-between gap-3">
        <div className="flex items-center gap-3">
          <Button size="icon-sm" variant="ghost" aria-label="Back to workflows" onClick={() => navigation.push(`/${workspace.slug}/workflows`)}><ArrowLeft /></Button>
          <div><h1 className="text-sm font-semibold">Evals</h1><p className="text-[11px] text-muted-foreground">Versioned quality contracts and workflow gates</p></div>
        </div>
        <Button size="sm" onClick={() => setShowCreate((value) => !value)}><Plus className="size-4" />New eval</Button>
      </PageHeader>
      <div className="flex-1 overflow-y-auto p-6">
        <div className="mx-auto flex max-w-6xl flex-col gap-6">
          {showCreate && <form onSubmit={submit} className="grid gap-4 rounded-lg border bg-card p-4 md:grid-cols-2">
            <div className="md:col-span-2"><h2 className="text-sm font-semibold">Create draft eval</h2><p className="text-xs text-muted-foreground">The draft becomes active only after datasets, graders, and thresholds are registered.</p></div>
            <Field label="Key"><Input required pattern="[a-z0-9]+(?:-[a-z0-9]+)*" value={form.key} onChange={(e) => setForm({ ...form, key: e.target.value })} placeholder="customer-service-quality" /></Field>
            <Field label="Version"><Input required value={form.version} onChange={(e) => setForm({ ...form, version: e.target.value })} /></Field>
            <Field label="Title"><Input required value={form.title} onChange={(e) => setForm({ ...form, title: e.target.value })} /></Field>
            <Field label="Target type"><Input required value={form.targetKind} onChange={(e) => setForm({ ...form, targetKind: e.target.value })} placeholder="agent, skill, workflow, code…" /></Field>
            <Field label="Target locator"><Input required value={form.targetLocator} onChange={(e) => setForm({ ...form, targetLocator: e.target.value })} placeholder="mention://… or https://github.com/…" /></Field>
            <Field label="Target ref"><Input required value={form.targetRef} onChange={(e) => setForm({ ...form, targetRef: e.target.value })} /></Field>
            <Field label="Source repository"><Input required value={form.sourceRepository} onChange={(e) => setForm({ ...form, sourceRepository: e.target.value })} /></Field>
            <Field label="Source commit"><Input value={form.sourceCommit} onChange={(e) => setForm({ ...form, sourceCommit: e.target.value })} placeholder="Pinned on activation" /></Field>
            <Field label="Source path"><Input required value={form.sourcePath} onChange={(e) => setForm({ ...form, sourcePath: e.target.value })} placeholder="evals/<id>/eval.json" /></Field>
            <Field label="Objective" className="md:col-span-2"><Textarea required value={form.objective} onChange={(e) => setForm({ ...form, objective: e.target.value })} /></Field>
            {createMutation.isError && <p className="text-sm text-destructive md:col-span-2">{createMutation.error instanceof Error ? createMutation.error.message : "Failed to create eval"}</p>}
            <div className="flex justify-end gap-2 md:col-span-2"><Button type="button" variant="ghost" onClick={() => setShowCreate(false)}>Cancel</Button><Button type="submit" disabled={createMutation.isPending}>Create draft</Button></div>
          </form>}

          <section className="flex flex-col gap-3">
            <div className="flex items-center justify-between gap-3"><div><h2 className="text-sm font-semibold">Catalog</h2><p className="text-xs text-muted-foreground">One searchable definition per eval version — never one workspace skill per eval.</p></div><Input className="max-w-xs" value={search} onChange={(e) => setSearch(e.target.value)} placeholder="Search evals…" /></div>
            {evalsQuery.isError && <p className="text-sm text-destructive">Failed to load evals.</p>}
            <Table><TableHeader><TableRow><TableHead>Eval</TableHead><TableHead>Target</TableHead><TableHead>Version</TableHead><TableHead>Status</TableHead><TableHead>Source</TableHead><TableHead className="text-right">Actions</TableHead></TableRow></TableHeader><TableBody>
              {filtered.length === 0 && !evalsQuery.isLoading && <TableRow><TableCell colSpan={6} className="py-10 text-center text-sm text-muted-foreground"><FlaskConical className="mx-auto mb-2 size-5" />No evals found.</TableCell></TableRow>}
              {filtered.map((item: CerebroEval) => <TableRow key={item.id}>
                <TableCell><div className="font-medium">{item.title}</div><div className="text-xs text-muted-foreground">{item.key}</div><div className="mt-1 max-w-md text-xs text-muted-foreground">{item.objective}</div></TableCell>
                <TableCell className="text-xs"><div>{String(item.target.kind ?? "Unknown")}</div><div className="max-w-[220px] truncate text-muted-foreground">{String(item.target.locator ?? "")}</div></TableCell>
                <TableCell className="font-mono text-xs">{item.version}</TableCell><TableCell><Badge variant={item.status === "active" ? "default" : "secondary"}>{item.status}</Badge></TableCell>
                <TableCell className="max-w-[220px] truncate text-xs">{String(item.source.path ?? item.source.repository ?? "Not pinned")}</TableCell>
                <TableCell className="text-right"><div className="flex justify-end gap-1"><Button size="sm" variant="ghost" onClick={() => setSelectedEvalId(item.id)}>Runs</Button><Button size="icon-sm" variant="ghost" aria-label={`Delete ${item.title}`} onClick={() => confirm(`Delete eval "${item.title}"?`) && deleteMutation.mutate(item.id)}><Trash2 className="size-4 text-destructive" /></Button></div></TableCell>
              </TableRow>)}
            </TableBody></Table>
          </section>

          {selectedEvalId && <section className="flex flex-col gap-3 rounded-lg border p-4">
            <div className="flex items-center justify-between"><div><h2 className="text-sm font-semibold">Run history</h2><p className="text-xs text-muted-foreground">Latest immutable results for the selected eval version.</p></div><Button size="sm" variant="ghost" onClick={() => setSelectedEvalId("")}>Close</Button></div>
            <Table><TableHeader><TableRow><TableHead>Status</TableHead><TableHead>Target version</TableHead><TableHead>Issue</TableHead><TableHead>Cost</TableHead><TableHead>Latency</TableHead><TableHead>Created</TableHead></TableRow></TableHeader><TableBody>
              {(runsQuery.data ?? []).map((run) => <TableRow key={run.id}><TableCell><Badge variant={run.status === "passed" ? "default" : "secondary"}>{run.status}</Badge></TableCell><TableCell className="font-mono text-xs">{run.target_version || "—"}</TableCell><TableCell className="font-mono text-xs">{run.issue_id?.slice(0, 8) ?? "—"}</TableCell><TableCell className="text-xs">{run.cost_cents}¢</TableCell><TableCell className="text-xs">{run.latency_ms} ms</TableCell><TableCell className="text-xs">{new Date(run.created_at).toLocaleString()}</TableCell></TableRow>)}
              {(runsQuery.data ?? []).length === 0 && !runsQuery.isLoading && <TableRow><TableCell colSpan={6} className="py-8 text-center text-sm text-muted-foreground">No runs recorded for this eval.</TableCell></TableRow>}
            </TableBody></Table>
          </section>}

          <section className="flex flex-col gap-3 rounded-lg border p-4">
            <div><h2 className="text-sm font-semibold">Workflow gates</h2><p className="text-xs text-muted-foreground">A blocking delivery binding prevents the workflow from advancing until the latest issue-specific run passes.</p></div>
            <div className="grid gap-2 md:grid-cols-[1fr_1fr_auto]">
              <select className="h-9 rounded-md border bg-background px-3 text-sm" value={bindingEvalId} onChange={(e) => setBindingEvalId(e.target.value)}><option value="">Select eval…</option>{(evalsQuery.data ?? []).map((item) => <option key={item.id} value={item.id}>{item.title} · {item.version}</option>)}</select>
              <select className="h-9 rounded-md border bg-background px-3 text-sm" value={bindingWorkflowId} onChange={(e) => setBindingWorkflowId(e.target.value)}><option value="">Select Issue workflow…</option>{(workflowsQuery.data?.workflows ?? []).filter((item) => item.workflow_type === "issue_loop").map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}</select>
              <Button disabled={!bindingEvalId || !bindingWorkflowId || bindMutation.isPending} onClick={() => bindMutation.mutate()}>Add blocking gate</Button>
            </div>
            <div className="flex flex-col divide-y rounded-md border">{(bindingsQuery.data ?? []).map((binding) => <div key={binding.id} className="flex items-center justify-between gap-3 p-3"><div><span className="text-sm font-medium">{binding.eval_title}</span><span className="ml-2 font-mono text-xs text-muted-foreground">{binding.eval_version}</span><div className="text-xs text-muted-foreground">{binding.phase} · {binding.blocking ? "Blocking" : "Advisory"} · workflow {binding.workflow_id.slice(0, 8)}</div></div><Button size="sm" variant="ghost" onClick={() => unbindMutation.mutate(binding.id)}>Remove</Button></div>)}{(bindingsQuery.data ?? []).length === 0 && <p className="p-4 text-sm text-muted-foreground">No eval gates are bound yet.</p>}</div>
          </section>
        </div>
      </div>
    </div>
  );
}

function Field({ label, className, children }: { label: string; className?: string; children: ReactNode }) {
  return <label className={`flex flex-col gap-1.5 text-xs font-medium ${className ?? ""}`}><span>{label}</span>{children}</label>;
}
