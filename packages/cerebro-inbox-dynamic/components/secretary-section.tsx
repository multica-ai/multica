// TECH-3557 — focused Secretary batch over the existing dynamic inbox rows.
"use client";

import { useEffect, useMemo, useState } from "react";
import { Check, Minus, PartyPopper, Play, Plus, RotateCcw, Sparkles, X } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Badge } from "@multica/ui/components/ui/badge";
import {
  entryIsUnread,
  entryMatchesQuery,
  type DynInboxEntry,
  type SectionFilterContext,
} from "../section-filter";
import { DynamicInboxRow } from "./dynamic-inbox-row";

const QUICK_COUNTS = [3, 5, 10, 15] as const;
const MIN_COUNT = 1;
const MAX_COUNT = 15;

type SecretaryPhase = "setup" | "manual" | "active" | "done";

export function secretaryEntryKey(entry: DynInboxEntry): string {
  return `${entry.kind}:${entry.id}`;
}

function rowSelectedKey(entry: DynInboxEntry): string {
  return entry.kind === "notif" ? entry.item.issue_id ?? entry.item.id : entry.id;
}

export function chooseSecretaryEntries(entries: DynInboxEntry[], count: number): DynInboxEntry[] {
  return [...entries]
    .sort((a, b) => {
      const unreadDelta = Number(entryIsUnread(b)) - Number(entryIsUnread(a));
      if (unreadDelta !== 0) return unreadDelta;
      return a.time - b.time;
    })
    .slice(0, count);
}

interface SecretarySectionProps {
  entries: DynInboxEntry[];
  filterContext: SectionFilterContext;
  selectedKey: string | null;
  completedKeys: Set<string>;
  query?: string;
  dragHandle?: React.ReactNode;
  onSelect: (entry: DynInboxEntry) => void;
  onArchive: (entry: DynInboxEntry) => void;
  onResetCompleted: () => void;
}

