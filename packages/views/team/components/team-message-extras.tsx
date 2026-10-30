"use client";

import type { ReactNode } from "react";
import { ListTodo, MessagesSquare } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { useWorkspacePaths } from "@multica/core/paths";
import type { ChatMessageIssue } from "@multica/core/types";
import { AppLink } from "../../navigation";
import { useT } from "../../i18n";
import { StatusIcon } from "../../issues/components/status-icon";

const CHIP =
  "inline-flex h-6 max-w-72 items-center gap-1.5 rounded-md border bg-background px-2 text-caption text-muted-foreground transition-colors hover:bg-accent hover:text-foreground";

function formatClock(iso: string): string {
  return new Date(iso).toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" });
}

/** "👤👤 3 replies  Last reply 14:41" under a message that has a thread. */
export function ThreadSummaryLink({
  href,
  count,
  lastReplyAt,
  avatars,
  active = false,
  unread = false,
}: {
  href: string;
  count: number;
  lastReplyAt?: string | null;
  avatars?: ReactNode;
  active?: boolean;
  unread?: boolean;
}) {
  const { t } = useT("team");
  return (
    <AppLink
      href={href}
      aria-current={active ? "true" : undefined}
      className={cn(
        "mt-1 inline-flex h-7 max-w-full items-center gap-2 rounded-md border border-transparent px-1.5 text-caption transition-colors hover:border-border hover:bg-background",
        active && "border-brand/30 bg-brand/5 hover:border-brand/30 hover:bg-brand/5",
      )}
    >
      {avatars && <span className="flex shrink-0 -space-x-1">{avatars}</span>}
      <span className="font-medium text-brand">{t(($) => $.chat.thread.replies, { count })}</span>
      {lastReplyAt && (
        <span className="truncate text-muted-foreground">
          {t(($) => $.chat.thread.last_reply, { time: formatClock(lastReplyAt) })}
        </span>
      )}
      {unread && <span aria-label={t(($) => $.panel.unread)} className="size-1.5 shrink-0 rounded-full bg-brand" />}
    </AppLink>
  );
}

/** Hover toolbar on a message: reply in thread, turn it into an issue. */
export function MessageHoverActions({
  onReply,
  onCreateIssue,
  disabled = false,
}: {
  onReply?: () => void;
  onCreateIssue?: () => void;
  disabled?: boolean;
}) {
  const { t } = useT("team");
  if (!onReply && !onCreateIssue) return null;
  const button =
    "flex size-7 items-center justify-center rounded text-muted-foreground transition-colors hover:bg-surface-hover hover:text-foreground disabled:opacity-50";
  return (
    <div className="absolute -top-3 right-0 z-10 hidden items-center rounded-md border bg-background p-0.5 shadow-sm group-hover/message:flex focus-within:flex [@media(hover:none)]:flex">
      {onReply && (
        <button
          type="button"
          className={button}
          disabled={disabled}
          onClick={onReply}
          aria-label={t(($) => $.chat.thread.reply)}
          title={t(($) => $.chat.thread.reply)}
        >
          <MessagesSquare className="size-4" />
        </button>
      )}
      {onCreateIssue && (
        <button
          type="button"
          className={button}
          disabled={disabled}
          onClick={onCreateIssue}
          aria-label={t(($) => $.chat.issue.create)}
          title={t(($) => $.chat.issue.create)}
        >
          <ListTodo className="size-4" />
        </button>
      )}
    </div>
  );
}

/**
 * What a message led to, under the message itself: the issue it became and
 * its thread, plus — on hover — the actions that create them.
 */
export function TeamMessageExtras({
  thread,
  issues,
  onReply,
  onCreateIssue,
  busy,
}: {
  thread: ReactNode;
  issues: readonly ChatMessageIssue[];
  onReply?: () => void;
  onCreateIssue?: () => void;
  busy: boolean;
}) {
  const wsPaths = useWorkspacePaths();
  return (
    <>
      {issues.length > 0 && (
        <div className="mt-1 flex flex-wrap items-center gap-1.5">
          {issues.map((issue) => (
            <AppLink key={issue.issue_id} href={wsPaths.issueDetail(issue.issue_id)} className={CHIP}>
              <StatusIcon status={issue.status} className="size-3.5 shrink-0" />
              <span className="shrink-0 font-medium text-foreground">{issue.identifier}</span>
              <span className="truncate">{issue.title}</span>
            </AppLink>
          ))}
        </div>
      )}
      {thread}
      <MessageHoverActions onReply={onReply} onCreateIssue={onCreateIssue} disabled={busy} />
    </>
  );
}
