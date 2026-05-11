"use client";

import { useState, useEffect, useCallback, useMemo, useRef } from "react";
import { Virtuoso, type VirtuosoHandle } from "react-virtuoso";
import { useDefaultLayout, usePanelRef } from "react-resizable-panels";
import { AppLink } from "../../navigation";
import { useNavigation } from "../../navigation";
import {
  Archive,
  Calendar,
  ChevronDown,
  ChevronLeft,
  ChevronRight,
  CircleCheck,
  MoreHorizontal,
  PanelRight,
  Pin,
  PinOff,
  Plus,
  Users,
} from "lucide-react";
import { PageHeader } from "../../layout/page-header";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Button } from "@multica/ui/components/ui/button";
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
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { Command, CommandInput, CommandList, CommandEmpty, CommandGroup, CommandItem } from "@multica/ui/components/ui/command";
import { AvatarGroup, AvatarGroupCount } from "@multica/ui/components/ui/avatar";
import { ActorAvatar } from "../../common/actor-avatar";
import { PropRow } from "../../common/prop-row";
import type { Issue, IssueStatus, IssuePriority, TimelineEntry, UpdateIssueRequest } from "@multica/core/types";
import { STATUS_CONFIG, PRIORITY_CONFIG } from "@multica/core/issues/config";
import { useUpdateIssue } from "@multica/core/issues/mutations";
import { toast } from "sonner";
import { StatusIcon, PriorityIcon, StatusPicker, PriorityPicker, DueDatePicker, AssigneePicker, LabelPicker } from ".";
import { IssueActionsDropdown, useIssueActions } from "../actions";
import { ProjectPicker } from "../../projects/components/project-picker";
import { CommentCard } from "./comment-card";
import { CommentInput } from "./comment-input";
import { ResolvedThreadBar } from "./resolved-thread-bar";
import { collectThreadReplies } from "./thread-utils";
import { AgentLiveCard } from "./agent-live-card";
import { ExecutionLogSection } from "./execution-log-section";
import { useQuery } from "@tanstack/react-query";
import { useAuthStore } from "@multica/core/auth";
import { useCurrentWorkspace, useWorkspacePaths } from "@multica/core/paths";
import { useActorName } from "@multica/core/workspace/hooks";
import { useWorkspaceId } from "@multica/core/hooks";
import { issueListOptions, issueDetailOptions, childIssuesOptions, issueUsageOptions, issueAttachmentsOptions } from "@multica/core/issues/queries";
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
import { timeAgo } from "@multica/core/utils";
import { cn } from "@multica/ui/lib/utils";

import { ProgressRing } from "./progress-ring";
import { useT } from "../../i18n";

