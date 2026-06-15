"use client";

import { useMemo, useRef, useState } from "react";
import {
  AlertCircle,
  AlertTriangle,
  BookOpen,
  Download,
  HardDrive,
  Lock,
  Pencil,
  Plus,
} from "lucide-react";
import type {
  Agent,
  AgentRuntime,
  MemberWithUser,
  Skill,
  SkillSummary,
} from "@multica/core/types";
import { useQuery } from "@tanstack/react-query";
import { useVirtualizer } from "@tanstack/react-virtual";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import {
  agentListOptions,
  memberListOptions,
  selectSkillAssignments,
  skillListOptions,
} from "@multica/core/workspace/queries";
import { runtimeListOptions } from "@multica/core/runtimes";
import { resolvePublicFileUrl } from "@multica/core/workspace/avatar-url";
import { Button } from "@multica/ui/components/ui/button";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import {
  LIST_GRID_BOTTOM_CLEARANCE,
  ListGrid,
  ListGridBody,
  ListGridCell,
  ListGridHeader,
  ListGridHeaderCell,
  ListGridRow,
  type ListGridSortDirection,
} from "@multica/ui/components/ui/list-grid";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import { ActorAvatar } from "@multica/ui/components/common/actor-avatar";
import { useNavigation, useRowLink } from "../../navigation";
import { PageHeader } from "../../layout/page-header";
import { canEditSkill } from "../hooks/use-can-edit-skill";
import { readOrigin, type OriginInfo } from "../lib/origin";
import { CreateSkillDialog } from "./create-skill-dialog";
import {
  useSkillsViewStore,
  DEFAULT_HIDDEN_COLUMNS,
  type SkillColumnKey,
  type SkillSortField,
} from "@multica/core/skills/stores";
import { SkillListToolbar } from "./skill-list-toolbar";
import {
  SkillBatchToolbar,
  SkillRowActions,
  type SkillActionsContext,
} from "./skill-list-actions";
import { useT, useTimeAgo } from "../../i18n";

// Column template — single source of truth for header, rows, and skeletons.
// Tracks: [edge 0.75rem] [checkbox 1rem] [name, only fr track]
// [usedBy] [source] [creator] [updated] [created] [kebab 1.75rem]
// [edge 0.75rem].
// Content cells carry a default px-2 from list-grid.tsx
// (structural columns opt out with px-0), so the narrow edge tracks plus
// cell padding land content 20px from the container edge. Non-core cells
// carry `hidden @2xl:flex`. The breakpoint queries the CONTAINER (the page
// wrapper is the `@container`), not the viewport, so sidebars and split
// panes are accounted for.
// Hideable tracks are DETERMINISTIC widths via CSS vars (no max-content):
// rows are virtualized, so with only the visible slice mounted a
// content-driven track would resize as different rows scrolled into view.
// Truncation moved from per-cell max-w caps to the tracks themselves.
// A user-hidden column zeroes its var (columnTrackVars), collapsing the
// track exactly like the old max-content placeholder did; the empty
// placeholder cell stays rendered to keep subgrid auto-placement intact.
//
// TWO-ZONE RESPONSIVENESS (replaces the retired per-tier breakpoints):
// - Container ≥ @2xl (672px): WYSIWYG — every user-enabled column renders.
//   The grid carries min-width = Σ(enabled tracks + gaps) so when the
//   enabled set outgrows the container the wrapper scrolls horizontally.
//   The scrollbar is the escape valve for an over-provisioned column set,
//   never the default experience; an enabled column must NEVER silently
//   vanish (that "dead toggle" bug shipped twice).
// - Container < @2xl (phones, slim split panes): static core set
//   (name + usedBy), no horizontal scroll, column toggles don't apply.
const GRID_COLS =
  "grid-cols-[0.75rem_1rem_minmax(120px,1fr)_var(--lgc-usedby)_1.75rem_0.75rem] " +
  "@2xl:grid-cols-[0.75rem_1rem_minmax(200px,1fr)_var(--lgc-usedby)_var(--lgc-source)_var(--lgc-creator)_var(--lgc-updated)_var(--lgc-created)_1.75rem_0.75rem]";

