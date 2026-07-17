"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { useWorkspaceId } from "@multica/core/hooks";
import { DEFAULT_TERMINOLOGY } from "../core/api-schemas";
import { rocksOptions, settingsOptions, strategyHistoryOptions, strategyOptions } from "../core/queries";
import type { Rock, StrategyItem } from "../core/types";
import { HealthBadge } from "./health-score";
import { StrategyForm } from "./strategy-form";

function ItemCard({ item, editable, onEdit }: { item: StrategyItem; editable: boolean; onEdit: () => void }) {
  return <article className="rounded-lg border bg-background p-3"><div className="flex items-start justify-between gap-2"><div className="min-w-0"><h3 className="break-words text-sm font-semibold">{item.title}</h3>{item.description && <p className="mt-1 break-words text-xs leading-relaxed text-muted-foreground">{item.description}</p>}</div>{editable && <button type="button" onClick={onEdit} className="shrink-0 text-xs font-medium underline underline-offset-4">Edit</button>}</div></article>;
}

function Column({ title, items, empty, editable, onEdit }: { title: string; items: StrategyItem[]; empty: string; editable: boolean; onEdit: (item: StrategyItem) => void }) {
  return <section className="min-w-0 rounded-xl border bg-card p-3"><h2 className="mb-3 text-xs font-semibold uppercase tracking-wide text-muted-foreground">{title}</h2><div className="grid gap-2">{items.length ? items.map((item) => <ItemCard key={item.id} item={item} editable={editable} onEdit={() => onEdit(item)} />) : <p className="rounded-lg border border-dashed p-3 text-xs text-muted-foreground">{empty}</p>}</div></section>;
}

function RockColumn({ rocks }: { rocks: Rock[] }) {
  return <section className="min-w-0 rounded-xl border bg-card p-3"><h2 className="mb-3 text-xs font-semibold uppercase tracking-wide text-muted-foreground">Quarterly Rocks</h2><div className="grid gap-2">{rocks.length ? rocks.map((rock) => <article key={rock.id} className="rounded-lg border bg-background p-3"><div className="flex items-start justify-between gap-2"><h3 className="break-words text-sm font-semibold">{rock.title}</h3><HealthBadge state={rock.derived_health.state} /></div><p className="mt-2 text-xs text-muted-foreground">{rock.done_issue_count}/{rock.issue_count} issues · {rock.confidence}% confidence</p></article>) : <p className="rounded-lg border border-dashed p-3 text-xs text-muted-foreground">No connected Rocks yet.</p>}</div></section>;
}

function byHorizon(items: StrategyItem[], label: string, count: number) {
  return items.filter((item) => item.kind === "horizon_goal" && (item.horizon_label === label || item.horizon_count === count));
}

