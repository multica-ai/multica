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
  type CellContext,
  type ColumnDef,
  type ColumnSizingState,
  type HeaderContext,
  type OnChangeFn,
  type Table as TanstackTable,
  type TableMeta,
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
  Pencil,
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
  TableFooter,
  TableRow,
} from "@multica/ui/components/ui/table";
import { cn } from "@multica/ui/lib/utils";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  TABLE_SYSTEM_COLUMNS,
  propertyIdFromViewKey,
  type SortField,
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
  Project,
  UpdateIssueRequest,
} from "@multica/core/types";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { ActorAvatar } from "../../common/actor-avatar";
import { LabelChip } from "../../labels/label-chip";
import { useNavigation } from "../../navigation";
import { ProjectPicker } from "../../projects/components/project-picker";
import { useT } from "../../i18n";
import { useIssueSurfaceActionsOptional } from "../surface/actions-context";
import { useIssueSurfaceSelection } from "../surface/selection-context";
import type { IssueCreateDefaults } from "../surface/types";
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
  TABLE_STRUCTURE_MAX_WINDOW,
  buildIssueTableCsv,
  buildIssueTableRows,
  getIssueTableSelectionRange,
  isTableStructureSuspended,
  refreshFrozenTableRows,
  shouldAutoLoadNextWindowPage,
  type IssueTableDisplayRow,
} from "./table-view-model";
import type { ChildProgress } from "./list-row";
import { InfiniteScrollSentinel } from "./infinite-scroll-sentinel";

const SELECT_COLUMN_ID = "__select";
const ADD_COLUMN_ID = "__add";

type TableViewProps = {
  issues: Issue[];
  childProgressMap: Map<string, ChildProgress>;
  fetchNextPage: () => Promise<unknown>;
  hasNextPage: boolean;
  isFetchingNextPage: boolean;
  /** The window query is in error state — page auto-advance (structure loop
   *  AND scroll sentinel) must stop and hand control to the explicit Retry. */
  windowError: boolean;
  total: number;
  search: string;
  onSearchChange: (query: string) => void;
  onCreateIssue: (defaults: IssueCreateDefaults) => void;
  exportIssues: () => Promise<Issue[]>;
  resolveExportLookups: (needs: {
    projects: boolean;
    childProgress: boolean;
  }) => Promise<{
    projectMap: Map<string, Project>;
    childProgressMap: Map<string, ChildProgress>;
  }>;
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
  editing,
  onEditingChange,
  onUpdate,
  onOpen,
  onCreateSubIssue,
  onToggleParent,
  toggleLabel,
  renameLabel,
  createSubIssueLabel,
}: {
  row: Extract<IssueTableDisplayRow, { kind: "issue" }>;
  /** Rename state is owned by the table (one editor at a time) so it also
   *  survives cell remounts and drives the structure freeze. */
  editing: boolean;
  onEditingChange: (editing: boolean) => void;
  onUpdate: (updates: Partial<UpdateIssueRequest>) => void;
  /** Navigate to the issue — clicking the title is the primary way IN. */
  onOpen: () => void;
  onCreateSubIssue: () => void;
  onToggleParent: () => void;
  toggleLabel: string;
  renameLabel: string;
  createSubIssueLabel: string;
}) {
  const [draft, setDraft] = useState(row.issue.title);
  const editingRef = useRef(editing);
  editingRef.current = editing;
  // True between the mousedown and the click of ONE gesture when that gesture
  // began while the rename input was up. onBlur commits and flips `editing`
  // off synchronously, before the click that caused the blur lands — so a
  // guard keyed only on the current `editing` value is already gone by click
  // time, and the commit-click bubbles into row navigation (and could hit the
  // title's own open handler): clicking away to save a rename would also open
  // the issue (MUL-5108 review R1#2).
  const gestureStartedWhileEditingRef = useRef(false);

  useEffect(() => {
    // Realtime/cache snapshots should refresh the passive label, but must not
    // overwrite text the user is actively composing.
    if (!editingRef.current) setDraft(row.issue.title);
  }, [row.issue.title]);

  const commit = () => {
    const title = draft.trim();
    onEditingChange(false);
    if (title && title !== row.issue.title) onUpdate({ title });
    else setDraft(row.issue.title);
  };

  return (
    <div
      className="flex min-w-0 items-center gap-1.5"
      style={{ paddingLeft: row.depth * 18 }}
      // Record whether the gesture began while editing (mousedown fires before
      // the blur that commits), then swallow that click in the capture phase —
      // before it can reach the row (navigation) or the title's open handler.
      // A gesture that began while NOT editing passes through untouched, so
      // clicking dead space still opens the issue.
      onMouseDownCapture={() => {
        gestureStartedWhileEditingRef.current = editingRef.current;
      }}
      onClickCapture={(event) => {
        if (editing || gestureStartedWhileEditingRef.current) {
          event.stopPropagation();
        }
        gestureStartedWhileEditingRef.current = false;
      }}
    >
      {row.hasChildren ? (
        <button
          type="button"
          aria-label={toggleLabel}
          className="rounded p-0.5 text-muted-foreground hover:bg-accent"
          onClick={(event) => {
            event.stopPropagation();
            onToggleParent();
          }}
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
              onEditingChange(false);
            }
          }}
          className="h-7 min-w-0 flex-1 px-2"
        />
      ) : (
        <>
          <button
            type="button"
            className="min-w-0 flex-1 truncate text-left hover:underline"
            onClick={(event) => {
              event.stopPropagation();
              onOpen();
            }}
          >
            {row.issue.title}
          </button>
          <button
            type="button"
            aria-label={createSubIssueLabel}
            className="shrink-0 rounded p-1 text-muted-foreground/60 opacity-0 hover:bg-accent hover:text-foreground focus-visible:opacity-100 group-hover:opacity-100"
            onClick={(event) => {
              event.stopPropagation();
              onCreateSubIssue();
            }}
          >
            <Plus className="size-3" />
          </button>
          <button
            type="button"
            aria-label={renameLabel}
            className="shrink-0 rounded p-1 text-muted-foreground/60 opacity-0 hover:bg-accent hover:text-foreground focus-visible:opacity-100 group-hover:opacity-100"
            onClick={(event) => {
              event.stopPropagation();
              setDraft(row.issue.title);
              onEditingChange(true);
            }}
          >
            <Pencil className="size-3" />
          </button>
        </>
      )}
    </div>
  );
}