// h-12 rows. The virtualizer's fixed-size contract: every row renders at
// exactly this height, which is what lets it skip per-row measurement.
const ROW_HEIGHT = 48;

// Single source for hideable column widths: track vars and the grid's
// min-width derive from the same numbers.
const COLUMN_WIDTHS: Record<SkillColumnKey, number> = {
  usedBy: 144,
  source: 152,
  creator: 144,
  updated: 104,
  created: 104,
};

// Fixed tracks (edges 12+12, checkbox 16, name min 200, kebab 28) plus the
// 9 gap-x-3 gaps between the wide template's 10 tracks (zero-width tracks
// still carry gaps).
const FIXED_TRACKS_WIDTH = 268 + 9 * 12;

function columnTrackVars(
  isVisible: (key: SkillColumnKey) => boolean,
): React.CSSProperties {
  const width = (key: SkillColumnKey) =>
    isVisible(key) ? `${COLUMN_WIDTHS[key]}px` : "0px";
  const minWidth =
    FIXED_TRACKS_WIDTH +
    (Object.keys(COLUMN_WIDTHS) as SkillColumnKey[]).reduce(
      (sum, key) => sum + (isVisible(key) ? COLUMN_WIDTHS[key] : 0),
      0,
    );
  return {
    "--lgc-usedby": width("usedBy"),
    "--lgc-source": width("source"),
    "--lgc-creator": width("creator"),
    "--lgc-updated": width("updated"),
    "--lgc-created": width("created"),
    "--lgc-minw": `${minWidth}px`,
  } as React.CSSProperties;
}

// Sort/filter/column types and defaults live in the core view store
// (@multica/core/skills/stores/view-store) so the persisted state and the
// UI share one definition. Re-exported here for the toolbar's convenience.
export type SortField = SkillSortField;

export interface SkillRow {
  skill: SkillSummary;
  agents: Agent[];
  creator: MemberWithUser | null;
  runtime: AgentRuntime | null;
  originType: OriginInfo["type"];
  canEdit: boolean;
}

// ---------------------------------------------------------------------------
// Page header bar — uses shared PageHeader so the mobile sidebar trigger and
// h-12 chrome stay consistent with every other dashboard list page.
// ---------------------------------------------------------------------------

function PageHeaderBar({
  totalCount,
  onCreate,
}: {
  totalCount: number;
  onCreate: () => void;
}) {
  const { t } = useT("skills");
  return (
    <PageHeader className="justify-between px-5">
      <div className="flex items-center gap-2">
        <BookOpen className="h-4 w-4 text-muted-foreground" />
        <h1 className="text-sm font-medium">{t(($) => $.page.title)}</h1>
        {totalCount > 0 && (
          <span className="font-mono text-xs tabular-nums text-muted-foreground/70">
            {totalCount}
          </span>
        )}
        <p className="ml-2 hidden text-xs text-muted-foreground md:block">
          {t(($) => $.page.tagline)}{" "}
          <a
            href="https://multica.ai/docs/skills"
            target="_blank"
            rel="noopener noreferrer"
            className="underline decoration-muted-foreground/30 underline-offset-4 transition-colors hover:text-foreground"
          >
            {t(($) => $.page.learn_more)}
          </a>
        </p>
      </div>
      {/* Quiet chrome button (outline, icon-only below md) — primary is
          reserved for the empty state's single CTA. */}
      <Button
        type="button"
        size="sm"
        variant="outline"
        className="h-8 w-8 gap-1 px-0 md:w-auto md:px-2.5"
        aria-label={t(($) => $.page.new_skill)}
        onClick={onCreate}
      >
        <Plus className="h-3.5 w-3.5" />
        <span className="hidden md:inline">{t(($) => $.page.new_skill)}</span>
      </Button>
    </PageHeader>
  );
}

// ---------------------------------------------------------------------------
// Cells
// ---------------------------------------------------------------------------

