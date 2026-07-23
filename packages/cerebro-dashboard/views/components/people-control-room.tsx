"use client";

import { useEffect, useState } from "react";
import { cn } from "@multica/ui/lib/utils";
import {
  ControlRoomEmpty,
  ControlRoomLoading,
  ControlRoomPanel,
  formatDollars,
} from "./control-room-primitives";

import type { PeopleImpactPeriod } from "../../core/ai-impact-api";
export type { PeopleImpactPeriod } from "../../core/ai-impact-api";

export interface PeopleImpact {
  id: string;
  type: "member" | "agent";
  name: string;
  activity: { bucket: string; count: number }[];
  usage: {
    runs: number | null;
    issues: number | null;
    projects: number | null;
    chats: number | null;
    channels: number | null;
  };
  outcomes: {
    needs_solved: { solved: number; measurable: number } | null;
    solution_quality: number | null;
    frustration_free: number | null;
    prompt_effectiveness: number | null;
    skill_activity: number | null;
    cost_cents: number | null;
  };
  confidence: number | null;
  sample_size: number;
}

const PERIODS: { value: PeopleImpactPeriod; label: string }[] = [
  { value: "hour", label: "Hour" },
  { value: "day", label: "Day" },
  { value: "week", label: "Week" },
  { value: "month", label: "Month" },
];

const USAGE_LABELS = ["Runs", "Issues", "Projects", "Chats", "Channels"] as const;

