"use client";

import { useIssueStatuses } from "@multica/core/issue-statuses/hooks";
import { StatusIcon } from "../../issues/components";
import {
  IssueAgentActivityIndicator,
} from "../../issues/components/issue-agent-activity-indicator";
import { ActorAvatar } from "../../common/actor-avatar";
import { DeferredTooltip } from "../../common/deferred-tooltip";
import { Archive, ArchiveRestore, Clock } from "@multica/ui/icons";
import type { InboxSectionRow } from "@multica/core/inbox/sections";
import type { InboxItem } from "@multica/core/types";
import { useActorName } from "@multica/core/workspace/hooks";
import type { InboxView } from "./inbox-view";
import { InboxDetailLabel, useTypeLabels } from "./inbox-detail-label";
import { InboxReasonLabel } from "./inbox-reason-label";
import { formatSnoozeTime } from "./inbox-snooze-time";
import {
  getInboxDisplayTitle,
  isAutopilotQuotaNotice,
} from "./inbox-display";
import { useInboxContextMenu } from "./inbox-context-menu";
import { useStatusLabel } from "../../issues/utils/status-label";
import { InboxRowMenu } from "./inbox-row-menu";
import { handleRowActivationKey } from "../../common/row-actions-menu";
import { useLocale, useT } from "../../i18n";
import { paths, useWorkspaceSlug } from "@multica/core/paths";
import { resolveClickIntent, useIntentNavigate } from "../../navigation";

// Hook returning a localized relative-time formatter — the i18n equivalent
// of the previous static `timeAgo` function. Returning a function (rather
// than a string) keeps call-site usage identical: `timeAgo(dateStr)`.
export function useTimeAgo() {
  const { t } = useT("inbox");
  return (dateStr: string): string => {
    const diff = Date.now() - new Date(dateStr).getTime();
    const minutes = Math.floor(diff / 60000);
    if (minutes < 1) return t(($) => $.list.time.just_now);
    if (minutes < 60) return t(($) => $.list.time.minutes, { count: minutes });
    const hours = Math.floor(minutes / 60);
    if (hours < 24) return t(($) => $.list.time.hours, { count: hours });
    const days = Math.floor(hours / 24);
    return t(($) => $.list.time.days, { count: days });
  };
}

// The two below are components so that only the rows that show them
// subscribe to what they read: the workspace's people, and the locale.

function InboxActorName({ actorType, actorId }: { actorType: string; actorId: string }) {
  const { getActorName } = useActorName();
  return <span className="shrink-0">{getActorName(actorType, actorId)}</span>;
}

function InboxSnoozeTime({ until }: { until: string }) {
  const locale = useLocale();
  return (
    <span className="text-caption text-muted-foreground">
      {formatSnoozeTime(new Date(until), new Date(), locale)}
    </span>
  );
}

/** The section a row is listed under, which decides how much of it shows. */
export type InboxRowSection = "needs_you" | "updates" | "snoozed";

