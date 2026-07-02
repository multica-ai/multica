"use client";

import { useState, useEffect, useCallback, useMemo, useRef, Fragment } from "react";
import { Virtuoso } from "react-virtuoso";
import { useDefaultLayout, usePanelRef } from "react-resizable-panels";
import { AppLink } from "../../navigation";
import { useNavigation } from "../../navigation";
import {
  Archive,
  Calendar,
  CalendarClock,
  CalendarDays,
  ChevronDown,
  ChevronLeft,
  ChevronRight,
  CircleCheck,
  ListTree,
  Milestone,
  MoreHorizontal,
  PanelRight,
  Pin,
  PinOff,
  Plus,
  Search,
  Tag,
  Unlink,
  Users,
  X,
} from "lucide-react";
import { BreadcrumbHeader, type BreadcrumbSegment } from "../../layout/breadcrumb-header";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { ResizablePanelGroup, ResizablePanel, ResizableHandle } from "@multica/ui/components/ui/resizable";
import { Sheet, SheetContent } from "@multica/ui/components/ui/sheet";
import { useIsMobile } from "@multica/ui/hooks/use-mobile";
import { ContentEditor, type ContentEditorRef, TitleEditor, useFileDropZone, FileDropOverlay } from "../../editor";
import { FileUploadButton } from "@multica/ui/components/common/file-upload-button";
import {
  Tooltip,
  TooltipTrigger,
  TooltipContent,
} from "@multica/ui/components/ui/tooltip";
import { Popover, PopoverTrigger, PopoverContent } from "@multica/ui/components/ui/popover";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@multica/ui/components/ui/dialog";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { Command, CommandInput, CommandList, CommandEmpty, CommandGroup, CommandItem } from "@multica/ui/components/ui/command";
import { AvatarGroup, AvatarGroupCount } from "@multica/ui/components/ui/avatar";
import { ActorAvatar } from "../../common/actor-avatar";
import { PropRow } from "../../common/prop-row";
import type { Attachment, Issue, IssueStatus, IssuePriority, TimelineEntry, UpdateIssueRequest } from "@multica/core/types";
import { contentReferencesAttachment } from "@multica/core/types";
import { STATUS_CONFIG, PRIORITY_CONFIG } from "@multica/core/issues/config";
import { formatDateOnly } from "@multica/core/issues/date";
import { useUpdateIssue } from "@multica/core/issues/mutations";
import { toast } from "sonner";
import { StatusIcon, PriorityIcon, StatusPicker, PriorityPicker, StagePicker, StartDatePicker, DueDatePicker, AssigneePicker, LabelPicker } from ".";
import { maxSiblingStage } from "./pickers/stage-picker";
import { IssueActionsDropdown, useIssueActions } from "../actions";
import { ProjectPicker } from "../../projects/components/project-picker";
import { LocalDirectoryHint } from "../../projects/components/local-directory-hint";
import { CommentCard } from "./comment-card";
import { CommentInput } from "./comment-input";
import { ResolvedThreadBar } from "./resolved-thread-bar";
import { collectThreadReplies, deriveThreadResolution } from "./thread-utils";
import { IssueAgentHeaderChip } from "./issue-agent-header-chip";
import { ExecutionLogSection } from "./execution-log-section";
import { PullRequestList } from "./pull-request-list";
import { useGitHubSettings } from "@multica/core/github";
import { useQuery } from "@tanstack/react-query";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspacePaths } from "@multica/core/paths";
import { useActorName } from "@multica/core/workspace/hooks";
import { useWorkspaceId } from "@multica/core/hooks";
import { useRecentContextStore } from "@multica/core/chat";
import { issueListOptions, issueDetailOptions, childIssuesOptions, issueUsageOptions, issueAttachmentsOptions } from "@multica/core/issues/queries";
import { projectDetailOptions } from "@multica/core/projects/queries";
import { ProjectIcon } from "../../projects/components/project-icon";
import { issueLabelsOptions } from "@multica/core/labels";
import { memberListOptions, agentListOptions } from "@multica/core/workspace/queries";
import { useRecentIssuesStore } from "@multica/core/issues/stores";
import { useIssueSelectionStore } from "@multica/core/issues/stores/selection-store";
import { BatchActionToolbar } from "./batch-action-toolbar";
import { useIssueTimeline } from "../hooks/use-issue-timeline";
import { useIssueReactions } from "../hooks/use-issue-reactions";
import { useIssueSubscribers } from "../hooks/use-issue-subscribers";
import { ReactionBar } from "@multica/ui/components/common/reaction-bar";
import { useFileUpload } from "@multica/core/hooks/use-file-upload";
import { api } from "@multica/core/api";
import { useTimeAgo } from "../../i18n";
import { cn } from "@multica/ui/lib/utils";

import { ProgressRing } from "./progress-ring";
import { matchesPinyin } from "../../editor/extensions/pinyin-match";
import { useT } from "../../i18n";
import { useIssueDetailScrollRestore } from "../hooks/use-issue-detail-scroll-restore";
import {
  AnimatedRightSidebar,
  getAnimatedRightSidebarInitialOpen,
  rightSidebarPanelMotionProps,
  useAnimatedRightSidebarState,
} from "../../layout/animated-right-sidebar";

type IssueSearchResult = {
  id: string;
  targetId: string;
  kind: "title" | "description" | "comment" | "comment-actor" | "activity" | "activity-actor";
  text: string;
  matchIndex: number;
  rootCommentId?: string;
  activityGroupId?: string;
};

type CommentNavItem = {
  id: string;
  label: string;
};

type CommentNavPosition = {
  top: number;
  right: number;
  width: number;
  maxHeight: number;
};

const COMMENT_NAV_TOP = 64;
const COMMENT_NAV_TOP_WITH_SEARCH = 112;
const COMMENT_NAV_STORAGE_PREFIX = "multica_issue_comment_nav_open";

function commentNavStorageKey(wsId: string | null, issueId: string) {
  return wsId ? `${COMMENT_NAV_STORAGE_PREFIX}:${wsId}:${issueId}` : null;
}

function readCommentNavOpen(wsId: string | null, issueId: string) {
  const key = commentNavStorageKey(wsId, issueId);
  if (!key || typeof window === "undefined") return false;
  try {
    return window.localStorage.getItem(key) === "1";
  } catch {
    return false;
  }
}

function writeCommentNavOpen(wsId: string | null, issueId: string, open: boolean) {
  const key = commentNavStorageKey(wsId, issueId);
  if (!key || typeof window === "undefined") return;
  try {
    if (open) window.localStorage.setItem(key, "1");
    else window.localStorage.removeItem(key);
  } catch {
    // localStorage can throw in private/sandboxed contexts; UI still works.
  }
}

function normalizeIssueSearchQuery(query: string) {
  return query.trim().toLowerCase();
}

function countIssueTextMatches(text: string | null | undefined, query: string) {
  const q = normalizeIssueSearchQuery(query);
  if (!q) return 0;
  const source = (text ?? "").toLowerCase();
  let count = 0;
  let index = source.indexOf(q);
  while (index !== -1) {
    count += 1;
    index = source.indexOf(q, index + q.length);
  }
  return count;
}

function issueSearchVisibleText(text: string | null | undefined) {
  return (text ?? "").replace(/\[([^\]]+)\]\(mention:\/\/[^)]+\)/g, "$1");
}

function commentNavLabel(entry: TimelineEntry, actorName: string, fallbackTime: string) {
  const plain = issueSearchVisibleText(entry.content)
    .replace(/```[\s\S]*?```/g, " ")
    .replace(/`([^`]+)`/g, "$1")
    .replace(/!\[([^\]]*)\]\([^)]+\)/g, "$1")
    .replace(/\[([^\]]+)\]\([^)]+\)/g, "$1")
    .replace(/[#>*_~\-]+/g, " ")
    .replace(/\s+/g, " ")
    .trim();
  return plain || actorName || fallbackTime;
}

function HighlightIssueText({
  text,
  query,
  active,
  activeMatchIndex,
}: {
  text: string;
  query: string;
  active?: boolean;
  activeMatchIndex?: number;
}) {
  const q = query.trim();
  if (!q) return <>{text}</>;
  const escaped = q.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const regex = new RegExp(`(${escaped})`, "gi");
  const parts = text.split(regex);
  let matchIndex = -1;
  return (
    <>
      {parts.map((part, index) => {
        if (part.toLowerCase() !== q.toLowerCase()) return part;
        matchIndex += 1;
        return (
          <mark
            key={index}
            className={cn(
              "rounded-sm bg-yellow-200 text-inherit dark:bg-yellow-900/60",
              active && (activeMatchIndex == null || activeMatchIndex === matchIndex) && "ring-2 ring-brand/60",
            )}
          >
            {part}
          </mark>
        );
      })}
    </>
  );
}

function SubscriberPopoverContent({
  members,
  agents,
  subscribers,
  toggleSubscriber,
  t,
}: {
  members: { user_id: string; name: string }[];
  agents: { id: string; name: string; archived_at?: string | null }[];
  subscribers: { user_type: string; user_id: string }[];
  toggleSubscriber: (id: string, type: "member" | "agent", subscribed: boolean) => void;
  t: ActivityT;
}) {
  const [search, setSearch] = useState("");
  const q = search.trim().toLowerCase();

  const uniqueMembers = members.filter((m, i, arr) => arr.findIndex((x) => x.user_id === m.user_id) === i);
  const activeAgents = agents.filter((a) => !a.archived_at);

  const filteredMembers = q
    ? uniqueMembers.filter((m) => m.name.toLowerCase().includes(q) || matchesPinyin(m.name, q))
    : uniqueMembers;
  const filteredAgents = q
    ? activeAgents.filter((a) => a.name.toLowerCase().includes(q) || matchesPinyin(a.name, q))
    : activeAgents;

  return (
    <PopoverContent align="end" className="w-64 p-0">
      <Command shouldFilter={false}>
        <CommandInput
          placeholder={t(($) => $.detail.change_subscribers_placeholder)}
          value={search}
          onValueChange={setSearch}
        />
        <CommandList className="max-h-64">
          {filteredMembers.length === 0 && filteredAgents.length === 0 && (
            <CommandEmpty>{t(($) => $.detail.no_subscribers_results)}</CommandEmpty>
          )}
          {filteredMembers.length > 0 && (
            <CommandGroup heading={t(($) => $.detail.members_group)}>
              {filteredMembers.map((m) => {
                const sub = subscribers.find((s) => s.user_type === "member" && s.user_id === m.user_id);
                const isSubbed = !!sub;
                return (
                  <CommandItem
                    key={`member-${m.user_id}`}
                    onSelect={() => toggleSubscriber(m.user_id, "member", isSubbed)}
                    className="flex items-center gap-2.5"
                  >
                    <Checkbox checked={isSubbed} className="pointer-events-none" />
                    <ActorAvatar actorType="member" actorId={m.user_id} size={22} />
                    <span className="truncate flex-1">{m.name}</span>
                  </CommandItem>
                );
              })}
            </CommandGroup>
          )}
          {filteredAgents.length > 0 && (
            <CommandGroup heading={t(($) => $.detail.agents_group)}>
              {filteredAgents.map((a) => {
                const sub = subscribers.find((s) => s.user_type === "agent" && s.user_id === a.id);
                const isSubbed = !!sub;
                return (
                  <CommandItem
                    key={`agent-${a.id}`}
                    onSelect={() => toggleSubscriber(a.id, "agent", isSubbed)}
                    className="flex items-center gap-2.5"
                  >
                    <Checkbox checked={isSubbed} className="pointer-events-none" />
                    <ActorAvatar actorType="agent" actorId={a.id} size={22} showStatusDot />
                    <span className="truncate flex-1">{a.name}</span>
                  </CommandItem>
                );
              })}
            </CommandGroup>
          )}
        </CommandList>
      </Command>
    </PopoverContent>
  );
}

function shortDate(date: string | null): string {
  if (!date) return "—";
  return formatDateOnly(date, { month: "short", day: "numeric" }, "en-US");
}

type ActivityT = ReturnType<typeof useT<"issues">>["t"];

function statusLabel(status: string, t: ActivityT): string {
  if (status in STATUS_CONFIG) {
    return t(($) => $.status[status as IssueStatus]);
  }
  return status;
}

function priorityLabel(priority: string, t: ActivityT): string {
  if (priority in PRIORITY_CONFIG) {
    return t(($) => $.priority[priority as IssuePriority]);
  }
  return priority;
}

function formatActivity(
  entry: TimelineEntry,
  t: ActivityT,
  resolveActorName?: (type: string, id: string) => string,
): string {
  const details = (entry.details ?? {}) as Record<string, string>;
  switch (entry.action) {
    case "created":
      return t(($) => $.activity.created);
    case "status_changed":
      return t(($) => $.activity.status_changed, {
        from: statusLabel(details.from ?? "?", t),
        to: statusLabel(details.to ?? "?", t),
      });
    case "priority_changed":
      return t(($) => $.activity.priority_changed, {
        from: priorityLabel(details.from ?? "?", t),
        to: priorityLabel(details.to ?? "?", t),
      });
    case "assignee_changed": {
      const isSelfAssign = details.to_type === entry.actor_type && details.to_id === entry.actor_id;
      if (isSelfAssign) return t(($) => $.activity.self_assigned);
      const toName = details.to_id && details.to_type && resolveActorName
        ? resolveActorName(details.to_type, details.to_id)
        : null;
      if (toName) return t(($) => $.activity.assigned_to, { name: toName });
      if (details.from_id && !details.to_id) return t(($) => $.activity.removed_assignee);
      return t(($) => $.activity.changed_assignee);
    }
    case "start_date_changed": {
      if (!details.to) return t(($) => $.activity.start_date_removed);
      const formatted = formatDateOnly(details.to, { month: "short", day: "numeric" }, "en-US");
      return t(($) => $.activity.start_date_set, { date: formatted });
    }
    case "due_date_changed": {
      if (!details.to) return t(($) => $.activity.due_date_removed);
      const formatted = formatDateOnly(details.to, { month: "short", day: "numeric" }, "en-US");
      return t(($) => $.activity.due_date_set, { date: formatted });
    }
    case "title_changed":
      return t(($) => $.activity.title_renamed, {
        from: details.from ?? "?",
        to: details.to ?? "?",
      });
    case "description_updated":
      return t(($) => $.activity.description_updated);
    case "task_completed":
      return t(($) => $.activity.task_completed, { count: entry.coalesced_count ?? 1 });
    case "task_failed":
      return t(($) => $.activity.task_failed, { count: entry.coalesced_count ?? 1 });
    case "squad_leader_evaluated": {
      const reason = details.reason?.trim();
      switch (details.outcome) {
        case "action":
          return reason
            ? t(($) => $.activity.squad_leader_action_reason, { reason })
            : t(($) => $.activity.squad_leader_action);
        case "no_action":
          return reason
            ? t(($) => $.activity.squad_leader_no_action_reason, { reason })
            : t(($) => $.activity.squad_leader_no_action);
        case "failed":
          return reason
            ? t(($) => $.activity.squad_leader_failed_reason, { reason })
            : t(($) => $.activity.squad_leader_failed);
        default:
          return t(($) => $.activity.squad_leader_evaluated);
      }
    }
    default:
      return entry.action ?? "";
  }
}


// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function formatTokenCount(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}k`;
  return String(n);
}

