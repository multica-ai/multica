"use client";

import { useEffect, useMemo, useState, type ReactNode } from "react";
import { useStore } from "zustand";
import { toast } from "sonner";
import {
  ArrowLeft,
  Bot,
  ChevronDown,
  CircleUser,
  Copy,
  Globe,
  Layers,
  Link2,
  ListTodo,
  Lock,
  Pencil,
  SquarePen,
  Star,
  EyeOff,
  Filter,
  ListTree,
  Trash2,
  Users,
} from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { IconButton } from "@multica/ui/components/common/icon-button";
import { Separator } from "@multica/ui/components/ui/separator";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { cn } from "@multica/ui/lib/utils";
import type { CreateIssueViewRequest, IssueView } from "@multica/core/api/schemas";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { useActiveIssueView } from "@multica/core/issue-views/use-active-view";
import type { IssueViewScope } from "@multica/core/issue-views/queries";
import {
  builtinViewItemId,
  savedViewItemId,
  type BuiltinViewKey,
} from "@multica/core/issue-views/sidebar-views";
import { issueViewTarget } from "@multica/core/issue-views/view-target";
import {
  isIssueViewModified,
  issueViewDefinitionFromState,
  issueViewSeedDefinition,
  modifiedFilterDimensions,
} from "@multica/core/issue-views/definition";
import { useIssueViewRenameStore } from "@multica/core/issue-views/rename-store";
import {
  myIssuesViewStore,
  type MyIssuesScope,
} from "@multica/core/issues/stores/my-issues-view-store";
import {
  useIssuesScope,
  useIssuesScopeStore,
  type IssuesScope,
} from "@multica/core/issues/stores/issues-scope-store";
import { actorKindForViewVariant } from "@multica/core/issues/surface/scope";
import { useViewStore, useViewStoreApi } from "@multica/core/issues/stores/view-store-context";
import {
  countAdvancedConditions,
  type NegatableFilterDimension,
} from "@multica/core/issues/stores/filter-tree";
import type { Project } from "@multica/core/types";
import { PageHeader } from "../layout/page-header";
import { RefreshablePageIcon } from "../layout/refreshable-page-icon";
import { ActionShortcutKeycaps } from "../common/shortcut-keycaps";
import { ProjectIcon } from "../projects/components/project-icon";
import { IssueDisplayControls, IssueFilterMenu } from "../issues/components/issues-header";
import { useFilterChips } from "../issues/components/filter-chips-bar";
import { WorkspaceAgentWorkingChip } from "../issues/components/workspace-agent-working-chip";
import type { IssueSurfaceController } from "../issues/surface/use-issue-surface-controller";
import { useT } from "../i18n";
import { useMyIssuesScope } from "../my-issues/use-my-issues-scope";
import { DeleteViewDialog } from "./delete-view-dialog";
import { useIssueViewActions } from "./use-view-actions";
import { ViewNameInput } from "./view-name-input";

/** The page a header sits on; a saved view opened there replaces its title. */
export type IssueViewHeaderBase =
  | { kind: "workspace" }
  | { kind: "my" }
  | { kind: "project"; project: Project };

const ASSIGNEE_TYPES: IssuesScope[] = ["all", "members", "agents"];
const PROJECT_HIDDEN_FIELDS: NegatableFilterDimension[] = ["project"];
const MY_RELATIONS: Exclude<MyIssuesScope, "agents">[] = ["assigned", "created", "all"];
const MY_SAVE_VARIANT: Record<MyIssuesScope, NonNullable<CreateIssueViewRequest["scope_variant"]>> = {
  all: "any",
  assigned: "assigned",
  created: "created",
  agents: "involved",
};

function viewScopeOf(base: IssueViewHeaderBase): IssueViewScope {
  return base.kind === "project"
    ? { scope_type: "project", scope_id: base.project.id }
    : { scope_type: base.kind };
}

