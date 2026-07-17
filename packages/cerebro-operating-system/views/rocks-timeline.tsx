"use client";

import type { OperatingPeriod, Rock, Terminology } from "../core/types";
import { formatPeriodRange } from "../core/periods";
import { HealthBadge } from "./health-score";

interface RocksTimelineProps {
  rocks: Rock[];
  periods: OperatingPeriod[];
  terminology: Terminology;
  groupBy: "owner" | "type";
  onSelect: (id: string) => void;
}

export function RocksTimeline({ rocks, periods, terminology, groupBy, onSelect }: RocksTimelineProps) {
  const sorted = [...periods].sort((a, b) => a.starts_on.localeCompare(b.starts_on));
  const groupKey = (rock: Rock) => (groupBy === "owner" ? rock.owner_name || "No owner" : rock.goal_type_name || "No type");
  const groups = [...new Set(rocks.map(groupKey))].sort((a, b) => a.localeCompare(b));

  if (sorted.length === 0) {
    return <p className="rounded-xl border border-dashed bg-card p-8 text-center text-sm text-muted-foreground">No periods defined yet. Plan a period to see the timeline.</p>;
  }

  return (
    <section aria-label={`${terminology.rocks} timeline`} className="overflow-x-auto rounded-xl border bg-card">
      <div className="grid min-w-fit" style={{ gridTemplateColumns: `11rem repeat(${sorted.length}, minmax(15rem, 1fr))` }}>
        <div className="border-b bg-muted/40 px-4 py-3" />
        {sorted.map((period) => (
          <div key={period.id} className="border-b border-l bg-muted/40 px-4 py-3">
            <p className="text-sm font-semibold">{period.name}</p>
            <p className="text-xs text-muted-foreground">{formatPeriodRange(period)}</p>
          </div>
        ))}
        {groups.length === 0 && (
          <p className="col-span-full p-8 text-center text-sm text-muted-foreground">No {terminology.rocks.toLowerCase()} to place on the timeline yet.</p>
        )}
        {groups.map((group) => (
          <div key={group} className="contents">
            <div className="border-b px-4 py-3 text-sm font-medium">{group}</div>
            {sorted.map((period) => (
              <div key={period.id} className="grid content-start gap-2 border-b border-l p-2">
                {rocks.filter((rock) => rock.period_id === period.id && groupKey(rock) === group).map((rock) => (
                  <button key={rock.id} type="button" onClick={() => onSelect(rock.id)} aria-label={`Open ${rock.title}`} className="rounded-lg border bg-background p-2.5 text-left hover:bg-muted/40">
                    <span className="flex items-center gap-2">
                      {rock.goal_type_color && <span aria-hidden className="h-2 w-2 shrink-0 rounded-full" style={{ backgroundColor: rock.goal_type_color }} />}
                      <span className="truncate text-sm font-medium">{rock.title}</span>
                    </span>
                    <span className="mt-1.5 flex items-center gap-2"><HealthBadge state={rock.derived_health.state} /><span className="text-xs text-muted-foreground">{rock.confidence}%</span></span>
                  </button>
                ))}
              </div>
            ))}
          </div>
        ))}
      </div>
    </section>
  );
}