// Hover-revealed multi-select checkbox. Same pattern as SkillPickerList:
// the shadcn Checkbox is presentational only (`pointer-events-none`, so the
// Base UI button can never swallow the click) and the wrapping <button> owns
// the toggle. It stops click propagation so toggling never triggers the
// row's whole-row navigation (see `useRowLink`) — no preventDefault needed,
// the row is a plain <div>, not an <a>.
function CheckboxCell({
  checked,
  onToggle,
}: {
  checked: boolean;
  onToggle: () => void;
}) {
  return (
    <ListGridCell className="justify-center px-0">
      <button
        type="button"
        aria-pressed={checked}
        onClick={(e) => {
          e.stopPropagation();
          onToggle();
        }}
        className={`-m-1.5 flex items-center p-1.5 ${
          checked ? "" : "opacity-0 transition-opacity group-hover/row:opacity-100"
        }`}
      >
        <Checkbox
          checked={checked}
          tabIndex={-1}
          className="pointer-events-none"
        />
      </button>
    </ListGridCell>
  );
}

function NameCell({ row }: { row: SkillRow }) {
  const { t } = useT("skills");
  const { skill, canEdit } = row;
  return (
    <ListGridCell className="gap-1.5">
      <span className="min-w-0 truncate text-sm font-medium">
        {skill.name}
      </span>
      {!canEdit && (
        <Tooltip>
          <TooltipTrigger
            render={
              <Lock className="h-3 w-3 shrink-0 text-muted-foreground/60" />
            }
          />
          <TooltipContent>{t(($) => $.table.lock_tooltip)}</TooltipContent>
        </Tooltip>
      )}
    </ListGridCell>
  );
}

function UsedByCell({ agents }: { agents: Agent[] }) {
  const { t } = useT("skills");
  if (agents.length === 0) {
    return (
      <ListGridCell>
        <span className="text-xs text-muted-foreground/70">
          {t(($) => $.table.unused)}
        </span>
      </ListGridCell>
    );
  }
  const soleAgent = agents.length === 1 ? agents[0] : undefined;
  if (soleAgent) {
    const agent = soleAgent;
    return (
      <ListGridCell className="gap-1.5">
        <ActorAvatar
          name={agent.name}
          initials={agent.name.slice(0, 2).toUpperCase()}
          avatarUrl={resolvePublicFileUrl(agent.avatar_url)}
          isAgent
          size={22}
        />
        <span className="min-w-0 truncate text-xs text-muted-foreground">
          {agent.name}
        </span>
      </ListGridCell>
    );
  }
  const visible = agents.slice(0, 3);
  const extra = agents.length - visible.length;
  return (
    <ListGridCell>
      <div className="flex items-center -space-x-1.5">
        {visible.map((a) => (
          <Tooltip key={a.id}>
            <TooltipTrigger
              render={
                <span className="inline-flex rounded-full ring-2 ring-background">
                  <ActorAvatar
                    name={a.name}
                    initials={a.name.slice(0, 2).toUpperCase()}
                    avatarUrl={resolvePublicFileUrl(a.avatar_url)}
                    isAgent
                    size={22}
                  />
                </span>
              }
            />
            <TooltipContent>{a.name}</TooltipContent>
          </Tooltip>
        ))}
        {extra > 0 && (
          <span className="inline-flex h-6 w-6 items-center justify-center rounded-full bg-muted text-xs font-medium text-muted-foreground ring-2 ring-background">
            +{extra}
          </span>
        )}
      </div>
    </ListGridCell>
  );
}

