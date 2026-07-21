"use client";

import { useMemo, useState, type FormEvent, type ReactNode } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowLeft, ChevronDown, Copy, FlaskConical, Pencil, Plus, Trash2 } from "lucide-react";
import { useCurrentWorkspace } from "@multica/core/paths";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { cerebroWorkflowsListOptions } from "@multica/cerebro-workflows/core";
import { PageHeader } from "@multica/views/layout/page-header";
import { useNavigation } from "@multica/views/navigation";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { Input } from "@multica/ui/components/ui/input";
import { NativeSelect, NativeSelectOption } from "@multica/ui/components/ui/native-select";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@multica/ui/components/ui/table";
import { createEval, createEvalBinding, deleteEval, deleteEvalBinding, deleteEvalSchedule, runEvalNow, updateEval, upsertEvalSchedule } from "../api";
import { evalBindingsOptions, evalKeys, evalRunsOptions, evalScheduleOptions, evalsListOptions } from "../queries";
import { buildWriteInput, EMPTY_FORM, formFromEval, GRADER_MATCHES, TARGET_KINDS, withDerivedIdentity, type DraftTask } from "./form";
import { ConfirmDelete } from "./confirm-delete";
import { EVAL_TEMPLATES, duplicateForm } from "./templates";
import { statusInput } from "./lifecycle";
import { filterEvals, STATUS_FILTERS, statusCounts, type StatusFilter } from "./catalog";
import { EvalDetail, type EvalDetailTab } from "./eval-detail";
import { GateBinder } from "./gate-binder";
import type { CerebroEval, EvalBindingPhase, EvalSchedule, EvalScheduleInput, EvalStatus } from "../types";