// Stable reference for threads with no replies. Inline `[]` would create a
// new array on every render and bust React.memo on CommentCard / ResolvedThreadBar.
const EMPTY_REPLIES: TimelineEntry[] = [];

// ---------------------------------------------------------------------------
// Sidebar progressive disclosure
// ---------------------------------------------------------------------------
//
// Properties shown in the sidebar split into two groups:
//   - core: always rendered (status / assignee / project)
//   - optional: rendered only when the issue has a value for that field OR
//     the user explicitly added it via "+ Add property" in this session
//     (priority / due_date / labels)
//
// Parent is not in either group — it has its own standalone section below
// the Properties block, rendered only when the issue actually has a parent.
//
// `OPTIONAL_PROP_KEYS` is the open set — adding a new optional field
// means appending here, wiring its row in the JSX switch below, and
// adding a locale key. The picker, visibility rules, and add-property
// menu all flow from this one list.
// `stage` is only meaningful for a sub-issue (relative to its siblings), so
// its row and add-property entry are gated on `issue.parent_issue_id` at the
// render site below — it stays in this list so seeding/visibility flow through
// the same machinery as the other optional props.
const OPTIONAL_PROP_KEYS = ["priority", "stage", "start_date", "due_date", "labels"] as const;
type OptionalPropKey = (typeof OPTIONAL_PROP_KEYS)[number];

function isOptionalPropSet(
  issue: Issue,
  key: OptionalPropKey,
  attachedLabelsCount: number,
): boolean {
  switch (key) {
    case "priority":
      return issue.priority !== "none";
    case "stage":
      return issue.stage !== null && issue.stage !== undefined;
    case "start_date":
      return !!issue.start_date;
    case "due_date":
      return !!issue.due_date;
    case "labels":
      return attachedLabelsCount > 0;
  }
}

// groupSubIssuesByStage orders a parent's children for display: staged groups
// ascending by stage, then the unstaged group (stage === null) last. Callers
// render a per-group stage header only when the set is actually staged.
export function groupSubIssuesByStage(
  children: Issue[],
): { stage: number | null; items: Issue[] }[] {
  const byStage = new Map<number, Issue[]>();
  const unstaged: Issue[] = [];
  for (const c of children) {
    if (c.stage != null) {
      const arr = byStage.get(c.stage);
      if (arr) arr.push(c);
      else byStage.set(c.stage, [c]);
    } else {
      unstaged.push(c);
    }
  }
  const groups: { stage: number | null; items: Issue[] }[] = [...byStage.keys()]
    .sort((a, b) => a - b)
    .map((s) => ({ stage: s, items: byStage.get(s) as Issue[] }));
  if (unstaged.length > 0) groups.push({ stage: null, items: unstaged });
  return groups;
}

// Shallow array equality by element identity. Used to reuse the previous
// render's per-thread reply slice when nothing in *this* thread changed,
// even if the surrounding `timeline` array was rebuilt by a WS event in
// some unrelated thread.
function shallowEqualEntries(a: TimelineEntry[], b: TimelineEntry[]): boolean {
  if (a === b) return true;
  if (a.length !== b.length) return false;
  for (let i = 0; i < a.length; i++) {
    if (a[i] !== b[i]) return false;
  }
  return true;
}

// Flat per-item shape consumed by <Virtuoso>. Virtuoso needs a flat array
// where each entry is one rendered row; we keep the grouping logic from
// `timelineView.groups` (consecutive same-actor activities still collapse
// into one activity-group row) but project it into a discriminated union
// the itemContent dispatcher can switch on.
type TimelineItem =
  | { kind: "comment"; id: string; entry: TimelineEntry }
  | { kind: "resolved-bar"; id: string; entry: TimelineEntry }
  | { kind: "activity-group"; id: string; entries: TimelineEntry[] };

type RawTimelineGroup = {
  type: "comment" | "activities";
  entries: TimelineEntry[];
};

function flattenGroups(
  groups: ReadonlyArray<RawTimelineGroup>,
  expandedResolved: ReadonlySet<string>,
): TimelineItem[] {
  const out: TimelineItem[] = [];
  for (const group of groups) {
    if (group.type === "comment") {
      const entry = group.entries[0]!;
      const isResolved = !!entry.resolved_at;
      const isExpanded = expandedResolved.has(entry.id);
      out.push(
        isResolved && !isExpanded
          ? { kind: "resolved-bar", id: entry.id, entry }
          : { kind: "comment", id: entry.id, entry },
      );
    } else {
      out.push({
        kind: "activity-group",
        id: group.entries[0]!.id,
        entries: group.entries,
      });
    }
  }
  return out;
}

function TimelineSkeleton() {
  return (
    <div className="mt-4 flex flex-col gap-3">
      {[0, 1].map((i) => (
        <div key={i} className="flex gap-3 p-4">
          <Skeleton className="h-10 w-10 shrink-0 rounded-full" />
          <div className="flex-1 space-y-2">
            <Skeleton className="h-4 w-32" />
            <Skeleton className="h-4 w-full" />
            <Skeleton className="h-4 w-4/5" />
          </div>
        </div>
      ))}
    </div>
  );
}

// When the trailing block is expanded, we still truncate its body to the most
// recent N entries — a single block of 50 status flips drowns the comment area
// as badly as N blocks of 1 would. Older entries fold behind a "Show N more
// activities" line that expands in place.
const LAST_ACTIVITY_BLOCK_VISIBLE_LIMIT = 8;

// Collapsible wrapper for an activity block. Older blocks default to a single
// "N activities" summary line so the timeline isn't dominated by status /
// priority / assignee churn; the trailing block stays expanded because it
// usually answers "what just happened?". Expansion state is owned by the
// parent so it survives Virtuoso's mount/unmount on scroll.
function ActivityBlock({
  entries,
  searchQuery,
  activeSearchResultId,
  expanded,
  onToggle,
  truncateOlder,
  showOlder,
  onToggleShowOlder,
  getActorName,
  t,
  timeAgo,
}: {
  entries: TimelineEntry[];
  searchQuery?: string;
  activeSearchResultId?: string | null;
  expanded: boolean;
  onToggle: () => void;
  // Trailing block only: when true, the body shows only the most recent
  // LAST_ACTIVITY_BLOCK_VISIBLE_LIMIT entries with the older ones folded
  // behind a "Show N more activities" inline toggle.
  truncateOlder: boolean;
  showOlder: boolean;
  onToggleShowOlder: () => void;
  getActorName: (type: string, id: string) => string;
  t: ActivityT;
  timeAgo: (dateStr: string) => string;
}) {
  if (!expanded) {
    const count = entries.length;
    return (
      <div className="pb-3 px-4">
        <button
          type="button"
          onClick={onToggle}
          className="flex items-center gap-1.5 text-xs text-muted-foreground transition-colors hover:text-foreground"
        >
          <ChevronRight className="h-3 w-3 shrink-0" />
          <span>{t(($) => $.activity.activity_count, { count })}</span>
        </button>
      </div>
    );
  }
  const hiddenOlderCount =
    truncateOlder && !showOlder && entries.length > LAST_ACTIVITY_BLOCK_VISIBLE_LIMIT
      ? entries.length - LAST_ACTIVITY_BLOCK_VISIBLE_LIMIT
      : 0;
  const visibleEntries =
    hiddenOlderCount > 0
      ? entries.slice(-LAST_ACTIVITY_BLOCK_VISIBLE_LIMIT)
      : entries;
  // Hide the "v N activities" collapse header while we're in the truncated
  // default state. The "Show N more" link is the only control users need
  // when they're glancing at recent activity — stacking two chevron rows
  // looked like nested folds and added visual noise without value. Once the
  // user explicitly reveals older entries, the header reappears so they can
  // fold the whole block back to a single count line.
  const showHeader = hiddenOlderCount === 0;
  return (
    <div className="pb-3 px-4 flex flex-col gap-3">
      {showHeader && (
        <button
          type="button"
          onClick={onToggle}
          className="flex items-center gap-1.5 text-xs text-muted-foreground transition-colors hover:text-foreground"
        >
          <ChevronDown className="h-3 w-3 shrink-0" />
          <span>{t(($) => $.activity.activity_count, { count: entries.length })}</span>
        </button>
      )}
      {hiddenOlderCount > 0 && (
        <button
          type="button"
          onClick={onToggleShowOlder}
          className="flex items-center gap-1.5 text-xs text-muted-foreground transition-colors hover:text-foreground"
        >
          <ChevronRight className="h-3 w-3 shrink-0" />
          <span>{t(($) => $.activity.show_more_activities, { count: hiddenOlderCount })}</span>
        </button>
      )}
      {visibleEntries.map((entry) => {
        const details = (entry.details ?? {}) as Record<string, string>;
        const isStatusChange = entry.action === "status_changed";
        const isPriorityChange = entry.action === "priority_changed";
        const isStartDateChange = entry.action === "start_date_changed";
        const isDueDateChange = entry.action === "due_date_changed";

        let leadIcon: React.ReactNode;
        if (isStatusChange && details.to) {
          leadIcon = <StatusIcon status={details.to as IssueStatus} className="h-4 w-4 shrink-0" />;
        } else if (isPriorityChange && details.to) {
          leadIcon = <PriorityIcon priority={details.to as IssuePriority} className="h-4 w-4 shrink-0" />;
        } else if (isStartDateChange) {
          leadIcon = <CalendarClock className="h-4 w-4 shrink-0 text-muted-foreground" />;
        } else if (isDueDateChange) {
          leadIcon = <Calendar className="h-4 w-4 shrink-0 text-muted-foreground" />;
        } else {
          leadIcon = <ActorAvatar actorType={entry.actor_type} actorId={entry.actor_id} size={16} />;
        }

        return (
          <div
            key={entry.id}
            id={`issue-search-activity-${entry.id}`}
            className="flex items-center text-xs text-muted-foreground"
          >
            <div className="mr-2 flex w-4 shrink-0 justify-center">
              {leadIcon}
            </div>
            <div className="flex min-w-0 flex-1 items-center gap-1">
              <span id={`issue-search-activity-actor-${entry.id}`} className="shrink-0 font-medium">
                <HighlightIssueText
                  text={getActorName(entry.actor_type, entry.actor_id)}
                  query={searchQuery ?? ""}
                  active={activeSearchResultId?.startsWith(`activity-actor:${entry.id}:`)}
                  activeMatchIndex={activeSearchResultId?.startsWith(`activity-actor:${entry.id}:`)
                    ? Number(activeSearchResultId.split(":").at(-1)) || 0
                    : undefined}
                />
              </span>
              <span className="truncate">
                <HighlightIssueText
                  text={formatActivity(entry, t, getActorName)}
                  query={searchQuery ?? ""}
                  active={activeSearchResultId?.startsWith(`activity:${entry.id}:`)}
                  activeMatchIndex={activeSearchResultId?.startsWith(`activity:${entry.id}:`)
                    ? Number(activeSearchResultId.split(":").at(-1)) || 0
                    : undefined}
                />
              </span>
              {(entry.coalesced_count ?? 1) > 1 &&
                entry.action !== "task_completed" &&
                entry.action !== "task_failed" && (
                  <span className="shrink-0 rounded bg-muted px-1.5 py-0.5 text-xs font-medium tabular-nums text-muted-foreground">
                    {t(($) => $.activity.coalesced_badge, { count: entry.coalesced_count ?? 1 })}
                  </span>
                )}
              <Tooltip>
                <TooltipTrigger
                  render={
                    <span className="ml-auto shrink-0 cursor-default">
                      {timeAgo(entry.created_at)}
                    </span>
                  }
                />
                <TooltipContent side="top">
                  {new Date(entry.created_at).toLocaleString()}
                </TooltipContent>
              </Tooltip>
            </div>
          </div>
        );
      })}
    </div>
  );
}

// ---------------------------------------------------------------------------
// SubIssueRow — sub-issue list item with inline status & assignee editing
// ---------------------------------------------------------------------------