function SourceCell({
  skill,
  runtime,
}: {
  skill: SkillSummary;
  runtime: AgentRuntime | null;
}) {
  const { t } = useT("skills");
  const origin = readOrigin(skill);

  let icon = <Pencil className="h-3 w-3 shrink-0" />;
  let label: string = t(($) => $.table.source_manual);
  if (origin.type === "runtime_local") {
    icon = <HardDrive className="h-3 w-3 shrink-0" />;
    label = runtime
      ? t(($) => $.table.source_runtime_named, { name: runtime.name })
      : origin.provider
        ? t(($) => $.table.source_runtime_provider, {
            provider: origin.provider,
          })
        : t(($) => $.table.source_runtime_unknown);
  } else if (origin.type === "clawhub") {
    icon = <Download className="h-3 w-3 shrink-0" />;
    label = t(($) => $.table.source_clawhub);
  } else if (origin.type === "skills_sh") {
    icon = <Download className="h-3 w-3 shrink-0" />;
    label = t(($) => $.table.source_skills_sh);
  } else if (origin.type === "github") {
    icon = <Download className="h-3 w-3 shrink-0" />;
    label = t(($) => $.table.source_github);
  }

  return (
    <ListGridCell className="hidden gap-1.5 text-xs text-muted-foreground @2xl:flex">
      {icon}
      <span className="min-w-0 truncate">{label}</span>
    </ListGridCell>
  );
}

function CreatorCell({ creator }: { creator: MemberWithUser | null }) {
  return (
    <ListGridCell className="hidden gap-1.5 @2xl:flex">
      {creator && (
        <>
          <ActorAvatar
            name={creator.name}
            initials={creator.name.slice(0, 2).toUpperCase()}
            avatarUrl={resolvePublicFileUrl(creator.avatar_url)}
            size={22}
          />
          <span className="min-w-0 truncate text-xs text-muted-foreground">
            {creator.name}
          </span>
        </>
      )}
    </ListGridCell>
  );
}

// ---------------------------------------------------------------------------
// Empty state
// ---------------------------------------------------------------------------

function EmptyState({ onCreate }: { onCreate: () => void }) {
  const { t } = useT("skills");
  return (
    <div className="flex flex-1 flex-col items-center justify-center px-6 py-16 text-center">
      <div className="flex h-12 w-12 items-center justify-center rounded-full bg-muted">
        <BookOpen className="h-6 w-6 text-muted-foreground" />
      </div>
      <h2 className="mt-4 text-base font-semibold">{t(($) => $.page.empty.title)}</h2>
      <p className="mt-1 max-w-md text-sm text-muted-foreground">
        {t(($) => $.page.empty.description)}
      </p>
      <Button type="button" onClick={onCreate} size="sm" className="mt-5">
        <Plus className="h-3 w-3" />
        {t(($) => $.page.new_skill)}
      </Button>
    </div>
  );
}

// ---------------------------------------------------------------------------
// List
// ---------------------------------------------------------------------------

function SkillListHeader({
  sortField,
  sortDirection,
  onSort,
  allSelected,
  someSelected,
  onToggleAll,
  isColVisible,
}: {
  sortField: SortField;
  sortDirection: ListGridSortDirection;
  onSort: (field: SortField) => void;
  allSelected: boolean;
  someSelected: boolean;
  onToggleAll: () => void;
  isColVisible: (key: SkillColumnKey) => boolean;
}) {
  const { t } = useT("skills");
  const sorted = (field: SortField) =>
    sortField === field ? sortDirection : false;
  const anySelected = allSelected || someSelected;
  return (
    <ListGridHeader>
      {/* Tri-state select-all in the checkbox track. Same presentational
          Checkbox + interactive wrapper pattern as the row cells; revealed
          on header hover or whenever a selection exists. */}
      <div className="flex items-center justify-center">
        <button
          type="button"
          aria-pressed={allSelected}
          onClick={onToggleAll}
          className={`-m-1.5 flex items-center p-1.5 ${
            anySelected
              ? ""
              : "opacity-0 transition-opacity group-hover/header:opacity-100"
          }`}
        >
          <Checkbox
            checked={allSelected}
            indeterminate={someSelected && !allSelected}
            tabIndex={-1}
            className="pointer-events-none"
          />
        </button>
      </div>
      <ListGridHeaderCell sorted={sorted("name")} onSort={() => onSort("name")}>
        {t(($) => $.table.name)}
      </ListGridHeaderCell>
      {isColVisible("usedBy") ? (
        <ListGridHeaderCell
          sorted={sorted("usedBy")}
          onSort={() => onSort("usedBy")}
        >
          {t(($) => $.table.used_by)}
        </ListGridHeaderCell>
      ) : (
        <ListGridHeaderCell className="px-0" />
      )}
      {isColVisible("source") ? (
        <ListGridHeaderCell className="hidden @2xl:flex">
          {t(($) => $.table.source)}
        </ListGridHeaderCell>
      ) : (
        <ListGridHeaderCell className="hidden px-0 @2xl:flex" />
      )}
      {isColVisible("creator") ? (
        <ListGridHeaderCell className="hidden @2xl:flex">
          {t(($) => $.table.created_by)}
        </ListGridHeaderCell>
      ) : (
        <ListGridHeaderCell className="hidden px-0 @2xl:flex" />
      )}
      {isColVisible("updated") ? (
        <ListGridHeaderCell
          className="hidden @2xl:flex"
          sorted={sorted("updated")}
          onSort={() => onSort("updated")}
        >
          {t(($) => $.table.updated)}
        </ListGridHeaderCell>
      ) : (
        <ListGridHeaderCell className="hidden px-0 @2xl:flex" />
      )}
      {isColVisible("created") ? (
        <ListGridHeaderCell
          className="hidden @2xl:flex"
          sorted={sorted("created")}
          onSort={() => onSort("created")}
        >
          {t(($) => $.table.created)}
        </ListGridHeaderCell>
      ) : (
        <ListGridHeaderCell className="hidden px-0 @2xl:flex" />
      )}
      <span aria-hidden="true" />
    </ListGridHeader>
  );
}

