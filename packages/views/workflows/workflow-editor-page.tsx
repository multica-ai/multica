"use client";

import { useMemo, useState } from "react";
import { AlertTriangle, ArrowLeft, History, Play, Redo2, Rocket, Save, Undo2 } from "lucide-react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import { useAuthStore } from "@multica/core/auth";
import { agentListOptions, memberListOptions } from "@multica/core/workspace/queries";
import {
  createWorkflowGraph,
  validateWorkflowGraph,
  useStartWorkflowRun,
  usePublishWorkflow,
  usePreviewWorkflowUpgrade,
  useUpgradeWorkflow,
  useValidateWorkflow,
  useWorkflowDraft,
  useWorkflowHistory,
  workflowDetailOptions,
  workflowRunDetailOptions,
  workflowKeys,
  unsupportedWorkflowNodeTypes,
  type Workflow,
} from "@multica/core/workflows";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { Label } from "@multica/ui/components/ui/label";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@multica/ui/components/ui/dialog";
import { useNavigation } from "../navigation";
import { useWorkspacePaths } from "@multica/core/paths";
import { useT } from "../i18n";
import { WorkflowCanvas } from "./workflow-canvas";
import { WorkflowChatPanel } from "./workflow-chat-panel";
import { WorkflowRunPanel } from "./workflow-run-panel";

function structuredInputType(type: unknown): boolean {
  return type === "json" || type === "array" || type === "attachment" || type === "attachments";
}