function SubIssueRow({ child }: { child: Issue }) {
  const { t } = useT("issues");
  const paths = useWorkspacePaths();
  const updateIssue = useUpdateIssue();
  const selected = useIssueSelectionStore((s) => s.selectedIds.has(child.id));
  const toggleSelected = useIssueSelectionStore((s) => s.toggle);
  const isDone = child.status === "done" || child.status === "cancelled";

  const handleUpdate = useCallback(
    (updates: Partial<UpdateIssueRequest>) => {
      updateIssue.mutate(
        { id: child.id, ...updates },
        {
          onError: (err) =>
            toast.error(
              err instanceof Error && err.message
                ? err.message
                : t(($) => $.detail.update_failed),
            ),
        },
      );
    },
    [child.id, updateIssue, t],
  );

  // AppLink wraps only the title/identifier area. Pickers and checkbox are
  // siblings, so their clicks never navigate — no stopPropagation acrobatics
  // and no risk of the native checkbox / picker triggers being blocked.
  return (
    <div
      className={cn(
        "flex items-center gap-2.5 px-3 py-2 hover:bg-accent/50 transition-colors group/row",
        selected && "bg-accent/30",
      )}
    >
      <div
        className={cn(
          "flex h-4 w-4 shrink-0 items-center justify-center transition-opacity",
          selected
            ? "opacity-100"
            : "opacity-0 group-hover/row:opacity-100 focus-within:opacity-100",
        )}
      >
        <input
          type="checkbox"
          checked={selected}
          onChange={() => toggleSelected(child.id)}
          aria-label={`Select ${child.identifier}`}
          className="cursor-pointer accent-primary"
        />
      </div>
      <StatusPicker
        status={child.status}
        onUpdate={handleUpdate}
        align="start"
        trigger={
          <StatusIcon
            status={child.status}
            className="h-[15px] w-[15px] shrink-0"
          />
        }
      />
      <AppLink
        href={paths.issueDetail(child.id)}
        className="flex min-w-0 flex-1 items-center gap-2.5"
      >
        <span className="text-[11px] text-muted-foreground tabular-nums font-medium shrink-0">
          {child.identifier}
        </span>
        <span
          className={cn(
            "text-sm truncate flex-1",
            isDone
              ? "text-muted-foreground"
              : "group-hover/row:text-foreground",
          )}
        >
          {child.title}
        </span>
      </AppLink>
      <AssigneePicker
        assigneeType={child.assignee_type}
        assigneeId={child.assignee_id}
        onUpdate={handleUpdate}
        align="end"
        trigger={
          child.assignee_type && child.assignee_id ? (
            <ActorAvatar
              actorType={child.assignee_type}
              actorId={child.assignee_id}
              size={20}
              className="shrink-0"
            />
          ) : (
            <span
              aria-hidden
              className="h-5 w-5 rounded-full border border-dashed border-muted-foreground/30 shrink-0"
            />
          )
        }
      />
    </div>
  );
}

// ---------------------------------------------------------------------------
// Props
// ---------------------------------------------------------------------------

interface IssueDetailProps {
  issueId: string;
  onDelete?: () => void;
  /** Called after the issue is marked as done via the toolbar button. */
  onDone?: () => void;
  defaultSidebarOpen?: boolean;
  layoutId?: string;
  /** When set, the issue detail will auto-scroll to this comment and briefly highlight it. */
  highlightCommentId?: string;
}

// ---------------------------------------------------------------------------
// IssueDetail
// ---------------------------------------------------------------------------

