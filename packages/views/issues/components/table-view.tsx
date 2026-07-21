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
  Loader2,
  Plus,
  Search,
  X,
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
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import {
  TableCell,
  TableRow,
} from "@multica/ui/components/ui/table";
import { cn } from "@multica/ui/lib/utils";
import { ApiError } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { ALL_STATUSES } from "@multica/core/issues/config";
import {
  issueKeys,
  issueTableGroupsOptions,
  issueTableRowPageOptions,
} from "@multica/core/issues/queries";
import {
  TABLE_SYSTEM_COLUMNS,
  propertyIdFromViewKey,
  type SortField,
  type TableColumnConfig,
  type TableColumnKey,
  type TableSystemColumnKey,
} from "@multica/core/issues/stores/view-store";
import { useViewStore } from "@multica/core/issues/stores/view-store-context";
import { propertyListOptions } from "@multica/core/properties";
import { useWorkspacePaths } from "@multica/core/paths";
import { buildActorNameResolver, useActorName } from "@multica/core/workspace/hooks";
import {
  agentListOptions,
  memberListOptions,
  squadListOptions,
} from "@multica/core/workspace/queries";
import type {
  Issue,
  IssueProperty,
  IssuePropertyValue,
  IssueStatus,
  IssueTableGroupDescriptor,
  IssueTableGroupSpec,
  IssueTableQuerySpec,
  IssueTableRowsResponse,
  Project,
  UpdateIssueRequest,
} from "@multica/core/types";
import {
  useInfiniteQuery,
  useQueries,
  useQuery,
  useQueryClient,
  type UseQueryResult,
} from "@tanstack/react-query";
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
  getIssueTableSelectionRange,
  IssueTableExportIntegrityError,
  type IssueTableDisplayRow,
} from "./table-view-model";
import type { ChildProgress } from "./list-row";
import { InfiniteScrollSentinel } from "./infinite-scroll-sentinel";

const SELECT_COLUMN_ID = "__select";
const ADD_COLUMN_ID = "__add";

type TableViewProps = {
  serverQuery: IssueTableQuerySpec;
  childProgressMap: Map<string, ChildProgress>;
  search: string;
  onSearchChange: (query: string) => void;
  onLoadedIssuesChange: (issues: Issue[]) => void;
  exportIssues: () => Promise<Issue[]>;
  resolveExportLookups: (needs: {
    projects: boolean;
    childProgress: boolean;
  }) => Promise<{
    projectMap: Map<string, Project>;
    childProgressMap: Map<string, ChildProgress>;
  }>;
};

type ServerBranch = {
  key: string;
  groupKey: string | null;
  parentId: string | null;
  ancestorIds: string[];
  cursors: Array<string | null>;
};

type ServerBranchState = {
  identity: string;
  structureIdentity: string;
  branches: Map<string, ServerBranch>;
};

type ServerBranchPageTarget = {
  branch: ServerBranch;
  cursor: string | null;
};

type ServerBranchData = {
  rows: IssueTableRowsResponse["rows"];
  total: number;
  nextCursor: string | null;
  headUpdatedAt: number;
  headFetching: boolean;
  loading: boolean;
  error: boolean;
  placeholder: boolean;
};

type LoadedIssueState = {
  membershipIdentity: string;
  issues: Map<string, Issue>;
};

function serverBranchKey(groupKey: string | null, parentId: string | null) {
  return `${groupKey ?? "ungrouped"}::${parentId ?? "root"}`;
}

function sameStringArray(left: string[], right: string[]) {
  return (
    left.length === right.length &&
    left.every((value, index) => value === right[index])
  );
}

/** Rebase the branch graph synchronously when a query changes.
 *
 * Filters/search/sort keep the same group/hierarchy structure, so preserving
 * branch identities while resetting every cursor to the head lets React Query
 * show the previous rows during the new request. A real structure change
 * (group kind or hierarchy) discards incompatible group/parent identities. */
function rebaseServerBranchState(
  previous: ServerBranchState,
  identity: string,
  structureIdentity: string,
  usesServerGrouping: boolean,
): ServerBranchState {
  if (previous.identity === identity) return previous;

  const branches =
    previous.structureIdentity === structureIdentity
      ? new Map(
          [...previous.branches].map(([key, branch]) => [
            key,
            { ...branch, cursors: [null] },
          ]),
        )
      : new Map<string, ServerBranch>();

  if (!usesServerGrouping) {
    const key = serverBranchKey(null, null);
    if (!branches.has(key)) {
      branches.set(key, {
        key,
        groupKey: null,
        parentId: null,
        ancestorIds: [],
        cursors: [null],
      });
    }
  }

  return { identity, structureIdentity, branches };
}

function tableGroupSpec(grouping: string): IssueTableGroupSpec {
  if (grouping === "status") return { kind: "status" };
  if (grouping === "assignee") return { kind: "assignee" };
  const propertyId = propertyIdFromViewKey(grouping);
  if (propertyId) return { kind: "property", property_id: propertyId };
  return { kind: "none" };
}

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

