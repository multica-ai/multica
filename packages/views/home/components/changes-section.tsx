"use client";

import { useMemo, useState } from "react";
import { ArrowRight } from "lucide-react";
import { toast } from "sonner";
import { groupActivityByIssue, type ActivityGroup } from "@multica/core/home";
import { deduplicateInboxItems } from "@multica/core/inbox/queries";
import {
  useArchiveInbox,
  useMarkInboxRead,
  useMarkInboxUnread,
} from "@multica/core/inbox/mutations";
import { useIssueStatuses } from "@multica/core/issue-statuses/hooks";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import type { InboxItem } from "@multica/core/types";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { cn } from "@multica/ui/lib/utils";
import { ActorAvatar } from "../../common/actor-avatar";
import { StatusIcon } from "../../issues/components/status-icon";
import { InboxContextMenuProvider } from "../../inbox/components/inbox-context-menu";
import { InboxDetailLabel } from "../../inbox/components/inbox-detail-label";
import { getInboxDisplayTitle } from "../../inbox/components/inbox-display";
import { InboxListItem } from "../../inbox/components/inbox-list-item";
import { AppLink, useNavigation } from "../../navigation";
import { useLocale, useT, useTimeAgo } from "../../i18n";

type ChangesView = "by_issue" | "all";

// The inline list is a preview; the full, virtualized list with filters and
// the archive lives one click away in the all-activity view.
const INLINE_ACTIVITY_LIMIT = 20;

export function formatSince(iso: string, locale: string): string {
  return new Intl.DateTimeFormat(locale, {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  }).format(new Date(iso));
}

export function ChangesSection({
  inboxItems,
  since,
  isLoading,
}: {
  inboxItems: InboxItem[];
  since: string;
  isLoading: boolean;
}) {
  const { t } = useT("home");
  const locale = useLocale();
  const [view, setView] = useState<ChangesView>("by_issue");
  const groups = useMemo(() => groupActivityByIssue(inboxItems, since), [inboxItems, since]);
  const activity = useMemo(() => deduplicateInboxItems(inboxItems), [inboxItems]);

  return (
    <section aria-labelledby="home-changes-title" className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center gap-x-2 gap-y-2">
        <h2 id="home-changes-title" className="text-title-sm font-semibold">
          {t(($) => $.changes.title_since, { time: formatSince(since, locale) })}
        </h2>
        {groups.length > 0 && (
          <span className="text-caption text-muted-foreground">
            {t(($) => $.changes.issue_count, { count: groups.length })}
          </span>
        )}
        <div role="tablist" className="ml-auto flex items-center rounded-md bg-muted p-0.5">
          <ViewTab active={view === "by_issue"} onSelect={() => setView("by_issue")} count={groups.length}>
            {t(($) => $.changes.view_by_issue)}
          </ViewTab>
          <ViewTab active={view === "all"} onSelect={() => setView("all")} count={activity.length}>
            {t(($) => $.changes.view_all)}
          </ViewTab>
        </div>
      </div>

      {isLoading ? (
        <div className="space-y-2 rounded-lg border bg-card p-4">
          <Skeleton className="h-4 w-2/3" />
          <Skeleton className="h-4 w-1/2" />
        </div>
      ) : view === "by_issue" ? (
        <ByIssueList groups={groups} />
      ) : (
        <ActivityPreview items={activity} />
      )}
    </section>
  );
}

function ViewTab({
  active,
  onSelect,
  count,
  children,
}: {
  active: boolean;
  onSelect: () => void;
  count: number;
  children: React.ReactNode;
}) {
  return (
    <button
      type="button"
      role="tab"
      aria-selected={active}
      onClick={onSelect}
      className={cn(
        "flex items-center gap-1.5 rounded-sm px-2.5 py-1 text-caption font-medium outline-none transition-colors focus-visible:ring-1 focus-visible:ring-ring",
        active ? "bg-background text-foreground shadow-xs" : "text-muted-foreground hover:text-foreground",
      )}
    >
      {children}
      {count > 0 && <span className="tabular-nums text-muted-foreground">{count}</span>}
    </button>
  );
}

function ByIssueList({ groups }: { groups: ActivityGroup[] }) {
  const { t } = useT("home");
  if (groups.length === 0) {
    return (
      <p className="rounded-lg border bg-card px-4 py-3 text-body text-muted-foreground">
        {t(($) => $.changes.empty)}
      </p>
    );
  }
  return (
    <ul className="overflow-hidden rounded-lg border bg-card">
      {groups.map((group) => (
        <li key={group.key} className="border-b last:border-b-0">
          <ByIssueRow group={group} />
        </li>
      ))}
    </ul>
  );
}

