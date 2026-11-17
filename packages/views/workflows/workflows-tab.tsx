"use client";

import { useMemo, useState, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  ArrowRight,
  ChevronDown,
  ChevronRight,
  Info,
  LayoutTemplate,
  Plus,
  Search,
  User,
} from "@multica/ui/icons";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { useIssueStatuses } from "@multica/core/issue-statuses/hooks";
import { statusColumnKeys } from "@multica/core/issues";
import { useIssueWorkflows } from "@multica/core/issue-workflows";
import { projectListOptions } from "@multica/core/projects/queries";
import { memberListOptions } from "@multica/core/workspace/queries";
import type { IssueWorkflow, IssueWorkflowStep, Project } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import {
  Popover,
  PopoverContent,
  PopoverDescription,
  PopoverTitle,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";
import { ActorAvatar } from "../common/actor-avatar";
import { matchesPinyin } from "../editor/extensions/pinyin-match";
import { StatusIcon } from "../issues/components/status-icon";
import { useStatusLabel } from "../issues/utils/status-label";
import { AppLink, useNavigation } from "../navigation";
import { ProjectIcon } from "../projects/components/project-icon";
import { IssueStatusesTab } from "../settings/components/issue-statuses-tab";
import { SettingsTab } from "../settings/components/settings-layout";
import { useT } from "../i18n";
import { useStepHandlerLabel } from "./step-handler";
import { WORKFLOW_TEMPLATES, type WorkflowTemplateId } from "./templates";
import { WorkflowEditorPage } from "./workflow-editor-page";

const WORKFLOW_PARAM = "workflow";
const DUPLICATE_PARAM = "from";
const TEMPLATE_PARAM = "template";
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
  // Any member creates workflows; a workflow's owner and admins edit it
  // (MUL-8120). Mirrors useWorkflowPermissions.
  const canCreate = !!role;
  const canEditWorkflow = (id: string) => {
    const owner = workflows.find((w) => w.id === id)?.owner_id;
    return isAdmin || (!!currentUser?.id && !!owner && owner === currentUser.id);
  };

  const open = navigation.searchParams.get(WORKFLOW_PARAM);
  const duplicateOf = navigation.searchParams.get(DUPLICATE_PARAM);
  const templateParam = navigation.searchParams.get(TEMPLATE_PARAM);
  const template = WORKFLOW_TEMPLATES.find((tpl) => tpl.id === templateParam)?.id ?? null;
  const hrefWith = (params: Record<string, string | null>) => {
    const next = new URLSearchParams(navigation.searchParams);
    for (const [key, value] of Object.entries(params)) {
      if (value === null) next.delete(key);
      else next.set(key, value);
    }
    return `${navigation.pathname}?${next.toString()}`;
  };
  const editorHref = (id: string, from: WorkflowTemplateId | null = null) =>
    hrefWith({ [WORKFLOW_PARAM]: id, [DUPLICATE_PARAM]: null, [TEMPLATE_PARAM]: from });
  const openEditor = (id: string, from: WorkflowTemplateId | null = null) => navigation.push(editorHref(id, from));
  const backToList = () =>
    navigation.push(hrefWith({ [WORKFLOW_PARAM]: null, [DUPLICATE_PARAM]: null, [TEMPLATE_PARAM]: null }));

  const usage = useMemo(() => {
    const counts = new Map<string, number>();
    for (const workflow of workflows) {
      if (workflow.is_default) continue;
      for (const step of workflow.steps) counts.set(step.status_key, (counts.get(step.status_key) ?? 0) + 1);
    }
    return (key: string) => counts.get(key) ?? 0;
  }, [workflows]);

  if (open) {
    const seed = open === NEW_WORKFLOW && duplicateOf ? workflows.find((w) => w.id === duplicateOf) ?? null : null;
    return (
      <WorkflowEditorPage
        key={`${open}:${duplicateOf ?? ""}:${template ?? ""}`}
        workflowId={open === NEW_WORKFLOW ? null : open}
        seed={seed}
        template={open === NEW_WORKFLOW && !duplicateOf ? template : null}
        canEdit={open === NEW_WORKFLOW ? canCreate : canEditWorkflow(open)}
        canDuplicate={canCreate}
        canCreateStatus={isAdmin}
        onBack={backToList}
        onOpen={(target) =>
          "id" in target
            ? navigation.replace(hrefWith({ [WORKFLOW_PARAM]: target.id, [DUPLICATE_PARAM]: null, [TEMPLATE_PARAM]: null }))
            : navigation.push(
                hrefWith({ [WORKFLOW_PARAM]: NEW_WORKFLOW, [DUPLICATE_PARAM]: target.duplicateOf, [TEMPLATE_PARAM]: null }),
              )
        }
      />
    );
  }

  const defaultProjects = projects.filter((p) => !p.workflow_id);
  const defaultRow = workflows.find((w) => w.is_default);

  return (
    <SettingsTab
      title={t(($) => $.workflows.settings.title)}
      actions={
        canCreate && (
          <DropdownMenu>
            <DropdownMenuTrigger
              render={
                <Button variant="outline" className="gap-1.5">
                  <Plus className="size-3.5" />
                  {t(($) => $.workflows.settings.new)}
                  <ChevronDown className="size-3" />
                </Button>
              }
            />
            <DropdownMenuContent align="end" className="w-72">
              <DropdownMenuItem onClick={() => openEditor(NEW_WORKFLOW)}>
                <Plus className="size-3.5" />
                {t(($) => $.workflows.settings.new_blank)}
              </DropdownMenuItem>
              <DropdownMenuSeparator />
              <DropdownMenuGroup>
                <DropdownMenuLabel>{t(($) => $.workflows.settings.from_template)}</DropdownMenuLabel>
                {WORKFLOW_TEMPLATES.map((tpl) => (
                  <DropdownMenuItem key={tpl.id} className="items-start py-2" onClick={() => openEditor(NEW_WORKFLOW, tpl.id)}>
                    <LayoutTemplate className="mt-0.5 size-3.5" />
                    <span className="min-w-0">
                      <span className="block font-medium">{t(($) => $.workflows.templates[tpl.id].name)}</span>
                      <span className="block text-caption text-muted-foreground">
                        {t(($) => $.workflows.templates[tpl.id].summary)}
                      </span>
                    </span>
                  </DropdownMenuItem>
                ))}
              </DropdownMenuGroup>
            </DropdownMenuContent>
          </DropdownMenu>
        )
      }
    >
      <div className="divide-y divide-surface-border overflow-hidden rounded-lg border border-surface-border bg-card">
        {defaultRow ? (
          <WorkflowRow workflow={defaultRow} projects={defaultProjects} href={editorHref(defaultRow.id)} />
        ) : (
          <DefaultWorkflowRow projects={defaultProjects} />
        )}
        {isLoaded &&
          workflows.filter((w) => !w.is_default).map((workflow) => (
            <WorkflowRow
              key={workflow.id}
              workflow={workflow}
              projects={projects.filter((p) => workflow.project_ids.includes(p.id))}
              href={editorHref(workflow.id)}
            />
          ))}
      </div>

      <IssueStatusesTab
        embedded={{ title: t(($) => $.workflows.settings.statuses_title) }}
        workflowUsage={usage}
      />
    </SettingsTab>
  );
}

