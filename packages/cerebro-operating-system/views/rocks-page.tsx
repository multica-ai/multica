"use client";

import { useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { useWorkspaceId } from "@multica/core/hooks";
import { agentListOptions, memberListOptions } from "@multica/core/workspace/queries";
import { DEFAULT_TERMINOLOGY } from "../core/api-schemas";
import { formatPeriodRange, nextPeriodInput } from "../core/periods";
import { rockToInput } from "../core/rock-input";
import { goalTypesOptions, periodsOptions, rocksOptions, settingsOptions, strategyOptions, useSavePeriod, useSaveRock } from "../core/queries";
import type { GoalType, OperatingPeriod, Rock, RockInput, Terminology } from "../core/types";
import { HealthBadge, HealthScore } from "./health-score";
import { RockDetail } from "./rock-detail";
import { RockForm } from "./rock-form";
import { RocksTimeline } from "./rocks-timeline";
import { SearchSelect, type SearchSelectOption } from "./search-select";

const formatDate = (value: string) => new Intl.DateTimeFormat("en-US", { month: "short", day: "numeric", timeZone: "UTC" }).format(new Date(`${value.slice(0, 10)}T00:00:00Z`));

function daysLeft(value?: string) {
  if (!value) return 0;
  return Math.max(0, Math.ceil((new Date(`${value}T23:59:59Z`).getTime() - Date.now()) / 86_400_000));
}

const periodOption = (period: OperatingPeriod): SearchSelectOption => ({ value: period.id, label: period.name, hint: formatPeriodRange(period) });
const typeOption = (goalType: GoalType): SearchSelectOption => ({ value: goalType.id, label: goalType.name, hint: goalType.scope_label || undefined, color: goalType.color || undefined });

function InlineRockAdd({ terminology, periods, goalTypes, defaultPeriodId, onPlanPeriod, onCreate, isPending }: {
  terminology: Terminology;
  periods: OperatingPeriod[];
  goalTypes: GoalType[];
  defaultPeriodId: string;
  onPlanPeriod: (onCreated: (id: string) => void) => void;
  onCreate: (input: RockInput, reset: () => void) => void;
  isPending: boolean;
}) {
  const [title, setTitle] = useState("");
  const [periodId, setPeriodId] = useState(defaultPeriodId);
  const [goalTypeId, setGoalTypeId] = useState("");

  useEffect(() => {
    if (!periodId && defaultPeriodId) setPeriodId(defaultPeriodId);
  }, [periodId, defaultPeriodId]);

  function submit() {
    if (!title.trim() || !periodId) return;
    onCreate(
      { title: title.trim(), description: "", period_id: periodId, goal_type_id: goalTypeId || undefined, confidence: 50, reported_health: "unset", project_ids: [], issue_ids: [] },
      () => { setTitle(""); setGoalTypeId(""); },
    );
  }

  return (
    <div className="flex flex-wrap items-center gap-2 border-b bg-muted/20 px-4 py-3">
      <input
        aria-label={`New ${terminology.rock} title`}
        value={title}
        onChange={(event) => setTitle(event.target.value)}
        onKeyDown={(event) => { if (event.key === "Enter") submit(); }}
        placeholder={`Add a ${terminology.rock}… (press Enter)`}
        className="h-9 min-w-56 flex-1 rounded-md border bg-background px-3 text-sm"
      />
      <SearchSelect compact className="w-40" label={`New ${terminology.rock} period`} options={periods.map(periodOption)} value={periodId} onChange={setPeriodId} actionLabel="Plan next period" onAction={() => onPlanPeriod(setPeriodId)} placeholder="Period" />
      <SearchSelect compact className="w-36" label={`New ${terminology.rock} type`} options={goalTypes.map(typeOption)} value={goalTypeId} onChange={setGoalTypeId} clearLabel="No type" placeholder="Type" />
      <button type="button" aria-label={`Add ${terminology.rock}`} disabled={isPending || !title.trim() || !periodId} onClick={submit} className="h-9 rounded-md bg-primary px-4 text-sm font-medium text-primary-foreground disabled:opacity-50">Add</button>
    </div>
  );
}

export function RocksPage() {
  const enabled = useFeatureFlag("cerebro_operating_system");
  const wsId = useWorkspaceId();
  const settings = useQuery(settingsOptions(wsId));
  const periods = useQuery(periodsOptions(wsId));
  const strategy = useQuery(strategyOptions(wsId));
  const goalTypes = useQuery(goalTypesOptions(wsId));
  const members = useQuery(memberListOptions(wsId));
  const agents = useQuery(agentListOptions(wsId));
  const list = useQuery(rocksOptions(wsId));
  const save = useSaveRock(wsId);
  const savePeriod = useSavePeriod(wsId);
  const [formRock, setFormRock] = useState<Rock | undefined>(undefined);
  const [selectedId, setSelectedId] = useState<string>();
  const [typeFilter, setTypeFilter] = useState<string>("");
  const [periodFilter, setPeriodFilter] = useState<string>("");
  const [view, setView] = useState<"list" | "timeline">("list");
  const [groupBy, setGroupBy] = useState<"owner" | "type">("owner");
  const [renamingId, setRenamingId] = useState<string>();

  const terminology = settings.data?.terminology ?? DEFAULT_TERMINOLOGY;
  const periodList = periods.data?.periods ?? [];
  const currentPeriod = periodList[0];
  const types = goalTypes.data?.goal_types ?? [];
  const allRocks = list.data?.rocks ?? [];
  const rocks = allRocks.filter((rock) => (!typeFilter || rock.goal_type_id === typeFilter) && (!periodFilter || rock.period_id === periodFilter));
  const selected = allRocks.find((rock) => rock.id === selectedId);
  const metrics = useMemo(() => {
    const tracked = rocks.filter((rock) => rock.derived_health.state !== "unset" && rock.derived_health.state !== "unknown");
    return {
      score: tracked.length ? Math.round(tracked.reduce((sum, rock) => sum + rock.health_score, 0) / tracked.length) : 0,
      onTrack: rocks.filter((rock) => rock.derived_health.state === "on_track").length,
      attention: rocks.filter((rock) => rock.derived_health.state === "at_risk" || rock.derived_health.state === "off_track").length,
    };
  }, [rocks]);

  const ownerOptions: SearchSelectOption[] = [
    ...(members.data ?? []).map((member) => ({ value: `member:${member.id}`, label: member.name, group: "Members" })),
    ...(agents.data ?? []).map((agent) => ({ value: `agent:${agent.id}`, label: agent.name, group: "Agents" })),
  ];

  function planNextPeriod(onCreated?: (id: string) => void) {
    savePeriod.mutate({ input: nextPeriodInput(periodList) }, { onSuccess: (period) => { if (period && onCreated) onCreated(period.id); } });
  }

  function saveInline(rock: Rock, overrides: Partial<RockInput>) {
    save.mutate({ id: rock.id, input: rockToInput(rock, overrides) });
  }

  if (!enabled) return null;

  return (
    <main className="h-full min-w-0 overflow-y-auto bg-muted/20">
      <div className="mx-auto flex w-full max-w-7xl min-w-0 flex-col gap-5 p-4 sm:p-6">
        <header className="flex flex-wrap items-end justify-between gap-4">
          <div><h1 className="text-3xl font-semibold tracking-tight">{terminology.rocks}</h1><p className="mt-1 text-sm text-muted-foreground">Priorities · {currentPeriod?.name ?? "No active period"}{currentPeriod ? ` · ${formatDate(currentPeriod.starts_on)} – ${formatDate(currentPeriod.ends_on)}` : ""}</p></div>
          <div className="flex flex-wrap items-center gap-2">
            <SearchSelect compact className="w-44" label="Period filter" options={periodList.map(periodOption)} value={periodFilter} onChange={setPeriodFilter} clearLabel="All periods" actionLabel="Plan next period" onAction={() => planNextPeriod(setPeriodFilter)} placeholder="All periods" />
            {types.length > 0 && (
              <div className="flex flex-wrap gap-1 rounded-md border bg-background p-1" role="group" aria-label={`Filter by ${terminology.rock.toLowerCase()} type`}>
                <button type="button" onClick={() => setTypeFilter("")} className={`h-8 rounded px-3 text-sm font-medium ${typeFilter === "" ? "bg-muted" : ""}`}>All</button>
                {types.map((goalType) => (
                  <button key={goalType.id} type="button" onClick={() => setTypeFilter(typeFilter === goalType.id ? "" : goalType.id)} className={`flex h-8 items-center gap-1.5 rounded px-3 text-sm font-medium ${typeFilter === goalType.id ? "bg-muted" : ""}`}>
                    <span aria-hidden className="h-2 w-2 rounded-full" style={{ backgroundColor: goalType.color }} />{goalType.name}
                  </button>
                ))}
              </div>
            )}
            <div className="flex rounded-md border bg-background p-1" role="group" aria-label="View">
              <button type="button" aria-label="List view" onClick={() => setView("list")} className={`h-8 rounded px-3 text-sm font-medium ${view === "list" ? "bg-muted" : ""}`}>List</button>
              <button type="button" aria-label="Timeline view" onClick={() => setView("timeline")} className={`h-8 rounded px-3 text-sm font-medium ${view === "timeline" ? "bg-muted" : ""}`}>Timeline</button>
            </div>
            {view === "timeline" && (
              <div className="flex rounded-md border bg-background p-1" role="group" aria-label="Group by">
                <button type="button" aria-label="Group by owner" onClick={() => setGroupBy("owner")} className={`h-8 rounded px-3 text-sm font-medium ${groupBy === "owner" ? "bg-muted" : ""}`}>By owner</button>
                <button type="button" aria-label="Group by type" onClick={() => setGroupBy("type")} className={`h-8 rounded px-3 text-sm font-medium ${groupBy === "type" ? "bg-muted" : ""}`}>By type</button>
              </div>
            )}
          </div>
        </header>

        <section className="grid overflow-hidden rounded-xl border bg-card sm:grid-cols-2 lg:grid-cols-4">
          <div className="flex items-center gap-3 border-b p-4 sm:border-r lg:border-b-0"><HealthScore score={metrics.score} state={metrics.score >= 70 ? "on_track" : metrics.score >= 40 ? "at_risk" : metrics.score ? "off_track" : "unset"} /><div><p className="text-xs font-semibold tracking-wide text-muted-foreground">OVERALL HEALTH</p><p className="text-sm font-medium">Portfolio score</p></div></div>
          <div className="border-b p-4 lg:border-b-0 lg:border-r"><p className="text-xs font-semibold tracking-wide text-muted-foreground">ON TRACK</p><p className="mt-1 text-2xl font-semibold">{metrics.onTrack}<span className="ml-1 text-sm font-normal text-muted-foreground">of {rocks.length}</span></p></div>
          <div className="border-b p-4 sm:border-r lg:border-b-0"><p className="text-xs font-semibold tracking-wide text-muted-foreground">NEED ATTENTION</p><p className="mt-1 text-2xl font-semibold">{metrics.attention}<span className="ml-1 text-sm font-normal text-muted-foreground">{metrics.attention === 1 ? "Rock" : "Rocks"}</span></p></div>
          <div className="p-4"><p className="text-xs font-semibold tracking-wide text-muted-foreground">DAYS LEFT IN {currentPeriod?.name?.split(" ")[0] ?? "PERIOD"}</p><p className="mt-1 text-2xl font-semibold">{daysLeft(currentPeriod?.ends_on)}</p></div>
        </section>

        {formRock !== undefined && <RockForm wsId={wsId} terminology={terminology} periods={periodList} strategyItems={strategy.data?.strategy_items ?? []} rock={formRock} onSaved={() => setFormRock(undefined)} onCancel={() => setFormRock(undefined)} />}

        {view === "timeline" ? (
          <RocksTimeline rocks={rocks} periods={periodList} terminology={terminology} groupBy={groupBy} onSelect={(id) => setSelectedId(selectedId === id ? undefined : id)} onRename={(rock, title) => saveInline(rock, { title })} onCreate={(input, reset) => save.mutate({ input }, { onSuccess: reset })} isCreating={save.isPending} />
        ) : (
          <section className="overflow-visible rounded-xl border bg-card">
            <div className="hidden grid-cols-[minmax(16rem,2fr)_minmax(11rem,1fr)_minmax(10rem,1fr)_7rem_minmax(9rem,1fr)] gap-4 border-b bg-muted/40 px-4 py-3 text-xs font-semibold uppercase tracking-wide text-muted-foreground md:grid"><span>{terminology.rock}</span><span>Owner</span><span>Issues &amp; Projects</span><span>Health</span><span>Period</span></div>
            <InlineRockAdd terminology={terminology} periods={periodList} goalTypes={types} defaultPeriodId={periodFilter || currentPeriod?.id || ""} onPlanPeriod={planNextPeriod} onCreate={(input, reset) => save.mutate({ input }, { onSuccess: reset })} isPending={save.isPending} />
            {list.isLoading ? <p className="p-8 text-sm text-muted-foreground">Loading {terminology.rocks}…</p> : list.isError ? <p role="alert" className="p-8 text-sm text-destructive">{terminology.rocks} could not be loaded.</p> : rocks.length === 0 ? <div className="p-10 text-center"><h2 className="font-semibold">No {terminology.rocks} yet</h2><p className="mt-1 text-sm text-muted-foreground">Type a title above to create the first one — connections can come later.</p></div> : rocks.map((rock) => (
              <div key={rock.id} className={`grid w-full gap-3 border-b px-4 py-3 last:border-b-0 md:grid-cols-[minmax(16rem,2fr)_minmax(11rem,1fr)_minmax(10rem,1fr)_7rem_minmax(9rem,1fr)] md:items-center md:gap-4 ${selectedId === rock.id ? "bg-primary/5" : "hover:bg-muted/40"}`}>
                <span className="min-w-0">
                  <span className="flex min-w-0 items-center gap-2">
                    {renamingId === rock.id ? (
                      <input
                        aria-label={`${rock.title} new title`}
                        autoFocus
                        defaultValue={rock.title}
                        onKeyDown={(event) => {
                          if (event.key === "Enter") { const value = event.currentTarget.value.trim(); if (value && value !== rock.title) saveInline(rock, { title: value }); setRenamingId(undefined); }
                          if (event.key === "Escape") setRenamingId(undefined);
                        }}
                        onBlur={(event) => { const value = event.currentTarget.value.trim(); if (value && value !== rock.title) saveInline(rock, { title: value }); setRenamingId(undefined); }}
                        className="h-8 min-w-0 flex-1 rounded-md border bg-background px-2 text-sm"
                      />
                    ) : (
                      <>
                        <button type="button" aria-label={`View ${rock.title}`} onClick={() => setSelectedId(selectedId === rock.id ? undefined : rock.id)} className="min-w-0 truncate text-left text-sm font-semibold hover:underline">{rock.title}</button>
                        <button type="button" aria-label={`Rename ${rock.title}`} onClick={() => setRenamingId(rock.id)} className="shrink-0 rounded px-1 text-xs text-muted-foreground hover:bg-muted">✎</button>
                        <SearchSelect compact className="w-28 shrink-0" label={`${rock.title} type`} options={types.map(typeOption)} value={rock.goal_type_id ?? ""} onChange={(value) => saveInline(rock, { goal_type_id: value || undefined })} clearLabel="No type" placeholder="Type" />
                      </>
                    )}
                  </span>
                  <span className="mt-1 block truncate text-xs text-muted-foreground">{rock.strategy_item_title ? `↳ 1-Year Plan · ${rock.strategy_item_title}` : `No ${terminology.strategy} connection`}</span>
                </span>
                <SearchSelect compact label={`${rock.title} owner`} options={ownerOptions} value={rock.owner_id ? `${rock.owner_type}:${rock.owner_id}` : ""} onChange={(value) => { const [ownerType, ownerId] = value.split(":"); saveInline(rock, { owner_type: ownerId ? (ownerType as "member" | "agent") : undefined, owner_id: ownerId || undefined }); }} clearLabel="No owner" placeholder="No owner" />
                <span className="text-sm">{rock.done_issue_count}/{rock.issue_count} issues · {rock.project_count} {rock.project_count === 1 ? "project" : "projects"}</span>
                <span><HealthBadge state={rock.derived_health.state} /></span>
                <SearchSelect compact label={`${rock.title} period`} options={periodList.map(periodOption)} value={rock.period_id} onChange={(value) => { if (value) saveInline(rock, { period_id: value }); }} actionLabel="Plan next period" onAction={() => planNextPeriod((id) => saveInline(rock, { period_id: id }))} placeholder="Period" />
              </div>
            ))}
          </section>
        )}

        {selected && <RockDetail wsId={wsId} rock={selected} terminology={terminology} onEdit={() => setFormRock(selected)} />}

        <footer className="rounded-xl border border-dashed bg-card px-5 py-4 text-center text-sm text-muted-foreground"><strong className="text-foreground">Built on Multica</strong> — a {terminology.rock} can stand alone or connect to Projects and Issues.</footer>
      </div>
    </main>
  );
}
