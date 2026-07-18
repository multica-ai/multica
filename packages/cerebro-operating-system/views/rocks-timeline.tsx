"use client";

import { useState } from "react";
import type { OperatingPeriod, Rock, RockInput, Terminology } from "../core/types";
import { formatPeriodRange } from "../core/periods";
import { HealthBadge } from "./health-score";

interface RocksTimelineProps {
  rocks: Rock[];
  periods: OperatingPeriod[];
  terminology: Terminology;
  groupBy: "owner" | "type";
  onSelect: (id: string) => void;
  onRename?: (rock: Rock, title: string) => void;
  onCreate?: (input: RockInput, reset: () => void) => void;
  isCreating?: boolean;
}

/** Owner/type context a new rock inherits when it is added into a given group column. */
function groupDefaults(rocks: Rock[], group: string, groupKey: (rock: Rock) => string, groupBy: "owner" | "type"): Partial<RockInput> {
  const representative = rocks.find((rock) => groupKey(rock) === group);
  if (!representative) return {};
  if (groupBy === "owner") {
    if (representative.owner_type !== "member" && representative.owner_type !== "agent") return {};
    return { owner_type: representative.owner_type, owner_id: representative.owner_id || undefined };
  }
  return { goal_type_id: representative.goal_type_id || undefined };
}

export function RocksTimeline({ rocks, periods, terminology, groupBy, onSelect, onRename, onCreate, isCreating }: RocksTimelineProps) {
  const sorted = [...periods].sort((a, b) => a.starts_on.localeCompare(b.starts_on));
  const groupKey = (rock: Rock) => (groupBy === "owner" ? rock.owner_name || "No owner" : rock.goal_type_name || "No type");
  const groups = [...new Set(rocks.map(groupKey))].sort((a, b) => a.localeCompare(b));
  const [renamingId, setRenamingId] = useState<string>();
  const [addingKey, setAddingKey] = useState<string>();
  const [draft, setDraft] = useState("");

  if (sorted.length === 0) {
    return <p className="rounded-xl border border-dashed bg-card p-8 text-center text-sm text-muted-foreground">No periods defined yet. Plan a period to see the timeline.</p>;
  }

  function commitRename(rock: Rock, value: string) {
    const title = value.trim();
    if (onRename && title && title !== rock.title) onRename(rock, title);
    setRenamingId(undefined);
  }

  function commitCreate(periodId: string, group: string) {
    const title = draft.trim();
    if (!onCreate || !title) { setAddingKey(undefined); setDraft(""); return; }
    onCreate(
      { title, description: "", period_id: periodId, confidence: 50, reported_health: "unset", project_ids: [], issue_ids: [], ...groupDefaults(rocks, group, groupKey, groupBy) },
      () => { setDraft(""); },
    );
    setAddingKey(undefined);
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
            {sorted.map((period) => {
              const cellKey = `${group}::${period.id}`;
              return (
                <div key={period.id} className="grid content-start gap-2 border-b border-l p-2">
                  {rocks.filter((rock) => rock.period_id === period.id && groupKey(rock) === group).map((rock) => (
                    <div key={rock.id} className="rounded-lg border bg-background p-2.5 hover:bg-muted/40">
                      {renamingId === rock.id ? (
                        <input
                          aria-label={`${rock.title} new title`}
                          autoFocus
                          defaultValue={rock.title}
                          onKeyDown={(event) => { if (event.key === "Enter") commitRename(rock, event.currentTarget.value); if (event.key === "Escape") setRenamingId(undefined); }}
                          onBlur={(event) => commitRename(rock, event.currentTarget.value)}
                          className="h-8 w-full rounded-md border bg-background px-2 text-sm"
                        />
                      ) : (
                        <>
                          <span className="flex items-center gap-2">
                            {rock.goal_type_color && <span aria-hidden className="h-2 w-2 shrink-0 rounded-full" style={{ backgroundColor: rock.goal_type_color }} />}
                            <button type="button" onClick={() => onSelect(rock.id)} aria-label={`Open ${rock.title}`} className="min-w-0 flex-1 truncate text-left text-sm font-medium hover:underline">{rock.title}</button>
                            {onRename && <button type="button" aria-label={`Rename ${rock.title}`} onClick={() => setRenamingId(rock.id)} className="shrink-0 rounded px-1 text-xs text-muted-foreground hover:bg-muted">✎</button>}
                          </span>
                          <span className="mt-1.5 flex items-center gap-2"><HealthBadge state={rock.derived_health.state} /><span className="text-xs text-muted-foreground">{rock.confidence}%</span></span>
                        </>
                      )}
                    </div>
                  ))}
                  {onCreate && (
                    addingKey === cellKey ? (
                      <input
                        aria-label={`New ${terminology.rock.toLowerCase()} in ${period.name} for ${group}`}
                        autoFocus
                        value={draft}
                        disabled={isCreating}
                        onChange={(event) => setDraft(event.target.value)}
                        onKeyDown={(event) => { if (event.key === "Enter") commitCreate(period.id, group); if (event.key === "Escape") { setAddingKey(undefined); setDraft(""); } }}
                        onBlur={() => commitCreate(period.id, group)}
                        placeholder={`New ${terminology.rock.toLowerCase()}… (Enter)`}
                        className="h-8 w-full rounded-md border bg-background px-2 text-sm"
                      />
                    ) : (
                      <button type="button" aria-label={`Add a ${terminology.rock.toLowerCase()} in ${period.name} for ${group}`} onClick={() => { setAddingKey(cellKey); setDraft(""); }} className="rounded-md border border-dashed px-2 py-1.5 text-left text-xs text-muted-foreground hover:bg-muted/40">+ Add</button>
                    )
                  )}
                </div>
              );
            })}
          </div>
        ))}
      </div>
    </section>
  );
}
