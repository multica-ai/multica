"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { createStore, type StoreApi } from "zustand/vanilla";
import { useQuery } from "@tanstack/react-query";
import { ArrowDown, ArrowUp, ChevronRight, Plus } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@multica/ui/components/ui/collapsible";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { Toggle } from "@multica/ui/components/ui/toggle";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import { useCreateIssueView, useUpdateIssueView } from "@multica/core/issue-views/mutations";
import {
  issueViewContainerKey,
  useActiveIssueViewStore,
} from "@multica/core/issue-views/active-view-store";
import { ApiError } from "@multica/core/api/client";
import type { CreateIssueViewRequest, IssueView } from "@multica/core/api/schemas";
import { projectListOptions } from "@multica/core/projects/queries";
import { propertyListOptions } from "@multica/core/properties";
import {
  viewStoreSlice,
  viewStorePersistOptions,
  mergeViewStatePersisted,
  GROUPING_OPTIONS,
  SORT_OPTIONS,
  SWIMLANE_GROUPINGS,
  CARD_PROPERTY_OPTIONS,
  type IssueGrouping,
  type IssueViewState,
  type SortField,
  type SwimlaneGrouping,
  type ViewMode,
} from "@multica/core/issues/stores/view-store";
import {
  ViewStoreProvider,
  useViewStore,
  useViewStoreApi,
} from "@multica/core/issues/stores/view-store-context";
import { IssueFilterMenu } from "./issues-header";
import { FilterChipList } from "./filter-chips-bar";
import { useT } from "../../i18n";

/** Where the view being saved will live. Decided by the hosting surface, not
 *  by the user — the dialog only explains it. */
export type SaveViewScope =
  | { kind: "workspace"; actorKind?: "all" | "members" | "agents" }
  | { kind: "my"; variant: "any" | "assigned" | "created" | "involved" }
  | {
      kind: "project";
      projectId: string;
      actorKind?: "all" | "members" | "agents";
    };

const WORKSPACE_VARIANTS = ["all", "members", "agents"] as const;
const MY_VARIANTS = ["any", "assigned", "created", "involved"] as const;
type ScopeVariantValue =
  | (typeof WORKSPACE_VARIANTS)[number]
  | (typeof MY_VARIANTS)[number];

const VARIANT_LABEL_KEY = {
  all: "variant_all",
  members: "variant_members",
  agents: "variant_agents",
  any: "variant_my_any",
  assigned: "variant_my_assigned",
  created: "variant_my_created",
  involved: "variant_my_involved",
} as const;

const MY_VARIANT_HINT_KEY = {
  any: "hint_my_any",
  assigned: "hint_my_assigned",
  created: "hint_my_created",
  involved: "hint_my_involved",
} as const;

const LAYOUT_LABEL_KEY = {
  list: "list",
  board: "board",
  table: "table",
  swimlane: "swimlane",
  gantt: "gantt",
} as const;

const GROUPING_LABEL_KEY = {
  status: "group_status",
  assignee: "group_assignee",
} as const;

const SWIMLANE_LABEL_KEY = {
  parent: "group_parent",
  project: "group_project",
  assignee: "group_assignee",
} as const;

const SORT_LABEL_KEY = {
  position: "sort_manual",
  status: "sort_status",
  priority: "sort_priority",
  start_date: "sort_start_date",
  due_date: "sort_due_date",
  created_at: "sort_created",
  updated_at: "sort_updated",
  title: "sort_title",
} as const;

const CARD_PROPERTY_LABEL_KEY = {
  priority: "card_priority",
  description: "card_description",
  assignee: "card_assignee",
  startDate: "card_start_date",
  dueDate: "card_due_date",
  project: "card_project",
  labels: "card_labels",
  childProgress: "card_child_progress",
} as const;

/** Data-only snapshot: `partialize` strips the store's bound actions (which
 *  would otherwise write back to the LIVE store) and the deliberately
 *  unsaved fields (date filter, agents-working toggle). */
const snapshotState = viewStorePersistOptions("save-view-draft").partialize;

