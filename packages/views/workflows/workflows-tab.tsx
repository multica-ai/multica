"use client";

import { Fragment, useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { ChevronRight, Plus } from "lucide-react";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { useIssueStatuses } from "@multica/core/issue-statuses/hooks";
import { statusColumnKeys } from "@multica/core/issues";
import { handoffStepCount, useIssueWorkflows } from "@multica/core/issue-workflows";
import { projectListOptions } from "@multica/core/projects/queries";
import { memberListOptions } from "@multica/core/workspace/queries";
import type { IssueWorkflow, IssueWorkflowStep, Project } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { ActorAvatar } from "../common/actor-avatar";
import { StatusIcon } from "../issues/components/status-icon";
import { useStatusLabel } from "../issues/utils/status-label";
import { useNavigation } from "../navigation";
import { IssueStatusesTab } from "../settings/components/issue-statuses-tab";
import { SettingsTab } from "../settings/components/settings-layout";
import { useT } from "../i18n";
import { WorkflowEditorPage } from "./workflow-editor-page";

const WORKFLOW_PARAM = "workflow";
const DUPLICATE_PARAM = "from";
const NEW_WORKFLOW = "new";

/**
 * Settings → Workflows (MUL-7420): the workflows, then the status library
 * they all draw from. `?workflow=<id>` (or `new`) opens the editor in place,
 * so a workflow is a linkable page rather than a dialog.
 */
export function WorkflowsTab() {
  const { t } = useT("issues");
  const navigation = useNavigation();
  const wsId = useWorkspaceId();
  const { workflows, isLoaded } = useIssueWorkflows(wsId);
  const { data: projects = [] } = useQuery(projectListOptions(wsId));
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const currentUser = useAuthStore((s) => s.user);
  const role = members.find((m) => m.user_id === currentUser?.id)?.role;
  const isAdmin = role === "owner" || role === "admin";

  const open = navigation.searchParams.get(WORKFLOW_PARAM);
  const duplicateOf = navigation.searchParams.get(DUPLICATE_PARAM);
  const hrefWith = (params: Record<string, string | null>) => {
    const next = new URLSearchParams(navigation.searchParams);
    for (const [key, value] of Object.entries(params)) {
      if (value === null) next.delete(key);
      else next.set(key, value);
    }
    return `${navigation.pathname}?${next.toString()}`;
  };
  const openEditor = (id: string) => navigation.push(hrefWith({ [WORKFLOW_PARAM]: id, [DUPLICATE_PARAM]: null }));
  const backToList = () => navigation.push(hrefWith({ [WORKFLOW_PARAM]: null, [DUPLICATE_PARAM]: null }));

  const usage = useMemo(() => {
    const counts = new Map<string, number>();
    for (const workflow of workflows) {
      for (const step of workflow.steps) counts.set(step.status_key, (counts.get(step.status_key) ?? 0) + 1);
    }
    return (key: string) => counts.get(key) ?? 0;
  }, [workflows]);

  if (open) {
    const seed = open === NEW_WORKFLOW && duplicateOf ? workflows.find((w) => w.id === duplicateOf) ?? null : null;
    return (
      <WorkflowEditorPage
        key={`${open}:${duplicateOf ?? ""}`}
        workflowId={open === NEW_WORKFLOW ? null : open}
        seed={seed}
        canEdit={isAdmin}
        onBack={backToList}
        onOpen={(target) =>
          "id" in target
            ? navigation.replace(hrefWith({ [WORKFLOW_PARAM]: target.id, [DUPLICATE_PARAM]: null }))
            : navigation.push(hrefWith({ [WORKFLOW_PARAM]: NEW_WORKFLOW, [DUPLICATE_PARAM]: target.duplicateOf }))
        }
      />
    );
  }

  const defaultProjects = projects.filter((p) => !p.workflow_id);

  return (
    <SettingsTab title={t(($) => $.workflows.settings.title)} description={t(($) => $.workflows.settings.description)}>
      <section className="space-y-4">
        <header className="flex items-end gap-4">
          <div className="min-w-0 flex-1">
            <h3 className="text-title-sm font-semibold">{t(($) => $.workflows.settings.section_title)}</h3>
            <p className="mt-1 text-caption text-muted-foreground">{t(($) => $.workflows.settings.section_description)}</p>
          </div>
          {isAdmin && (
            <Button variant="outline" className="gap-1.5" onClick={() => openEditor(NEW_WORKFLOW)}>
              <Plus className="size-3.5" />
              {t(($) => $.workflows.settings.new)}
            </Button>
          )}
        </header>
        <div className="divide-y divide-surface-border overflow-hidden rounded-lg border border-surface-border bg-card">
          <DefaultWorkflowRow projects={defaultProjects} />
          {isLoaded &&
            workflows.map((workflow) => (
              <WorkflowRow
                key={workflow.id}
                workflow={workflow}
                projects={projects.filter((p) => workflow.project_ids.includes(p.id))}
                onOpen={() => openEditor(workflow.id)}
              />
            ))}
        </div>
      </section>

      <IssueStatusesTab
        embedded={{
          title: t(($) => $.workflows.settings.statuses_title),
          description: t(($) => $.workflows.settings.statuses_description),
        }}
        workflowUsage={usage}
      />
    </SettingsTab>
  );
}

function RowMeta({ statuses, handoffs }: { statuses: number; handoffs: number }) {
  const { t } = useT("issues");
  return (
    <span className="text-caption text-muted-foreground">
      {t(($) => $.workflows.settings.statuses, { count: statuses })}
      {" · "}
      {handoffs > 0
        ? t(($) => $.workflows.settings.handoffs, { count: handoffs })
        : t(($) => $.workflows.settings.no_handoffs)}
    </span>
  );
}

/** The implicit workflow of every project without one: the library itself. */
function DefaultWorkflowRow({ projects }: { projects: Project[] }) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const catalog = useIssueStatuses(wsId);
  const keys = statusColumnKeys(catalog);
  const separator = t(($) => $.workflows.list_separator);
  return (
    <button
      type="button"
      className="flex w-full items-center gap-4 px-4 py-3.5 text-left transition-colors hover:bg-accent/40"
      onClick={() => document.getElementById("status-library")?.scrollIntoView({ behavior: "smooth", block: "start" })}
    >
      <div className="min-w-0 flex-1 space-y-2">
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-body font-semibold">{t(($) => $.workflows.default_name)}</span>
          <span className="rounded bg-muted px-1.5 py-0.5 text-micro text-muted-foreground">
            {t(($) => $.workflows.settings.workspace_default)}
          </span>
          <RowMeta statuses={keys.length} handoffs={0} />
        </div>
        <StepChain steps={keys.map((key) => ({ status_key: key, handler: { type: "none" }, instructions: "" }))} />
        <p className="text-caption text-muted-foreground">
          {projects.length > 0
            ? t(($) => $.workflows.settings.used_by_default, {
                count: projects.length,
                names: projects.map((p) => p.title).join(separator),
              })
            : t(($) => $.workflows.settings.used_by_default_none)}
        </p>
      </div>
      <ChevronRight aria-hidden className="size-4 shrink-0 text-muted-foreground" />
    </button>
  );
}