export function InboxListItem({
  item,
  view,
  isSelected,
  onClick,
  onAction,
  section,
  row,
}: {
  item: InboxItem;
  view: InboxView;
  isSelected: boolean;
  onClick: () => void;
  // Archive in the main list, unarchive in the archived one — the row action is
  // always the reversal of the current view, so the two lists share this row.
  onAction: () => void;
  /**
   * Set by the sectioned main list. "Needs you" rows are the full two lines
   * and lead with why the issue waits; "Updates" and "Snoozed" rows are
   * smaller and quieter, because nothing in them is waiting. Absent in the
   * archive, whose rows are as they always were.
   */
  section?: InboxRowSection;
  row?: InboxSectionRow;
}) {
  const { t } = useT("inbox");
  const timeAgo = useTimeAgo();
  const typeLabels = useTypeLabels();
  // Inbox is a cross-workspace surface, so the catalog is read against the
  // item's OWN workspace rather than the route's. (MUL-6243)
  const { categoryOf: statusCategoryOf, colorOf: statusColorOf, iconOf: statusIconOf } =
    useIssueStatuses(item.workspace_id);
  const statusLabelOf = useStatusLabel(item.workspace_id);
  const openContextMenu = useInboxContextMenu();
  // Null-safe slug (not useWorkspacePaths, which throws): the row renders in
  // tests and could render outside a workspace route; without a slug the
  // modifier-click affordance simply stays off.
  const slug = useWorkspaceSlug();
  const issueHref =
    slug && item.issue_id
      ? paths.workspace(slug).issueDetail(item.issue_id)
      : null;
  const intentNavigate = useIntentNavigate();
  const displayTitle = isAutopilotQuotaNotice(item.type)
    ? typeLabels[item.type]
    : getInboxDisplayTitle(item);
  const isArchivedView = view === "archived";
  // Archiving deliberately leaves `read` untouched so unarchiving restores the
  // real unread state, so archived rows would otherwise keep an unread marker
  // the user cannot clear from this view. Suppress the affordance here only.
  // A snoozed row is unread on purpose, so the issue comes back as new when
  // its time passes; until then it is put away and shows nothing to do.
  const showUnread = item.read !== true && !isArchivedView && section !== "snoozed";
  const ActionIcon = isArchivedView ? ArchiveRestore : Archive;
  const actionLabel = isArchivedView
    ? t(($) => $.list.unarchive_tooltip)
    : t(($) => $.list.archive_tooltip);
  // A "Needs you" row shows who is waiting, which is whoever sent the
  // notification that put it there, not whoever touched the issue last.
  const actor = (section === "needs_you" ? row?.reason : null) ?? item;
  const actorType = actor.actor_type ?? actor.recipient_type;
  const actorId = actor.actor_id ?? actor.recipient_id;
  const compact = section === "updates" || section === "snoozed";
  // The glyph is per CATEGORY, so it alone cannot tell "In Review" from a
  // custom "Human Review" — moving between two statuses of the same category
  // left this row pixel-identical and read as "the inbox never updated"
  // (MUL-6395). Colour is what carries a custom status's own identity, exactly
  // as the status-changed detail label already renders it. `colorOf` returns
  // null for a built-in, which keeps it on its semantic token.
  const statusColor = item.issue_status ? statusColorOf(item.issue_status) : null;

  return (
    // A div, not a <button>: the row carries its own controls (the action
    // button, the compact menu), and interactive descendants of a <button>
    // are invalid HTML that screen readers cannot reach. `role="button"` plus
    // the Enter/Space handler keeps the row's own keyboard behavior.
    <div
      data-mc="inbox-item"
      data-read={item.read === true ? "true" : "false"}
      data-selected={isSelected ? "true" : "false"}
      role="button"
      tabIndex={0}
      onClick={(e) => {
        // Plain click keeps the master-detail selection; a modifier click on
        // a row that references an issue opens that issue as its own tab.
        if (issueHref) {
          const intent = resolveClickIntent(e);
          if (intent !== "push") {
            intentNavigate(issueHref, intent);
            return;
          }
        }
        onClick();
      }}
      onKeyDown={(e) => handleRowActivationKey(e, onClick)}
      onAuxClick={(e) => {
        if (e.defaultPrevented || e.button !== 1 || !issueHref) return;
        e.preventDefault();
        intentNavigate(issueHref, "background-tab");
      }}
      // Right-click opens the list's shared menu (mark read/unread, archive).
      // `select-none` mirrors what Base UI's own trigger used to merge in, so
      // right-clicking a row never starts a text selection.
      onContextMenu={
        openContextMenu ? (e) => openContextMenu(item, e) : undefined
      }
      data-section={section}
      className={`group flex w-full cursor-default select-none items-center rounded-md px-2 text-left outline-none transition-colors focus-visible:ring-1 focus-visible:ring-ring ${
        compact ? "gap-2 py-1.5" : "gap-2.5 py-2.5"
      } ${
        isSelected
          ? "bg-accent"
          : "hover:bg-accent/50 data-[popup-open]:bg-accent/50"
      }`}
    >
      {section === "snoozed" ? (
        // The row's width matches the small avatar the rows above it carry.
        <span className="flex size-5 shrink-0 items-center justify-center text-muted-foreground">
          <Clock className="h-3.5 w-3.5" />
        </span>
      ) : (
        <ActorAvatar
          actorType={actorType}
          actorId={actorId}
          size={compact ? "sm" : "lg"}
          enableHoverCard
        />
      )}
      <div className="min-w-0 flex-1">
        <div className="flex items-center justify-between gap-2">
          <div className="flex min-w-0 items-center gap-1.5">
            {showUnread && (
              <span data-mc="unread-dot" className="h-1.5 w-1.5 shrink-0 rounded-full bg-brand" />
            )}
            <span
              className={`truncate text-body ${
                showUnread
                  ? "font-medium"
                  : // Read is not handled: a "Needs you" row keeps its colour
                    // until the issue leaves the section.
                    section === "needs_you"
                    ? ""
                    : "text-muted-foreground"
              }`}
            >
              {displayTitle}
            </span>
          </div>
          <div className="flex shrink-0 items-center gap-1">
            {/* Pointer-only affordance: revealed on hover, and on keyboard
                focus anywhere in the row so it is reachable by Tab. Touch has
                neither, so it stays hidden on a pointer that cannot hover and
                the compact menu below carries the same action. */}
            <DeferredTooltip
              content={actionLabel}
              trigger={
                <button
                  type="button"
                  aria-label={actionLabel}
                  onClick={(e) => {
                    e.stopPropagation();
                    onAction();
                  }}
                  className="hidden rounded-xs p-0.5 text-muted-foreground outline-none hover:bg-accent hover:text-foreground focus-visible:ring-1 focus-visible:ring-ring [@media(hover:hover)]:group-hover:inline-flex [@media(hover:hover)]:group-focus-within:inline-flex"
                >
                  <ActionIcon className="h-3.5 w-3.5" />
                </button>
              }
            />
            <InboxRowMenu item={item} view={view} />
            {section === "snoozed" && row?.snoozedUntil && (
              <InboxSnoozeTime until={row.snoozedUntil} />
            )}
          </div>
        </div>
        {section !== "snoozed" && (
        <div className="mt-0.5 flex items-center justify-between gap-2">
          <p className="min-w-0 overflow-hidden text-ellipsis whitespace-nowrap text-caption text-muted-foreground">
            {section === "needs_you" && row?.reason ? (
              <InboxReasonLabel reason={row.reason} latest={item} preview={row.preview} />
            ) : section === "updates" ? (
              // Who did what: the avatar is too small here to carry the name.
              <span className="flex min-w-0 items-center gap-1">
                {item.actor_id && (
                  <InboxActorName actorType={item.actor_type ?? "member"} actorId={item.actor_id} />
                )}
                <span className="min-w-0 truncate">
                  <InboxDetailLabel item={item} />
                </span>
              </span>
            ) : (
              <InboxDetailLabel item={item} />
            )}
          </p>
          {section === "updates" ? (
            // Only when: nothing here waits on the user, so the row carries
            // neither the issue's status nor the agent badge.
            <span className="shrink-0 text-caption text-muted-foreground">
              {timeAgo(item.created_at)}
            </span>
          ) : (
          <div className="flex shrink-0 items-center gap-1.5">
            {/* Badge only, no hover card (MUL-5189). "An agent is on this"
                is worth showing while triaging; the card behind it adds only
                elapsed time, which does not change whether you open the row.
                The row already carries the ActorAvatar hover card on the
                left, so a second popup here was mostly noise. */}
            {item.issue_id && (
              <IssueAgentActivityIndicator
                issueId={item.issue_id}
                hoverCard={false}
              />
            )}
            {/* On the detail line rather than the title's, so the title keeps
                the whole first line to itself. */}
            {item.issue_status && (
              // Icon-only, like every other issue row — but a colour is not a
              // name, and this row has no space for the CustomStatusChip the
              // board card and list row carry. `title` is that name.
              <span
                title={statusLabelOf(item.issue_status)}
                className="flex shrink-0 items-center"
              >
                <StatusIcon
                  status={item.issue_status}
                  category={statusCategoryOf(item.issue_status)}
                  color={statusColor}
                  icon={item.issue_status ? statusIconOf(item.issue_status) : null}
                  className="h-3.5 w-3.5 shrink-0"
                />
              </span>
            )}
            <span className="text-caption text-muted-foreground">
              {timeAgo(item.created_at)}
            </span>
          </div>
          )}
        </div>
        )}
      </div>
    </div>
  );
}