const ROW_LABEL = "w-16 shrink-0 text-caption text-muted-foreground";

/** Filter row + layout row + collapsible display defaults, all bound to the
 *  DRAFT store via the surrounding provider. */
export function DraftDefinitionFields() {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const viewMode = useViewStore((s) => s.viewMode);
  const grouping = useViewStore((s) => s.grouping);
  const swimlaneGrouping = useViewStore((s) => s.swimlaneGrouping);
  const sortBy = useViewStore((s) => s.sortBy);
  const sortDirection = useViewStore((s) => s.sortDirection);
  const cardProperties = useViewStore((s) => s.cardProperties);
  const cardPropertyIds = useViewStore((s) => s.cardPropertyIds);
  const act = useViewStoreApi().getState();

  const { data: workspaceProperties = [] } = useQuery(propertyListOptions(wsId));
  const groupableProperties = useMemo(
    () => workspaceProperties.filter((p) => p.type === "select"),
    [workspaceProperties],
  );
  const sortableProperties = useMemo(
    () => workspaceProperties.filter((p) => p.type === "number" || p.type === "date"),
    [workspaceProperties],
  );
  const propertyName = (key: string) =>
    workspaceProperties.find((p) => `property:${p.id}` === key)?.name ??
    t(($) => $.save_view.custom_property);

  const layoutLabel = t(($) => $.view[LAYOUT_LABEL_KEY[viewMode]]);
  const groupingLabel =
    viewMode === "board"
      ? grouping in GROUPING_LABEL_KEY
        ? t(($) => $.display[GROUPING_LABEL_KEY[grouping as keyof typeof GROUPING_LABEL_KEY]])
        : propertyName(grouping)
      : viewMode === "swimlane"
        ? t(($) => $.display[SWIMLANE_LABEL_KEY[swimlaneGrouping]])
        : null;
  const sortLabel =
    sortBy in SORT_LABEL_KEY
      ? t(($) => $.display[SORT_LABEL_KEY[sortBy as keyof typeof SORT_LABEL_KEY]])
      : propertyName(sortBy);
  const sortDirectionLabel =
    sortBy === "position"
      ? null
      : sortDirection === "asc"
        ? t(($) => $.display.ascending_title)
        : t(($) => $.display.descending_title);
  const displaySummary = [
    layoutLabel,
    groupingLabel,
    sortLabel,
    sortDirectionLabel,
  ]
    .filter(Boolean)
    .join(" · ");

  return (
    <>
      {/* Filters: bare chips + the full filter menu behind a ghost chip. */}
      <div className="flex items-start gap-3">
        <Label className={`${ROW_LABEL} pt-1`}>
          {t(($) => $.save_view.filters_label)}
        </Label>
        <div className="min-w-0 flex-1">
          <FilterChipList
            trailing={
              <IssueFilterMenu
                freezeAnchor
                trigger={
                  <button
                    type="button"
                    className="flex h-6 items-center gap-1 rounded-md border border-dashed border-border px-2 text-caption text-muted-foreground hover:border-foreground/30 hover:bg-muted/60 hover:text-foreground"
                  >
                    <Plus className="size-3" />
                    {t(($) => $.save_view.add_filter)}
                  </button>
                }
              />
            }
          />
        </div>
      </div>

      {/* Display defaults: captured already; expandable for fine-tuning. */}
      <Collapsible>
        <CollapsibleTrigger className="group flex w-fit max-w-full items-center gap-1.5 rounded-md -mx-1 px-1 py-1 text-left hover:bg-muted/60">
          <ChevronRight className="size-3.5 shrink-0 text-muted-foreground transition-transform group-data-[panel-open]:rotate-90" />
          <span className="text-caption font-medium">
            {t(($) => $.save_view.display_defaults)}
          </span>
          <span className="min-w-0 truncate text-caption text-muted-foreground">
            {displaySummary}
          </span>
        </CollapsibleTrigger>
        <CollapsibleContent className="h-(--collapsible-panel-height) overflow-hidden transition-[height] duration-200 ease-out data-starting-style:h-0 data-ending-style:h-0">
          <div className="space-y-3 pt-2 pl-5">
            <div className="flex items-center gap-3">
              <Label className={ROW_LABEL}>{t(($) => $.save_view.layout_label)}</Label>
              <Select
                items={(["list", "board", "table", "swimlane"] as const).map((mode) => ({
                  value: mode as string,
                  label: t(($) => $.view[LAYOUT_LABEL_KEY[mode]]),
                }))}
                value={viewMode}
                onValueChange={(v) => {
                  if (v) act.setViewMode(v as ViewMode);
                }}
              >
                <SelectTrigger size="sm" className="w-64" aria-label={t(($) => $.save_view.layout_label)}>
                  <SelectValue>{layoutLabel}</SelectValue>
                </SelectTrigger>
                <SelectContent align="start">
                  <SelectGroup>
                    {(["list", "board", "table", "swimlane"] as const).map((mode) => (
                      <SelectItem key={mode} value={mode}>
                        {t(($) => $.view[LAYOUT_LABEL_KEY[mode]])}
                      </SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
            </div>
            {viewMode === "board" && (
              <div className="flex items-center gap-3">
                <Label className={ROW_LABEL}>
                  {t(($) => $.display.grouping_section)}
                </Label>
                <Select
                  items={[
                    ...GROUPING_OPTIONS.map((opt) => ({
                      value: opt.value as string,
                      label: t(($) => $.display[GROUPING_LABEL_KEY[opt.value]]),
                    })),
                    ...groupableProperties.map((p) => ({
                      value: `property:${p.id}`,
                      label: p.name,
                    })),
                  ]}
                  value={grouping}
                  onValueChange={(v) => {
                    if (v) act.setGrouping(v as IssueGrouping);
                  }}
                >
                  <SelectTrigger size="sm" className="w-64" aria-label={t(($) => $.display.grouping_section)}>
                    <SelectValue>
                      {grouping in GROUPING_LABEL_KEY
                        ? t(($) => $.display[GROUPING_LABEL_KEY[grouping as keyof typeof GROUPING_LABEL_KEY]])
                        : propertyName(grouping)}
                    </SelectValue>
                  </SelectTrigger>
                  <SelectContent align="start">
                    <SelectGroup>
                      {GROUPING_OPTIONS.map((opt) => (
                        <SelectItem key={opt.value} value={opt.value}>
                          {t(($) => $.display[GROUPING_LABEL_KEY[opt.value]])}
                        </SelectItem>
                      ))}
                      {groupableProperties.map((property) => (
                        <SelectItem key={property.id} value={`property:${property.id}`}>
                          {property.name}
                        </SelectItem>
                      ))}
                    </SelectGroup>
                  </SelectContent>
                </Select>
              </div>
            )}
            {viewMode === "swimlane" && (
              <div className="flex items-center gap-3">
                <Label className={ROW_LABEL}>
                  {t(($) => $.display.grouping_section)}
                </Label>
                <Select
                  items={SWIMLANE_GROUPINGS.map((value) => ({
                    value: value as string,
                    label: t(($) => $.display[SWIMLANE_LABEL_KEY[value]]),
                  }))}
                  value={swimlaneGrouping}
                  onValueChange={(v) => {
                    if (v) act.setSwimlaneGrouping(v as SwimlaneGrouping);
                  }}
                >
                  <SelectTrigger size="sm" className="w-64" aria-label={t(($) => $.display.grouping_section)}>
                    <SelectValue>
                      {t(($) => $.display[SWIMLANE_LABEL_KEY[swimlaneGrouping]])}
                    </SelectValue>
                  </SelectTrigger>
                  <SelectContent align="start">
                    <SelectGroup>
                      {SWIMLANE_GROUPINGS.map((value) => (
                        <SelectItem key={value} value={value}>
                          {t(($) => $.display[SWIMLANE_LABEL_KEY[value]])}
                        </SelectItem>
                      ))}
                    </SelectGroup>
                  </SelectContent>
                </Select>
              </div>
            )}
            <div className="flex items-center gap-3">
              <Label className={ROW_LABEL}>
                {t(($) => $.display.ordering_section)}
              </Label>
              <div className="flex items-center gap-1.5">
                <Select
                  items={[
                    ...SORT_OPTIONS.map((opt) => ({
                      value: opt.value as string,
                      label: t(($) => $.display[SORT_LABEL_KEY[opt.value as keyof typeof SORT_LABEL_KEY]]),
                    })),
                    ...sortableProperties.map((p) => ({
                      value: `property:${p.id}`,
                      label: p.name,
                    })),
                  ]}
                  value={sortBy}
                  onValueChange={(v) => {
                    if (v) act.setSortBy(v as SortField);
                  }}
                >
                  <SelectTrigger size="sm" className="w-64" aria-label={t(($) => $.display.ordering_section)}>
                    <SelectValue>{sortLabel}</SelectValue>
                  </SelectTrigger>
                  <SelectContent align="start">
                    <SelectGroup>
                      {SORT_OPTIONS.map((opt) => (
                        <SelectItem key={opt.value} value={opt.value}>
                          {t(($) => $.display[SORT_LABEL_KEY[opt.value as keyof typeof SORT_LABEL_KEY]])}
                        </SelectItem>
                      ))}
                      {sortableProperties.map((property) => (
                        <SelectItem key={property.id} value={`property:${property.id}`}>
                          {property.name}
                        </SelectItem>
                      ))}
                    </SelectGroup>
                  </SelectContent>
                </Select>
                {sortBy !== "position" && (
                  <Button
                    type="button"
                    variant="outline"
                    size="icon-sm"
                    onClick={() =>
                      act.setSortDirection(
                        sortDirection === "asc" ? "desc" : "asc",
                      )
                    }
                    aria-label={sortDirectionLabel ?? undefined}
                    title={sortDirectionLabel ?? undefined}
                  >
                    {sortDirection === "asc" ? (
                      <ArrowUp className="size-3.5" />
                    ) : (
                      <ArrowDown className="size-3.5" />
                    )}
                  </Button>
                )}
              </div>
            </div>
            {viewMode !== "table" && (
              <div className="flex items-start gap-3">
                <Label className={`${ROW_LABEL} pt-1`}>
                  {t(($) => $.display.card_properties_section)}
                </Label>
                <div className="flex min-w-0 flex-1 flex-wrap gap-1">
                  {CARD_PROPERTY_OPTIONS.map((opt) => (
                    <Toggle
                      key={opt.key}
                      size="sm"
                      variant="outline"
                      pressed={cardProperties[opt.key]}
                      onPressedChange={() => act.toggleCardProperty(opt.key)}
                      className="h-6 rounded-md px-2 text-caption text-muted-foreground hover:bg-muted/60 aria-pressed:border-transparent aria-pressed:bg-accent aria-pressed:text-accent-foreground aria-pressed:hover:bg-accent/80"
                    >
                      {t(($) => $.display[CARD_PROPERTY_LABEL_KEY[opt.key]])}
                    </Toggle>
                  ))}
                  {workspaceProperties.map((property) => (
                    <Toggle
                      key={property.id}
                      size="sm"
                      variant="outline"
                      pressed={cardPropertyIds.includes(property.id)}
                      onPressedChange={() => act.toggleCardPropertyId(property.id)}
                      className="h-6 max-w-40 rounded-md px-2 text-caption text-muted-foreground hover:bg-muted/60 aria-pressed:border-transparent aria-pressed:bg-accent aria-pressed:text-accent-foreground aria-pressed:hover:bg-accent/80"
                    >
                      <span className="truncate">{property.name}</span>
                    </Toggle>
                  ))}
                </div>
              </div>
            )}
            <p className="text-caption text-faint-foreground">
              {t(($) => $.save_view.default_hint)}
            </p>
          </div>
        </CollapsibleContent>
      </Collapsible>
    </>
  );
}

/**
 * Save the current surface state as a view. Uncontrolled: opening the dialog
 * snapshots the live panel state into a throwaway store, the embedded fields
 * edit THAT copy, and the page underneath never notices. Save reads the
 * copy; closing discards it. Prints until the API exists.
 */
export function SaveViewDialog({
  open,
  onOpenChange,
  scope,
  editView = null,
  seedFromDefinition = false,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  scope: SaveViewScope;
  /** Edit mode: same dialog, prefilled from this view; confirm PATCHes it
   *  (with optimistic-concurrency 409 handling) instead of creating. */
  editView?: IssueView | null;
  /** Manager-initiated edits seed from the view's own definition (the user
   *  may not have it open); in-view edits keep seeding from the live panel
   *  so the user's current additions are part of the draft. */
  seedFromDefinition?: boolean;
}) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const liveStore = useViewStoreApi();
  const createView = useCreateIssueView(wsId);
  const setActiveInStore = useActiveIssueViewStore((s) => s.setActive);
  const updateView = useUpdateIssueView(wsId);

  const [name, setName] = useState("");
  const [nameError, setNameError] = useState(false);
  const nameInputRef = useRef<HTMLInputElement>(null);
  const [visibility, setVisibility] = useState<"private" | "workspace">("private");
  // Scope-layer variant (the "which tab" axis). Defaults to the tab the
  // user stood on when saving; switchable here because the dialog IS the
  // view-definition editor. scope_type itself never switches.
  const [variant, setVariant] = useState<ScopeVariantValue>("all");
  const [draftStore, setDraftStore] = useState<StoreApi<IssueViewState> | null>(null);

  // Fresh draft per open: seed a private store instance from the live
  // panel's data fields, then let the embedded controls mutate it freely.
  useEffect(() => {
    if (!open) {
      setDraftStore(null);
      return;
    }
    const store = createStore<IssueViewState>()(viewStoreSlice);
    if (seedFromDefinition && editView) {
      // Same sanitizer the surface seed uses — the definition is a server
      // blob, never trusted wholesale.
      const defaults = viewStoreSlice(store.setState);
      store.setState(
        mergeViewStatePersisted({ ...editView.query, ...editView.display }, defaults),
        true,
      );
    } else {
      store.setState(snapshotState(liveStore.getState()));
    }
    setDraftStore(store);
    setName(editView?.name ?? "");
    setNameError(false);
    setVisibility(editView?.visibility === "workspace" ? "workspace" : "private");
    if (scope.kind === "workspace" || scope.kind === "project") {
      const fromEdit = editView?.scope_variant;
      setVariant(
        fromEdit === "members" || fromEdit === "agents"
          ? fromEdit
          : editView
            ? "all"
            : (scope.actorKind ?? "all"),
      );
    } else if (scope.kind === "my") {
      const fromEdit = editView?.scope_variant;
      setVariant(
        fromEdit && (MY_VARIANTS as readonly string[]).includes(fromEdit)
          ? (fromEdit as ScopeVariantValue)
          : scope.variant,
      );
    }
  }, [open, liveStore, editView, seedFromDefinition, scope]);

  const { data: projects = [] } = useQuery({
    ...projectListOptions(wsId),
    enabled: open && scope.kind === "project",
  });
  const projectTitle =
    scope.kind === "project"
      ? projects.find((p) => p.id === scope.projectId)?.title ?? ""
      : "";

  const scopeHint =
    scope.kind === "workspace"
      ? variant === "members"
        ? t(($) => $.save_view.hint_workspace_members)
        : variant === "agents"
          ? t(($) => $.save_view.hint_workspace_agents)
          : t(($) => $.save_view.hint_workspace)
      : scope.kind === "my"
        ? t(($) => $.save_view[
            MY_VARIANT_HINT_KEY[
              (MY_VARIANTS as readonly string[]).includes(variant)
                ? (variant as (typeof MY_VARIANTS)[number])
                : scope.variant
            ]
          ])
        : variant === "members"
          ? t(($) => $.save_view.hint_project_members, { title: projectTitle })
          : variant === "agents"
            ? t(($) => $.save_view.hint_project_agents, { title: projectTitle })
            : t(($) => $.save_view.hint_project, { title: projectTitle });

  const create = () => {
    if (!draftStore) return;
    if (!name.trim()) {
      setNameError(true);
      nameInputRef.current?.focus();
      return;
    }
    const state = draftStore.getState();
    const payload = {
      name: name.trim(),
      visibility: scope.kind === "my" ? ("private" as const) : visibility,
      scope_type: scope.kind,
      scope_id: scope.kind === "project" ? scope.projectId : null,
      scope_variant:
        scope.kind === "my"
          ? ((MY_VARIANTS as readonly string[]).includes(variant)
              ? (variant as CreateIssueViewRequest["scope_variant"])
              : scope.variant)
          : variant === "members" || variant === "agents"
            ? variant
            : null,
      definition_version: 1,
      query: {
        statusFilters: state.statusFilters,
        priorityFilters: state.priorityFilters,
        assigneeFilters: state.assigneeFilters,
        includeNoAssignee: state.includeNoAssignee,
        creatorFilters: state.creatorFilters,
        projectFilters: state.projectFilters,
        includeNoProject: state.includeNoProject,
        labelFilters: state.labelFilters,
        propertyFilters: state.propertyFilters,
      },
      display: {
        // Personal display preference (like layout): saved as the view's
        // first-open default, freely adjustable per user afterwards.
        showSubIssues: state.showSubIssues,
        viewMode: state.viewMode,
        grouping: state.grouping,
        sortBy: state.sortBy,
        sortDirection: state.sortDirection,
        cardProperties: state.cardProperties,
        cardPropertyIds: state.cardPropertyIds,
        tableColumns: state.tableColumns,
        tableGrouping: state.tableGrouping,
        tableHierarchy: state.tableHierarchy,
        tableCalculation: state.tableCalculation,
        swimlaneGrouping: state.swimlaneGrouping,
        ganttZoom: state.ganttZoom,
        ganttShowCompleted: state.ganttShowCompleted,
      },
    };
    if (editView) {
      updateView.mutate(
        {
          id: editView.id,
          name: payload.name,
          visibility: payload.visibility,
          scope_variant: payload.scope_variant ?? null,
          query: payload.query,
          display: payload.display,
          expected_revision: editView.revision,
        },
        {
          onSuccess: () => {
            toast.success(t(($) => $.save_view.toast_updated));
            onOpenChange(false);
          },
          onError: (err) => {
            if (err instanceof ApiError && err.status === 409) {
              toast.error(t(($) => $.save_view.toast_conflict));
              return;
            }
            toast.error(
              err instanceof Error ? err.message : t(($) => $.save_view.toast_failed),
            );
          },
        },
      );
      return;
    }
    createView.mutate(payload, {
      onSuccess: (created) => {
        toast.success(t(($) => $.save_view.toast_created));
        // The view you just saved is the one you want to be looking at:
        // activate it on its surface right away. `created` is null only
        // when the response failed to parse — the list refetch still shows
        // the view, it just doesn't auto-open.
        if (created) {
          setActiveInStore(
            issueViewContainerKey(wsId, {
              scope_type: payload.scope_type,
              scope_id: payload.scope_id,
            }),
            created.id,
          );
        }
        onOpenChange(false);
      },
      onError: (err) => {
        toast.error(
          err instanceof Error ? err.message : t(($) => $.save_view.toast_failed),
        );
      },
    });
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>
            {editView
              ? t(($) => $.save_view.edit_title)
              : t(($) => $.save_view.title)}
          </DialogTitle>
          <DialogDescription className="break-words">{scopeHint}</DialogDescription>
        </DialogHeader>

        <div className="space-y-4">
          <div className="flex items-start gap-3">
            <Label className={`${ROW_LABEL} pt-2`} htmlFor="save-view-name">
              {t(($) => $.save_view.name_label)}
            </Label>
            <div className="min-w-0">
              <Input
                id="save-view-name"
                ref={nameInputRef}
                autoFocus
                required
                aria-invalid={nameError || undefined}
                aria-describedby={nameError ? "save-view-name-error" : undefined}
                value={name}
                onChange={(e) => {
                  setName(e.target.value);
                  if (nameError && e.target.value.trim()) setNameError(false);
                }}
                onKeyDown={(e) => {
                  if (e.key === "Enter") create();
                }}
                maxLength={80}
                placeholder={t(($) => $.save_view.name_placeholder)}
                className="h-8 w-64"
              />
              {nameError && (
                <p
                  id="save-view-name-error"
                  className="mt-1 text-caption text-destructive animate-in fade-in-0 slide-in-from-top-1 duration-150"
                >
                  {t(($) => $.save_view.name_required)}
                </p>
              )}
            </div>
          </div>

          {scope.kind !== "my" && (
            <div className="flex items-center gap-3">
              <Label className={ROW_LABEL}>
                {t(($) => $.save_view.visibility_label)}
              </Label>
              <Select
                items={[
                  { value: "private", label: t(($) => $.save_view.visibility_private) },
                  { value: "workspace", label: t(($) => $.save_view.visibility_workspace) },
                ]}
                value={visibility}
                onValueChange={(v) => {
                  if (v) setVisibility(v as "private" | "workspace");
                }}
              >
                <SelectTrigger size="sm" className="w-64" aria-label={t(($) => $.save_view.visibility_label)}>
                  <SelectValue>
                    {visibility === "private"
                      ? t(($) => $.save_view.visibility_private)
                      : t(($) => $.save_view.visibility_workspace)}
                  </SelectValue>
                </SelectTrigger>
                <SelectContent align="start">
                  <SelectGroup>
                    <SelectItem value="private">
                      {t(($) => $.save_view.visibility_private)}
                    </SelectItem>
                    <SelectItem value="workspace">
                      {t(($) => $.save_view.visibility_workspace)}
                    </SelectItem>
                  </SelectGroup>
                </SelectContent>
              </Select>
            </div>
          )}
          {scope.kind === "my" && (
            <p className="pl-19 text-caption text-muted-foreground">
              {t(($) => $.save_view.my_private_note)}
            </p>
          )}

          <div className="flex items-center gap-3">
              <Label className={ROW_LABEL}>
                {scope.kind === "my"
                  ? t(($) => $.save_view.scope_label_my)
                  : t(($) => $.save_view.scope_label_workspace)}
              </Label>
              <Select
                items={(scope.kind === "my"
                  ? MY_VARIANTS
                  : WORKSPACE_VARIANTS
                ).map((v) => ({
                  value: v as string,
                  label: t(($) => $.save_view[VARIANT_LABEL_KEY[v]]),
                }))}
                value={variant}
                onValueChange={(v) => {
                  if (v) setVariant(v as ScopeVariantValue);
                }}
              >
                <SelectTrigger size="sm" className="w-64" aria-label={
                    scope.kind === "my"
                      ? t(($) => $.save_view.scope_label_my)
                      : t(($) => $.save_view.scope_label_workspace)
                  }>
                  <SelectValue>
                    {t(($) => $.save_view[VARIANT_LABEL_KEY[variant]])}
                  </SelectValue>
                </SelectTrigger>
                <SelectContent align="start">
                  <SelectGroup>
                    {(scope.kind === "my" ? MY_VARIANTS : WORKSPACE_VARIANTS).map((v) => (
                      <SelectItem key={v} value={v}>
                        {t(($) => $.save_view[VARIANT_LABEL_KEY[v]])}
                      </SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
            </div>

          {draftStore && (
            <ViewStoreProvider store={draftStore}>
              <DraftDefinitionFields />
            </ViewStoreProvider>
          )}
        </div>

        <DialogFooter>
          <Button variant="ghost" size="sm" onClick={() => onOpenChange(false)}>
            {t(($) => $.save_view.cancel)}
          </Button>
          <Button
            size="sm"
            onClick={create}
            disabled={createView.isPending || updateView.isPending}
          >
            {editView
              ? t(($) => $.save_view.update)
              : t(($) => $.save_view.create)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