function WorkflowRow({
  workflow,
  projects,
  onOpen,
}: {
  workflow: IssueWorkflow;
  projects: Project[];
  onOpen: () => void;
}) {
  const { t } = useT("issues");
  const separator = t(($) => $.workflows.list_separator);
  return (
    <button
      type="button"
      onClick={onOpen}
      className="flex w-full items-center gap-4 px-4 py-3.5 text-left transition-colors hover:bg-accent/40"
    >
      <div className="min-w-0 flex-1 space-y-2">
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-body font-semibold">{workflow.name}</span>
          <RowMeta statuses={workflow.steps.length} handoffs={handoffStepCount(workflow)} />
          {workflow.description && (
            <span className="truncate text-caption text-muted-foreground">· {workflow.description}</span>
          )}
        </div>
        <StepChain steps={workflow.steps} />
        <p className="text-caption text-muted-foreground">
          {projects.length > 0
            ? t(($) => $.workflows.settings.used_by, {
                count: projects.length,
                names: projects.map((p) => p.title).join(separator),
              })
            : t(($) => $.workflows.settings.no_projects)}
        </p>
      </div>
      <ChevronRight aria-hidden className="size-4 shrink-0 text-muted-foreground" />
    </button>
  );
}

/**
 * A workflow's steps as a chain of chips, each with its handler. Side states
 * (the closed category and `blocked`) are left out so the chain reads as the
 * path an issue takes.
 */
function StepChain({ steps }: { steps: IssueWorkflowStep[] }) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const catalog = useIssueStatuses(wsId);
  const labelOf = useStatusLabel(wsId);
  const path = steps.filter((s) => s.status_key !== "blocked" && catalog.categoryOf(s.status_key) !== "closed");
  return (
    <div className="flex flex-wrap items-center gap-x-1 gap-y-1.5">
      {path.map((step, index) => {
        const { type, id } = step.handler;
        return (
          <Fragment key={step.status_key}>
            {index > 0 && <ChevronRight aria-hidden className="size-3 text-muted-foreground" />}
            <span className="inline-flex items-center gap-1.5 rounded-full border border-surface-border px-2 py-0.5 text-caption">
              <StatusIcon
                status={step.status_key}
                category={catalog.categoryOf(step.status_key)}
                color={catalog.colorOf(step.status_key)}
                icon={catalog.iconOf(step.status_key)}
                className="h-3 w-3"
              />
              {labelOf(step.status_key)}
              {(type === "agent" || type === "squad" || type === "member") && id && (
                <ActorAvatar actorType={type} actorId={id} size="xs" profileLink={false} />
              )}
              {type === "project_lead" && (
                <span className="text-muted-foreground">· {t(($) => $.workflows.handler.project_lead)}</span>
              )}
              {type === "creator" && (
                <span className="text-muted-foreground">· {t(($) => $.workflows.handler.creator)}</span>
              )}
            </span>
          </Fragment>
        );
      })}
    </div>
  );
}