export function EvalsPage() {
  const workflowsEnabled = useFeatureFlag("cerebro_workflows");
  const evalsEnabled = useFeatureFlag("cerebro_evals");
  const workspace = useCurrentWorkspace();
  const navigation = useNavigation();
  const queryClient = useQueryClient();
  const workspaceId = workspace?.id ?? "";
  const [search, setSearch] = useState("");
  const [statusFilter, setStatusFilter] = useState<StatusFilter>("all");
  const [showCreate, setShowCreate] = useState(false);
  const [editing, setEditing] = useState<CerebroEval | null>(null);
  const [form, setForm] = useState(EMPTY_FORM);
  const [selectedEvalId, setSelectedEvalId] = useState("");
  const [selectedRunId, setSelectedRunId] = useState("");
  const [detailEvalId, setDetailEvalId] = useState("");
  const [detailTab, setDetailTab] = useState<EvalDetailTab>("overview");

  const evalsQuery = useQuery(evalsListOptions(workspaceId));
  const workflowsQuery = useQuery(cerebroWorkflowsListOptions(workspaceId));
  const bindingsQuery = useQuery(evalBindingsOptions(workspaceId));
  const runsQuery = useQuery(evalRunsOptions(workspaceId, selectedEvalId));
  const scheduleQuery = useQuery(evalScheduleOptions(workspaceId, detailEvalId));
  const selectedRun = useMemo(() => (runsQuery.data ?? []).find((run) => run.id === selectedRunId) ?? null, [runsQuery.data, selectedRunId]);
  const detailItem = useMemo(() => (detailEvalId ? (evalsQuery.data ?? []).find((item) => item.id === detailEvalId) ?? null : null), [evalsQuery.data, detailEvalId]);

  const openDetail = (id: string, tab: EvalDetailTab = "overview") => { setDetailEvalId(id); setDetailTab(tab); setSelectedEvalId(id); setSelectedRunId(""); closeForm(); };
  const closeDetail = () => { setDetailEvalId(""); setSelectedRunId(""); };

  // The catalog key and the source path are derived from the title until the
  // user edits them by hand under Advanced. Anything opened from an existing
  // eval counts as already-typed so a prefilled key is never rewritten.
  const [touched, setTouched] = useState({ key: false, sourcePath: false });
  const [showAdvanced, setShowAdvanced] = useState(false);
  const patchForm = (patch: Partial<typeof EMPTY_FORM>) =>
    setForm((f) => withDerivedIdentity({ ...f, ...patch }, touched.key, touched.sourcePath));

  const closeForm = () => { setForm(EMPTY_FORM); setTouched({ key: false, sourcePath: false }); setEditing(null); setShowCreate(false); };
  const startCreate = () => { setForm(EMPTY_FORM); setTouched({ key: false, sourcePath: false }); setEditing(null); setShowCreate(true); };
  const startEdit = (item: CerebroEval) => { setForm(formFromEval(item)); setTouched({ key: true, sourcePath: true }); setEditing(item); setShowCreate(true); };
  const startDuplicate = (item: CerebroEval) => { setForm(duplicateForm(item)); setTouched({ key: true, sourcePath: true }); setEditing(null); setShowCreate(true); };

  const addTask = () => setForm((f) => ({ ...f, tasks: [...f.tasks, { id: crypto.randomUUID(), situation: "", expected: "", critical: false }] }));
  const updateTask = (id: string, patch: Partial<DraftTask>) => setForm((f) => ({ ...f, tasks: f.tasks.map((t) => (t.id === id ? { ...t, ...patch } : t)) }));
  const removeTask = (id: string) => setForm((f) => ({ ...f, tasks: f.tasks.filter((t) => t.id !== id) }));

  const saveMutation = useMutation({
    mutationFn: () => {
      const input = buildWriteInput(form, editing ?? undefined);
      return editing ? updateEval(editing.id, input) : createEval(input);
    },
    onSuccess: () => { queryClient.invalidateQueries({ queryKey: evalKeys.list(workspaceId) }); closeForm(); },
  });
  const deleteMutation = useMutation({ mutationFn: deleteEval, onSuccess: () => queryClient.invalidateQueries({ queryKey: evalKeys.list(workspaceId) }) });
  const statusMutation = useMutation({
    mutationFn: ({ item, to }: { item: CerebroEval; to: EvalStatus }) => updateEval(item.id, statusInput(item, to)),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: evalKeys.list(workspaceId) }),
  });
  const bindMutation = useMutation({
    mutationFn: (input: { workflowId: string; evalId: string; phase: EvalBindingPhase; blocking: boolean }) =>
      createEvalBinding({ workflow_id: input.workflowId, eval_id: input.evalId, phase: input.phase, blocking: input.blocking }),
    onSuccess: () => { queryClient.invalidateQueries({ queryKey: evalKeys.bindings(workspaceId) }); },
  });
  const unbindMutation = useMutation({ mutationFn: deleteEvalBinding, onSuccess: () => queryClient.invalidateQueries({ queryKey: evalKeys.bindings(workspaceId) }) });
  const scheduleMutation = useMutation({
    mutationFn: async ({ evalId, input }: { evalId: string; input: EvalScheduleInput | null }) => {
      if (input) await upsertEvalSchedule(evalId, input);
      else await deleteEvalSchedule(evalId);
    },
    onMutate: async (variables) => {
      const queryKey = evalKeys.schedule(workspaceId, variables.evalId);
      await queryClient.cancelQueries({ queryKey });
      const previous = queryClient.getQueryData<EvalSchedule | null>(queryKey) ?? null;
      const optimistic = variables.input ? {
        id: previous?.id ?? "optimistic",
        workspace_id: workspaceId,
        eval_id: variables.evalId,
        created_by_id: previous?.created_by_id ?? "",
        created_at: previous?.created_at ?? new Date().toISOString(),
        ...previous,
        ...variables.input,
      } satisfies EvalSchedule : null;
      queryClient.setQueryData(queryKey, optimistic);
      return { previous, queryKey };
    },
    onError: (_error, _variables, context) => {
      if (context) queryClient.setQueryData(context.queryKey, context.previous);
    },
    onSettled: (_result, _error, variables) => queryClient.invalidateQueries({ queryKey: evalKeys.schedule(workspaceId, variables.evalId) }),
  });
  const runMutation = useMutation({
    mutationFn: runEvalNow,
    onSuccess: (run) => {
      setSelectedEvalId(run.eval_id); setSelectedRunId(run.id);
      queryClient.invalidateQueries({ queryKey: evalKeys.runs(workspaceId, run.eval_id) });
    },
  });

  const counts = useMemo(() => statusCounts(evalsQuery.data ?? []), [evalsQuery.data]);
  const filtered = useMemo(() => filterEvals(evalsQuery.data ?? [], statusFilter, search), [evalsQuery.data, statusFilter, search]);
  // Resolve the workflow name for a gate, the same way the detail screen does —
  // the list used to print an eight-character slice of the workflow UUID.
  const workflowNames = useMemo(
    () => new Map((workflowsQuery.data?.workflows ?? []).map((item) => [item.id, item.name])),
    [workflowsQuery.data],
  );
  // Tell "nothing exists yet" apart from "the filter hid everything".
  const emptyMessage = (evalsQuery.data ?? []).length === 0
    ? "No evals yet. Create one to start gating work on measured quality."
    : "No evals match this filter.";

  if (!workflowsEnabled || !evalsEnabled) return null;
  if (!workspace) return <div className="flex h-full items-center justify-center text-sm text-muted-foreground">Loading workspace context…</div>;
  const submit = (event: FormEvent) => { event.preventDefault(); saveMutation.mutate(); };

  return (
    <div className="flex h-full flex-col">
      <PageHeader className="justify-between gap-3">
        <div className="flex items-center gap-3">
          <Button size="icon-sm" variant="ghost" aria-label="Back to workflows" onClick={() => navigation.push(`/${workspace.slug}/workflows`)}><ArrowLeft /></Button>
          <div className="flex min-w-0 flex-col"><h1 className="text-sm font-semibold">Evals</h1><p className="truncate text-[11px] text-muted-foreground">Versioned quality contracts and workflow gates</p></div>
        </div>
        <Button size="sm" className="shrink-0" onClick={() => { closeDetail(); showCreate ? closeForm() : startCreate(); }}><Plus className="size-4" />New eval</Button>
      </PageHeader>
      <div className="min-h-0 flex-1 overflow-y-auto p-6">
        <div className="mx-auto flex max-w-6xl flex-col gap-6">
          {detailItem ? <EvalDetail
            item={detailItem}
            versions={evalsQuery.data ?? []}
            initialTab={detailTab}
            runs={runsQuery.data ?? []}
            runsLoading={runsQuery.isLoading}
            runsError={runsQuery.isError}
            selectedRunId={selectedRunId}
            selectedRun={selectedRun}
            onSelectRun={setSelectedRunId}
            bindings={bindingsQuery.data ?? []}
            workflows={workflowsQuery.data?.workflows ?? []}
            onEdit={() => { closeDetail(); startEdit(detailItem); }}
            onDuplicate={() => { closeDetail(); startDuplicate(detailItem); }}
            onStatusChange={(to) => statusMutation.mutate({ item: detailItem, to })}
            statusPending={statusMutation.isPending}
            statusError={statusMutation.isError}
            onDelete={() => { deleteMutation.mutate(detailItem.id); closeDetail(); }}
            onRunNow={() => runMutation.mutate(detailItem.id)}
            runPending={runMutation.isPending}
            runError={runMutation.isError}
            schedule={scheduleQuery.data ?? null}
            scheduleLoading={scheduleQuery.isLoading}
            schedulePending={scheduleMutation.isPending}
            scheduleError={scheduleMutation.isError}
            onSaveSchedule={(input) => scheduleMutation.mutate({ evalId: detailItem.id, input })}
            onOpenHooks={() => navigation.push(`/${workspace.slug}/workflows`)}
            onClose={closeDetail}
            onOpenVersion={(id) => openDetail(id)}
            onOpenEvidence={(artifactId) => navigation.push(`/${workspace.slug}/documents/${artifactId}`)}
          /> : <>
          {showCreate && <form onSubmit={submit} className="grid gap-4 rounded-lg border bg-card p-4 md:grid-cols-2">
            <div className="md:col-span-2"><h2 className="text-sm font-semibold">{editing ? `Edit ${editing.title}` : "Create draft eval"}</h2><p className="text-xs text-muted-foreground">{editing ? "Change the eval — tasks, grader, and pass rate are all editable here." : "Add the tasks, pick a grader, and set the pass rate. Saved as a draft you can activate later."}</p></div>
            {!editing && <div className="md:col-span-2 flex flex-col gap-2 rounded-md border border-dashed p-3">
              <p className="text-[11px] text-muted-foreground">Start from a template — it fills in tasks, grader and pass rate, which you can then tailor. Or keep the blank form below.</p>
              <div className="flex flex-wrap gap-2">
                {EVAL_TEMPLATES.map((template) => <Button key={template.id} type="button" size="sm" variant="outline" title={template.description} onClick={() => setForm(template.build())}>{template.name}</Button>)}
                <Button type="button" size="sm" variant="ghost" onClick={() => setForm(EMPTY_FORM)}>Blank</Button>
              </div>
            </div>}
            <Field label="Title" className="md:col-span-2"><Input required value={form.title} onChange={(e) => patchForm({ title: e.target.value })} placeholder="Customer-service reply quality" /></Field>
            <Field label="Objective" className="md:col-span-2"><Textarea required value={form.objective} onChange={(e) => patchForm({ objective: e.target.value })} placeholder="What must the target get right for this eval to pass?" /></Field>
            <Field label="What is being tested">
              <NativeSelect className="w-full capitalize" aria-label="What is being tested" value={form.targetKind} onChange={(e) => patchForm({ targetKind: e.target.value })}>
                {TARGET_KINDS.map((kind) => <NativeSelectOption key={kind} value={kind}>{kind}</NativeSelectOption>)}
              </NativeSelect>
            </Field>
            <Field label="Which one"><Input required value={form.targetLocator} onChange={(e) => patchForm({ targetLocator: e.target.value })} placeholder="mention://… or https://github.com/…" /></Field>

            <div className="md:col-span-2 flex flex-col gap-2 rounded-md border p-3">
              <div className="flex items-center justify-between"><div><h3 className="text-xs font-semibold">Tasks</h3><p className="text-[11px] text-muted-foreground">Each task is a situation the target must handle and the answer it should reach. Mark a task critical if failing it must sink the whole run.</p></div><Button type="button" size="sm" variant="ghost" onClick={addTask}><Plus className="size-4" />Add task</Button></div>
              {form.tasks.length === 0 && <p className="text-xs text-muted-foreground">No tasks yet.</p>}
              {form.tasks.map((task, i) => <div key={task.id} className="grid items-start gap-2 rounded-md border bg-background/50 p-2 md:grid-cols-[1fr_1fr_auto_auto]">
                <Textarea className="min-h-9" value={task.situation} onChange={(e) => updateTask(task.id, { situation: e.target.value })} placeholder={`Situation ${i + 1}`} />
                <Input value={task.expected} onChange={(e) => updateTask(task.id, { expected: e.target.value })} placeholder="Expected answer" />
                <label className="flex h-9 items-center gap-2 whitespace-nowrap text-xs"><Checkbox checked={task.critical} onCheckedChange={(checked) => updateTask(task.id, { critical: checked === true })} />Critical</label>
                <Button type="button" size="icon-sm" variant="ghost" aria-label={`Remove task ${i + 1}`} onClick={() => removeTask(task.id)}><Trash2 className="size-4 text-destructive" /></Button>
              </div>)}
            </div>

            <div className="md:col-span-2 grid gap-3 rounded-md border p-3 md:grid-cols-2">
              <Field label="Grader">
                <NativeSelect className="w-full" aria-label="Grader" value={form.graderType} onChange={(e) => patchForm({ graderType: e.target.value })}>
                  <NativeSelectOption value="ai_judge">AI judge</NativeSelectOption>
                  <NativeSelectOption value="hard_rule">Hard rule (string match)</NativeSelectOption>
                </NativeSelect>
              </Field>
              {form.graderType === "ai_judge" ? <>
                <Field label="Judge model (optional)"><Input value={form.graderModel} onChange={(e) => patchForm({ graderModel: e.target.value })} placeholder="Default judge model" /></Field>
                <Field label="Rubric (optional)" className="md:col-span-2"><Textarea value={form.graderRubric} onChange={(e) => patchForm({ graderRubric: e.target.value })} placeholder="Extra grading guidance for the judge" /></Field>
              </> : <Field label="Match">
                <NativeSelect className="w-full" aria-label="Match" value={form.graderMatch} onChange={(e) => patchForm({ graderMatch: e.target.value })}>
                  {GRADER_MATCHES.map((m) => <NativeSelectOption key={m} value={m}>{m}</NativeSelectOption>)}
                </NativeSelect>
              </Field>}
              <Field label="Pass rate (%)"><Input type="number" min={0} max={100} value={form.passRate} onChange={(e) => patchForm({ passRate: e.target.value })} /></Field>
              <label className="flex items-end gap-2 pb-2 text-xs font-medium"><Checkbox checked={form.requireAllCritical} onCheckedChange={(checked) => patchForm({ requireAllCritical: checked === true })} />Every critical task must pass</label>
            </div>

            {/* Identity and source plumbing. These used to be the first nine
                required fields on the form, which put a code-repo file path in
                front of the user before the eval's own content. They are derived
                from the title and only shown on request. */}
            <div className="md:col-span-2 flex flex-col gap-3">
              <Button type="button" size="sm" variant="ghost" className="w-fit" onClick={() => setShowAdvanced((c) => !c)}>
                <ChevronDown className={`size-4 transition-transform${showAdvanced ? " rotate-180" : ""}`} />
                {showAdvanced ? "Hide advanced" : "Advanced — key, version and source"}
              </Button>
              {showAdvanced && <div className="grid gap-3 rounded-md border p-3 md:grid-cols-2">
                <Field label="Key"><Input required pattern="[a-z0-9]+(?:-[a-z0-9]+)*" value={form.key} onChange={(e) => { setTouched((t) => ({ ...t, key: true })); setForm((f) => ({ ...f, key: e.target.value })); }} placeholder="customer-service-quality" /></Field>
                <Field label="Version"><Input required value={form.version} onChange={(e) => patchForm({ version: e.target.value })} /></Field>
                <Field label="Target ref"><Input required value={form.targetRef} onChange={(e) => patchForm({ targetRef: e.target.value })} /></Field>
                <Field label="Source repository"><Input required value={form.sourceRepository} onChange={(e) => patchForm({ sourceRepository: e.target.value })} /></Field>
                <Field label="Source commit"><Input value={form.sourceCommit} onChange={(e) => patchForm({ sourceCommit: e.target.value })} placeholder="Pinned on activation" /></Field>
                <Field label="Source path"><Input required value={form.sourcePath} onChange={(e) => { setTouched((t) => ({ ...t, sourcePath: true })); setForm((f) => ({ ...f, sourcePath: e.target.value })); }} placeholder="evals/<key>/eval.json" /></Field>
              </div>}
            </div>
            {saveMutation.isError && <p className="text-sm text-destructive md:col-span-2">{saveMutation.error instanceof Error ? saveMutation.error.message : "Failed to save eval"}</p>}
            <div className="flex justify-end gap-2 md:col-span-2"><Button type="button" variant="ghost" onClick={closeForm}>Cancel</Button><Button type="submit" disabled={saveMutation.isPending}>{editing ? "Save changes" : "Create draft"}</Button></div>
          </form>}

          <section className="flex flex-col gap-3">
            <div className="flex flex-wrap items-center justify-between gap-3"><div><h2 className="text-sm font-semibold">Catalog</h2><p className="text-xs text-muted-foreground">One searchable definition per eval version.</p></div><Input className="max-w-xs" value={search} onChange={(e) => setSearch(e.target.value)} placeholder="Search evals…" /></div>
            <div className="flex flex-wrap gap-1.5">{STATUS_FILTERS.map((status) => <Button key={status} size="sm" variant={statusFilter === status ? "secondary" : "ghost"} aria-pressed={statusFilter === status} onClick={() => setStatusFilter(status)}><span className="capitalize">{status}</span><Badge variant="outline" className="ml-1.5">{counts[status]}</Badge></Button>)}</div>
            {evalsQuery.isError && <p className="text-sm text-destructive">Failed to load evals.</p>}
            {evalsQuery.isLoading ? <CatalogSkeleton /> : <>
              {/* Desktop: the full table. Mobile: the same rows as cards, so the
                  actions stay on screen instead of behind a sideways scroll. */}
              <div className="hidden md:block">
                <Table><TableHeader><TableRow><TableHead>Eval</TableHead><TableHead>Target</TableHead><TableHead>Version</TableHead><TableHead>Status</TableHead><TableHead>Source</TableHead><TableHead className="text-right">Actions</TableHead></TableRow></TableHeader><TableBody>
                  {filtered.length === 0 && <TableRow><TableCell colSpan={6} className="py-10 text-center text-sm text-muted-foreground"><FlaskConical className="mx-auto mb-2 size-5" />{emptyMessage}</TableCell></TableRow>}
                  {filtered.map((item: CerebroEval) => <TableRow key={item.id}>
                    <TableCell><button type="button" className="text-left font-medium hover:underline" onClick={() => openDetail(item.id)}>{item.title}</button><div className="text-xs text-muted-foreground">{item.key}</div><div className="mt-1 max-w-md text-xs text-muted-foreground">{item.objective}</div></TableCell>
                    <TableCell className="text-xs"><div>{String(item.target.kind ?? "Unknown")}</div><div className="max-w-[220px] truncate text-muted-foreground">{String(item.target.locator ?? "")}</div></TableCell>
                    <TableCell className="font-mono text-xs">{item.version}</TableCell><TableCell><Badge variant={item.status === "active" ? "default" : "secondary"}>{item.status}</Badge></TableCell>
                    <TableCell className="max-w-[220px] truncate text-xs">{String(item.source.path ?? item.source.repository ?? "Not pinned")}</TableCell>
                    <TableCell className="text-right"><div className="flex justify-end gap-1"><Button size="sm" variant="ghost" onClick={() => openDetail(item.id, "runs")}>Runs</Button><Button size="icon-sm" variant="ghost" aria-label={`Edit ${item.title}`} onClick={() => startEdit(item)}><Pencil className="size-4" /></Button><Button size="icon-sm" variant="ghost" aria-label={`Duplicate ${item.title}`} onClick={() => startDuplicate(item)}><Copy className="size-4" /></Button>
                      <ConfirmDelete
                        trigger={<Button size="icon-sm" variant="ghost" aria-label={`Delete ${item.title}`}><Trash2 className="size-4 text-destructive" /></Button>}
                        title="Delete eval?" description={`This permanently removes "${item.title} · ${item.version}". It will stop being used as a gate. This cannot be undone.`}
                        actionLabel="Delete" onConfirm={() => deleteMutation.mutate(item.id)} pending={deleteMutation.isPending}
                      />
                    </div></TableCell>
                  </TableRow>)}
                </TableBody></Table>
              </div>

              <div className="grid gap-3 md:hidden" aria-label="Evals">
                {filtered.length === 0 && <div className="rounded-lg border border-dashed p-8 text-center text-sm text-muted-foreground"><FlaskConical className="mx-auto mb-2 size-5" />{emptyMessage}</div>}
                {filtered.map((item: CerebroEval) => <article key={item.id} className="grid min-w-0 gap-3 rounded-lg border bg-card p-4 text-card-foreground">
                  <button type="button" className="min-w-0 text-left" onClick={() => openDetail(item.id)}>
                    <div className="flex flex-wrap items-center gap-2"><span className="truncate font-medium">{item.title}</span><Badge variant={item.status === "active" ? "default" : "secondary"}>{item.status}</Badge><span className="font-mono text-[11px] text-muted-foreground">{item.version}</span></div>
                    <div className="text-[11px] text-muted-foreground">{item.key}</div>
                  </button>
                  <div className="grid min-w-0 gap-1 text-xs text-muted-foreground">
                    <span className="line-clamp-2">{item.objective}</span>
                    <span className="truncate">{String(item.target.kind ?? "Unknown")} · {String(item.target.locator ?? "")}</span>
                  </div>
                  <div className="flex flex-wrap items-center gap-2 border-t pt-3">
                    <Button size="sm" variant="ghost" className="mr-auto" onClick={() => openDetail(item.id, "runs")}>Runs</Button>
                    <Button size="sm" variant="ghost" onClick={() => startEdit(item)}><Pencil className="size-4" />Edit</Button>
                    <Button size="sm" variant="ghost" onClick={() => startDuplicate(item)}><Copy className="size-4" />Duplicate</Button>
                    <ConfirmDelete
                      trigger={<Button size="sm" variant="ghost" className="text-destructive">Delete</Button>}
                      title="Delete eval?" description={`This permanently removes "${item.title} · ${item.version}". It will stop being used as a gate. This cannot be undone.`}
                      actionLabel="Delete" onConfirm={() => deleteMutation.mutate(item.id)} pending={deleteMutation.isPending}
                    />
                  </div>
                </article>)}
              </div>
            </>}
          </section>

          <section className="flex flex-col gap-3 rounded-lg border p-4">
            <div><h2 className="text-sm font-semibold">Workflow gates</h2><p className="text-xs text-muted-foreground">Bind an eval to an Issue workflow phase. A Block binding holds the workflow at that phase until the latest issue-specific run passes; Warn only notifies the owners and never blocks.</p></div>
            <GateBinder
              evals={evalsQuery.data ?? []}
              workflows={(workflowsQuery.data?.workflows ?? []).filter((item) => item.workflow_type === "issue_loop")}
              pending={bindMutation.isPending}
              onBind={(input) => bindMutation.mutate(input)}
            />
            {bindMutation.isError && <p className="text-sm text-destructive">{errorMessage(bindMutation.error, "Failed to add this gate.")}</p>}
            {unbindMutation.isError && <p className="text-sm text-destructive">{errorMessage(unbindMutation.error, "Failed to remove this gate.")}</p>}
            <div className="flex flex-col divide-y rounded-md border">{(bindingsQuery.data ?? []).map((binding) => <div key={binding.id} className="flex flex-wrap items-center justify-between gap-3 p-3"><div className="min-w-0"><span className="text-sm font-medium">{binding.eval_title}</span><span className="ml-2 font-mono text-xs text-muted-foreground">{binding.eval_version}</span><div className="text-xs text-muted-foreground">{binding.phase} · {binding.blocking ? "Blocking" : "Advisory"} · {workflowNames.get(binding.workflow_id) ?? "Unknown workflow"}</div></div><Button size="sm" variant="ghost" onClick={() => unbindMutation.mutate(binding.id)}>Remove</Button></div>)}{(bindingsQuery.data ?? []).length === 0 && <p className="p-4 text-sm text-muted-foreground">No eval gates are bound yet.</p>}</div>
          </section>
          </>}
        </div>
      </div>
    </div>
  );
}

function Field({ label, className, children }: { label: string; className?: string; children: ReactNode }) {
  return <label className={`flex flex-col gap-1.5 text-xs font-medium ${className ?? ""}`}><span>{label}</span>{children}</label>;
}

// errorMessage prefers what the server actually said over a guess at the cause.
// The gate errors used to assert "you do not have permission, or the eval is not
// active" — the screen cannot tell which, and it may have been neither.
function errorMessage(error: unknown, fallback: string): string {
  return error instanceof Error && error.message ? error.message : fallback;
}

// CatalogSkeleton stands in for the catalog while it loads: table-row height on
// desktop, card height on mobile, matching the two real renderings.
function CatalogSkeleton() {
  return (
    <div className="flex flex-col gap-2" aria-label="Loading evals">
      {Array.from({ length: 4 }).map((_, i) => <Skeleton key={i} className="h-[132px] w-full md:h-16" />)}
    </div>
  );
}