/**
 * One row for every issue list page. The left side says what is being looked
 * at: the view's name and its menu, the filter, then Reset / Save once they
 * drift from what the view saves. The right side holds icon-only controls
 * whose names live in their tooltips, so the title is the row's only text.
 */
export function IssueViewHeader({
  controller,
  base,
  baseMenu,
  trailing,
}: {
  controller: IssueSurfaceController;
  base: IssueViewHeaderBase;
  /** Name-menu items for the page itself, e.g. project actions. */
  baseMenu?: ReactNode;
  /** Layout toggles at the far end, e.g. the project's properties panel. */
  trailing?: ReactNode;
}) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const viewScope = viewScopeOf(base);
  const { activeView, setActive, missing } = useActiveIssueView(wsId, viewScope);
  const actions = useIssueViewActions();
  const [deleteTarget, setDeleteTarget] = useState<IssueView | null>(null);
  const renameTarget = useIssueViewRenameStore((s) => s.target);
  const startRename = useIssueViewRenameStore((s) => s.start);
  const stopRename = useIssueViewRenameStore((s) => s.stop);

  useEffect(() => {
    if (missing) {
      setActive(null);
      toast.info(t(($) => $.views.unavailable));
    }
  }, [missing, setActive, t]);

  const scopePage = base.kind === "project" ? (`project:${base.project.id}` as const) : "issues";
  const assigneeType = useIssuesScope(scopePage);
  const setAssigneeTypeForPage = useIssuesScopeStore((s) => s.setScope);
  const setAssigneeType = (next: IssuesScope) => setAssigneeTypeForPage(scopePage, next);
  const myScope = useMyIssuesScope();
  const setMyScope = useStore(myIssuesViewStore, (s) => s.setScope);

  const storeApi = useViewStoreApi();
  const state = useViewStore((s) => s);
  const dateFilter = useViewStore((s) => s.dateFilter);
  const setDateFilter = useViewStore((s) => s.setDateFilter);
  const agentRunningFilter = useViewStore((s) => s.agentRunningFilter);
  const toggleAgentRunningFilter = useViewStore((s) => s.toggleAgentRunningFilter);

  const builtinKey: BuiltinViewKey | null =
    base.kind === "workspace" ? "all_issues" : base.kind === "my" ? (myScope === "agents" ? "my_agents" : "my_issues") : null;
  const baseName =
    base.kind === "project"
      ? base.project.title
      : t(($) => $.views.builtin[builtinKey!]);
  const baseIcon =
    base.kind === "project" ? (
      <ProjectIcon project={base.project} size="sm" />
    ) : builtinKey === "my_agents" ? (
      <Bot className="size-4" />
    ) : builtinKey === "my_issues" ? (
      <CircleUser className="size-4" />
    ) : (
      <ListTodo className="size-4" />
    );

  const modified = activeView ? isIssueViewModified(activeView, state) : null;
  const dirty = activeView
    ? modified!.filters || modified!.display
    : controller.hasActiveFilters;
  const canManage = activeView ? actions.canManage(activeView) : false;
  const starId = activeView ? savedViewItemId(activeView.id) : builtinKey ? builtinViewItemId(builtinKey) : null;
  const starred = starId ? actions.sidebar.isStarred(starId) : false;
  const renaming =
    !!activeView && renameTarget?.surface === "header" && renameTarget.viewId === activeView.id;

  const scopeVariant: CreateIssueViewRequest["scope_variant"] = activeView
    ? ((activeView.scope_variant ?? null) as CreateIssueViewRequest["scope_variant"])
    : base.kind === "my"
      ? MY_SAVE_VARIANT[myScope]
      : assigneeType === "all"
        ? null
        : assigneeType;

  const reset = () => {
    if (activeView) {
      const seed = issueViewSeedDefinition(activeView);
      storeApi.setState({ ...seed.query, ...seed.display } as Partial<typeof state>);
    } else {
      storeApi.getState().clearFilters();
    }
    setDateFilter(null);
  };
  const saveAsNew = () =>
    actions.create({
      scope: viewScope,
      scopeVariant,
      definition: issueViewDefinitionFromState(storeApi.getState()),
      renameIn: "header",
    });

  const assigneeTypeLabel = (value: IssuesScope) =>
    t(($) => $.save_view[value === "all" ? "variant_all" : value === "members" ? "variant_members" : "variant_agents"]);
  const myRelationLabel = (value: string) =>
    t(($) =>
      $.save_view[
        value === "assigned"
          ? "variant_my_assigned"
          : value === "created"
            ? "variant_my_created"
            : value === "involved"
              ? "variant_my_involved"
              : "variant_my_any"
      ],
    );

  // The surface's scope axis heads the filter menu: who the issues are
  // assigned to, or how they relate to me. A saved view fixes it, so there it
  // is a read-only row.
  let scopeSection: ReactNode = null;
  let scopeCounts = false;
  if (activeView) {
    const variant = activeView.scope_variant;
    const fixedScope =
      base.kind === "my"
        ? { label: t(($) => $.save_view.scope_label_my), value: myRelationLabel(variant ?? "any") }
        : actorKindForViewVariant(variant) !== "all"
          ? {
              label: t(($) => $.save_view.scope_label_workspace),
              value: assigneeTypeLabel(actorKindForViewVariant(variant)),
            }
          : null;
    if (fixedScope) {
      scopeCounts = base.kind !== "my";
      scopeSection = (
        <DropdownMenuItem disabled title={t(($) => $.views.set_by_view)}>
          <Users className="size-3.5" />
          <span className="flex-1">{fixedScope.label}</span>
          <span className="ml-3 text-caption font-medium text-muted-foreground">{fixedScope.value}</span>
        </DropdownMenuItem>
      );
    }
  } else if (base.kind === "my") {
    if (myScope !== "agents") {
      scopeSection = (
        <ScopeSub
          label={t(($) => $.save_view.scope_label_my)}
          value={myScope}
          options={MY_RELATIONS.map((value) => ({ value, label: myRelationLabel(value === "all" ? "any" : value) }))}
          onChange={setMyScope}
        />
      );
    }
  } else {
    scopeCounts = assigneeType !== "all";
    scopeSection = (
      <ScopeSub
        label={t(($) => $.save_view.scope_label_workspace)}
        value={assigneeType}
        options={ASSIGNEE_TYPES.map((value) => ({ value, label: assigneeTypeLabel(value) }))}
        onChange={setAssigneeType}
        active={scopeCounts}
      />
    );
  }

  // "My issues" is already a relation to me; a second one there is noise.
  const showRelation = base.kind !== "my";
  const { chips: allFilterChips } = useFilterChips(dateFilter, setDateFilter);
  const filterChips = showRelation
    ? allFilterChips
    : allFilterChips.filter((chip) => chip.key !== "relation");
  // In advanced mode the trigger names the mode and counts its conditions.
  const advancedFilter = useViewStore((s) => s.advancedFilter);
  const advancedMode = advancedFilter !== null;
  const filterCount =
    filterChips.filter((chip) => chip.key !== "advanced").length +
    countAdvancedConditions(advancedFilter) +
    (scopeCounts ? 1 : 0);
  const filterLabel = advancedMode ? t(($) => $.filters.advanced_title) : t(($) => $.views.filter);
  // The trigger is an icon, so the tooltip is where the reader learns what
  // the count is made of without opening the menu.
  const filterFields = [
    ...(scopeCounts ? [t(($) => $.save_view.scope_label_workspace)] : []),
    ...filterChips.map((chip) => chip.label),
  ];
  const filterTooltip =
    filterFields.length > 0
      ? `${filterLabel} · ${filterFields.join(t(($) => $.workflows.list_separator))}`
      : filterLabel;
  // An idle surface has nothing to report, so the chip only takes room while
  // someone is working or its filter is on.
  const showAgentsChip = agentRunningFilter || (controller.workingAgents?.length ?? 0) > 0;
  const modifiedKeys = useMemo(
    () => (activeView && modified?.filters ? modifiedFilterDimensions(activeView, state) : undefined),
    [activeView, modified?.filters, state],
  );

  const title = (
    <>
      <RefreshablePageIcon refreshing={controller.isRefreshing}>
        {activeView ? <Layers className="size-4" /> : baseIcon}
      </RefreshablePageIcon>
      <span className="min-w-0 truncate">{activeView ? activeView.name : baseName}</span>
    </>
  );

  return (
    <PageHeader className="@container/issue-header">
      <div className="flex min-w-0 flex-1 items-center gap-1 overflow-x-clip">
        {renaming && activeView ? (
          <span className="flex min-w-0 shrink-0 items-center gap-2">
            <Layers className="size-4 shrink-0 text-muted-foreground" />
            <ViewNameInput
              initial={activeView.name}
              onCommit={(name) => actions.rename(activeView, name)}
              onDone={stopRename}
              className="w-48"
            />
          </span>
        ) : (
          <DropdownMenu>
            <h1 className="flex min-w-20">
              <DropdownMenuTrigger
                render={
                  <Button
                    variant="ghost"
                    size="sm"
                    className="h-7 min-w-0 max-w-64 shrink gap-2 px-2 text-body font-medium text-foreground"
                  />
                }
              >
                {title}
                <ChevronDown className="size-3 shrink-0 text-muted-foreground" />
              </DropdownMenuTrigger>
            </h1>
            <DropdownMenuContent align="start" className="w-auto min-w-52">
              {activeView ? (
                <>
                  <DropdownMenuGroup>
                    {canManage && (
                      <DropdownMenuItem onClick={() => startRename(activeView.id, "header")}>
                        <Pencil className="size-3.5" />
                        {t(($) => $.views.rename)}
                      </DropdownMenuItem>
                    )}
                    <DropdownMenuItem onClick={() => actions.duplicate(activeView, "header")}>
                      <Copy className="size-3.5" />
                      {t(($) => $.views.duplicate)}
                    </DropdownMenuItem>
                    <DropdownMenuItem
                      onClick={() => actions.copyLink(issueViewTarget(wsId, activeView, paths).href)}
                    >
                      <Link2 className="size-3.5" />
                      {t(($) => $.views.copy_link)}
                    </DropdownMenuItem>
                  </DropdownMenuGroup>
                  {canManage && activeView.scope_type !== "my" && (
                    <>
                      <DropdownMenuSeparator />
                      <DropdownMenuGroup>
                        <DropdownMenuLabel>{t(($) => $.views.visible_to)}</DropdownMenuLabel>
                        <DropdownMenuRadioGroup
                          value={activeView.visibility === "workspace" ? "workspace" : "private"}
                          onValueChange={(value) =>
                            actions.setVisibility(activeView, value as "private" | "workspace")
                          }
                        >
                          <DropdownMenuRadioItem value="private">
                            <Lock className="size-3.5" />
                            {t(($) => $.views.only_me)}
                          </DropdownMenuRadioItem>
                          <DropdownMenuRadioItem value="workspace">
                            <Globe className="size-3.5" />
                            {t(($) => $.views.workspace_members)}
                          </DropdownMenuRadioItem>
                        </DropdownMenuRadioGroup>
                      </DropdownMenuGroup>
                    </>
                  )}
                  <DropdownMenuSeparator />
                  <DropdownMenuGroup>
                    <StarItem starred={starred} onToggle={() => starId && (starred ? actions.sidebar.unstar(starId) : actions.sidebar.star(starId))} />
                    <DropdownMenuItem onClick={() => setActive(null)}>
                      <ArrowLeft className="size-3.5" />
                      {t(($) => $.views.back_to, { name: baseName })}
                    </DropdownMenuItem>
                  </DropdownMenuGroup>
                  {canManage && (
                    <>
                      <DropdownMenuSeparator />
                      <DropdownMenuItem variant="destructive" onClick={() => setDeleteTarget(activeView)}>
                        <Trash2 className="size-3.5" />
                        {t(($) => $.views.delete)}
                      </DropdownMenuItem>
                    </>
                  )}
                </>
              ) : (
                <>
                  {baseMenu ?? (
                    <DropdownMenuItem
                      onClick={() =>
                        actions.copyLink(
                          builtinKey === "my_agents"
                            ? paths.myAgents()
                            : base.kind === "my"
                              ? paths.myIssues()
                              : paths.issues(),
                        )
                      }
                    >
                      <Link2 className="size-3.5" />
                      {t(($) => $.views.copy_link)}
                    </DropdownMenuItem>
                  )}
                  {starId && (
                    <>
                      <DropdownMenuSeparator />
                      <StarItem starred={starred} onToggle={() => (starred ? actions.sidebar.unstar(starId) : actions.sidebar.star(starId))} />
                    </>
                  )}
                </>
              )}
            </DropdownMenuContent>
          </DropdownMenu>
        )}

        <IssueFilterMenu
          align="start"
          tooltip={filterTooltip}
          trigger={
            <Button
              variant="ghost"
              size="sm"
              className={cn(
                "h-7 min-w-7 shrink-0 gap-1.5 px-1.5 text-muted-foreground",
                filterCount > 0 && "text-foreground",
              )}
              aria-label={
                filterCount > 0 && !advancedMode
                  ? t(($) => $.views.filter_count, { count: filterCount })
                  : filterLabel
              }
            >
              {advancedMode ? <ListTree className="size-4" /> : <Filter className="size-4" />}
              {filterCount > 0 && (
                <span className="rounded-sm bg-brand/10 px-1 text-caption font-medium tabular-nums text-brand">
                  {filterCount}
                </span>
              )}
            </Button>
          }
          leadingSections={scopeSection}
          showRelation={showRelation}
          hiddenFields={base.kind === "project" ? PROJECT_HIDDEN_FIELDS : undefined}
          modifiedKeys={modifiedKeys}
          summaryTitle={activeView?.name ?? baseName}
          scopedIssues={controller.surfaceIssues}
          facetCountsExact={controller.facetCountsExact}
          tableFacetCounts={controller.tableFacetCounts}
          onTableFacetChange={controller.setActiveTableFacet}
          dateFilter={dateFilter}
          onDateFilterChange={setDateFilter}
        />

        {dirty && (
          <span className="flex shrink-0 items-center gap-1">
            <Button variant="ghost" size="sm" className="h-7 px-2 text-muted-foreground" onClick={reset}>
              {t(($) => $.views.reset)}
            </Button>
            {activeView && canManage ? (
              <SaveSplitButton
                viewName={activeView.name}
                shared={activeView.visibility === "workspace"}
                onSave={() => actions.save(activeView, issueViewDefinitionFromState(storeApi.getState()))}
                onSaveAsNew={saveAsNew}
              />
            ) : (
              <Button size="sm" className="h-7 px-2.5" onClick={saveAsNew}>
                {activeView ? t(($) => $.views.save_as_new) : t(($) => $.views.save_as_view)}
              </Button>
            )}
          </span>
        )}
      </div>

      <div className="flex shrink-0 items-center gap-1">
        {showAgentsChip && (
          <WorkspaceAgentWorkingChip
            value={agentRunningFilter}
            onToggle={toggleAgentRunningFilter}
            agents={controller.workingAgents}
            compact
          />
        )}
        <IssueDisplayControls
          hideFilter
          scopedIssues={controller.surfaceIssues}
          allowGantt={controller.allowGantt}
          facetCountsExact={controller.facetCountsExact}
          tableFacetCounts={controller.tableFacetCounts}
          onTableFacetChange={controller.setActiveTableFacet}
          iconOnly
        />
        <IconButton
          label={t(($) => $.views.new_issue)}
          shortcut={<ActionShortcutKeycaps action="createIssue" />}
          tooltipSide="bottom"
          className="text-muted-foreground"
          onClick={() => controller.openCreateIssue()}
        >
          <SquarePen />
        </IconButton>
        {trailing && (
          <>
            <Separator orientation="vertical" className="ml-1 data-vertical:h-4 data-vertical:self-center" />
            {trailing}
          </>
        )}
      </div>

      <DeleteViewDialog
        view={deleteTarget}
        onOpenChange={(open) => !open && setDeleteTarget(null)}
        onConfirm={actions.remove}
      />
    </PageHeader>
  );
}

