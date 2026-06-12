"use client";

// TECH-3352 — per-row "remind me" (snooze) + "mark as unread/read" controls for
// channel/DM rows in the inbox, bringing them to parity with notification rows
// (CerebroInboxRowActions). Archive already existed; this adds the rest.
import { useState } from "react";
import { Archive, BellOff, BellPlus, Mail, MailOpen, MoreHorizontal } from "lucide-react";
import { toast } from "sonner";
import type { Channel } from "@multica/core/types";
import { useMarkChannelRead } from "@multica/core/channels";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import {
  CerebroSwipeArchive,
  isMuted,
  addHours,
  nextLocalNineAm,
  nextBusinessDayNineAm,
  nextMondayNineAm,
  toDateTimeLocalValue,
} from "@multica/cerebro-inbox";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
} from "@multica/ui/components/ui/dropdown-menu";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from "@multica/ui/components/ui/dialog";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { useMuteChannel, useUnmuteChannel, useMarkChannelUnread } from "./state-mutations";

export function CerebroChannelRowActions({
  channel,
  onArchive,
}: {
  channel: Channel;
  onArchive: () => void;
}) {
  const enabled = useFeatureFlag("cerebro_channel_row_actions");
  const mute = useMuteChannel();
  const unmute = useUnmuteChannel();
  const markUnread = useMarkChannelUnread();
  const markRead = useMarkChannelRead();
  const [customOpen, setCustomOpen] = useState(false);
  const [customValue, setCustomValue] = useState(() =>
    toDateTimeLocalValue(nextBusinessDayNineAm()),
  );

  // Flag off → fall back to the plain swipe-archive the channel row had before.
  if (!enabled) return <CerebroSwipeArchive onArchive={onArchive} />;

  const muted = isMuted(channel.muted_until);
  const isUnread = channel.unread_count > 0;

  const snooze = (d: Date) =>
    mute.mutate(
      { id: channel.id, mutedUntil: d },
      { onError: () => toast.error("Kunne ikke sætte påmindelse") },
    );

  const commitCustom = () => {
    const d = new Date(customValue);
    if (Number.isNaN(d.getTime()) || d.getTime() <= Date.now()) {
      toast.error("Vælg et tidspunkt i fremtiden");
      return;
    }
    snooze(d);
    setCustomOpen(false);
  };

  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger
          render={
            <button
              type="button"
              aria-label="Channel actions"
              onClick={(e) => e.stopPropagation()}
              className="absolute right-2 top-1/2 inline-flex size-7 -translate-y-1/2 items-center justify-center rounded text-muted-foreground opacity-100 hover:bg-accent hover:text-foreground sm:opacity-0 sm:group-hover:opacity-100"
            />
          }
        >
          <MoreHorizontal className="size-4" />
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" onClick={(e) => e.stopPropagation()}>
          {isUnread ? (
            <DropdownMenuItem onClick={() => markRead.mutate(channel.id)}>
              <MailOpen className="size-4" /> Markér som læst
            </DropdownMenuItem>
          ) : (
            <DropdownMenuItem onClick={() => markUnread.mutate(channel.id)}>
              <Mail className="size-4" /> Markér som ulæst
            </DropdownMenuItem>
          )}
          <DropdownMenuSeparator />
          <DropdownMenuItem disabled>
            <BellPlus className="size-4" /> Påmind mig
          </DropdownMenuItem>
          <DropdownMenuItem onClick={() => snooze(addHours(1))}>Om 1 time</DropdownMenuItem>
          <DropdownMenuItem onClick={() => snooze(nextLocalNineAm())}>I morgen kl. 9</DropdownMenuItem>
          <DropdownMenuItem onClick={() => snooze(nextBusinessDayNineAm())}>
            Næste hverdag kl. 9
          </DropdownMenuItem>
          <DropdownMenuItem onClick={() => snooze(nextMondayNineAm())}>
            Næste mandag kl. 9
          </DropdownMenuItem>
          <DropdownMenuItem onClick={() => setCustomOpen(true)}>Vælg tidspunkt…</DropdownMenuItem>
          {muted && (
            <DropdownMenuItem onClick={() => unmute.mutate(channel.id)}>
              <BellOff className="size-4" /> Fjern påmindelse
            </DropdownMenuItem>
          )}
          <DropdownMenuSeparator />
          <DropdownMenuItem onClick={onArchive}>
            <Archive className="size-4" /> Arkivér
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>

      {/* Keep the mobile swipe-right archive gesture the channel row had before. */}
      <CerebroSwipeArchive hideOnDesktop onArchive={onArchive} />

      <Dialog open={customOpen} onOpenChange={setCustomOpen}>
        <DialogContent className="sm:max-w-sm" onClick={(e) => e.stopPropagation()}>
          <DialogHeader>
            <DialogTitle>Påmind mig</DialogTitle>
          </DialogHeader>
          <div className="grid gap-2 py-1">
            <Label htmlFor="channel-remind">Tidspunkt</Label>
            <Input
              id="channel-remind"
              type="datetime-local"
              value={customValue}
              onChange={(e) => setCustomValue(e.target.value)}
            />
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setCustomOpen(false)}>
              Annullér
            </Button>
            <Button onClick={commitCustom}>Sæt påmindelse</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}
