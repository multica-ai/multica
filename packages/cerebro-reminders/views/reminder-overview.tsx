"use client";

import { useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { BellRing } from "lucide-react";

import { Spinner } from "@multica/ui/components/ui/spinner";
import { cn } from "@multica/ui/lib/utils";
import { useWorkspaceId } from "@multica/core/hooks";

import { remindersListOptions } from "../core/queries";
import type { Reminder } from "../core/types";
import { formatReminderSource, formatReminderWhen } from "../lib/format";
import { ReminderCard } from "./reminder-card";

/**
 * Reminder overview (FIR-394): the member's reminders as their own entity. Left
 * panel lists every pending/snoozed/fired reminder; clicking one opens its card
 * on the right. Matches the approved mockup in Multica light mode.
 */
export function ReminderOverview() {
  const wsId = useWorkspaceId();
  const { data: reminders = [], isLoading } = useQuery(remindersListOptions(wsId));

  const [selectedId, setSelectedId] = useState<string | null>(null);

  // Keep a valid selection: default to the first reminder, and recover if the
  // selected one is completed/deleted out from under us.
  useEffect(() => {
    const first = reminders[0];
    if (!first) {
      if (selectedId !== null) setSelectedId(null);
      return;
    }
    if (!selectedId || !reminders.some((r) => r.id === selectedId)) {
      setSelectedId(first.id);
    }
  }, [reminders, selectedId]);

  const selected = useMemo<Reminder | null>(
    () => reminders.find((r) => r.id === selectedId) ?? null,
    [reminders, selectedId],
  );

  return (
    <div className="mx-auto flex w-full max-w-5xl flex-col gap-7 p-6 md:flex-row md:items-start">
      {/* List panel */}
      <div className="w-full overflow-hidden rounded-xl border border-border bg-card shadow-sm md:w-[340px] md:shrink-0">
        <div className="flex items-center gap-2 border-b border-border px-4 py-4 text-[15px] font-semibold">
          <BellRing className="size-4 text-primary" />
          Mine reminders
          {reminders.length > 0 && (
            <span className="ml-auto rounded-full bg-primary/10 px-2.5 py-0.5 text-xs font-semibold text-primary">
              {reminders.length}
            </span>
          )}
        </div>

        {isLoading ? (
          <div className="flex items-center justify-center py-12">
            <Spinner />
          </div>
        ) : reminders.length === 0 ? (
          <div className="px-4 py-12 text-center text-sm text-muted-foreground">
            Du har ingen reminders.
            <br />
            Sæt en på en besked for at blive mindet om den her.
          </div>
        ) : (
          <ul>
            {reminders.map((r) => {
              const isActive = r.id === selectedId;
              return (
                <li key={r.id}>
                  <button
                    type="button"
                    onClick={() => setSelectedId(r.id)}
                    className={cn(
                      "w-full border-b border-border px-4 py-3.5 text-left transition-colors last:border-b-0 hover:bg-accent/40",
                      isActive && "bg-primary/5 shadow-[inset_3px_0_0_var(--color-primary)]",
                    )}
                  >
                    <div className="mb-1 text-xs font-semibold text-primary">
                      {formatReminderWhen(r.remind_at)}
                    </div>
                    <div className="mb-1 text-xs text-muted-foreground">
                      {formatReminderSource(r.conversation_kind, r.conversation_title)}
                    </div>
                    <div className="line-clamp-2 text-sm text-foreground">{r.text}</div>
                  </button>
                </li>
              );
            })}
          </ul>
        )}
      </div>

      {/* Opened reminder */}
      <div className="w-full md:flex-1">
        {selected ? (
          <ReminderCard key={selected.id} reminder={selected} />
        ) : (
          !isLoading && (
            <div className="rounded-xl border border-dashed border-border px-6 py-16 text-center text-sm text-muted-foreground">
              Vælg en reminder for at se beskeden.
            </div>
          )
        )}
      </div>
    </div>
  );
}
