"use client";

import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import {
  DndContext,
  KeyboardSensor,
  PointerSensor,
  closestCenter,
  useSensor,
  useSensors,
  type DragEndEvent,
} from "@dnd-kit/core";
import {
  SortableContext,
  horizontalListSortingStrategy,
  sortableKeyboardCoordinates,
  useSortable,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import {
  getCoreRowModel,
  useReactTable,
  type ColumnDef,
  type ColumnSizingState,
  type OnChangeFn,
} from "@tanstack/react-table";
import {
  ArrowDown,
  ArrowUp,
  ChevronDown,
  ChevronRight,
  Download,
  EyeOff,
  GripVertical,
  ListTree,
  Loader2,
  Plus,
  Sigma,
  TableProperties,
} from "lucide-react";
import { toast } from "sonner";
import { DataTable } from "@multica/ui/components/ui/data-table";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import {
  TableCell,
  TableFooter,
  TableRow,
} from "@multica/ui/components/ui/table";
import { getCellStyle } from "@multica/ui/lib/data-table";
import { cn } from "@multica/ui/lib/utils";
import { useWorkspaceId } from "@multica/core/hooks";
import { useCreateIssue } from "@multica/core/issues/mutations";
import {
  TABLE_SYSTEM_COLUMNS,
  propertyIdFromViewKey,
  type SortField,
  type TableCalculation,
  type TableColumnConfig,
  type TableColumnKey,
  type TableGrouping,
  type TableSystemColumnKey,
} from "@multica/core/issues/stores/view-store";
import { useViewStore } from "@multica/core/issues/stores/view-store-context";
import { propertyListOptions } from "@multica/core/properties";
import { useWorkspacePaths } from "@multica/core/paths";
import { useActorName } from "@multica/core/workspace/hooks";
import type {
  CreateIssueRequest,
  Issue,
  IssueProperty,
  IssuePropertyValue,
  Project,
  UpdateIssueRequest,
} from "@multica/core/types";
import { useQuery } from "@tanstack/react-query";
import { ActorAvatar } from "../../common/actor-avatar";
import { LabelChip } from "../../labels/label-chip";
import { useNavigation } from "../../navigation";
import { ProjectPicker } from "../../projects/components/project-picker";
import { useT } from "../../i18n";
import { useIssueSurfaceActionsOptional } from "../surface/actions-context";
import { useIssueSurfaceSelection } from "../surface/selection-context";
import { ProgressRing } from "./progress-ring";
import {
  AssigneePicker,
  DueDatePicker,
  LabelPicker,
  PriorityPicker,
  StartDatePicker,
  StatusPicker,
} from "./pickers";
import { CustomPropertyValueEditor } from "./pickers/custom-property-picker";
import {
  buildIssueTableCsv,
  buildIssueTableRows,
  calculateIssueTableColumn,
  type IssueTableDisplayRow,
} from "./table-view-model";
import type { ChildProgress } from "./list-row";
import type { IssueCreateDefaults } from "../surface/types";

const SELECT_COLUMN_ID = "__select";
const ADD_COLUMN_ID = "__add";

type TableViewProps = {
  issues: Issue[];
  childProgressMap: Map<string, ChildProgress>;
  projectMap: Map<string, Project>;
  createDefaults: IssueCreateDefaults;
  fetchNextPage: () => Promise<unknown>;
  hasNextPage: boolean;
  isFetchingNextPage: boolean;
  total: number;
  exportIssues: () => Promise<Issue[]>;
};

type ColumnLabelKey =
  | "title"
  | "identifier"
  | "status"
  | "priority"
  | "assignee"
  | "labels"
  | "project"
  | "start_date"
  | "due_date"
  | "created_at"
  | "updated_at"
  | "child_progress"
  | "creator";

const SORTABLE_COLUMNS: Partial<Record<TableSystemColumnKey, SortField>> = {
  title: "title",
  status: "status",
  priority: "priority",
  start_date: "start_date",
  due_date: "due_date",
  created_at: "created_at",
  updated_at: "updated_at",
};

function stopRowNavigation(event: React.SyntheticEvent) {
  event.stopPropagation();
}

function SelectAllCheckbox({
  issueIds,
  label,
}: {
  issueIds: string[];
  label: string;
}) {
  const selection = useIssueSurfaceSelection();
  const ref = useRef<HTMLInputElement>(null);
  const selectedCount = issueIds.filter((id) => selection.selectedIds.has(id)).length;
  const checked = issueIds.length > 0 && selectedCount === issueIds.length;

  useEffect(() => {
    if (ref.current) {
      ref.current.indeterminate = selectedCount > 0 && !checked;
    }
  }, [checked, selectedCount]);

  return (
    <input
      ref={ref}
      type="checkbox"
      aria-label={label}
      checked={checked}
      onChange={() =>
        checked ? selection.deselect(issueIds) : selection.select(issueIds)
      }
      className="size-3.5 cursor-pointer accent-primary"
    />
  );
}

function IssueCheckbox({ issueId, label }: { issueId: string; label: string }) {
  const selection = useIssueSurfaceSelection();
  return (
    <input
      type="checkbox"
      aria-label={label}
      checked={selection.selectedIds.has(issueId)}
      onClick={stopRowNavigation}
      onChange={() => selection.toggle(issueId)}
      className="size-3.5 cursor-pointer accent-primary"
    />
  );
}

function SortableColumnHeader({
  columnKey,
  label,
  sortField,
  sortBy,
  sortDirection,
  onSort,
  onHide,
  ascendingLabel,
  descendingLabel,
  hideLabel,
  reorderLabel,
}: {
  columnKey: TableColumnKey;
  label: string;
  sortField?: SortField;
  sortBy: SortField;
  sortDirection: "asc" | "desc";
  onSort: (field: SortField, direction: "asc" | "desc") => void;
  onHide?: () => void;
  ascendingLabel: string;
  descendingLabel: string;
  hideLabel: string;
  reorderLabel: string;
}) {
  const sortable = columnKey !== "title";
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } =
    useSortable({ id: columnKey, disabled: !sortable });
  const active = sortField === sortBy;

  return (
    <div
      ref={setNodeRef}
      style={{ transform: CSS.Transform.toString(transform), transition }}
      className={cn(
        "group/header flex min-w-0 items-center",
        isDragging && "opacity-40",
      )}
    >
      {sortable && (
        <button
          type="button"
          aria-label={reorderLabel}
          className="-ml-2 mr-0.5 rounded p-0.5 text-muted-foreground/50 opacity-0 hover:bg-accent hover:text-muted-foreground group-hover/header:opacity-100 focus-visible:opacity-100"
          {...attributes}
          {...listeners}
        >
          <GripVertical className="size-3" />
        </button>
      )}
      <DropdownMenu>
        <DropdownMenuTrigger
          className="flex min-w-0 items-center gap-1 rounded px-1.5 py-1 hover:bg-accent"
        >
          <span className="truncate">{label}</span>
          {active &&
            (sortDirection === "asc" ? (
              <ArrowUp className="size-3 shrink-0" />
            ) : (
              <ArrowDown className="size-3 shrink-0" />
            ))}
        </DropdownMenuTrigger>
        <DropdownMenuContent align="start" className="w-40">
          {sortField && (
            <>
              <DropdownMenuItem onClick={() => onSort(sortField, "asc")}>
                <ArrowUp />
                {ascendingLabel}
              </DropdownMenuItem>
              <DropdownMenuItem onClick={() => onSort(sortField, "desc")}>
                <ArrowDown />
                {descendingLabel}
              </DropdownMenuItem>
            </>
          )}
          {sortField && onHide && <DropdownMenuSeparator />}
          {onHide && (
            <DropdownMenuItem onClick={onHide}>
              <EyeOff />
              {hideLabel}
            </DropdownMenuItem>
          )}
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  );
}