/** The implicit workflow of every project without one: the library itself. */
function DefaultWorkflowRow({ projects }: { projects: Project[] }) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const catalog = useIssueStatuses(wsId);
  const keys = statusColumnKeys(catalog);
  return (
    <WorkflowRowLayout
      name={
        <button
          type="button"
          className={ROW_LINK_CLASS}
          onClick={() => document.getElementById("status-library")?.scrollIntoView({ behavior: "smooth", block: "start" })}
        >
          {t(($) => $.workflows.default_name)}
        </button>
      }
      description={t(($) => $.workflows.default_summary)}
      usage={<WorkflowUsage name={t(($) => $.workflows.default_name)} projects={projects} isDefault />}
      steps={keys.map((key) => ({ status_key: key, handler: { type: "none" }, instructions: "" }))}
    />
  );
}

function WorkflowRow({ workflow, projects, href }: { workflow: IssueWorkflow; projects: Project[]; href: string }) {
  return (
    <WorkflowRowLayout
      name={
        <AppLink href={href} className={ROW_LINK_CLASS}>
          {workflow.name}
        </AppLink>
      }
      description={workflow.description}
      usage={<WorkflowUsage name={workflow.name} projects={projects} isDefault={workflow.is_default} />}
      steps={workflow.steps}
    />
  );
}