function LoadingSkeleton() {
  return (
    <ListGrid
      className={GRID_COLS}
      style={columnTrackVars((key) => !DEFAULT_HIDDEN_COLUMNS.includes(key))}
    >
      <ListGridHeader>
        <span aria-hidden="true" />
        <ListGridHeaderCell>
          <Skeleton className="h-3 w-12" />
        </ListGridHeaderCell>
        <ListGridHeaderCell>
          <Skeleton className="h-3 w-14" />
        </ListGridHeaderCell>
        {/* Source and created are hidden by default — keep their tracks
            mapped with empty placeholders so the skeleton matches the
            default layout. */}
        <ListGridHeaderCell className="hidden px-0 @2xl:flex" />
        <ListGridHeaderCell className="hidden @2xl:flex">
          <Skeleton className="h-3 w-10" />
        </ListGridHeaderCell>
        <ListGridHeaderCell className="hidden @2xl:flex">
          <Skeleton className="h-3 w-12" />
        </ListGridHeaderCell>
        <ListGridHeaderCell className="hidden px-0 @2xl:flex" />
        <span aria-hidden="true" />
      </ListGridHeader>
      {Array.from({ length: 5 }).map((_, i) => (
        <ListGridRow key={i} className="hover:bg-transparent">
          <span aria-hidden="true" />
          <ListGridCell>
            <Skeleton className="h-3.5 w-40 max-w-full" />
          </ListGridCell>
          <ListGridCell>
            <Skeleton className="h-5 w-14" />
          </ListGridCell>
          <ListGridCell className="hidden px-0 @2xl:flex" />
          <ListGridCell className="hidden gap-1.5 @2xl:flex">
            <Skeleton className="size-5 rounded-full" />
            <Skeleton className="h-3 w-12" />
          </ListGridCell>
          <ListGridCell className="hidden @2xl:flex">
            <Skeleton className="h-3 w-10" />
          </ListGridCell>
          <ListGridCell className="hidden px-0 @2xl:flex" />
          <span aria-hidden="true" />
        </ListGridRow>
      ))}
    </ListGrid>
  );
}

// ---------------------------------------------------------------------------
// Page
// ---------------------------------------------------------------------------

