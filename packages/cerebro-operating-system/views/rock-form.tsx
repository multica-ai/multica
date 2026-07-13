import { useState, type FormEvent } from "react";
import { useQuery } from "@tanstack/react-query";
import { projectListOptions } from "@multica/core/projects";
import type { HealthState, Terminology } from "../core/types";
import { useUpsertRock } from "../core/queries";

interface RockFormProps { wsId: string; terminology: Terminology; onSaved: () => void; }

export function RockForm({ wsId, terminology, onSaved }: RockFormProps) {
  const projects = useQuery(projectListOptions(wsId));
  const save = useUpsertRock(wsId);
  const [projectId, setProjectId] = useState("");
  const [start, setStart] = useState("");
  const [end, setEnd] = useState("");
  const [confidence, setConfidence] = useState(50);
  const [health, setHealth] = useState<Exclude<HealthState, "unknown">>("unset");

  function submit(event: FormEvent) {
    event.preventDefault();
    save.mutate({ project_id: projectId, period_start: start, period_end: end, confidence, reported_health: health }, { onSuccess: onSaved });
  }

  return (
    <form onSubmit={submit} className="grid gap-4 rounded-xl border bg-card p-4 sm:grid-cols-2">
      <label className="grid gap-1 text-sm sm:col-span-2">Project
        <select aria-label="Project" required value={projectId} onChange={(e) => setProjectId(e.target.value)} className="h-9 min-w-0 rounded-md border bg-background px-3">
          <option value="">Select an existing Project</option>
          {(projects.data ?? []).map((project) => <option key={project.id} value={project.id}>{project.title}</option>)}
        </select>
      </label>
      <label className="grid gap-1 text-sm">Period start<input aria-label="Period start" required type="date" value={start} onChange={(e) => setStart(e.target.value)} className="h-9 min-w-0 rounded-md border bg-background px-3" /></label>
      <label className="grid gap-1 text-sm">Period end<input aria-label="Period end" required type="date" value={end} onChange={(e) => setEnd(e.target.value)} className="h-9 min-w-0 rounded-md border bg-background px-3" /></label>
      <label className="grid gap-1 text-sm">Confidence<input aria-label="Confidence" type="number" min={0} max={100} value={confidence} onChange={(e) => setConfidence(Number(e.target.value))} className="h-9 min-w-0 rounded-md border bg-background px-3" /></label>
      <label className="grid gap-1 text-sm">Reported health
        <select aria-label="Reported health" value={health} onChange={(e) => setHealth(e.target.value as Exclude<HealthState, "unknown">)} className="h-9 min-w-0 rounded-md border bg-background px-3">
          <option value="unset">Not reported</option><option value="on_track">On track</option><option value="at_risk">At risk</option><option value="off_track">Off track</option>
        </select>
      </label>
      <button disabled={save.isPending} className="h-9 rounded-md bg-primary px-3 text-sm font-medium text-primary-foreground sm:col-span-2">Save {terminology.rock}</button>
    </form>
  );
}