function IssueCheckbox({
  checked,
  label,
  onToggle,
}: {
  checked: boolean;
  label: string;
  onToggle: (shiftKey: boolean) => void;
}) {
  return (
    <input
      type="checkbox"
      aria-label={label}
      checked={checked}
      onClick={(event) => {
        event.stopPropagation();
        onToggle(event.shiftKey);
      }}
      onChange={() => undefined}
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

export function TableIssueSearch({
  value,
  onChange,
  placeholder,
  clearLabel,
}: {
  value: string;
  onChange: (value: string) => void;
  placeholder: string;
  clearLabel: string;
}) {
  return (
    <div className="relative w-56 shrink-0">
      <Search
        aria-hidden
        className="pointer-events-none absolute left-2 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground"
      />
      <Input
        type="text"
        role="searchbox"
        inputMode="search"
        value={value}
        onChange={(event) => onChange(event.target.value)}
        aria-label={placeholder}
        placeholder={placeholder}
        className="h-7 pl-7 pr-7 text-xs"
      />
      {value && (
        <Button
          type="button"
          variant="ghost"
          size="icon-xs"
          aria-label={clearLabel}
          onClick={() => onChange("")}
          className="absolute right-0.5 top-0.5 text-muted-foreground"
        >
          <X className="size-3" />
        </Button>
      )}
    </div>
  );
}

export function InlineTitle({
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
  const editingRef = useRef(editing);
  editingRef.current = editing;

  useEffect(() => {
    // Realtime/cache snapshots should refresh the passive label, but must not
    // overwrite text the user is actively composing.
    if (!editingRef.current) setDraft(row.issue.title);
  }, [row.issue.title]);

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

type IssueTableGroupRowProps = {
  group: Extract<IssueTableDisplayRow, { kind: "group" }>;
  colSpan: number;
  onToggle: () => void;
};

export function IssueTableGroupRow({
  group,
  colSpan,
  onToggle,
}: IssueTableGroupRowProps) {
  return (
    <TableRow
      className="bg-muted/40 hover:bg-muted/60"
      onClick={onToggle}
    >
      <TableCell colSpan={colSpan} className="h-9 px-4 py-1.5">
        <button
          type="button"
          className="sticky left-4 flex w-fit items-center gap-2 text-xs font-medium"
        >
          {group.collapsed ? (
            <ChevronRight className="size-3.5" />
          ) : (
            <ChevronDown className="size-3.5" />
          )}
          {group.label}
          <span className="font-normal tabular-nums text-muted-foreground">
            {group.count}
          </span>
        </button>
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
  serverQuery,
  childProgressMap,
  search,
  onSearchChange,
  onLoadedIssuesChange,
  exportIssues,
  resolveExportLookups,
}: TableViewProps) {
  const { t, i18n } = useT("issues");
  const wsId = useWorkspaceId();
  const queryClient = useQueryClient();
  const navigation = useNavigation();
  const paths = useWorkspacePaths();
  const actions = useIssueSurfaceActionsOptional();
  const selection = useIssueSurfaceSelection();
  const { getActorName } = useActorName();
  const {
    data: properties = [],
    isSuccess: propertyCatalogSettled,
  } = useQuery(propertyListOptions(wsId));
  const propertyById = useMemo(
    () => new Map(properties.map((property) => [property.id, property])),
    [properties],
  );
  const activePropertyIds = useMemo(
    () => new Set(properties.map((property) => property.id)),
    [properties],
  );
  const groupablePropertyIds = useMemo(
    () =>
      new Set(
        properties
          .filter((property) => ["select", "checkbox"].includes(property.type))
          .map((property) => property.id),
      ),
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
  const sortBy = useViewStore((state) => state.sortBy);
  const setSortBy = useViewStore((state) => state.setSortBy);
  const sortDirection = useViewStore((state) => state.sortDirection);
  const setSortDirection = useViewStore((state) => state.setSortDirection);
  const [exporting, setExporting] = useState<"all" | "selected" | null>(null);
  const selectionAnchorRef = useRef<string | null>(null);

  const groupingPropertyId = propertyIdFromViewKey(tableGrouping);
  const effectiveTableGrouping =
    groupingPropertyId &&
    propertyCatalogSettled &&
    !groupablePropertyIds.has(groupingPropertyId)
      ? "none"
      : tableGrouping;
  useEffect(() => {
    if (
      !groupingPropertyId ||
      !propertyCatalogSettled ||
      groupablePropertyIds.has(groupingPropertyId)
    ) {
      return;
    }
    setTableGrouping("none");
    toast.info(t(($) => $.table.group_property_unavailable));
  }, [
    groupablePropertyIds,
    groupingPropertyId,
    propertyCatalogSettled,
    setTableGrouping,
    t,
  ]);

  const serverGroupSpec = useMemo(
    () => tableGroupSpec(effectiveTableGrouping),
    [effectiveTableGrouping],
  );
  const usesServerGrouping = serverGroupSpec.kind !== "none";
  const serverGroupsRequestGroup =
    serverGroupSpec.kind === "none"
      ? ({ kind: "status" } as const)
      : serverGroupSpec;
  const serverGroupsQuery = useInfiniteQuery({
    ...issueTableGroupsOptions(
      wsId,
      serverQuery,
      serverGroupsRequestGroup,
    ),
    enabled: usesServerGrouping,
  });
  const {
    data: serverGroupsData,
    isPending: serverGroupsPending,
    isError: serverGroupsError,
    hasNextPage: hasNextServerGroupPage,
    isFetchingNextPage: fetchingNextServerGroupPage,
    refetch: refetchServerGroups,
    fetchNextPage: fetchNextServerGroupPage,
  } = serverGroupsQuery;
  useEffect(() => {
    const body =
      serverGroupsQuery.error instanceof ApiError &&
      serverGroupsQuery.error.body &&
      typeof serverGroupsQuery.error.body === "object"
        ? (serverGroupsQuery.error.body as { error?: unknown })
        : null;
    if (
      serverGroupsQuery.error instanceof ApiError &&
      serverGroupsQuery.error.status === 422 &&
      body?.error === "unsupported_group"
    ) {
      setTableGrouping("none");
      toast.info(t(($) => $.table.group_property_unavailable));
    }
  }, [serverGroupsQuery.error, setTableGrouping, t]);
  const serverGroups = useMemo(
    () => serverGroupsData?.pages.flatMap((page) => page.groups) ?? [],
    [serverGroupsData?.pages],
  );
  const latestServerGroupPage =
    serverGroupsData?.pages[serverGroupsData.pages.length - 1];
  const groupedServerTotal = latestServerGroupPage?.total ?? 0;
  const serverIdentity = useMemo(
    () => JSON.stringify([serverQuery, serverGroupSpec, tableHierarchy]),
    [serverGroupSpec, serverQuery, tableHierarchy],
  );
  const serverStructureIdentity = useMemo(
    () => JSON.stringify([serverGroupSpec, tableHierarchy]),
    [serverGroupSpec, tableHierarchy],
  );
  const collapsedGroupSet = useMemo(
    () => new Set(tableCollapsedGroups),
    [tableCollapsedGroups],
  );
  const collapsedParentSet = useMemo(
    () => new Set(tableCollapsedParents),
    [tableCollapsedParents],
  );
  const [serverBranchState, setServerBranchState] =
    useState<ServerBranchState>({
      identity: "",
      structureIdentity: "",
      branches: new Map(),
    });

  const rebasedServerBranchState = useMemo(
    () =>
      rebaseServerBranchState(
        serverBranchState,
        serverIdentity,
        serverStructureIdentity,
        usesServerGrouping,
      ),
    [
      serverBranchState,
      serverIdentity,
      serverStructureIdentity,
      usesServerGrouping,
    ],
  );

  // Commit the synchronous rebase after render. Consumers use the derived
  // state above immediately, so a filter/search/sort transition never has an
  // empty frame while this effect catches state up.
  useEffect(() => {
    if (rebasedServerBranchState !== serverBranchState) {
      setServerBranchState(rebasedServerBranchState);
    }
  }, [rebasedServerBranchState, serverBranchState]);

  const activeServerBranches = rebasedServerBranchState.branches;
  const serverBranchPlaceholderRef = useRef(
    new Map<string, IssueTableRowsResponse>(),
  );
  const serverBranchPageTargets = useMemo<ServerBranchPageTarget[]>(
    () =>
      [...activeServerBranches.values()].flatMap((branch) =>
        branch.cursors.map((cursor) => ({ branch, cursor })),
      ),
    [activeServerBranches],
  );
  // useQueries compares and installs the supplied query list in an effect.
  // Keeping this array stable prevents a settled branch from being installed
  // again on every render (which can otherwise create a render loop once the
  // virtualized table starts measuring rows).
  const serverBranchQueries = useMemo(
    () =>
      serverBranchPageTargets.map(({ branch, cursor }) => {
        const placeholder =
          cursor === null
            ? serverBranchPlaceholderRef.current.get(
                `${serverStructureIdentity}:${branch.key}`,
              )
            : undefined;
        return {
          ...issueTableRowPageOptions(wsId, {
            query: serverQuery,
            group: serverGroupSpec,
            group_key: branch.groupKey,
            hierarchy: { enabled: tableHierarchy },
            parent_id: branch.parentId,
            page: { limit: 50, cursor },
          }),
          // QueriesObserver replaces observers by query hash, so
          // keepPreviousData alone cannot bridge a changed table query inside
          // useQueries. Retain the last settled head per structural branch to
          // keep the previous table painted while the new query is pending.
          ...(placeholder ? { placeholderData: () => placeholder } : {}),
          enabled:
            (branch.groupKey === null ||
              !collapsedGroupSet.has(branch.groupKey)) &&
            !branch.ancestorIds.some((id) => collapsedParentSet.has(id)),
        };
      }),
    [
      collapsedGroupSet,
      collapsedParentSet,
      serverBranchPageTargets,
      serverGroupSpec,
      serverQuery,
      serverStructureIdentity,
      tableHierarchy,
      wsId,
    ],
  );
  const combineServerBranchQueries = useCallback(
    (results: Array<UseQueryResult<IssueTableRowsResponse, Error>>) => {
      const byBranch: Record<string, ServerBranchData> = {};
      for (let index = 0; index < serverBranchPageTargets.length; index += 1) {
        const target = serverBranchPageTargets[index];
        const result = results[index];
        if (!target || !result) continue;
        const current = byBranch[target.branch.key] ?? {
          rows: [],
          total: 0,
          nextCursor: null,
          headUpdatedAt: 0,
          headFetching: false,
          loading: false,
          error: false,
          placeholder: false,
        };
        const page = result.data;
        if (page) {
          current.rows.push(...page.rows);
          current.total = page.total;
          current.nextCursor = page.next_cursor;
        }
        if (target.cursor === null) {
          current.headUpdatedAt = result.dataUpdatedAt;
          current.headFetching = result.isFetching;
        }
        current.loading ||= result.isPending || result.isFetching;
        current.error ||= result.isError;
        current.placeholder ||= result.isPlaceholderData;
        byBranch[target.branch.key] = current;
      }
      return byBranch;
    },
    [serverBranchPageTargets],
  );
  // `combine` is structurally shared by React Query. It must return only plain
  // objects/arrays: Map instances and freshly-created retry closures cannot be
  // shared, and make useQueries publish a different snapshot on every render.
  const serverBranchData = useQueries({
    queries: serverBranchQueries,
    combine: combineServerBranchQueries,
  });

  useEffect(() => {
    const next = new Map<string, IssueTableRowsResponse>();
    for (const branch of activeServerBranches.values()) {
      const key = `${serverStructureIdentity}:${branch.key}`;
      const data = serverBranchData[branch.key];
      if (!data || data.placeholder || data.loading || data.error) {
        const previous = serverBranchPlaceholderRef.current.get(key);
        if (previous) next.set(key, previous);
        continue;
      }
      next.set(key, {
        query_fingerprint: "__table_placeholder__",
        group_key: branch.groupKey,
        parent_id: branch.parentId,
        total: data.total,
        rows: data.rows,
        branch_total: data.rows.length,
        next_cursor: null,
      });
    }
    // Bound placeholders to the active structural graph. Sort/filter/search
    // transitions reuse these entries; old group/property configurations do
    // not accumulate for the lifetime of the Table component.
    serverBranchPlaceholderRef.current = next;
  }, [
    activeServerBranches,
    serverBranchData,
    serverStructureIdentity,
  ]);

  // Re-derive ancestry from the current row graph after realtime re-parenting.
  // Branch keys are parent ids, so an existing branch can move under a new
  // collapsed ancestor without being re-activated by the viewport sentinel.
  useEffect(() => {
    const desiredAncestors = new Map<string, string[]>();
    const visited = new Set<string>();
    const visit = (
      groupKey: string | null,
      parentId: string | null,
      ancestors: string[],
    ) => {
      const key = serverBranchKey(groupKey, parentId);
      if (visited.has(key)) return;
      visited.add(key);
      desiredAncestors.set(key, ancestors);
      for (const row of serverBranchData[key]?.rows ?? []) {
        if (row.direct_child_count > 0) {
          visit(groupKey, row.issue.id, [...ancestors, row.issue.id]);
        }
      }
    };

    if (usesServerGrouping) {
      for (const group of serverGroups) visit(group.key, null, []);
    } else {
      visit(null, null, []);
    }

    setServerBranchState((previous) => {
      if (previous.identity !== serverIdentity) return previous;
      let branches: Map<string, ServerBranch> | null = null;
      for (const [key, ancestors] of desiredAncestors) {
        const branch = previous.branches.get(key);
        if (!branch || sameStringArray(branch.ancestorIds, ancestors)) continue;
        branches ??= new Map(previous.branches);
        branches.set(key, { ...branch, ancestorIds: ancestors });
      }
      return branches ? { ...previous, branches } : previous;
    });
  }, [serverBranchData, serverGroups, serverIdentity, usesServerGrouping]);

  const activateServerBranch = useCallback(
    (
      groupKey: string | null,
      parentId: string | null,
      ancestorIds: string[],
    ) => {
      setServerBranchState((previous) => {
        if (previous.identity !== serverIdentity) return previous;
        const key = serverBranchKey(groupKey, parentId);
        const existing = previous.branches.get(key);
        if (existing && sameStringArray(existing.ancestorIds, ancestorIds)) {
          return previous;
        }
        const branches = new Map(previous.branches);
        branches.set(
          key,
          existing
            ? { ...existing, ancestorIds }
            : {
                key,
                groupKey,
                parentId,
                ancestorIds,
                cursors: [null],
              },
        );
        return { ...previous, branches };
      });
    },
    [serverIdentity],
  );

  // A broad Table invalidation refetches every active page. As soon as the
  // head starts refreshing, every later cursor is obsolete; discard them
  // before their concurrent responses can create a duplicate or missing row.
  // The revision check is a second line of defence for clients that restore a
  // refreshed head directly into the cache without exposing a fetching frame.
  const branchHeadRevisionRef = useRef<Record<string, number>>({});
  useEffect(() => {
    const previousRevisions = branchHeadRevisionRef.current;
    const nextRevisions: Record<string, number> = {};
    const branchesToTrim = new Set<string>();
    for (const [key, branch] of activeServerBranches) {
      const revision = serverBranchData[key]?.headUpdatedAt ?? 0;
      if (revision === 0) continue;
      nextRevisions[key] = revision;
      const seen = previousRevisions[key];
      if (
        branch.cursors.length > 1 &&
        (serverBranchData[key]?.headFetching ||
          (seen !== undefined && seen !== revision))
      ) {
        branchesToTrim.add(key);
      }
    }
    // Keep only the current query's active branches. This bounds the revision
    // bookkeeping across long sessions with many filter/search identities.
    branchHeadRevisionRef.current = nextRevisions;
    if (branchesToTrim.size === 0) return;

    setServerBranchState((previous) => {
      if (previous.identity !== serverIdentity) return previous;
      let branches: Map<string, ServerBranch> | null = null;
      for (const [key, branch] of previous.branches) {
        if (branchesToTrim.has(key)) {
          branches ??= new Map(previous.branches);
          branches.set(key, { ...branch, cursors: [null] });
        }
      }
      return branches ? { ...previous, branches } : previous;
    });
  }, [activeServerBranches, serverBranchData, serverIdentity]);

  const loadNextServerBranchPage = useCallback(
    (branchKey: string, cursor: string) => {
      setServerBranchState((previous) => {
        if (previous.identity !== serverIdentity) return previous;
        const branch = previous.branches.get(branchKey);
        if (!branch || branch.cursors.includes(cursor)) return previous;
        const branches = new Map(previous.branches);
        branches.set(branchKey, {
          ...branch,
          cursors: [...branch.cursors, cursor],
        });
        return { ...previous, branches };
      });
    },
    [serverIdentity],
  );

  const retryServerBranch = useCallback(
    (branchKey: string) => {
      const branch = activeServerBranches.get(branchKey);
      if (!branch) return;
      void queryClient.refetchQueries({
        queryKey: issueKeys.tableRows(
          wsId,
          serverQuery,
          serverGroupSpec,
          branch.groupKey,
          tableHierarchy,
          branch.parentId,
        ),
        exact: false,
        type: "active",
      });
    },
    [
      activeServerBranches,
      queryClient,
      serverGroupSpec,
      serverQuery,
      tableHierarchy,
      wsId,
    ],
  );

  const serverGroupLabel = useCallback(
    (descriptor: IssueTableGroupDescriptor) => {
      const value = descriptor.value;
      if (value.kind === "status") {
        if (ALL_STATUSES.includes(value.status as IssueStatus)) {
          return t(($) => $.status[value.status as IssueStatus]);
        }
        // Installed clients can receive a status introduced by a newer
        // backend. Keep the group usable instead of collapsing the response
        // to the schema fallback or rendering an empty label.
        return value.status;
      }
      if (value.kind === "assignee") {
        return value.actor
          ? getActorName(value.actor.type, value.actor.id)
          : t(($) => $.table.unassigned);
      }
      if (value.value_state === "unset") return t(($) => $.table.no_value);
      if (value.value_state === "unavailable") {
        return t(($) => $.table.value_unavailable);
      }
      const property = propertyById.get(value.property_id);
      if (typeof value.value === "boolean") {
        return value.value
          ? t(($) => $.pickers.custom_property.true_label)
          : t(($) => $.pickers.custom_property.false_label);
      }
      return (
        property?.config.options?.find((option) => option.id === value.value)
          ?.name ?? String(value.value ?? "")
      );
    },
    [getActorName, propertyById, t],
  );

  const serverDisplayRows = useMemo<IssueTableDisplayRow[]>(() => {
    const result: IssueTableDisplayRow[] = [];
    const seenIssueIds = new Set<string>();
    const appendBranch = (
      groupKey: string | null,
      parentId: string | null,
      depth: number,
      ancestorIds: string[],
    ) => {
      const key = serverBranchKey(groupKey, parentId);
      const data = serverBranchData[key];
      if (!data) {
        const registered = activeServerBranches.has(key);
        result.push({
          kind: "load_more",
          key: `${registered ? "loading" : "activate"}:${key}`,
          label: t(($) => $.table.loading_branch),
          loading: registered,
          autoLoad: !registered,
          onLoad: registered
            ? undefined
            : () => activateServerBranch(groupKey, parentId, ancestorIds),
        });
        return;
      }
      if (data.rows.length === 0 && data.loading) {
        result.push({
          kind: "load_more",
          key: `loading:${key}`,
          label: t(($) => $.table.loading_branch),
          loading: true,
        });
      }
      for (const row of data.rows) {
        // A realtime move can briefly leave the same entity in old and new
        // branch caches. Render the first authoritative position only; duplicate
        // ids otherwise create duplicate React keys and duplicate selection.
        if (seenIssueIds.has(row.issue.id)) continue;
        seenIssueIds.add(row.issue.id);
        const collapsed = collapsedParentSet.has(row.issue.id);
        result.push({
          kind: "issue",
          key: row.issue.id,
          issue: row.issue,
          depth,
          hasChildren: tableHierarchy && row.direct_child_count > 0,
          collapsed,
        });
        if (tableHierarchy && row.direct_child_count > 0 && !collapsed) {
          appendBranch(groupKey, row.issue.id, depth + 1, [
            ...ancestorIds,
            row.issue.id,
          ]);
        }
      }
      if (data.error) {
        result.push({
          kind: "load_more",
          key: `retry:${key}`,
          label: t(($) => $.table.load_more_failed_retry),
          loading: false,
          onLoad: () => retryServerBranch(key),
        });
      } else if (data.nextCursor) {
        const nextCursor = data.nextCursor;
        result.push({
          kind: "load_more",
          key: `more:${key}:${nextCursor}`,
          label: t(($) => $.table.load_more),
          loading: data.loading,
          autoLoad: true,
          onLoad: () => loadNextServerBranchPage(key, nextCursor),
        });
      }
    };

    if (usesServerGrouping) {
      for (const descriptor of serverGroups) {
        const collapsed = collapsedGroupSet.has(descriptor.key);
        result.push({
          kind: "group",
          key: descriptor.key,
          label: serverGroupLabel(descriptor),
          count: descriptor.count,
          collapsed,
        });
        if (!collapsed) appendBranch(descriptor.key, null, 0, []);
      }
    } else {
      appendBranch(null, null, 0, []);
    }
    if (
      usesServerGrouping &&
      serverGroups.length === 0 &&
      serverGroupsPending
    ) {
      result.push({
        kind: "load_more",
        key: "loading:groups",
        label: t(($) => $.table.loading_branch),
        loading: true,
      });
    } else if (usesServerGrouping && serverGroupsError) {
      result.push({
        kind: "load_more",
        key: "retry:groups",
        label: t(($) => $.table.load_failed_retry),
        loading: false,
        onLoad: () => void refetchServerGroups(),
      });
    } else if (usesServerGrouping && hasNextServerGroupPage) {
      result.push({
        kind: "load_more",
        key: "more:groups",
        label: t(($) => $.table.load_more),
        loading: fetchingNextServerGroupPage,
        autoLoad: true,
        onLoad: () => void fetchNextServerGroupPage(),
      });
    }
    return result;
  }, [
    collapsedGroupSet,
    collapsedParentSet,
    activeServerBranches,
    activateServerBranch,
    loadNextServerBranchPage,
    retryServerBranch,
    serverBranchData,
    serverGroupLabel,
    serverGroups,
    serverGroupsPending,
    serverGroupsError,
    hasNextServerGroupPage,
    fetchingNextServerGroupPage,
    refetchServerGroups,
    fetchNextServerGroupPage,
    t,
    tableHierarchy,
    usesServerGrouping,
  ]);

  const ungroupedRootData = serverBranchData[serverBranchKey(null, null)];
  const serverTotal = usesServerGrouping
    ? groupedServerTotal
    : (ungroupedRootData?.total ?? 0);

  const tableMembershipIdentity = useMemo(
    () =>
      JSON.stringify([
        serverQuery.scope,
        serverQuery.filters,
        serverQuery.search ?? "",
      ]),
    [serverQuery.filters, serverQuery.scope, serverQuery.search],
  );
  const authoritativeLoadedIssues = useMemo(() => {
    const byId = new Map<string, Issue>();
    for (const branch of Object.values(serverBranchData)) {
      // Previous-data placeholders keep the old table painted during a query
      // transition, but they are not members of the new filter/search window.
      if (branch.placeholder) continue;
      for (const row of branch.rows) byId.set(row.issue.id, row.issue);
    }
    return [...byId.values()];
  }, [serverBranchData]);
  const [loadedIssueState, setLoadedIssueState] = useState<LoadedIssueState>({
    membershipIdentity: tableMembershipIdentity,
    issues: new Map(),
  });
  useEffect(() => {
    setLoadedIssueState((previous) => {
      const reset = previous.membershipIdentity !== tableMembershipIdentity;
      const issues = reset
        ? new Map<string, Issue>()
        : new Map(previous.issues);
      let changed = reset;
      for (const issue of authoritativeLoadedIssues) {
        if (issues.get(issue.id) !== issue) {
          issues.set(issue.id, issue);
          changed = true;
        }
      }
      return changed
        ? { membershipIdentity: tableMembershipIdentity, issues }
        : previous;
    });
  }, [authoritativeLoadedIssues, tableMembershipIdentity]);
  const loadedIssues = useMemo(
    () =>
      loadedIssueState.membershipIdentity === tableMembershipIdentity
        ? [...loadedIssueState.issues.values()]
        : [],
    [
      loadedIssueState.issues,
      loadedIssueState.membershipIdentity,
      tableMembershipIdentity,
    ],
  );

  const visibleColumnConfigs = useMemo(
    () =>
      tableColumns.filter((column) => {
        const propertyId = propertyIdFromViewKey(column.key);
        return !propertyId || activePropertyIds.has(propertyId);
      }),
    [activePropertyIds, tableColumns],
  );

  const displayRows = serverDisplayRows;
  const visibleIssueIds = useMemo(
    () =>
      displayRows
        .filter((row): row is Extract<IssueTableDisplayRow, { kind: "issue" }> => row.kind === "issue")
        .map((row) => row.issue.id),
    [displayRows],
  );
  useEffect(() => {
    onLoadedIssuesChange(loadedIssues);
  }, [loadedIssues, onLoadedIssuesChange]);
  const selectedIssues = useMemo(
    () => loadedIssues.filter((issue) => selection.selectedIds.has(issue.id)),
    [loadedIssues, selection.selectedIds],
  );
  const handleIssueSelection = useCallback(
    (issueId: string, shiftKey: boolean) => {
      const range = shiftKey
        ? getIssueTableSelectionRange(
            visibleIssueIds,
            selectionAnchorRef.current,
            issueId,
          )
        : null;

      if (range) {
        if (selection.selectedIds.has(issueId)) selection.deselect(range);
        else selection.select(range);
        return;
      }

      selection.toggle(issueId);
      selectionAnchorRef.current = issueId;
    },
    [selection, visibleIssueIds],
  );

  useEffect(() => {
    if (selection.selectedIds.size === 0) selectionAnchorRef.current = null;
  }, [selection.selectedIds]);

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
        cell: ({ row }) => {
          if (row.original.kind !== "issue") return null;
          const issue = row.original.issue;
          return (
            <IssueCheckbox
              checked={selection.selectedIds.has(issue.id)}
              label={t(($) => $.table.select_issue, {
                identifier: issue.identifier,
              })}
              onToggle={(shiftKey) => handleIssueSelection(issue.id, shiftKey)}
            />
          );
        },
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
    [
      handleIssueSelection,
      makeColumn,
      properties,
      selection.selectedIds,
      t,
      visibleColumnConfigs,
      visibleIssueIds,
    ],
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

  const handleExport = async (mode: "all" | "selected") => {
    setExporting(mode);
    try {
      // Every lookup the file depends on is AWAITED here rather than read
      // from render-time hook state — a cold or errored query must fail the
      // export, not silently degrade it: with an unsettled catalog the
      // user's property columns vanish from the file, and with unsettled
      // directories every actor exports as "Unknown *" (round-2 review
      // P2#4). fetchQuery throws on failure, which lands in the catch below.
      const needsPropertyCatalog = tableColumns.some(
        (column) => propertyIdFromViewKey(column.key) !== null,
      );
      // fetchQuery bypasses the options' `select`, so unwrap the response.
      const exportProperties = needsPropertyCatalog
        ? (await queryClient.fetchQuery(propertyListOptions(wsId))).properties
        : [];
      const exportPropertyById = new Map(
        exportProperties.map((property) => [property.id, property]),
      );
      // Configured columns against the RESOLVED catalog — only a property
      // definition that is genuinely gone (archived/deleted) drops out.
      const csvColumns = tableColumns.filter((column) => {
        const propertyId = propertyIdFromViewKey(column.key);
        return !propertyId || exportPropertyById.has(propertyId);
      });
      const needsActors = csvColumns.some(
        (column) => column.key === "assignee" || column.key === "creator",
      );
      const [rows, exportLookups, exportActorName] = await Promise.all([
        mode === "all" ? exportIssues() : Promise.resolve(selectedIssues),
        resolveExportLookups({
          projects: csvColumns.some((column) => column.key === "project"),
          childProgress: csvColumns.some(
            (column) => column.key === "child_progress",
          ),
        }),
        needsActors
          ? Promise.all([
              queryClient.fetchQuery(memberListOptions(wsId)),
              queryClient.fetchQuery(agentListOptions(wsId)),
              queryClient.fetchQuery(squadListOptions(wsId)),
            ]).then(([members, agents, squads]) =>
              buildActorNameResolver({ members, agents, squads }),
            )
          : Promise.resolve(getActorName),
      ]);
      const headers = csvColumns.map((column) => {
        const propertyId = propertyIdFromViewKey(column.key);
        if (propertyId) return exportPropertyById.get(propertyId)?.name ?? "";
        return columnLabel(column.key);
      });
      const csvRows = rows.map((issue) =>
        csvColumns.map((column) => {
          const propertyId = propertyIdFromViewKey(column.key);
          if (propertyId) {
            const property = exportPropertyById.get(propertyId);
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
                ? exportActorName(issue.assignee_type, issue.assignee_id)
                : "";
            case "labels":
              return issue.labels?.map((label) => label.name).join(", ") ?? "";
            case "project":
              return issue.project_id
                ? exportLookups.projectMap.get(issue.project_id)?.title ?? ""
                : "";
            case "start_date":
            case "due_date":
              return issue[column.key] ?? "";
            case "created_at":
            case "updated_at":
              return issue[column.key];
            case "child_progress": {
              const progress = exportLookups.childProgressMap.get(issue.id);
              return progress ? `${progress.done}/${progress.total}` : "";
            }
            case "creator":
              return exportActorName(issue.creator_type, issue.creator_id);
          }
          return "";
        }),
      );
      const csv = buildIssueTableCsv(headers, csvRows);
      const blob = new Blob(["\uFEFF", csv], { type: "text/csv;charset=utf-8" });
      const url = URL.createObjectURL(blob);
      const anchor = document.createElement("a");
      anchor.href = url;
      const filenamePrefix = mode === "all" ? "issues" : "issues-selected";
      anchor.download = `${filenamePrefix}-${new Date().toISOString().slice(0, 10)}.csv`;
      anchor.click();
      URL.revokeObjectURL(url);
      toast.success(t(($) => $.table.export_success, { count: rows.length }));
    } catch (error) {
      toast.error(
        error instanceof Error &&
          !(error instanceof IssueTableExportIntegrityError) &&
          error.message
          ? error.message
          : t(($) => $.table.export_failed),
      );
    } finally {
      setExporting(null);
    }
  };

  const displayedTotal = serverTotal;
  const displayedLoaded = loadedIssues.length;

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex shrink-0 items-center gap-2 border-b px-3 py-1.5">
        <TableIssueSearch
          value={search}
          onChange={onSearchChange}
          placeholder={t(($) => $.table.search_placeholder)}
          clearLabel={t(($) => $.table.search_clear)}
        />
        <span className="mr-auto min-w-0 truncate text-xs text-muted-foreground">
          {t(($) => $.table.loaded_count, {
            count: displayedLoaded,
            total: displayedTotal,
          })}
        </span>
        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <Button
                variant="ghost"
                size="sm"
                className="h-7"
                disabled={exporting !== null}
              >
                {exporting ? (
                  <Loader2 className="size-3.5 animate-spin" />
                ) : (
                  <Download className="size-3.5" />
                )}
                {t(($) => $.table.export)}
                <ChevronDown className="size-3" />
              </Button>
            }
          />
          <DropdownMenuContent align="end" className="w-52">
            <DropdownMenuItem onClick={() => void handleExport("all")}>
              <Download className="size-3.5" />
              {t(($) => $.table.export_all)}
            </DropdownMenuItem>
            <DropdownMenuItem
              disabled={selectedIssues.length === 0}
              onClick={() => void handleExport("selected")}
            >
              <Download className="size-3.5" />
              {t(($) => $.table.export_selected, {
                count: selectedIssues.length,
              })}
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
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
            virtualizeRows
            emptyMessage={t(($) => $.table.empty)}
            onRowClick={(row) => {
              if (row.original.kind === "issue") {
                navigation.push(paths.issueDetail(row.original.issue.id));
              }
            }}
            renderRow={(row) => {
              if (row.original.kind === "group") {
                return (
                  <IssueTableGroupRow
                    group={row.original}
                    colSpan={table.getVisibleLeafColumns().length}
                    onToggle={() => toggleTableGroupCollapsed(row.original.key)}
                  />
                );
              }
              if (row.original.kind === "load_more") {
                const loadMoreRow = row.original;
                return (
                  <TableRow className="hover:bg-transparent">
                    <TableCell
                      colSpan={table.getVisibleLeafColumns().length}
                      className="relative h-9 px-4 py-1"
                    >
                      {loadMoreRow.autoLoad &&
                        loadMoreRow.onLoad &&
                        !loadMoreRow.loading && (
                        <InfiniteScrollSentinel
                          onVisible={loadMoreRow.onLoad}
                          loading={false}
                          rootMargin="240px"
                          className="absolute inset-y-0 left-0 w-px"
                        />
                      )}
                      <button
                        type="button"
                        disabled={loadMoreRow.loading || !loadMoreRow.onLoad}
                        onClick={(event) => {
                          event.stopPropagation();
                          loadMoreRow.onLoad?.();
                        }}
                        className="sticky left-4 flex items-center gap-2 text-xs text-muted-foreground enabled:hover:text-foreground disabled:cursor-default"
                      >
                        {loadMoreRow.loading && (
                          <Loader2 className="size-3.5 animate-spin" />
                        )}
                        {loadMoreRow.label}
                      </button>
                    </TableCell>
                  </TableRow>
                );
              }
              return null;
            }}
            className="min-h-0 flex-1"
          />
        </SortableContext>
      </DndContext>
    </div>
  );
}
