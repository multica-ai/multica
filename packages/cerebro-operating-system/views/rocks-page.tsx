"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { useWorkspaceId } from "@multica/core/hooks";
import { rocksOptions, settingsOptions } from "../core/queries";
import type { HealthState, Rock } from "../core/types";
import { RockForm } from "./rock-form";

const healthLabel: Record<HealthState, string> = { on_track: "On track", at_risk: "At risk", off_track: "Off track", unset: "Not set", unknown: "Unknown" };
const date = (value: string) => new Intl.DateTimeFormat("en-US", { month: "short", day: "numeric", year: "numeric", timeZone: "UTC" }).format(new Date(`${value.slice(0, 10)}T00:00:00Z`));
const dateTime = (value: string) => new Intl.DateTimeFormat("en-US", { month: "short", day: "numeric", year: "numeric", timeZone: "UTC" }).format(new Date(value));

function RockCard({ rock, noun }: { rock: Rock; noun: string }) {
  const [why, setWhy] = useState(false);
  const progress = rock.issue_count ? Math.round((rock.done_issue_count / rock.issue_count) * 100) : 0;
  return (
    <article className="min-w-0 overflow-hidden rounded-xl border bg-card p-4 sm:p-5">
      <div className="flex min-w-0 flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
        <div className="min-w-0"><p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">{noun} · Project</p><h2 className="break-words text-lg font-semibold">{rock.project_title}</h2>{rock.project_description && <p className="mt-1 break-words text-sm text-muted-foreground">{rock.project_description}</p>}</div>
        <span className="shrink-0 rounded-full border px-2 py-1 text-xs">{healthLabel[rock.derived_health.state]}</span>
      </div>
      <div className="mt-5 grid gap-3 text-sm sm:grid-cols-3"><div><p className="text-muted-foreground">Progress</p><p className="font-medium">{rock.done_issue_count} of {rock.issue_count} issues done</p></div><div><p className="text-muted-foreground">Confidence</p><p className="font-medium">{rock.confidence}% confidence</p></div><div><p className="text-muted-foreground">Deadline</p><p className="font-medium">{date(rock.period_end)}</p></div></div>
      <div className="mt-3 h-2 overflow-hidden rounded-full bg-muted" role="progressbar" aria-valuenow={progress} aria-valuemin={0} aria-valuemax={100}><div className="h-full bg-primary" style={{ width: `${progress}%` }} /></div>
      <div className="mt-3 flex flex-wrap items-center gap-3 text-xs"><span className={rock.blocked_issue_count ? "text-destructive" : "text-muted-foreground"}>{rock.blocked_issue_count} blocked</span><button type="button" className="font-medium underline underline-offset-4" aria-expanded={why} onClick={() => setWhy((value) => !value)}>Why {healthLabel[rock.derived_health.state].toLowerCase()}?</button></div>
      {why && <div className="mt-3 rounded-lg bg-muted/60 p-3 text-sm"><p>{rock.derived_health.reason}</p><p className="mt-1 text-xs text-muted-foreground">Calculated {dateTime(rock.derived_health.calculated_at)}</p></div>}
    </article>
  );
}

export function RocksPage() {
  const enabled = useFeatureFlag("cerebro_operating_system");
  const wsId = useWorkspaceId();
  const settings = useQuery(settingsOptions(wsId));
  const list = useQuery(rocksOptions(wsId));
  const [showForm, setShowForm] = useState(false);
  if (!enabled) return null;
  const terminology = settings.data?.terminology ?? { strategy: "Strategy", rock: "Rock", rocks: "Rocks" };
  return <main className="h-full min-w-0 overflow-y-auto"><div className="mx-auto flex w-full max-w-6xl min-w-0 flex-col gap-5 p-4 sm:p-6"><header className="flex flex-wrap items-end justify-between gap-3"><div><h1 className="text-2xl font-semibold">{terminology.rocks}</h1><p className="text-sm text-muted-foreground">Existing Projects with a focused period and transparent Issue rollup.</p></div><button type="button" onClick={() => setShowForm((value) => !value)} className="h-9 rounded-md bg-primary px-3 text-sm font-medium text-primary-foreground">Add {terminology.rock}</button></header>
    {showForm && <RockForm wsId={wsId} terminology={terminology} onSaved={() => setShowForm(false)} />}
    {list.isLoading ? <p>Loading {terminology.rocks}…</p> : list.isError ? <p role="alert">{terminology.rocks} could not be loaded</p> : (list.data?.rocks ?? []).length === 0 ? <div className="rounded-xl border border-dashed p-10 text-center"><h2 className="font-semibold">No {terminology.rocks} yet</h2><p className="mt-1 text-sm text-muted-foreground">Choose an existing Project to create the first one.</p></div> : <div className="grid min-w-0 gap-4">{list.data?.rocks.map((rock) => <RockCard key={rock.project_id} rock={rock} noun={terminology.rock} />)}</div>}
  </div></main>;
}
