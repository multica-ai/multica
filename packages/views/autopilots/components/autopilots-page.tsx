"use client";

import { useEffect, useMemo, useState } from "react";
import {
  AlertCircle,
  Clock,
  Code,
  Pause,
  Plus,
  Webhook,
  Zap,
} from "@multica/ui/icons";
import { useQuery } from "@tanstack/react-query";
import { autopilotListOptions } from "@multica/core/autopilots/queries";
import {
  useAutopilotsViewStore,
  AUTOPILOT_SCOPES,
  type AutopilotColumnKey,
  type AutopilotScope,
  type AutopilotSortField,
} from "@multica/core/autopilots/stores";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { useActorName } from "@multica/core/workspace/hooks";
import type { Autopilot } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { WorkspaceWakeups } from "./workspace-wakeups";
import { useNavigation } from "../../navigation";
import { ActorAvatar } from "../../common/actor-avatar";
import { formatInTimeZone } from "../../common/format-in-time-zone";
import {
  CollectionCreateButton,
  CollectionPageHeader,
  CollectionPageState,
  CollectionResetButton,
  CollectionScopeItems,
} from "../../layout/collection-page";
import {
  CollectionDisplayMenu,
  CollectionEmptyValue,
  CollectionList,
  CollectionNameCell,
  type CollectionColumn,
  type CollectionSort,
} from "../../layout/collection-list";
import { AutopilotDialog } from "./autopilot-dialog";
import {
  AutopilotFilterMenu,
  actorFilterValue,
  countActiveFilterDimensions,
} from "./autopilot-list-toolbar";
import {
  AutopilotBatchToolbar,
  AutopilotRowActions,
} from "./autopilot-list-actions";
import { AUTOPILOT_TEMPLATES, type AutopilotTemplate } from "./autopilot-templates";
import { isWakeupsTab } from "./wakeups-tab";
import { useLocale, useT, useTimeAgo } from "../../i18n";

// Cells

function NameCell({ autopilot }: { autopilot: Autopilot }) {
  const { t } = useT("autopilots");
  return (
    <CollectionNameCell
      title={autopilot.title}
      badges={
        // Paused marker: in the "all" scope active and paused rows mix, so a
        // paused automation needs an inline signal.
        autopilot.status === "paused" ? (
          <span
            title={
              autopilot.pause_reason === "agent_runtime_required"
                ? t(($) => $.status.paused_runtime_required)
                : t(($) => $.status.paused)
            }
            className="flex shrink-0 items-center text-amber-500"
          >
            <Pause className="size-3" />
          </span>
        ) : null
      }
    />
  );
}

function ActorCell({ type, id, hoverCard = false }: { type: string; id: string; hoverCard?: boolean }) {
  const { getActorName } = useActorName();
  return (
    <>
      <ActorAvatar
        actorType={type}
        actorId={id}
        size="sm"
        enableHoverCard={hoverCard}
        showStatusDot={hoverCard}
      />
      <span className="min-w-0 truncate">{getActorName(type, id)}</span>
    </>
  );
}

const TRIGGER_ICONS: Record<string, typeof Zap> = {
  schedule: Clock,
  webhook: Webhook,
  api: Code,
};

function TriggerCell({ autopilot }: { autopilot: Autopilot }) {
  const { t } = useT("autopilots");
  const kinds = autopilot.trigger_kinds ?? [];
  if (kinds.length === 0) return <CollectionEmptyValue />;
  return (
    <span className="flex min-w-0 items-center gap-2">
      {kinds.map((kind) => {
        // Server-driven enum: unknown kinds get a generic icon + raw label.
        const Icon = TRIGGER_ICONS[kind] ?? Zap;
        const label =
          kind === "schedule" || kind === "webhook" || kind === "api"
            ? t(($) => $.trigger_kind[kind])
            : kind;
        return (
          <span key={kind} className="flex min-w-0 items-center gap-1">
            <Icon className="size-3 shrink-0" />
            <span className="truncate">{label}</span>
          </span>
        );
      })}
    </span>
  );
}

// Dot color per last run outcome. Server-driven enum — unknown values fall
// through to the neutral dot, never crash (API compatibility rule).
function runStatusDotClass(status: string | null | undefined): string {
  switch (status) {
    case "completed":
    case "issue_created":
      return "bg-emerald-500";
    case "failed":
      return "bg-red-500";
    case "skipped":
      return "bg-amber-500";
    case "running":
      return "bg-blue-500";
    default:
      return "bg-muted-foreground/40";
  }
}

