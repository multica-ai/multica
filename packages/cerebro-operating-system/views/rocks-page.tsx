"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { useWorkspaceId } from "@multica/core/hooks";
import { periodsOptions, rocksOptions, settingsOptions, strategyOptions } from "../core/queries";
import type { Rock } from "../core/types";
import { HealthBadge, HealthScore } from "./health-score";
import { RockDetail } from "./rock-detail";
import { RockForm } from "./rock-form";

const formatDate = (value: string) => new Intl.DateTimeFormat("en-US", { month: "short", day: "numeric", timeZone: "UTC" }).format(new Date(`${value.slice(0, 10)}T00:00:00Z`));

function daysLeft(value?: string) {
  if (!value) return 0;
  return Math.max(0, Math.ceil((new Date(`${value}T23:59:59Z`).getTime() - Date.now()) / 86_400_000));
}

export function RocksPage() {
  const enabled = useFeatureFlag("cerebro_operating_system");
  const wsId = useWorkspaceId();
  const settings = useQuery(settingsOptions(wsId));
  const periods = useQuery(periodsOptions(wsId));
  const strategy = useQuery(strategyOptions(wsId));
  const list = useQuery(rocksOptions(wsId));
  const [formRock, setFormRock] = useState<Rock | null | undefined>(undefined);
  const [selectedId, setSelectedId] = useState<string>();

  const terminology = settings.data?.terminology ?? { strategy: "Strategy", rock: "Rock", rocks: "Rocks" };
  const currentPeriod = periods.data?.periods[0];
  const rocks = list.data?.rocks ?? [];
  const selected = rocks.find((rock) => rock.id === selectedId);
  const metrics = useMemo(() => {
    const tracked = rocks.filter((rock) => rock.derived_health.state !== "unset" && rock.derived_health.state !== "unknown");
    return {
      score: tracked.length ? Math.round(tracked.reduce((sum, rock) => sum + rock.health_score, 0) / tracked.length) : 0,
      onTrack: rocks.filter((rock) => rock.derived_health.state === "on_track").length,
      attention: rocks.filter((rock) => rock.derived_health.state === "at_risk" || rock.derived_health.state === "off_track").length,
    };
  }, [rocks]);

  if (!enabled) return null;

  return (
    <main className="h-full min-w-0 overflow-y-auto bg-muted/20">
      <div className="mx-auto flex w-full max-w-7xl min-w-0 flex-col gap-5 p-4 sm:p-6">
        <header className="flex flex-wrap items-end justify-between gap-4">
          <div><h1 className="text-3xl font-semibold tracking-tight">{terminology.rocks}</h1><p className="mt-1 text-sm text-muted-foreground">Quarterly priorities · {currentPeriod?.name ?? "No active period"}{currentPeriod ? ` · ${formatDate(currentPeriod.starts_on)} – ${formatDate(currentPeriod.ends_on)}` : ""}</p></div>
          <div className="flex gap-2"><button type="button" className="h-10 rounded-md border bg-background px-3 text-sm font-medium">Company</button><button type="button" onClick={() => setFormRock(null)} className="h-10 rounded-md bg-primary px-4 text-sm font-medium text-primary-foreground">+ New {terminology.rock}</button></div>
        </header>

        <section className="grid overflow-hidden rounded-xl border bg-card sm:grid-cols-2 lg:grid-cols-4">
          <div className="flex items-center gap-3 border-b p-4 sm:border-r lg:border-b-0"><HealthScore score={metrics.score} state={metrics.score >= 70 ? "on_track" : metrics.score >= 40 ? "at_risk" : metrics.score ? "off_track" : "unset"} /><div><p className="text-xs font-semibold tracking-wide text-muted-foreground">COMPANY HEALTH</p><p className="text-sm font-medium">Portfolio score</p></div></div>
          <div className="border-b p-4 lg:border-b-0 lg:border-r"><p className="text-xs font-semibold tracking-wide text-muted-foreground">ON TRACK</p><p className="mt-1 text-2xl font-semibold">{metrics.onTrack}<span className="ml-1 text-sm font-normal text-muted-foreground">of {rocks.length}</span></p></div>
          <div className="border-b p-4 sm:border-r lg:border-b-0"><p className="text-xs font-semibold tracking-wide text-muted-foreground">NEED ATTENTION</p><p className="mt-1 text-2xl font-semibold">{metrics.attention}<span className="ml-1 text-sm font-normal text-muted-foreground">{metrics.attention === 1 ? "Rock" : "Rocks"}</span></p></div>
          <div className="p-4"><p className="text-xs font-semibold tracking-wide text-muted-foreground">DAYS LEFT IN {currentPeriod?.name?.split(" ")[0] ?? "PERIOD"}</p><p className="mt-1 text-2xl font-semibold">{daysLeft(currentPeriod?.ends_on)}</p></div>
        </section>

        {formRock !== undefined && <RockForm wsId={wsId} terminology={terminology} periods={periods.data?.periods ?? []} strategyItems={strategy.data?.strategy_items ?? []} rock={formRock ?? undefined} onSaved={() => setFormRock(undefined)} onCancel={() => setFormRock(undefined)} />}

        <section className="overflow-hidden rounded-xl border bg-card">
          <div className="hidden grid-cols-[minmax(16rem,2fr)_minmax(10rem,1fr)_minmax(10rem,1fr)_7rem_6rem] gap-4 border-b bg-muted/40 px-4 py-3 text-xs font-semibold uppercase tracking-wide text-muted-foreground md:grid"><span>{terminology.rock}</span><span>Owner</span><span>Issues &amp; Projects</span><span>Health</span><span>Due</span></div>
          {list.isLoading ? <p className="p-8 text-sm text-muted-foreground">Loading {terminology.rocks}…</p> : list.isError ? <p role="alert" className="p-8 text-sm text-destructive">{terminology.rocks} could not be loaded.</p> : rocks.length === 0 ? <div className="p-10 text-center"><h2 className="font-semibold">No {terminology.rocks} yet</h2><p className="mt-1 text-sm text-muted-foreground">Create the first outcome now and connect execution later.</p></div> : rocks.map((rock) => (
            <button key={rock.id} type="button" aria-label={`View ${rock.title}`} onClick={() => setSelectedId(selectedId === rock.id ? undefined : rock.id)} className={`grid w-full gap-3 border-b px-4 py-4 text-left last:border-b-0 md:grid-cols-[minmax(16rem,2fr)_minmax(10rem,1fr)_minmax(10rem,1fr)_7rem_6rem] md:items-center md:gap-4 ${selectedId === rock.id ? "bg-primary/5" : "hover:bg-muted/40"}`}>
              <span className="min-w-0"><strong className="block truncate text-sm">{rock.title}</strong><span className="mt-1 block truncate text-xs text-muted-foreground">{rock.strategy_item_title ? `↳ 1-Year Plan · ${rock.strategy_item_title}` : `No ${terminology.strategy} connection`}</span></span>
              <span className="text-sm">{rock.owner_name ? `${rock.owner_name} · ${rock.owner_type}` : "No owner"}</span>
              <span className="text-sm">{rock.done_issue_count}/{rock.issue_count} issues · {rock.project_count} {rock.project_count === 1 ? "project" : "projects"}</span>
              <span><HealthBadge state={rock.derived_health.state} /></span>
              <span className="text-sm text-muted-foreground">{formatDate(rock.period_end)}</span>
            </button>
          ))}
        </section>

        {selected && <RockDetail wsId={wsId} rock={selected} terminology={terminology} onEdit={() => setFormRock(selected)} />}

        <footer className="rounded-xl border border-dashed bg-card px-5 py-4 text-center text-sm text-muted-foreground"><strong className="text-foreground">Built on Multica</strong> — a {terminology.rock} can stand alone or connect to Projects and Issues.</footer>
      </div>
    </main>
  );
}