export function IssueDetail({ issueId, onDelete, onDone, defaultSidebarOpen = true, layoutId = "multica_issue_detail_layout", highlightCommentId }: IssueDetailProps) {
  const { t } = useT("issues");
  const timeAgo = useTimeAgo();
  const id = issueId;
  const router = useNavigation();
  const user = useAuthStore((s) => s.user);
  const paths = useWorkspacePaths();

  // Issue navigation — read from TQ list cache
  const wsId = useWorkspaceId();
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  // Workspace owners and admins moderate any comment authored by anyone
  // (mirrors backend `comment.go:507-512`). Computed here so per-comment
  // rendering doesn't have to re-derive it for every row.
  const currentUserRole =
    members.find((m) => m.user_id === user?.id)?.role ?? null;
  const canModerateComments =
    currentUserRole === "owner" || currentUserRole === "admin";
  const { data: allIssues = [] } = useQuery(issueListOptions(wsId));
  const { getActorName } = useActorName();
  const { uploadWithToast } = useFileUpload(api);
  const { defaultLayout, onLayoutChanged } = useDefaultLayout({
    id: layoutId,
  });
  const sidebarRef = usePanelRef();
  const isMobile = useIsMobile();
  const desktopSidebarInitialOpen = getAnimatedRightSidebarInitialOpen(
    defaultSidebarOpen,
    defaultLayout,
  );
  const {
    open: desktopSidebarOpen,
    visualOpen: desktopSidebarVisualOpen,
    motionEnabled: desktopSidebarMotionEnabled,
    beginToggle: beginDesktopSidebarToggle,
    handleResize: handleDesktopSidebarResize,
  } = useAnimatedRightSidebarState(desktopSidebarInitialOpen);
  const [mobileSidebarOpen, setMobileSidebarOpen] = useState(false);

  useEffect(() => {
    if (isMobile) {
      setMobileSidebarOpen(false);
    }
  }, [isMobile]);
  const sidebarOpen = isMobile ? mobileSidebarOpen : desktopSidebarOpen;
  const [propertiesOpen, setPropertiesOpen] = useState(true);
  const [detailsOpen, setDetailsOpen] = useState(true);
  const [parentIssueOpen, setParentIssueOpen] = useState(true);
  const [pullRequestsOpen, setPullRequestsOpen] = useState(true);
  const [metadataOpen, setMetadataOpen] = useState(false);
  const [tokenUsageOpen, setTokenUsageOpen] = useState(true);
  const [issueSearchOpen, setIssueSearchOpen] = useState(false);
  const [issueSearchQuery, setIssueSearchQuery] = useState("");
  const [activeIssueSearchIndex, setActiveIssueSearchIndex] = useState(0);
  const issueSearchInputRef = useRef<HTMLInputElement>(null);
  const [commentNavOpen, setCommentNavOpen] = useState(() => readCommentNavOpen(wsId, id));
  const [activeCommentNavId, setActiveCommentNavId] = useState<string | null>(null);
  const activitySectionRef = useRef<HTMLDivElement>(null);
  const [commentNavPosition, setCommentNavPosition] = useState<CommentNavPosition | null>(null);
  const timelineVirtuosoRef = useRef<any>(null);
  const githubSettings = useGitHubSettings();

  // Per-issue, per-session set of optional properties currently visible in
  // the sidebar Properties section. Seeded on issue switch with whichever
  // fields are already set; "+ Add property" adds an entry, clearing a
  // value does *not* remove one (avoids row-flicker on edit → clear).
  // Resets when the user navigates to a different issue.
  const [visibleOptionalProps, setVisibleOptionalProps] = useState<Set<OptionalPropKey>>(
    () => new Set(),
  );
  // Optional property to auto-open as soon as it's mounted (the user just
  // picked it from "+ Add property" and we want them dropped straight into
  // edit state). Consumed by the row that matches this key, cleared after.
  const [autoOpenProp, setAutoOpenProp] = useState<OptionalPropKey | null>(null);
  // Controlled state for the "+ Add property" popover. Base UI's Popover
  // doesn't auto-dismiss on item click (it's not a Menu primitive), so the
  // popover would stay open behind the newly auto-opened picker — two
  // popovers stacked. We close it explicitly in `addOptionalProp`.
  const [addPropPopoverOpen, setAddPropPopoverOpen] = useState(false);
  // Virtuoso's `customScrollParent` wants the HTMLElement, not a ref. A plain
  // `useRef.current` does not trigger a re-render when it populates, so the
  // Virtuoso prop would never receive the element. Callback ref + state fixes
  // that: setState triggers the re-render that hands Virtuoso the element.
  const [scrollContainerEl, setScrollContainerEl] = useState<HTMLDivElement | null>(null);
  const [highlightedId, setHighlightedId] = useState<string | null>(null);

  useEffect(() => {
    setCommentNavOpen(readCommentNavOpen(wsId, id));
    setActiveCommentNavId(null);
  }, [id, wsId]);

  // Per-session: which resolved threads the user has temporarily expanded.
  // Not persisted (matches Linear) — reload collapses everything back to bars.
  const [expandedResolved, setExpandedResolved] = useState<Set<string>>(() => new Set());
  const toggleResolvedExpand = useCallback((commentId: string, expand: boolean) => {
    setExpandedResolved((prev) => {
      const next = new Set(prev);
      if (expand) next.add(commentId);
      else next.delete(commentId);
      return next;
    });
    // On collapse the thread shrinks and the viewport would jump to whatever was
    // below; pull the just-folded thread back into view with the smallest
    // movement. rAF waits for the collapse to land before measuring.
    if (!expand) {
      requestAnimationFrame(() =>
        document.getElementById(`comment-${commentId}`)?.scrollIntoView({ block: "nearest" }),
      );
    }
  }, []);
  const clearResolvedExpand = useCallback((commentId: string) => {
    setExpandedResolved((prev) => {
      if (!prev.has(commentId)) return prev;
      const next = new Set(prev);
      next.delete(commentId);
      return next;
    });
  }, []);

  // Per-session activity-block expansion overrides. The default rule is
  // "only the trailing block is expanded" (computed from timelineView.groups
  // below); these two sets capture user clicks that diverge from the default.
  // Two sets are needed because "default" can flip when a new activity block
  // appends — without an explicit collapse override, a manually-collapsed
  // older block would re-expand when it stops being the trailing one (or vice
  // versa). Not persisted, matches the resolved-thread behaviour above.
  const [expandedActivityIds, setExpandedActivityIds] = useState<Set<string>>(() => new Set());
  const [collapsedActivityIds, setCollapsedActivityIds] = useState<Set<string>>(() => new Set());
  // Block IDs where the user has explicitly chosen to also reveal the older
  // (pre-last-8) entries within the trailing block. Kept independent of the
  // expanded/collapsed sets so collapsing then re-expanding preserves the
  // "show all" choice, and so the choice survives the block losing its
  // trailing position when a new comment lands after it.
  const [showOlderActivityIds, setShowOlderActivityIds] = useState<Set<string>>(() => new Set());
  const toggleActivityBlock = useCallback((id: string, currentlyExpanded: boolean) => {
    if (currentlyExpanded) {
      setCollapsedActivityIds((prev) => {
        const next = new Set(prev);
        next.add(id);
        return next;
      });
      setExpandedActivityIds((prev) => {
        if (!prev.has(id)) return prev;
        const next = new Set(prev);
        next.delete(id);
        return next;
      });
    } else {
      setExpandedActivityIds((prev) => {
        const next = new Set(prev);
        next.add(id);
        return next;
      });
      setCollapsedActivityIds((prev) => {
        if (!prev.has(id)) return prev;
        const next = new Set(prev);
        next.delete(id);
        return next;
      });
    }
  }, []);
  const showOlderActivities = useCallback((id: string) => {
    setShowOlderActivityIds((prev) => {
      if (prev.has(id)) return prev;
      const next = new Set(prev);
      next.add(id);
      return next;
    });
  }, []);
  const didHighlightRef = useRef<string | null>(null);

  useEffect(() => {
    setIssueSearchOpen(false);
    setIssueSearchQuery("");
    setActiveIssueSearchIndex(0);
  }, [id]);

  // Issue data from TQ — uses detail query, seeded from list cache if available.
  // Only seed when description is present; list API omits it, and ContentEditor
  // reads defaultValue on mount only — seeding null description shows an empty editor.
  const { data: issue = null, isLoading: issueLoading } = useQuery({
    ...issueDetailOptions(wsId, id),
    initialData: () => {
      const cached = allIssues.find((i) => i.id === id);
      return cached?.description != null ? cached : undefined;
    },
  });

  // Record recent visit
  const recordVisit = useRecentIssuesStore((s) => s.recordVisit);
  const recordRecentContext = useRecentContextStore((s) => s.recordVisit);
  useEffect(() => {
    if (issue) {
      recordVisit(wsId, issue.id);
      recordRecentContext(wsId, {
        type: "issue",
        id: issue.id,
        label: issue.identifier,
        subtitle: issue.title,
        status: issue.status,
      });
    }
  }, [issue?.id, wsId]); // eslint-disable-line react-hooks/exhaustive-deps

  // Fire `onDelete` once when the issue transitions from loaded to missing.
  // Delete goes through a shell-level modal, so the caller (e.g. inbox) can't
  // be notified directly — instead, the detail page observes its own cache
  // clearing and runs the callback. We navigate via `onDeletedNavigateTo` on
  // the actions menu when no callback is supplied (standalone routes).
  const hadIssueRef = useRef(false);
  const firedDeleteCallbackRef = useRef(false);
  useEffect(() => {
    if (issue) {
      hadIssueRef.current = true;
      firedDeleteCallbackRef.current = false;
      return;
    }
    if (
      hadIssueRef.current &&
      !issueLoading &&
      !firedDeleteCallbackRef.current &&
      onDelete
    ) {
      firedDeleteCallbackRef.current = true;
      onDelete();
    }
  }, [issue, issueLoading, onDelete]);

  // Custom hooks — encapsulate timeline, reactions, subscribers
  const {
    timeline, loading: timelineLoading,
    submitComment, submitReply,
    editComment, deleteComment, toggleResolveComment, toggleReaction: handleToggleReaction,
  } = useIssueTimeline(id, user?.id);

  // Resolve / unresolve must always clear the per-session expand entry so
  // re-resolving an already-expanded thread folds it back to the bar (the
  // expand Set is keyed only on commentId, not on resolution state). Without
  // this wrapper, an expand → unresolve → resolve sequence keeps the thread
  // visually expanded after the second resolve.
  const handleResolveToggle = useCallback(
    (commentId: string, resolved: boolean) => {
      // Fold the thread back on any resolve change: clear the thread ROOT's
      // expand entry (expand state is keyed on root id, but a resolve target
      // can be a reply). Walk parent_id up to the root.
      const byId = new Map(timeline.map((e) => [e.id, e]));
      let cur = byId.get(commentId);
      while (cur?.parent_id && byId.get(cur.parent_id)) cur = byId.get(cur.parent_id)!;
      clearResolvedExpand(cur?.id ?? commentId);
      toggleResolveComment(commentId, resolved);
    },
    [timeline, clearResolvedExpand, toggleResolveComment],
  );

  // Memoized timeline grouping. Each render rebuilds the per-parent map from
  // the latest timeline, then pre-flattens each thread's reply subtree into a
  // dedicated `threadReplies` slice per root. Slices are stabilized against
  // the previous render via `prevThreadRepliesRef`: if a thread's flat list
  // is shallow-equal to the previous one, we reuse the previous array so
  // React.memo on CommentCard / ResolvedThreadBar can short-circuit. Without
  // this, every WS event (including reactions, edits, AI streaming on an
  // unrelated thread) hands every card a brand-new prop reference and forces
  // every thread subtree to re-render in lockstep.
  const prevThreadRepliesRef = useRef<Map<string, TimelineEntry[]>>(new Map());
  const timelineView = useMemo(() => {
    // Group entries: top-level = activities + root comments; replies are
    // bucketed under their parent's id and rendered nested inside CommentCard.
    // No orphan rescue needed: the timeline is fetched in full, so every
    // reply's parent is always in the same array.
    const topLevel = timeline.filter(
      (e) => e.type === "activity" || !e.parent_id,
    );
    const repliesByParent = new Map<string, TimelineEntry[]>();
    for (const e of timeline) {
      if (e.type === "comment" && e.parent_id) {
        const list = repliesByParent.get(e.parent_id) ?? [];
        list.push(e);
        repliesByParent.set(e.parent_id, list);
      }
    }
    const compareByTimeAndId = (a: TimelineEntry, b: TimelineEntry) => {
      if (a.created_at !== b.created_at) {
        return a.created_at < b.created_at ? -1 : 1;
      }
      return a.id < b.id ? -1 : 1;
    };
    const chronologicalTopLevel = [...topLevel].sort(compareByTimeAndId);
    const timelineUnits: { entries: TimelineEntry[]; sortTime: string; id: string }[] = [];
    let pendingActivities: TimelineEntry[] = [];
    for (const entry of chronologicalTopLevel) {
      if (entry.type === "activity") {
        pendingActivities.push(entry);
        continue;
      }
      timelineUnits.push({
        entries: [...pendingActivities, entry],
        sortTime: entry.created_at,
        id: entry.id,
      });
      pendingActivities = [];
    }
    if (pendingActivities.length > 0) {
      const lastActivity = pendingActivities[pendingActivities.length - 1]!;
      timelineUnits.push({
        entries: pendingActivities,
        sortTime: lastActivity.created_at,
        id: lastActivity.id,
      });
    }
    const sortedTimelineUnits = timelineUnits;
    const sortedTopLevel = sortedTimelineUnits.flatMap((unit) => unit.entries);

    // Pre-flatten each top-level comment's thread subtree (parent + every
    // descendant in render order). Reuse the previous array reference when
    // the thread is unchanged so unrelated CommentCards keep their memo.
    const prevThreadReplies = prevThreadRepliesRef.current;
    const threadReplies = new Map<string, TimelineEntry[]>();
    for (const root of sortedTopLevel) {
      if (root.type !== "comment") continue;
      const fresh = collectThreadReplies(root.id, repliesByParent);
      const previous = prevThreadReplies.get(root.id);
      threadReplies.set(
        root.id,
        previous && shallowEqualEntries(previous, fresh) ? previous : fresh,
      );
    }
    prevThreadRepliesRef.current = threadReplies;

    // Coalesce consecutive activities from the same actor + action.
    // - task_completed / task_failed: no time limit (these repeat across runs)
    // - all other actions: within a 2-minute window
    // - squad_leader_evaluated: never coalesce; outcome/reason are audit data
    const COALESCE_MS = 2 * 60 * 1000;
    const NO_TIME_LIMIT_ACTIONS = new Set(["task_completed", "task_failed"]);
    const NEVER_COALESCE_ACTIONS = new Set(["squad_leader_evaluated"]);
    const coalesceActivities = (entries: TimelineEntry[]) => {
      const coalesced: TimelineEntry[] = [];
      for (const entry of entries) {
        if (entry.type === "activity") {
          const prev = coalesced[coalesced.length - 1];
          if (
            !NEVER_COALESCE_ACTIONS.has(entry.action!) &&
            prev?.type === "activity" &&
            prev.action === entry.action &&
            prev.actor_type === entry.actor_type &&
            prev.actor_id === entry.actor_id &&
            (NO_TIME_LIMIT_ACTIONS.has(entry.action!) ||
              Math.abs(new Date(entry.created_at).getTime() - new Date(prev.created_at).getTime()) <= COALESCE_MS)
          ) {
            coalesced[coalesced.length - 1] = { ...entry, coalesced_count: (prev.coalesced_count ?? 1) + 1 };
            continue;
          }
        }
        coalesced.push(entry);
      }
      return coalesced;
    };

    // Group consecutive activities together so the connector line works
    const groups: { type: "activities" | "comment"; entries: TimelineEntry[] }[] = [];
    for (const unit of sortedTimelineUnits) {
      const unitGroups: { type: "activities" | "comment"; entries: TimelineEntry[] }[] = [];
      for (const entry of coalesceActivities(unit.entries)) {
        if (entry.type === "activity") {
          const last = unitGroups[unitGroups.length - 1];
          if (last?.type === "activities") {
            last.entries.push(entry);
          } else {
            unitGroups.push({ type: "activities", entries: [entry] });
          }
        } else {
          unitGroups.push({ type: "comment", entries: [entry] });
        }
      }
      groups.push(...unitGroups);
    }

    return { threadReplies, groups };
  }, [timeline]);

  // Flat array consumed by <Virtuoso>. Recomputed when timelineView.groups
  // changes (timeline events) or expandedResolved flips (user toggles a
  // resolved thread). Kept in a useMemo so Virtuoso's data identity is stable
  // across unrelated re-renders.
  const items = useMemo<TimelineItem[]>(
    () => flattenGroups(timelineView.groups, expandedResolved),
    [timelineView.groups, expandedResolved],
  );

  const commentNavItems = useMemo<CommentNavItem[]>(() => {
    return timelineView.groups
      .filter((group) => group.type === "comment")
      .map((group) => group.entries[0])
      .filter((entry): entry is TimelineEntry => !!entry && entry.type === "comment" && !entry.parent_id)
      .map((entry) => ({
        id: entry.id,
        label: commentNavLabel(entry, getActorName(entry.actor_type, entry.actor_id), timeAgo(entry.created_at)),
      }));
  }, [getActorName, timeAgo, timelineView.groups]);

  const showCommentNav = commentNavOpen && !isMobile && commentNavItems.length > 0;

  const toggleCommentNav = useCallback(() => {
    setCommentNavOpen((open) => {
      const next = !open;
      writeCommentNavOpen(wsId, id, next);
      return next;
    });
  }, [id, wsId]);

  useEffect(() => {
    if (!showCommentNav) {
      setCommentNavPosition(null);
      return;
    }

    let frame = 0;
    const updateNow = () => {
      const rect = activitySectionRef.current?.getBoundingClientRect();
      const scrollRect = scrollContainerEl?.getBoundingClientRect();
      if (!rect || !scrollRect) return;
      const left = rect.right + 12;
      const right = scrollRect.right - 8;
      const availableWidth = Math.floor(right - left);
      if (availableWidth < 24) {
        setCommentNavPosition(null);
        return;
      }
      const width = Math.min(160, availableWidth);
      const safeTop = issueSearchOpen ? COMMENT_NAV_TOP_WITH_SEARCH : COMMENT_NAV_TOP;
      const top = Math.max(safeTop, Math.ceil(rect.top + 16));
      const maxHeight = Math.max(
        0,
        Math.floor(Math.min(window.innerHeight - top - 16, rect.bottom - top)),
      );
      const next = {
        top,
        right: Math.max(8, window.innerWidth - right),
        width,
        maxHeight,
      };
      setCommentNavPosition((prev) => {
        if (
          prev?.top === next.top &&
          prev.right === next.right &&
          prev.width === next.width &&
          prev.maxHeight === next.maxHeight
        ) {
          return prev;
        }
        return next;
      });
    };
    const update = () => {
      cancelAnimationFrame(frame);
      frame = requestAnimationFrame(updateNow);
    };

    updateNow();
    scrollContainerEl?.addEventListener("scroll", update, { passive: true });
    window.addEventListener("resize", update);
    const observer = new ResizeObserver(update);
    if (activitySectionRef.current) observer.observe(activitySectionRef.current);
    if (scrollContainerEl) observer.observe(scrollContainerEl);
    return () => {
      scrollContainerEl?.removeEventListener("scroll", update);
      window.removeEventListener("resize", update);
      observer.disconnect();
      cancelAnimationFrame(frame);
    };
  }, [issueSearchOpen, scrollContainerEl, showCommentNav]);

  // ID of the newest activity block by activity timestamp — the only one
  // expanded by default. Activity expansion/truncation is based on activity
  // chronology, independent of comment rendering.
  const newestActivityGroupId = useMemo(() => {
    let newestGroup: { id: string; createdAt: string } | null = null;
    for (const group of timelineView.groups) {
      if (group.type !== "activities") continue;
      const latestEntry = group.entries.reduce((latest, entry) =>
        entry.created_at > latest.created_at ? entry : latest,
      );
      if (!newestGroup || latestEntry.created_at > newestGroup.createdAt) {
        newestGroup = { id: group.entries[0]!.id, createdAt: latestEntry.created_at };
      }
    }
    return newestGroup?.id ?? null;
  }, [timelineView.groups]);

  const activityGroupByEntryId = useMemo(() => {
    const map = new Map<string, string>();
    for (const group of timelineView.groups) {
      if (group.type !== "activities") continue;
      for (const entry of group.entries) map.set(entry.id, group.entries[0]!.id);
    }
    return map;
  }, [timelineView.groups]);

  // Map of reply-comment id → root-comment id, so a deep-link to a reply
  // (which lives inside a CommentCard, not in the flat items array) can fall
  // back to scrolling the root thread into view. Without this, an inbox
  // notification on a reply would land at items[-1] and short-circuit.
  const replyToRoot = useMemo(() => {
    const map = new Map<string, string>();
    for (const [rootId, replies] of timelineView.threadReplies) {
      for (const reply of replies) {
        map.set(reply.id, rootId);
      }
    }
    return map;
  }, [timelineView.threadReplies]);

  const issueSearchResults = useMemo<IssueSearchResult[]>(() => {
    const q = normalizeIssueSearchQuery(issueSearchQuery);
    if (!issue || q.length === 0) return [];
    const results: IssueSearchResult[] = [];
    const pushMatches = (result: Omit<IssueSearchResult, "id" | "matchIndex"> & { idPrefix: string }) => {
      const count = countIssueTextMatches(result.text, q);
      for (let matchIndex = 0; matchIndex < count; matchIndex += 1) {
        const { idPrefix, ...rest } = result;
        results.push({
          ...rest,
          id: `${idPrefix}:${matchIndex}`,
          matchIndex,
        });
      }
    };
    pushMatches({ idPrefix: "title", targetId: "issue-search-title", kind: "title", text: issue.title });
    pushMatches({
      idPrefix: "description",
      targetId: "issue-search-description",
      kind: "description",
      text: issueSearchVisibleText(issue.description),
    });
    for (const entry of timeline) {
      if (entry.type === "comment") {
        const rootCommentId = entry.parent_id ? (replyToRoot.get(entry.id) ?? entry.parent_id) : entry.id;
        pushMatches({
          idPrefix: `comment-actor:${entry.id}`,
          targetId: `issue-search-comment-actor-${entry.id}`,
          kind: "comment-actor",
          text: getActorName(entry.actor_type, entry.actor_id),
          rootCommentId,
        });
        pushMatches({
          idPrefix: `comment:${entry.id}`,
          targetId: `comment-${entry.id}`,
          kind: "comment",
          text: issueSearchVisibleText(entry.content),
          rootCommentId,
        });
      }
    }
    for (const group of timelineView.groups) {
      if (group.type !== "activities") continue;
      for (const entry of group.entries) {
        pushMatches({
          idPrefix: `activity-actor:${entry.id}`,
          targetId: `issue-search-activity-actor-${entry.id}`,
          kind: "activity-actor",
          text: getActorName(entry.actor_type, entry.actor_id),
          activityGroupId: activityGroupByEntryId.get(entry.id),
        });
        const text = formatActivity(entry, t, getActorName);
        pushMatches({
          idPrefix: `activity:${entry.id}`,
          targetId: `issue-search-activity-${entry.id}`,
          kind: "activity",
          text,
          activityGroupId: activityGroupByEntryId.get(entry.id),
        });
      }
    }
    return results;
  }, [activityGroupByEntryId, getActorName, issue, issueSearchQuery, replyToRoot, t, timeline, timelineView.groups]);

  const activeIssueSearchResult = issueSearchResults[activeIssueSearchIndex] ?? null;

  useEffect(() => {
    if (activeIssueSearchIndex >= issueSearchResults.length) {
      setActiveIssueSearchIndex(0);
    }
  }, [activeIssueSearchIndex, issueSearchResults.length]);

  const moveIssueSearch = useCallback((delta: 1 | -1) => {
    setActiveIssueSearchIndex((index) => {
      if (issueSearchResults.length === 0) return 0;
      return (index + delta + issueSearchResults.length) % issueSearchResults.length;
    });
  }, [issueSearchResults.length]);

  const closeIssueSearch = useCallback(() => {
    setIssueSearchOpen(false);
    setIssueSearchQuery("");
    setActiveIssueSearchIndex(0);
  }, []);

  useEffect(() => {
    if (issueSearchOpen) {
      requestAnimationFrame(() => issueSearchInputRef.current?.focus());
    }
  }, [issueSearchOpen]);

  useEffect(() => {
    if (!activeIssueSearchResult) return;
    if (activeIssueSearchResult.rootCommentId) {
      setExpandedResolved((prev) => {
        if (prev.has(activeIssueSearchResult.rootCommentId!)) return prev;
        const next = new Set(prev);
        next.add(activeIssueSearchResult.rootCommentId!);
        return next;
      });
    }
    if (activeIssueSearchResult.activityGroupId) {
      setExpandedActivityIds((prev) => {
        if (prev.has(activeIssueSearchResult.activityGroupId!)) return prev;
        const next = new Set(prev);
        next.add(activeIssueSearchResult.activityGroupId!);
        return next;
      });
      setCollapsedActivityIds((prev) => {
        if (!prev.has(activeIssueSearchResult.activityGroupId!)) return prev;
        const next = new Set(prev);
        next.delete(activeIssueSearchResult.activityGroupId!);
        return next;
      });
      setShowOlderActivityIds((prev) => {
        if (prev.has(activeIssueSearchResult.activityGroupId!)) return prev;
        const next = new Set(prev);
        next.add(activeIssueSearchResult.activityGroupId!);
        return next;
      });
    }

    const scrollToTarget = () => {
      const target = document.getElementById(activeIssueSearchResult.targetId);
      if (!target || !scrollContainerEl) return;
      const c = scrollContainerEl.getBoundingClientRect();
      const e = target.getBoundingClientRect();
      scrollContainerEl.scrollTop = Math.max(
        0,
        scrollContainerEl.scrollTop + (e.top - c.top) - (scrollContainerEl.clientHeight - e.height) / 2,
      );
    };
    const raf = requestAnimationFrame(() => requestAnimationFrame(scrollToTarget));
    return () => cancelAnimationFrame(raf);
  }, [activeIssueSearchResult, scrollContainerEl]);

  const centerCommentInTimeline = useCallback((commentId: string) => {
    const target = document.getElementById(`comment-${commentId}`);
    if (!target || !scrollContainerEl) return false;
    const c = scrollContainerEl.getBoundingClientRect();
    const e = target.getBoundingClientRect();
    scrollContainerEl.scrollTop = Math.max(
      0,
      scrollContainerEl.scrollTop + (e.top - c.top) - (scrollContainerEl.clientHeight - e.height) / 2,
    );
    return true;
  }, [scrollContainerEl]);

  const handleCommentNavClick = useCallback((commentId: string) => {
    setActiveCommentNavId(commentId);
    if (centerCommentInTimeline(commentId)) return;
    const index = items.findIndex((item) => item.id === commentId);
    if (index >= 0) {
      timelineVirtuosoRef.current?.scrollToIndex?.({ index, align: "center" });
      requestAnimationFrame(() => centerCommentInTimeline(commentId));
    }
  }, [centerCommentInTimeline, items]);

  useEffect(() => {
    if (!showCommentNav || !scrollContainerEl) return;
    const updateActive = () => {
      const center = scrollContainerEl.getBoundingClientRect().top + scrollContainerEl.clientHeight / 2;
      let nearest: { id: string; distance: number } | null = null;
      for (const item of commentNavItems) {
        const el = document.getElementById(`comment-${item.id}`);
        if (!el) continue;
        const rect = el.getBoundingClientRect();
        const distance = Math.abs(rect.top + rect.height / 2 - center);
        if (!nearest || distance < nearest.distance) nearest = { id: item.id, distance };
      }
      if (nearest) setActiveCommentNavId(nearest.id);
    };
    scrollContainerEl.addEventListener("scroll", updateActive, { passive: true });
    window.addEventListener("resize", updateActive);
    return () => {
      scrollContainerEl.removeEventListener("scroll", updateActive);
      window.removeEventListener("resize", updateActive);
    };
  }, [commentNavItems, scrollContainerEl, showCommentNav]);

  useEffect(() => {
    if (!commentNavItems.some((item) => item.id === activeCommentNavId)) {
      setActiveCommentNavId(commentNavItems[0]?.id ?? null);
    }
  }, [activeCommentNavId, commentNavItems]);

  // Deep-link target index in the flat items array. For root comments this is
  // a direct findIndex hit; for reply ids we look up the enclosing root.
  const targetIdx = useMemo(() => {
    if (!highlightCommentId) return -1;
    const direct = items.findIndex((it) => it.id === highlightCommentId);
    if (direct >= 0) return direct;
    const rootId = replyToRoot.get(highlightCommentId);
    if (!rootId) return -1;
    return items.findIndex((it) => it.id === rootId);
  }, [items, highlightCommentId, replyToRoot]);

  const {
    reactions: issueReactions,
    toggleReaction: handleToggleIssueReaction,
  } = useIssueReactions(id, user?.id);

  const {
    subscribers, isSubscribed, toggleSubscribe: handleToggleSubscribe, toggleSubscriber,
  } = useIssueSubscribers(id, user?.id);

  // Token usage
  const { data: usage } = useQuery(issueUsageOptions(id));

  // Attachments uploaded against this issue. Drives the description
  // editor's click-time fresh-sign download: NodeViews match
  // `src`/`href` against this list to resolve an attachment id before
  // calling `/api/attachments/{id}`.
  const { data: issueAttachments } = useQuery(issueAttachmentsOptions(id));

  // Sub-issue queries
  const parentIssueId = issue?.parent_issue_id;
  const { data: parentIssue = null } = useQuery({
    ...issueDetailOptions(wsId, parentIssueId ?? ""),
    enabled: !!parentIssueId,
    initialData: () => allIssues.find((i) => i.id === parentIssueId),
  });

  // Project segment in the breadcrumb. The issue's project_id is the source of
  // truth — same URL renders the same breadcrumb regardless of entry path.
  const issueProjectId = issue?.project_id;
  const { data: breadcrumbProject = null } = useQuery({
    ...projectDetailOptions(wsId, issueProjectId ?? ""),
    enabled: !!issueProjectId,
  });
  const { data: childIssues = [] } = useQuery({
    ...childIssuesOptions(wsId, id),
    enabled: !!issue,
  });
  // Parent's children — used to render the "x/y" progress next to the
  // "Sub-issue of …" breadcrumb under the title.
  const { data: parentChildIssues = [] } = useQuery({
    ...childIssuesOptions(wsId, parentIssueId ?? ""),
    enabled: !!parentIssueId,
  });
  const [subIssuesCollapsed, setSubIssuesCollapsed] = useState(false);

  // Selection store is global (workspace-scoped); clear it whenever this
  // issue detail is mounted or switched, so leftover selections from the
  // main list view (or another sub-issue list) don't leak into this one.
  const clearSelection = useIssueSelectionStore((s) => s.clear);
  const selectedIds = useIssueSelectionStore((s) => s.selectedIds);
  const selectIds = useIssueSelectionStore((s) => s.select);
  const deselectIds = useIssueSelectionStore((s) => s.deselect);
  useEffect(() => {
    clearSelection();
    return clearSelection;
  }, [id, clearSelection]);

  const childIssueIds = useMemo(() => childIssues.map((c) => c.id), [childIssues]);
  const childSelectedCount = childIssueIds.filter((cid) =>
    selectedIds.has(cid),
  ).length;
  const allChildrenSelected =
    childIssueIds.length > 0 && childSelectedCount === childIssueIds.length;
  const someChildrenSelected = childSelectedCount > 0;
  const handleToggleSelectAllChildren = useCallback(() => {
    if (allChildrenSelected) deselectIds(childIssueIds);
    else selectIds(childIssueIds);
  }, [allChildrenSelected, childIssueIds, deselectIds, selectIds]);

  const loading = issueLoading;

  // Deep-link landing. Semantically equivalent to navigating to
  // `#comment-${id}`: find the element with that id, scrollIntoView it.
  // When `highlightCommentId` is set the timeline below renders flat (no
  // virtualization), so every comment id is in the DOM by the time this
  // effect runs after commit.
  //
  // For a reply inside a folded resolved thread, the reply is not in items
  // (only the resolved-bar root is). Auto-expand the thread first; the
  // effect re-runs once items re-flatten.
  //
  // `scrollContainerEl` is in deps because the component early-returns a
  // loading skeleton while the issue query is pending. The scroll-container
  // ref populates only on the post-loading render, so it's the signal that
  // the timeline (and the deep-link target id) has actually rendered.
  useEffect(() => {
    if (!highlightCommentId || items.length === 0) return;
    if (didHighlightRef.current === highlightCommentId) return;

    const rootId = replyToRoot.get(highlightCommentId);
    if (rootId && rootId !== highlightCommentId) {
      // Root resolved → the whole thread is a folded bar.
      if (items[targetIdx]?.kind === "resolved-bar") {
        toggleResolvedExpand(rootId, true);
        return;
      }
      // A reply is the resolution → the other replies fold behind the
      // "N comments" bar; expand if the target is one of those folded replies.
      const rootItem = items[targetIdx];
      if (rootItem?.kind === "comment" && !expandedResolved.has(rootId)) {
        const resolution = deriveThreadResolution(
          rootItem.entry,
          timelineView.threadReplies.get(rootId) ?? EMPTY_REPLIES,
        );
        if (resolution.kind === "reply" && resolution.resolutionId !== highlightCommentId) {
          toggleResolvedExpand(rootId, true);
          return;
        }
      }
    }

    const el = document.getElementById(`comment-${highlightCommentId}`);
    const container = scrollContainerEl;
    if (!el || !container) return;

    didHighlightRef.current = highlightCommentId;

    // Center the target comment WITHIN its own scroll container by driving the
    // container's scrollTop directly — never native scrollIntoView. Native
    // scrollIntoView is spec'd to scroll EVERY scrollable ancestor: on a cold
    // mount where the timeline is still growing (streaming agent), the inner
    // scroller can't satisfy centering on its own, so the scroll propagates up
    // and moves the desktop shell's `overflow:hidden` wrapper — shoving the
    // whole page, header included, off the top with no scrollbar to recover,
    // until a resize reflows it (#3929). Scoping the scroll to `container`
    // keeps it contained; re-centering across frames lands the comment
    // precisely once async heights (markdown, code highlight, streamed replies)
    // settle, instead of leaning on the ancestor scroll the way native did.
    let rafId = 0;
    let frames = 0;
    let last = -1;
    const center = () => {
      const c = container.getBoundingClientRect();
      const e = el.getBoundingClientRect();
      const target = Math.max(
        0,
        container.scrollTop + (e.top - c.top) - (container.clientHeight - e.height) / 2,
      );
      container.scrollTop = target;
      // Content is still laying out → the centered offset keeps shifting; keep
      // re-centering until it stabilizes (within 1px) or we hit ~0.5s of frames.
      if (Math.abs(target - last) > 1 && ++frames < 30) {
        last = target;
        rafId = requestAnimationFrame(center);
      }
    };
    rafId = requestAnimationFrame(center);

    setHighlightedId(highlightCommentId);
    const fade = window.setTimeout(() => setHighlightedId(null), 2500);
    return () => {
      cancelAnimationFrame(rafId);
      clearTimeout(fade);
    };
  }, [highlightCommentId, items, targetIdx, scrollContainerEl, replyToRoot, expandedResolved, timelineView, toggleResolvedExpand]);

  const descEditorRef = useRef<ContentEditorRef>(null);
  const { isDragOver: descDragOver, dropZoneProps: descDropZoneProps } = useFileDropZone({
    onDrop: (files) => files.forEach((f) => descEditorRef.current?.uploadFile(f)),
  });
  // Pending uploads in the description editor. We don't pass `issueId` on
  // upload (to avoid orphaning attachments when the user deletes the file
  // from the markdown), so they start unattached and we re-bind them via
  // `attachment_ids` on the next description save. Drives editor previews
  // so text/code attachments show an Eye before the bind round-trips.
  const [descPendingAttachments, setDescPendingAttachments] = useState<Attachment[]>([]);
  const descPendingAttachmentsRef = useRef<Attachment[]>([]);
  const descEditorAttachments = descPendingAttachments.length > 0
    ? [...(issueAttachments ?? []), ...descPendingAttachments]
    : issueAttachments;
  const handleDescriptionUpload = useCallback(
    async (file: File) => {
      const result = await uploadWithToast(file);
      if (result) {
        descPendingAttachmentsRef.current = [
          ...descPendingAttachmentsRef.current,
          result,
        ];
        setDescPendingAttachments(descPendingAttachmentsRef.current);
      }
      return result;
    },
    [uploadWithToast],
  );

  useEffect(() => {
    descPendingAttachmentsRef.current = [];
    setDescPendingAttachments([]);
  }, [id]);

  // Shared issue actions (mutations, pin, copy-link, modal dispatch, etc.).
  // Called before the `if (!issue)` early return so hook order stays stable.
  const actions = useIssueActions(issue);
  const handleUpdateField = actions.updateField;

  // Labels live in their own query (not on the issue body) — fetch the count
  // here so seeding can decide whether the "Labels" optional row should be
  // shown for an issue that already has labels attached.
  const { data: attachedLabels = [] } = useQuery(issueLabelsOptions(wsId, id));
  const attachedLabelsCount = attachedLabels.length;

  // Seed the visible-optional-props set:
  //   - on issue switch, reset to whichever fields are currently set
  //   - on the SAME issue, additively pick up fields the user just set
  //     (so the row stays visible after they edit + clear in one session)
  // Removal happens only on issue switch — never on clear.
  const seededIssueIdRef = useRef<string | null>(null);
  useEffect(() => {
    if (!issue) return;
    if (seededIssueIdRef.current !== issue.id) {
      seededIssueIdRef.current = issue.id;
      setAutoOpenProp(null);
      const seed = new Set<OptionalPropKey>();
      for (const k of OPTIONAL_PROP_KEYS) {
        if (isOptionalPropSet(issue, k, attachedLabelsCount)) seed.add(k);
      }
      setVisibleOptionalProps(seed);
      return;
    }
    setVisibleOptionalProps((prev) => {
      let next = prev;
      for (const k of OPTIONAL_PROP_KEYS) {
        if (isOptionalPropSet(issue, k, attachedLabelsCount) && !next.has(k)) {
          if (next === prev) next = new Set(prev);
          next.add(k);
        }
      }
      return next;
    });
  }, [issue, attachedLabelsCount]);

  const addOptionalProp = useCallback(
    (key: OptionalPropKey) => {
      setVisibleOptionalProps((prev) => {
        if (prev.has(key)) return prev;
        const next = new Set(prev);
        next.add(key);
        return next;
      });
      setAutoOpenProp(key);
      // Dismiss the "+ Add property" popover so it doesn't sit stacked
      // behind the picker we're about to auto-open.
      setAddPropPopoverOpen(false);
    },
    [],
  );

  // Clear the auto-open flag after the next render so pickers (which read
  // `defaultOpen` once via a useState initializer) keep the open state they
  // captured on mount, but later interactions don't re-trigger it.
  useEffect(() => {
    if (autoOpenProp === null) return;
    setAutoOpenProp(null);
  }, [autoOpenProp]);

  const handleToggleSidebar = useCallback(() => {
    if (isMobile) {
      setMobileSidebarOpen((open) => !open);
      return;
    }

    const panel = sidebarRef.current;
    if (!panel) return;
    const nextOpen = panel.isCollapsed();
    beginDesktopSidebarToggle(nextOpen);
    window.requestAnimationFrame(() => {
      if (nextOpen) panel.expand();
      else panel.collapse();
    });
  }, [beginDesktopSidebarToggle, isMobile, sidebarRef]);

  useIssueDetailScrollRestore({
    restoreKey: `${wsId}:${id}`,
    scrollContainerEl,
    ready: !!issue && !loading && !timelineLoading,
    disabled: !!highlightCommentId,
  });

  if (loading) {
    return (
      <div className="flex flex-1 min-h-0 flex-col">
        <div className="flex h-12 shrink-0 items-center gap-2 border-b px-4">
          <Skeleton className="h-4 w-16" />
          <Skeleton className="h-4 w-4" />
          <Skeleton className="h-4 w-24" />
        </div>
        <div className="flex flex-1 min-h-0">
          <div className="flex-1 overflow-y-auto">
            <div className="mx-auto w-full max-w-4xl px-8 py-8 space-y-6">
              <Skeleton className="h-8 w-3/4" />
              <div className="space-y-2">
                <Skeleton className="h-4 w-full" />
                <Skeleton className="h-4 w-5/6" />
                <Skeleton className="h-4 w-2/3" />
              </div>
              <Skeleton className="h-px w-full" />
              <div className="space-y-3">
                <Skeleton className="h-4 w-20" />
                <div className="flex items-start gap-3">
                  <Skeleton className="h-8 w-8 shrink-0 rounded-full" />
                  <div className="flex-1 space-y-2">
                    <Skeleton className="h-4 w-32" />
                    <Skeleton className="h-16 w-full rounded-lg" />
                  </div>
                </div>
              </div>
            </div>
          </div>
          <div className="hidden md:block w-80 border-l p-4 space-y-5">
            {Array.from({ length: 4 }).map((_, i) => (
              <div key={i} className="flex items-center gap-2">
                <Skeleton className="h-3 w-16 shrink-0" />
                <Skeleton className="h-5 w-24" />
              </div>
            ))}
            <Skeleton className="h-px w-full" />
            {Array.from({ length: 3 }).map((_, i) => (
              <div key={i} className="flex items-center gap-2">
                <Skeleton className="h-3 w-16 shrink-0" />
                <Skeleton className="h-4 w-28" />
              </div>
            ))}
          </div>
        </div>
      </div>
    );
  }

  if (!issue) {
    return (
      <div className="flex flex-1 min-h-0 flex-col items-center justify-center gap-3 text-sm text-muted-foreground">
        <p>{t(($) => $.detail.not_found)}</p>
        {!onDelete && (
          <Button variant="outline" size="sm" onClick={() => router.push(paths.issues())}>
            <ChevronLeft className="mr-1 h-3.5 w-3.5" />
            {t(($) => $.detail.back_to_issues)}
          </Button>
        )}
      </div>
    );
  }

  const sidebarContent = (
    <div className="space-y-5">
      {/* Properties */}
      <div>
        <button
          type="button"
          className={`flex w-full items-center gap-1 rounded-md px-2 py-1 text-xs font-medium transition-colors mb-2 hover:bg-accent/70 ${propertiesOpen ? "" : "text-muted-foreground hover:text-foreground"}`}
          onClick={() => setPropertiesOpen(!propertiesOpen)}
        >
          {t(($) => $.detail.section_properties)}
          <ChevronRight className={`!size-3 shrink-0 stroke-[2.5] text-muted-foreground transition-transform ${propertiesOpen ? "rotate-90" : ""}`} />
        </button>
        {propertiesOpen && <div className="grid grid-cols-[auto_1fr] gap-x-2 gap-y-0.5 pl-2">
          {/* Core props — always rendered. */}
          <PropRow label={t(($) => $.detail.prop_status)}>
            <StatusPicker status={issue.status} onUpdate={handleUpdateField} align="start" />
          </PropRow>
          <PropRow label={t(($) => $.detail.prop_assignee)}>
            <AssigneePicker assigneeType={issue.assignee_type} assigneeId={issue.assignee_id} onUpdate={handleUpdateField} align="start" />
          </PropRow>
          <PropRow label={t(($) => $.detail.prop_project)}>
            <ProjectPicker
              projectId={issue.project_id}
              onUpdate={handleUpdateField}
            />
          </PropRow>

          {/* Optional props — rendered only when set on the issue OR added
              via "+ Add property" in this session. Row order follows the
              order of `OPTIONAL_PROP_KEYS`. */}
          {visibleOptionalProps.has("priority") && (
            <PropRow label={t(($) => $.detail.prop_priority)}>
              <PriorityPicker
                priority={issue.priority}
                onUpdate={handleUpdateField}
                align="start"
                defaultOpen={autoOpenProp === "priority"}
              />
            </PropRow>
          )}
          {issue.parent_issue_id != null && visibleOptionalProps.has("stage") && (
            <PropRow label={t(($) => $.detail.prop_stage)}>
              <StagePicker
                stage={issue.stage}
                onUpdate={handleUpdateField}
                maxStage={maxSiblingStage(parentChildIssues)}
                align="start"
                defaultOpen={autoOpenProp === "stage"}
              />
            </PropRow>
          )}
          {visibleOptionalProps.has("start_date") && (
            <PropRow label={t(($) => $.detail.prop_start_date)}>
              <StartDatePicker
                startDate={issue.start_date}
                onUpdate={handleUpdateField}
                defaultOpen={autoOpenProp === "start_date"}
              />
            </PropRow>
          )}
          {visibleOptionalProps.has("due_date") && (
            <PropRow label={t(($) => $.detail.prop_due_date)}>
              <DueDatePicker
                dueDate={issue.due_date}
                onUpdate={handleUpdateField}
                defaultOpen={autoOpenProp === "due_date"}
              />
            </PropRow>
          )}
          {visibleOptionalProps.has("labels") && (
            <PropRow label={t(($) => $.detail.prop_labels)}>
              <LabelPicker
                issueId={issue.id}
                align="start"
                defaultOpen={autoOpenProp === "labels"}
              />
            </PropRow>
          )}

          {/* "+ Add property" — opens a Popover listing optional fields
              not yet displayed. Hidden once every optional field is on
              screen. Sits inside the same grid as a full-row, with its
              own padding so the visual rhythm follows the rows above. */}
          {OPTIONAL_PROP_KEYS.some((k) => !visibleOptionalProps.has(k) && (k !== "stage" || issue.parent_issue_id != null)) && (
            <div className="col-span-2 mt-1">
              <Popover open={addPropPopoverOpen} onOpenChange={setAddPropPopoverOpen}>
                <PopoverTrigger
                  className="flex items-center gap-1.5 rounded-md px-2 py-1 -mx-2 text-xs text-muted-foreground hover:bg-accent/50 hover:text-foreground transition-colors"
                >
                  <Plus className="h-3 w-3 shrink-0" />
                  <span>{t(($) => $.detail.add_property_action)}</span>
                </PopoverTrigger>
                {/* Item visuals mirror the inspector rows' typography
                    (text-xs, muted icons) and each option leads with the
                    icon the resulting picker uses, so the dropdown reads
                    as a preview of what will show up below. */}
                <PopoverContent align="start" className="w-44 p-1">
                  {OPTIONAL_PROP_KEYS.filter((k) => !visibleOptionalProps.has(k) && (k !== "stage" || issue.parent_issue_id != null)).map((k) => (
                    <button
                      key={k}
                      type="button"
                      onClick={() => addOptionalProp(k)}
                      className="flex w-full items-center gap-2 rounded-md px-2 py-1 text-xs text-foreground/90 transition-colors hover:bg-accent focus-visible:bg-accent focus-visible:outline-none"
                    >
                      {k === "priority" && (
                        <PriorityIcon priority="medium" inheritColor className="text-muted-foreground" />
                      )}
                      {k === "stage" && (
                        <Milestone className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
                      )}
                      {k === "start_date" && (
                        <CalendarClock className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
                      )}
                      {k === "due_date" && (
                        <CalendarDays className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
                      )}
                      {k === "labels" && (
                        <Tag className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
                      )}
                      <span className="truncate">
                        {k === "priority" && t(($) => $.detail.prop_priority)}
                        {k === "stage" && t(($) => $.detail.prop_stage)}
                        {k === "start_date" && t(($) => $.detail.prop_start_date)}
                        {k === "due_date" && t(($) => $.detail.prop_due_date)}
                        {k === "labels" && t(($) => $.detail.prop_labels)}
                      </span>
                    </button>
                  ))}
                </PopoverContent>
              </Popover>
            </div>
          )}
        </div>}
      </div>

      {/* Parent issue — standalone section, only when the issue has a
          parent. Setting a parent is reachable via the issue actions menu;
          this card surfaces an existing parent without occupying sidebar
          space for issues that don't have one. */}
      {parentIssue && (
        <div>
          <button
            type="button"
            className={`flex w-full items-center gap-1 rounded-md px-2 py-1 text-xs font-medium transition-colors mb-2 hover:bg-accent/70 ${parentIssueOpen ? "" : "text-muted-foreground hover:text-foreground"}`}
            onClick={() => setParentIssueOpen(!parentIssueOpen)}
          >
            {t(($) => $.detail.section_parent_issue)}
            <ChevronRight className={`!size-3 shrink-0 stroke-[2.5] text-muted-foreground transition-transform ${parentIssueOpen ? "rotate-90" : ""}`} />
          </button>
          {parentIssueOpen && <div className="pl-2">
            <div className="flex items-center gap-0.5 rounded-md px-2 -mx-2 hover:bg-accent/50 transition-colors group">
              <AppLink
                href={paths.issueDetail(parentIssue.id)}
                className="flex flex-1 min-w-0 items-center gap-1.5 py-1.5 text-xs"
              >
                <StatusIcon status={parentIssue.status} className="h-3.5 w-3.5 shrink-0" />
                <span className="text-muted-foreground shrink-0">{parentIssue.identifier}</span>
                <span className="truncate group-hover:text-foreground">{parentIssue.title}</span>
              </AppLink>
              <button
                type="button"
                title={t(($) => $.actions.remove_parent_issue)}
                aria-label={t(($) => $.actions.remove_parent_issue)}
                onClick={() => actions.removeParent()}
                className="shrink-0 rounded p-1 text-muted-foreground opacity-0 transition-opacity hover:bg-accent hover:text-foreground focus-visible:opacity-100 group-hover:opacity-100"
              >
                <Unlink className="h-3.5 w-3.5" />
              </button>
            </div>
          </div>}
        </div>
      )}

      {/* Pull requests — hidden when the workspace disables the PR sidebar
          (or the GitHub master switch is off). Backend data is kept either
          way so re-enabling restores the section instantly. */}
      {githubSettings.prSidebar && (
        <div>
          <button
            type="button"
            className={`flex w-full items-center gap-1 rounded-md px-2 py-1 text-xs font-medium transition-colors mb-2 hover:bg-accent/70 ${pullRequestsOpen ? "" : "text-muted-foreground hover:text-foreground"}`}
            onClick={() => setPullRequestsOpen(!pullRequestsOpen)}
          >
            {t(($) => $.detail.section_pull_requests)}
            <ChevronRight className={`!size-3 shrink-0 stroke-[2.5] text-muted-foreground transition-transform ${pullRequestsOpen ? "rotate-90" : ""}`} />
          </button>
          {pullRequestsOpen && <div className="pl-2"><PullRequestList issueId={id} /></div>}
        </div>
      )}

      {/* Details */}
      <div>
        <button
          type="button"
          className={`flex w-full items-center gap-1 rounded-md px-2 py-1 text-xs font-medium transition-colors mb-2 hover:bg-accent/70 ${detailsOpen ? "" : "text-muted-foreground hover:text-foreground"}`}
          onClick={() => setDetailsOpen(!detailsOpen)}
        >
          {t(($) => $.detail.section_details)}
          <ChevronRight className={`!size-3 shrink-0 stroke-[2.5] text-muted-foreground transition-transform ${detailsOpen ? "rotate-90" : ""}`} />
        </button>
        {detailsOpen && <div className="grid grid-cols-[auto_1fr] gap-x-2 gap-y-0.5 pl-2">
          <PropRow label={t(($) => $.detail.prop_created_by)}>
            <ActorAvatar actorType={issue.creator_type} actorId={issue.creator_id} size={18} enableHoverCard />
            <span className="cursor-pointer truncate">{getActorName(issue.creator_type, issue.creator_id)}</span>
          </PropRow>
          <PropRow label={t(($) => $.detail.prop_created)}>
            <span className="text-muted-foreground">{shortDate(issue.created_at)}</span>
          </PropRow>
          <PropRow label={t(($) => $.detail.prop_updated)}>
            <span className="text-muted-foreground">{shortDate(issue.updated_at)}</span>
          </PropRow>
        </div>}
      </div>

      {/* Execution log — active runs + collapsed past runs. Self-contained;
          owns its own collapse state and WS subscriptions. Hides itself
          when there are no runs to show. */}
      <ExecutionLogSection issueId={id} />

      {/* Token usage */}
      {usage && usage.task_count > 0 && (
        <div>
          <button
            type="button"
            className={`flex w-full items-center gap-1 rounded-md px-2 py-1 text-xs font-medium transition-colors mb-2 hover:bg-accent/70 ${tokenUsageOpen ? "" : "text-muted-foreground hover:text-foreground"}`}
            onClick={() => setTokenUsageOpen(!tokenUsageOpen)}
          >
            {t(($) => $.detail.section_token_usage)}
            <ChevronRight className={`!size-3 shrink-0 stroke-[2.5] text-muted-foreground transition-transform ${tokenUsageOpen ? "rotate-90" : ""}`} />
          </button>
          {tokenUsageOpen && <div className="grid grid-cols-[auto_1fr] gap-x-2 gap-y-0.5 pl-2">
            <PropRow label={t(($) => $.detail.prop_input)}>
              <span className="text-muted-foreground">{formatTokenCount(usage.total_input_tokens)}</span>
            </PropRow>
            <PropRow label={t(($) => $.detail.prop_output)}>
              <span className="text-muted-foreground">{formatTokenCount(usage.total_output_tokens)}</span>
            </PropRow>
            {(usage.total_cache_read_tokens > 0 || usage.total_cache_write_tokens > 0) && (
              <PropRow label={t(($) => $.detail.prop_cache)}>
                <span className="text-muted-foreground">
                  {t(($) => $.detail.prop_cache_value, {
                    read: formatTokenCount(usage.total_cache_read_tokens),
                    write: formatTokenCount(usage.total_cache_write_tokens),
                  })}
                </span>
              </PropRow>
            )}
            <PropRow label={t(($) => $.detail.prop_runs)}>
              <span className="text-muted-foreground">{usage.task_count}</span>
            </PropRow>
          </div>}
        </div>
      )}

      {/* Metadata — agent-facing free-form KV bag. The values almost
          never mean anything to humans, so the trigger row matches the
          sibling section headers (Pull requests / Details / Parent issue)
          but clicking opens a dialog with the raw JSON instead of expanding
          inline — the payload can be large and pushing the rest of the
          sidebar down was noisy. */}
      {Object.keys(issue.metadata ?? {}).length > 0 && (
        <>
          <button
            type="button"
            className="flex w-full items-center gap-1 rounded-md px-2 py-1 text-xs font-medium text-muted-foreground transition-colors hover:bg-accent/70 hover:text-foreground"
            onClick={() => setMetadataOpen(true)}
          >
            {t(($) => $.detail.section_metadata)}
            <span className="tabular-nums">
              · {Object.keys(issue.metadata ?? {}).length}
            </span>
          </button>
          <Dialog open={metadataOpen} onOpenChange={setMetadataOpen}>
            <DialogContent className="max-w-2xl">
              <DialogHeader>
                <DialogTitle>{t(($) => $.detail.section_metadata)}</DialogTitle>
              </DialogHeader>
              <pre className="max-h-[60vh] overflow-auto rounded-md bg-muted p-3 font-mono text-xs">
                {JSON.stringify(issue.metadata ?? {}, null, 2)}
              </pre>
            </DialogContent>
          </Dialog>
        </>
      )}
    </div>
  );

  // Shared row renderer for both timeline render modes (flat / virtualized).
  // The wrapper `id="comment-..."` is the deep-link target — equivalent to
  // a native `<a href="#comment-...">` anchor.
  const renderItem = (_i: number, item: TimelineItem): React.ReactElement => {
    const activeSearchId = activeIssueSearchResult?.id ?? null;
    if (item.kind === "resolved-bar") {
      return (
        <div className="pb-3" id={`comment-${item.id}`}>
          <ResolvedThreadBar
            entry={item.entry}
            replies={timelineView.threadReplies.get(item.id) ?? EMPTY_REPLIES}
            onExpand={() => toggleResolvedExpand(item.id, true)}
          />
        </div>
      );
    }
    if (item.kind === "comment") {
      const isResolved = !!item.entry.resolved_at;
      return (
        <div className="pb-3" id={`comment-${item.id}`}>
          <CommentCard
            issueId={id}
            entry={item.entry}
            replies={timelineView.threadReplies.get(item.id) ?? EMPTY_REPLIES}
            currentUserId={user?.id}
            canModerate={canModerateComments}
            onReply={submitReply}
            onEdit={editComment}
            onDelete={deleteComment}
            onToggleReaction={handleToggleReaction}
            onResolveToggle={handleResolveToggle}
            onCollapseResolved={isResolved ? () => toggleResolvedExpand(item.id, false) : undefined}
            expandedResolvedIds={expandedResolved}
            onResolvedExpandChange={toggleResolvedExpand}
            highlightedCommentId={highlightedId}
            searchQuery={issueSearchQuery}
            activeSearchResultId={activeSearchId}
          />
        </div>
      );
    }
    // activity-group
    const expanded = expandedActivityIds.has(item.id)
      ? true
      : collapsedActivityIds.has(item.id)
        ? false
        : item.id === newestActivityGroupId;
    const truncateOlder = item.id === newestActivityGroupId;
    const showOlder = showOlderActivityIds.has(item.id);
    return (
      <ActivityBlock
        entries={item.entries}
        searchQuery={issueSearchQuery}
        activeSearchResultId={activeSearchId}
        expanded={expanded}
        onToggle={() => toggleActivityBlock(item.id, expanded)}
        truncateOlder={truncateOlder}
        showOlder={showOlder}
        onToggleShowOlder={() => showOlderActivities(item.id)}
        getActorName={getActorName}
        t={t}
        timeAgo={timeAgo}
      />
    );
  };

  // Breadcrumb shows the single most-direct container, never a fabricated chain.
  // project_id and parent_issue_id are orthogonal (a sub-issue can live in a
  // different project than its parent), so we never render both: parent wins,
  // else project, else nothing. The project is still shown in the properties
  // panel. The workspace name is intentionally absent — "all issues" is a view,
  // not a container.
  const breadcrumbSegments: BreadcrumbSegment[] = parentIssue
    ? [{ href: paths.issueDetail(parentIssue.id), label: parentIssue.identifier }]
    : breadcrumbProject
      ? [
          {
            href: paths.projectDetail(breadcrumbProject.id),
            className: "flex items-center gap-1 min-w-0 max-w-72",
            label: (
              <>
                <ProjectIcon project={breadcrumbProject} size="sm" />
                <span className="min-w-0 truncate">{breadcrumbProject.title}</span>
              </>
            ),
          },
        ]
      : [];

  const detailContent = (
    <div className="flex h-full min-w-0 flex-1 flex-col">
        <BreadcrumbHeader
          segments={breadcrumbSegments}
          leaf={
            <AppLink
              href={paths.issueDetail(issue.id)}
              className="flex min-w-0 transition-opacity hover:opacity-80"
            >
              <span className="truncate font-medium text-foreground">
                {issue.identifier} {issue.title}
              </span>
            </AppLink>
          }
          actions={
            <>
            {/* Live "agent is working" chip, leftmost in the right cluster so
                it never overlaps the title (which truncates to make room).
                It self-hides when no agent is active. */}
            <IssueAgentHeaderChip issueId={id} />
            <Tooltip>
              <TooltipTrigger
                render={
                  <Button
                    variant={issueSearchOpen ? "secondary" : "ghost"}
                    size="icon-sm"
                    className={issueSearchOpen ? "" : "text-muted-foreground"}
                    onClick={() => {
                      if (issueSearchOpen) {
                        closeIssueSearch();
                      } else {
                        setIssueSearchOpen(true);
                      }
                    }}
                    aria-label={t(($) => $.detail.issue_search_tooltip)}
                  >
                    <Search />
                  </Button>
                }
              />
              <TooltipContent side="bottom">{t(($) => $.detail.issue_search_tooltip)}</TooltipContent>
            </Tooltip>
            {onDone && issue.status !== "done" && issue.status !== "cancelled" && (
              <Tooltip>
                <TooltipTrigger
                  render={
                    <Button
                      variant="ghost"
                      size="icon-sm"
                      className="text-muted-foreground"
                      onClick={() => { handleUpdateField({ status: "done" }); onDone?.(); }}
                    >
                      <CircleCheck />
                    </Button>
                  }
                />
                <TooltipContent side="bottom">{t(($) => $.detail.mark_done_tooltip)}</TooltipContent>
              </Tooltip>
            )}
            {onDone && issue.status === "done" && (
              <Tooltip>
                <TooltipTrigger
                  render={
                    <Button
                      variant="ghost"
                      size="icon-sm"
                      className="text-muted-foreground"
                      onClick={() => { onDone(); }}
                    >
                      <Archive />
                    </Button>
                  }
                />
                <TooltipContent side="bottom">{t(($) => $.detail.archive_tooltip)}</TooltipContent>
              </Tooltip>
            )}
            <Tooltip>
              <TooltipTrigger
                render={
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    className={cn("text-muted-foreground", actions.isPinned && "text-foreground")}
                    onClick={actions.togglePin}
                  >
                    {actions.isPinned ? <PinOff /> : <Pin />}
                  </Button>
                }
              />
              <TooltipContent side="bottom">{actions.isPinned ? t(($) => $.detail.unpin_tooltip) : t(($) => $.detail.pin_tooltip)}</TooltipContent>
            </Tooltip>
            <IssueActionsDropdown
              issue={issue}
              align="end"
              // When a parent passes `onDelete`, we detect deletion via effect
              // above and skip navigation. Otherwise the modal navigates for us.
              onDeletedNavigateTo={onDelete ? undefined : paths.issues()}
              trigger={
                <Button variant="ghost" size="icon-sm" className="text-muted-foreground">
                  <MoreHorizontal />
                </Button>
              }
            />
            <Tooltip>
              <TooltipTrigger
                render={
                  <Button
                    variant={sidebarOpen ? "secondary" : "ghost"}
                    size="icon-sm"
                    className={sidebarOpen ? "" : "text-muted-foreground"}
                    onClick={handleToggleSidebar}
                  >
                    <PanelRight />
                  </Button>
                }
              />
              <TooltipContent side="bottom">{t(($) => $.detail.sidebar_tooltip)}</TooltipContent>
            </Tooltip>
            </>
          }
        />

        <div
          ref={setScrollContainerEl}
          data-tab-scroll-root
          className="relative flex-1 overflow-y-auto"
        >
        {issueSearchOpen && (
          <div className="sticky top-0 z-30 border-b bg-background/95 px-8 py-2 backdrop-blur">
            <div className="mx-auto flex w-full max-w-4xl items-center gap-2">
              <Search className="h-4 w-4 shrink-0 text-muted-foreground" />
              <Input
                ref={issueSearchInputRef}
                value={issueSearchQuery}
                onChange={(event) => {
                  setIssueSearchQuery(event.target.value);
                  setActiveIssueSearchIndex(0);
                }}
                placeholder={t(($) => $.detail.issue_search_placeholder)}
                className="h-8 flex-1"
              />
              <span className="w-14 shrink-0 text-center text-xs tabular-nums text-muted-foreground">
                {issueSearchResults.length > 0
                  ? t(($) => $.detail.issue_search_count, {
                      current: activeIssueSearchIndex + 1,
                      total: issueSearchResults.length,
                    })
                  : t(($) => $.detail.issue_search_count_empty)}
              </span>
              <Button
                type="button"
                variant="ghost"
                size="icon-sm"
                disabled={issueSearchResults.length === 0}
                onClick={() => moveIssueSearch(-1)}
                aria-label={t(($) => $.detail.issue_search_previous)}
              >
                <ChevronLeft />
              </Button>
              <Button
                type="button"
                variant="ghost"
                size="icon-sm"
                disabled={issueSearchResults.length === 0}
                onClick={() => moveIssueSearch(1)}
                aria-label={t(($) => $.detail.issue_search_next)}
              >
                <ChevronRight />
              </Button>
              <Button
                type="button"
                variant="ghost"
                size="icon-sm"
                onClick={closeIssueSearch}
                aria-label={t(($) => $.detail.issue_search_close)}
              >
                <X />
              </Button>
            </div>
          </div>
        )}
        <div className="mx-auto w-full max-w-4xl px-8 py-8">
          <div id="issue-search-title">
            {issueSearchQuery.trim() ? (
              <h1 className="w-full text-2xl font-bold leading-snug tracking-tight">
                <HighlightIssueText
                  text={issue.title}
                  query={issueSearchQuery}
                  active={activeIssueSearchResult?.kind === "title"}
                  activeMatchIndex={activeIssueSearchResult?.kind === "title" ? activeIssueSearchResult.matchIndex : undefined}
                />
              </h1>
            ) : (
              <TitleEditor
                key={`title-${id}`}
                defaultValue={issue.title}
                placeholder={t(($) => $.detail.title_placeholder)}
                className="w-full text-2xl font-bold leading-snug tracking-tight"
                onBlur={(value) => {
                  const trimmed = value.trim();
                  if (trimmed && trimmed !== issue.title) handleUpdateField({ title: trimmed });
                }}
              />
            )}
          </div>

          {parentIssue && (
            <AppLink
              href={paths.issueDetail(parentIssue.id)}
              className="mt-2 inline-flex max-w-full items-center gap-1.5 text-xs text-muted-foreground hover:text-foreground transition-colors group/parent"
            >
              <span className="font-medium shrink-0">{t(($) => $.detail.sub_issue_of)}</span>
              <StatusIcon status={parentIssue.status} className="h-3.5 w-3.5 shrink-0" />
              <span className="tabular-nums shrink-0">{parentIssue.identifier}</span>
              <span className="truncate group-hover/parent:text-foreground">
                {parentIssue.title}
              </span>
              {parentChildIssues.length > 0 && (() => {
                const done = parentChildIssues.filter((c) => c.status === "done").length;
                return (
                  <span className="ml-1 inline-flex items-center gap-1 rounded-full bg-muted/60 px-1.5 py-0.5 shrink-0">
                    <ProgressRing done={done} total={parentChildIssues.length} size={11} />
                    <span className="tabular-nums text-[10.5px] font-medium">
                      {done}/{parentChildIssues.length}
                    </span>
                  </span>
                );
              })()}
            </AppLink>
          )}

          <div {...descDropZoneProps} id="issue-search-description" className="relative mt-5 rounded-lg">
            {issueSearchQuery.trim() ? (
              <div className="min-h-16 whitespace-pre-wrap text-sm leading-relaxed text-foreground/85">
                <HighlightIssueText
                  text={issueSearchVisibleText(issue.description)}
                  query={issueSearchQuery}
                  active={activeIssueSearchResult?.kind === "description"}
                  activeMatchIndex={activeIssueSearchResult?.kind === "description" ? activeIssueSearchResult.matchIndex : undefined}
                />
              </div>
            ) : (
              <ContentEditor
                ref={descEditorRef}
                key={id}
                defaultValue={issue.description || ""}
                placeholder={t(($) => $.detail.desc_placeholder)}
                onUpdate={(md) => {
                  // Bind any pending uploads still referenced in the markdown
                  // so they appear in `issueAttachments` after refresh and the
                  // editor's text/code preview keeps working past reload.
                  //
                  // Match with `contentReferencesAttachment`, NOT `md.includes(a.url)`:
                  // the editor persists the durable `markdownLink`
                  // (`/api/attachments/<id>/download` / `markdown_url`) into the
                  // body, never the raw storage `a.url`. A bare `md.includes(a.url)`
                  // therefore never matches, so the upload is never linked via
                  // `attachment_ids`. After reload it's absent from
                  // `issueAttachments`, the renderer can't resolve it to a
                  // freshly-signed `download_url`, and the persisted auth-gated
                  // download endpoint fails to load as a native <img> on clients
                  // whose origin isn't the API host (Desktop/Electron, mobile
                  // webview) — while still working on web via the cookie/proxy.
                  // This mirrors the comment/reply/chat composers, which already
                  // bind via `contentReferencesAttachment` (MUL-3130 / MUL-3192).
                  const ids = descPendingAttachmentsRef.current
                    .filter((a) => contentReferencesAttachment(md, a))
                    .map((a) => a.id);
                  handleUpdateField({ description: md, attachment_ids: ids.length > 0 ? ids : undefined });
                }}
                onUploadFile={handleDescriptionUpload}
                debounceMs={1500}
                // Closing the issue modal must save what the user last saw —
                // without the flush, a paste followed by a quick close loses
                // the image markdown and its attachment_ids bind (MUL-3254).
                flushPendingOnUnmount
                currentIssueId={id}
                attachments={descEditorAttachments}
              />
            )}

            <div className="flex items-center gap-1 mt-3">
              <ReactionBar
                reactions={issueReactions}
                currentUserId={user?.id}
                onToggle={handleToggleIssueReaction}
                getActorName={getActorName}
              />
              <FileUploadButton
                size="sm"
                onSelect={(file) => descEditorRef.current?.uploadFile(file)}
              />
            </div>
            {descDragOver && <FileDropOverlay />}
          </div>

          {/* Sub-issues — Linear-style */}
          {childIssues.length === 0 && (
            <div className="mt-6">
              <button
                type="button"
                className="inline-flex items-center gap-1.5 text-xs text-muted-foreground hover:text-foreground transition-colors"
                onClick={() => actions.openCreateSubIssue()}
              >
                <Plus className="h-3.5 w-3.5" />
                <span>{t(($) => $.detail.add_sub_issues)}</span>
              </button>
            </div>
          )}
          {childIssues.length > 0 && (() => {
            const doneCount = childIssues.filter((c) => c.status === "done").length;
            return (
              <div className="mt-10 group/sub-issues">
                {/* Header */}
                <div className="flex items-center gap-2 mb-2">
                  <button
                    type="button"
                    onClick={() => setSubIssuesCollapsed((v) => !v)}
                    className="flex items-center gap-1.5 text-sm font-medium text-foreground hover:text-foreground/80 transition-colors"
                  >
                    <ChevronDown
                      className={cn(
                        "h-3.5 w-3.5 text-muted-foreground transition-transform",
                        subIssuesCollapsed && "-rotate-90",
                      )}
                    />
                    <span>{t(($) => $.detail.sub_issues_label)}</span>
                  </button>
                  <div className="inline-flex items-center gap-1.5 rounded-full bg-muted/60 px-2 py-0.5">
                    <ProgressRing done={doneCount} total={childIssues.length} size={11} />
                    <span className="text-[11px] text-muted-foreground tabular-nums font-medium">
                      {doneCount}/{childIssues.length}
                    </span>
                  </div>
                  <input
                    type="checkbox"
                    checked={allChildrenSelected}
                    ref={(el) => {
                      if (el) el.indeterminate = someChildrenSelected && !allChildrenSelected;
                    }}
                    onChange={handleToggleSelectAllChildren}
                    aria-label="Select all sub-issues"
                    className={cn(
                      "ml-1 cursor-pointer accent-primary transition-opacity",
                      someChildrenSelected
                        ? "opacity-100"
                        : "opacity-0 group-hover/sub-issues:opacity-100 focus-visible:opacity-100",
                    )}
                  />
                  <Tooltip>
                    <TooltipTrigger
                      render={
                        <button
                          type="button"
                          className="ml-auto inline-flex h-7 w-7 items-center justify-center rounded-md text-muted-foreground hover:bg-accent hover:text-foreground transition-colors"
                          onClick={() => actions.openCreateSubIssue()}
                          aria-label={t(($) => $.detail.add_sub_issue_aria)}
                        >
                          <Plus className="h-4 w-4" />
                        </button>
                      }
                    />
                    <TooltipContent side="bottom">{t(($) => $.detail.add_sub_issue_tooltip)}</TooltipContent>
                  </Tooltip>
                </div>

                {/* Inline batch toolbar — appears next to the rows when
                    selections exist, instead of as a far-away fixed bar. */}
                <BatchActionToolbar issues={childIssues} placement="inline" />

                {/* List */}
                {!subIssuesCollapsed && (() => {
                  const groups = groupSubIssuesByStage(childIssues);
                  const staged = childIssues.some((c) => c.stage != null);
                  return (
                    <div className="overflow-hidden rounded-lg border bg-card/30 divide-y divide-border/60">
                      {groups.map(({ stage: groupStage, items }) => (
                        <Fragment key={groupStage ?? "unstaged"}>
                          {staged && (
                            <div className="bg-muted/40 px-3 py-1 text-[11px] font-medium uppercase tracking-wider text-muted-foreground">
                              {groupStage == null
                                ? t(($) => $.stage.none)
                                : t(($) => $.stage.value, { n: groupStage })}
                            </div>
                          )}
                          {items.map((child) => (
                            <SubIssueRow key={child.id} child={child} />
                          ))}
                        </Fragment>
                      ))}
                    </div>
                  );
                })()}
              </div>
            );
          })()}

          <div className="my-8 border-t" />

          {/* Activity / Comments */}
          <div ref={activitySectionRef} data-comment-nav-anchor>
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-3">
                <h2 className="text-base font-semibold">{t(($) => $.detail.activity_section)}</h2>
              </div>
              <div className="flex items-center gap-2">
                <Tooltip>
                  <TooltipTrigger
                    render={
                      <Button
                        variant={commentNavOpen ? "secondary" : "ghost"}
                        size="sm"
                        className={cn("h-7 gap-1.5 px-2 text-xs", !commentNavOpen && "text-muted-foreground")}
                        onClick={toggleCommentNav}
                        aria-label={t(($) => $.detail.comment_nav_toggle)}
                        aria-pressed={commentNavOpen}
                      >
                        <ListTree className="h-3.5 w-3.5" />
                        <span className="hidden sm:inline">{t(($) => $.detail.comment_nav_label)}</span>
                      </Button>
                    }
                  />
                  <TooltipContent side="bottom">{t(($) => $.detail.comment_nav_toggle)}</TooltipContent>
                </Tooltip>
                <button
                  type="button"
                  onClick={handleToggleSubscribe}
                  className="text-xs text-muted-foreground hover:text-foreground transition-colors"
                >
                  {isSubscribed ? t(($) => $.detail.unsubscribe) : t(($) => $.detail.subscribe)}
                </button>
                <Popover>
                  <PopoverTrigger className="cursor-pointer hover:opacity-80 transition-opacity">
                    {subscribers.length > 0 ? (
                      <AvatarGroup>
                        {subscribers.slice(0, 4).map((sub) => (
                          <ActorAvatar
                            key={`${sub.user_type}-${sub.user_id}`}
                            actorType={sub.user_type}
                            actorId={sub.user_id}
                            size={24}
                            enableHoverCard
                          />
                        ))}
                        {subscribers.length > 4 && (
                          <AvatarGroupCount>+{subscribers.length - 4}</AvatarGroupCount>
                        )}
                      </AvatarGroup>
                    ) : (
                      <span className="flex items-center justify-center h-6 w-6 rounded-full border border-dashed border-muted-foreground/30 text-muted-foreground">
                        <Users className="h-3 w-3" />
                      </span>
                    )}
                  </PopoverTrigger>
                  <SubscriberPopoverContent
                    members={members}
                    agents={agents}
                    subscribers={subscribers}
                    toggleSubscriber={toggleSubscriber}
                    t={t}
                  />
                </Popover>
              </div>
            </div>

            <LocalDirectoryHint projectId={issue?.project_id} />

            <div className="relative">
              {showCommentNav && commentNavPosition && (
                <nav
                  aria-label={t(($) => $.detail.comment_nav_label)}
                  className="fixed z-40"
                  style={{
                    top: commentNavPosition.top,
                    right: commentNavPosition.right,
                    width: commentNavPosition.width,
                  }}
                >
                  <div
                    className="overflow-x-hidden overflow-y-auto overscroll-contain pr-1"
                    style={{ maxHeight: commentNavPosition.maxHeight }}
                  >
                    <div className="flex flex-col">
                      {commentNavItems.map((item, index) => {
                        const active = (activeCommentNavId ?? commentNavItems[0]?.id) === item.id;
                        return (
                          <button
                            key={item.id}
                            type="button"
                            onClick={() => handleCommentNavClick(item.id)}
                            className="group flex min-h-7 w-full items-center gap-2 text-left text-xs text-muted-foreground hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
                            aria-label={t(($) => $.detail.comment_nav_item_aria, {
                              index: index + 1,
                              text: item.label,
                            })}
                            aria-current={active ? "true" : undefined}
                          >
                            <span className="flex w-4 shrink-0 flex-col items-center self-stretch">
                              {index > 0 && <span className="h-2 w-px bg-border" aria-hidden />}
                              <span
                                className={cn(
                                  "h-2.5 w-2.5 rounded-full border bg-background transition-colors",
                                  active ? "border-primary bg-primary" : "border-muted-foreground/40 group-hover:border-foreground",
                                )}
                                aria-hidden
                              />
                              {index < commentNavItems.length - 1 && <span className="min-h-2 flex-1 w-px bg-border" aria-hidden />}
                            </span>
                            {commentNavPosition.width >= 120 && (
                              <span className="min-w-0 flex-1 truncate">{item.label}</span>
                            )}
                          </button>
                        );
                      })}
                    </div>
                  </div>
                </nav>
              )}

            {/* The "agent is working" live signal now lives in the header
                (IssueAgentHeaderChip) so it stays in one fixed place and
                doesn't compete with sticky banners in this content column.
                The per-task timeline + past runs live in the right panel
                via ExecutionLogSection. */}

            {/* Timeline entries — virtualized via react-virtuoso to keep
                first-paint cost O(viewport) instead of O(N). On a 500-comment
                issue the unvirtualized .map froze the page for several
                seconds (markdown parse + lowlight code highlight runs per
                CommentCard on mount).

                customScrollParent guard: callback ref populates after the
                first commit. Without this null guard Virtuoso falls back to
                its own scroller, grabs 0 height inside overflow-y-auto, and
                miscomputes total-height on first paint. */}
            {timelineLoading && timelineView.groups.length === 0 ? (
              <TimelineSkeleton />
            ) : (
              // Two render modes:
              //   - `highlightCommentId` set (came from inbox deep-link) →
              //     render flat. Every comment mounts, every height is real,
              //     the target id is in the DOM the instant the useEffect
              //     above runs `scrollIntoView`. No virtualization estimate
              //     errors, no spacer reflow drift. Pays cold-mount cost
              //     proportional to items.length (markdown + lowlight per
              //     comment), which is acceptable in the deep-link case —
              //     the user has explicit intent to land on a specific item.
              //   - otherwise → Virtuoso. Browsing mode, virtualization
              //     wins on first-paint perf for long timelines.
              //
              // The split is deliberate: virtualization and "land precisely
              // on a target" have fundamentally opposed contracts (estimated
              // heights vs real heights). Trying to satisfy both in one
              // path is what produced the bug history this PR closes.
              !highlightCommentId && !issueSearchQuery.trim() ? (
                !scrollContainerEl ? (
                  // Skeleton while the callback ref populates so the gap
                  // between IssueDetail mount and Virtuoso mount doesn't
                  // flash empty.
                  <TimelineSkeleton />
                ) : (
                  <div className="mt-4">
                    <Virtuoso
                      ref={timelineVirtuosoRef}
                      key={`${wsId}:${id}`}
                      customScrollParent={scrollContainerEl}
                      data={items}
                      increaseViewportBy={{ top: 800, bottom: 800 }}
                      computeItemKey={(_i, item) => `${item.kind}:${item.id}`}
                      skipAnimationFrameInResizeObserver
                      // followOutput intentionally NOT set. Virtuoso treats
                      // it as a sticky "is at bottom" flag and resets
                      // scrollTop to maxScrollTop on every height-change
                      // tick — issue-detail is document-shaped, not chat.
                      itemContent={renderItem}
                    />
                  </div>
                )
              ) : (
                <div className="mt-4">
                  {items.map((item, i) => (
                    <Fragment key={`${item.kind}:${item.id}`}>
                      {renderItem(i, item)}
                    </Fragment>
                  ))}
                </div>
              )
            )}

            {/* Bottom comment input — no avatar, full width */}
            <div className="mt-4">
              {/* key={id}: web's /issues/[id] route doesn't remount on
                  issueId change, so without an explicit key the editor
                  keeps the previous issue's in-memory content and the
                  next keystroke would flush it into the new issue's
                  draft key. */}
              <CommentInput key={id} issueId={id} onSubmit={submitComment} />
            </div>
            </div>
          </div>
        </div>
        </div>
      </div>
  );

  if (isMobile) {
    return (
      <div className="flex flex-1 min-h-0">
        {detailContent}
        <Sheet open={mobileSidebarOpen} onOpenChange={setMobileSidebarOpen}>
          <SheetContent side="right" showCloseButton={false} className="w-[320px] overflow-y-auto p-4">
            {sidebarContent}
          </SheetContent>
        </Sheet>
      </div>
    );
  }

  return (
    <ResizablePanelGroup orientation="horizontal" className="flex-1 min-h-0" defaultLayout={defaultLayout} onLayoutChanged={onLayoutChanged}>
      <ResizablePanel id="content" minSize="50%">
        {detailContent}
      </ResizablePanel>
      <ResizableHandle />
      <ResizablePanel
        id="sidebar"
        {...rightSidebarPanelMotionProps}
        data-right-sidebar-motion={desktopSidebarMotionEnabled ? "enabled" : undefined}
        defaultSize={desktopSidebarOpen ? 320 : 0}
        minSize={260}
        maxSize={420}
        collapsible
        groupResizeBehavior="preserve-pixel-size"
        panelRef={sidebarRef}
        onResize={handleDesktopSidebarResize}
      >
        <AnimatedRightSidebar open={desktopSidebarVisualOpen} motionEnabled={desktopSidebarMotionEnabled}>
          {sidebarContent}
        </AnimatedRightSidebar>
      </ResizablePanel>
    </ResizablePanelGroup>
  );
}