function LazyLabelCell({
  issue,
  open,
  onOpenChange,
}: {
  issue: Issue;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const { t } = useT("issues");
  const labels = issue.labels ?? [];
  if (open) {
    return (
      <div onClick={stopRowNavigation}>
        <LabelPicker
          issueId={issue.id}
          open
          onOpenChange={(next) => {
            if (!next) onOpenChange(false);
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
        onOpenChange(true);
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

/**
 * Render-time context for the module-level cell/header components below,
 * carried on `table.options.meta`. The renderers MUST be module-level
 * components with stable identities: TanStack's flexRender mounts a
 * function-typed `cell`/`header` as a React component, so a renderer closure
 * rebuilt when any lookup changed identity (childProgressMap on every
 * realtime refetch, propertyById, actor names…) was a NEW element type and
 * React remounted every cell — closing any open picker popup and dropping
 * in-progress drafts the moment workspace activity refreshed the window
 * (MUL-5108). Data flows through meta instead so the element types never
 * change.
 */
type TableViewMeta = {
  childProgressMap: Map<string, ChildProgress>;
  propertyById: Map<string, IssueProperty>;
  properties: IssueProperty[];
  visibleIssueIds: string[];
  /** `${row.key}:${column.id}` of the cell whose editor popup / rename input
   *  is open, or null. Owned by TableView so the open editor survives cell
   *  remounts and freezes the table structure while it is up. */
  editingCellKey: string | null;
  setEditingCellKey: (key: string | null) => void;
  updateIssue: (issueId: string, updates: Partial<UpdateIssueRequest>) => void;
  openIssue: (issueId: string) => void;
  createSubIssue: (issue: Issue) => void;
  toggleTableParentCollapsed: (issueId: string) => void;
  handleIssueSelection: (issueId: string, shiftKey: boolean) => void;
  getActorName: (actorType: string, actorId: string) => string;
  columnLabel: (key: TableColumnKey) => string;
  sortBy: SortField;
  sortDirection: "asc" | "desc";
  onSort: (field: SortField, direction: "asc" | "desc") => void;
  toggleTableColumn: (key: TableColumnKey) => void;
};

function getTableViewMeta(
  table: TanstackTable<IssueTableDisplayRow>,
): TableViewMeta {
  return table.options.meta as unknown as TableViewMeta;
}

/**
 * Release the hoisted editing key when the cell that owns it unmounts.
 *
 * Row virtualization (see data-table.tsx) unmounts a cell as its row scrolls
 * out of the rendered window. Base UI does NOT call onOpenChange(false) on
 * unmount, so without this the open picker's key — and the frozen row
 * structure keyed off it — would persist after the anchor row leaves the
 * viewport: the table would stay frozen, and scrolling the row back would
 * silently reopen the picker and discard any in-progress rename draft
 * (MUL-5108 review R1#3). Clearing the key iff this unmounting cell still owns
 * it thaws the structure and closes the editor.
 *
 * Live values are read through refs so the empty-dep cleanup always sees the
 * current key/setter. At initial mount a cell is never yet the active editor
 * (the editor is opened by a later interaction, which does not remount the
 * cell), so this never fires spuriously — including under StrictMode's
 * mount → unmount → mount probe, whose first cleanup sees `editingCellKey`
 * still unequal to this cell's key.
 */
export function useReleaseEditingCellOnUnmount(
  cellKey: string | null,
  editingCellKey: string | null,
  setEditingCellKey: (key: string | null) => void,
) {
  const editingCellKeyRef = useRef(editingCellKey);
  editingCellKeyRef.current = editingCellKey;
  const setEditingCellKeyRef = useRef(setEditingCellKey);
  setEditingCellKeyRef.current = setEditingCellKey;
  useEffect(() => {
    return () => {
      if (cellKey !== null && editingCellKeyRef.current === cellKey) {
        setEditingCellKeyRef.current(null);
      }
    };
  }, [cellKey]);
}

function IssueTableSelectHeader({
  table,
}: HeaderContext<IssueTableDisplayRow, unknown>) {
  const meta = getTableViewMeta(table);
  const { t } = useT("issues");
  return (
    <SelectAllCheckbox
      issueIds={meta.visibleIssueIds}
      label={t(($) => $.table.select_all)}
    />
  );
}

function IssueTableSelectCell({
  row,
  table,
}: CellContext<IssueTableDisplayRow, unknown>) {
  const meta = getTableViewMeta(table);
  const selection = useIssueSurfaceSelection();
  const { t } = useT("issues");
  if (row.original.kind !== "issue") return null;
  const issue = row.original.issue;
  return (
    <IssueCheckbox
      checked={selection.selectedIds.has(issue.id)}
      label={t(($) => $.table.select_issue, { identifier: issue.identifier })}
      onToggle={(shiftKey) => meta.handleIssueSelection(issue.id, shiftKey)}
    />
  );
}

function IssueTableAddColumnHeader({
  table,
}: HeaderContext<IssueTableDisplayRow, unknown>) {
  const meta = getTableViewMeta(table);
  const { t } = useT("issues");
  return (
    <TableColumnPicker
      properties={meta.properties}
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
  );
}

function IssueTableEmptyCell() {
  return null;
}

function IssueTableHeaderCell({
  column,
  table,
}: HeaderContext<IssueTableDisplayRow, unknown>) {
  const meta = getTableViewMeta(table);
  const { t } = useT("issues");
  const key = column.id as TableColumnKey;
  const propertyId = propertyIdFromViewKey(key);
  const property = propertyId ? meta.propertyById.get(propertyId) : undefined;
  const staticSort = propertyId
    ? property && !["multi_select", "checkbox"].includes(property.type)
      ? (`property:${propertyId}` as SortField)
      : undefined
    : SORTABLE_COLUMNS[key as TableSystemColumnKey];
  const label = meta.columnLabel(key);
  return (
    <SortableColumnHeader
      columnKey={key}
      label={label}
      sortField={staticSort}
      sortBy={meta.sortBy}
      sortDirection={meta.sortDirection}
      onSort={meta.onSort}
      onHide={key === "title" ? undefined : () => meta.toggleTableColumn(key)}
      ascendingLabel={t(($) => $.table.sort_ascending)}
      descendingLabel={t(($) => $.table.sort_descending)}
      hideLabel={t(($) => $.table.columns.hide)}
      reorderLabel={t(($) => $.table.columns.reorder, { column: label })}
    />
  );
}

function IssueTableBodyCell({
  row,
  column,
  table,
}: CellContext<IssueTableDisplayRow, unknown>) {
  const meta = getTableViewMeta(table);
  const { t, i18n } = useT("issues");
  // Computed (and the unmount responder registered) before the early return so
  // the hook order is stable across issue/group rows.
  const cellKey =
    row.original.kind === "issue" ? `${row.original.key}:${column.id}` : null;
  useReleaseEditingCellOnUnmount(
    cellKey,
    meta.editingCellKey,
    meta.setEditingCellKey,
  );
  if (row.original.kind !== "issue") return null;
  const issueRow = row.original;
  const issue = issueRow.issue;
  const key = column.id as TableColumnKey;
  const editorOpen = meta.editingCellKey === cellKey;
  const setEditorOpen = (open: boolean) =>
    meta.setEditingCellKey(open ? cellKey : null);
  const onUpdate = (updates: Partial<UpdateIssueRequest>) =>
    meta.updateIssue(issue.id, updates);

  const propertyId = propertyIdFromViewKey(key);
  if (propertyId) {
    const property = meta.propertyById.get(propertyId);
    if (!property) return null;
    return (
      <div onClick={stopRowNavigation}>
        <CustomPropertyValueEditor
          issue={issue}
          property={property}
          open={editorOpen}
          onOpenChange={setEditorOpen}
        />
      </div>
    );
  }
  switch (key) {
    case "title":
      return (
        <InlineTitle
          row={issueRow}
          editing={editorOpen}
          onEditingChange={setEditorOpen}
          onUpdate={onUpdate}
          onOpen={() => meta.openIssue(issue.id)}
          onCreateSubIssue={() => meta.createSubIssue(issue)}
          onToggleParent={() => meta.toggleTableParentCollapsed(issue.id)}
          toggleLabel={t(($) => $.table.toggle_sub_issues)}
          renameLabel={t(($) => $.table.rename_title)}
          createSubIssueLabel={t(($) => $.actions.create_sub_issue)}
        />
      );
    case "identifier":
      return (
        <span className="text-xs text-muted-foreground">{issue.identifier}</span>
      );
    case "status":
      return (
        <div onClick={stopRowNavigation}>
          <StatusPicker
            status={issue.status}
            onUpdate={onUpdate}
            align="start"
            open={editorOpen}
            onOpenChange={setEditorOpen}
          />
        </div>
      );
    case "priority":
      return (
        <div onClick={stopRowNavigation}>
          <PriorityPicker
            priority={issue.priority}
            onUpdate={onUpdate}
            align="start"
            open={editorOpen}
            onOpenChange={setEditorOpen}
          />
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
            open={editorOpen}
            onOpenChange={setEditorOpen}
          />
        </div>
      );
    case "labels":
      return (
        <LazyLabelCell
          issue={issue}
          open={editorOpen}
          onOpenChange={setEditorOpen}
        />
      );
    case "project":
      return (
        <div onClick={stopRowNavigation}>
          <ProjectPicker
            projectId={issue.project_id}
            onUpdate={onUpdate}
            open={editorOpen}
            onOpenChange={setEditorOpen}
            triggerRender={
              <button
                type="button"
                className="flex max-w-full items-center gap-1.5 rounded px-1 py-0.5 hover:bg-accent"
              />
            }
          />
        </div>
      );
    case "start_date":
      return (
        <div onClick={stopRowNavigation}>
          <StartDatePicker
            startDate={issue.start_date}
            onUpdate={onUpdate}
            open={editorOpen}
            onOpenChange={setEditorOpen}
          />
        </div>
      );
    case "due_date":
      return (
        <div onClick={stopRowNavigation}>
          <DueDatePicker
            dueDate={issue.due_date}
            onUpdate={onUpdate}
            open={editorOpen}
            onOpenChange={setEditorOpen}
          />
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
          }).format(new Date(issue[key]))}
        </span>
      );
    case "child_progress": {
      const progress = meta.childProgressMap.get(issue.id);
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
            {meta.getActorName(issue.creator_type, issue.creator_id)}
          </span>
        </span>
      );
  }
  return null;
}

export function TableView({
  issues,
  childProgressMap,
  fetchNextPage,
  hasNextPage,
  isFetchingNextPage,
  windowError,
  total,
  search,
  onSearchChange,
  onCreateIssue,
  exportIssues,
  resolveExportLookups,
}: TableViewProps) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const queryClient = useQueryClient();
  const navigation = useNavigation();
  const paths = useWorkspacePaths();
  const actions = useIssueSurfaceActionsOptional();
  const selection = useIssueSurfaceSelection();
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
  // The one cell whose editor (picker popup / rename input) is open — see
  // TableViewMeta.editingCellKey.
  const [editingCellKey, setEditingCellKey] = useState<string | null>(null);

  const groupingPropertyId = propertyIdFromViewKey(tableGrouping);
  const effectiveTableGrouping =
    groupingPropertyId && !activePropertyIds.has(groupingPropertyId)
      ? "none"
      : tableGrouping;

  // Grouping and hierarchy are whole-window statements, so the window must
  // be materialized for them — but only under an explicit ceiling: grouping
  // is persisted view state and hierarchy is on by default, so an unbounded
  // loop would re-download entire large workspaces on every visit (round-3
  // review P1#1). Below the ceiling the remaining pages load sequentially
  // (one per completed fetch — the toolbar's loaded-of-total line is the
  // visible progress, and flipping the toggles off stops the loop), which
  // also makes hierarchy apply without scrolling to the last page (round-3
  // P1#2). Above the ceiling both features suspend and the toolbar notice
  // explains why; the window keeps the one-page-per-scroll sentinel.
  // Advancement gates (error stop, hard loaded-count ceiling, fresh total)
  // live in shouldAutoLoadNextWindowPage — see its doc for the failure
  // modes each gate closes (round-4 review P1#1/P1#2).
  const structureSuspended = isTableStructureSuspended(total);
  const structureWanted = effectiveTableGrouping !== "none" || tableHierarchy;
  const structureGrouping = structureSuspended ? "none" : effectiveTableGrouping;
  const structureHierarchy = tableHierarchy && !structureSuspended;
  const loadedCount = issues.length;
  useEffect(() => {
    if (
      shouldAutoLoadNextWindowPage({
        windowWanted: structureWanted,
        total,
        loadedCount,
        hasNextPage,
        isFetchingNextPage,
        hasError: windowError,
      })
    ) {
      void fetchNextPage();
    }
  }, [
    fetchNextPage,
    hasNextPage,
    isFetchingNextPage,
    loadedCount,
    structureWanted,
    total,
    windowError,
  ]);

  const visibleColumnConfigs = useMemo(
    () =>
      tableColumns.filter((column) => {
        const propertyId = propertyIdFromViewKey(column.key);
        return !propertyId || activePropertyIds.has(propertyId);
      }),
    [activePropertyIds, tableColumns],
  );

  const liveDisplayRows = useMemo(
    () =>
      buildIssueTableRows(issues, {
        grouping: structureGrouping,
        properties,
        collapsedGroups: new Set(tableCollapsedGroups),
        collapsedParents: new Set(tableCollapsedParents),
        hierarchy: structureHierarchy,
        windowComplete: !hasNextPage,
        getActorName,
        getStatusLabel: (status) => t(($) => $.status[status]),
        noValueLabel: t(($) => $.table.no_value),
        unassignedLabel: t(($) => $.table.unassigned),
        trueLabel: t(($) => $.pickers.custom_property.true_label),
        falseLabel: t(($) => $.pickers.custom_property.false_label),
      }),
    [
      getActorName,
      hasNextPage,
      issues,
      properties,
      t,
      tableCollapsedGroups,
      tableCollapsedParents,
      structureGrouping,
      structureHierarchy,
    ],
  );

  // While a cell editor popup / rename input is open, hold the row structure
  // still: window materialization, the end-of-load hierarchy assembly, and
  // realtime refetches all rebuild the row list, and a reorder can move the
  // anchor row out of the virtualized render window — unmounting the cell and
  // closing the popup the user just opened (MUL-5108). The snapshot freezes
  // ORDER only; issue objects inside the rows keep tracking the live cache so
  // the open editor reflects optimistic updates. Live structure snaps back
  // the moment the editor closes. Ref writes happen during render on purpose:
  // the snapshot must be captured from the same render that flips `editing`
  // on, and both branches are idempotent under StrictMode double-render.
  const frozenRowsRef = useRef<IssueTableDisplayRow[] | null>(null);
  if (editingCellKey === null) frozenRowsRef.current = null;
  else if (frozenRowsRef.current === null)
    frozenRowsRef.current = liveDisplayRows;
  const frozenRows = frozenRowsRef.current;
  const issueById = useMemo(
    () => new Map(issues.map((issue) => [issue.id, issue])),
    [issues],
  );
  const displayRows = useMemo(
    () =>
      frozenRows && frozenRows !== liveDisplayRows
        ? refreshFrozenTableRows(frozenRows, issueById)
        : liveDisplayRows,
    [frozenRows, issueById, liveDisplayRows],
  );
  const visibleIssueIds = useMemo(
    () =>
      displayRows
        .filter((row): row is Extract<IssueTableDisplayRow, { kind: "issue" }> => row.kind === "issue")
        .map((row) => row.issue.id),
    [displayRows],
  );
  const selectedIssues = useMemo(
    () => issues.filter((issue) => selection.selectedIds.has(issue.id)),
    [issues, selection.selectedIds],
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

  const openIssue = useCallback(
    (issueId: string) => navigation.push(paths.issueDetail(issueId)),
    [navigation, paths],
  );

  const createSubIssue = useCallback(
    (issue: Issue) =>
      onCreateIssue({
        parent_issue_id: issue.id,
        parent_issue_identifier: issue.identifier,
        ...(issue.project_id ? { project_id: issue.project_id } : {}),
      }),
    [onCreateIssue],
  );

  const onSort = useCallback(
    (field: SortField, direction: "asc" | "desc") => {
      setSortBy(field);
      setSortDirection(direction);
    },
    [setSortBy, setSortDirection],
  );

  // Fresh object every render is fine — cells read it through
  // table.options.meta at render time. What must NOT change per render are
  // the column defs' component identities below.
  const viewMeta: TableViewMeta = {
    childProgressMap,
    propertyById,
    properties,
    visibleIssueIds,
    editingCellKey,
    setEditingCellKey,
    updateIssue,
    openIssue,
    createSubIssue,
    toggleTableParentCollapsed,
    handleIssueSelection,
    getActorName,
    columnLabel,
    sortBy,
    sortDirection,
    onSort,
    toggleTableColumn,
  };

  const columns = useMemo<ColumnDef<IssueTableDisplayRow>[]>(
    () => [
      {
        id: SELECT_COLUMN_ID,
        size: 44,
        minSize: 44,
        maxSize: 44,
        enableResizing: false,
        header: IssueTableSelectHeader,
        cell: IssueTableSelectCell,
      },
      ...visibleColumnConfigs.map((config): ColumnDef<IssueTableDisplayRow> => {
        const definition: ColumnDef<IssueTableDisplayRow> = {
          id: config.key,
          minSize: config.key === "title" ? 260 : 96,
          maxSize: 640,
          enableResizing: true,
          header: IssueTableHeaderCell,
          cell: IssueTableBodyCell,
        };
        if (config.width !== undefined) definition.size = config.width;
        return definition;
      }),
      {
        id: ADD_COLUMN_ID,
        size: 48,
        minSize: 48,
        maxSize: 48,
        enableResizing: false,
        header: IssueTableAddColumnHeader,
        cell: IssueTableEmptyCell,
      },
    ],
    [visibleColumnConfigs],
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
    meta: viewMeta as TableMeta<IssueTableDisplayRow>,
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
        error instanceof Error ? error.message : t(($) => $.table.export_failed),
      );
    } finally {
      setExporting(null);
    }
  };

  const footer = (
    <TableFooter className="border-0 bg-transparent">
      <TableRow className="h-px border-0 hover:bg-transparent">
        <TableCell
          colSpan={table.getVisibleLeafColumns().length}
          className="h-px p-0"
        >
          <InfiniteScrollSentinel
            onVisible={() => {
              // A failed window stops implicit loading too — otherwise every
              // visibility transition retries into the same failure. The
              // toolbar Retry is the explicit resume path.
              if (hasNextPage && !isFetchingNextPage && !windowError) {
                void fetchNextPage();
              }
            }}
            loading={false}
            rootMargin="320px"
            className="h-px w-px"
          />
        </TableCell>
      </TableRow>
    </TableFooter>
  );

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
          {t(($) => $.table.loaded_count, { count: issues.length, total })}
          {windowError && hasNextPage && (
            <button
              type="button"
              onClick={() => void fetchNextPage()}
              className="ml-2 text-destructive underline-offset-2 hover:underline"
            >
              {t(($) => $.table.load_more_failed_retry)}
            </button>
          )}
          {structureSuspended && structureWanted && (
            <span className="ml-2">
              {t(($) => $.table.structure_paused, {
                total,
                limit: TABLE_STRUCTURE_MAX_WINDOW,
              })}
            </span>
          )}
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
            footer={footer}
            virtualizeRows
            emptyMessage={t(($) => $.table.empty)}
            onRowClick={(row) => {
              if (row.original.kind === "issue") {
                openIssue(row.original.issue.id);
              }
            }}
            renderRow={(row) => {
              if (row.original.kind !== "group") return null;
              return (
                <IssueTableGroupRow
                  group={row.original}
                  colSpan={table.getVisibleLeafColumns().length}
                  onToggle={() => toggleTableGroupCollapsed(row.original.key)}
                />
              );
            }}
            className="min-h-0 flex-1"
          />
        </SortableContext>
      </DndContext>
    </div>
  );
}
