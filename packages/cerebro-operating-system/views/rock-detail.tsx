import { useState, type FormEvent } from "react";
import type { Rock, Terminology } from "../core/types";
import { useRockCheckIn } from "../core/queries";
import { HealthBadge, HealthScore } from "./health-score";

const formatDate = (value: string) => new Intl.DateTimeFormat("en-US", { month: "short", day: "numeric", timeZone: "UTC" }).format(new Date(value));

export function RockDetail({ wsId, rock, terminology, onEdit }: { wsId: string; rock: Rock; terminology: Terminology; onEdit: () => void }) {
  const saveCheckIn = useRockCheckIn(wsId);
  const [confidence, setConfidence] = useState(rock.confidence);
  const [reportedHealth, setReportedHealth] = useState(rock.reported_health === "unknown" ? "unset" : rock.reported_health);
  const [note, setNote] = useState("");

  function submit(event: FormEvent) {
    event.preventDefault();
    saveCheckIn.mutate({ id: rock.id, input: { confidence, reported_health: reportedHealth, note: note.trim() } }, { onSuccess: () => setNote("") });
  }

  return (
    <section className="grid gap-5 rounded-xl border bg-card p-5 lg:grid-cols-[minmax(0,1fr)_18rem]">
      <div className="min-w-0">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div><p className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">Under this {terminology.rock} — {rock.title}</p><h2 className="mt-1 text-xl font-semibold">Connected execution</h2></div>
          <button type="button" onClick={onEdit} className="h-9 rounded-md border px-3 text-sm font-medium">Edit {terminology.rock}</button>
        </div>
        <div className="mt-4 grid gap-3">
          {rock.projects.length === 0 && rock.issues.length === 0 && <p className="rounded-lg border border-dashed p-4 text-sm text-muted-foreground">This {terminology.rock.toLowerCase()} currently stands on its own. Add connections whenever execution is ready.</p>}
          {rock.projects.map((project) => (
            <article key={project.id} className="rounded-lg border p-4">
              <div className="flex justify-between gap-3"><h3 className="font-medium">{project.title}</h3><span className="text-sm text-muted-foreground">{project.done_issue_count}/{project.issue_count} issues</span></div>
              <div className="mt-2 h-1.5 overflow-hidden rounded-full bg-muted"><div className="h-full bg-primary" style={{ width: `${project.issue_count ? (project.done_issue_count / project.issue_count) * 100 : 0}%` }} /></div>
              <div className="mt-3 grid gap-2">{rock.issues.filter((issue) => issue.project_id === project.id).map((issue) => <div key={issue.id} className="flex flex-wrap items-center justify-between gap-2 text-sm"><span><strong>{issue.identifier}</strong> · {issue.title}</span><span className="capitalize text-muted-foreground">{issue.status.replaceAll("_", " ")}</span></div>)}</div>
            </article>
          ))}
          {rock.issues.filter((issue) => !issue.project_id).map((issue) => <article key={issue.id} className="flex flex-wrap items-center justify-between gap-2 rounded-lg border p-4 text-sm"><span><strong>{issue.identifier}</strong> · {issue.title}</span><span className="capitalize text-muted-foreground">{issue.status.replaceAll("_", " ")}</span></article>)}
        </div>
        <div className="mt-6"><h3 className="font-semibold">Weekly check-in history</h3><div className="mt-3 grid gap-2">{rock.check_ins.length === 0 ? <p className="text-sm text-muted-foreground">No check-ins yet.</p> : rock.check_ins.map((checkIn) => <div key={checkIn.id} className="flex items-start gap-3 rounded-lg bg-muted/50 p-3"><HealthScore score={checkIn.confidence} state={checkIn.reported_health} /><div className="min-w-0"><div className="flex flex-wrap items-center gap-2"><HealthBadge state={checkIn.reported_health} /><span className="text-xs text-muted-foreground">{formatDate(checkIn.created_at)}</span></div><p className="mt-1 break-words text-sm">{checkIn.note || "No note"}</p></div></div>)}</div></div>
      </div>
      <form onSubmit={submit} className="h-fit rounded-xl bg-muted/50 p-4">
        <h3 className="font-semibold">Weekly check-in</h3>
        <label className="mt-4 grid gap-1 text-sm">Confidence
          <input aria-label="Check-in confidence" type="range" min={0} max={100} value={confidence} onChange={(event) => setConfidence(Number(event.target.value))} />
          <span className="text-right text-xs text-muted-foreground">{confidence}%</span>
        </label>
        <label className="mt-3 grid gap-1 text-sm">Health
          <select aria-label="Check-in health" value={reportedHealth} onChange={(event) => setReportedHealth(event.target.value as "unset" | "on_track" | "at_risk" | "off_track")} className="h-10 rounded-md border bg-background px-3"><option value="unset">Not reported</option><option value="on_track">On track</option><option value="at_risk">At risk</option><option value="off_track">Off track</option></select>
        </label>
        <label className="mt-3 grid gap-1 text-sm">Note
          <textarea aria-label="Check-in note" value={note} onChange={(event) => setNote(event.target.value)} rows={4} className="rounded-md border bg-background px-3 py-2" placeholder="What changed this week?" />
        </label>
        <button disabled={saveCheckIn.isPending} className="mt-4 h-10 w-full rounded-md bg-primary px-3 text-sm font-medium text-primary-foreground">Save check-in</button>
      </form>
    </section>
  );
}
