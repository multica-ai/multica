"use client";

// TECH-3352 — per-row inbox controls for channel/DM rows, aligned with issue
// rows so the whole inbox behaves identically:
//   - Desktop: a single "..." (3-dot) dropdown menu (mark read/unread, remind,
//     archive). Same affordance as issue rows.
//   - Mobile: the exact same swipe surface as issue rows (swipe-archive +
//     swipe-left reveal + long-press drawer), reused from @multica/cerebro-inbox.
import { useState, type ReactNode } from "react";
import { Archive, BellOff, BellPlus, Mail, MailOpen, MoreHorizontal } from "lucide-react";
import { toast } from "sonner";
import type { Channel } from "@multica/core/types";
import { useMarkChannelRead } from "@multica/core/channels";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { useIsMobile } from "@multica/ui/hooks/use-mobile";
import {
  CerebroSwipeArchive,
  MobileRowActions,
  ReminderSheet,
  useCerebroInboxStrings,
  isMuted,
  toDateTimeLocalValue,
  nextBusinessDayNineAm,
} from "@multica/cerebro-inbox";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
} from "@multica/ui/components/ui/dropdown-menu";
import { cn } from "@multica/ui/lib/utils";
import { useMuteChannel, useUnmuteChannel, useMarkChannelUnread } from "./state-mutations";

export function CerebroChannelRowActions({
  channel,
  onArchive,
}: {
  channel: Channel;
  onArchive: () => void;
}) {
  const enabled = useFeatureFlag("cerebro_channel_row_actions");
  const isMobile = useIsMobile();
  const mute = useMuteChannel();
  const unmute = useUnmuteChannel();
  const markUnread = useMarkChannelUnread();
  const markRead = useMarkChannelRead();
  const strings = useCerebroInboxStrings();
  const [menuOpen, setMenuOpen] = useState(false);
  const [reminderOpen, setReminderOpen] = useState(false);
  const [customReminder, setCustomReminder] = useState(() =>
    toDateTimeLocalValue(nextBusinessDayNineAm()),
  );

  // Flag off → fall back to the plain swipe-archive the channel row had before.
  if (!enabled) return <CerebroSwipeArchive onArchive={onArchive} />;

  const muted = isMuted(channel.muted_until);
  const unread = channel.unread_count > 0;

  const toggleRead = () => {
    if (unread) markRead.mutate(channel.id);
    else markUnread.mutate(channel.id);
  };

  // "Remind me" = open the shared reminder sheet (presets + custom). Unmuting a
  // snoozed channel is the inverse and happens directly.
  const toggleMute = () => {
    if (muted) unmute.mutate(channel.id);
    else setReminderOpen(true);
  };

  const schedule = (mutedUntil: Date) => {
    mute.mutate(
      { id: channel.id, mutedUntil },
      { onError: () => toast.error("Kunne ikke sætte påmindelse") },
    );
    setReminderOpen(false);
    setCustomReminder(toDateTimeLocalValue(mutedUntil));
  };

  const reminderSheet = (
    <ReminderSheet
      isMobile={isMobile}
      customReminder={customReminder}
      open={reminderOpen}
      strings={strings}
      onCustomReminderChange={setCustomReminder}
      onOpenChange={setReminderOpen}
      onSchedule={schedule}
    />
  );

  if (isMobile) {
    return (
      <>
        <MobileRowActions
          muted={muted}
          unread={unread}
          onArchive={onArchive}
          onToggleRead={toggleRead}
          onToggleMute={toggleMute}
          strings={strings}
          renderDrawerMenu={(close) => (
            <div className="flex flex-col">
              <DrawerButton
                onClick={() => {
                  close();
                  toggleRead();
                }}
              >
                {unread ? <MailOpen className="size-4" /> : <Mail className="size-4" />}
                {unread ? "Markér som læst" : "Markér som ulæst"}
              </DrawerButton>
              <DrawerButton
                onClick={() => {
                  close();
                  toggleMute();
                }}
              >
                {muted ? <BellOff className="size-4" /> : <BellPlus className="size-4" />}
                {muted ? "Fjern påmindelse" : "Påmind mig"}
              </DrawerButton>
              <DrawerButton
                onClick={() => {
                  close();
                  onArchive();
                }}
              >
                <Archive className="size-4" /> Arkivér
              </DrawerButton>
            </div>
          )}
        />
        {reminderSheet}
      </>
    );
  }

  return (
    <>
      {/* Keep the trigger displayed while the menu is open — it anchors the
          popup, and losing :hover when the cursor moves toward the menu would
          collapse it to display:none, sending the menu to the top-left corner
          (TECH-3623). */}
      <DropdownMenu open={menuOpen} onOpenChange={setMenuOpen}>
        <DropdownMenuTrigger
          render={
            <button
              type="button"
              aria-label="Channel actions"
              onClick={(e) => e.stopPropagation()}
              className={cn(
                "absolute right-2 top-1/2 size-7 -translate-y-1/2 items-center justify-center rounded text-muted-foreground hover:bg-accent hover:text-foreground",
                menuOpen ? "inline-flex" : "hidden sm:group-hover:inline-flex",
              )}
            />
          }
        >
          <MoreHorizontal className="size-4" />
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" onClick={(e) => e.stopPropagation()}>
          <DropdownMenuItem onClick={toggleRead}>
            {unread ? <MailOpen className="size-4" /> : <Mail className="size-4" />}
            {unread ? "Markér som læst" : "Markér som ulæst"}
          </DropdownMenuItem>
          <DropdownMenuItem onClick={toggleMute}>
            {muted ? <BellOff className="size-4" /> : <BellPlus className="size-4" />}
            {muted ? "Fjern påmindelse" : "Påmind mig"}
          </DropdownMenuItem>
          <DropdownMenuSeparator />
          <DropdownMenuItem onClick={onArchive}>
            <Archive className="size-4" /> Arkivér
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
      {reminderSheet}
    </>
  );
}

// Plain full-width button for the mobile long-press drawer. Not a
// DropdownMenuItem — Base UI's Menu.Item no-ops onClick outside a menu context.
function DrawerButton({
  children,
  onClick,
}: {
  children: ReactNode;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={(e) => {
        e.stopPropagation();
        onClick();
      }}
      className="flex w-full items-center gap-3 px-4 py-3 text-left text-sm hover:bg-accent"
    >
      {children}
    </button>
  );
}
