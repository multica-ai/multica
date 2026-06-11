"use client";

import { useRef, useState } from "react";
import { Plus, Workflow as WorkflowIcon, Play, Pause, FileText, Archive, History, Trash2 } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { workflowListOptions, workflowNodesOptions, workflowEdgesOptions, useCreateWorkflow, useDeleteWorkflow, workflowTemplateListOptions, useCreateWorkflowFromTemplate } from "@multica/core/workflows/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { AppLink, useNavigation } from "../../navigation";
import { PageHeader } from "../../layout/page-header";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Button } from "@multica/ui/components/ui/button";
import { Badge } from "@multica/ui/components/ui/badge";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import { Dialog, DialogContent, DialogHeader } from "@multica/ui/components/ui/dialog";
import { ReactFlowProvider } from "@xyflow/react";
import { DAGCanvas } from "./dag-canvas";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";
import type { Workflow, WorkflowStatus } from "@multica/core/types";

const STATUS_ICON: Record<WorkflowStatus, typeof Play> = {
  active: Play,
  paused: Pause,
  draft: FileText,
  archived: Archive,
};

const STATUS_COLOR: Record<WorkflowStatus, string> = {
  active: "text-emerald-500",
  paused: "text-amber-500",
  draft: "text-muted-foreground",
  archived: "text-muted-foreground",
};

const STATUS_LABEL_KEY: Record<WorkflowStatus, string> = {
  active: "active",
  paused: "paused",
  draft: "draft",
  archived: "archived",
};

function getStatusKey(status: string): string {
  return STATUS_LABEL_KEY[status as WorkflowStatus] ?? status;
}

