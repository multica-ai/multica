"use client";

// TECH-3489 — per-row 3-dot actions menu for agent chat-session rows in the
// inbox, bringing them to parity with notification rows (CerebroInboxRowActions)
// and channel/DM rows (CerebroChannelRowActions). Chats previously only had a
// swipe / hover archive button. This adds the same hover menu (desktop) +
// always-visible tap menu (mobile) with the chat-appropriate actions, and lets
// an archived chat be reopened ("unarchive") so the conversation can continue.
//
// The component owns every action through the shared chat mutations
// (mark-as-read, rename, convert-to-issue, archive, unarchive, delete); the
// host only supplies `isArchivedView` (so the menu offers "reopen" instead of
// "archive") and an `onCleared` callback to drop the row from the host's
// selection after archive/unarchive/delete. Snooze ("remind me") and
// mark-as-unread are intentionally absent — chat sessions have no backend
// support for either today (see TECH-3489).
import { useState, type ReactNode } from "react";
import {
  Archive,
  ArchiveRestore,
  BellOff,
  BellPlus,
  FileText,
  Mail,
  MailOpen,
  MoreHorizontal,
  Pencil,
  Trash2,
} from "lucide-react";
import { toast } from "sonner";
import type { ChatSession } from "@multica/core/types";
import { useWorkspacePaths } from "@multica/core/paths";
import { useMarkChatSessionRead, useDeleteChatSession } from "@multica/core/chat/mutations";
import { useNavigation } from "@multica/views/navigation";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { useIsMobile } from "@multica/ui/hooks/use-mobile";
import {
  CerebroSwipeArchive,
  CerebroUnarchiveAction,
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
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from "@multica/ui/components/ui/dialog";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import {
  useUpdateChatSession,
  useConvertChatSessionToIssue,
  useMuteChat,
  useUnmuteChat,
  useMarkChatUnread,
} from "../../mutations";
import { useCerebroChatHeaderStrings } from "../../strings";

export function CerebroChatSessionRowActions({
  session,
  isArchivedView = false,
  onCleared,
}: {
  session: ChatSession;
  /** Archived view → the menu offers "reopen" (unarchive) instead of "archive". */
  isArchivedView?: boolean;
  /** Clear the host's selection if this row was the open one, after
   *  archive / unarchive / delete removes it from the visible list. */
  onCleared?: () => void;
}) {
  const enabled = useFeatureFlag("cerebro_chat_row_actions");
  const s = useCerebroChatHeaderStrings();
  const inboxStrings = useCerebroInboxStrings();
  const isMobile = useIsMobile();
  const wsPaths = useWorkspacePaths();
  const router = useNavigation();
  const update = useUpdateChatSession();
  const convert = useConvertChatSessionToIssue();
  const markRead = useMarkChatSessionRead();
  const markUnread = useMarkChatUnread();
  const mute = useMuteChat();
  const unmute = useUnmuteChat();
  const remove = useDeleteChatSession();

  const [renameOpen, setRenameOpen] = useState(false);
  const [renameValue, setRenameValue] = useState("");
  const [confirm, setConfirm] = useState<"convert" | "delete" | null>(null);
  const [reminderOpen, setReminderOpen] = useState(false);
  const [customReminder, setCustomReminder] = useState(() =>
    toDateTimeLocalValue(nextBusinessDayNineAm()),
  );

  const muted = isMuted(session.muted_until);

  // Swipe-left / drawer read toggle: mark a read chat unread, or a read chat
  // read. Mark-as-unread is meaningless on an archived chat, so skip it there.
  const toggleRead = () => {
    if (session.has_unread) markRead.mutate(session.id);
    else if (!isArchivedView) markUnread.mutate(session.id);
  };

  // "Remind me" opens the shared reminder sheet (presets + custom). Removing a
  // reminder on a snoozed chat is the inverse and happens directly.
  const toggleMute = () => {
    if (muted) unmute.mutate(session.id);
    else setReminderOpen(true);
  };

  const scheduleReminder = (mutedUntil: Date) => {
    mute.mutate(
      { id: session.id, mutedUntil },
      { onError: () => toast.error("Kunne ikke sætte påmindelse") },
    );
    setReminderOpen(false);
    setCustomReminder(toDateTimeLocalValue(mutedUntil));
  };

  function archive() {
    update.mutate(
      { sessionId: session.id, status: "archived" },
      { onError: () => toast.error(s.toast_archive_failed) },
    );
    onCleared?.();
  }

  function reopen() {
    update.mutate(
      { sessionId: session.id, status: "active" },
      { onError: () => toast.error(s.toast_unarchive_failed) },
    );
    onCleared?.();
  }

  // Flag off → keep the plain swipe/hover archive (or unarchive) the chat row
  // had before, so disabling the feature is a clean revert.
  if (!enabled) {
    return isArchivedView ? (
      <CerebroUnarchiveAction onUnarchive={reopen} />
    ) : (
      <CerebroSwipeArchive onArchive={archive} />
    );
  }

  function startRename() {
    setRenameValue(session.title ?? "");
    setRenameOpen(true);
  }

  function commitRename() {
    const next = renameValue.trim();
    if (next && next !== session.title) {
      update.mutate({ sessionId: session.id, title: next });
    }
    setRenameOpen(false);
  }

  function onConvertConfirm() {
    convert.mutate(session.id, {
      onSuccess: (resp) => {
        setConfirm(null);
        toast.success(s.toast_converted);
        router.push(wsPaths.issueDetail(resp.identifier));
      },
      onError: () => {
        setConfirm(null);
        toast.error(s.toast_convert_failed);
      },
    });
  }

  function onDeleteConfirm() {
    remove.mutate(session.id, {
      onSuccess: () => {
        setConfirm(null);
        onCleared?.();
      },
      onError: () => {
        setConfirm(null);
        toast.error(s.toast_delete_failed);
      },
    });
  }

  // Long-press drawer (mobile) — the same action set as the desktop dropdown,
  // rendered as full-width buttons so it matches issue/channel/DM rows exactly.
  const drawerMenu = (close: () => void) => (
    <div className="flex flex-col">
      {session.has_unread ? (
        <DrawerButton onClick={() => { close(); markRead.mutate(session.id); }}>
          <MailOpen className="size-4" /> {s.mark_read}
        </DrawerButton>
      ) : (
        !isArchivedView && (
          <DrawerButton onClick={() => { close(); markUnread.mutate(session.id); }}>
            <Mail className="size-4" /> {s.mark_unread}
          </DrawerButton>
        )
      )}
      {!isArchivedView && (
        <DrawerButton onClick={() => { close(); toggleMute(); }}>
          {muted ? <BellOff className="size-4" /> : <BellPlus className="size-4" />}
          {muted ? s.remind_remove : s.remind}
        </DrawerButton>
      )}
      <DrawerButton onClick={() => { close(); startRename(); }}>
        <Pencil className="size-4" /> {s.rename}
      </DrawerButton>
      <DrawerButton onClick={() => { close(); setConfirm("convert"); }}>
        <FileText className="size-4" /> {s.convert_to_issue}
      </DrawerButton>
      {isArchivedView ? (
        <DrawerButton onClick={() => { close(); reopen(); }}>
          <ArchiveRestore className="size-4" /> {s.unarchive}
        </DrawerButton>
      ) : (
        <DrawerButton onClick={() => { close(); archive(); }}>
          <Archive className="size-4" /> {s.archive}
        </DrawerButton>
      )}
      <DrawerButton onClick={() => { close(); setConfirm("delete"); }}>
        <Trash2 className="size-4" /> {s.delete}
      </DrawerButton>
    </div>
  );

  return (
    <>
      {isMobile ? (
        // Mobile: the exact same swipe surface (swipe-archive + swipe-left
        // reveal + long-press drawer) as issue/channel/DM rows, reused from
        // @multica/cerebro-inbox so all four row types behave identically.
        <MobileRowActions
          muted={muted}
          unread={!!session.has_unread}
          onArchive={isArchivedView ? reopen : archive}
          onToggleRead={toggleRead}
          onToggleMute={toggleMute}
          strings={inboxStrings}
          renderDrawerMenu={drawerMenu}
        />
      ) : (
        // Desktop: a single hover "..." dropdown, matching issue/channel rows.
        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <button
                type="button"
                aria-label={s.more_actions}
                onClick={(e) => e.stopPropagation()}
                className="absolute right-2 top-1/2 hidden size-7 -translate-y-1/2 items-center justify-center rounded text-muted-foreground hover:bg-accent hover:text-foreground sm:group-hover:inline-flex"
              />
            }
          >
            <MoreHorizontal className="size-4" />
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" onClick={(e) => e.stopPropagation()}>
            {session.has_unread ? (
              <DropdownMenuItem onClick={() => markRead.mutate(session.id)}>
                <MailOpen className="size-4" /> {s.mark_read}
              </DropdownMenuItem>
            ) : (
              !isArchivedView && (
                <DropdownMenuItem onClick={() => markUnread.mutate(session.id)}>
                  <Mail className="size-4" /> {s.mark_unread}
                </DropdownMenuItem>
              )
            )}
            {!isArchivedView && (
              <DropdownMenuItem onClick={toggleMute}>
                {muted ? <BellOff className="size-4" /> : <BellPlus className="size-4" />}
                {muted ? s.remind_remove : s.remind}
              </DropdownMenuItem>
            )}
            <DropdownMenuItem onClick={startRename}>
              <Pencil className="size-4" /> {s.rename}
            </DropdownMenuItem>
            <DropdownMenuItem onClick={() => setConfirm("convert")}>
              <FileText className="size-4" /> {s.convert_to_issue}
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            {isArchivedView ? (
              <DropdownMenuItem onClick={reopen}>
                <ArchiveRestore className="size-4" /> {s.unarchive}
              </DropdownMenuItem>
            ) : (
              <DropdownMenuItem onClick={archive}>
                <Archive className="size-4" /> {s.archive}
              </DropdownMenuItem>
            )}
            <DropdownMenuItem variant="destructive" onClick={() => setConfirm("delete")}>
              <Trash2 className="size-4" /> {s.delete}
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      )}

      {/* "Remind me" time picker (presets + custom), shared with channel/DM rows. */}
      <ReminderSheet
        isMobile={isMobile}
        customReminder={customReminder}
        open={reminderOpen}
        strings={inboxStrings}
        onCustomReminderChange={setCustomReminder}
        onOpenChange={setReminderOpen}
        onSchedule={scheduleReminder}
      />

      <Dialog open={renameOpen} onOpenChange={setRenameOpen}>
        <DialogContent className="sm:max-w-sm" onClick={(e) => e.stopPropagation()}>
          <DialogHeader>
            <DialogTitle>{s.rename_dialog_title}</DialogTitle>
          </DialogHeader>
          <div className="py-1">
            <Input
              autoFocus
              value={renameValue}
              placeholder={s.rename_placeholder}
              onChange={(e) => setRenameValue(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter") {
                  e.preventDefault();
                  commitRename();
                }
              }}
              aria-label={s.rename_dialog_title}
            />
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setRenameOpen(false)}>
              {s.cancel}
            </Button>
            <Button onClick={commitRename}>{s.rename_save}</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <AlertDialog
        open={confirm === "convert"}
        onOpenChange={(o) => {
          if (!o && !convert.isPending) setConfirm(null);
        }}
      >
        <AlertDialogContent onClick={(e) => e.stopPropagation()}>
          <AlertDialogHeader>
            <AlertDialogTitle>{s.convert_dialog_title}</AlertDialogTitle>
            <AlertDialogDescription>{s.convert_dialog_description}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={convert.isPending}>{s.cancel}</AlertDialogCancel>
            <AlertDialogAction onClick={onConvertConfirm} disabled={convert.isPending}>
              {convert.isPending ? s.convert_confirming : s.convert_confirm}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <AlertDialog
        open={confirm === "delete"}
        onOpenChange={(o) => {
          if (!o && !remove.isPending) setConfirm(null);
        }}
      >
        <AlertDialogContent onClick={(e) => e.stopPropagation()}>
          <AlertDialogHeader>
            <AlertDialogTitle>{s.delete_dialog_title}</AlertDialogTitle>
            <AlertDialogDescription>{s.delete_dialog_description}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={remove.isPending}>{s.cancel}</AlertDialogCancel>
            <AlertDialogAction
              onClick={onDeleteConfirm}
              disabled={remove.isPending}
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
            >
              {remove.isPending ? s.delete_confirming : s.delete_confirm}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}

// Plain full-width button for the mobile long-press drawer. Not a
// DropdownMenuItem — Base UI's Menu.Item no-ops onClick outside a menu context.
// Mirrors the same helper in @multica/cerebro-channels for a consistent drawer.
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
