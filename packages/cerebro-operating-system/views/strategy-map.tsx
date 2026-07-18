import { ArrowDown, CircleAlert, FolderKanban, ListTodo, Pencil } from "lucide-react";

import type { Rock, Terminology, VisionPlanSection } from "../core/types";
import { HealthBadge } from "./health-score";

interface StrategyMapProps {
  sections: VisionPlanSection[];
  rocks: Rock[];
  terminology: Terminology;
  onEditSection: (sectionId: string) => void;
  onOpenRock: (rockId: string) => void;
}

const preferredBands = [
  { keys: ["ten-year-target", "core-focus"], fallback: "Long-term direction" },
  { keys: ["three-year-picture"], fallback: "Mid-term picture" },
  { keys: ["one-year-plan"], fallback: "One-Year Plan" },
] as const;

export function StrategyMap({ sections, rocks, terminology, onEditSection, onOpenRock }: StrategyMapProps) {
  const ordered = preferredBands.map(({ keys, fallback }) => ({
    section: sections.find((candidate) => (keys as readonly string[]).includes(candidate.key)),
    fallback, primaryKey: keys[0],
  }));
  const annualSection = ordered.find(({ primaryKey }) => primaryKey === "one-year-plan")?.section;
  const connectedIds = new Set(annualSection?.items.flatMap((item) => item.goal_connections.map((connection) => connection.goal_id)) ?? []);
  const connectedRocks = rocks.filter((rock) => connectedIds.has(rock.id));

  return (
    <section aria-label={terminology.strategy_map} className="grid min-w-0 gap-3">
      {ordered.map(({ section, fallback, primaryKey }, index) => {
        const label = section?.name || fallback;
        const items = section?.items.filter((item) => item.state === "active") ?? [];
        return (
          <div key={fallback} className="grid justify-items-center gap-2">
            <section aria-label={label} className="w-full rounded-2xl border bg-card p-4 shadow-sm sm:p-5">
              <header className="flex items-center justify-between gap-3">
                <div><p className="text-xs font-semibold uppercase tracking-[0.16em] text-muted-foreground">{index === 0 ? "Direction" : index === 1 ? "Picture" : "Annual commitments"}</p><h2 className="mt-1 text-lg font-semibold">{label}</h2></div>
                <button type="button" aria-label={`${section ? "Edit" : "Add"} ${label}`} onClick={() => onEditSection(section?.id || primaryKey)} className="grid size-11 shrink-0 place-items-center rounded-lg border bg-background text-muted-foreground hover:bg-muted hover:text-foreground"><Pencil aria-hidden className="size-4" /></button>
              </header>
              {items.length > 0 ? <div className="mt-3 grid gap-2 md:grid-cols-2">{items.map((item) => <button key={item.id} type="button" onClick={() => onEditSection(section?.id || primaryKey)} className="min-h-11 rounded-lg border bg-background px-3 py-2 text-left text-sm font-medium hover:border-primary/40 hover:bg-muted/40">{item.title}</button>)}</div> : <button type="button" onClick={() => onEditSection(section?.id || primaryKey)} className="mt-3 flex min-h-11 w-full items-center justify-center gap-2 rounded-lg border border-dashed text-sm text-muted-foreground hover:bg-muted"><CircleAlert aria-hidden className="size-4" />Add direction</button>}
            </section>
            <ArrowDown aria-hidden className="size-5 text-primary/60" />
          </div>
        );
      })}
      <section aria-label={terminology.rocks} className="rounded-2xl border border-primary/20 bg-primary/[0.03] p-4 sm:p-5">
        <header><p className="text-xs font-semibold uppercase tracking-[0.16em] text-primary">Current execution</p><h2 className="mt-1 text-lg font-semibold">{terminology.rocks}</h2></header>
        {connectedRocks.length > 0 ? <div className="mt-3 grid gap-3 lg:grid-cols-2">{connectedRocks.map((rock) => <article key={rock.id} className="rounded-xl border bg-card p-4 shadow-sm">
          <div className="flex items-start justify-between gap-3"><button type="button" onClick={() => onOpenRock(rock.id)} aria-label={`Open ${terminology.rock} ${rock.title}`} className="min-h-11 flex-1 text-left font-semibold hover:underline">{rock.title}</button><HealthBadge state={rock.derived_health.state} /></div>
          <p className="mt-2 text-xs text-muted-foreground">{rock.owner_name || "No owner"}{rock.period_name ? ` · ${rock.period_name}` : ""}</p>
          <div className="mt-3 flex flex-wrap gap-3 text-xs text-muted-foreground"><span className="inline-flex items-center gap-1"><FolderKanban aria-hidden className="size-3.5" />{rock.project_count} projects</span><span className="inline-flex items-center gap-1"><ListTodo aria-hidden className="size-3.5" />{rock.issue_count} issues</span></div>
          <span className="sr-only">{rock.project_count} projects · {rock.issue_count} issues</span>
        </article>)}</div> : <p className="mt-3 rounded-lg border border-dashed p-5 text-center text-sm text-muted-foreground">No {terminology.rocks.toLowerCase()} connected yet</p>}
      </section>
    </section>
  );
}
