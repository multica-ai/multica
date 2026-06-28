"use client";

import { useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { BellRing, Plus } from "lucide-react";

import { Button } from "@multica/ui/components/ui/button";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { cn } from "@multica/ui/lib/utils";
import { PageHeader } from "@multica/views/layout/page-header";
import { useWorkspaceId } from "@multica/core/hooks";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";

import { useTranslation } from "react-i18next";

import { remindersListOptions } from "../core/queries";
import type { Reminder } from "../core/types";
import { formatReminderAnchor, formatReminderWhen } from "../lib/format";
import { useCerebroReminderStrings } from "../strings";
import { ReminderCard } from "./reminder-card";
import { CreateReminderSheet } from "./create-reminder-sheet";

/**
 * Reminder overview (FIR-394): the member's reminders as their own entity. Left
 * panel lists every pending/snoozed/fired reminder; clicking one opens its card
 * on the right. Matches the approved mockup in Multica light mode.
 */
export function ReminderOverview() {
  const wsId = useWorkspaceId();
  // FIR-394 (Jesper) — the standalone reminder page is toggleable. When the page
  // flag is off, hitting /reminders directly renders nothing (the nav entry is
  // already hidden); the "remind me" actions are unaffected.
  const pageEnabled = useFeatureFlag("cerebro_reminders_page");
  const s = useCerebroReminderStrings();
  const { i18n } = useTranslation();
  const { data: reminders = [], isLoading } = useQuery(remindersListOptions(wsId));

  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [createOpen, setCreateOpen] = useState(false);

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

  if (!pageEnabled) return null;

  return (
    <div className="flex h-full min-h-0 flex-col bg-background">
      <PageHeader>
        <BellRing className="mr-2 size-4 text-muted-foreground" />
        <h1 className="text-base font-semibold">{s.overview_title}</h1>
        {reminders.length > 0 && (
          <span className="ml-2 rounded-full bg-muted px-2 text-xs font-semibold text-muted-foreground">
            {reminders.length}
          </span>
        )}
        <Button
          variant="ghost"
          size="sm"
          className="ml-auto gap-1"
          onClick={() => setCreateOpen(true)}
        >
          <Plus className="size-3.5" />
          {s.overview_new}
        </Button>
      </PageHeader>

      <div className="min-h-0 flex-1 overflow-y-auto">
        <div className="mx-auto flex w-full max-w-4xl flex-col gap-7 p-6 md:flex-row md:items-start">
          {/* List panel */}
          <div className="w-full overflow-hidden rounded-xl border border-border bg-card shadow-sm md:w-[340px] md:shrink-0">
            {isLoading ? (
              <div className="flex flex-col gap-2 p-3">
                {Array.from({ length: 5 }).map((_, i) => (
                  <Skeleton key={i} className="h-16 w-full rounded-lg" />
                ))}
              </div>
            ) : reminders.length === 0 ? (
              <div className="px-4 py-12 text-center text-sm text-muted-foreground">
                {s.overview_empty_title}
                <br />
                {s.overview_empty_hint}
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
                          "relative w-full border-b border-border px-4 py-3.5 text-left transition-colors last:border-b-0 hover:bg-accent/40",
                          isActive && "bg-accent",
                        )}
                      >
                        {isActive && (
                          <span className="absolute bottom-3 left-0 top-3 w-[3px] rounded-r bg-brand" />
                        )}
                        <div className="mb-1 text-xs font-semibold text-primary">
                          {formatReminderWhen(r.remind_at, s, i18n.language)}
                        </div>
                        <div className="mb-1 text-xs text-muted-foreground">
                          {formatReminderAnchor(r, s).label}
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
                  {s.overview_select_prompt}
                </div>
              )
            )}
          </div>
        </div>
      </div>

      <CreateReminderSheet open={createOpen} onOpenChange={setCreateOpen} />
    </div>
  );
}