export function StrategyPage() {
  const enabled = useFeatureFlag("cerebro_operating_system");
  const wsId = useWorkspaceId();
  const settings = useQuery(settingsOptions(wsId));
  const strategy = useQuery(strategyOptions(wsId));
  const rocks = useQuery(rocksOptions(wsId));
  const [editing, setEditing] = useState(false);
  const [formItem, setFormItem] = useState<StrategyItem | null | undefined>(undefined);
  const [history, setHistory] = useState(false);
  const strategyHistory = useQuery({ ...strategyHistoryOptions(wsId), enabled: history && !!wsId });
  if (!enabled) return null;

  const terminology = settings.data?.terminology ?? DEFAULT_TERMINOLOGY;
  const items = (strategy.data?.strategy_items ?? []).filter((item) => item.state === "active").sort((a, b) => a.position - b.position);
  const values = items.filter((item) => item.kind === "core_value");
  const focus = items.filter((item) => item.kind === "core_focus");
  const tenYear = byHorizon(items, "10-Year Target", 10);
  const threeYear = byHorizon(items, "3-Year Picture", 3);
  const oneYear = byHorizon(items, "1-Year Plan", 1);
  const known = new Set([...tenYear, ...threeYear, ...oneYear].map((item) => item.id));
  const otherHorizons = items.filter((item) => item.kind === "horizon_goal" && !known.has(item.id));
  const customHorizons = Array.from(otherHorizons.reduce((groups, item) => {
    const label = item.horizon_label || `${item.horizon_count ?? "Custom"}-${item.horizon_unit ?? "period"} Horizon`;
    groups.set(label, [...(groups.get(label) ?? []), item]);
    return groups;
  }, new Map<string, StrategyItem[]>()));
  const activeRocks = rocks.data?.rocks ?? [];

  return (
    <main className="h-full min-w-0 overflow-y-auto bg-muted/20">
      <div className="mx-auto flex w-full max-w-7xl min-w-0 flex-col gap-5 p-4 sm:p-6">
        <header className="flex flex-wrap items-end justify-between gap-4"><div><h1 className="text-3xl font-semibold tracking-tight">{terminology.strategy}</h1><p className="mt-1 text-sm text-muted-foreground">From long-term direction to weekly execution</p></div><div className="flex gap-2"><button type="button" onClick={() => setHistory((value) => !value)} className="h-10 rounded-md border bg-background px-4 text-sm font-medium">History</button><button type="button" onClick={() => setEditing((value) => !value)} className="h-10 rounded-md bg-primary px-4 text-sm font-medium text-primary-foreground">Edit map</button></div></header>

        {history && <section className="rounded-xl border bg-card p-5"><h2 className="font-semibold">Strategy history</h2><p className="mt-1 text-sm text-muted-foreground">Every saved map change is retained, including deleted items.</p><div className="mt-4 grid gap-2">{strategyHistory.isLoading ? <p className="text-sm text-muted-foreground">Loading history…</p> : (strategyHistory.data?.history ?? []).length === 0 ? <p className="text-sm text-muted-foreground">No saved changes yet.</p> : strategyHistory.data?.history.map((entry) => <div key={entry.id} className="flex flex-wrap justify-between gap-2 rounded-lg bg-muted/50 p-3 text-sm"><span><span className="mr-2 rounded-full border px-2 py-0.5 text-xs capitalize">{entry.action}</span>{entry.title}</span><time className="text-muted-foreground">{new Intl.DateTimeFormat("en-US", { dateStyle: "medium" }).format(new Date(entry.changed_at))}</time></div>)}</div></section>}

        {editing && <section className="grid gap-3 rounded-xl border border-dashed p-4"><div className="flex items-center justify-between"><div><h2 className="font-semibold">Edit map</h2><p className="text-sm text-muted-foreground">Edit any card or add a named horizon.</p></div><button type="button" onClick={() => setFormItem(null)} className="h-9 rounded-md border bg-background px-3 text-sm font-medium">+ Add item</button></div>{formItem !== undefined && <StrategyForm wsId={wsId} terminology={terminology} position={items.length} item={formItem ?? undefined} onSaved={() => setFormItem(undefined)} onCancel={() => setFormItem(undefined)} />}</section>}

        {strategy.isLoading ? <p>Loading {terminology.strategy}…</p> : strategy.isError ? <p role="alert">{terminology.strategy} could not be loaded</p> : <>
          <section className="grid gap-3 md:grid-cols-2"><div className="rounded-xl border bg-card p-4"><h2 className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">Core Values</h2><div className="mt-3 grid gap-2 sm:grid-cols-2">{values.length ? values.map((item) => <ItemCard key={item.id} item={item} editable={editing} onEdit={() => setFormItem(item)} />) : <p className="text-sm text-muted-foreground">Define the behaviors that guide every decision.</p>}</div></div><div className="rounded-xl border bg-card p-4"><h2 className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">Core Focus</h2><div className="mt-3 grid gap-2">{focus.length ? focus.map((item) => <ItemCard key={item.id} item={item} editable={editing} onEdit={() => setFormItem(item)} />) : <p className="text-sm text-muted-foreground">Define purpose, niche and advantage.</p>}</div></div></section>
          <section className="grid min-w-0 gap-3 md:grid-cols-2 xl:grid-cols-4"><Column title={tenYear[0]?.horizon_label || "10-Year Target"} items={tenYear} empty="Set the long-term destination." editable={editing} onEdit={setFormItem} /><Column title={threeYear[0]?.horizon_label || "3-Year Picture"} items={threeYear} empty="Describe the mid-term picture." editable={editing} onEdit={setFormItem} /><Column title={oneYear[0]?.horizon_label || "1-Year Plan"} items={oneYear} empty="Choose this year's outcomes." editable={editing} onEdit={setFormItem} />{customHorizons.map(([label, horizonItems]) => <Column key={label} title={label} items={horizonItems} empty="Define this horizon." editable={editing} onEdit={setFormItem} />)}<RockColumn rocks={activeRocks} /></section>
        </>}

        <footer className="rounded-xl border border-dashed bg-card px-5 py-4 text-center text-sm text-muted-foreground"><strong className="text-foreground">Built on Multica</strong> — {terminology.strategy} connects direction to {terminology.rocks}, Projects and Issues without duplicating execution.</footer>
      </div>
    </main>
  );
}
