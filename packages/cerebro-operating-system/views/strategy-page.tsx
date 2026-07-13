"use client";

import { useState, type FormEvent } from "react";
import { useQuery } from "@tanstack/react-query";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { useWorkspaceId } from "@multica/core/hooks";
import { connectionsOptions, rocksOptions, settingsOptions, strategyOptions, useUpdateSettings } from "../core/queries";
import type { Rock, StrategyItem, Terminology } from "../core/types";
import { StrategyForm } from "./strategy-form";

const plural = (count: number, unit: string) => `${count} ${unit}${count === 1 ? "" : "s"}`;

function TerminologyForm({ wsId, current, onSaved }: { wsId: string; current: Terminology; onSaved: () => void }) {
  const update = useUpdateSettings(wsId);
  const [values, setValues] = useState(current);
  function submit(event: FormEvent) { event.preventDefault(); update.mutate(values, { onSuccess: onSaved }); }
  return <form onSubmit={submit} className="grid gap-4 rounded-xl border bg-card p-4 sm:grid-cols-3"><label className="grid gap-1 text-sm">Strategy label<input aria-label="Strategy label" required value={values.strategy} onChange={(e) => setValues({ ...values, strategy: e.target.value })} className="h-9 rounded-md border bg-background px-3" /></label><label className="grid gap-1 text-sm">Rock label<input aria-label="Rock label" required value={values.rock} onChange={(e) => setValues({ ...values, rock: e.target.value })} className="h-9 rounded-md border bg-background px-3" /></label><label className="grid gap-1 text-sm">Rocks label<input aria-label="Rocks label" required value={values.rocks} onChange={(e) => setValues({ ...values, rocks: e.target.value })} className="h-9 rounded-md border bg-background px-3" /></label><button disabled={update.isPending} className="h-9 rounded-md bg-primary px-3 text-sm font-medium text-primary-foreground sm:col-span-3">Save labels</button></form>;
}

function StrategyCard({ item, wsId, rocks, terminology }: { item: StrategyItem; wsId: string; rocks: Rock[]; terminology: Terminology }) {
  const connections = useQuery(connectionsOptions(wsId, "strategy_item", item.id));
  const linkedIds = new Set((connections.data?.connections ?? []).flatMap((connection) => connection.target_type === "rock" ? [connection.target_id] : []));
  const linked = rocks.filter((rock) => linkedIds.has(rock.project_id));
  return <article className="min-w-0 rounded-xl border bg-card p-4"><div className="flex flex-wrap items-center justify-between gap-2"><h3 className="break-words font-semibold">{item.title}</h3>{item.kind === "horizon_goal" && item.horizon_count && item.horizon_unit && <span className="rounded-full bg-muted px-2 py-1 text-xs">{plural(item.horizon_count, item.horizon_unit)}</span>}</div>{item.description && <p className="mt-2 break-words text-sm text-muted-foreground">{item.description}</p>}{linked.length > 0 && <div className="mt-3 border-t pt-3"><p className="text-xs font-medium text-muted-foreground">Connected {terminology.rocks}</p>{linked.map((rock) => <p key={rock.project_id} className="mt-1 text-sm">{rock.project_title}</p>)}</div>}</article>;
}

export function StrategyPage() {
  const enabled = useFeatureFlag("cerebro_operating_system");
  const wsId = useWorkspaceId();
  const settings = useQuery(settingsOptions(wsId));
  const strategy = useQuery(strategyOptions(wsId));
  const rocks = useQuery(rocksOptions(wsId));
  const [showForm, setShowForm] = useState(false);
  const [showTerminology, setShowTerminology] = useState(false);
  if (!enabled) return null;
  const terminology = settings.data?.terminology ?? { strategy: "Strategy", rock: "Rock", rocks: "Rocks" };
  const items = strategy.data?.strategy_items ?? [];
  const activeRocks = rocks.data?.rocks ?? [];
  const values = items.filter((item) => item.kind === "core_value");
  const focus = items.filter((item) => item.kind === "core_focus");
  const horizons = items.filter((item) => item.kind !== "core_value" && item.kind !== "core_focus").sort((a, b) => a.position - b.position);
  const section = (title: string, sectionItems: StrategyItem[]) => <section><h2 className="mb-3 text-sm font-semibold uppercase tracking-wide text-muted-foreground">{title}</h2><div className="grid min-w-0 gap-3 md:grid-cols-2">{sectionItems.map((item) => <StrategyCard key={item.id} item={item} wsId={wsId} rocks={activeRocks} terminology={terminology} />)}</div></section>;
  return <main className="h-full min-w-0 overflow-y-auto"><div className="mx-auto flex w-full max-w-6xl min-w-0 flex-col gap-6 p-4 sm:p-6"><header className="flex flex-wrap items-end justify-between gap-3"><div><h1 className="text-2xl font-semibold">{terminology.strategy}</h1><p className="text-sm text-muted-foreground">Shape your {terminology.strategy}</p></div><div className="flex flex-wrap gap-2"><button type="button" onClick={() => setShowTerminology((value) => !value)} className="h-9 rounded-md border px-3 text-sm font-medium">Customize labels</button><button type="button" onClick={() => setShowForm((value) => !value)} className="h-9 rounded-md bg-primary px-3 text-sm font-medium text-primary-foreground">Add {terminology.strategy} item</button></div></header>{showTerminology && <TerminologyForm wsId={wsId} current={terminology} onSaved={() => setShowTerminology(false)} />}{showForm && <StrategyForm wsId={wsId} terminology={terminology} rocks={activeRocks} position={items.length} onSaved={() => setShowForm(false)} />}{strategy.isLoading ? <p>Loading {terminology.strategy}…</p> : strategy.isError ? <p role="alert">{terminology.strategy} could not be loaded</p> : <>{items.length === 0 && <div className="rounded-xl border border-dashed p-10 text-center"><h2 className="font-semibold">Shape your {terminology.strategy}</h2><p className="mt-1 text-sm text-muted-foreground">Start with a value, focus, or time horizon.</p></div>}{section("Core Values", values)}{section("Core Focus", focus)}{section("Horizons", horizons)}<section><h2 className="mb-3 text-sm font-semibold uppercase tracking-wide text-muted-foreground">Connected {terminology.rocks}</h2>{activeRocks.length === 0 ? <p className="text-sm text-muted-foreground">No current {terminology.rocks.toLowerCase()}.</p> : <div className="grid gap-3 md:grid-cols-2">{activeRocks.map((rock) => <div key={rock.project_id} className="min-w-0 rounded-xl border bg-card p-4"><p className="break-words font-medium">{rock.project_title}</p><p className="text-xs text-muted-foreground">{rock.done_issue_count} of {rock.issue_count} issues done</p></div>)}</div>}</section></>}</div></main>;
}