export function PeopleControlRoom({
  people,
  period,
  onPeriodChange,
  loading,
}: {
  people: PeopleImpact[];
  period: PeopleImpactPeriod;
  onPeriodChange: (period: PeopleImpactPeriod) => void;
  loading: boolean;
}) {
  const [selectedId, setSelectedId] = useState(people[0]?.id ?? null);
  const selected = people.find((person) => person.id === selectedId) ?? people[0];
  useEffect(() => {
    if (!people.some((person) => person.id === selectedId)) setSelectedId(people[0]?.id ?? null);
  }, [people, selectedId]);

  return (
    <div className="min-w-0 space-y-3 p-6">
      <div className="flex items-center justify-between gap-3">
        <div>
          <h2 className="text-sm font-semibold">People</h2>
          <p className="text-[11px] text-muted-foreground">
            Private outcomes for members and agents · no ranking
          </p>
        </div>
        <div className="inline-flex rounded-md border bg-background p-0.5" aria-label="Activity period">
          {PERIODS.map((option) => (
            <button
              key={option.value}
              type="button"
              onClick={() => onPeriodChange(option.value)}
              className={cn(
                "rounded-sm px-3 py-1 text-xs font-medium transition-colors",
                period === option.value
                  ? "bg-muted text-foreground"
                  : "text-muted-foreground hover:text-foreground",
              )}
            >
              {option.label}
            </button>
          ))}
        </div>
      </div>

      <div className="grid min-h-[420px] gap-3 lg:grid-cols-[280px_minmax(0,1fr)]">
        <ControlRoomPanel title="Members and agents" meta="Select a person to inspect their results">
          {loading ? (
            <ControlRoomLoading />
          ) : people.length === 0 ? (
            <ControlRoomEmpty>No sampled activity in this period.</ControlRoomEmpty>
          ) : (
            <div className="space-y-1 p-2">
              {people.map((person) => (
                <button
                  key={person.id}
                  type="button"
                  onClick={() => setSelectedId(person.id)}
                  aria-label={`${person.name}, ${person.type}`}
                  className={cn(
                    "grid w-full grid-cols-[32px_minmax(0,1fr)] items-center gap-3 rounded-md px-2 py-2 text-left",
                    selected?.id === person.id ? "bg-muted text-foreground" : "hover:bg-muted/60",
                  )}
                >
                  <span className="flex size-8 items-center justify-center rounded-full bg-primary/10 text-xs font-semibold text-primary">
                    {person.name.slice(0, 1).toUpperCase()}
                  </span>
                  <span className="min-w-0">
                    <span className="block truncate text-xs font-medium">{person.name}</span>
                    <span className="block text-[10px] capitalize text-muted-foreground">{person.type}</span>
                  </span>
                </button>
              ))}
            </div>
          )}
        </ControlRoomPanel>

        <ControlRoomPanel
          title={selected?.name ?? "Person results"}
          meta={selected ? selected.sample_size > 0 && selected.confidence !== null
            ? `${selected.sample_size} sampled conversations · ${formatPercent(selected.confidence)} confidence`
            : "Direct usage only · coaching assessment not available"
            : undefined}
        >
          {loading ? (
            <ControlRoomLoading rows={6} />
          ) : !selected ? (
            <ControlRoomEmpty>Select a person to inspect their results.</ControlRoomEmpty>
          ) : (
            <div className="space-y-5 p-4">
              <section aria-label="Primary outcome" className="rounded-lg border bg-background p-4">
                <p className="text-[10px] font-medium uppercase tracking-wide text-muted-foreground">Needs solved</p>
                <p className="mt-1 font-mono text-3xl font-semibold tabular-nums text-foreground">
                  {selected.outcomes.needs_solved && selected.outcomes.needs_solved.measurable > 0
                    ? formatPercent(selected.outcomes.needs_solved.solved / selected.outcomes.needs_solved.measurable)
                    : "Missing"}
                </p>
                <p className="mt-1 text-[10px] text-muted-foreground">Sampled outcomes, not an employee score</p>
              </section>

              <section aria-label="Usage" className="grid grid-cols-2 divide-x divide-y overflow-hidden rounded-lg border bg-background sm:grid-cols-5 sm:divide-y-0">
                {USAGE_LABELS.map((label) => {
                  const key = label.toLowerCase() as keyof PeopleImpact["usage"];
                  return (
                    <div key={label} className="px-3 py-3">
                      <p className="text-[10px] text-muted-foreground">{label}</p>
                      <p className="mt-1 font-mono text-lg font-semibold tabular-nums">{selected.usage[key] ?? "—"}</p>
                    </div>
                  );
                })}
              </section>

              <section aria-label="Supporting outcomes" className="grid gap-2 sm:grid-cols-2 xl:grid-cols-5">
                <Outcome label="Solution quality" value={formatOptionalPercent(selected.outcomes.solution_quality)} />
                <Outcome label="Frustration-free" value={formatOptionalPercent(selected.outcomes.frustration_free)} />
                <Outcome label="Prompt effectiveness" value={formatOptionalPercent(selected.outcomes.prompt_effectiveness)} />
                <Outcome label="Skill activity" value={selected.outcomes.skill_activity?.toLocaleString("en-US") ?? "—"} />
                <Outcome label="Cost" value={selected.outcomes.cost_cents === null ? "—" : formatDollars(selected.outcomes.cost_cents)} />
              </section>
              <section aria-label="Activity" className="rounded-lg border bg-background p-3">
                <p className="mb-2 text-[10px] font-medium uppercase tracking-wide text-muted-foreground">Activity</p>
                <div className="flex min-h-8 items-end gap-1 overflow-hidden">
                  {selected.activity.length === 0 ? <span className="text-xs text-muted-foreground">No activity in this period.</span> : selected.activity.map((item) => (
                    <span key={item.bucket} title={`${item.bucket}: ${item.count}`} aria-label={`${item.bucket}: ${item.count} activities`}
                      className="min-w-2 flex-1 rounded-sm bg-primary/20" style={{ height: `${Math.max(8, Math.min(32, item.count * 4))}px` }} />
                  ))}
                </div>
              </section>
            </div>
          )}
        </ControlRoomPanel>
      </div>
    </div>
  );
}

function formatOptionalPercent(value: number | null): string {
  return value === null ? "—" : formatPercent(value);
}

function Outcome({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-md border bg-background px-3 py-2">
      <p className="text-[10px] text-muted-foreground">{label}</p>
      <p className="mt-1 font-mono text-sm font-semibold tabular-nums">{value}</p>
    </div>
  );
}

function formatPercent(value: number): string {
  return `${Math.round(value * 100)}%`;
}