/**
 * The name's `::after` stretches over the whole row, so the row clicks
 * through to the workflow while its accessible name stays the workflow name.
 * Controls on the row sit above it with `relative z-10`.
 */
const ROW_LINK_CLASS =
  "min-w-0 max-w-[40%] shrink-0 truncate text-body font-semibold outline-none after:absolute after:inset-0 focus-visible:after:outline-2 focus-visible:after:-outline-offset-2 focus-visible:after:outline-ring";

function WorkflowRowLayout({
  name,
  description,
  usage,
  steps,
}: {
  name: ReactNode;
  description?: string | null;
  usage: ReactNode;
  steps: IssueWorkflowStep[];
}) {
  return (
    <div className="relative flex items-center gap-4 px-4 py-3.5 transition-colors hover:bg-accent/40">
      <div className="min-w-0 flex-1 space-y-2.5">
        <div className="flex min-h-7 min-w-0 items-center gap-2">
          {name}
          {description && <span className="min-w-0 truncate text-caption text-muted-foreground">{description}</span>}
          {usage}
        </div>
        <StepChain steps={steps} />
      </div>
      <ChevronRight aria-hidden className="size-4 shrink-0 text-faint-foreground" />
    </div>
  );
}

const USAGE_STACK_SIZE = 3;
const USAGE_FILTER_MIN = 7;

/**
 * Which projects use a workflow, as one line: up to three project icons and a
 * count (or the name, for a single project). The full list opens on click.
 */
function WorkflowUsage({ name, projects, isDefault }: { name: string; projects: Project[]; isDefault: boolean }) {
  const { t } = useT("issues");
  const paths = useWorkspacePaths();
  const [open, setOpen] = useState(false);
  const [filter, setFilter] = useState("");
  const sorted = useMemo(
    () => [...projects].sort((a, b) => b.updated_at.localeCompare(a.updated_at)),
    [projects],
  );

  if (sorted.length === 0) {
    return (
      <span className="ml-auto shrink-0 text-label text-muted-foreground">
        {isDefault ? t(($) => $.workflows.settings.usage_default_none) : t(($) => $.workflows.settings.no_projects)}
      </span>
    );
  }

  const query = filter.trim().toLowerCase();
  const visible = query
    ? sorted.filter((p) => p.title.toLowerCase().includes(query) || matchesPinyin(p.title, query))
    : sorted;
  const summary = isDefault
    ? t(($) => $.workflows.settings.usage_default, { count: sorted.length })
    : sorted.length === 1
      ? sorted[0]!.title
      : t(($) => $.workflows.settings.projects, { count: sorted.length });

  return (
    <Popover
      open={open}
      onOpenChange={(next) => {
        setOpen(next);
        if (!next) setFilter("");
      }}
    >
      <PopoverTrigger className="relative z-10 -mr-2 ml-auto inline-flex h-7 min-w-0 max-w-[45%] shrink-0 items-center gap-2 rounded-md px-2 text-label text-muted-foreground transition-colors hover:bg-surface-selected hover:text-foreground focus-visible:outline-2 focus-visible:outline-ring data-popup-open:bg-surface-selected data-popup-open:text-foreground">
        <span aria-hidden className="flex shrink-0 -space-x-1.5">
          {sorted.slice(0, USAGE_STACK_SIZE).map((project) => (
            <span key={project.id} className="flex size-5 items-center justify-center rounded-full bg-muted ring-2 ring-card">
              <ProjectIcon project={project} size="sm" />
            </span>
          ))}
        </span>
        <span className="truncate">{summary}</span>
      </PopoverTrigger>
      <PopoverContent align="end" className="w-80 gap-0 p-0">
        <div className="flex min-w-0 items-baseline gap-1.5 px-3 pb-2 pt-3">
          <PopoverTitle className="truncate text-label font-semibold">{name}</PopoverTitle>
          <PopoverDescription className="shrink-0 text-caption">
            {t(($) => $.workflows.settings.projects, { count: sorted.length })}
          </PopoverDescription>
        </div>
        {sorted.length >= USAGE_FILTER_MIN && (
          <div className="relative mx-2 mb-1">
            <Search aria-hidden className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
            <input
              type="text"
              value={filter}
              onChange={(e) => setFilter(e.target.value)}
              placeholder={t(($) => $.workflows.settings.filter_projects)}
              aria-label={t(($) => $.workflows.settings.filter_projects)}
              className="h-8 w-full rounded-md border border-input bg-transparent pl-8 pr-2 text-label outline-none placeholder:text-muted-foreground focus-visible:border-ring"
            />
          </div>
        )}
        <div className="max-h-72 overflow-y-auto p-1">
          {visible.length === 0 ? (
            <p className="px-2 py-3 text-center text-caption text-muted-foreground">
              {t(($) => $.workflows.settings.no_matching_projects)}
            </p>
          ) : (
            visible.map((project) => (
              <AppLink
                key={project.id}
                href={paths.projectDetail(project.id)}
                onClick={() => setOpen(false)}
                className="flex h-8 items-center gap-2 rounded-md px-2 text-label transition-colors hover:bg-accent focus-visible:bg-accent focus-visible:outline-none"
              >
                <ProjectIcon project={project} size="sm" />
                <span className="min-w-0 flex-1 truncate">{project.title}</span>
                <span className="shrink-0 text-caption tabular-nums text-muted-foreground">{project.issue_count}</span>
              </AppLink>
            ))
          )}
        </div>
        {isDefault && (
          <p className="flex items-center gap-2 border-t border-surface-border px-3 py-2.5 text-caption text-muted-foreground">
            <Info aria-hidden className="size-3.5 shrink-0" />
            {t(($) => $.workflows.settings.default_covers_no_project)}
          </p>
        )}
      </PopoverContent>
    </Popover>
  );
}