function WorkflowRow({ workflow }: { workflow: Workflow }) {
  const { t } = useT("workflows");
  const wsId = useWorkspaceId();
  const wsPaths = useWorkspacePaths();
  const status = (workflow.status as WorkflowStatus) || "draft";
  const Icon = STATUS_ICON[status] ?? FileText;
  const deleteWorkflow = useDeleteWorkflow(wsId);

  const { data: nodes = [] } = useQuery({
    ...workflowNodesOptions(wsId, workflow.id),
    staleTime: 5 * 60 * 1000,
  });
  const { data: edges = [] } = useQuery({
    ...workflowEdgesOptions(wsId, workflow.id),
    staleTime: 5 * 60 * 1000,
  });

  // Build mini thumbnail from real nodes/edges
  const hasNodes = nodes.length > 0;
  const minX = hasNodes ? Math.min(...nodes.map((n) => n.position_x)) : 0;
  const minY = hasNodes ? Math.min(...nodes.map((n) => n.position_y)) : 0;
  const maxX = hasNodes ? Math.max(...nodes.map((n) => n.position_x + 180)) : 1;
  const maxY = hasNodes ? Math.max(...nodes.map((n) => n.position_y + 64)) : 1;
  const vw = maxX - minX + 40;
  const vh = maxY - minY + 20;
  const thumbH = 44;
  const thumbW = Math.min(vw * (thumbH / vh), 180);

  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);
  const dialogRootRef = useRef<HTMLDivElement>(null);
  const portalContainer = dialogRootRef.current;

  const handleDelete = async () => {
    setDeleteDialogOpen(false);
    try {
      await deleteWorkflow.mutateAsync(workflow.id);
      toast.success(t(($) => $.detail.toast_deleted));
    } catch {
      toast.error(t(($) => $.detail.toast_delete_failed));
    }
  };

  return (
    <div className="group/row flex items-center gap-2 border-b px-5 py-2 text-sm transition-colors hover:bg-accent/40 h-16">
      <AppLink
        href={wsPaths.workflowDetail(workflow.id)}
        className="flex min-w-0 items-center gap-2 w-48 shrink-0"
      >
        <WorkflowIcon className="h-4 w-4 shrink-0 text-muted-foreground" />
        <span className="min-w-0 truncate font-medium">{workflow.title}</span>
        {workflow.is_template && (
          <Badge variant="outline" className="text-[9px] px-1 py-0 h-4 shrink-0">{t(($) => $.page.template_badge)}</Badge>
        )}
      </AppLink>

      {hasNodes && (
        <svg
          className="shrink-0 rounded opacity-50 group-hover/row:opacity-80 transition-opacity self-center"
          width={thumbW}
          height={thumbH}
          viewBox={`${minX - 20} ${minY - 10} ${vw} ${vh}`}
        >
          {edges.map((e) => {
            const s = nodes.find((n) => n.id === e.source_node_id);
            const t = nodes.find((n) => n.id === e.target_node_id);
            if (!s || !t) return null;
            const sx = s.position_x + 180;
            const sy = s.position_y + 32;
            const tx = t.position_x;
            const ty = t.position_y + 32;
            const midX = (sx + tx) / 2;
            return (
              <path
                key={e.id}
                d={`M ${sx},${sy} L ${midX},${sy} L ${midX},${ty} L ${tx},${ty}`}
                stroke="#94a3b8"
                strokeWidth="2"
                fill="none"
              />
            );
          })}
          {nodes.map((n) => (
            <rect
              key={n.id}
              x={n.position_x}
              y={n.position_y}
              width={180}
              height={64}
              rx="6"
              fill="currentColor"
              className="text-muted-foreground/20"
              stroke="#94a3b8"
              strokeWidth="1"
            />
          ))}
        </svg>
      )}

      <div className="flex-1" />

      <div className="flex items-center gap-x-3 text-xs shrink-0">
        <span className={cn("flex items-center gap-1 w-16 justify-center", STATUS_COLOR[status])}>
          <Icon className="h-3 w-3" />
          {t(($) => $.status[getStatusKey(status) as keyof typeof $.status])}
        </span>
        <span className="text-muted-foreground tabular-nums sm:w-16 sm:shrink-0 sm:text-center">
          {workflow.node_count}
        </span>
        <AppLink
          href={wsPaths.workflowRuns(workflow.id)}
          className="shrink-0 w-16 flex justify-center p-1 rounded hover:bg-accent text-muted-foreground hover:text-foreground transition-colors"
          title="Run history"
        >
          <History className="h-3.5 w-3.5" />
        </AppLink>
        <button
          type="button"
          className="shrink-0 w-20 flex justify-center p-1 rounded hover:bg-destructive/10 text-muted-foreground hover:text-destructive transition-colors"
          title="Delete"
          onClick={() => setDeleteDialogOpen(true)}
        >
          <Trash2 className="h-3.5 w-3.5" />
        </button>
      </div>

      {/* Dialog portal container for iframe compatibility */}
      <div ref={dialogRootRef} />

      {/* Delete confirm dialog */}
      <AlertDialog open={deleteDialogOpen} onOpenChange={setDeleteDialogOpen}>
        <AlertDialogContent container={portalContainer}>
          <AlertDialogHeader>
            <AlertDialogTitle>{t(($) => $.detail.delete_dialog.title)}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.detail.delete_dialog.description, { title: workflow.title })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t(($) => $.detail.delete_dialog.cancel)}</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              onClick={handleDelete}
              disabled={deleteWorkflow.isPending}
            >
              {deleteWorkflow.isPending
                ? t(($) => $.detail.delete_dialog.deleting)
                : t(($) => $.detail.delete_dialog.confirm)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

export function WorkflowsPage() {
  const { t } = useT("workflows");
  const wsId = useWorkspaceId();
  const wsPaths = useWorkspacePaths();
  const { push } = useNavigation();
  const { data, isLoading } = useQuery(workflowListOptions(wsId));
  const createWorkflow = useCreateWorkflow(wsId);
  const createFromTemplate = useCreateWorkflowFromTemplate(wsId);
  const { data: templateData } = useQuery(workflowTemplateListOptions(wsId));
  const backendTemplates = templateData?.workflows ?? [];
  const workflows = data?.workflows ?? [];
  const [statusFilter, setStatusFilter] = useState<WorkflowStatus | "all">("all");
  const [selectedTemplateId, setSelectedTemplateId] = useState<string | null>(null);
  const selectedTemplate = backendTemplates.find((t) => t.id === selectedTemplateId) ?? null;

  // Lazy-fetch nodes & edges for the previewed template
  const { data: previewNodes = [] } = useQuery({
    ...workflowNodesOptions(selectedTemplate?.workspace_id ?? wsId, selectedTemplateId ?? ""),
    enabled: !!selectedTemplateId,
  });
  const { data: previewEdges = [] } = useQuery({
    ...workflowEdgesOptions(selectedTemplate?.workspace_id ?? wsId, selectedTemplateId ?? ""),
    enabled: !!selectedTemplateId,
  });

  const handleCreate = async () => {
    const workflow = await createWorkflow.mutateAsync({ title: "Untitled Workflow" });
    push(wsPaths.workflowDetail(workflow.id));
  };

  const filtered = statusFilter === "all"
    ? workflows
    : workflows.filter((w) => w.status === statusFilter);

  return (
    <div className="flex h-full flex-col">
      <PageHeader className="justify-between px-5">
        <div className="flex items-center gap-2">
          <WorkflowIcon className="h-4 w-4 text-muted-foreground" />
          <h1 className="text-sm font-medium">{t(($) => $.page.title)}</h1>
          {!isLoading && workflows.length > 0 && (
            <span className="text-xs text-muted-foreground tabular-nums">{workflows.length}</span>
          )}
        </div>
        <Button size="sm" variant="outline" onClick={handleCreate}>
          <Plus className="h-3.5 w-3.5 mr-1" />
          {t(($) => $.page.new_workflow)}
        </Button>
      </PageHeader>

      {/* Template cards — driven by backend templates */}
      {backendTemplates.length > 0 && (
      <div className="px-5 py-3 border-b">
        <div className="flex items-center gap-2 mb-2">
          <WorkflowIcon className="h-3.5 w-3.5 text-amber-500" />
          <span className="text-xs font-medium text-muted-foreground uppercase tracking-wider">{t(($) => $.page.template_section)}</span>
        </div>
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-3">
          {backendTemplates.map((tmpl) => (
            <button
              key={tmpl.id}
              type="button"
              className="flex flex-col items-start gap-1.5 rounded-lg border px-4 py-3 text-left transition-colors hover:bg-accent/40 hover:border-primary/30"
              onClick={() => setSelectedTemplateId(tmpl.id)}
            >
              <div className="flex items-center gap-2 w-full">
                <WorkflowIcon className="h-4 w-4 shrink-0 text-primary" />
                <span className="text-sm font-medium truncate">{tmpl.title}</span>
              </div>
              <p className="text-xs text-muted-foreground line-clamp-2">{tmpl.description || "No description"}</p>
              <div className="text-[10px] text-muted-foreground mt-0.5">
                {tmpl.node_count} nodes
              </div>
            </button>
          ))}
        </div>
      </div>
      )}

      <div className="flex-1 overflow-y-auto">
        {isLoading ? (
          <>
            <div className="sticky top-0 z-[1] hidden h-8 items-center gap-2 border-b bg-muted/30 px-5 sm:flex">
              <Skeleton className="h-3 w-12 flex-1 max-w-[48px]" />
              <Skeleton className="h-3 w-10 shrink-0" />
              <Skeleton className="h-3 w-10 shrink-0" />
            </div>
            <div className="space-y-2 p-4 sm:space-y-1 sm:p-5 sm:pt-1">
              {Array.from({ length: 4 }).map((_, i) => (
                <Skeleton key={i} className="h-[72px] w-full sm:h-11" />
              ))}
            </div>
          </>
        ) : workflows.length === 0 ? (
          <div className="flex flex-col items-center py-16 px-5">
            <WorkflowIcon className="h-10 w-10 mb-3 text-muted-foreground opacity-30" />
            <p className="text-sm text-muted-foreground">{t(($) => $.page.empty.title)}</p>
            <p className="text-xs text-muted-foreground mt-1 mb-6">
              {t(($) => $.page.empty.description)}
            </p>
            <Button size="sm" variant="outline" onClick={handleCreate}>
              <Plus className="h-3.5 w-3.5 mr-1" />
              {t(($) => $.page.new_workflow)}
            </Button>
          </div>
        ) : (
          <>
            <div className="sticky top-0 z-[1] hidden h-8 items-center gap-2 border-b bg-muted/30 px-5 text-xs font-medium text-muted-foreground sm:flex">
              <span className="shrink-0 w-4" />
              <span className="min-w-0 flex-1">{t(($) => $.page.table.name)}</span>
              <span className="w-20 text-center shrink-0">{t(($) => $.page.table.status)}</span>
              <span className="w-16 text-center shrink-0">{t(($) => $.page.table.nodes)}</span>
              <span className="w-16 shrink-0 text-center">{t(($) => $.page.table.runs)}</span>
              <span className="w-20 shrink-0 text-center">{t(($) => $.page.table.actions)}</span>
            </div>
            <div className="flex gap-1 px-5 py-2 border-b">
              {(["all", "active", "draft", "paused", "archived"] as const).map((s) => (
                <Button
                  key={s}
                  variant={statusFilter === s ? "secondary" : "ghost"}
                  size="sm"
                  className="h-6 text-xs px-2"
                  onClick={() => setStatusFilter(s)}
                >
                  {t(($) => $.page.filter[s])}
                </Button>
              ))}
            </div>
            {filtered.map((workflow) => (
              <WorkflowRow key={workflow.id} workflow={workflow} />
            ))}
          </>
        )}
      </div>

      {/* Template preview dialog */}
      <Dialog open={!!selectedTemplateId} onOpenChange={(open) => { if (!open) setSelectedTemplateId(null); }}>
        <DialogContent className="sm:max-w-2xl max-h-[85vh] flex flex-col">
          <DialogHeader>
            <span className="text-sm font-medium">{selectedTemplate?.title}</span>
          </DialogHeader>
          <p className="text-xs text-muted-foreground -mt-2">{selectedTemplate?.description}</p>
          <div className="h-[400px] overflow-hidden rounded-lg border bg-muted/20">
            <ReactFlowProvider>
              <DAGCanvas nodes={previewNodes} edges={previewEdges} />
            </ReactFlowProvider>
          </div>
          <div className="flex items-center justify-between pt-2">
            <span className="text-xs text-muted-foreground">
              {previewNodes.length} nodes · {previewEdges.length} edges
            </span>
            <Button
              size="sm"
              onClick={async () => {
                if (!selectedTemplate) return;
                const workflow = await createFromTemplate.mutateAsync({
                  templateId: selectedTemplate.id,
                  title: selectedTemplate.title,
                });
                setSelectedTemplateId(null);
                push(wsPaths.workflowDetail(workflow.id));
              }}
              disabled={createFromTemplate.isPending}
            >
              {createFromTemplate.isPending ? t(($) => $.page.creating) : t(($) => $.page.use_template)}
            </Button>
          </div>
        </DialogContent>
      </Dialog>
    </div>
  );
}