function shortDate(date: string | null): string {
  if (!date) return "—";
  return new Date(date).toLocaleDateString("en-US", {
    month: "short",
    day: "numeric",
  });
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
    case "due_date_changed": {
      if (!details.to) return t(($) => $.activity.due_date_removed);
      const formatted = new Date(details.to).toLocaleDateString("en-US", { month: "short", day: "numeric" });
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
        { onError: () => toast.error(t(($) => $.detail.update_failed)) },
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
  const id = issueId;
  const router = useNavigation();
  const user = useAuthStore((s) => s.user);
  const workspace = useCurrentWorkspace();
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
  const [desktopSidebarOpen, setDesktopSidebarOpen] = useState(defaultSidebarOpen);
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
  const [tokenUsageOpen, setTokenUsageOpen] = useState(true);
  // Virtuoso's `customScrollParent` wants the HTMLElement, not a ref. A plain
  // `useRef.current` does not trigger a re-render when it populates, so the
  // Virtuoso prop would never receive the element. Callback ref + state fixes
  // that: setState triggers the re-render that hands Virtuoso the element.
  const [scrollContainerEl, setScrollContainerEl] = useState<HTMLDivElement | null>(null);
  const virtuosoRef = useRef<VirtuosoHandle>(null);
  const [highlightedId, setHighlightedId] = useState<string | null>(null);

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
  }, []);
  const clearResolvedExpand = useCallback((commentId: string) => {
    setExpandedResolved((prev) => {
      if (!prev.has(commentId)) return prev;
      const next = new Set(prev);
      next.delete(commentId);
      return next;
    });
  }, []);
  const didHighlightRef = useRef<string | null>(null);

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
  useEffect(() => {
    if (issue) {
      recordVisit(wsId, issue.id);
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
      clearResolvedExpand(commentId);
      toggleResolveComment(commentId, resolved);
    },
    [clearResolvedExpand, toggleResolveComment],
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

    // Pre-flatten each top-level comment's thread subtree (parent + every
    // descendant in render order). Reuse the previous array reference when
    // the thread is unchanged so unrelated CommentCards keep their memo.
    const prevThreadReplies = prevThreadRepliesRef.current;
    const threadReplies = new Map<string, TimelineEntry[]>();
    for (const root of topLevel) {
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
    const COALESCE_MS = 2 * 60 * 1000;
    const NO_TIME_LIMIT_ACTIONS = new Set(["task_completed", "task_failed"]);
    const coalesced: TimelineEntry[] = [];
    for (const entry of topLevel) {
      if (entry.type === "activity") {
        const prev = coalesced[coalesced.length - 1];
        if (
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

    // Group consecutive activities together so the connector line works
    const groups: { type: "activities" | "comment"; entries: TimelineEntry[] }[] = [];
    for (const entry of coalesced) {
      if (entry.type === "activity") {
        const last = groups[groups.length - 1];
        if (last?.type === "activities") {
          last.entries.push(entry);
        } else {
          groups.push({ type: "activities", entries: [entry] });
        }
      } else {
        groups.push({ type: "comment", entries: [entry] });
      }
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

  // Scroll to highlighted comment once the timeline (and target index) are
  // ready. Under virtualization the entry is not in the DOM until Virtuoso
  // mounts it, so we ask Virtuoso to scroll the index instead of querying the
  // DOM. `initialTopMostItemIndex` on <Virtuoso> handles the rough landing;
  // this is the precision pass that runs after ResizeObserver has measured
  // real heights and can correct any drift from the estimated landing.
  //
  // Double rAF: Virtuoso #883 — initialTopMostItemIndex / scrollToIndex are
  // racy if applied before the first ResizeObserver pass updates scrollHeight.
  // One rAF waits for mount, the nested rAF waits for measurement.
  // Highlight flash bumped 2s→3s to outlast mount latency on cold cards.
  useEffect(() => {
    if (!highlightCommentId || items.length === 0 || targetIdx < 0) return;
    if (didHighlightRef.current === highlightCommentId) return;

    // Strategy: Virtuoso's scrollToIndex is asynchronous — it queues an
    // internal state change that mounts the target a few frames later. The
    // target also isn't in the DOM until Virtuoso settles. So:
    //   1. Kick off Virtuoso to start moving toward the target
    //   2. Poll for the target's DOM element (max ~20 frames ≈ 320ms)
    //   3. Once found, use the browser's scrollIntoView which correctly
    //      accounts for all sibling content above the list
    //   4. After Virtuoso fully settles, scrollIntoView one more time —
    //      Virtuoso may have overwritten our scroll position
    const rafIds: number[] = [];
    const timeoutIds: number[] = [];
    const cancelled = { value: false };

    const findTarget = () => {
      // First try the exact id — works for root comments and for replies
      // whose enclosing thread is currently expanded.
      const direct = scrollContainerEl?.querySelector(
        `[data-comment-id="${CSS.escape(highlightCommentId)}"]`,
      ) as HTMLElement | null;
      if (direct) return direct;
      // Reply in a collapsed thread: fall back to the root wrapper so we at
      // least scroll the card containing the target into view.
      const rootId = replyToRoot.get(highlightCommentId);
      if (rootId) {
        return scrollContainerEl?.querySelector(
          `[data-comment-id="${CSS.escape(rootId)}"]`,
        ) as HTMLElement | null;
      }
      return null;
    };

    const performScroll = () => {
      const el = findTarget();
      if (!el) return false;
      el.scrollIntoView({ block: "center", behavior: "auto" });
      return true;
    };

    const pollMount = (attemptsLeft: number) => {
      if (cancelled.value) return;
      if (performScroll()) {
        setHighlightedId(highlightCommentId);
        didHighlightRef.current = highlightCommentId;
        // Done. No reanchor — polling already waits until Virtuoso has
        // settled enough to mount the target, so scrollIntoView IS the
        // last write. Adding a delayed re-scroll would yank the user back
        // if they scrolled away in the meantime.
        return;
      }
      if (attemptsLeft <= 0) {
        // Virtuoso didn't mount the target within ~320ms. Still set the
        // highlight so when the user manually scrolls there, the card
        // flashes. Mark handled so subsequent renders don't keep polling.
        // eslint-disable-next-line no-console
        console.warn(
          `[deep-link] target ${highlightCommentId} did not mount in time; highlight set without scroll`,
        );
        setHighlightedId(highlightCommentId);
        didHighlightRef.current = highlightCommentId;
        return;
      }
      const id = requestAnimationFrame(() => pollMount(attemptsLeft - 1));
      rafIds.push(id);
    };

    const start = requestAnimationFrame(() => {
      if (cancelled.value) return;
      // Step 1: kick Virtuoso so it begins mounting target index.
      virtuosoRef.current?.scrollToIndex({
        index: targetIdx,
        align: "center",
        behavior: "auto",
      });
      // Step 2-4: start polling for the DOM element.
      pollMount(20);
    });
    rafIds.push(start);

    const flash = window.setTimeout(() => setHighlightedId(null), 2000);
    timeoutIds.push(flash);

    return () => {
      cancelled.value = true;
      for (const id of rafIds) cancelAnimationFrame(id);
      for (const id of timeoutIds) clearTimeout(id);
    };
  }, [highlightCommentId, items.length, targetIdx, scrollContainerEl]);

  // Cmd-F / Ctrl-F on a virtualized timeline only searches what's mounted in
  // the viewport — off-screen comments are invisible to browser find-in-page.
  // Intercept once per (session, issue) when the list is long enough that the
  // user might actually try; let the keystroke pass through on short lists.
  // Real fix is in-app search (separate PR); this is the toast stopgap.
  useEffect(() => {
    if (items.length <= 30) return;
    const flagKey = `multica_cmdF_warned:${id}`;
    const handler = (e: KeyboardEvent) => {
      if (e.key !== "f" || !(e.metaKey || e.ctrlKey)) return;
      if (sessionStorage.getItem(flagKey)) return;
      e.preventDefault();
      sessionStorage.setItem(flagKey, "1");
      toast.message(t(($) => $.detail.cmdf_toast_title), {
        description: t(($) => $.detail.cmdf_toast_description),
      });
    };
    document.addEventListener("keydown", handler);
    return () => document.removeEventListener("keydown", handler);
  }, [id, items.length, t]);

  const descEditorRef = useRef<ContentEditorRef>(null);
  const { isDragOver: descDragOver, dropZoneProps: descDropZoneProps } = useFileDropZone({
    onDrop: (files) => files.forEach((f) => descEditorRef.current?.uploadFile(f)),
  });
  // Description uploads don't pass issueId — the URL lives in the markdown.
  // This avoids stale attachment records when users delete images from the editor.
  const handleDescriptionUpload = useCallback(
    (file: File) => uploadWithToast(file),
    [uploadWithToast],
  );

  // Shared issue actions (mutations, pin, copy-link, modal dispatch, etc.).
  // Called before the `if (!issue)` early return so hook order stays stable.
  const actions = useIssueActions(issue);
  const handleUpdateField = actions.updateField;

  const handleToggleSidebar = useCallback(() => {
    if (isMobile) {
      setMobileSidebarOpen((open) => !open);
      return;
    }

    const panel = sidebarRef.current;
    if (!panel) return;
    if (panel.isCollapsed()) panel.expand();
    else panel.collapse();
  }, [isMobile, sidebarRef]);

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
          className={`flex w-full items-center gap-1 rounded-md px-2 py-1 text-xs font-medium transition-colors mb-2 hover:bg-accent/70 ${propertiesOpen ? "" : "text-muted-foreground hover:text-foreground"}`}
          onClick={() => setPropertiesOpen(!propertiesOpen)}
        >
          {t(($) => $.detail.section_properties)}
          <ChevronRight className={`!size-3 shrink-0 stroke-[2.5] text-muted-foreground transition-transform ${propertiesOpen ? "rotate-90" : ""}`} />
        </button>
        {propertiesOpen && <div className="grid grid-cols-[auto_1fr] gap-x-2 gap-y-0.5 pl-2">
          <PropRow label={t(($) => $.detail.prop_status)}>
            <StatusPicker status={issue.status} onUpdate={handleUpdateField} align="start" />
          </PropRow>
          <PropRow label={t(($) => $.detail.prop_priority)}>
            <PriorityPicker priority={issue.priority} onUpdate={handleUpdateField} align="start" />
          </PropRow>
          <PropRow label={t(($) => $.detail.prop_assignee)}>
            <AssigneePicker assigneeType={issue.assignee_type} assigneeId={issue.assignee_id} onUpdate={handleUpdateField} align="start" />
          </PropRow>
          <PropRow label={t(($) => $.detail.prop_due_date)}>
            <DueDatePicker dueDate={issue.due_date} onUpdate={handleUpdateField} />
          </PropRow>
          <PropRow label={t(($) => $.detail.prop_project)}>
            <ProjectPicker projectId={issue.project_id} onUpdate={handleUpdateField} />
          </PropRow>
          <PropRow label={t(($) => $.detail.prop_labels)}>
            <LabelPicker issueId={issue.id} align="start" />
          </PropRow>
        </div>}
      </div>

      {/* Parent issue */}
      {parentIssue && (
        <div>
          <button
            className={`flex w-full items-center gap-1 rounded-md px-2 py-1 text-xs font-medium transition-colors mb-2 hover:bg-accent/70 ${parentIssueOpen ? "" : "text-muted-foreground hover:text-foreground"}`}
            onClick={() => setParentIssueOpen(!parentIssueOpen)}
          >
            {t(($) => $.detail.section_parent_issue)}
            <ChevronRight className={`!size-3 shrink-0 stroke-[2.5] text-muted-foreground transition-transform ${parentIssueOpen ? "rotate-90" : ""}`} />
          </button>
          {parentIssueOpen && <div className="pl-2">
            <AppLink
              href={paths.issueDetail(parentIssue.id)}
              className="flex items-center gap-1.5 rounded-md px-2 py-1.5 -mx-2 text-xs hover:bg-accent/50 transition-colors group"
            >
              <StatusIcon status={parentIssue.status} className="h-3.5 w-3.5 shrink-0" />
              <span className="text-muted-foreground shrink-0">{parentIssue.identifier}</span>
              <span className="truncate group-hover:text-foreground">{parentIssue.title}</span>
            </AppLink>
          </div>}
        </div>
      )}

      {/* Details */}
      <div>
        <button
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
    </div>
  );

  const detailContent = (
    <div className="flex h-full min-w-0 flex-1 flex-col">
        <PageHeader className="gap-2 bg-background text-sm">
          <div className="flex flex-1 items-center gap-1.5 min-w-0">
            {workspace && (
              <>
                <AppLink
                  href={paths.issues()}
                  className="text-muted-foreground hover:text-foreground transition-colors shrink-0"
                >
                  {workspace.name}
                </AppLink>
                <ChevronRight className="h-3 w-3 text-muted-foreground/50 shrink-0" />
              </>
            )}
            {parentIssue && (
              <>
                <AppLink
                  href={paths.issueDetail(parentIssue.id)}
                  className="text-muted-foreground hover:text-foreground transition-colors truncate shrink-0"
                >
                  {parentIssue.identifier}
                </AppLink>
                <ChevronRight className="h-3 w-3 text-muted-foreground/50 shrink-0" />
              </>
            )}
            <span className="text-muted-foreground tabular-nums shrink-0">
              {issue.identifier}
            </span>
            <span className="truncate font-medium text-foreground">
              {issue.title}
            </span>
          </div>
          <div className="flex items-center gap-1 shrink-0">
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
          </div>
        </PageHeader>

        {/* Content — scrollable */}
        <div ref={setScrollContainerEl} className="relative flex-1 overflow-y-auto">
        <div className="mx-auto w-full max-w-4xl px-8 py-8">
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

          <div {...descDropZoneProps} className="relative mt-5 rounded-lg">
            <ContentEditor
              ref={descEditorRef}
              key={id}
              defaultValue={issue.description || ""}
              placeholder={t(($) => $.detail.desc_placeholder)}
              onUpdate={(md) => handleUpdateField({ description: md })}
              onUploadFile={handleDescriptionUpload}
              debounceMs={1500}
              currentIssueId={id}
              attachments={issueAttachments}
            />

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
                <BatchActionToolbar placement="inline" />

                {/* List */}
                {!subIssuesCollapsed && (
                  <div className="overflow-hidden rounded-lg border bg-card/30 divide-y divide-border/60">
                    {childIssues.map((child) => (
                      <SubIssueRow key={child.id} child={child} />
                    ))}
                  </div>
                )}
              </div>
            );
          })()}

          <div className="my-8 border-t" />

          {/* Activity / Comments */}
          <div>
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-3">
                <h2 className="text-base font-semibold">{t(($) => $.detail.activity_section)}</h2>
              </div>
              <div className="flex items-center gap-2">
                <button
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
                  <PopoverContent align="end" className="w-64 p-0">
                    <Command>
                      <CommandInput placeholder={t(($) => $.detail.change_subscribers_placeholder)} />
                      <CommandList className="max-h-64">
                        <CommandEmpty>{t(($) => $.detail.no_subscribers_results)}</CommandEmpty>
                        {members.length > 0 && (
                          <CommandGroup heading={t(($) => $.detail.members_group)}>
                            {members.filter((m, i, arr) => arr.findIndex((x) => x.user_id === m.user_id) === i).map((m) => {
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
                        {agents.filter((a) => !a.archived_at).length > 0 && (
                          <CommandGroup heading={t(($) => $.detail.agents_group)}>
                            {agents.filter((a) => !a.archived_at).map((a) => {
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
                </Popover>
              </div>
            </div>

            {/* Agent live output — sticky banner in the activity section,
                keyed by issue id so switching issues remounts the card and
                clears any in-flight task state from the previous issue.
                The execution log itself (per-task timeline + past runs)
                lives in the right panel via ExecutionLogSection — this
                card is just a header-style "agent is working" anchor. */}
            <AgentLiveCard key={id} issueId={id} />

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
            ) : !scrollContainerEl ? (
              // Show skeleton (not blank) while the callback ref populates,
              // so the gap between IssueDetail mount and Virtuoso mount feels
              // continuous with the loading state instead of flashing empty.
              <TimelineSkeleton />
            ) : (
              <div className="mt-4">
                <Virtuoso
                  key={`${wsId}:${id}`}
                  ref={virtuosoRef}
                  customScrollParent={scrollContainerEl}
                  data={items}
                  increaseViewportBy={{ top: 800, bottom: 800 }}
                  computeItemKey={(_i, item) => `${item.kind}:${item.id}`}
                  skipAnimationFrameInResizeObserver
                  // followOutput intentionally NOT set. Virtuoso treats it as
                  // a sticky "is at bottom" flag and resets scrollTop to
                  // maxScrollTop on every ResizeObserver / height-change tick
                  // — this is what was yanking the user back to scrollTop=299
                  // whenever they tried to scroll up after a deep-link
                  // landed on the last item. Issue-detail is document-shaped
                  // (not a chat), so auto-follow on new comments is not
                  // critical; users can scroll to bottom themselves.
                  // Intentionally NOT passing `initialTopMostItemIndex`.
                  // In customScrollParent mode Virtuoso treats this prop as
                  // a persistent anchor and resets scrollTop whenever the
                  // list height changes (ResizeObserver firing on real-card
                  // measurement, etc.) — which fights against the user when
                  // they scroll up. Deep-link landing is handled imperatively
                  // by the useEffect above (scrollToIndex + scrollIntoView).
                  itemContent={(_i, item) => {
                    if (item.kind === "resolved-bar") {
                      return (
                        // data-comment-id is the anchor for inbox deep-link;
                        // see the deep-link useEffect for how scrollIntoView
                        // finds it after Virtuoso mounts the item.
                        <div className="pb-3" data-comment-id={item.id}>
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
                        <div className="pb-3" data-comment-id={item.id}>
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
                            highlightedCommentId={highlightedId}
                          />
                        </div>
                      );
                    }
                    // activity-group
                    return (
                      <div className="pb-3 px-4 flex flex-col gap-3">
                        {item.entries.map((entry) => {
                          const details = (entry.details ?? {}) as Record<string, string>;
                          const isStatusChange = entry.action === "status_changed";
                          const isPriorityChange = entry.action === "priority_changed";
                          const isDueDateChange = entry.action === "due_date_changed";

                          let leadIcon: React.ReactNode;
                          if (isStatusChange && details.to) {
                            leadIcon = <StatusIcon status={details.to as IssueStatus} className="h-4 w-4 shrink-0" />;
                          } else if (isPriorityChange && details.to) {
                            leadIcon = <PriorityIcon priority={details.to as IssuePriority} className="h-4 w-4 shrink-0" />;
                          } else if (isDueDateChange) {
                            leadIcon = <Calendar className="h-4 w-4 shrink-0 text-muted-foreground" />;
                          } else {
                            leadIcon = <ActorAvatar actorType={entry.actor_type} actorId={entry.actor_id} size={16} />;
                          }

                          return (
                            <div key={entry.id} className="flex items-center text-xs text-muted-foreground">
                              <div className="mr-2 flex w-4 shrink-0 justify-center">
                                {leadIcon}
                              </div>
                              <div className="flex min-w-0 flex-1 items-center gap-1">
                                <span className="shrink-0 font-medium">{getActorName(entry.actor_type, entry.actor_id)}</span>
                                <span className="truncate">{formatActivity(entry, t, getActorName)}</span>
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
                  }}
                />
              </div>
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
        defaultSize={defaultSidebarOpen ? 320 : 0}
        minSize={260}
        maxSize={420}
        collapsible
        groupResizeBehavior="preserve-pixel-size"
        panelRef={sidebarRef}
        onResize={(size) => setDesktopSidebarOpen(size.inPixels > 0)}
      >
      <div className="overflow-y-auto border-l h-full">
        <div className="p-4">
          {sidebarContent}
        </div>
      </div>
      </ResizablePanel>
    </ResizablePanelGroup>
  );
}
