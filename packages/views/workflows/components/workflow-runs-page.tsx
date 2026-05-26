"use client";

import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { workflowRunsOptions, workflowDetailOptions } from "@multica/core/workflows/queries";
import { useWorkspacePaths } from "@multica/core/paths";
import { useNavigation } from "../../navigation";
import { PageHeader } from "../../layout/page-header";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Badge } from "@multica/ui/components/ui/badge";
import { useT } from "../../i18n";
import type { WorkflowRunStatus } from "@multica/core/types";

interface WorkflowRunsPageProps {
  workflowId: string;
}

export function WorkflowRunsPage({ workflowId }: WorkflowRunsPageProps) {
  const { t } = useT("workflows");
  const wsId = useWorkspaceId();
  const wsPaths = useWorkspacePaths();
  const navigation = useNavigation();

  const { data: workflow } = useQuery(workflowDetailOptions(wsId, workflowId));
  const { data: runs = [], isLoading } = useQuery(workflowRunsOptions(wsId, workflowId));

  if (isLoading) {
    return (
      <div className="flex h-full items-center justify-center">
        <Skeleton className="h-[400px] w-[600px]" />
      </div>
    );
  }

  return (
    <div className="flex h-full flex-col">
      <PageHeader className="justify-between px-5 shrink-0">
        <div className="flex items-center gap-2 min-w-0">
          <button
            type="button"
            className="text-sm text-muted-foreground hover:text-foreground transition-colors"
            onClick={() => navigation.push(wsPaths.workflowDetail(workflowId))}
          >
            {workflow?.title ?? workflowId}
          </button>
          <span className="text-muted-foreground">/</span>
          <h1 className="text-sm font-medium">{t(($) => $.detail.section_run_history)}</h1>
        </div>
      </PageHeader>

      <div className="flex-1 overflow-y-auto">
        {runs.length === 0 ? (
          <div className="flex items-center justify-center py-16">
            <p className="text-sm text-muted-foreground">{t(($) => $.detail.no_runs)}</p>
          </div>
        ) : (
          <div className="divide-y">
            {runs.map((run) => (
              <button
                key={run.id}
                type="button"
                className="w-full text-left px-5 py-3 hover:bg-accent/40 transition-colors"
                onClick={() => navigation.push(wsPaths.workflowRunDetail(workflowId, run.id))}
              >
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-3">
                    <span className="text-sm font-medium">
                      {new Date(run.started_at).toLocaleDateString()}
                    </span>
                    <span className="text-sm text-muted-foreground">
                      {new Date(run.started_at).toLocaleTimeString()}
                    </span>
                  </div>
                  <Badge variant="secondary" className="text-[10px] px-1.5 h-4">
                    {t(($) => ($.run.status as Record<string, string>)[run.status as WorkflowRunStatus] ?? run.status)}
                  </Badge>
                </div>
              </button>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}
