"use client";

import { inboxReasonKind, type InboxReasonKind } from "@multica/core/inbox/sections";
import type { InboxItem } from "@multica/core/types";
import {
  AtSign,
  CheckCheck,
  CircleCheck,
  CircleHelp,
  MessageSquare,
  TriangleAlert,
  UserRound,
  type IconComponent,
} from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";
import { stripMentionMarkdown } from "../../issues/utils/strip-mention-markdown";
import { InboxDetailLabel, useTypeLabels } from "./inbox-detail-label";

const REASON_ICON: Record<InboxReasonKind, IconComponent> = {
  review: CheckCheck,
  question: CircleHelp,
  mention: AtSign,
  failed: TriangleAlert,
  assigned: UserRound,
  comment: MessageSquare,
  finished: CircleCheck,
};

// Only the two that ask for something to be fixed or answered take a colour;
// the rest stay the text colour so a full section does not read as an alarm.
const REASON_TONE: Partial<Record<InboxReasonKind, string>> = {
  question: "text-warning",
  failed: "text-destructive",
};

// Notifications whose own label says what changed, which is worth showing
// after the reason when no notification on the issue carries any text.
const CHANGE_TYPES: ReadonlySet<InboxItem["type"]> = new Set([
  "status_changed",
  "priority_changed",
  "assignee_changed",
  "unassigned",
  "start_date_changed",
  "due_date_changed",
  "reaction_added",
  "children_done",
]);

/**
 * Second line of a "Needs you" row: why the issue waits on the user, then the
 * latest thing said on it.
 *
 * `reason` is the notification that put the issue in the section; `latest` is
 * the row's newest notification and `preview` the newest that carries text.
 */
export function InboxReasonLabel({
  reason,
  latest,
  preview,
}: {
  reason: InboxItem;
  latest: InboxItem;
  preview?: InboxItem | null;
}) {
  const { t } = useT("inbox");
  const typeLabels = useTypeLabels();
  const kind = inboxReasonKind(reason);
  const Icon = kind ? REASON_ICON[kind] : null;
  const label = kind
    ? {
        review: t(($) => $.reasons.review),
        question: t(($) => $.reasons.question),
        mention: t(($) => $.reasons.mention),
        failed: t(($) => $.reasons.failed),
        assigned: t(($) => $.reasons.assigned),
        comment: t(($) => $.reasons.comment),
        finished: t(($) => $.reasons.finished),
      }[kind]
    : (typeLabels[reason.type] ?? reason.type);
  // The body is the comment as written. A comment that mentions someone opens
  // with the mention's link, which would fill the line before any of its words.
  // An issue title repeated as the body adds nothing under the title.
  const text = preview?.body ? stripMentionMarkdown(preview.body).trim() : "";
  const said = text && text !== latest.title.trim() ? text : null;

  return (
    <span className="flex min-w-0 items-center gap-1">
      <span
        data-reason={kind ?? reason.type}
        className={cn("inline-flex shrink-0 items-center gap-1", (kind && REASON_TONE[kind]) ?? "text-foreground")}
      >
        {Icon && <Icon className="h-3 w-3" />}
        {label}
      </span>
      {said ? (
        <>
          <span aria-hidden>·</span>
          <span className="min-w-0 truncate">{said}</span>
        </>
      ) : CHANGE_TYPES.has(latest.type) && latest.id !== reason.id ? (
        <>
          <span aria-hidden>·</span>
          <span className="min-w-0 truncate">
            <InboxDetailLabel item={latest} />
          </span>
        </>
      ) : null}
    </span>
  );
}
