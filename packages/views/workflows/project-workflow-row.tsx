"use client";

import { useState, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { Check, ChevronDown, Settings, Workflow } from "@multica/ui/icons";
import { toast } from "sonner";
import { api, clientErrorMessage } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { handoffStepCount, resolveProjectWorkflow, useIssueWorkflows, useSetProjectWorkflow } from "@multica/core/issue-workflows";
import { useWorkspacePaths } from "@multica/core/paths";
import { projectListOptions } from "@multica/core/projects/queries";
import type { IssueWorkflow, IssueWorkflowMappingPlan, Project } from "@multica/core/types";
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
import { useWorkflowPermissions } from "./permissions";

interface PendingSwitch {
  workflowId: string | null;
  name: string;
  plan: IssueWorkflowMappingPlan;
  targetKeys: string[];
}

/**
 * Switching a project's workflow (MUL-7420). Choosing one always confirms
 * first: the dialog maps only the statuses the new workflow lacks and lists
 * the ones that stay. Render `dialog` once wherever `choose` is offered.
 */
export function useProjectWorkflowSwitch(project: Project): {
  choose: (workflow: IssueWorkflow) => Promise<void>;
  busy: boolean;
  dialog: ReactNode;
} {
  const { t } = useT("issues");
  const setWorkflow = useSetProjectWorkflow();
  const [pending, setPending] = useState<PendingSwitch | null>(null);
  const [checking, setChecking] = useState(false);

  const choose = async (workflow: IssueWorkflow) => {
    const workflowId = workflow.is_default ? null : workflow.id;
    if (workflowId === (project.workflow_id ?? null)) return;
    setChecking(true);
    try {
      const { plan } = await api.dryRunProjectWorkflow(project.id, workflowId);
      setPending({
        workflowId,
        name: workflow.name,
        plan,
        targetKeys: workflow.steps.map((s) => s.status_key),
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

  return {
    choose,
    busy: checking || setWorkflow.isPending,
    dialog: (
      <WorkflowMappingDialog
        open={pending !== null}
        onOpenChange={(open) => !open && setPending(null)}
        variant={{ kind: "switch", projectName: project.title, workflowName: pending?.name ?? "" }}
        plan={pending?.plan ?? null}
        targetKeys={pending?.targetKeys ?? []}
        pending={setWorkflow.isPending}
        onConfirm={(mapping) => void apply(mapping)}
      />
    ),
  };
}

/** The workflows a project may switch to, each with what it has and who uses it. */
export function WorkflowChoiceItems({
  project,
  onChoose,
}: {
  project: Project;
  onChoose: (workflow: IssueWorkflow) => void;
}) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const { workflows } = useIssueWorkflows(wsId);
  const { data: projects = [] } = useQuery(projectListOptions(wsId));
  const current = resolveProjectWorkflow(workflows, project);
  const separator = t(($) => $.workflows.list_separator);

  return (
    <>
      {workflows.map((workflow) => {
        const usedBy = projects
          .filter((p) => (workflow.is_default ? !p.workflow_id : workflow.project_ids.includes(p.id)))
          .map((p) => p.title)
          .join(separator);
        return (
          <DropdownMenuItem key={workflow.id} className="items-start gap-2 py-2" onClick={() => onChoose(workflow)}>
            <Workflow aria-hidden className="mt-0.5 size-3.5 shrink-0 text-muted-foreground" />
            <span className="min-w-0 flex-1">
              <span className="flex items-center gap-2">
                <span className="truncate font-medium">{workflow.name}</span>
                {workflow.is_default && (
                  <span className="shrink-0 rounded-xs bg-muted px-1.5 py-0.5 text-micro text-muted-foreground">
                    {t(($) => $.workflows.project.default_badge)}
                  </span>
                )}
              </span>
              <span className="mt-0.5 block text-caption text-muted-foreground">
                {t(($) => $.workflows.settings.statuses, { count: workflow.steps.length })}
                {handoffStepCount(workflow) > 0 &&
                  ` · ${t(($) => $.workflows.settings.handoffs, { count: handoffStepCount(workflow) })}`}
              </span>
              <span className="block truncate text-caption text-muted-foreground">
                {usedBy
                  ? t(($) => $.workflows.project.used_by_short, { names: usedBy })
                  : t(($) => $.workflows.project.used_by_nobody)}
              </span>
            </span>
            {workflow.id === current?.id && <Check aria-hidden className="mt-0.5 size-3.5 shrink-0" />}
          </DropdownMenuItem>
        );
      })}
    </>
  );
}

/**
 * The project's Workflow property (MUL-7420). Admins and the project lead
 * switch it; everyone else sees which one it uses.
 */
export function ProjectWorkflowControl({ project }: { project: Project }) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const navigation = useNavigation();
  const paths = useWorkspacePaths();
  const { workflows } = useIssueWorkflows(wsId);
  const { canManageProject } = useWorkflowPermissions(project);
  const { choose, busy, dialog } = useProjectWorkflowSwitch(project);
  const current = resolveProjectWorkflow(workflows, project);

  const name = current?.name ?? t(($) => $.workflows.default_name);
  const handoffs = current ? handoffStepCount(current) : 0;
  const handoffLabel = handoffs > 0 ? t(($) => $.workflows.settings.handoffs, { count: handoffs }) : null;

  if (!canManageProject) {
    return (
      <span title={name} className="-ml-1 inline-flex min-w-0 items-center gap-1.5 px-1 py-0.5 text-caption">
        <Workflow aria-hidden className="size-3.5 shrink-0 text-muted-foreground" />
        <span className="truncate font-medium">{name}</span>
      </span>
    );
  }

  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger
          disabled={busy}
          render={
            <button
              type="button"
              title={handoffLabel ? `${name} · ${handoffLabel}` : name}
              className="-ml-1 inline-flex min-w-0 max-w-full items-center gap-1.5 rounded-md px-1 py-0.5 text-caption transition-colors hover:bg-accent data-popup-open:bg-accent"
            >
              <Workflow aria-hidden className="size-3.5 shrink-0 text-muted-foreground" />
              {/* Only the name competes for the narrow sidebar row (MUL-8119):
                  a sibling summary, however it is weighted, still takes width
                  from the name. The handoff count lives in the title and the
                  picker items instead. */}
              <span className="min-w-0 truncate font-medium">{name}</span>
              <ChevronDown aria-hidden className="size-3 shrink-0 text-muted-foreground" />
            </button>
          }
        />
        <DropdownMenuContent align="start" className="w-80">
          <DropdownMenuGroup>
            <DropdownMenuLabel>{t(($) => $.workflows.project.picker_title)}</DropdownMenuLabel>
            <WorkflowChoiceItems project={project} onChoose={(w) => void choose(w)} />
          </DropdownMenuGroup>
          <DropdownMenuSeparator />
          <DropdownMenuItem onClick={() => navigation.push(`${paths.settings()}?tab=workflows`)}>
            <Settings aria-hidden className="size-3.5" />
            {t(($) => $.workflows.project.manage)}
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
      {dialog}
    </>
  );
}
