"use client";

import { useState } from "react";
import { Plus, Workflow as WorkflowIcon, Play, Pause, FileText, Archive, Zap } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { workflowListOptions, useCreateWorkflow } from "@multica/core/workflows/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { AppLink, useNavigation } from "../../navigation";
import { PageHeader } from "../../layout/page-header";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Button } from "@multica/ui/components/ui/button";
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
  const wsPaths = useWorkspacePaths();
  const status = (workflow.status as WorkflowStatus) || "draft";
  const Icon = STATUS_ICON[status] ?? FileText;

  return (
    <div className="group/row flex flex-col gap-2 border-b px-4 py-3 text-sm transition-colors hover:bg-accent/40 sm:h-11 sm:flex-row sm:items-center sm:gap-2 sm:border-b-0 sm:px-5 sm:py-0">
      <AppLink
        href={wsPaths.workflowDetail(workflow.id)}
        className="flex min-w-0 items-center gap-2 sm:flex-1"
      >
        <WorkflowIcon className="h-4 w-4 shrink-0 text-muted-foreground" />
        <span className="min-w-0 flex-1 truncate font-medium">{workflow.title}</span>
      </AppLink>

      <div className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1 pl-6 text-xs sm:contents sm:pl-0">
        <span className={cn("flex items-center gap-1 sm:w-20 sm:shrink-0 sm:justify-center", STATUS_COLOR[status])}>
          <Icon className="h-3 w-3" />
          {t(($) => $.status[getStatusKey(status) as keyof typeof $.status])}
        </span>
        <span className="text-muted-foreground tabular-nums sm:w-16 sm:shrink-0 sm:text-center">
          {workflow.node_count}
        </span>
      </div>
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
  const workflows = data?.workflows ?? [];
  const [statusFilter, setStatusFilter] = useState<WorkflowStatus | "all">("all");

  const handleCreate = async () => {
    const workflow = await createWorkflow.mutateAsync({ title: "Untitled Workflow" });
    push(wsPaths.workflowDetail(workflow.id));
  };

  const handleCreateFromTemplate = async () => {
    const workflow = await createWorkflow.mutateAsync({
      title: "AI Coding 全链路",
      template: "ai-coding",
    });
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
        <Button size="sm" onClick={handleCreateFromTemplate} disabled={createWorkflow.isPending}>
          <Zap className="h-3.5 w-3.5 mr-1" />
          使用模板
        </Button>
      </PageHeader>

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
    </div>
  );
}