/** One issue that moved: its latest event, how many there were, unread first-class. */
function ByIssueRow({ group }: { group: ActivityGroup }) {
  const { t } = useT("home");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const timeAgo = useTimeAgo();
  const { categoryOf, colorOf, iconOf } = useIssueStatuses(wsId);
  const latest = group.latest;
  const href = `${paths.inbox()}?issue=${encodeURIComponent(group.key)}`;
  const status = latest.issue_status;

  return (
    <AppLink
      href={href}
      className="flex min-w-0 items-center gap-3 px-4 py-2.5 outline-none transition-colors hover:bg-accent/40 focus-visible:bg-accent/40"
    >
      <span className="flex size-4 shrink-0 items-center justify-center">
        {status ? (
          <StatusIcon
            status={status}
            category={categoryOf(status)}
            color={colorOf(status)}
            icon={iconOf(status)}
            className="size-3.5"
          />
        ) : (
          <ActorAvatar
            actorType={latest.actor_type ?? "system"}
            actorId={latest.actor_id ?? ""}
            size="xs"
          />
        )}
      </span>
      <div className="min-w-0 flex-1">
        <div className="flex min-w-0 items-center gap-1.5">
          {group.unreadCount > 0 && <span className="size-1.5 shrink-0 rounded-full bg-brand" />}
          <span className={cn("truncate text-body", group.unreadCount > 0 ? "font-medium" : "text-foreground")}>
            {getInboxDisplayTitle(latest)}
          </span>
        </div>
        <p className="truncate text-caption text-muted-foreground">
          <InboxDetailLabel item={latest} />
        </p>
      </div>
      {group.items.length > 1 && (
        <span className="shrink-0 whitespace-nowrap text-caption text-muted-foreground">
          {t(($) => $.changes.updates, { count: group.items.length })}
        </span>
      )}
      <span className="shrink-0 whitespace-nowrap text-right text-caption text-muted-foreground">
        {timeAgo(latest.created_at)}
      </span>
    </AppLink>
  );
}

/**
 * The newest rows of the all-activity list, rendered with the Inbox's own row
 * so read state, archive and the row menu behave exactly as they do there.
 * Opening a row lands in the full two-pane view with that issue selected.
 */
function ActivityPreview({ items }: { items: InboxItem[] }) {
  const { t } = useT("home");
  const { t: tInbox } = useT("inbox");
  const paths = useWorkspacePaths();
  const { push } = useNavigation();
  const markRead = useMarkInboxRead();
  const markUnread = useMarkInboxUnread();
  const archive = useArchiveInbox();
  const visible = items.slice(0, INLINE_ACTIVITY_LIMIT);

  const onError = (fallback: string) => (err: unknown) =>
    toast.error(err instanceof Error && err.message ? err.message : fallback);
  const handleArchive = (id: string) =>
    archive.mutate(id, {
      onSuccess: () => toast.success(tInbox(($) => $.toasts.archived)),
      onError: onError(tInbox(($) => $.errors.archive_failed)),
    });

  const allHref = paths.inbox();

  if (visible.length === 0) {
    return (
      <p className="rounded-lg border bg-card px-4 py-3 text-body text-muted-foreground">
        {tInbox(($) => $.detail.empty)}
      </p>
    );
  }

  return (
    <div className="overflow-hidden rounded-lg border bg-card">
      <InboxContextMenuProvider
        view="inbox"
        actions={{
          onMarkRead: (id) => markRead.mutate(id, { onError: onError(tInbox(($) => $.errors.mark_read_failed)) }),
          onMarkUnread: (id) => markUnread.mutate(id, { onError: onError(tInbox(($) => $.errors.mark_unread_failed)) }),
          onAction: handleArchive,
        }}
      >
        <div className="p-1">
          {visible.map((item) => (
            <InboxListItem
              key={item.id}
              item={item}
              view="inbox"
              isSelected={false}
              onClick={() => push(`${allHref}?issue=${encodeURIComponent(item.issue_id ?? item.id)}`)}
              onAction={() => handleArchive(item.id)}
            />
          ))}
        </div>
      </InboxContextMenuProvider>
      <AppLink
        href={allHref}
        className="flex items-center justify-center gap-1.5 border-t px-4 py-2 text-caption text-muted-foreground outline-none transition-colors hover:bg-accent/40 hover:text-foreground focus-visible:bg-accent/40"
      >
        {items.length > INLINE_ACTIVITY_LIMIT
          ? t(($) => $.changes.show_more)
          : t(($) => $.changes.open_all)}
        <ArrowRight className="size-3.5" />
      </AppLink>
    </div>
  );
}