function ScopeSub<T extends string>({
  label,
  value,
  options,
  onChange,
  active = false,
}: {
  label: string;
  value: T;
  options: { value: T; label: string }[];
  onChange: (value: T) => void;
  /** Narrows the page beyond its default, so it reads like a filter. */
  active?: boolean;
}) {
  const current = options.find((option) => option.value === value)?.label ?? value;
  return (
    <DropdownMenuSub>
      <DropdownMenuSubTrigger>
        <Users className="size-3.5" />
        <span className="flex-1">{label}</span>
        <span
          className={cn(
            "ml-3 text-caption font-medium",
            active ? "text-primary" : "text-muted-foreground",
          )}
        >
          {current}
        </span>
      </DropdownMenuSubTrigger>
      <DropdownMenuSubContent className="w-auto min-w-44">
        <DropdownMenuRadioGroup value={value} onValueChange={(next) => onChange(next as T)}>
          {options.map((option) => (
            <DropdownMenuRadioItem key={option.value} value={option.value}>
              {option.label}
            </DropdownMenuRadioItem>
          ))}
        </DropdownMenuRadioGroup>
      </DropdownMenuSubContent>
    </DropdownMenuSub>
  );
}

function StarItem({ starred, onToggle }: { starred: boolean; onToggle: () => void }) {
  const { t } = useT("issues");
  return (
    <DropdownMenuItem onClick={onToggle}>
      {starred ? <EyeOff className="size-3.5" /> : <Star className="size-3.5" />}
      {starred ? t(($) => $.views.remove_from_sidebar) : t(($) => $.views.add_to_sidebar)}
    </DropdownMenuItem>
  );
}