export function WorkflowEditorPage({ workflowId }: { workflowId: string }) {
  const { t } = useT("workflows");
  const wsId = useWorkspaceId();
  const user = useAuthStore((state) => state.user);
  const navigation = useNavigation();
  const paths = useWorkspacePaths();
  const queryClient = useQueryClient();
  const detail = useQuery(workflowDetailOptions(wsId, workflowId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const server = detail.data;
  const draft = useWorkflowDraft(wsId, server);
  const effective = useMemo<Workflow | undefined>(() => {
    if (!server || !draft.draft) return server;
    return { ...server, ...draft.draft };
  }, [draft.draft, server]);
  const unsupportedNodeTypes = useMemo(() => effective ? unsupportedWorkflowNodeTypes(effective.graph) : [], [effective]);
  const readOnly = unsupportedNodeTypes.length > 0;
  const history = useWorkflowHistory(wsId, workflowId);
  const startRun = useStartWorkflowRun(wsId, workflowId);
  const validate = useValidateWorkflow(wsId, workflowId);
  const publish = usePublishWorkflow(wsId, workflowId);
  const previewUpgrade = usePreviewWorkflowUpgrade(wsId, workflowId);
  const upgrade = useUpgradeWorkflow(wsId, workflowId);
  const [selectedNodeId, setSelectedNodeId] = useState<string | null>(null);
  const [chatOpen, setChatOpen] = useState(true);
  const [historyOpen, setHistoryOpen] = useState(false);
  const [selectedRunId, setSelectedRunId] = useState<string | null>(null);
  const [runInput, setRunInput] = useState("");
  const [runInputValues, setRunInputValues] = useState<Record<string, unknown>>({});
  const [runDialog, setRunDialog] = useState(false);
  const [upgradeDialog, setUpgradeDialog] = useState(false);
  const legacy = (effective?.graph.schemaVersion ?? 1) < 2;
  const selectedRun = useQuery({ ...workflowRunDetailOptions(wsId, workflowId, selectedRunId ?? ""), enabled: historyOpen && !!selectedRunId });
  const canvasStatuses = useMemo(() => {
    if (!selectedRun.data) return undefined;
    return Object.fromEntries(selectedRun.data.nodes.map((node) => [node.nodeId, node.status]));
  }, [selectedRun.data]);

  const startFields = useMemo(() => {
    const start = effective?.graph.nodes.find((node) => node.type === "start");
    const fields = start?.config?.fields;
    return Array.isArray(fields) ? fields.filter((field): field is Record<string, unknown> => !!field && typeof field === "object" && typeof (field as Record<string, unknown>).key === "string") : [];
  }, [effective]);

  if (detail.isPending) return <div className="p-6 text-body" role="status">{t(($) => $.loading)}</div>;
  if (detail.isError || !effective) return <div className="p-6 text-body" role="alert">{detail.error?.message ?? t(($) => $.error)}</div>;

  const flush = async () => draft.save();
  const handleRun = async () => {
    try {
      const saved = await flush();
      const errors = validateWorkflowGraph(saved.graph);
      if (errors.length) { toast.error(`${t(($) => $.invalid_graph)}: ${errors.join(", ")}`); return; }
      let inputValues: Record<string, unknown> | undefined;
      if (startFields.length) {
        inputValues = {};
        for (const field of startFields) {
          const key = String(field.key);
          const type = typeof field.type === "string" ? field.type : "text";
          const value = runInputValues[key];
          if (value === undefined || value === null || value === "") {
            if (field.required === true) { toast.error(t(($) => $.run_input_invalid)); return; }
            continue;
          }
          if (structuredInputType(type)) {
            if (typeof value !== "string") { toast.error(t(($) => $.run_input_invalid)); return; }
            try { inputValues[key] = JSON.parse(value); } catch { toast.error(t(($) => $.run_input_invalid)); return; }
          } else if (type === "integer" && (typeof value !== "number" || !Number.isInteger(value))) {
            toast.error(t(($) => $.run_input_invalid));
            return;
          } else {
            inputValues[key] = value;
          }
        }
      }
      const run = await startRun.mutateAsync({ input: startFields.length ? undefined : runInput.trim(), inputValues, expectedRevision: saved.revision, idempotencyKey: crypto.randomUUID() });
      setRunDialog(false);
      setHistoryOpen(true);
      setSelectedRunId(run.id);
      toast.success(t(($) => $.run_started));
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t(($) => $.error));
    }
  };
  const handlePublish = async () => {
    try {
      const saved = await flush();
      const result = await validate.mutateAsync({ expectedRevision: saved.revision, target: "publish" });
      if (!result.valid) {
        toast.error(`${t(($) => $.publish_invalid)}: ${result.errors.map((item) => item.message).join("、")}`);
        return;
      }
      await publish.mutateAsync({ expectedRevision: saved.revision, idempotencyKey: crypto.randomUUID() });
      toast.success(t(($) => $.published));
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t(($) => $.error));
    }
  };
  const handleUpgradePreview = async () => {
    try {
      await previewUpgrade.mutateAsync();
      setUpgradeDialog(true);
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t(($) => $.error));
    }
  };
  const handleUpgrade = async () => {
    try {
      const updated = await upgrade.mutateAsync({ expectedRevision: effective.revision });
      queryClient.setQueryData(workflowKeys.detail(wsId, workflowId), updated);
      draft.reset();
      setUpgradeDialog(false);
      toast.success(t(($) => $.upgrade_applied));
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t(($) => $.error));
    }
  };
  const changeHistory = async (direction: "undo" | "redo") => {
    try {
      const saved = await flush();
      const changed = await history.mutateAsync({ direction, expectedRevision: saved.revision });
      queryClient.setQueryData(workflowKeys.detail(wsId, workflowId), changed);
      draft.reset();
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t(($) => $.error));
    }
  };

  return <div className="flex h-full min-h-0 flex-col">
    <header className="flex shrink-0 flex-wrap items-center gap-2 border-b px-4 py-3">
      <Button variant="ghost" size="icon-sm" aria-label={t(($) => $.back)} onClick={() => navigation.push(paths.workflows())}><ArrowLeft className="size-4" /></Button>
      <Input className="h-8 max-w-sm text-body font-semibold" value={effective.name} onChange={(event) => draft.update({ name: event.target.value })} aria-label={t(($) => $.name)} disabled={readOnly} />
      <span className="text-micro text-muted-foreground">{t(($) => $.save_status[draft.saveStatus])}</span>
      <span className="rounded-full border bg-muted/50 px-2 py-0.5 text-micro text-muted-foreground">{effective.publishedReleaseId ? t(($) => $.published_badge) : t(($) => $.draft_badge, { revision: effective.revision })}</span>
      <div className="ml-auto flex items-center gap-1">
        <Button variant="ghost" size="icon-sm" disabled={readOnly || !effective.canUndo || draft.isSaving} aria-label={t(($) => $.undo)} onClick={() => void changeHistory("undo")}><Undo2 className="size-4" /></Button>
        <Button variant="ghost" size="icon-sm" disabled={readOnly || !effective.canRedo || draft.isSaving} aria-label={t(($) => $.redo)} onClick={() => void changeHistory("redo")}><Redo2 className="size-4" /></Button>
        <Button variant="outline" size="sm" disabled={readOnly || !draft.isDirty || draft.isSaving} onClick={() => void flush()}><Save className="size-4" />{t(($) => $.save)}</Button>
        <Button variant="outline" size="sm" disabled={readOnly || legacy || publish.isPending || validate.isPending || draft.isSaving} onClick={() => void handlePublish()}><Rocket className="size-4" />{publish.isPending ? t(($) => $.publishing) : t(($) => $.publish)}</Button>
        {legacy && <Button variant="outline" size="sm" disabled={previewUpgrade.isPending || upgrade.isPending} onClick={() => void handleUpgradePreview()}><AlertTriangle className="size-4" />{t(($) => $.upgrade)}</Button>}
        <Button variant="outline" size="sm" onClick={() => setHistoryOpen(true)}><History className="size-4" />{t(($) => $.history)}</Button>
        <Button size="sm" disabled={readOnly || legacy || startRun.isPending || draft.isSaving} onClick={() => setRunDialog(true)}><Play className="size-4" />{t(($) => $.run)}</Button>
        <Button variant="ghost" size="sm" onClick={() => setChatOpen((value) => !value)}>{chatOpen ? t(($) => $.hide_chat) : t(($) => $.show_chat)}</Button>
      </div>
    </header>
    {readOnly && <div className="flex items-start gap-2 border-b bg-muted px-4 py-2 text-caption" role="alert"><AlertTriangle className="mt-0.5 size-4 shrink-0" /><span>{t(($) => $.unsupported_node_readonly, { types: unsupportedNodeTypes.join(", ") })}</span></div>}
    {draft.error && <div className="border-b bg-destructive/10 px-4 py-2 text-caption text-destructive" role="alert">{draft.error.message}</div>}
    <div className="flex min-h-0 flex-1">
      <WorkflowCanvas graph={effective.graph} onChange={(graph) => draft.update({ graph })} selectedNodeId={selectedNodeId} onSelect={setSelectedNodeId} agents={agents} members={members} statuses={canvasStatuses} readOnly={readOnly} />
      {chatOpen && <WorkflowChatPanel wsId={wsId} workflow={effective} selectedNodeId={selectedNodeId} agents={agents} userId={user?.id} save={flush} readOnly={readOnly} />}
    </div>
    <WorkflowRunPanel wsId={wsId} workflowId={workflowId} open={historyOpen} onOpenChange={setHistoryOpen} selectedRunId={selectedRunId} onSelect={setSelectedRunId} />
    <Dialog open={upgradeDialog} onOpenChange={setUpgradeDialog}><DialogContent><DialogHeader><DialogTitle>{t(($) => $.upgrade_title)}</DialogTitle></DialogHeader>{previewUpgrade.data && <div className="space-y-3 text-caption"><p>{previewUpgrade.data.autoConvertible ? t(($) => $.upgrade_auto) : t(($) => $.upgrade_manual)}</p>{previewUpgrade.data.warnings.length > 0 && <div><p className="font-medium">{t(($) => $.upgrade_warnings)}</p><ul className="list-disc pl-5">{previewUpgrade.data.warnings.map((item, index) => <li key={`${item.code}-${index}`}>{item.message}</li>)}</ul></div>}{previewUpgrade.data.issues.length > 0 && <div className="rounded-lg border border-destructive/40 p-3"><p className="font-medium text-destructive">{t(($) => $.upgrade_issues)}</p><ul className="list-disc pl-5">{previewUpgrade.data.issues.map((item, index) => <li key={`${item.code}-${item.nodeId ?? ""}-${index}`}>{item.message}</li>)}</ul></div>}</div>}<DialogFooter><Button variant="outline" onClick={() => setUpgradeDialog(false)}>{t(($) => $.cancel)}</Button><Button disabled={!previewUpgrade.data || upgrade.isPending} onClick={() => void handleUpgrade()}>{t(($) => $.upgrade_apply)}</Button></DialogFooter></DialogContent></Dialog>
    <Dialog open={runDialog} onOpenChange={setRunDialog}><DialogContent><DialogHeader><DialogTitle>{t(($) => $.run_title)}</DialogTitle></DialogHeader>{startFields.length ? <div className="space-y-3">{startFields.map((field) => { const key = String(field.key); const rawType = typeof field.type === "string" ? field.type : "text"; const type = rawType === "boolean" ? "boolean" : rawType === "number" || rawType === "integer" ? "number" : "text"; const label = `${String(field.label ?? key)}${field.required === true ? " *" : ""}`; return <div key={key} className="space-y-1">{type === "boolean" ? <label className="flex items-center gap-2 text-caption"><input type="checkbox" checked={runInputValues[key] === true} onChange={(event) => setRunInputValues((current) => ({ ...current, [key]: event.target.checked }))} />{label}</label> : structuredInputType(rawType) ? <><Label htmlFor={`workflow-run-input-${key}`}>{label}</Label><Textarea id={`workflow-run-input-${key}`} value={runInputValues[key] == null ? "" : String(runInputValues[key])} onChange={(event) => setRunInputValues((current) => ({ ...current, [key]: event.target.value }))} placeholder={typeof field.placeholder === "string" ? field.placeholder : undefined} rows={4} /></> : <><Label htmlFor={`workflow-run-input-${key}`}>{label}</Label><Input id={`workflow-run-input-${key}`} type={type} step={rawType === "integer" ? 1 : undefined} value={runInputValues[key] == null ? "" : String(runInputValues[key])} onChange={(event) => setRunInputValues((current) => ({ ...current, [key]: type === "number" ? (event.target.value === "" ? undefined : Number(event.target.value)) : event.target.value }))} maxLength={typeof field.max_length === "number" ? field.max_length : undefined} placeholder={typeof field.placeholder === "string" ? field.placeholder : undefined} /></>}</div>; })}</div> : <div className="space-y-2"><Label htmlFor="workflow-run-input">{t(($) => $.run_input)}</Label><Textarea id="workflow-run-input" value={runInput} onChange={(event) => setRunInput(event.target.value)} placeholder={t(($) => $.run_input_placeholder)} rows={6} /></div>}<DialogFooter><Button variant="outline" onClick={() => setRunDialog(false)}>{t(($) => $.cancel)}</Button><Button disabled={startRun.isPending} onClick={() => void handleRun()}><Play className="size-4" />{t(($) => $.run)}</Button></DialogFooter></DialogContent></Dialog>
  </div>;
}

export function NewWorkflowGraph() { return createWorkflowGraph(); }