function LastRunCell({ autopilot }: { autopilot: Autopilot }) {
  const { t } = useT("autopilots");
  const timeAgo = useTimeAgo();
  if (!autopilot.last_run_at) return <CollectionEmptyValue />;
  const status = autopilot.last_run_status;
  const knownStatus =
    status === "issue_created" ||
    status === "running" ||
    status === "completed" ||
    status === "failed" ||
    status === "skipped"
      ? status
      : null;
  return (
    <>
      <span
        title={
          knownStatus
            ? t(($) => $.run_status[knownStatus])
            : (status ?? undefined)
        }
        className={`size-1.5 shrink-0 rounded-full ${runStatusDotClass(status)}`}
      />
      <span className="tabular-nums">{timeAgo(autopilot.last_run_at)}</span>
    </>
  );
}

function NextRunCell({ autopilot }: { autopilot: Autopilot }) {
  const { i18n } = useT("autopilots");
  const next = autopilot.next_run_at;
  if (!next) return <CollectionEmptyValue />;
  return (
    <span className="truncate tabular-nums">
      {formatInTimeZone(next, undefined, i18n.language)}
    </span>
  );
}

function ModeCell({ autopilot }: { autopilot: Autopilot }) {
  const { t } = useT("autopilots");
  const mode = autopilot.execution_mode;
  const label =
    mode === "create_issue" || mode === "run_only"
      ? t(($) => $.execution_mode[mode])
      : mode;
  return <span className="truncate">{label}</span>;
}

// Page

export function AutopilotsPage() {
  const navigation = useNavigation();
  const wsId = useWorkspaceId();
  if (isWakeupsTab(navigation.searchParams)) {
    return (
      <div className="flex min-h-0 flex-1 flex-col">
        <WorkspaceWakeups key={wsId} />
      </div>
    );
  }
  return <AutopilotList />;
}