/**
 * A workflow's steps as a chain of chips, each with its handler. Side states
 * (the closed category and `blocked`) are left out so the chain reads as the
 * path an issue takes.
 */
function StepChain({ steps }: { steps: IssueWorkflowStep[] }) {
  const wsId = useWorkspaceId();
  const catalog = useIssueStatuses(wsId);
  const labelOf = useStatusLabel(wsId);
  const handlerLabel = useStepHandlerLabel(null);
  const path = steps.filter((s) => s.status_key !== "blocked" && catalog.categoryOf(s.status_key) !== "closed");
  return (
    <div className="flex flex-wrap items-center gap-x-1 gap-y-1.5">
      {path.map((step, index) => {
        const { type, id } = step.handler;
        const handler = handlerLabel(step);
        const actor = (type === "agent" || type === "squad" || type === "member") && id ? { type, id } : null;
        // The arrow travels with the step it points to, so a wrapped line
        // starts with "→ step" instead of ending on an arrow beside the
        // row's chevron.
        return (
          <span key={step.status_key} className="inline-flex items-center gap-1">
            {index > 0 && <ArrowRight aria-hidden className="size-3 text-faint-foreground" />}
            <span
              className={`inline-flex h-[22px] items-center gap-1.5 rounded-full border border-surface-border bg-card pl-2 text-caption ${handler ? "pr-0.5" : "pr-2"}`}
            >
              <StatusIcon
                status={step.status_key}
                category={catalog.categoryOf(step.status_key)}
                color={catalog.colorOf(step.status_key)}
                icon={catalog.iconOf(step.status_key)}
                className="h-3 w-3"
              />
              {labelOf(step.status_key)}
              {handler && (
                <span className="inline-flex h-4 max-w-40 items-center gap-1 rounded-full bg-muted pl-px pr-1.5 text-muted-foreground [--avatar-slot-size:14px]">
                  {actor ? (
                    <ActorAvatar actorType={actor.type} actorId={actor.id} size="xs" profileLink={false} />
                  ) : (
                    <span aria-hidden className="flex size-3.5 shrink-0 items-center justify-center rounded-full border border-dashed border-faint-foreground">
                      <User className="size-2" />
                    </span>
                  )}
                  <span className="truncate">{handler}</span>
                </span>
              )}
            </span>
          </span>
        );
      })}
    </div>
  );
}
