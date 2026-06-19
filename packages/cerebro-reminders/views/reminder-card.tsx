"use client";

import { ArrowRight, BellRing, Check, Clock } from "lucide-react";
import { toast } from "sonner";

import { Button } from "@multica/ui/components/ui/button";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";
import { useWorkspacePaths } from "@multica/core/paths";
import { useNavigation } from "@multica/views/navigation";

import type { Reminder } from "../core/types";
import { useMarkReminderDone, useSnoozeReminder } from "../core/mutations";
import { formatReminderDue, formatReminderSource } from "../lib/format";

// Snooze presets ("Udsæt"). Kept local so this package never imports the inbox.
function addHours(h: number): Date {
  const d = new Date();
  d.setHours(d.getHours() + h);
  return d;
}
function tomorrowNineAm(): Date {
  const d = new Date();
  d.setDate(d.getDate() + 1);
  d.setHours(9, 0, 0, 0);
  return d;
}

/**
 * The opened reminder = its own card showing the message you are being reminded
 * about, with "Gå til besked" / "Udsæt" / "Færdig" (FIR-394, mockup). The
 * reminder links BACK to the source; we never load the message to render the
 * conversation, so a reminder can't lock a thread (FIR-249).
 */
export function ReminderCard({ reminder }: { reminder: Reminder }) {
  const navigation = useNavigation();
  const paths = useWorkspacePaths();
  const snooze = useSnoozeReminder();
  const markDone = useMarkReminderDone();

  const source = formatReminderSource(reminder.conversation_kind, reminder.conversation_title);
  const preview = reminder.source_preview?.trim();

  const canGoToMessage = Boolean(reminder.conversation_id);

  const goToMessage = () => {
    if (!reminder.conversation_id) return;
    const kind = reminder.conversation_kind;
    const base =
      kind === "dm" || kind === "channel"
        ? paths.channelDetail(reminder.conversation_id)
        : paths.issueDetail(reminder.conversation_id);
    // Carry the source message so the conversation can highlight/scroll to it
    // (mirrors the inbox highlightCommentId deep-link param).
    const href = reminder.message_id
      ? `${base}?highlight=${encodeURIComponent(reminder.message_id)}`
      : base;
    navigation.push(href);
  };

  const doSnooze = (until: Date) => {
    snooze.mutate(
      { id: reminder.id, until: until.toISOString() },
      {
        onSuccess: () => toast.success("Reminder udsat"),
        onError: () => toast.error("Kunne ikke udsætte reminder"),
      },
    );
  };

  const doDone = () => {
    markDone.mutate(reminder.id, {
      onSuccess: () => toast.success("Reminder markeret færdig"),
      onError: () => toast.error("Kunne ikke markere færdig"),
    });
  };

  return (
    <div className="overflow-hidden rounded-xl border border-border bg-card shadow-sm">
      <div className="border-b border-border px-5 pb-4 pt-5">
        <div className="flex items-center gap-1.5 text-xs font-semibold uppercase tracking-wide text-primary">
          <BellRing className="size-3.5" />
          Reminder
        </div>
        <h2 className="mt-1.5 text-lg font-semibold text-foreground">{reminder.text}</h2>
        <p className="mt-1 flex items-center gap-1.5 text-sm text-muted-foreground">
          <Clock className="size-3.5" />
          {formatReminderDue(reminder.remind_at)}
        </p>
      </div>

      <div className="px-5 py-4">
        <p className="mb-2 text-xs text-muted-foreground">Beskeden du bliver mindet om:</p>
        <div className="rounded-lg border border-border bg-muted/40 p-3.5">
          <p className="mb-1.5 text-xs font-medium text-muted-foreground">{source}</p>
          {preview ? (
            <p className="text-sm leading-relaxed text-foreground">{preview}</p>
          ) : (
            <p className="text-sm italic text-muted-foreground">
              Kilde-beskeden er ikke længere tilgængelig.
            </p>
          )}
        </div>
      </div>

      <div className="flex flex-wrap gap-2.5 px-5 pb-5">
        <Button onClick={goToMessage} disabled={!canGoToMessage}>
          Gå til besked
          <ArrowRight className="size-4" />
        </Button>

        <Popover>
          <PopoverTrigger
            render={
              <Button variant="outline" disabled={snooze.isPending}>
                Udsæt
              </Button>
            }
          />
          <PopoverContent align="start" className="w-44 p-1">
            <button
              type="button"
              className="w-full rounded-md px-3 py-2 text-left text-sm hover:bg-accent"
              onClick={() => doSnooze(addHours(1))}
            >
              Om 1 time
            </button>
            <button
              type="button"
              className="w-full rounded-md px-3 py-2 text-left text-sm hover:bg-accent"
              onClick={() => doSnooze(addHours(3))}
            >
              Om 3 timer
            </button>
            <button
              type="button"
              className="w-full rounded-md px-3 py-2 text-left text-sm hover:bg-accent"
              onClick={() => doSnooze(tomorrowNineAm())}
            >
              I morgen kl. 9
            </button>
          </PopoverContent>
        </Popover>

        <Button variant="outline" onClick={doDone} disabled={markDone.isPending}>
          <Check className="size-4" />
          Færdig
        </Button>
      </div>
    </div>
  );
}