function SaveSplitButton({
  viewName,
  shared,
  onSave,
  onSaveAsNew,
}: {
  viewName: string;
  shared: boolean;
  onSave: () => void;
  onSaveAsNew: () => void;
}) {
  const { t } = useT("issues");
  return (
    <span className="flex items-center">
      <Button size="sm" className="h-7 rounded-r-none px-2.5" onClick={onSave}>
        {t(($) => $.views.save)}
      </Button>
      <DropdownMenu>
        <DropdownMenuTrigger
          render={
            <Button
              size="sm"
              className="h-7 w-6 rounded-l-none border-l border-primary-foreground/20 px-0"
              aria-label={t(($) => $.views.save_options)}
            />
          }
        >
          <ChevronDown className="size-3" />
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" className="w-72">
          <DropdownMenuItem onClick={onSave} className="items-start">
            <span className="flex flex-col gap-0.5">
              <span>{t(($) => $.views.save_to, { name: viewName })}</span>
              <span className={cn("text-caption text-muted-foreground")}>
                {shared ? t(($) => $.views.save_to_hint_shared) : t(($) => $.views.save_to_hint_private)}
              </span>
            </span>
          </DropdownMenuItem>
          <DropdownMenuItem onClick={onSaveAsNew} className="items-start">
            <span className="flex flex-col gap-0.5">
              <span>{t(($) => $.views.save_as_new)}</span>
              <span className="text-caption text-muted-foreground">
                {t(($) => $.views.save_as_new_hint, { name: viewName })}
              </span>
            </span>
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </span>
  );
}