function AutopilotList() {
  const navigation = useNavigation();
  const { t } = useT("autopilots");
  const { t: tLayout } = useT("layout");
  const locale = useLocale();
  const wsId = useWorkspaceId();
  const wsPaths = useWorkspacePaths();
  const {
    data: autopilots = [],
    isLoading,
    error: listError,
    refetch: refetchList,
  } = useQuery({
    ...autopilotListOptions(wsId),
    enabled: !!wsId,
  });

  const [createOpen, setCreateOpen] = useState(false);
  const [selectedTemplate, setSelectedTemplate] =
    useState<AutopilotTemplate | null>(null);
  const [selectedIds, setSelectedIds] = useState<ReadonlySet<string>>(
    new Set(),
  );
  // Persisted scope may hold a retired value (e.g. "archived" from an older
  // build) — fall back to "all" instead of stranding the user on an
  // unreachable scope.
  const rawScope = useAutopilotsViewStore((s) => s.scope);
  const scope = AUTOPILOT_SCOPES.includes(rawScope) ? rawScope : "all";
  const setScope = useAutopilotsViewStore((s) => s.setScope);
  const sortField = useAutopilotsViewStore((s) => s.sortField);
  const sortDirection = useAutopilotsViewStore((s) => s.sortDirection);
  const hiddenColumns = useAutopilotsViewStore((s) => s.hiddenColumns);
  const filters = useAutopilotsViewStore((s) => s.filters);
  const handleSort = useAutopilotsViewStore((s) => s.toggleSort);
  const handleSortFieldSelect = useAutopilotsViewStore((s) => s.setSortField);
  const setSortDirection = useAutopilotsViewStore((s) => s.setSortDirection);
  const toggleColumn = useAutopilotsViewStore((s) => s.toggleColumn);
  const toggleFilter = useAutopilotsViewStore((s) => s.toggleFilter);
  const clearFilters = useAutopilotsViewStore((s) => s.clearFilters);

  const scopeLabels: Record<AutopilotScope, string> = {
    all: tLayout(($) => $.panels.autopilots.all),
    active: t(($) => $.status.active),
    paused: t(($) => $.status.paused),
  };

  // Scope counts come from the FULL set (filters never affect them — they
  // are stage inventories, not result counts). API-archived rows (no UI
  // flow creates them) are excluded everywhere.
  const scopeCounts = useMemo<Record<AutopilotScope, number>>(() => {
    let active = 0;
    let paused = 0;
    for (const a of autopilots) {
      if (a.status === "archived") continue;
      if (a.status === "paused") paused++;
      else active++;
    }
    return { all: active + paused, active, paused };
  }, [autopilots]);

  // Rows within the current scope, unfiltered — the filter menu's option
  // lists and counts derive from this.
  const scopeRows = useMemo<Autopilot[]>(() => {
    if (scope === "all") {
      return autopilots.filter((a) => a.status !== "archived");
    }
    return autopilots.filter((a) => a.status === scope);
  }, [autopilots, scope]);

  const narrowed = countActiveFilterDimensions(filters) > 0;

  // Visible rows: filters, then sort.
  const rows = useMemo<Autopilot[]>(() => {
    const filtered = scopeRows.filter((a) => {
      if (
        filters.assignees.length > 0 &&
        !filters.assignees.includes(
          actorFilterValue(a.assignee_type, a.assignee_id),
        )
      ) {
        return false;
      }
      if (
        filters.modes.length > 0 &&
        !filters.modes.includes(a.execution_mode)
      ) {
        return false;
      }
      if (
        filters.triggerKinds.length > 0 &&
        !(a.trigger_kinds ?? []).some((k) => filters.triggerKinds.includes(k))
      ) {
        return false;
      }
      if (
        filters.creators.length > 0 &&
        !filters.creators.includes(
          actorFilterValue(a.created_by_type, a.created_by_id),
        )
      ) {
        return false;
      }
      return true;
    });

    const dir = sortDirection === "asc" ? 1 : -1;
    filtered.sort((a, b) => {
      if (sortField === "name") {
        return a.title.localeCompare(b.title) * dir;
      }
      if (sortField === "nextRun") {
        // Missing next run sorts last regardless of direction.
        const av = a.next_run_at ? Date.parse(a.next_run_at) : null;
        const bv = b.next_run_at ? Date.parse(b.next_run_at) : null;
        if (av === null && bv === null) return a.title.localeCompare(b.title);
        if (av === null) return 1;
        if (bv === null) return -1;
        return (av - bv) * dir;
      }
      if (sortField === "created") {
        return (Date.parse(a.created_at) - Date.parse(b.created_at)) * dir;
      }
      // lastRun: never-ran rows sort as oldest.
      const av = a.last_run_at ? Date.parse(a.last_run_at) : 0;
      const bv = b.last_run_at ? Date.parse(b.last_run_at) : 0;
      return (av - bv) * dir || a.title.localeCompare(b.title);
    });
    return filtered;
  }, [scopeRows, filters, sortField, sortDirection]);

  const columns = useMemo<CollectionColumn<Autopilot>[]>(
    () => [
      {
        key: "name",
        header: t(($) => $.page.table.name),
        sortKey: "name",
        cell: (a) => <NameCell autopilot={a} />,
      },
      {
        key: "assignee",
        header: t(($) => $.page.table.assignee),
        width: 144,
        core: true,
        hideable: true,
        cell: (a) => (
          <ActorCell
            type={a.assignee_type}
            id={a.assignee_id}
            hoverCard={a.assignee_type === "agent"}
          />
        ),
      },
      {
        key: "trigger",
        header: t(($) => $.page.table.trigger),
        width: 144,
        hideable: true,
        cell: (a) => <TriggerCell autopilot={a} />,
      },
      {
        key: "lastRun",
        header: t(($) => $.page.table.last_run),
        width: 112,
        sortKey: "lastRun",
        hideable: true,
        cell: (a) => <LastRunCell autopilot={a} />,
      },
      {
        key: "nextRun",
        header: t(($) => $.page.table.next_run),
        width: 128,
        sortKey: "nextRun",
        hideable: true,
        cell: (a) => <NextRunCell autopilot={a} />,
      },
      {
        key: "mode",
        header: t(($) => $.page.table.mode),
        width: 104,
        hideable: true,
        cell: (a) => <ModeCell autopilot={a} />,
      },
      {
        key: "creator",
        header: t(($) => $.page.table.created_by),
        width: 144,
        hideable: true,
        cell: (a) => <ActorCell type={a.created_by_type} id={a.created_by_id} />,
      },
      {
        key: "created",
        header: t(($) => $.page.table.created),
        width: 104,
        sortKey: "created",
        hideable: true,
        cell: (a) => (
          <span className="tabular-nums">
            {new Date(a.created_at).toLocaleDateString(locale)}
          </span>
        ),
      },
    ],
    [t, locale],
  );

  const sort: CollectionSort = {
    field: sortField,
    direction: sortDirection,
    onToggle: (field) => handleSort(field as AutopilotSortField),
    onFieldChange: (field) => handleSortFieldSelect(field as AutopilotSortField),
    onDirectionChange: setSortDirection,
  };

  const openCreate = (template?: AutopilotTemplate) => {
    setSelectedTemplate(template ?? null);
    setCreateOpen(true);
  };

  // The Autopilots panel links here with `?create` or `?template=<id>`.
  const createParam = navigation.searchParams.get("create");
  const templateParam = navigation.searchParams.get("template");
  useEffect(() => {
    if (createParam === null && templateParam === null) return;
    setSelectedTemplate(AUTOPILOT_TEMPLATES.find((tpl) => tpl.id === templateParam) ?? null);
    setCreateOpen(true);
    const params = new URLSearchParams(navigation.searchParams);
    params.delete("create");
    params.delete("template");
    navigation.replace(`${navigation.pathname}${params.size ? `?${params}` : ""}${navigation.hash}`);
    // eslint-disable-next-line react-hooks/exhaustive-deps -- consume the intent once per URL change
  }, [createParam, templateParam]);

  const selectedRows = rows.filter((a) => selectedIds.has(a.id));

  const totalCount = autopilots.length;
  const showEmpty = !isLoading && !listError && totalCount === 0;

  return (
    // relative: positioning anchor for the batch toolbar (page-centered,
    // not viewport-centered).
    <div className="relative flex min-h-0 flex-1 flex-col">
      <CollectionPageHeader
        icon={Zap}
        title={scopeLabels[scope]}
        menu={
          <CollectionScopeItems
            value={scope}
            onChange={setScope}
            options={AUTOPILOT_SCOPES.map((value) => ({
              value,
              label: scopeLabels[value],
              count: scopeCounts[value],
            }))}
          />
        }
        tools={
          <>
            <AutopilotFilterMenu
              filters={filters}
              onToggleFilter={toggleFilter}
              allRows={scopeRows}
            />
            {narrowed && <CollectionResetButton onClick={clearFilters} />}
          </>
        }
        actions={
          <>
            <CollectionDisplayMenu
              columns={columns}
              hiddenColumns={hiddenColumns}
              onToggleColumn={(key) => toggleColumn(key as AutopilotColumnKey)}
              sort={sort}
            />
            <CollectionCreateButton
              label={t(($) => $.page.new_autopilot)}
              onClick={() => openCreate()}
            />
          </>
        }
      />

      {listError ? (
        <CollectionPageState
          role="alert"
          tone="destructive"
          icon={AlertCircle}
          title={
            listError instanceof Error ? listError.message : String(listError)
          }
          actions={
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={() => refetchList()}
            >
              {t(($) => $.page.retry)}
            </Button>
          }
        />
      ) : showEmpty ? (
        <div className="flex flex-col items-center px-5 py-16">
          <Zap className="mb-3 h-10 w-10 text-faint-foreground" />
          <p className="text-body text-muted-foreground">
            {t(($) => $.page.empty.title)}
          </p>
          <p className="mb-6 mt-1 text-caption text-muted-foreground">
            {t(($) => $.page.empty.hint)}
          </p>
          <div className="grid w-full max-w-3xl grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
            {AUTOPILOT_TEMPLATES.map((tpl) => {
              const Icon = tpl.icon;
              return (
                <button
                  key={tpl.id}
                  type="button"
                  className="flex items-start gap-3 rounded-lg border p-3 text-left transition-colors hover:bg-accent/40"
                  onClick={() => openCreate(tpl)}
                >
                  <Icon className="mt-0.5 h-5 w-5 shrink-0 text-muted-foreground" />
                  <div className="min-w-0">
                    <div className="text-body font-medium">
                      {t(($) => $.templates[tpl.id].title)}
                    </div>
                    <div className="mt-0.5 line-clamp-2 text-caption text-muted-foreground">
                      {t(($) => $.templates[tpl.id].summary)}
                    </div>
                  </div>
                </button>
              );
            })}
          </div>
          <Button
            size="sm"
            variant="outline"
            className="mt-4"
            onClick={() => openCreate()}
          >
            <Plus className="mr-1 h-3.5 w-3.5" />
            {t(($) => $.page.start_blank)}
          </Button>
        </div>
      ) : (
        <CollectionList
          label={scopeLabels[scope]}
          columns={columns}
          hiddenColumns={hiddenColumns}
          rows={rows}
          loading={isLoading}
          getRowId={(a) => a.id}
          getRowHref={(a) => wsPaths.autopilotDetail(a.id)}
          getRowTitle={(a) => a.title}
          rowMenu={(a) => <AutopilotRowActions row={a} />}
          sort={sort}
          selection={{ selectedIds, onChange: setSelectedIds }}
          empty={t(($) => $.page.no_matches)}
        />
      )}

      <AutopilotBatchToolbar
        rows={selectedRows}
        onClear={() => setSelectedIds(new Set())}
      />

      {createOpen && (
        <AutopilotDialog
          mode="create"
          open={createOpen}
          onOpenChange={setCreateOpen}
          initial={
            selectedTemplate
              ? {
                  // Template title pulls from i18n so the user-visible default
                  // matches their locale, while the prompt body stays raw EN
                  // since it's injected directly into the agent task.
                  title: t(($) => $.templates[selectedTemplate.id].title),
                  description: selectedTemplate.prompt,
                }
              : undefined
          }
          initialSchedule={
            selectedTemplate ? selectedTemplate.schedule : undefined
          }
        />
      )}
    </div>
  );
}
