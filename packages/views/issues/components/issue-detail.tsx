"use client";

import { useState, useEffect, useCallback, useMemo, useRef, Fragment } from "react";
import { Virtuoso } from "react-virtuoso";
import { useDefaultLayout, usePanelRef } from "react-resizable-panels";
import { AppLink } from "../../navigation";
import { useNavigation } from "../../navigation";
import {
  Archive,
  ArrowDownToLine,
  ArrowUpToLine,
  Calendar,
  CalendarClock,
  CalendarDays,
  ChevronDown,
  ChevronLeft,
  ChevronRight,
  CircleCheck,
  Eraser,
  ExternalLink,
  Link2,
  MoreHorizontal,
  PanelRight,
  Pin,
  PinOff,
  Plus,
  MessageSquare,
  MessageSquareReply,
  Square,
  Terminal,
  Tag,
  Trash2,
  Users,
} from "lucide-react";
import { BreadcrumbHeader, type BreadcrumbSegment } from "../../layout/breadcrumb-header";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Button } from "@multica/ui/components/ui/button";
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
import { ResizablePanelGroup, ResizablePanel, ResizableHandle } from "@multica/ui/components/ui/resizable";
import { Sheet, SheetContent } from "@multica/ui/components/ui/sheet";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@multica/ui/components/ui/tabs";
import { useIsMobile } from "@multica/ui/hooks/use-mobile";
import { ContentEditor, type ContentEditorRef, TitleEditor, useFileDropZone, FileDropOverlay, AttachmentCard, AttachmentDownloadProvider, useAttachmentPreview, useDownloadAttachment } from "../../editor";
import { FileUploadButton } from "@multica/ui/components/common/file-upload-button";
import {
  Tooltip,
  TooltipTrigger,
  TooltipContent,
} from "@multica/ui/components/ui/tooltip";
import { Popover, PopoverTrigger, PopoverContent } from "@multica/ui/components/ui/popover";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@multica/ui/components/ui/dialog";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { Command, CommandInput, CommandList, CommandEmpty, CommandGroup, CommandItem } from "@multica/ui/components/ui/command";
import { AvatarGroup, AvatarGroupCount } from "@multica/ui/components/ui/avatar";
import { ActorAvatar } from "../../common/actor-avatar";
import { PropRow } from "../../common/prop-row";
import type { Attachment, Issue, IssueStatus, IssuePriority, LocalPreview, TimelineEntry, UpdateIssueRequest } from "@multica/core/types";
import { STATUS_CONFIG, PRIORITY_CONFIG } from "@multica/core/issues/config";
import { useUpdateIssue } from "@multica/core/issues/mutations";
import { toast } from "sonner";
import { StatusIcon, PriorityIcon, StatusPicker, PriorityPicker, StartDatePicker, DueDatePicker, AssigneePicker, LabelPicker } from ".";
import { IssueActionsDropdown, useIssueActions } from "../actions";
import { ProjectPicker } from "../../projects/components/project-picker";
import { LocalDirectoryHint } from "../../projects/components/local-directory-hint";
import { CommentCard } from "./comment-card";
import { CommentInput, type CommentInputRef } from "./comment-input";
import type { ReplyInputRef } from "./reply-input";
import { AgentStreamSidebar } from "./agent-stream-sidebar";
import { ResolvedThreadBar } from "./resolved-thread-bar";
import { collectThreadReplies } from "./thread-utils";
import { AgentLiveCard } from "./agent-live-card";
import { ExecutionLogSection } from "./execution-log-section";
import { PullRequestList } from "./pull-request-list";
import { useGitHubSettings } from "@multica/core/github";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspacePaths } from "@multica/core/paths";
import { useActorName } from "@multica/core/workspace/hooks";
import { useWorkspaceId } from "@multica/core/hooks";
import { issueListOptions, issueDetailOptions, childIssuesOptions, issueUsageOptions, issueAttachmentsOptions } from "@multica/core/issues/queries";
import { useClearIssueHistory } from "@multica/core/issues/mutations";
import { projectDetailOptions } from "@multica/core/projects/queries";
import { ProjectIcon } from "../../projects/components/project-icon";
import { issueLabelsOptions } from "@multica/core/labels";
import { memberListOptions, agentListOptions } from "@multica/core/workspace/queries";
import { useRecentIssuesStore } from "@multica/core/issues/stores";
import { useActiveIssueContextStore } from "@multica/core/issues/stores/active-issue-context-store";
import { useCommentCollapseStore } from "@multica/core/issues/stores";
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

type PreviewWithPort = LocalPreview & { healthPort: number };

