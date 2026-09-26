"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Check, ChevronDown, Settings, Workflow } from "lucide-react";
import { toast } from "sonner";
import { api, clientErrorMessage } from "@multica/core/api";
import { useFeatureEnabled } from "@multica/core/config";
import { PROJECT_WORKFLOWS_V1_FLAG } from "@multica/core/feature-flags";
import { useWorkspaceId } from "@multica/core/hooks";
import { useIssueStatuses } from "@multica/core/issue-statuses/hooks";
import { statusColumnKeys } from "@multica/core/issues";
import { handoffStepCount, useIssueWorkflows, useSetProjectWorkflow } from "@multica/core/issue-workflows";
import { useWorkspacePaths } from "@multica/core/paths";
import { projectListOptions } from "@multica/core/projects/queries";
import type { IssueWorkflowMappingPlan, Project } from "@multica/core/types";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { useNavigation } from "../navigation";
import { useT } from "../i18n";
import { WorkflowMappingDialog } from "./workflow-mapping-dialog";

interface PendingSwitch {
  workflowId: string | null;
  name: string;
  plan: IssueWorkflowMappingPlan;
  targetKeys: string[];
}

/**
 * The project's Workflow property (MUL-7420). Each choice says how many
 * statuses and handoffs it has and where else it is used. Choosing one always
 * confirms first: the dialog maps only the statuses the new workflow lacks
 * and lists the ones that stay. Adopting a workflow is behind
 * project_workflows_v1; switching back to Default always stays available.
 */
export function ProjectWorkflowControl({ project }: { project: Project }) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const navigation = useNavigation();
  const paths = useWorkspacePaths();
  const enabled = useFeatureEnabled(PROJECT_WORKFLOWS_V1_FLAG, false);
  const { workflows } = useIssueWorkflows(wsId);
  const { data: projects = [] } = useQuery(projectListOptions(wsId));
  const catalog = useIssueStatuses(wsId);
  const setWorkflow = useSetProjectWorkflow();
  const [pending, setPending] = useState<PendingSwitch | null>(null);
  const [checking, setChecking] = useState(false);

  const current = workflows.find((w) => w.id === project.workflow_id) ?? null;
  const defaultName = t(($) => $.workflows.default_name);
  const separator = t(($) => $.workflows.list_separator);
  const projectNames = (ids: (p: Project) => boolean) =>
    projects.filter(ids).map((p) => p.title).join(separator);

  const choose = async (workflowId: string | null) => {
    if (workflowId === (project.workflow_id ?? null)) return;
    setChecking(true);
    try {
      const { plan } = await api.dryRunProjectWorkflow(project.id, workflowId);
      const target = workflows.find((w) => w.id === workflowId) ?? null;
      setPending({
        workflowId,
        name: target?.name ?? defaultName,
        plan,
        targetKeys: target ? target.steps.map((s) => s.status_key) : statusColumnKeys(catalog),
      });
    } catch (err) {
      toast.error(clientErrorMessage(err) ?? t(($) => $.workflows.project.error));
    } finally {
      setChecking(false);
    }
  };

  const apply = async (mapping: Record<string, string>) => {
    if (!pending) return;
    try {
      const moved = pending.plan.required.reduce((sum, r) => sum + r.issue_count, 0);
      await setWorkflow.mutateAsync({
        projectId: project.id,
        workflow_id: pending.workflowId,
        status_mapping: pending.plan.required.length > 0 ? mapping : undefined,
      });
      toast.success(
        t(($) => $.workflows.mapping.switched, { project: project.title, workflow: pending.name, count: moved }),
      );
      setPending(null);
    } catch (err) {
      toast.error(clientErrorMessage(err) ?? t(($) => $.workflows.project.error));
    }
  };

  if (!enabled && !project.workflow_id) return null;

  const options = [
    {
      id: null as string | null,
      name: defaultName,
      isDefault: true,
      statuses: statusColumnKeys(catalog).length,
      handoffs: 0,
      usedBy: projectNames((p) => !p.workflow_id),
    },
    ...(enabled ? workflows : current ? [current] : []).map((w) => ({
      id: w.id as string | null,
      name: w.name,
      isDefault: false,
      statuses: w.steps.length,
      handoffs: handoffStepCount(w),
      usedBy: projectNames((p) => w.project_ids.includes(p.id)),
    })),
  ];

  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger
          disabled={checking || setWorkflow.isPending}
          render={
            <button
              type="button"
              className="-ml-1 inline-flex min-w-0 items-center gap-1.5 rounded-md px-1 py-0.5 text-caption transition-colors hover:bg-accent data-popup-open:bg-accent"
            >
              <Workflow aria-hidden className="size-3.5 shrink-0 text-muted-foreground" />
              <span className="truncate font-medium">{current?.name ?? defaultName}</span>
              <ChevronDown aria-hidden className="size-3 shrink-0 text-muted-foreground" />
            </button>
          }
        />
        <DropdownMenuContent align="start" className="w-80">
          <DropdownMenuGroup>
            <DropdownMenuLabel>{t(($) => $.workflows.project.picker_title)}</DropdownMenuLabel>
            {options.map((option) => (
              <DropdownMenuItem
                key={option.id ?? "default"}
                className="items-start gap-2 py-2"
                onClick={() => void choose(option.id)}
              >
                <Workflow aria-hidden className="mt-0.5 size-3.5 shrink-0 text-muted-foreground" />
                <span className="min-w-0 flex-1">
                  <span className="flex items-center gap-2">
                    <span className="truncate font-medium">{option.name}</span>
                    {option.isDefault && (
                      <span className="shrink-0 rounded bg-muted px-1.5 py-0.5 text-micro text-muted-foreground">
                        {t(($) => $.workflows.project.default_badge)}
                      </span>
                    )}
                  </span>
                  <span className="mt-0.5 block text-caption text-muted-foreground">
                    {t(($) => $.workflows.settings.statuses, { count: option.statuses })}
                    {" · "}
                    {option.handoffs > 0
                      ? t(($) => $.workflows.settings.handoffs, { count: option.handoffs })
                      : t(($) => $.workflows.settings.no_handoffs)}
                  </span>
                  <span className="block truncate text-caption text-muted-foreground">
                    {option.usedBy
                      ? t(($) => $.workflows.project.used_by_short, { names: option.usedBy })
                      : t(($) => $.workflows.project.used_by_nobody)}
                  </span>
                </span>
                {option.id === (project.workflow_id ?? null) && <Check aria-hidden className="mt-0.5 size-3.5 shrink-0" />}
              </DropdownMenuItem>
            ))}
          </DropdownMenuGroup>
          {enabled && (
            <>
              <DropdownMenuSeparator />
              <DropdownMenuItem onClick={() => navigation.push(`${paths.settings()}?tab=workflows`)}>
                <Settings aria-hidden className="size-3.5" />
                {t(($) => $.workflows.project.manage)}
              </DropdownMenuItem>
            </>
          )}
        </DropdownMenuContent>
      </DropdownMenu>
      <WorkflowMappingDialog
        open={pending !== null}
        onOpenChange={(open) => !open && setPending(null)}
        variant={{ kind: "switch", projectName: project.title, workflowName: pending?.name ?? "" }}
        plan={pending?.plan ?? null}
        targetKeys={pending?.targetKeys ?? []}
        pending={setWorkflow.isPending}
        onConfirm={(mapping) => void apply(mapping)}
      />
    </>
  );
}