export default function SkillsPage() {
  const { t } = useT("skills");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const navigation = useNavigation();
  const rowLink = useRowLink();
  const timeAgo = useTimeAgo();
  const currentUserId = useAuthStore((s) => s.user?.id ?? null);

  const {
    data: skills = [],
    isLoading,
    error: listError,
    refetch: refetchList,
  } = useQuery(skillListOptions(wsId));
  const { data: agents = [], error: agentsError } = useQuery(
    agentListOptions(wsId),
  );
  const { data: members = [], error: membersError } = useQuery(
    memberListOptions(wsId),
  );
  const { data: runtimes = [], error: runtimesError } = useQuery(
    runtimeListOptions(wsId),
  );

  const [createOpen, setCreateOpen] = useState(false);
  const [selectedIds, setSelectedIds] = useState<ReadonlySet<string>>(
    new Set(),
  );
  const [search, setSearch] = useState("");

  // Persisted view preferences (per workspace, per user/device). Header sort
  // buttons and the toolbar's display panel mutate the SAME store, so both
  // surfaces always agree. Search and selection stay session-local on
  // purpose.
  const sortField = useSkillsViewStore((s) => s.sortField);
  const sortDirection = useSkillsViewStore((s) => s.sortDirection);
  const hiddenColumns = useSkillsViewStore((s) => s.hiddenColumns);
  const filters = useSkillsViewStore((s) => s.filters);
  const handleSort = useSkillsViewStore((s) => s.toggleSort);
  const handleSortFieldSelect = useSkillsViewStore((s) => s.setSortField);
  const setSortDirection = useSkillsViewStore((s) => s.setSortDirection);
  const toggleColumn = useSkillsViewStore((s) => s.toggleColumn);
  const toggleFilter = useSkillsViewStore((s) => s.toggleFilter);
  const clearFilters = useSkillsViewStore((s) => s.clearFilters);

  const isColVisible = (key: SkillColumnKey) => !hiddenColumns.includes(key);

  const toggleSelected = (id: string) => {
    setSelectedIds((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };

  const assignments = useMemo(() => selectSkillAssignments(agents), [agents]);

  const membersById = useMemo(() => {
    const map = new Map<string, MemberWithUser>();
    for (const m of members) map.set(m.user_id, m);
    return map;
  }, [members]);

  const runtimesById = useMemo(() => {
    const map = new Map<string, AgentRuntime>();
    for (const r of runtimes) map.set(r.id, r);
    return map;
  }, [runtimes]);

  const myRole =
    members.find((m: MemberWithUser) => m.user_id === currentUserId)?.role ??
    null;
  const isAdmin = myRole === "owner" || myRole === "admin";

  const actionsCtx: SkillActionsContext = {
    wsId,
    agents,
    currentUserId,
    isAdmin,
  };

  // Full assembled set — toolbar option lists and counts derive from this.
  const allRows = useMemo<SkillRow[]>(() => {
    return skills.map((skill) => {
      const origin = readOrigin(skill);
      const runtime =
        origin.type === "runtime_local" && origin.runtime_id
          ? runtimesById.get(origin.runtime_id) ?? null
          : null;
      return {
        skill,
        agents: assignments.get(skill.id) ?? [],
        creator: skill.created_by
          ? membersById.get(skill.created_by) ?? null
          : null,
        runtime,
        originType: origin.type,
        canEdit: canEditSkill(skill, { userId: currentUserId, role: myRole }),
      };
    });
  }, [skills, assignments, membersById, runtimesById, currentUserId, myRole]);

  // Visible rows: name search + filters, then sort.
  const rows = useMemo<SkillRow[]>(() => {
    const q = search.trim().toLowerCase();
    const filtered = allRows.filter((row) => {
      if (q && !row.skill.name.toLowerCase().includes(q)) return false;
      if (filters.usage.length > 0) {
        const usage = row.agents.length > 0 ? "used" : "unused";
        if (!filters.usage.includes(usage)) return false;
      }
      if (
        filters.origins.length > 0 &&
        !filters.origins.includes(row.originType)
      ) {
        return false;
      }
      if (
        filters.agents.length > 0 &&
        !row.agents.some((a) => filters.agents.includes(a.id))
      ) {
        return false;
      }
      if (
        filters.creators.length > 0 &&
        (!row.skill.created_by ||
          !filters.creators.includes(row.skill.created_by))
      ) {
        return false;
      }
      return true;
    });

    const dir = sortDirection === "asc" ? 1 : -1;
    filtered.sort((a, b) => {
      if (sortField === "name") {
        return a.skill.name.localeCompare(b.skill.name) * dir;
      }
      if (sortField === "usedBy") {
        return (
          (a.agents.length - b.agents.length) * dir ||
          a.skill.name.localeCompare(b.skill.name)
        );
      }
      if (sortField === "created") {
        return (
          (Date.parse(a.skill.created_at) - Date.parse(b.skill.created_at)) *
          dir
        );
      }
      return (
        (Date.parse(a.skill.updated_at) - Date.parse(b.skill.updated_at)) * dir
      );
    });
    return filtered;
  }, [allRows, search, filters, sortField, sortDirection]);

  // Row virtualization — Linear-style: the virtualizer only does the math
  // (visible index range + offsets); the DOM stays ours. Offsets become
  // padding on the rows wrapper, so the mounted rows remain direct subgrid
  // children and column alignment is untouched. Fixed ROW_HEIGHT rows mean
  // no per-row measurement. The scroll element is the SINGLE outer
  // scroller (both axes) — see ListGridBody's comment for why the split
  // scroll structure was retired.
  const listScrollRef = useRef<HTMLDivElement | null>(null);
  const rowVirtualizer = useVirtualizer({
    count: rows.length,
    getScrollElement: () => listScrollRef.current,
    estimateSize: () => ROW_HEIGHT,
    overscan: 10,
  });

  const handleCreated = (skill: Skill) => {
    navigation.push(paths.skillDetail(skill.id));
  };

  const selectedRows = rows.filter((row) => selectedIds.has(row.skill.id));
  const allSelected = rows.length > 0 && selectedRows.length === rows.length;
  const someSelected = selectedRows.length > 0 && !allSelected;
  const handleToggleAll = () => {
    setSelectedIds(
      allSelected ? new Set() : new Set(rows.map((r) => r.skill.id)),
    );
  };

  // --- List request error ---
  if (listError) {
    return (
      <div className="flex flex-1 min-h-0 flex-col">
        <PageHeaderBar totalCount={0} onCreate={() => setCreateOpen(true)} />
        <div className="flex flex-1 flex-col items-center justify-center gap-3 px-6 py-16 text-center">
          <AlertCircle className="h-8 w-8 text-destructive" />
          <div>
            <p className="text-sm font-medium">
              {t(($) => $.page.list_error.title)}
            </p>
            <p className="mt-1 text-xs text-muted-foreground">
              {listError instanceof Error
                ? listError.message
                : t(($) => $.page.list_error.fallback)}
            </p>
          </div>
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={() => refetchList()}
          >
            {t(($) => $.page.list_error.retry)}
          </Button>
        </div>
      </div>
    );
  }

  const totalCount = skills.length;
  const showEmpty = !isLoading && totalCount === 0;
  const supportingQueryDown =
    !!agentsError || !!membersError || !!runtimesError;

  // Unmounted rows above/below the visible slice become padding on the
  // scrolling body, exactly like Linear's --x-paddingTop/Bottom offsets.
  const virtualItems = rowVirtualizer.getVirtualItems();
  const firstVirtual = virtualItems[0];
  const lastVirtual = virtualItems[virtualItems.length - 1];
  const virtualPadding = {
    top: firstVirtual ? firstVirtual.start : 0,
    bottom: lastVirtual
      ? rowVirtualizer.getTotalSize() - lastVirtual.end
      : 0,
  };

  return (
    // relative: positioning anchor for the batch toolbar (page-centered,
    // not viewport-centered).
    <div className="relative flex flex-1 min-h-0 flex-col">
      <PageHeaderBar
        totalCount={totalCount}
        onCreate={() => setCreateOpen(true)}
      />

      {supportingQueryDown && (
        <div
          role="status"
          className="flex shrink-0 items-start gap-2 border-b bg-warning/10 px-6 py-2 text-xs text-muted-foreground"
        >
          <AlertTriangle className="mt-0.5 h-3.5 w-3.5 shrink-0 text-warning" />
          <span>{t(($) => $.page.supporting_data_warning)}</span>
        </div>
      )}

      {isLoading ? (
        <div className="flex-1 overflow-y-auto @container">
          <LoadingSkeleton />
        </div>
      ) : showEmpty ? (
        <div className="flex flex-1 items-center justify-center">
          <EmptyState onCreate={() => setCreateOpen(true)} />
        </div>
      ) : (
        <>
          <SkillListToolbar
            search={search}
            onSearchChange={setSearch}
            filters={filters}
            onToggleFilter={toggleFilter}
            onClearFilters={clearFilters}
            sortField={sortField}
            sortDirection={sortDirection}
            onSortFieldChange={handleSortFieldSelect}
            onSortDirectionChange={setSortDirection}
            hiddenColumns={hiddenColumns}
            onToggleColumn={toggleColumn}
            allRows={allRows}
            visibleCount={rows.length}
          />
          <div
            ref={listScrollRef}
            className="min-h-0 flex-1 overflow-auto @container"
          >
          <ListGrid
            className={`${GRID_COLS} @2xl:min-w-[var(--lgc-minw)]`}
            style={columnTrackVars(isColVisible)}
          >
            <SkillListHeader
              sortField={sortField}
              sortDirection={sortDirection}
              onSort={handleSort}
              allSelected={allSelected}
              someSelected={someSelected}
              onToggleAll={handleToggleAll}
              isColVisible={isColVisible}
            />
            <ListGridBody
              style={{
                paddingTop: virtualPadding.top,
                paddingBottom:
                  virtualPadding.bottom + LIST_GRID_BOTTOM_CLEARANCE,
              }}
            >
              {rows.length === 0 && (
                <div className="col-span-full py-16 text-center text-sm text-muted-foreground">
                  {t(($) => $.page.no_matches.title)}
                </div>
              )}
              {virtualItems.map((vi) => {
                const row = rows[vi.index];
                if (!row) return null;
                return (
              <ListGridRow
                key={row.skill.id}
                className={`cursor-pointer ${
                  selectedIds.has(row.skill.id) ? "bg-accent/30" : ""
                }`}
                {...rowLink(paths.skillDetail(row.skill.id))}
              >
                <CheckboxCell
                  checked={selectedIds.has(row.skill.id)}
                  onToggle={() => toggleSelected(row.skill.id)}
                />
                <NameCell row={row} />
                {isColVisible("usedBy") ? (
                  <UsedByCell agents={row.agents} />
                ) : (
                  <ListGridCell className="px-0" />
                )}
                {isColVisible("source") ? (
                  <SourceCell skill={row.skill} runtime={row.runtime} />
                ) : (
                  <ListGridCell className="hidden px-0 @2xl:flex" />
                )}
                {isColVisible("creator") ? (
                  <CreatorCell creator={row.creator} />
                ) : (
                  <ListGridCell className="hidden px-0 @2xl:flex" />
                )}
                {isColVisible("updated") ? (
                  <ListGridCell className="hidden whitespace-nowrap text-xs tabular-nums text-muted-foreground @2xl:flex">
                    {timeAgo(row.skill.updated_at)}
                  </ListGridCell>
                ) : (
                  <ListGridCell className="hidden px-0 @2xl:flex" />
                )}
                {isColVisible("created") ? (
                  <ListGridCell className="hidden whitespace-nowrap text-xs tabular-nums text-muted-foreground @2xl:flex">
                    {timeAgo(row.skill.created_at)}
                  </ListGridCell>
                ) : (
                  <ListGridCell className="hidden px-0 @2xl:flex" />
                )}
                <ListGridCell className="justify-end px-0">
                  <SkillRowActions row={row} ctx={actionsCtx} />
                </ListGridCell>
              </ListGridRow>
                );
              })}
            </ListGridBody>
          </ListGrid>
          </div>
        </>
      )}

      <SkillBatchToolbar
        rows={selectedRows}
        ctx={actionsCtx}
        onClear={() => setSelectedIds(new Set())}
      />

      {createOpen && (
        <CreateSkillDialog
          onClose={() => setCreateOpen(false)}
          onCreated={handleCreated}
        />
      )}
    </div>
  );
}
