"use client";

import { ArrowRight, BellRing, Bot, Check, Clock, User } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

import { Button } from "@multica/ui/components/ui/button";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";
import { useWorkspacePaths } from "@multica/core/paths";
import { useWorkspaceId } from "@multica/core/hooks";
import { useAuthStore } from "@multica/core/auth";
import { useChatStore } from "@multica/core/chat";
import { agentListOptions, memberListOptions } from "@multica/core/workspace/queries";
import { useNavigation } from "@multica/views/navigation";

import type { Reminder } from "../core/types";
import { useMarkReminderDone, useSnoozeReminder } from "../core/mutations";
import { formatReminderAnchor, formatReminderDue } from "../lib/format";
import { useCerebroReminderStrings } from "../strings";

// Snooze presets. Kept local so this package never imports the inbox.
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
 * about, with "go to" / "snooze" / "done" (FIR-394, mockup). The reminder links
 * BACK to the source; we never load the message to render the conversation, so a
 * reminder can't lock a thread (FIR-249).
 */
export function ReminderCard({ reminder }: { reminder: Reminder }) {
  const navigation = useNavigation();
  const paths = useWorkspacePaths();
  const wsId = useWorkspaceId();
  const currentUserId = useAuthStore((s) => s.user?.id ?? "");
  const snooze = useSnoozeReminder();
  const markDone = useMarkReminderDone();
  const openChat = useChatStore((s) => s.setOpen);
  const setActiveChatSession = useChatStore((s) => s.setActiveSession);
  const str = useCerebroReminderStrings();
  const { i18n } = useTranslation();

  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));

  const anchor = formatReminderAnchor(reminder, str);
  const preview = reminder.source_preview?.trim();
  const hasAnchor = reminder.anchor_type !== "none";

  // Show the recipient line only when the reminder is for someone OTHER than
  // the viewer (an agent, or a different member) — "for me" is the default and
  // would be noise on every card.
  const isAgent = reminder.recipient_type === "agent";
  const recipientName = isAgent
    ? agents.find((a) => a.id === reminder.recipient_id)?.name
    : members.find((m) => m.user_id === reminder.recipient_id)?.name;
  const showRecipient = isAgent || reminder.recipient_id !== currentUserId;

  // Navigate to the anchor: comment/issue resolve to a conversation_id (a
  // channel/DM thread or an issue, by kind); a project goes to the project page.
  const goToAnchor = () => {
    if (reminder.anchor_type === "chat_message" && reminder.chat_session_id) {
      // A chat is a floating panel, not a route: open it on the right session.
      setActiveChatSession(reminder.chat_session_id);
      openChat(true);
      return;
    }
    if (reminder.anchor_type === "project" && reminder.project_id) {
      navigation.push(paths.projectDetail(reminder.project_id));
      return;
    }
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
        onSuccess: () => toast.success(str.toast_snoozed),
        onError: () => toast.error(str.toast_snooze_failed),
      },
    );
  };

  const doDone = () => {
    markDone.mutate(reminder.id, {
      onSuccess: () => toast.success(str.toast_done),
      onError: () => toast.error(str.toast_done_failed),
    });
  };

  return (
    <div className="overflow-hidden rounded-xl border border-border bg-card shadow-sm">
      <div className="border-b border-border px-5 pb-4 pt-5">
        <div className="flex items-center gap-1.5 text-xs font-semibold uppercase tracking-wide text-primary">
          <BellRing className="size-3.5" />
          {str.card_badge}
        </div>
        <h2 className="mt-1.5 text-lg font-semibold text-foreground">{reminder.text}</h2>
        <p className="mt-1 flex items-center gap-1.5 text-sm text-muted-foreground">
          <Clock className="size-3.5" />
          {formatReminderDue(reminder.remind_at, str, i18n.language)}
        </p>
        {showRecipient && (
          <p className="mt-1 flex items-center gap-1.5 text-sm text-muted-foreground">
            {isAgent ? <Bot className="size-3.5" /> : <User className="size-3.5" />}
            {str.card_to_prefix}{" "}
            {recipientName ?? (isAgent ? str.recipient_agent_word : str.recipient_member_word)}
          </p>
        )}
      </div>

      {hasAnchor && (
        <div className="px-5 py-4">
          <p className="mb-2 text-xs text-muted-foreground">{str.card_about_label}</p>
          <div className="rounded-lg border border-border bg-muted/40 p-3.5">
            <p className="mb-1.5 text-xs font-medium text-muted-foreground">{anchor.label}</p>
            {(reminder.anchor_type === "comment" ||
              reminder.anchor_type === "chat_message") &&
              (preview ? (
                <p className="text-sm leading-relaxed text-foreground">{preview}</p>
              ) : (
                <p className="text-sm italic text-muted-foreground">
                  {str.card_source_gone}
                </p>
              ))}
          </div>
        </div>
      )}

      <div className="flex flex-wrap gap-2.5 px-5 pb-5 pt-1">
        {anchor.actionLabel && (
          <Button onClick={goToAnchor}>
            {anchor.actionLabel}
            <ArrowRight className="size-4" />
          </Button>
        )}

        <Popover>
          <PopoverTrigger
            render={
              <Button variant="outline" disabled={snooze.isPending}>
                {str.snooze_label}
              </Button>
            }
          />
          <PopoverContent align="start" className="w-44 p-1">
            <button
              type="button"
              className="w-full rounded-md px-3 py-2 text-left text-sm hover:bg-accent"
              onClick={() => doSnooze(addHours(1))}
            >
              {str.snooze_1h}
            </button>
            <button
              type="button"
              className="w-full rounded-md px-3 py-2 text-left text-sm hover:bg-accent"
              onClick={() => doSnooze(addHours(3))}
            >
              {str.snooze_3h}
            </button>
            <button
              type="button"
              className="w-full rounded-md px-3 py-2 text-left text-sm hover:bg-accent"
              onClick={() => doSnooze(tomorrowNineAm())}
            >
              {str.snooze_tomorrow_9}
            </button>
          </PopoverContent>
        </Popover>

        <Button variant="outline" onClick={doDone} disabled={markDone.isPending}>
          <Check className="size-4" />
          {str.done_label}
        </Button>
      </div>
    </div>
  );
}
