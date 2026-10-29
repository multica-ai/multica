"use client";

import { CircleDot, MessagesSquare } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { useWorkspacePaths } from "@multica/core/paths";
import type { ChatMessage, ChatMessageIssue, ChatSession } from "@multica/core/types";
import { AppLink } from "../../navigation";
import { useT } from "../../i18n";
import { StatusIcon } from "../../issues/components/status-icon";

const CHIP =
  "inline-flex h-6 max-w-64 items-center gap-1.5 rounded-md border bg-background px-2 text-caption text-muted-foreground transition-colors hover:bg-accent hover:text-foreground";

/**
 * What a message led to, under the message itself: its thread and the issue it
 * became, plus — on hover — the actions that create them.
 */
export function TeamMessageExtras({
  message,
  thread,
  threadHref,
  issues,
  canReply,
  canCreateIssue,
  busy,
  onReply,
  onCreateIssue,
}: {
  message: ChatMessage;
  thread: ChatSession | null;
  threadHref: string | null;
  issues: readonly ChatMessageIssue[];
  canReply: boolean;
  canCreateIssue: boolean;
  busy: boolean;
  onReply: () => void;
  onCreateIssue: () => void;
}) {
  const { t } = useT("team");
  const wsPaths = useWorkspacePaths();
  const showReply = canReply && !thread;
  const showCreate = canCreateIssue && issues.length === 0;
  if (!thread && issues.length === 0 && !showReply && !showCreate) return null;

  return (
    <div
      className={cn(
        "flex flex-wrap items-center gap-1.5",
        message.role === "user" && "justify-end",
      )}
    >
      {issues.map((issue) => (
        <AppLink key={issue.issue_id} href={wsPaths.issueDetail(issue.issue_id)} className={CHIP}>
          <StatusIcon status={issue.status} className="size-3.5 shrink-0" />
          <span className="shrink-0 font-medium text-foreground">{issue.identifier}</span>
          <span className="truncate">{issue.title}</span>
        </AppLink>
      ))}
      {thread && threadHref && (
        <AppLink href={threadHref} className={CHIP}>
          <MessagesSquare className="size-3.5 shrink-0" />
          {t(($) => $.chat.thread.replies, { count: thread.thread_reply_count ?? 0 })}
          {(thread.unread_count ?? 0) > 0 && (
            <span
              aria-label={t(($) => $.panel.unread)}
              className="size-1.5 shrink-0 rounded-full bg-[oklch(0.62_0.14_18)]"
            />
          )}
        </AppLink>
      )}
      {(showReply || showCreate) && (
        <div className="flex items-center gap-0.5 opacity-0 transition-opacity focus-within:opacity-100 group-hover/message:opacity-100 [@media(hover:none)]:opacity-100">
          {showReply && (
            <Button
              size="xs"
              variant="ghost"
              className="text-muted-foreground"
              disabled={busy}
              onClick={onReply}
            >
              <MessagesSquare className="size-3.5" />
              {t(($) => $.chat.thread.reply)}
            </Button>
          )}
          {showCreate && (
            <Button
              size="xs"
              variant="ghost"
              className="text-muted-foreground"
              disabled={busy}
              onClick={onCreateIssue}
            >
              <CircleDot className="size-3.5" />
              {t(($) => $.chat.issue.create)}
            </Button>
          )}
        </div>
      )}
    </div>
  );
}