export function SecretarySection({
  entries,
  selectedKey,
  completedKeys,
  query,
  dragHandle,
  onSelect,
  onArchive,
  onResetCompleted,
}: SecretarySectionProps) {
  const [phase, setPhase] = useState<SecretaryPhase>("setup");
  const [count, setCount] = useState(5);
  const [roundKeys, setRoundKeys] = useState<string[]>([]);
  const [manualKeys, setManualKeys] = useState<Set<string>>(() => new Set());

  const candidates = useMemo(
    () =>
      chooseSecretaryEntries(
        entries.filter(
          (entry) =>
            !completedKeys.has(secretaryEntryKey(entry)) &&
            entryMatchesQuery(entry, query ?? ""),
        ),
        MAX_COUNT,
      ),
    [entries, completedKeys, query],
  );
  const entryMap = useMemo(
    () => new Map(entries.map((entry) => [secretaryEntryKey(entry), entry])),
    [entries],
  );
  const activeEntries = useMemo(
    () =>
      roundKeys
        .filter((key) => !completedKeys.has(key))
        .map((key) => entryMap.get(key))
        .filter((entry): entry is DynInboxEntry => !!entry),
    [roundKeys, entryMap, completedKeys],
  );
  const manualSelectionCount = manualKeys.size;
  const remaining = activeEntries.length;

  useEffect(() => {
    setManualKeys((current) => {
      const valid = new Set(candidates.map(secretaryEntryKey));
      const next = new Set([...current].filter((key) => valid.has(key)));
      return next.size === current.size ? current : next;
    });
  }, [candidates]);

  useEffect(() => {
    if (phase === "active" && roundKeys.length > 0 && remaining === 0) {
      setPhase("done");
    }
  }, [phase, remaining, roundKeys.length]);

  const startAgentPick = () => {
    const picked = chooseSecretaryEntries(candidates, count);
    setRoundKeys(picked.map(secretaryEntryKey));
    setManualKeys(new Set());
    setPhase(picked.length === 0 ? "done" : "active");
  };

  const startManualPick = () => {
    setManualKeys(new Set());
    setPhase("manual");
  };

  const startManualRound = () => {
    setRoundKeys([...manualKeys]);
    setManualKeys(new Set());
    setPhase(manualKeys.size === 0 ? "done" : "active");
  };

  const resetRound = () => {
    setRoundKeys([]);
    setManualKeys(new Set());
    onResetCompleted();
    setPhase("setup");
  };

  const toggleManual = (entry: DynInboxEntry) => {
    const key = secretaryEntryKey(entry);
    setManualKeys((current) => {
      const next = new Set(current);
      if (next.has(key)) {
        next.delete(key);
      } else if (next.size < count) {
        next.add(key);
      }
      return next;
    });
  };

  return (
    <section className="overflow-hidden rounded-xl border border-border bg-card">
      <header className="flex items-center gap-2 border-b border-border px-3 py-2">
        {dragHandle}
        <div className="flex min-w-0 flex-1 items-center gap-2">
          <div className="flex size-7 items-center justify-center rounded-md bg-primary/10 text-primary">
            <Sparkles className="size-3.5" />
          </div>
          <div className="min-w-0">
            <h2 className="truncate text-xs font-bold uppercase tracking-wide text-muted-foreground">
              Secretary
            </h2>
            <p className="truncate text-[11px] text-muted-foreground/80">
              {phase === "active"
                ? `${remaining} left`
                : phase === "manual"
                  ? `${manualSelectionCount}/${count} selected`
                  : "Focused inbox batch"}
            </p>
          </div>
        </div>
        {phase !== "setup" && (
          <button
            type="button"
            className="rounded p-1 text-muted-foreground hover:bg-muted"
            onClick={resetRound}
            title="Reset Secretary"
          >
            <X className="size-3.5" />
          </button>
        )}
      </header>

      {phase === "setup" && (
        <div className="space-y-3 p-3">
          <div className="flex items-center gap-2">
            <Button
              type="button"
              size="icon"
              variant="outline"
              className="size-8"
              onClick={() => setCount((value) => Math.max(MIN_COUNT, value - 1))}
              aria-label="Decrease Secretary count"
            >
              <Minus className="size-3.5" />
            </Button>
            <div className="flex h-8 min-w-12 items-center justify-center rounded-md border border-border px-3 text-sm font-semibold">
              {count}
            </div>
            <Button
              type="button"
              size="icon"
              variant="outline"
              className="size-8"
              onClick={() => setCount((value) => Math.min(MAX_COUNT, value + 1))}
              aria-label="Increase Secretary count"
            >
              <Plus className="size-3.5" />
            </Button>
            <div className="ml-auto flex items-center gap-1">
              {QUICK_COUNTS.map((value) => (
                <button
                  key={value}
                  type="button"
                  className={`rounded-md px-2 py-1 text-xs font-medium ${
                    count === value
                      ? "bg-primary text-primary-foreground"
                      : "bg-muted text-muted-foreground hover:text-foreground"
                  }`}
                  onClick={() => setCount(value)}
                >
                  {value}
                </button>
              ))}
            </div>
          </div>
          <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
            <Button type="button" onClick={startAgentPick} disabled={candidates.length === 0}>
              <Sparkles className="size-4" /> Let agent choose
            </Button>
            <Button
              type="button"
              variant="outline"
              onClick={startManualPick}
              disabled={candidates.length === 0}
            >
              <Check className="size-4" /> I choose
            </Button>
          </div>
        </div>
      )}

      {phase === "manual" && (
        <div>
          <div className="flex items-center gap-2 border-b border-border/60 px-3 py-2">
            <Badge variant="secondary">{manualSelectionCount}/{count}</Badge>
            <Button
              type="button"
              size="sm"
              className="ml-auto"
              onClick={startManualRound}
              disabled={manualSelectionCount !== count}
            >
              <Play className="size-3.5" /> Start
            </Button>
          </div>
          <div className="divide-y divide-border/60">
            {candidates.length === 0 ? (
              <p className="px-3 py-3 text-xs text-muted-foreground">Nothing here right now.</p>
            ) : (
              candidates.map((entry) => {
                const key = secretaryEntryKey(entry);
                const checked = manualKeys.has(key);
                return (
                  <div key={key} className="relative">
                    <button
                      type="button"
                      className={`absolute left-7 top-1/2 z-10 flex size-5 -translate-y-1/2 items-center justify-center rounded-full border text-[10px] shadow-sm ${
                        checked
                          ? "border-primary bg-primary text-primary-foreground"
                          : "border-border bg-background text-transparent"
                      }`}
                      onClick={(event) => {
                        event.stopPropagation();
                        toggleManual(entry);
                      }}
                      aria-label={checked ? "Remove from Secretary" : "Add to Secretary"}
                    >
                      <Check className="size-3" />
                    </button>
                    <DynamicInboxRow
                      entry={entry}
                      isSelected={false}
                      onSelect={toggleManual}
                      onArchive={onArchive}
                    />
                  </div>
                );
              })
            )}
          </div>
        </div>
      )}

      {phase === "active" && (
        <div className="divide-y divide-border/60">
          {activeEntries.map((entry) => (
            <DynamicInboxRow
              key={secretaryEntryKey(entry)}
              entry={entry}
              isSelected={rowSelectedKey(entry) === selectedKey}
              onSelect={onSelect}
              onArchive={onArchive}
            />
          ))}
        </div>
      )}

      {phase === "done" && (
        <div className="overflow-hidden p-4 text-center">
          <div className="relative mx-auto mb-3 h-16 w-28">
            {[0, 1, 2].map((i) => (
              <span
                key={i}
                className="absolute bottom-1 inline-flex h-11 w-7 animate-bounce items-center justify-center rounded-full bg-yellow-300 text-[13px] font-bold text-yellow-950 shadow-sm"
                style={{ left: 16 + i * 28, animationDelay: `${i * 120}ms` }}
              >
                :)
              </span>
            ))}
            <PartyPopper className="absolute right-0 top-0 size-5 text-primary" />
          </div>
          <h3 className="text-sm font-semibold">Good work</h3>
          <p className="mt-1 text-xs text-muted-foreground">
            Start another Secretary round when you want the next focused batch.
          </p>
          <div className="mt-3 flex justify-center gap-2">
            <Button type="button" size="sm" onClick={resetRound}>
              <RotateCcw className="size-3.5" /> Next round
            </Button>
          </div>
        </div>
      )}
    </section>
  );
}
