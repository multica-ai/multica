import { useState, type FormEvent } from "react";
import type { HorizonUnit, Rock, StrategyKind, Terminology } from "../core/types";
import { useCreateConnection, useCreateStrategyItem } from "../core/queries";

interface StrategyFormProps { wsId: string; terminology: Terminology; rocks: Rock[]; position: number; onSaved: () => void; }

export function StrategyForm({ wsId, terminology, rocks, position, onSaved }: StrategyFormProps) {
  const createItem = useCreateStrategyItem(wsId);
  const createConnection = useCreateConnection(wsId);
  const [kind, setKind] = useState<Exclude<StrategyKind, "unknown">>("horizon_goal");
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [count, setCount] = useState(1);
  const [unit, setUnit] = useState<HorizonUnit>("year");
  const [rockId, setRockId] = useState("");

  async function submit(event: FormEvent) {
    event.preventDefault();
    const created = await createItem.mutateAsync({ kind, title, description, position, ...(kind === "horizon_goal" ? { horizon_count: count, horizon_unit: unit } : {}) });
    if (created && rockId) await createConnection.mutateAsync({ source_type: "strategy_item", source_id: created.id, target_type: "rock", target_id: rockId, relationship_type: "supports", provenance: "manual" });
    onSaved();
  }

  return <form onSubmit={submit} className="grid gap-4 rounded-xl border bg-card p-4 sm:grid-cols-2"><label className="grid gap-1 text-sm">Type<select aria-label="Type" value={kind} onChange={(e) => setKind(e.target.value as Exclude<StrategyKind, "unknown">)} className="h-9 rounded-md border bg-background px-3"><option value="core_value">Core Value</option><option value="core_focus">Core Focus</option><option value="horizon_goal">Horizon goal</option></select></label><label className="grid gap-1 text-sm">Title<input aria-label="Title" required value={title} onChange={(e) => setTitle(e.target.value)} className="h-9 min-w-0 rounded-md border bg-background px-3" /></label><label className="grid gap-1 text-sm sm:col-span-2">Description<textarea aria-label="Description" value={description} onChange={(e) => setDescription(e.target.value)} className="min-h-20 rounded-md border bg-background p-3" /></label>{kind === "horizon_goal" && <><label className="grid gap-1 text-sm">Horizon count<input aria-label="Horizon count" type="number" min={1} value={count} onChange={(e) => setCount(Number(e.target.value))} className="h-9 rounded-md border bg-background px-3" /></label><label className="grid gap-1 text-sm">Horizon unit<select aria-label="Horizon unit" value={unit} onChange={(e) => setUnit(e.target.value as HorizonUnit)} className="h-9 rounded-md border bg-background px-3"><option value="day">Days</option><option value="week">Weeks</option><option value="month">Months</option><option value="year">Years</option></select></label></>}
  <label className="grid gap-1 text-sm sm:col-span-2">Connected {terminology.rock}<select aria-label={`Connected ${terminology.rock}`} value={rockId} onChange={(e) => setRockId(e.target.value)} className="h-9 rounded-md border bg-background px-3"><option value="">None</option>{rocks.map((rock) => <option key={rock.project_id} value={rock.project_id}>{rock.project_title}</option>)}</select></label><button disabled={createItem.isPending || createConnection.isPending} className="h-9 rounded-md bg-primary px-3 text-sm font-medium text-primary-foreground sm:col-span-2">Save {terminology.strategy} item</button></form>;
}