export function TableColumnPicker({
  properties,
  trigger,
}: {
  properties: IssueProperty[];
  trigger: React.ReactElement;
}) {
  const { t } = useT("issues");
  const [search, setSearch] = useState("");
  const tableColumns = useViewStore((state) => state.tableColumns);
  const toggleTableColumn = useViewStore((state) => state.toggleTableColumn);
  const selected = useMemo(
    () => new Set(tableColumns.map((column) => column.key)),
    [tableColumns],
  );
  const query = search.trim().toLocaleLowerCase();
  const systemColumns = TABLE_SYSTEM_COLUMNS.filter((key) =>
    t(($) => $.table.columns[key as ColumnLabelKey])
      .toLocaleLowerCase()
      .includes(query),
  );
  const visibleProperties = properties.filter((property) =>
    property.name.toLocaleLowerCase().includes(query),
  );

  return (
    <DropdownMenu>
      <DropdownMenuTrigger render={trigger} />
      <DropdownMenuContent align="end" className="w-64 p-0">
        <div className="border-b p-2">
          <Input
            value={search}
            onChange={(event) => setSearch(event.target.value)}
            onKeyDown={(event) => {
              if (event.key !== "Escape") event.stopPropagation();
            }}
            placeholder={t(($) => $.table.columns.search_placeholder)}
            className="h-7"
          />
        </div>
        <div className="max-h-80 overflow-y-auto p-1">
          {systemColumns.length > 0 && (
            <DropdownMenuGroup>
              <DropdownMenuLabel>
                {t(($) => $.table.columns.system_section)}
              </DropdownMenuLabel>
              {systemColumns.map((key) => (
                <DropdownMenuItem
                  key={key}
                  disabled={key === "title"}
                  onClick={(event) => {
                    event.preventDefault();
                    toggleTableColumn(key);
                  }}
                >
                  <input
                    type="checkbox"
                    checked={selected.has(key)}
                    readOnly
                    className="size-3.5 accent-primary"
                  />
                  {t(($) => $.table.columns[key as ColumnLabelKey])}
                </DropdownMenuItem>
              ))}
            </DropdownMenuGroup>
          )}
          {visibleProperties.length > 0 && (
            <>
              {systemColumns.length > 0 && <DropdownMenuSeparator />}
              <DropdownMenuGroup>
                <DropdownMenuLabel>
                  {t(($) => $.table.columns.property_section)}
                </DropdownMenuLabel>
                {visibleProperties.map((property) => {
                  const key = `property:${property.id}` as const;
                  return (
                    <DropdownMenuItem
                      key={property.id}
                      onClick={(event) => {
                        event.preventDefault();
                        toggleTableColumn(key);
                      }}
                    >
                      <input
                        type="checkbox"
                        checked={selected.has(key)}
                        readOnly
                        className="size-3.5 accent-primary"
                      />
                      <span className="truncate">{property.name}</span>
                    </DropdownMenuItem>
                  );
                })}
              </DropdownMenuGroup>
            </>
          )}
          {systemColumns.length === 0 && visibleProperties.length === 0 && (
            <p className="px-2 py-6 text-center text-xs text-muted-foreground">
              {t(($) => $.table.columns.no_results)}
            </p>
          )}
        </div>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function InlineTitle({
  row,
  onUpdate,
  onToggleParent,
  toggleLabel,
}: {
  row: Extract<IssueTableDisplayRow, { kind: "issue" }>;
  onUpdate: (updates: Partial<UpdateIssueRequest>) => void;
  onToggleParent: () => void;
  toggleLabel: string;
}) {
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(row.issue.title);

  useEffect(() => setDraft(row.issue.title), [row.issue.title]);

  const commit = () => {
    const title = draft.trim();
    setEditing(false);
    if (title && title !== row.issue.title) onUpdate({ title });
    else setDraft(row.issue.title);
  };

  return (
    <div
      className="flex min-w-0 items-center gap-1.5"
      style={{ paddingLeft: row.depth * 18 }}
      onClick={stopRowNavigation}
    >
      {row.hasChildren ? (
        <button
          type="button"
          aria-label={toggleLabel}
          className="rounded p-0.5 text-muted-foreground hover:bg-accent"
          onClick={onToggleParent}
        >
          {row.collapsed ? (
            <ChevronRight className="size-3.5" />
          ) : (
            <ChevronDown className="size-3.5" />
          )}
        </button>
      ) : (
        <span className="w-4 shrink-0" />
      )}
      <span className="w-16 shrink-0 text-xs text-muted-foreground">
        {row.issue.identifier}
      </span>
      {editing ? (
        <Input
          autoFocus
          value={draft}
          onChange={(event) => setDraft(event.target.value)}
          onBlur={commit}
          onKeyDown={(event) => {
            if (event.key === "Enter") commit();
            if (event.key === "Escape") {
              setDraft(row.issue.title);
              setEditing(false);
            }
          }}
          className="h-7 min-w-0 flex-1 px-2"
        />
      ) : (
        <button
          type="button"
          className="min-w-0 flex-1 truncate text-left hover:underline"
          onClick={() => setEditing(true)}
        >
          {row.issue.title}
        </button>
      )}
    </div>
  );
}

function LazyLabelCell({ issue }: { issue: Issue }) {
  const { t } = useT("issues");
  const [editing, setEditing] = useState(false);
  const labels = issue.labels ?? [];
  if (editing) {
    return (
      <div onClick={stopRowNavigation}>
        <LabelPicker
          issueId={issue.id}
          open
          onOpenChange={(open) => {
            if (!open) setEditing(false);
          }}
          triggerRender={<button type="button" className="flex max-w-full gap-1" />}
        />
      </div>
    );
  }
  return (
    <button
      type="button"
      className="flex max-w-full items-center gap-1 overflow-hidden rounded px-1 py-0.5 hover:bg-accent"
      onClick={(event) => {
        event.stopPropagation();
        setEditing(true);
      }}
    >
      {labels.length > 0 ? (
        <>
          {labels.slice(0, 2).map((label) => (
            <LabelChip key={label.id} label={label} />
          ))}
          {labels.length > 2 && (
            <span className="text-xs text-muted-foreground">+{labels.length - 2}</span>
          )}
        </>
      ) : (
        <span className="text-muted-foreground">{t(($) => $.table.empty_value)}</span>
      )}
    </button>
  );
}

function QuickCreateFooter({
  colSpan,
  createDefaults,
  sentinelRef,
  loadingMore,
}: {
  colSpan: number;
  createDefaults: IssueCreateDefaults;
  sentinelRef: React.RefObject<HTMLDivElement | null>;
  loadingMore: boolean;
}) {
  const { t } = useT("issues");
  const [title, setTitle] = useState("");
  const createIssue = useCreateIssue();

  const submit = () => {
    const trimmed = title.trim();
    if (!trimmed || createIssue.isPending) return;
    const payload: CreateIssueRequest = { title: trimmed };
    const keys: (keyof IssueCreateDefaults)[] = [
      "status",
      "priority",
      "assignee_type",
      "assignee_id",
      "parent_issue_id",
      "project_id",
      "stage",
      "start_date",
      "due_date",
    ];
    for (const key of keys) {
      const value = createDefaults[key];
      if (value !== undefined && value !== null) {
        Object.assign(payload, { [key]: value });
      }
    }
    createIssue.mutate(payload, {
      onSuccess: () => setTitle(""),
      onError: (error) =>
        toast.error(
          error instanceof Error
            ? error.message
            : t(($) => $.table.quick_create_failed),
        ),
    });
  };

  return (
    <TableRow className="hover:bg-muted/30">
      <TableCell colSpan={colSpan} className="p-1.5">
        <div className="flex items-center gap-2">
          <Plus className="ml-2 size-3.5 text-muted-foreground" />
          <Input
            value={title}
            onChange={(event) => setTitle(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === "Enter") submit();
            }}
            placeholder={t(($) => $.table.quick_create_placeholder)}
            className="h-7 flex-1 border-0 bg-transparent shadow-none focus-visible:ring-0"
          />
          {(createIssue.isPending || loadingMore) && (
            <Loader2 className="mr-2 size-3.5 animate-spin text-muted-foreground" />
          )}
          <div ref={sentinelRef} className="h-px w-px" aria-hidden />
        </div>
      </TableCell>
    </TableRow>
  );
}

function propertyDisplayValue(
  property: IssueProperty,
  value: IssuePropertyValue | undefined,
) {
  if (value === undefined) return "";
  const options = property.config.options ?? [];
  if (property.type === "select") {
    return options.find((option) => option.id === value)?.name ?? "";
  }
  if (property.type === "multi_select") {
    const ids = Array.isArray(value) ? value : [];
    return options
      .filter((option) => ids.includes(option.id))
      .map((option) => option.name)
      .join(", ");
  }
  return String(value);
}

export function TableView({
  issues,
  childProgressMap,
  projectMap,
  createDefaults,
  fetchNextPage,
  hasNextPage,
  isFetchingNextPage,
  total,
  exportIssues,
}: TableViewProps) {
  const { t, i18n } = useT("issues");
  const wsId = useWorkspaceId();
  const navigation = useNavigation();
  const paths = useWorkspacePaths();
  const actions = useIssueSurfaceActionsOptional();
  const { getActorName } = useActorName();
  const { data: properties = [] } = useQuery(propertyListOptions(wsId));
  const propertyById = useMemo(
    () => new Map(properties.map((property) => [property.id, property])),
    [properties],
  );
  const activePropertyIds = useMemo(
    () => new Set(properties.map((property) => property.id)),
    [properties],
  );
  const tableColumns = useViewStore((state) => state.tableColumns);
  const toggleTableColumn = useViewStore((state) => state.toggleTableColumn);
  const reorderTableColumn = useViewStore((state) => state.reorderTableColumn);
  const setTableColumnWidth = useViewStore((state) => state.setTableColumnWidth);
  const tableGrouping = useViewStore((state) => state.tableGrouping);
  const setTableGrouping = useViewStore((state) => state.setTableGrouping);
  const tableCollapsedGroups = useViewStore((state) => state.tableCollapsedGroups);
  const toggleTableGroupCollapsed = useViewStore(
    (state) => state.toggleTableGroupCollapsed,
  );
  const tableCollapsedParents = useViewStore((state) => state.tableCollapsedParents);
  const toggleTableParentCollapsed = useViewStore(
    (state) => state.toggleTableParentCollapsed,
  );
  const tableHierarchy = useViewStore((state) => state.tableHierarchy);
  const toggleTableHierarchy = useViewStore((state) => state.toggleTableHierarchy);
  const tableCalculation = useViewStore((state) => state.tableCalculation);
  const setTableCalculation = useViewStore((state) => state.setTableCalculation);
  const sortBy = useViewStore((state) => state.sortBy);
  const setSortBy = useViewStore((state) => state.setSortBy);
  const sortDirection = useViewStore((state) => state.sortDirection);
  const setSortDirection = useViewStore((state) => state.setSortDirection);
  const [exporting, setExporting] = useState(false);
  const sentinelRef = useRef<HTMLDivElement>(null);

  const groupingPropertyId = propertyIdFromViewKey(tableGrouping);
  const effectiveTableGrouping =
    groupingPropertyId && !activePropertyIds.has(groupingPropertyId)
      ? "none"
      : tableGrouping;

  const visibleColumnConfigs = useMemo(
    () =>
      tableColumns.filter((column) => {
        const propertyId = propertyIdFromViewKey(column.key);
        return !propertyId || activePropertyIds.has(propertyId);
      }),
    [activePropertyIds, tableColumns],
  );

  const displayRows = useMemo(
    () =>
      buildIssueTableRows(issues, {
        grouping: effectiveTableGrouping,
        properties,
        collapsedGroups: new Set(tableCollapsedGroups),
        collapsedParents: new Set(tableCollapsedParents),
        hierarchy: tableHierarchy,
        getActorName,
        getStatusLabel: (status) => t(($) => $.status[status]),
        noValueLabel: t(($) => $.table.no_value),
        unassignedLabel: t(($) => $.table.unassigned),
        trueLabel: t(($) => $.pickers.custom_property.true_label),
        falseLabel: t(($) => $.pickers.custom_property.false_label),
      }),
    [
      getActorName,
      issues,
      properties,
      t,
      tableCollapsedGroups,
      tableCollapsedParents,
      effectiveTableGrouping,
      tableHierarchy,
    ],
  );
  const visibleIssueIds = useMemo(
    () =>
      displayRows
        .filter((row): row is Extract<IssueTableDisplayRow, { kind: "issue" }> => row.kind === "issue")
        .map((row) => row.issue.id),
    [displayRows],
  );

  const columnLabel = useCallback(
    (key: TableColumnKey) => {
      const propertyId = propertyIdFromViewKey(key);
      if (propertyId) return propertyById.get(propertyId)?.name ?? t(($) => $.table.no_value);
      return t(($) => $.table.columns[key as ColumnLabelKey]);
    },
    [propertyById, t],
  );

  const updateIssue = useCallback(
    (issueId: string, updates: Partial<UpdateIssueRequest>) =>
      actions?.updateIssue(issueId, updates),
    [actions],
  );

  const makeColumn = useCallback(
    (config: TableColumnConfig): ColumnDef<IssueTableDisplayRow> => {
      const propertyId = propertyIdFromViewKey(config.key);
      const property = propertyId ? propertyById.get(propertyId) : undefined;
      const staticSort = propertyId
        ? property && !["multi_select", "checkbox"].includes(property.type)
          ? (`property:${propertyId}` as SortField)
          : undefined
        : SORTABLE_COLUMNS[config.key as TableSystemColumnKey];
      const definition: ColumnDef<IssueTableDisplayRow> = {
        id: config.key,
        minSize: config.key === "title" ? 260 : 96,
        maxSize: 640,
        enableResizing: true,
        header: () => (
          <SortableColumnHeader
            columnKey={config.key}
            label={columnLabel(config.key)}
            sortField={staticSort}
            sortBy={sortBy}
            sortDirection={sortDirection}
            onSort={(field, direction) => {
              setSortBy(field);
              setSortDirection(direction);
            }}
            onHide={
              config.key === "title"
                ? undefined
                : () => toggleTableColumn(config.key)
            }
            ascendingLabel={t(($) => $.table.sort_ascending)}
            descendingLabel={t(($) => $.table.sort_descending)}
            hideLabel={t(($) => $.table.columns.hide)}
            reorderLabel={t(($) => $.table.columns.reorder, {
              column: columnLabel(config.key),
            })}
          />
        ),
        cell: ({ row }) => {
          if (row.original.kind !== "issue") return null;
          const issueRow = row.original;
          const issue = issueRow.issue;
          const onUpdate = (updates: Partial<UpdateIssueRequest>) =>
            updateIssue(issue.id, updates);

          if (property) {
            return (
              <div onClick={stopRowNavigation}>
                <CustomPropertyValueEditor issue={issue} property={property} />
              </div>
            );
          }
          switch (config.key) {
            case "title":
              return (
                <InlineTitle
                  row={issueRow}
                  onUpdate={onUpdate}
                  onToggleParent={() =>
                    toggleTableParentCollapsed(issue.id)
                  }
                  toggleLabel={t(($) => $.table.toggle_sub_issues)}
                />
              );
            case "identifier":
              return <span className="text-xs text-muted-foreground">{issue.identifier}</span>;
            case "status":
              return (
                <div onClick={stopRowNavigation}>
                  <StatusPicker status={issue.status} onUpdate={onUpdate} align="start" />
                </div>
              );
            case "priority":
              return (
                <div onClick={stopRowNavigation}>
                  <PriorityPicker priority={issue.priority} onUpdate={onUpdate} align="start" />
                </div>
              );
            case "assignee":
              return (
                <div onClick={stopRowNavigation}>
                  <AssigneePicker
                    assigneeType={issue.assignee_type}
                    assigneeId={issue.assignee_id}
                    onUpdate={onUpdate}
                    align="start"
                  />
                </div>
              );
            case "labels":
              return <LazyLabelCell issue={issue} />;
            case "project":
              return (
                <div onClick={stopRowNavigation}>
                  <ProjectPicker
                    projectId={issue.project_id}
                    onUpdate={onUpdate}
                    triggerRender={<button type="button" className="flex max-w-full items-center gap-1.5 rounded px-1 py-0.5 hover:bg-accent" />}
                  />
                </div>
              );
            case "start_date":
              return (
                <div onClick={stopRowNavigation}>
                  <StartDatePicker startDate={issue.start_date} onUpdate={onUpdate} />
                </div>
              );
            case "due_date":
              return (
                <div onClick={stopRowNavigation}>
                  <DueDatePicker dueDate={issue.due_date} onUpdate={onUpdate} />
                </div>
              );
            case "created_at":
            case "updated_at":
              return (
                <span className="text-xs text-muted-foreground">
                  {new Intl.DateTimeFormat(i18n.language, {
                    month: "short",
                    day: "numeric",
                    year: "numeric",
                  }).format(new Date(issue[config.key]))}
                </span>
              );
            case "child_progress": {
              const progress = childProgressMap.get(issue.id);
              return progress ? (
                <span className="inline-flex items-center gap-1.5 text-xs text-muted-foreground">
                  <ProgressRing done={progress.done} total={progress.total} size={15} />
                  {progress.done}/{progress.total}
                </span>
              ) : (
                <span className="text-muted-foreground">{t(($) => $.table.empty_value)}</span>
              );
            }
            case "creator":
              return (
                <span className="flex min-w-0 items-center gap-1.5">
                  <ActorAvatar
                    actorType={issue.creator_type}
                    actorId={issue.creator_id}
                    size="sm"
                  />
                  <span className="truncate">
                    {getActorName(issue.creator_type, issue.creator_id)}
                  </span>
                </span>
              );
          }
          return null;
        },
      };
      if (config.width !== undefined) definition.size = config.width;
      return definition;
    },
    [
      childProgressMap,
      columnLabel,
      getActorName,
      i18n.language,
      propertyById,
      setSortBy,
      setSortDirection,
      sortBy,
      sortDirection,
      t,
      toggleTableColumn,
      toggleTableParentCollapsed,
      updateIssue,
    ],
  );

  const columns = useMemo<ColumnDef<IssueTableDisplayRow>[]>(
    () => [
      {
        id: SELECT_COLUMN_ID,
        size: 44,
        minSize: 44,
        maxSize: 44,
        enableResizing: false,
        header: () => (
          <SelectAllCheckbox
            issueIds={visibleIssueIds}
            label={t(($) => $.table.select_all)}
          />
        ),
        cell: ({ row }) =>
          row.original.kind === "issue" ? (
            <IssueCheckbox
              issueId={row.original.issue.id}
              label={t(($) => $.table.select_issue, {
                identifier: row.original.issue.identifier,
              })}
            />
          ) : null,
      },
      ...visibleColumnConfigs.map(makeColumn),
      {
        id: ADD_COLUMN_ID,
        size: 48,
        minSize: 48,
        maxSize: 48,
        enableResizing: false,
        header: () => (
          <TableColumnPicker
            properties={properties}
            trigger={
              <button
                type="button"
                aria-label={t(($) => $.table.columns.add)}
                className="rounded p-1 text-muted-foreground hover:bg-accent hover:text-foreground"
              >
                <Plus className="size-3.5" />
              </button>
            }
          />
        ),
        cell: () => null,
      },
    ],
    [makeColumn, properties, t, visibleColumnConfigs, visibleIssueIds],
  );

  const columnSizing = useMemo<ColumnSizingState>(
    () =>
      Object.fromEntries(
        visibleColumnConfigs
          .filter((column) => column.width !== undefined)
          .map((column) => [column.key, column.width!]),
      ),
    [visibleColumnConfigs],
  );
  const handleColumnSizingChange = useCallback<OnChangeFn<ColumnSizingState>>(
    (updater) => {
      const next = typeof updater === "function" ? updater(columnSizing) : updater;
      for (const column of visibleColumnConfigs) {
        const width = next[column.key];
        if (width !== column.width) setTableColumnWidth(column.key, width);
      }
    },
    [columnSizing, setTableColumnWidth, visibleColumnConfigs],
  );

  const table = useReactTable({
    data: displayRows,
    columns,
    getRowId: (row) => row.key,
    getCoreRowModel: getCoreRowModel(),
    state: {
      columnSizing,
      columnPinning: { left: [SELECT_COLUMN_ID, "title"], right: [] },
    },
    onColumnSizingChange: handleColumnSizingChange,
    columnResizeMode: "onChange",
  });

  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 4 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
  );
  const handleDragEnd = useCallback(
    ({ active, over }: DragEndEvent) => {
      if (!over || active.id === over.id) return;
      reorderTableColumn(active.id as TableColumnKey, over.id as TableColumnKey);
    },
    [reorderTableColumn],
  );

  useEffect(() => {
    const sentinel = sentinelRef.current;
    if (!sentinel || !hasNextPage) return;
    const observer = new IntersectionObserver(
      (entries) => {
        if (entries[0]?.isIntersecting && !isFetchingNextPage) {
          void fetchNextPage();
        }
      },
      { rootMargin: "320px" },
    );
    observer.observe(sentinel);
    return () => observer.disconnect();
  }, [fetchNextPage, hasNextPage, isFetchingNextPage]);

  const groupingProperties = useMemo(
    () =>
      properties.filter((property) =>
        ["select", "multi_select", "checkbox"].includes(property.type),
      ),
    [properties],
  );

  const handleExport = async () => {
    setExporting(true);
    try {
      const rows = await exportIssues();
      const csvColumns = visibleColumnConfigs;
      const headers = csvColumns.map((column) => columnLabel(column.key));
      const csvRows = rows.map((issue) =>
        csvColumns.map((column) => {
          const propertyId = propertyIdFromViewKey(column.key);
          if (propertyId) {
            const property = propertyById.get(propertyId);
            return property
              ? propertyDisplayValue(property, issue.properties[propertyId])
              : "";
          }
          switch (column.key) {
            case "title":
              return issue.title;
            case "identifier":
              return issue.identifier;
            case "status":
              return t(($) => $.status[issue.status]);
            case "priority":
              return t(($) => $.priority[issue.priority]);
            case "assignee":
              return issue.assignee_type && issue.assignee_id
                ? getActorName(issue.assignee_type, issue.assignee_id)
                : "";
            case "labels":
              return issue.labels?.map((label) => label.name).join(", ") ?? "";
            case "project":
              return issue.project_id ? projectMap.get(issue.project_id)?.title ?? "" : "";
            case "start_date":
            case "due_date":
              return issue[column.key] ?? "";
            case "created_at":
            case "updated_at":
              return issue[column.key];
            case "child_progress": {
              const progress = childProgressMap.get(issue.id);
              return progress ? `${progress.done}/${progress.total}` : "";
            }
            case "creator":
              return getActorName(issue.creator_type, issue.creator_id);
          }
          return "";
        }),
      );
      const csv = buildIssueTableCsv(headers, csvRows);
      const blob = new Blob(["\uFEFF", csv], { type: "text/csv;charset=utf-8" });
      const url = URL.createObjectURL(blob);
      const anchor = document.createElement("a");
      anchor.href = url;
      anchor.download = `issues-${new Date().toISOString().slice(0, 10)}.csv`;
      anchor.click();
      URL.revokeObjectURL(url);
      toast.success(t(($) => $.table.export_success, { count: rows.length }));
    } catch (error) {
      toast.error(
        error instanceof Error ? error.message : t(($) => $.table.export_failed),
      );
    } finally {
      setExporting(false);
    }
  };

  const footer = (
    <TableFooter className="bg-background">
      {tableCalculation !== "none" && (
        <TableRow>
          {table.getVisibleLeafColumns().map((column) => {
            const key = column.id;
            const value =
              key === SELECT_COLUMN_ID || key === ADD_COLUMN_ID
                ? null
                : calculateIssueTableColumn(
                    issues,
                    key as TableColumnKey,
                    tableCalculation,
                  );
            return (
              <TableCell
                key={column.id}
                className="overflow-hidden px-4 py-1.5 text-xs text-muted-foreground"
                style={getCellStyle(column, { withBorder: true, hasExplicitSize: true })}
              >
                {key === "title"
                  ? t(($) => $.table.calculation[tableCalculation])
                  : typeof value === "number"
                    ? Number.isInteger(value)
                      ? value
                      : value.toFixed(2)
                    : null}
              </TableCell>
            );
          })}
        </TableRow>
      )}
      <QuickCreateFooter
        colSpan={table.getVisibleLeafColumns().length}
        createDefaults={createDefaults}
        sentinelRef={sentinelRef}
        loadingMore={isFetchingNextPage}
      />
    </TableFooter>
  );

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex shrink-0 items-center gap-2 border-b px-3 py-1.5">
        <span className="mr-auto text-xs text-muted-foreground">
          {t(($) => $.table.loaded_count, { count: issues.length, total })}
        </span>
        <Button
          variant={tableHierarchy ? "secondary" : "ghost"}
          size="sm"
          className="h-7"
          onClick={toggleTableHierarchy}
        >
          <ListTree className="size-3.5" />
          {t(($) => $.table.hierarchy)}
        </Button>
        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <Button variant="ghost" size="sm" className="h-7">
                <TableProperties className="size-3.5" />
                {t(($) => $.table.group_label)}
              </Button>
            }
          />
          <DropdownMenuContent align="end" className="w-48">
            <DropdownMenuRadioGroup
              value={effectiveTableGrouping}
              onValueChange={(value) => setTableGrouping(value as TableGrouping)}
            >
              <DropdownMenuRadioItem value="none">
                {t(($) => $.table.group_none)}
              </DropdownMenuRadioItem>
              <DropdownMenuRadioItem value="status">
                {t(($) => $.table.columns.status)}
              </DropdownMenuRadioItem>
              <DropdownMenuRadioItem value="assignee">
                {t(($) => $.table.columns.assignee)}
              </DropdownMenuRadioItem>
            </DropdownMenuRadioGroup>
            {groupingProperties.length > 0 && (
              <>
                <DropdownMenuSeparator />
                <DropdownMenuGroup>
                  <DropdownMenuLabel>
                    {t(($) => $.table.columns.property_section)}
                  </DropdownMenuLabel>
                  <DropdownMenuRadioGroup
                    value={effectiveTableGrouping}
                    onValueChange={(value) =>
                      setTableGrouping(value as TableGrouping)
                    }
                  >
                    {groupingProperties.map((property) => (
                      <DropdownMenuRadioItem
                        key={property.id}
                        value={`property:${property.id}`}
                      >
                        {property.name}
                      </DropdownMenuRadioItem>
                    ))}
                  </DropdownMenuRadioGroup>
                </DropdownMenuGroup>
              </>
            )}
          </DropdownMenuContent>
        </DropdownMenu>
        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <Button variant="ghost" size="sm" className="h-7">
                <Sigma className="size-3.5" />
                {t(($) => $.table.calculation.label)}
              </Button>
            }
          />
          <DropdownMenuContent align="end">
            <DropdownMenuRadioGroup
              value={tableCalculation}
              onValueChange={(value) =>
                setTableCalculation(value as TableCalculation)
              }
            >
              {(["none", "sum", "average", "count"] as const).map((value) => (
                <DropdownMenuRadioItem key={value} value={value}>
                  {t(($) => $.table.calculation[value])}
                </DropdownMenuRadioItem>
              ))}
            </DropdownMenuRadioGroup>
          </DropdownMenuContent>
        </DropdownMenu>
        <TableColumnPicker
          properties={properties}
          trigger={
            <Button variant="ghost" size="sm" className="h-7">
              <Plus className="size-3.5" />
              {t(($) => $.table.columns.add)}
            </Button>
          }
        />
        <Button
          variant="ghost"
          size="sm"
          className="h-7"
          disabled={exporting}
          onClick={() => void handleExport()}
        >
          {exporting ? (
            <Loader2 className="size-3.5 animate-spin" />
          ) : (
            <Download className="size-3.5" />
          )}
          {t(($) => $.table.export)}
        </Button>
      </div>
      <DndContext
        sensors={sensors}
        collisionDetection={closestCenter}
        onDragEnd={handleDragEnd}
      >
        <SortableContext
          items={visibleColumnConfigs.map((column) => column.key)}
          strategy={horizontalListSortingStrategy}
        >
          <DataTable
            table={table}
            footer={footer}
            emptyMessage={t(($) => $.table.empty)}
            onRowClick={(row) => {
              if (row.original.kind === "issue") {
                navigation.push(paths.issueDetail(row.original.issue.id));
              }
            }}
            renderRow={(row) => {
              if (row.original.kind !== "group") return null;
              return (
                <TableRow
                  className="bg-muted/40 hover:bg-muted/60"
                  onClick={() => toggleTableGroupCollapsed(row.original.key)}
                >
                  <TableCell
                    colSpan={table.getVisibleLeafColumns().length}
                    className="h-9 px-4 py-1.5"
                  >
                    <button
                      type="button"
                      className="flex items-center gap-2 text-xs font-medium"
                    >
                      {row.original.collapsed ? (
                        <ChevronRight className="size-3.5" />
                      ) : (
                        <ChevronDown className="size-3.5" />
                      )}
                      {row.original.label}
                      <span className="font-normal tabular-nums text-muted-foreground">
                        {row.original.count}
                      </span>
                    </button>
                  </TableCell>
                </TableRow>
              );
            }}
            className="min-h-0 flex-1"
          />
        </SortableContext>
      </DndContext>
    </div>
  );
}