function SubscriberPopoverContent({
  members,
  agents,
  subscribers,
  toggleSubscriber,
  t,
}: {
  members: { user_id: string; name: string }[];
  agents: { id: string; name: string; archived_at?: string | null }[];
  subscribers: { user_type: string; user_id: string; created_at?: string }[];
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

  // Sort: subscribed first (by created_at desc), then unsubscribed
  const sortedMembers = [...filteredMembers].sort((a, b) => {
    const subA = subscribers.find((s) => s.user_type === "member" && s.user_id === a.user_id);
    const subB = subscribers.find((s) => s.user_type === "member" && s.user_id === b.user_id);
    if (subA && !subB) return -1;
    if (!subA && subB) return 1;
    if (subA && subB) {
      return (subB.created_at ?? "").localeCompare(subA.created_at ?? "");
    }
    return 0;
  });
  const sortedAgents = [...filteredAgents].sort((a, b) => {
    const subA = subscribers.find((s) => s.user_type === "agent" && s.user_id === a.id);
    const subB = subscribers.find((s) => s.user_type === "agent" && s.user_id === b.id);
    if (subA && !subB) return -1;
    if (!subA && subB) return 1;
    if (subA && subB) {
      return (subB.created_at ?? "").localeCompare(subA.created_at ?? "");
    }
    return 0;
  });

  return (
    <PopoverContent align="end" className="w-64 p-0">
      <Command shouldFilter={false}>
        <CommandInput
          placeholder={t(($) => $.detail.change_subscribers_placeholder)}
          value={search}
          onValueChange={setSearch}
        />
        <CommandList className="max-h-64">
          {sortedMembers.length === 0 && sortedAgents.length === 0 && (
            <CommandEmpty>{t(($) => $.detail.no_subscribers_results)}</CommandEmpty>
          )}
          {sortedMembers.length > 0 && (
            <CommandGroup heading={t(($) => $.detail.members_group)}>
              {sortedMembers.map((m) => {
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
          {sortedAgents.length > 0 && (
            <CommandGroup heading={t(($) => $.detail.agents_group)}>
              {sortedAgents.map((a) => {
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

function metadataHealthPort(metadata: Record<string, unknown> | undefined): number | null {
  const value = metadata?.health_port;
  if (typeof value === "number" && Number.isFinite(value) && value > 0) return value;
  if (typeof value === "string") {
    const parsed = Number.parseInt(value, 10);
    if (Number.isFinite(parsed) && parsed > 0) return parsed;
  }
  return null;
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
      const formatted = new Date(details.to).toLocaleDateString("en-US", { month: "short", day: "numeric" });
      return t(($) => $.activity.start_date_set, { date: formatted });
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
    case "referenced_by": {
      const identifier = details.source_issue_identifier ?? details.source_issue_id ?? "?";
      const title = details.source_issue_title ?? "";
      return title ? `referenced by ${identifier}: ${title}` : `referenced by ${identifier}`;
    }
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

function mergeAttachments(
  primary?: Attachment[],
  pending?: Attachment[],
): Attachment[] | undefined {
  const all = [...(primary ?? []), ...(pending ?? [])];
  if (!all.length) return primary;
  const seen = new Set<string>();
  return all.filter((attachment) => {
    if (seen.has(attachment.id)) return false;
    seen.add(attachment.id);
    return true;
  });
}

// Stable reference for threads with no replies. Inline `[]` would create a
// new array on every render and bust React.memo on CommentCard / ResolvedThreadBar.
const EMPTY_REPLIES: TimelineEntry[] = [];

// ---------------------------------------------------------------------------
// Issue-level attachment list. These are explicit issue attachments, so keep
// every record visible even when the description also references the same URL.
// This preserves the dedicated attachment area below the issue description.
// ---------------------------------------------------------------------------

function IssueAttachmentList({
  attachments,
  pendingAttachments,
  className,
  onDelete,
  onAppendToDesc,
}: {
  attachments?: Attachment[];
  pendingAttachments?: Attachment[];
  className?: string;
  onDelete?: (id: string) => void;
  onAppendToDesc?: (a: Attachment) => void;
}) {
  const visible = mergeAttachments(attachments, pendingAttachments);
  if (!visible?.length) return null;
  const persistedIds = new Set((attachments ?? []).map((attachment) => attachment.id));

  return (
    <AttachmentDownloadProvider attachments={visible}>
      <div className={cn("flex flex-col gap-1", className)}>
        {visible.map((a) => (
          <IssueAttachmentRow
            key={a.id}
            attachment={a}
            pending={!persistedIds.has(a.id)}
            onDelete={onDelete}
            onAppendToDesc={onAppendToDesc}
          />
        ))}
      </div>
    </AttachmentDownloadProvider>
  );
}

function IssueAttachmentRow({
  attachment,
  pending: _pending,
  onDelete,
  onAppendToDesc,
}: {
  attachment: Attachment;
  pending: boolean;
  onDelete?: (id: string) => void;
  onAppendToDesc?: (a: Attachment) => void;
}) {
  const preview = useAttachmentPreview();
  const download = useDownloadAttachment();
  const handlePreview = () => {
    preview.tryOpen({ kind: "full", attachment });
  };
  const handleDownload = () => {
    void download(attachment.id);
  };

  return (
    <div className="flex items-center gap-1 group" onMouseDown={(e) => {
      // Prevent clicks in the attachment row from shifting focus to the
      // editor above. Without this, after the dropdown closes the browser
      // refocuses the editor and ProseMirror may process a stale delete
      // event that removes the last line of content instead of the
      // attachment card.
      if ((e.target as HTMLElement).closest('[role="menu"]') || (e.target as HTMLElement).closest('button')) {
        e.preventDefault();
      }
    }}>
      <div className="flex-1 min-w-0">
        <AttachmentCard
          filename={attachment.filename}
          contentType={attachment.content_type ?? undefined}
          attachmentId={attachment.id}
          href={attachment.url}
          className="my-0"
          onPreview={handlePreview}
          onDownload={handleDownload}
        />
      </div>
      {preview.modal}
      {(onDelete || onAppendToDesc) && (
        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <button
                type="button"
                className="opacity-0 group-hover:opacity-100 p-1 rounded hover:bg-accent text-muted-foreground hover:text-foreground transition-opacity"
                onMouseDown={(e) => e.preventDefault()}
              >
                <MoreHorizontal className="h-3.5 w-3.5" />
              </button>
            }
          />
          <DropdownMenuContent align="end">
            {onAppendToDesc && (
              <DropdownMenuItem onClick={() => onAppendToDesc(attachment)}>
                引用到描述末尾
              </DropdownMenuItem>
            )}
            {onDelete && onAppendToDesc && <DropdownMenuSeparator />}
            {onDelete && (
              <DropdownMenuItem
                className="text-destructive focus:text-destructive"
                onClick={() => onDelete(attachment.id)}
              >
                <Trash2 className="h-3.5 w-3.5 mr-2" />
                删除
              </DropdownMenuItem>
            )}
          </DropdownMenuContent>
        </DropdownMenu>
      )}
    </div>
  );
}

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
const OPTIONAL_PROP_KEYS = ["priority", "start_date", "due_date", "labels"] as const;
type OptionalPropKey = (typeof OPTIONAL_PROP_KEYS)[number];

function isOptionalPropSet(
  issue: Issue,
  key: OptionalPropKey,
  attachedLabelsCount: number,
): boolean {
  switch (key) {
    case "priority":
      return issue.priority !== "none";
    case "start_date":
      return !!issue.start_date;
    case "due_date":
      return !!issue.due_date;
    case "labels":
      return attachedLabelsCount > 0;
  }
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

const MAX_SELECTED_QUOTE_CHARS = 4000;
const SELECTION_BLOCKED_SELECTOR = [
  "input",
  "textarea",
  "button",
  "a",
  "[contenteditable='true']",
  ".ProseMirror",
  "[role='button']",
  "[data-node-view-wrapper]",
].join(",");

type QuoteSelectionMenuState = {
  text: string;
  x: number;
  y: number;
  threadRootId: string | null;
};

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

function formatSelectedTextAsQuote(text: string): { markdown: string; truncated: boolean } {
  const normalized = text.replace(/\r\n?/g, "\n").trim();
  const truncated = normalized.length > MAX_SELECTED_QUOTE_CHARS;
  const selected = truncated
    ? normalized.slice(0, MAX_SELECTED_QUOTE_CHARS).trimEnd()
    : normalized;
  const quoted = selected
    .split("\n")
    .map((line) => (line.trim().length > 0 ? `> ${line}` : ">"))
    .join("\n");
  return { markdown: `${quoted}\n\n`, truncated };
}

function elementFromSelectionNode(node: Node | null): Element | null {
  if (!node) return null;
  return node.nodeType === Node.ELEMENT_NODE
    ? (node as Element)
    : node.parentElement;
}

function isBlockedSelectionTarget(node: Node | null): boolean {
  return !!elementFromSelectionNode(node)?.closest(SELECTION_BLOCKED_SELECTOR);
}

function closestThreadRootId(node: Node | null): string | null {
  const el = elementFromSelectionNode(node);
  return el?.closest("[data-thread-root-id]")?.getAttribute("data-thread-root-id") ?? null;
}

function inferThreadRootId(selection: Selection): string | null {
  const anchorRoot = closestThreadRootId(selection.anchorNode);
  const focusRoot = closestThreadRootId(selection.focusNode);
  if (anchorRoot && focusRoot && anchorRoot === focusRoot) return anchorRoot;
  if (anchorRoot && !focusRoot) return anchorRoot;
  if (focusRoot && !anchorRoot) return focusRoot;
  return null;
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
  const paths = useWorkspacePaths();
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
    hiddenOlderCount > 0 ? entries.slice(-LAST_ACTIVITY_BLOCK_VISIBLE_LIMIT) : entries;
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
        const isReferencedBy = entry.action === "referenced_by";

        let leadIcon: React.ReactNode;
        if (isStatusChange && details.to) {
          leadIcon = <StatusIcon status={details.to as IssueStatus} className="h-4 w-4 shrink-0" />;
        } else if (isPriorityChange && details.to) {
          leadIcon = <PriorityIcon priority={details.to as IssuePriority} className="h-4 w-4 shrink-0" />;
        } else if (isStartDateChange) {
          leadIcon = <CalendarClock className="h-4 w-4 shrink-0 text-muted-foreground" />;
        } else if (isDueDateChange) {
          leadIcon = <Calendar className="h-4 w-4 shrink-0 text-muted-foreground" />;
        } else if (isReferencedBy) {
          leadIcon = <Link2 className="h-4 w-4 shrink-0 text-muted-foreground" />;
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
              {isReferencedBy ? (
                <span className="truncate">
                  {"referenced by "}
                  <AppLink
                    href={paths.issueDetail(details.source_issue_id ?? "")}
                    className="font-medium text-foreground hover:underline"
                  >
                    {details.source_issue_identifier ?? details.source_issue_id ?? "?"}
                  </AppLink>
                  {details.source_issue_title ? `: ${details.source_issue_title}` : ""}
                </span>
              ) : (
                <span className="truncate">{formatActivity(entry, t, getActorName)}</span>
              )}
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
        href={paths.issueDetail(child.identifier)}
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
  /** Pixel cap for the desktop right sidebar when resizing. */
  sidebarMaxSize?: number;
  /** When set, the issue detail will auto-scroll to this comment and briefly highlight it. */
  highlightCommentId?: string;
}

// ---------------------------------------------------------------------------
// IssueDetail
// ---------------------------------------------------------------------------

export function IssueDetail({
  issueId,
  onDelete,
  onDone,
  defaultSidebarOpen = true,
  layoutId = "multica_issue_detail_layout",
  sidebarMaxSize = 560,
  highlightCommentId,
}: IssueDetailProps) {
  const { t, i18n } = useT("issues");
  const timeAgo = useTimeAgo();
  // `issueId` is the raw route param — may be a UUID *or* a human-readable
  // identifier (e.g. "OPE-460") when the URL has been canonicalized.  We keep
  // it around only for the initial detail query (the API accepts both formats)
  // and for URL-related operations.  `resolvedId` (derived below after the
  // issue loads) is the canonical UUID and must be used for everything that
  // touches WebSocket event payloads, React Query cache keys, or mutations.
  const id = issueId;
  const router = useNavigation();
  const linkedCommentId = router.searchParams.get("comment")?.trim() || null;
  const requestedCommentId = (highlightCommentId ?? linkedCommentId) ?? undefined;
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
  const { uploadWithToast } = useFileUpload(api, (err) => toast.error(err.message));
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
  const [previewOpen, setPreviewOpen] = useState(true);
  const [detailsOpen, setDetailsOpen] = useState(true);
  const [parentIssueOpen, setParentIssueOpen] = useState(true);
  const [pullRequestsOpen, setPullRequestsOpen] = useState(true);
  const [metadataOpen, setMetadataOpen] = useState(false);
  const [tokenUsageOpen, setTokenUsageOpen] = useState(true);
  const [previewLogs, setPreviewLogs] = useState<{ title: string; logs: string } | null>(null);
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
  const detailRootRef = useRef<HTMLDivElement>(null);
  const commentInputRef = useRef<CommentInputRef>(null);
  const replyControllersRef = useRef<Map<string, ReplyInputRef>>(new Map());
  const [quoteMenu, setQuoteMenu] = useState<QuoteSelectionMenuState | null>(null);
  const [quoteChooserOpen, setQuoteChooserOpen] = useState(false);
  const closeQuoteMenu = useCallback(() => {
    setQuoteMenu(null);
    setQuoteChooserOpen(false);
  }, []);
  const registerReplyController = useCallback((threadRootId: string, controller: ReplyInputRef | null) => {
    if (controller) {
      replyControllersRef.current.set(threadRootId, controller);
    } else {
      replyControllersRef.current.delete(threadRootId);
    }
  }, []);
  const scrollToTop = useCallback(() => {
    scrollContainerEl?.scrollTo({ top: 0, behavior: "smooth" });
  }, [scrollContainerEl]);
  const scrollToBottom = useCallback(() => {
    if (!scrollContainerEl) return;
    scrollContainerEl.scrollTo({ top: scrollContainerEl.scrollHeight, behavior: "smooth" });
  }, [scrollContainerEl]);
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

  // Navigate by the canonical trigger_comment_id, then let the deep-link
  // timeline path fetch the exact target if it is not currently mounted.
  const handleHighlightComment = useCallback((commentId: string) => {
    // Update URL without triggering a router navigation/data fetch
    const url = paths.issueDetail(id, { commentId });
    window.history.replaceState(window.history.state, "", url);
    const el = document.getElementById(`comment-${commentId}`);
    if (el) {
      el.scrollIntoView({ behavior: "smooth", block: "center" });
      setHighlightedId(commentId);
      setTimeout(() => setHighlightedId(null), 2000);
    }
  }, [id, paths]);

  const [clearHistoryDialogOpen, setClearHistoryDialogOpen] = useState(false);
  const clearHistoryMutation = useClearIssueHistory();

  // Issue data from TQ — uses detail query, seeded from list cache if available.
  // Only seed when description is present; list API omits it, and ContentEditor
  // reads defaultValue on mount only — seeding null description shows an empty editor.
  const { data: issue = null, isLoading: issueLoading } = useQuery({
    ...issueDetailOptions(wsId, id),
    initialData: () => {
      const cached = allIssues.find((i) => i.id === id || i.identifier === id);
      return cached?.description != null ? cached : undefined;
    },
  });
  const setActiveIssueContext = useActiveIssueContextStore((s) => s.setCurrent);
  const clearActiveIssueContext = useActiveIssueContextStore((s) => s.clearCurrent);
  useEffect(() => {
    if (!issue) return;
    setActiveIssueContext({
      issueId: issue.id,
      identifier: issue.identifier,
      projectId: issue.project_id,
    });
    return () => clearActiveIssueContext(issue.id);
  }, [
    issue?.id,
    issue?.identifier,
    issue?.project_id,
    setActiveIssueContext,
    clearActiveIssueContext,
  ]);
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

  // Custom hooks — encapsulate timeline, reactions, subscribers.
  //
  // Resolved UUID: once the detail query loads, `issue.id` is the canonical
  // UUID returned by the server.  We use it for all WS-aware hooks and query
  // keys so that event payload comparisons (`comment.issue_id !== issueId`)
  // always compare UUIDs.  Before the query resolves we fall back to the raw
  // route param — the API accepts both formats, so the initial fetch works;
  // WS events arriving before the issue loads are an acceptable edge case
  // (the refetch-on-mount will pick them up).
  const resolvedId = issue?.id ?? id;
  const { data: localRuntimes = [] } = useQuery({
    queryKey: ["local-preview-runtimes", wsId],
    queryFn: () => api.listRuntimes({ workspace_id: wsId, owner: "me" }),
    enabled: !!wsId,
    staleTime: 30_000,
  });
  const localPreviewPorts = useMemo(() => {
    const ports = new Set<number>();
    for (const runtime of localRuntimes) {
      if (runtime.runtime_mode !== "local" || runtime.status !== "online") continue;
      const port = metadataHealthPort(runtime.metadata);
      if (port) ports.add(port);
    }
    return Array.from(ports);
  }, [localRuntimes]);
  const [localPreviewsByPort, setLocalPreviewsByPort] = useState<Record<number, PreviewWithPort[]>>({});
  const { refetch: refetchLocalPreviews } = useQuery({
    queryKey: ["local-previews", wsId, resolvedId, localPreviewPorts.join(",")],
    queryFn: async (): Promise<PreviewWithPort[]> => {
      const results = await Promise.allSettled(
        localPreviewPorts.map(async (healthPort) => {
          const response = await api.listLocalPreviews(healthPort, { workspace_id: wsId, issue_id: resolvedId });
          return response.previews.map((preview) => ({ ...preview, healthPort }));
        }),
      );
      const nextByPort: Record<number, PreviewWithPort[]> = {};
      for (const result of results) {
        if (result.status !== "fulfilled") continue;
        for (const preview of result.value) {
          nextByPort[preview.healthPort] = [...(nextByPort[preview.healthPort] ?? []), preview];
        }
      }
      setLocalPreviewsByPort(nextByPort);
      return results.flatMap((result) => result.status === "fulfilled" ? result.value : []);
    },
    enabled: !!resolvedId && localPreviewPorts.length > 0,
  });
  useEffect(() => {
    if (!resolvedId || localPreviewPorts.length === 0) {
      setLocalPreviewsByPort({});
      return;
    }

    const portSet = new Set(localPreviewPorts);
    setLocalPreviewsByPort((prev) => {
      const next = Object.fromEntries(
        Object.entries(prev).filter(([port]) => portSet.has(Number(port))),
      ) as Record<number, PreviewWithPort[]>;
      return Object.keys(next).length === Object.keys(prev).length ? prev : next;
    });

    const sources = localPreviewPorts.map((healthPort) => {
      const source = new EventSource(api.getLocalPreviewStreamUrl(healthPort, { workspace_id: wsId, issue_id: resolvedId }));
      let fallbackTimer: number | undefined;
      const handleSnapshot = (event: Event) => {
        try {
          const payload = JSON.parse((event as MessageEvent).data) as { previews?: LocalPreview[] };
          const previews = Array.isArray(payload.previews) ? payload.previews : [];
          setLocalPreviewsByPort((prev) => ({
            ...prev,
            [healthPort]: previews.map((preview) => ({ ...preview, healthPort })),
          }));
        } catch {
          // Ignore malformed local daemon events; the initial fetch remains as fallback.
        }
      };
      const handleError = () => {
        source.close();
        void refetchLocalPreviews();
        if (!fallbackTimer) {
          fallbackTimer = window.setInterval(() => {
            void refetchLocalPreviews();
          }, 15_000);
        }
      };

      source.addEventListener("ready", handleSnapshot);
      source.addEventListener("snapshot", handleSnapshot);
      source.addEventListener("error", handleError);
      return { source, fallbackTimer: () => fallbackTimer };
    });

    return () => {
      for (const { source, fallbackTimer } of sources) {
        source.close();
        const timer = fallbackTimer();
        if (timer) {
          window.clearInterval(timer);
        }
      }
    };
  }, [localPreviewPorts, refetchLocalPreviews, resolvedId, wsId]);
  const localPreviews = useMemo(
    () => Object.values(localPreviewsByPort).flat(),
    [localPreviewsByPort],
  );
  const issueLocalPreviews = useMemo(() => (
    localPreviews.filter((preview) => (
      preview.workspace_id === wsId &&
      preview.issue_id === resolvedId &&
      preview.visibility === "private" &&
      preview.status !== "stopped"
    ))
  ), [localPreviews, resolvedId, wsId]);
  const handleStopLocalPreview = useCallback(async (preview: PreviewWithPort) => {
    await api.stopLocalPreview(preview.healthPort, preview.id);
    await refetchLocalPreviews();
  }, [refetchLocalPreviews]);
  const handleShowLocalPreviewLogs = useCallback(async (preview: PreviewWithPort) => {
    const logs = await api.getLocalPreviewLogs(preview.healthPort, preview.id);
    setPreviewLogs({
      title: logs.log_path,
      logs: logs.logs || "No preview logs yet.",
    });
  }, []);
  const {
    timeline, loading: timelineLoading,
    submitComment, submitReply,
    editComment, deleteComment, toggleResolveComment, toggleReaction: handleToggleReaction,
  } = useIssueTimeline(resolvedId, user?.id, requestedCommentId);

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
    const commentById = new Map<string, TimelineEntry>();
    for (const e of timeline) {
      if (e.type === "comment") {
        commentById.set(e.id, e);
        if (e.parent_id) {
          const list = repliesByParent.get(e.parent_id) ?? [];
          list.push(e);
          repliesByParent.set(e.parent_id, list);
        }
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
    // - squad_leader_evaluated: never coalesce; outcome/reason are audit data
    const COALESCE_MS = 2 * 60 * 1000;
    const NO_TIME_LIMIT_ACTIONS = new Set(["task_completed", "task_failed"]);
    const NEVER_COALESCE_ACTIONS = new Set(["squad_leader_evaluated"]);
    const coalesced: TimelineEntry[] = [];
    for (const entry of topLevel) {
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

    return { threadReplies, commentById, groups };
  }, [timeline]);

  // Flat array consumed by <Virtuoso>. Recomputed when timelineView.groups
  // changes (timeline events) or expandedResolved flips (user toggles a
  // resolved thread).
  const items = useMemo<TimelineItem[]>(
    () => flattenGroups(timelineView.groups, expandedResolved),
    [timelineView.groups, expandedResolved],
  );

  // ID of the trailing activity block — the only one expanded by default.
  const lastActivityGroupId = useMemo(() => {
    for (let i = timelineView.groups.length - 1; i >= 0; i--) {
      const g = timelineView.groups[i]!;
      if (g.type === "activities") return g.entries[0]!.id;
    }
    return null;
  }, [timelineView.groups]);

  // Map of reply-comment id → root-comment id, so a deep-link to a reply
  // can fall back to scrolling the root thread into view.
  const replyToRoot = useMemo(() => {
    const map = new Map<string, string>();
    for (const [rootId, replies] of timelineView.threadReplies) {
      for (const reply of replies) {
        map.set(reply.id, rootId);
      }
    }
    return map;
  }, [timelineView.threadReplies]);

  // Deep-link target index in the flat items array.
  const targetIdx = useMemo(() => {
    if (!requestedCommentId) return -1;
    const direct = items.findIndex((it) => it.id === requestedCommentId);
    if (direct >= 0) return direct;
    const rootId = replyToRoot.get(requestedCommentId);
    if (!rootId) return -1;
    return items.findIndex((it) => it.id === rootId);
  }, [items, requestedCommentId, replyToRoot]);

  const {
    reactions: issueReactions,
    toggleReaction: handleToggleIssueReaction,
  } = useIssueReactions(resolvedId, user?.id);

  const {
    subscribers, isSubscribed, toggleSubscribe: handleToggleSubscribe, toggleSubscriber,
  } = useIssueSubscribers(resolvedId, user?.id);

  const topLevelComments = useMemo(
    () => timeline.filter((entry) => entry.type === "comment" && !entry.parent_id),
    [timeline],
  );
  const selectionQuoteReplyTargets = useMemo(
    () => topLevelComments.map((comment) => ({
      id: comment.id,
      label: getActorName(comment.actor_type, comment.actor_id),
      meta: new Date(comment.created_at).toLocaleString(i18n.language),
    })),
    [getActorName, i18n.language, topLevelComments],
  );

  // Token usage
  const { data: usage } = useQuery(issueUsageOptions(resolvedId));

  const queryClient = useQueryClient();

  // Attachments uploaded against this issue. Drives the description
  // editor's click-time fresh-sign download. Must use `resolvedId` so the
  // cache key matches what `useUpdateIssue.onSettled` invalidates (UUID).
  const { data: issueAttachments } = useQuery(issueAttachmentsOptions(resolvedId));

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
    ...childIssuesOptions(wsId, resolvedId),
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
  // issue detail is mounted or switched.
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
  // When `requestedCommentId` is set the timeline below renders flat (no
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
    if (!requestedCommentId || items.length === 0) return;
    if (didHighlightRef.current === requestedCommentId) return;

    const rootId = replyToRoot.get(requestedCommentId);
    if (
      rootId &&
      rootId !== requestedCommentId &&
      items[targetIdx]?.kind === "resolved-bar"
    ) {
      toggleResolvedExpand(rootId, true);
      return;
    }

    const el = document.getElementById(`comment-${requestedCommentId}`);
    if (!el) return;

    didHighlightRef.current = requestedCommentId;
    el.scrollIntoView({ block: "center" });

    setHighlightedId(requestedCommentId);
    const fade = window.setTimeout(() => setHighlightedId(null), 2500);
    return () => clearTimeout(fade);
  }, [requestedCommentId, items, targetIdx, scrollContainerEl, replyToRoot, toggleResolvedExpand]);

  // Cmd-F / Ctrl-F on a virtualized timeline only searches what's mounted in
  // the viewport — off-screen comments are invisible to browser find-in-page.
  // Intercept once per (session, issue) when the list is long enough that the
  // user might actually try; let the keystroke pass through on short lists.
  // Real fix is in-app search (separate PR); this is the toast stopgap.
  useEffect(() => {
    if (!issue) return;
    const match = router.pathname.match(/\/issues\/([^/?#]+)/);
    if (!match) return;
    const currentPathId = decodeURIComponent(match[1] ?? "");
    if (currentPathId !== issueId || issueId === issue.identifier) return;

    const canonicalPath = paths.issueDetail(issue.identifier);
    const query = router.searchParams.toString();
    router.replace(query ? `${canonicalPath}?${query}` : canonicalPath);
  }, [issue, issueId, paths, router]);

  // Shared issue actions (mutations, pin, copy-link, modal dispatch, etc.).
  // Called before the `if (!issue)` early return so hook order stays stable.
  const actions = useIssueActions(issue);
  const handleUpdateField = actions.updateField;

  const descEditorRef = useRef<ContentEditorRef>(null);
  // Description conflict detection baseline (OPE-2294). Stored in refs so the
  // onUpdate callback always sees the freshest values without recapturing.
  const descBaseUpdatedAtRef = useRef<string>("");
  const descBaseValueRef = useRef<string>("");
  const handleDescriptionExternalSyncAccepted = useCallback(() => {
    descBaseUpdatedAtRef.current = issue?.updated_at ?? "";
    descBaseValueRef.current = issue?.description ?? "";
  }, [issue?.updated_at, issue?.description]);
  const { isDragOver: descDragOver, dropZoneProps: descDropZoneProps } = useFileDropZone({
    onDrop: (files) => descEditorRef.current?.uploadFiles(files),
  });
  // Pending uploads in the description editor. We bind them immediately after
  // upload so IssueAttachmentList can show the compact attachment index while
  // the description keeps rendering attachments inline at their markdown
  // positions.
  const [descPendingAttachments, setDescPendingAttachments] = useState<Attachment[]>([]);
  useEffect(() => {
    setDescPendingAttachments([]);
  }, [resolvedId]);
  const descEditorAttachments = descPendingAttachments.length > 0
    ? [...(issueAttachments ?? []), ...descPendingAttachments]
    : issueAttachments;
  const handleDescriptionUpload = useCallback(
    async (file: File) => {
      // Pass issueId in upload context so the attachment record is created
      // with issue_id pre-set in the DB. This ensures the attachment survives
      // page refresh even if the separate link call races or the user removes
      // the node from the body before the debounced save fires.
      const result = await uploadWithToast(file, { issueId: resolvedId });
      if (result) {
        setDescPendingAttachments((prev) => [...prev, result]);
      }
      return result;
    },
    [resolvedId, uploadWithToast],
  );

  const insertQuoteToNewComment = useCallback((markdown: string) => {
    closeQuoteMenu();
    scrollToBottom();
    window.setTimeout(() => {
      commentInputRef.current?.appendMarkdown(markdown);
    }, 120);
  }, [closeQuoteMenu, scrollToBottom]);

  const insertQuoteToReply = useCallback((threadRootId: string, markdown: string) => {
    closeQuoteMenu();
    toggleResolvedExpand(threadRootId, true);
    const collapsed = useCommentCollapseStore.getState().isCollapsed(resolvedId, threadRootId);
    if (collapsed) {
      useCommentCollapseStore.getState().toggle(resolvedId, threadRootId);
    }

    const append = () => {
      const controller = replyControllersRef.current.get(threadRootId);
      if (!controller) return false;
      controller.appendMarkdown(markdown);
      return true;
    };

    if (append()) return;

    const target = document.getElementById(`comment-${threadRootId}`);
    target?.scrollIntoView({ behavior: "smooth", block: "center" });

    window.setTimeout(() => {
      if (append()) return;
      window.setTimeout(() => {
        if (!append()) {
          toast.error(t(($) => $.quote.reply_input_not_ready));
        }
      }, 250);
    }, 180);
  }, [closeQuoteMenu, resolvedId, t, toggleResolvedExpand]);

  const buildQuoteFromMenu = useCallback(() => {
    if (!quoteMenu) return null;
    const result = formatSelectedTextAsQuote(quoteMenu.text);
    if (result.truncated) {
      toast.message(t(($) => $.quote.truncated, { count: MAX_SELECTED_QUOTE_CHARS }));
    }
    return result.markdown;
  }, [quoteMenu, t]);

  const formatMarkdownSelectionAsQuote = useCallback((markdown: string) => {
    const result = formatSelectedTextAsQuote(markdown);
    if (result.truncated) {
      toast.message(t(($) => $.quote.truncated, { count: MAX_SELECTED_QUOTE_CHARS }));
    }
    return result.markdown;
  }, [t]);

  const handleQuoteToNewComment = useCallback(() => {
    const markdown = buildQuoteFromMenu();
    if (!markdown) return;
    insertQuoteToNewComment(markdown);
  }, [buildQuoteFromMenu, insertQuoteToNewComment]);

  const handleEditorQuoteToNewComment = useCallback((markdown: string) => {
    insertQuoteToNewComment(formatMarkdownSelectionAsQuote(markdown));
  }, [formatMarkdownSelectionAsQuote, insertQuoteToNewComment]);

  const handleEditorQuoteToReplyInThread = useCallback((threadRootId: string, markdown: string) => {
    insertQuoteToReply(threadRootId, formatMarkdownSelectionAsQuote(markdown));
  }, [formatMarkdownSelectionAsQuote, insertQuoteToReply]);

  const handleEditorQuoteToReplyTarget = useCallback((threadRootId: string, markdown: string) => {
    insertQuoteToReply(threadRootId, formatMarkdownSelectionAsQuote(markdown));
  }, [formatMarkdownSelectionAsQuote, insertQuoteToReply]);

  const handleQuoteToReply = useCallback(() => {
    const markdown = buildQuoteFromMenu();
    if (!markdown || !quoteMenu) return;
    if (quoteMenu.threadRootId) {
      insertQuoteToReply(quoteMenu.threadRootId, markdown);
      return;
    }
    setQuoteChooserOpen(true);
  }, [buildQuoteFromMenu, insertQuoteToReply, quoteMenu]);

  const handleSelectReplyTarget = useCallback((threadRootId: string) => {
    const markdown = buildQuoteFromMenu();
    if (!markdown) return;
    insertQuoteToReply(threadRootId, markdown);
  }, [buildQuoteFromMenu, insertQuoteToReply]);

  const updateQuoteMenuFromSelection = useCallback(() => {
    const selection = window.getSelection();
    if (!selection || selection.isCollapsed || !detailRootRef.current) {
      closeQuoteMenu();
      return;
    }
    if (
      isBlockedSelectionTarget(selection.anchorNode) ||
      isBlockedSelectionTarget(selection.focusNode)
    ) {
      closeQuoteMenu();
      return;
    }
    const text = selection.toString().trim();
    if (!text) {
      closeQuoteMenu();
      return;
    }
    const range = selection.rangeCount > 0 ? selection.getRangeAt(0) : null;
    if (!range || !detailRootRef.current.contains(range.commonAncestorContainer)) {
      closeQuoteMenu();
      return;
    }
    const rect = range.getBoundingClientRect();
    if (!rect.width && !rect.height) {
      closeQuoteMenu();
      return;
    }
    setQuoteMenu({
      text,
      x: Math.min(Math.max(rect.left + rect.width / 2, 16), window.innerWidth - 16),
      y: Math.max(rect.top - 8, 12),
      threadRootId: inferThreadRootId(selection),
    });
    setQuoteChooserOpen(false);
  }, [closeQuoteMenu]);

  const handleSelectionGestureEnd = useCallback(() => {
    window.setTimeout(updateQuoteMenuFromSelection, 0);
  }, [updateQuoteMenuFromSelection]);

  useEffect(() => {
    if (!quoteMenu) return;
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") closeQuoteMenu();
    };
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, [closeQuoteMenu, quoteMenu]);
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
    if (panel.isCollapsed()) panel.expand();
    else panel.collapse();
  }, [isMobile, sidebarRef]);

  const handleClearHistory = async () => {
    try {
      const result = await clearHistoryMutation.mutateAsync({
        issueId: resolvedId,
        clearComments: true,
        clearTasks: true,
      });
      toast.success(`Cleared ${result.comments_deleted} comments, ${result.tasks_deleted} task runs`);
      setClearHistoryDialogOpen(false);
    } catch {
      toast.error("Failed to clear history");
    }
  };

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

  const propertiesContent = (
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
                projectId={issue.project_id}
                align="start"
                defaultOpen={autoOpenProp === "labels"}
              />
            </PropRow>
          )}

          {/* "+ Add property" — opens a Popover listing optional fields
              not yet displayed. Hidden once every optional field is on
              screen. Sits inside the same grid as a full-row, with its
              own padding so the visual rhythm follows the rows above. */}
          {OPTIONAL_PROP_KEYS.some((k) => !visibleOptionalProps.has(k)) && (
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
                  {OPTIONAL_PROP_KEYS.filter((k) => !visibleOptionalProps.has(k)).map((k) => (
                    <button
                      key={k}
                      type="button"
                      onClick={() => addOptionalProp(k)}
                      className="flex w-full items-center gap-2 rounded-md px-2 py-1 text-xs text-foreground/90 transition-colors hover:bg-accent focus-visible:bg-accent focus-visible:outline-none"
                    >
                      {k === "priority" && (
                        <PriorityIcon priority="medium" inheritColor className="text-muted-foreground" />
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

      {/* Local Preview */}
      {issueLocalPreviews.length > 0 && (
        <div>
          <button
            className={`flex w-full items-center gap-1 rounded-md px-2 py-1 text-xs font-medium transition-colors mb-2 hover:bg-accent/70 ${previewOpen ? "" : "text-muted-foreground hover:text-foreground"}`}
            onClick={() => setPreviewOpen(!previewOpen)}
          >
            Local Preview
            <ChevronRight className={`!size-3 shrink-0 stroke-[2.5] text-muted-foreground transition-transform ${previewOpen ? "rotate-90" : ""}`} />
          </button>
          {previewOpen && (
            <div className="space-y-1 pl-2">
              {issueLocalPreviews.map((preview) => (
                <div key={preview.id} className="rounded-md border bg-muted/20 px-2 py-1.5">
                  <div className="flex min-w-0 items-center gap-2">
                    <span className={cn(
                      "h-2 w-2 shrink-0 rounded-full",
                      preview.status === "running" ? "bg-emerald-500" : preview.status === "starting" ? "bg-amber-500" : "bg-destructive",
                    )} />
                    <span className="min-w-0 flex-1 truncate text-xs text-muted-foreground">
                      {preview.status}{preview.port ? ` · :${preview.port}` : ""}
                    </span>
                    {preview.url && (
                      <Tooltip>
                        <TooltipTrigger
                          render={
                            <Button
                              variant="ghost"
                              size="icon"
                              className="h-6 w-6"
                              render={
                                <a href={preview.url} target="_blank" rel="noreferrer">
                                <ExternalLink className="h-3.5 w-3.5" />
                                </a>
                              }
                            />
                          }
                        />
                        <TooltipContent>Open preview</TooltipContent>
                      </Tooltip>
                    )}
                    <Tooltip>
                      <TooltipTrigger
                        render={
                          <Button
                            variant="ghost"
                            size="icon"
                            className="h-6 w-6"
                            onClick={() => handleShowLocalPreviewLogs(preview).catch((err) => toast.error(err instanceof Error ? err.message : "Failed to load preview logs"))}
                          >
                            <Terminal className="h-3.5 w-3.5" />
                          </Button>
                        }
                      />
                      <TooltipContent>Show logs</TooltipContent>
                    </Tooltip>
                    <Tooltip>
                      <TooltipTrigger
                        render={
                          <Button
                            variant="ghost"
                            size="icon"
                            className="h-6 w-6 text-destructive hover:text-destructive"
                            onClick={() => handleStopLocalPreview(preview).catch((err) => toast.error(err instanceof Error ? err.message : "Failed to stop preview"))}
                          >
                            <Square className="h-3.5 w-3.5" />
                          </Button>
                        }
                      />
                      <TooltipContent>Stop preview</TooltipContent>
                    </Tooltip>
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>
      )}

      {/* Parent issue */}
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
            <AppLink
              href={paths.issueDetail(parentIssue.identifier)}
              className="flex items-center gap-1.5 rounded-md px-2 py-1.5 -mx-2 text-xs hover:bg-accent/50 transition-colors group"
            >
              <StatusIcon status={parentIssue.status} className="h-3.5 w-3.5 shrink-0" />
              <span className="text-muted-foreground shrink-0">{parentIssue.identifier}</span>
              <span className="truncate group-hover:text-foreground">{parentIssue.title}</span>
            </AppLink>
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
      <ExecutionLogSection issueId={resolvedId} onHighlightComment={handleHighlightComment} />

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
            commentById={timelineView.commentById}
            agents={agents}
            issueOpen={issue.status !== "done" && issue.status !== "cancelled"}
            currentUserId={user?.id}
            canModerate={canModerateComments}
            onReply={submitReply}
            onEdit={editComment}
            onDelete={deleteComment}
            onToggleReaction={handleToggleReaction}
            onResolveToggle={handleResolveToggle}
            onCollapseResolved={isResolved ? () => toggleResolvedExpand(item.id, false) : undefined}
            highlightedCommentId={highlightedId}
            onRegisterReplyController={registerReplyController}
            selectionQuoteActions={{
              onQuoteToNewComment: handleEditorQuoteToNewComment,
              onQuoteToReplyTarget: handleEditorQuoteToReplyTarget,
              replyTargets: selectionQuoteReplyTargets,
            }}
            onQuoteToReplyInThread={handleEditorQuoteToReplyInThread}
          />
        </div>
      );
    }
    // activity-group
    const expanded = expandedActivityIds.has(item.id)
      ? true
      : collapsedActivityIds.has(item.id)
        ? false
        : item.id === lastActivityGroupId;
    const truncateOlder = item.id === lastActivityGroupId;
    const showOlder = showOlderActivityIds.has(item.id);
    return (
      <ActivityBlock
        entries={item.entries}
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

  const sidebarContent = (
    <Tabs defaultValue="runs" className="flex h-full min-h-0 flex-col">
      <TabsList className="mb-3 grid w-full grid-cols-2">
        <TabsTrigger value="runs">Runs</TabsTrigger>
        <TabsTrigger value="properties">Properties</TabsTrigger>
      </TabsList>
      <TabsContent value="runs" keepMounted className="mt-0 min-h-0 flex-1">
        <AgentStreamSidebar issueId={issue.id} onHighlightComment={handleHighlightComment} />
      </TabsContent>
      <TabsContent value="properties" keepMounted className="mt-0 min-h-0 flex-1 overflow-y-auto">
        {propertiesContent}
      </TabsContent>
    </Tabs>
  );
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
    <div
      ref={detailRootRef}
      className="flex h-full min-w-0 flex-1 flex-col"
      onMouseUp={handleSelectionGestureEnd}
      onKeyUp={handleSelectionGestureEnd}
    >
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
              trigger={
                <Button
                  variant="ghost"
                  size="icon-sm"
                  className="text-muted-foreground"
                  aria-label={t(($) => $.actions.more)}
                >
                  <MoreHorizontal />
                </Button>
              }
              onDeletedNavigateTo={onDelete ? undefined : paths.issues()}
            >
              <DropdownMenuSeparator />
              <DropdownMenuItem onClick={() => setClearHistoryDialogOpen(true)}>
                <Eraser className="h-3.5 w-3.5" />
                {t(($) => $.detail.clear_history)}
              </DropdownMenuItem>
            </IssueActionsDropdown>
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

        {/* Content — scrollable */}
        <div className="flex flex-1 min-h-0">
        <div
          ref={setScrollContainerEl}
          data-tab-scroll-root
          className="relative flex-1 overflow-y-auto"
        >
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
              href={paths.issueDetail(parentIssue.identifier)}
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
              onUpdate={(md) => {
                // Attachments are linked to the issue at upload time (via
                // issueId context), so we only save the description text here.
                // Removing an attachment node from the body does NOT unlink or
                // delete the attachment — only the attachment area delete does.
                handleUpdateField({
                  description: md,
                  description_base_updated_at: descBaseUpdatedAtRef.current || undefined,
                  description_base_value: descBaseValueRef.current ?? undefined,
                });
              }}
              onExternalSyncAccepted={handleDescriptionExternalSyncAccepted}
              onUploadFile={handleDescriptionUpload}
              debounceMs={1500}
              currentIssueId={resolvedId}
              attachments={descEditorAttachments}
              selectionQuoteActions={{
                onQuoteToNewComment: handleEditorQuoteToNewComment,
                onQuoteToReplyTarget: handleEditorQuoteToReplyTarget,
                replyTargets: selectionQuoteReplyTargets,
              }}
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
                multiple
                onSelect={(file) => descEditorRef.current?.uploadFile(file)}
                onSelectMany={(files) => descEditorRef.current?.uploadFiles(files)}
              />
            </div>
            {descDragOver && <FileDropOverlay />}
          </div>

          <IssueAttachmentList
            attachments={issueAttachments}
            pendingAttachments={descPendingAttachments}
            className="mt-3"
            onDelete={async (attachmentId) => {
              await api.deleteAttachment(attachmentId);
              setDescPendingAttachments((prev) =>
                prev.filter((attachment) => attachment.id !== attachmentId),
              );
              queryClient.invalidateQueries({ queryKey: issueAttachmentsOptions(id).queryKey });
            }}
            onAppendToDesc={(a) => {
              descEditorRef.current?.appendMarkdown(`\n\n[${a.filename}](${a.url})`);
            }}
          />

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

            {/* Agent live output — sticky banner in the activity section,
                keyed by issue id so switching issues remounts the card and
                clears any in-flight task state from the previous issue.
                The execution log itself (per-task timeline + past runs)
                lives in the right panel via ExecutionLogSection — this
                card is just a header-style "agent is working" anchor. */}
            <AgentLiveCard key={resolvedId} issueId={resolvedId} />

            {/* Timeline entries */}
            {timelineLoading && timelineView.groups.length === 0 ? (
              <TimelineSkeleton />
            ) : (
              // Two render modes:
              //   - `requestedCommentId` set (came from inbox or URL deep-link) →
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
              !requestedCommentId ? (
                !scrollContainerEl ? (
                  // Skeleton while the callback ref populates so the gap
                  // between IssueDetail mount and Virtuoso mount doesn't
                  // flash empty.
                  <TimelineSkeleton />
                ) : (
                  <div className="mt-4">
                    <Virtuoso
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
              <CommentInput
                ref={commentInputRef}
                key={resolvedId}
                issueId={resolvedId}
                onSubmit={submitComment}
                selectionQuoteActions={{
                  onQuoteToNewComment: handleEditorQuoteToNewComment,
                  onQuoteToReplyTarget: handleEditorQuoteToReplyTarget,
                  replyTargets: selectionQuoteReplyTargets,
                }}
              />
            </div>
          </div>
          </div>
        </div>
        {!isMobile && (
          <div className="pointer-events-none hidden w-12 shrink-0 justify-center md:flex">
            <div className="sticky top-[calc(50%+48px)] -translate-y-1/2 self-start">
              <div className="pointer-events-auto flex flex-col gap-1.5 rounded-lg border border-border/60 bg-background/70 p-1 shadow-xs backdrop-blur supports-[backdrop-filter]:bg-background/60">
                <Button type="button" variant="ghost" size="icon-sm" className="h-8 w-8 text-muted-foreground hover:bg-muted hover:text-foreground" onClick={scrollToTop} aria-label="Scroll to top">
                  <ArrowUpToLine className="h-4 w-4" />
                </Button>
                <Button type="button" variant="ghost" size="icon-sm" className="h-8 w-8 text-muted-foreground hover:bg-muted hover:text-foreground" onClick={scrollToBottom} aria-label="Scroll to comments">
                  <ArrowDownToLine className="h-4 w-4" />
                </Button>
              </div>
            </div>
          </div>
        )}
        {isMobile && (
          <div className="fixed bottom-20 right-3 z-40 flex flex-col gap-1.5 rounded-lg border border-border/60 bg-background/80 p-1 shadow-md backdrop-blur supports-[backdrop-filter]:bg-background/60">
            <Button type="button" variant="ghost" size="icon-sm" className="h-8 w-8 text-muted-foreground active:bg-muted active:text-foreground" onClick={scrollToTop} aria-label="Scroll to top">
              <ArrowUpToLine className="h-4 w-4" />
            </Button>
            <Button type="button" variant="ghost" size="icon-sm" className="h-8 w-8 text-muted-foreground active:bg-muted active:text-foreground" onClick={scrollToBottom} aria-label="Scroll to bottom">
              <ArrowDownToLine className="h-4 w-4" />
            </Button>
          </div>
        )}
        </div>
        {quoteMenu && (
          <div
            className="fixed z-50 min-w-44 -translate-x-1/2 -translate-y-full rounded-md border border-border bg-popover p-1 text-popover-foreground shadow-md"
            style={{ left: quoteMenu.x, top: quoteMenu.y }}
            onMouseDown={(event) => event.preventDefault()}
            onMouseUp={(event) => event.stopPropagation()}
          >
            <button
              type="button"
              className="flex w-full items-center gap-2 rounded-sm px-2.5 py-1.5 text-left text-sm hover:bg-accent hover:text-accent-foreground"
              onClick={handleQuoteToNewComment}
            >
              <MessageSquare className="h-3.5 w-3.5" />
              <span>{t(($) => $.quote.add_to_new_comment)}</span>
            </button>
            <button
              type="button"
              className="flex w-full items-center gap-2 rounded-sm px-2.5 py-1.5 text-left text-sm hover:bg-accent hover:text-accent-foreground"
              onClick={handleQuoteToReply}
            >
              <MessageSquareReply className="h-3.5 w-3.5" />
              <span>{t(($) => $.quote.add_to_reply)}</span>
            </button>
            {quoteChooserOpen && (
              <div className="mt-1 max-h-64 min-w-64 overflow-y-auto border-t border-border pt-1">
                {topLevelComments.length === 0 ? (
                  <div className="px-2.5 py-2 text-xs text-muted-foreground">
                    {t(($) => $.quote.no_comments_to_reply)}
                  </div>
                ) : (
                  topLevelComments.map((comment) => (
                    <button
                      key={comment.id}
                      type="button"
                      className="flex w-full items-center justify-between gap-4 rounded-sm px-2.5 py-1.5 text-left text-sm hover:bg-accent hover:text-accent-foreground"
                      onClick={() => handleSelectReplyTarget(comment.id)}
                    >
                      <span className="min-w-0 truncate font-medium">
                        {getActorName(comment.actor_type, comment.actor_id)}
                      </span>
                      <span className="shrink-0 text-xs text-muted-foreground">
                        {new Date(comment.created_at).toLocaleString(i18n.language)}
                      </span>
                    </button>
                  ))
                )}
              </div>
            )}
          </div>
        )}
      </div>
  );

  const clearHistoryDialog = (
    <AlertDialog open={clearHistoryDialogOpen} onOpenChange={setClearHistoryDialogOpen}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{t(($) => $.detail.clear_history_title)}</AlertDialogTitle>
          <AlertDialogDescription>
            {t(($) => $.detail.clear_history_description)}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>{t(($) => $.detail.clear_history_cancel)}</AlertDialogCancel>
          <AlertDialogAction onClick={handleClearHistory}>{t(($) => $.detail.clear_history_confirm)}</AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );

  const previewLogsDialog = (
    <AlertDialog open={!!previewLogs} onOpenChange={(open) => !open && setPreviewLogs(null)}>
      <AlertDialogContent className="max-w-3xl">
        <AlertDialogHeader>
          <AlertDialogTitle>Local Preview Logs</AlertDialogTitle>
          <AlertDialogDescription className="break-all">
            {previewLogs?.title}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <pre className="max-h-[55vh] overflow-auto rounded-md bg-muted p-3 text-xs text-muted-foreground">
          {previewLogs?.logs}
        </pre>
        <AlertDialogFooter>
          <AlertDialogAction onClick={() => setPreviewLogs(null)}>Close</AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );

  if (isMobile) {
    return (
      <>
      {clearHistoryDialog}
      {previewLogsDialog}
      <div className="flex flex-1 min-h-0">
        {detailContent}
        <Sheet open={mobileSidebarOpen} onOpenChange={setMobileSidebarOpen}>
          <SheetContent side="right" showCloseButton={false} className="w-[320px] flex flex-col overflow-hidden p-4">
            {sidebarContent}
          </SheetContent>
        </Sheet>
      </div>
      </>
    );
  }

  return (
    <>
    {clearHistoryDialog}
    {previewLogsDialog}
    <ResizablePanelGroup orientation="horizontal" className="flex-1 min-h-0" defaultLayout={defaultLayout} onLayoutChanged={onLayoutChanged}>
      <ResizablePanel id="content" minSize="50%">
        {detailContent}
      </ResizablePanel>
      <ResizableHandle />
      <ResizablePanel
        id="sidebar"
        defaultSize={defaultSidebarOpen ? 320 : 0}
        minSize={260}
        maxSize={sidebarMaxSize}
        collapsible
        groupResizeBehavior="preserve-pixel-size"
        panelRef={sidebarRef}
        onResize={(size) => setDesktopSidebarOpen(size.inPixels > 0)}
      >
      <div className="flex flex-col border-l h-full overflow-hidden">
        <div className="flex min-h-0 flex-1 flex-col p-4">
          {sidebarContent}
        </div>
      </div>
      </ResizablePanel>
    </ResizablePanelGroup>
    </>
  );
}
