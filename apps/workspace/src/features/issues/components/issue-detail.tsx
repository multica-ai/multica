"use client";

import { useState, useEffect, useCallback, useRef, memo } from "react";
import { useDefaultLayout, usePanelRef } from "react-resizable-panels";
import {
  Calendar,
  Archive,
  Check,
  ChevronDown,
  ChevronLeft,
  ChevronRight,
  Copy,
  Link2,
  MoreHorizontal,
  PanelRight,
  RotateCcw,
  UserMinus,
  Users,
  X,
} from "lucide-react";
import { toast } from "sonner";
import { Skeleton } from "@/components/ui/skeleton";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuGroup,
  DropdownMenuLabel,
  DropdownMenuSub,
  DropdownMenuSubTrigger,
  DropdownMenuSubContent,
} from "@/components/ui/dropdown-menu";
import { ResizablePanelGroup, ResizablePanel, ResizableHandle } from "@/components/ui/resizable";
import { ContentEditor, type ContentEditorRef } from "@/features/editor";
import { FileUploadButton } from "@/components/common/file-upload-button";
import { TitleEditor } from "@/features/editor";
import {
  Tooltip,
  TooltipTrigger,
  TooltipContent,
} from "@/components/ui/tooltip";
import { Popover, PopoverTrigger, PopoverContent } from "@/components/ui/popover";
import { Checkbox } from "@/components/ui/checkbox";
import { Command, CommandInput, CommandList, CommandEmpty, CommandGroup, CommandItem } from "@/components/ui/command";
import { AvatarGroup, AvatarGroupCount } from "@/components/ui/avatar";
import { ActorAvatar } from "@/components/common/actor-avatar";
import type { Issue, UpdateIssueRequest, IssueStatus, IssuePriority, TimelineEntry, IssueDependencyType } from "@/shared/types";
import { ALL_STATUSES, STATUS_CONFIG, PRIORITY_ORDER, PRIORITY_CONFIG } from "@/features/issues/config";
import { StatusIcon, PriorityIcon, DueDatePicker, IssueDateTimePicker, AssigneePicker, canAssignAgent, ParentIssuePicker, LabelPicker, DependencyPicker } from "@/features/issues/components";
import { CommentCard } from "./comment-card";
import { CommentInput } from "./comment-input";
import { AgentLiveCard, TaskRunHistory } from "./agent-live-card";
import { useAuthStore } from "@/features/auth";
import { useNavigationStore } from "@/features/navigation";
import { useWorkspaceStore, useActorName } from "@/features/workspace";
import { useIssueStore } from "@/features/issues";
import { useIssueTimeline } from "@/features/issues/hooks/use-issue-timeline";
import { useIssueReactions } from "@/features/issues/hooks/use-issue-reactions";
import { useIssueSubscribers } from "@/features/issues/hooks/use-issue-subscribers";
import { useIssueMutations } from "@/features/issues/mutations";
import { useIssueDetailQuery } from "@/features/issues/queries";
import { buildIssueTemplateData } from "@/features/issues/utils/template";
import { ReactionBar } from "@/components/common/reaction-bar";
import { useFileUpload } from "@/shared/hooks/use-file-upload";
import { ProjectPicker } from "@/features/projects/components/project-picker";
import { useModalStore } from "@/features/modals";
import { Link, useRouter } from "@/shared/router";
import { timeAgo } from "@/shared/utils";
import { useIsMobile } from "@/hooks/use-mobile";
import { IssueTimerSection } from "@/features/time-tracking";

function shortDate(date: string | null): string {
  if (!date) return "—";
  return new Date(date).toLocaleDateString("en-US", {
    month: "short",
    day: "numeric",
  });
}

function shortDateTime(date: string | null): string {
  if (!date) return "—";
  return new Date(date).toLocaleString("en-US", {
    month: "short",
    day: "numeric",
    hour: "numeric",
    minute: "2-digit",
  });
}

function statusLabel(status: string): string {
  return STATUS_CONFIG[status as IssueStatus]?.label ?? status;
}

function priorityLabel(priority: string): string {
  return PRIORITY_CONFIG[priority as IssuePriority]?.label ?? priority;
}

function formatActivity(
  entry: TimelineEntry,
  resolveActorName?: (type: string, id: string) => string,
): string {
  const details = (entry.details ?? {}) as Record<string, string>;
  switch (entry.action) {
    case "created":
      return "created this issue";
    case "status_changed":
      return `changed status from ${statusLabel(details.from ?? "?")} to ${statusLabel(details.to ?? "?")}`;
    case "priority_changed":
      return `changed priority from ${priorityLabel(details.from ?? "?")} to ${priorityLabel(details.to ?? "?")}`;
    case "assignee_changed": {
      const isSelfAssign = details.to_type === entry.actor_type && details.to_id === entry.actor_id;
      if (isSelfAssign) return "self-assigned this issue";
      const toName = details.to_id && details.to_type && resolveActorName
        ? resolveActorName(details.to_type, details.to_id)
        : null;
      if (toName) return `assigned to ${toName}`;
      if (details.from_id && !details.to_id) return "removed assignee";
      return "changed assignee";
    }
    case "due_date_changed": {
      if (!details.to) return "removed due date";
      const formatted = new Date(details.to).toLocaleDateString("en-US", { month: "short", day: "numeric" });
      return `set due date to ${formatted}`;
    }
    case "start_date_changed": {
      if (!details.to) return "removed start date";
      const formatted = shortDateTime(details.to);
      return `set start date to ${formatted}`;
    }
    case "end_date_changed": {
      if (!details.to) return "removed end date";
      const formatted = shortDateTime(details.to);
      return `set end date to ${formatted}`;
    }
    case "title_changed":
      return `renamed this issue from "${details.from ?? "?"}" to "${details.to ?? "?"}"`;
    case "description_updated":
      return "updated the description";
    case "task_completed":
      return "completed the task";
    case "task_failed":
      return "task failed";
    default:
      return entry.action ?? "";
  }
}


// ---------------------------------------------------------------------------
// Property row
// ---------------------------------------------------------------------------

function PropRow({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}) {
  return (
    <div className="flex min-h-8 items-center gap-2 rounded-md px-2 -mx-2 hover:bg-accent/50 transition-colors">
      <span className="w-16 shrink-0 text-xs text-muted-foreground">{label}</span>
      <div className="flex min-w-0 flex-1 items-center gap-1.5 text-xs truncate">
        {children}
      </div>
    </div>
  );
}

function RelationList({
  items,
  onRemove,
}: {
  items: { id: string; issue: { id: string; identifier: string; title: string } }[];
  onRemove: (dependencyId: string) => Promise<unknown>;
}) {
  if (items.length === 0) {
    return <span className="text-muted-foreground">None</span>;
  }

  return (
    <div className="flex min-w-0 flex-col gap-1">
      {items.map((item) => (
        <div key={item.id} className="flex min-w-0 items-center gap-2">
          <Link href={`/issues/${item.issue.id}`} className="truncate hover:underline">
            {item.issue.identifier} · {item.issue.title}
          </Link>
          <button
            type="button"
            className="text-[11px] text-muted-foreground hover:text-foreground"
            onClick={() => void onRemove(item.id)}
          >
            Remove
          </button>
        </div>
      ))}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Labels + Links section (main content area)
// ---------------------------------------------------------------------------

function IssueLabelsDepsSection({
  issue,
  onAddLabel,
  onRemoveLabel,
  onAddDependency,
  onRemoveDependency,
}: {
  issue: Issue;
  onAddLabel: (input: { labelId?: string; name?: string; color?: string }) => Promise<unknown>;
  onRemoveLabel: (labelId: string) => Promise<unknown>;
  onAddDependency: (dependencyIssueId: string, type: IssueDependencyType) => Promise<unknown>;
  onRemoveDependency: (dependencyId: string) => Promise<unknown>;
}) {
  const labels = issue.labels ?? [];
  const dependencies = issue.dependencies ?? null;

  return (
    <div className="rounded-xl border bg-card p-4 space-y-4">
      {/* Labels */}
      <div className="space-y-2">
        <div className="text-xs font-medium text-muted-foreground">Labels</div>
        <LabelPicker
          labels={labels}
          onAdd={onAddLabel}
          onRemove={onRemoveLabel}
          align="start"
        />
      </div>

      {/* Links */}
      <div className="space-y-2">
        <div className="flex items-center justify-between gap-2 text-xs font-medium text-muted-foreground">
          <span>Links</span>
          <div className="flex flex-wrap items-center gap-1">
            <DependencyPicker
              issueId={issue.id}
              dependencies={dependencies}
              type="blocks"
              onAdd={onAddDependency}
            />
            <DependencyPicker
              issueId={issue.id}
              dependencies={dependencies}
              type="blocked_by"
              onAdd={onAddDependency}
            />
            <DependencyPicker
              issueId={issue.id}
              dependencies={dependencies}
              type="related"
              onAdd={onAddDependency}
            />
            <DependencyPicker
              issueId={issue.id}
              dependencies={dependencies}
              type="copy"
              onAdd={onAddDependency}
            />
          </div>
        </div>
        <div className="space-y-2 text-xs">
          <div className="space-y-1">
            <div className="text-muted-foreground">Blocks</div>
            <RelationList items={dependencies?.blocks ?? []} onRemove={onRemoveDependency} />
          </div>
          <div className="space-y-1">
            <div className="text-muted-foreground">Blocked by</div>
            <RelationList items={dependencies?.blocked_by ?? []} onRemove={onRemoveDependency} />
          </div>
          <div className="space-y-1">
            <div className="text-muted-foreground">Related</div>
            <RelationList items={dependencies?.related ?? []} onRemove={onRemoveDependency} />
          </div>
          <div className="space-y-1">
            <div className="text-muted-foreground">Copy</div>
            <RelationList items={dependencies?.copy ?? []} onRemove={onRemoveDependency} />
          </div>
        </div>
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Properties sidebar sections (status, priority, assignee, dates, details)
// ---------------------------------------------------------------------------

function IssueSidebarSections({
  issue,
  propertiesOpen,
  detailsOpen,
  onToggleProperties,
  onToggleDetails,
  onUpdateField,
  getActorName,
}: {
  issue: Issue;
  propertiesOpen: boolean;
  detailsOpen: boolean;
  onToggleProperties: () => void;
  onToggleDetails: () => void;
  onUpdateField: (updates: Partial<UpdateIssueRequest>) => void;
  getActorName: (type: string, id: string) => string;
}) {
  const childIssues = issue.child_issues ?? [];

  return (
    <>
      <div>
        <button
          className={`mb-2 flex w-full items-center gap-1 text-xs font-medium transition-colors ${propertiesOpen ? "" : "text-muted-foreground hover:text-foreground"}`}
          onClick={onToggleProperties}
        >
          <ChevronRight className={`h-3.5 w-3.5 shrink-0 text-muted-foreground transition-transform ${propertiesOpen ? "rotate-90" : ""}`} />
          Properties
        </button>

        {propertiesOpen ? (
          <div className="space-y-0.5 pl-2">
            <PropRow label="Status">
              <DropdownMenu>
                <DropdownMenuTrigger className="flex items-center gap-1.5 cursor-pointer rounded px-1 -mx-1 overflow-hidden transition-colors hover:bg-accent/30">
                  <StatusIcon status={issue.status} className="h-3.5 w-3.5 shrink-0" />
                  <span className="truncate">{STATUS_CONFIG[issue.status].label}</span>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="start" className="w-44">
                  {ALL_STATUSES.map((status) => (
                    <DropdownMenuItem key={status} onClick={() => onUpdateField({ status })}>
                      <StatusIcon status={status} className="h-3.5 w-3.5" />
                      {STATUS_CONFIG[status].label}
                      {status === issue.status ? <Check className="ml-auto h-3.5 w-3.5" /> : null}
                    </DropdownMenuItem>
                  ))}
                </DropdownMenuContent>
              </DropdownMenu>
            </PropRow>

            <PropRow label="Priority">
              <DropdownMenu>
                <DropdownMenuTrigger className="flex items-center gap-1.5 cursor-pointer rounded px-1 -mx-1 overflow-hidden transition-colors hover:bg-accent/30">
                  <PriorityIcon priority={issue.priority} className="shrink-0" />
                  <span className="truncate">{PRIORITY_CONFIG[issue.priority].label}</span>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="start" className="w-44">
                  {PRIORITY_ORDER.map((priority) => (
                    <DropdownMenuItem key={priority} onClick={() => onUpdateField({ priority })}>
                      <span className={`inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-xs font-medium ${PRIORITY_CONFIG[priority].badgeBg} ${PRIORITY_CONFIG[priority].badgeText}`}>
                        <PriorityIcon priority={priority} className="h-3 w-3" inheritColor />
                        {PRIORITY_CONFIG[priority].label}
                      </span>
                      {priority === issue.priority ? <Check className="ml-auto h-3.5 w-3.5" /> : null}
                    </DropdownMenuItem>
                  ))}
                </DropdownMenuContent>
              </DropdownMenu>
            </PropRow>

            <PropRow label="Assignee">
              <AssigneePicker
                assigneeType={issue.assignee_type}
                assigneeId={issue.assignee_id}
                onUpdate={onUpdateField}
                align="start"
              />
            </PropRow>

            <PropRow label="Project">
              <ProjectPicker
                projectId={issue.project_id}
                onUpdate={onUpdateField}
                align="start"
              />
            </PropRow>

            <PropRow label="Parent">
              <ParentIssuePicker
                issueId={issue.id}
                parentIssueId={issue.parent_issue_id}
                parentIssue={issue.parent_issue}
                onUpdate={onUpdateField}
                align="start"
              />
            </PropRow>

            <PropRow label="Start date">
              <IssueDateTimePicker
                field="start_date"
                dateTimeValue={issue.start_date}
                onUpdate={onUpdateField}
              />
            </PropRow>

            <PropRow label="End date">
              <IssueDateTimePicker
                field="end_date"
                dateTimeValue={issue.end_date}
                onUpdate={onUpdateField}
              />
            </PropRow>

            <PropRow label="Due date">
              <DueDatePicker
                dueDate={issue.due_date}
                onUpdate={onUpdateField}
              />
            </PropRow>
          </div>
        ) : null}
      </div>

      <div>
        <button
          className={`mb-2 flex w-full items-center gap-1 text-xs font-medium transition-colors ${detailsOpen ? "" : "text-muted-foreground hover:text-foreground"}`}
          onClick={onToggleDetails}
        >
          <ChevronRight className={`h-3.5 w-3.5 shrink-0 text-muted-foreground transition-transform ${detailsOpen ? "rotate-90" : ""}`} />
          Details
        </button>

        {detailsOpen ? (
          <div className="space-y-0.5 pl-2">
            <PropRow label="Created by">
              <ActorAvatar
                actorType={issue.creator_type}
                actorId={issue.creator_id}
                size={18}
              />
              <span className="truncate">{getActorName(issue.creator_type, issue.creator_id)}</span>
            </PropRow>
            <PropRow label="Created">
              <span className="text-muted-foreground">{shortDate(issue.created_at)}</span>
            </PropRow>
            <PropRow label="Updated">
              <span className="text-muted-foreground">{shortDate(issue.updated_at)}</span>
            </PropRow>
            <div className="space-y-1 rounded-md px-2 py-2 -mx-2 hover:bg-accent/50 transition-colors">
              <div className="text-xs text-muted-foreground">Children</div>
              {childIssues.length > 0 ? (
                <div className="flex min-w-0 flex-col gap-1 text-xs">
                  {childIssues.map((childIssue) => (
                    <Link
                      key={childIssue.id}
                      href={`/issues/${childIssue.id}`}
                      className="truncate hover:underline"
                    >
                      {childIssue.identifier} · {childIssue.title}
                    </Link>
                  ))}
                </div>
              ) : (
                <span className="text-xs text-muted-foreground">No child issues</span>
              )}
            </div>
          </div>
        ) : null}
      </div>
    </>
  );
}


// ---------------------------------------------------------------------------
// Props
// ---------------------------------------------------------------------------

interface IssueDetailProps {
  issueId: string;
  onDelete?: () => void;
  defaultSidebarOpen?: boolean;
  layoutId?: string;
  /** When set, the issue detail will auto-scroll to this comment and briefly highlight it. */
  highlightCommentId?: string;
}

// ---------------------------------------------------------------------------
// IssueDetail
// ---------------------------------------------------------------------------

export function IssueDetail({ issueId, onDelete, defaultSidebarOpen = true, layoutId = "multica_issue_detail_layout", highlightCommentId }: IssueDetailProps) {
  const id = issueId;
  const isMobile = useIsMobile();
  const router = useRouter();
  const user = useAuthStore((s) => s.user);
  const workspace = useWorkspaceStore((s) => s.workspace);
  const members = useWorkspaceStore((s) => s.members);
  const agents = useWorkspaceStore((s) => s.agents);
  const currentMemberRole = members.find((m) => m.user_id === user?.id)?.role;

  // Issue navigation
  const allIssues = useIssueStore((s) => s.issues);
  const currentIndex = allIssues.findIndex((i) => i.id === id);
  const prevIssue = currentIndex > 0 ? allIssues[currentIndex - 1] : null;
  const nextIssue = currentIndex < allIssues.length - 1 ? allIssues[currentIndex + 1] : null;
  const { getActorName } = useActorName();
  const lastPath = useNavigationStore((state) => state.lastPath);
  const { uploadWithToast } = useFileUpload();
  const { defaultLayout, onLayoutChanged } = useDefaultLayout({
    id: layoutId,
  });
  const sidebarRef = usePanelRef();
  const [sidebarOpen, setSidebarOpen] = useState(defaultSidebarOpen);
  const [deleting, setDeleting] = useState(false);
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);
  const [propertiesOpen, setPropertiesOpen] = useState(true);
  const [detailsOpen, setDetailsOpen] = useState(true);
  const scrollContainerRef = useRef<HTMLDivElement>(null);
  const [showScrollBottom, setShowScrollBottom] = useState(false);
  const [highlightedId, setHighlightedId] = useState<string | null>(null);
  const didHighlightRef = useRef<string | null>(null);
  const issueDetailQuery = useIssueDetailQuery(id);
  const {
    updateIssue,
    archiveIssue,
    restoreIssue,
    addIssueLabel,
    removeIssueLabel,
    addIssueDependency,
    removeIssueDependency,
  } = useIssueMutations();

  useEffect(() => {
    if (isMobile) {
      setSidebarOpen(false);
    }
  }, [isMobile]);

  useEffect(() => {
    if (!defaultSidebarOpen || isMobile) return;

    const panel = sidebarRef.current;
    if (!panel || !panel.isCollapsed()) return;

    panel.expand();
  }, [defaultSidebarOpen, isMobile, sidebarRef]);

  // Single source of truth: read issue directly from global store
  const listedIssue = useIssueStore((s) => s.issues.find((i) => i.id === id)) ?? null;
  const issue = issueDetailQuery.data ?? listedIssue ?? null;
  const issueLoading = !issue && issueDetailQuery.isPending;

  // Custom hooks — encapsulate timeline, reactions, subscribers
  const {
    timeline, loading: timelineLoading, submitting, submitComment, submitReply,
    editComment, deleteComment, toggleReaction: handleToggleReaction,
  } = useIssueTimeline(id, user?.id);

  const {
    reactions: issueReactions, loading: reactionsLoading,
    toggleReaction: handleToggleIssueReaction,
  } = useIssueReactions(id, user?.id);

  const {
    subscribers, loading: subscribersLoading, isSubscribed, toggleSubscribe: handleToggleSubscribe, toggleSubscriber,
  } = useIssueSubscribers(id, user?.id);

  const loading = issueLoading;
  const backHref = lastPath && !lastPath.startsWith("/issues/") ? lastPath : "/issues";

  // Scroll to highlighted comment once timeline loads (fire only once per highlightCommentId)
  useEffect(() => {
    if (!highlightCommentId || timeline.length === 0) return;
    if (didHighlightRef.current === highlightCommentId) return;
    const el = document.getElementById(`comment-${highlightCommentId}`);
    if (el) {
      didHighlightRef.current = highlightCommentId;
      requestAnimationFrame(() => {
        el.scrollIntoView({ behavior: "smooth", block: "center" });
        setHighlightedId(highlightCommentId);
        const timer = setTimeout(() => setHighlightedId(null), 2000);
        return () => clearTimeout(timer);
      });
    }
  }, [highlightCommentId, timeline.length]);

  // Track scroll position for jump-to-bottom button
  useEffect(() => {
    const container = scrollContainerRef.current;
    if (!container) return;
    const onScroll = () => {
      const { scrollTop, scrollHeight, clientHeight } = container;
      setShowScrollBottom(scrollHeight - scrollTop - clientHeight > 200);
    };
    container.addEventListener("scroll", onScroll, { passive: true });
    onScroll();
    return () => container.removeEventListener("scroll", onScroll);
  }, []);

  const scrollToBottom = useCallback(() => {
    scrollContainerRef.current?.scrollTo({ top: scrollContainerRef.current.scrollHeight, behavior: "smooth" });
  }, []);

  // Issue field updates — write directly to the global store (single source of truth)
  const handleUpdateField = useCallback(
    (updates: Partial<UpdateIssueRequest>) => {
      if (!issue) return;
      void updateIssue(id, updates).catch((error) => {
        toast.error(error instanceof Error ? error.message : "Failed to update issue");
      });
    },
    [id, issue, updateIssue],
  );

  const descEditorRef = useRef<ContentEditorRef>(null);
  // Description embeds are inline markdown content — they should not appear in
  // the issue attachment list, so we intentionally omit issueId here.
  const handleDescriptionUpload = useCallback(
    (file: File) => uploadWithToast(file),
    [uploadWithToast],
  );

  const handleDelete = async () => {
    setDeleting(true);
    try {
      await archiveIssue(issue!.id);
      toast.success("Issue archived");
      if (onDelete) onDelete();
      else router.push(backHref);
    } catch {
      toast.error("Failed to archive issue");
      setDeleting(false);
    }
  };

  const handleRestore = async () => {
    if (!issue) return;
    setDeleting(true);
    try {
      await restoreIssue(issue.id);
      toast.success("Issue restored");
      setDeleting(false);
    } catch {
      toast.error("Failed to restore issue");
      setDeleting(false);
    }
  };

  const handleAddIssueLabel = useCallback(
    (input: { labelId?: string; name?: string; color?: string }) => {
      return addIssueLabel(id, input).catch(() => {
        toast.error("Failed to update labels");
      });
    },
    [addIssueLabel, id],
  );

  const handleRemoveIssueLabel = useCallback(
    (labelId: string) => {
      return removeIssueLabel(id, labelId).catch(() => {
        toast.error("Failed to update labels");
      });
    },
    [id, removeIssueLabel],
  );

  const handleAddIssueDependency = useCallback(
    (dependencyIssueId: string, type: IssueDependencyType) => {
      return addIssueDependency(id, dependencyIssueId, type).catch((error: Error) => {
        toast.error(error.message || "Failed to update links");
      });
    },
    [addIssueDependency, id],
  );

  const handleRemoveIssueDependency = useCallback(
    (dependencyId: string) => {
      return removeIssueDependency(id, dependencyId).catch(() => {
        toast.error("Failed to update links");
      });
    },
    [id, removeIssueDependency],
  );

  if (loading) {
    return (
      <div className="flex flex-1 min-h-0 flex-col">
        {/* Header skeleton */}
        <div className="flex h-12 shrink-0 items-center gap-2 border-b px-4">
          <Skeleton className="h-4 w-16" />
          <Skeleton className="h-4 w-4" />
          <Skeleton className="h-4 w-24" />
        </div>
        <div className="flex flex-1 min-h-0">
          {/* Content skeleton */}
          <div className="flex-1 p-8 space-y-6">
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
                <Skeleton className="h-8 w-8 rounded-full" />
                <div className="flex-1 space-y-2">
                  <Skeleton className="h-4 w-32" />
                  <Skeleton className="h-16 w-full rounded-lg" />
                </div>
              </div>
            </div>
          </div>
          {/* Sidebar skeleton */}
          <div className="w-64 border-l p-4 space-y-4">
            {Array.from({ length: 4 }).map((_, i) => (
              <div key={i} className="flex items-center justify-between">
                <Skeleton className="h-3 w-16" />
                <Skeleton className="h-5 w-24" />
              </div>
            ))}
            <Skeleton className="h-px w-full" />
            {Array.from({ length: 3 }).map((_, i) => (
              <div key={i} className="flex items-center justify-between">
                <Skeleton className="h-3 w-16" />
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
        <p>This issue does not exist or has been deleted in this workspace.</p>
        {!onDelete && (
          <Button variant="outline" size="sm" onClick={() => router.push(backHref)}>
            <ChevronLeft className="mr-1 h-3.5 w-3.5" />
            Back to Issues
          </Button>
        )}
      </div>
    );
  }

  return (
    <ResizablePanelGroup orientation="horizontal" className="flex-1 min-h-0" defaultLayout={defaultLayout} onLayoutChanged={onLayoutChanged}>
      <ResizablePanel id="content" minSize="50%">
      {/* LEFT: Content area */}
      <div className="flex h-full flex-col">
        {/* Header bar */}
        <div className="flex h-12 shrink-0 items-center justify-between border-b bg-background px-4 text-sm">
          <div className="flex items-center gap-1.5 min-w-0">
            {workspace && (
              <>
                <Link
                  href={backHref}
                  className="text-muted-foreground hover:text-foreground transition-colors truncate shrink-0"
                >
                  {workspace.name}
                </Link>
                <ChevronRight className="h-3 w-3 text-muted-foreground/50 shrink-0" />
              </>
            )}
            <span className="truncate text-muted-foreground">
              {issue.identifier}
            </span>
            <ChevronRight className="h-3 w-3 text-muted-foreground/50 shrink-0" />
            <span className="truncate">{issue.title}</span>
          </div>
          <div className="flex items-center gap-1 shrink-0">
            {/* Issue navigation */}
            {allIssues.length > 1 && (
              <div className="flex items-center gap-0.5 mr-1">
                <Tooltip>
                  <TooltipTrigger
                    render={
                      <Button
                        variant="ghost"
                        size="icon-xs"
                        className="text-muted-foreground"
                        disabled={!prevIssue}
                        onClick={() => prevIssue && router.push(`/issues/${prevIssue.id}`)}
                      >
                        <ChevronLeft className="h-4 w-4" />
                      </Button>
                    }
                  />
                  <TooltipContent side="bottom">Previous issue</TooltipContent>
                </Tooltip>
                <span className="text-xs text-muted-foreground tabular-nums px-0.5">
                  {currentIndex >= 0 ? currentIndex + 1 : "?"} / {allIssues.length}
                </span>
                <Tooltip>
                  <TooltipTrigger
                    render={
                      <Button
                        variant="ghost"
                        size="icon-xs"
                        className="text-muted-foreground"
                        disabled={!nextIssue}
                        onClick={() => nextIssue && router.push(`/issues/${nextIssue.id}`)}
                      >
                        <ChevronRight className="h-4 w-4" />
                      </Button>
                    }
                  />
                  <TooltipContent side="bottom">Next issue</TooltipContent>
                </Tooltip>
              </div>
            )}
            <DropdownMenu>
              <DropdownMenuTrigger
                render={
                  <Button variant="ghost" size="icon-xs" className="text-muted-foreground">
                    <MoreHorizontal className="h-4 w-4" />
                  </Button>
                }
              />
              <DropdownMenuContent align="end" className="w-auto">
                {/* Status */}
                <DropdownMenuSub>
                  <DropdownMenuSubTrigger>
                    <StatusIcon status={issue.status} className="h-3.5 w-3.5" />
                    Status
                  </DropdownMenuSubTrigger>
                  <DropdownMenuSubContent>
                    {ALL_STATUSES.map((s) => (
                      <DropdownMenuItem
                        key={s}
                        onClick={() => handleUpdateField({ status: s })}
                      >
                        <StatusIcon status={s} className="h-3.5 w-3.5" />
                        {STATUS_CONFIG[s].label}
                        {issue.status === s && <span className="ml-auto text-xs text-muted-foreground">✓</span>}
                      </DropdownMenuItem>
                    ))}
                  </DropdownMenuSubContent>
                </DropdownMenuSub>

                {/* Priority */}
                <DropdownMenuSub>
                  <DropdownMenuSubTrigger>
                    <PriorityIcon priority={issue.priority} />
                    Priority
                  </DropdownMenuSubTrigger>
                  <DropdownMenuSubContent>
                    {PRIORITY_ORDER.map((p) => (
                      <DropdownMenuItem
                        key={p}
                        onClick={() => handleUpdateField({ priority: p })}
                      >
                        <span className={`inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-xs font-medium ${PRIORITY_CONFIG[p].badgeBg} ${PRIORITY_CONFIG[p].badgeText}`}>
                          <PriorityIcon priority={p} className="h-3 w-3" inheritColor />
                          {PRIORITY_CONFIG[p].label}
                        </span>
                        {issue.priority === p && <span className="ml-auto text-xs text-muted-foreground">✓</span>}
                      </DropdownMenuItem>
                    ))}
                  </DropdownMenuSubContent>
                </DropdownMenuSub>

                {/* Assignee */}
                <DropdownMenuSub>
                  <DropdownMenuSubTrigger>
                    <UserMinus className="h-3.5 w-3.5" />
                    Assignee
                  </DropdownMenuSubTrigger>
                  <DropdownMenuSubContent>
                    <DropdownMenuItem
                      onClick={() => handleUpdateField({ assignee_type: null, assignee_id: null })}
                    >
                      <UserMinus className="h-3.5 w-3.5 text-muted-foreground" />
                      Unassigned
                      {!issue.assignee_type && <span className="ml-auto text-xs text-muted-foreground">✓</span>}
                    </DropdownMenuItem>
                    {members.map((m) => (
                      <DropdownMenuItem
                        key={m.user_id}
                        onClick={() => handleUpdateField({ assignee_type: "member", assignee_id: m.user_id })}
                      >
                        <ActorAvatar actorType="member" actorId={m.user_id} size={16} />
                        {m.name}
                        {issue.assignee_type === "member" && issue.assignee_id === m.user_id && <span className="ml-auto text-xs text-muted-foreground">✓</span>}
                      </DropdownMenuItem>
                    ))}
                    {agents.filter((a) => !a.archived_at && canAssignAgent(a, user?.id, currentMemberRole)).map((a) => (
                      <DropdownMenuItem
                        key={a.id}
                        onClick={() => handleUpdateField({ assignee_type: "agent", assignee_id: a.id })}
                      >
                        <ActorAvatar actorType="agent" actorId={a.id} size={16} />
                        {a.name}
                        {issue.assignee_type === "agent" && issue.assignee_id === a.id && <span className="ml-auto text-xs text-muted-foreground">✓</span>}
                      </DropdownMenuItem>
                    ))}
                  </DropdownMenuSubContent>
                </DropdownMenuSub>

                {/* Due date */}
                <DropdownMenuSub>
                  <DropdownMenuSubTrigger>
                    <Calendar className="h-3.5 w-3.5" />
                    Due date
                  </DropdownMenuSubTrigger>
                  <DropdownMenuSubContent>
                    <DropdownMenuItem onClick={() => handleUpdateField({ due_date: new Date().toISOString() })}>
                      Today
                    </DropdownMenuItem>
                    <DropdownMenuItem onClick={() => {
                      const d = new Date(); d.setDate(d.getDate() + 1);
                      handleUpdateField({ due_date: d.toISOString() });
                    }}>
                      Tomorrow
                    </DropdownMenuItem>
                    <DropdownMenuItem onClick={() => {
                      const d = new Date(); d.setDate(d.getDate() + 7);
                      handleUpdateField({ due_date: d.toISOString() });
                    }}>
                      Next week
                    </DropdownMenuItem>
                    {issue.due_date && (
                      <>
                        <DropdownMenuSeparator />
                        <DropdownMenuItem onClick={() => handleUpdateField({ due_date: null })}>
                          Clear date
                        </DropdownMenuItem>
                      </>
                    )}
                  </DropdownMenuSubContent>
                </DropdownMenuSub>

                <DropdownMenuSeparator />

                <DropdownMenuItem onClick={() => useModalStore.getState().open("create-issue", buildIssueTemplateData(issue))}>
                  <Copy className="h-3.5 w-3.5" />
                  Create from template
                </DropdownMenuItem>

                {/* Copy link */}
                <DropdownMenuItem onClick={() => {
                  navigator.clipboard.writeText(window.location.href);
                  toast.success("Link copied");
                }}>
                  <Link2 className="h-3.5 w-3.5" />
                  Copy link
                </DropdownMenuItem>

                <DropdownMenuSeparator />

                {/* Delete */}
                {issue.archived_at ? (
                  <DropdownMenuItem onClick={handleRestore}>
                    <RotateCcw className="h-3.5 w-3.5" />
                    Restore issue
                  </DropdownMenuItem>
                ) : (
                  <DropdownMenuItem onClick={() => setDeleteDialogOpen(true)}>
                    <Archive className="h-3.5 w-3.5" />
                    Archive issue
                  </DropdownMenuItem>
                )}
              </DropdownMenuContent>
            </DropdownMenu>
            {!isMobile ? (
              <Tooltip>
                <TooltipTrigger
                  render={
                    <Button
                      variant={sidebarOpen ? "secondary" : "ghost"}
                      size="icon-xs"
                      className={sidebarOpen ? "" : "text-muted-foreground"}
                      onClick={() => {
                        const panel = sidebarRef.current;
                        if (!panel) return;
                        if (panel.isCollapsed()) panel.expand();
                        else panel.collapse();
                      }}
                    >
                      <PanelRight className="h-4 w-4" />
                    </Button>
                  }
                />
                <TooltipContent side="bottom">Toggle sidebar</TooltipContent>
              </Tooltip>
            ) : null}
          </div>

            {/* Archive confirmation dialog (controlled by state) */}
            <AlertDialog open={deleteDialogOpen} onOpenChange={setDeleteDialogOpen}>
              <AlertDialogContent>
                <AlertDialogHeader>
                  <AlertDialogTitle>Archive issue</AlertDialogTitle>
                  <AlertDialogDescription>
                    This issue will leave active views, but comments, attachments, labels, links, and time history will be preserved.
                  </AlertDialogDescription>
                </AlertDialogHeader>
                <AlertDialogFooter>
                  <AlertDialogCancel>Cancel</AlertDialogCancel>
                  <AlertDialogAction
                    onClick={handleDelete}
                    disabled={deleting}
                  >
                    {deleting ? "Archiving..." : "Archive"}
                  </AlertDialogAction>
                </AlertDialogFooter>
              </AlertDialogContent>
            </AlertDialog>
          </div>

        {/* Content — scrollable */}
        <div ref={scrollContainerRef} className="relative flex-1 overflow-y-auto">
        <div className="mx-auto w-full max-w-4xl px-8 py-8">
          {issue.archived_at && (
            <div className="mb-5 flex items-center justify-between rounded-md border bg-muted/40 px-3 py-2 text-sm">
              <span className="text-muted-foreground">This issue is archived.</span>
              <Button size="sm" variant="outline" onClick={handleRestore} disabled={deleting}>
                <RotateCcw className="mr-1.5 h-3.5 w-3.5" />
                Restore
              </Button>
            </div>
          )}
          <TitleEditor
            key={`title-${id}`}
            defaultValue={issue.title}
            placeholder="Issue title"
            className="w-full text-2xl font-bold leading-snug tracking-tight"
            onBlur={(value) => {
              const trimmed = value.trim();
              if (trimmed && trimmed !== issue.title) handleUpdateField({ title: trimmed });
            }}
          />

          <ContentEditor
            ref={descEditorRef}
            key={id}
            defaultValue={issue.description || ""}
            placeholder="Add description..."
            toolbar
            onUpdate={(md) => handleUpdateField({ description: md || undefined })}
            onUploadFile={handleDescriptionUpload}
            debounceMs={1500}
            className="mt-5"
          />

          <div className="flex items-center gap-1 mt-3">
            {reactionsLoading ? (
              <div className="flex items-center gap-1">
                <Skeleton className="h-7 w-14 rounded-full" />
                <Skeleton className="h-7 w-14 rounded-full" />
              </div>
            ) : (
              <ReactionBar
                reactions={issueReactions}
                currentUserId={user?.id}
                onToggle={handleToggleIssueReaction}
              />
            )}
            <FileUploadButton
              size="sm"
              onSelect={(file) => descEditorRef.current?.uploadFile(file)}
            />
          </div>

          <div className="mt-6 space-y-5 rounded-xl border bg-card p-4 md:hidden">
            <IssueSidebarSections
              issue={issue}
              propertiesOpen={propertiesOpen}
              detailsOpen={detailsOpen}
              onToggleProperties={() => setPropertiesOpen(!propertiesOpen)}
              onToggleDetails={() => setDetailsOpen(!detailsOpen)}
              onUpdateField={handleUpdateField}
              getActorName={getActorName}
            />
          </div>

          <div className="mt-6">
            <IssueLabelsDepsSection
              issue={issue}
              onAddLabel={handleAddIssueLabel}
              onRemoveLabel={handleRemoveIssueLabel}
              onAddDependency={handleAddIssueDependency}
              onRemoveDependency={handleRemoveIssueDependency}
            />
          </div>

          <div className="my-8 border-t" />

          {/* Time tracking */}
          <IssueTimerSection issueId={issue.id} />

          <div className="my-8 border-t" />

          {/* Activity / Comments */}
          <div>
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-3">
                <h2 className="text-base font-semibold">Activity</h2>
              </div>
              <div className="flex items-center gap-2">
                {subscribersLoading ? (
                  <div className="flex items-center gap-1">
                    <Skeleton className="h-4 w-16" />
                    <div className="flex -space-x-1">
                      <Skeleton className="h-6 w-6 rounded-full" />
                      <Skeleton className="h-6 w-6 rounded-full" />
                    </div>
                  </div>
                ) : (<>
                <button
                  onClick={handleToggleSubscribe}
                  className="text-xs text-muted-foreground hover:text-foreground transition-colors"
                >
                  {isSubscribed ? "Unsubscribe" : "Subscribe"}
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
                      <CommandInput placeholder="Change subscribers..." />
                      <CommandList className="max-h-64">
                        <CommandEmpty>No results found</CommandEmpty>
                        {members.length > 0 && (
                          <CommandGroup heading="Members">
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
                          <CommandGroup heading="Agents">
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
                                  <ActorAvatar actorType="agent" actorId={a.id} size={22} />
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
                </>)}
              </div>
            </div>

            {/* Agent live output */}
            <AgentLiveCard
              issueId={id}
              agentName={issue.assignee_type === "agent" && issue.assignee_id ? getActorName("agent", issue.assignee_id) : undefined}
              scrollContainerRef={scrollContainerRef}
            />

            {/* Agent execution history */}
            <div className="mt-3">
              <TaskRunHistory issueId={id} />
            </div>

            {/* Timeline entries */}
            <div className="mt-4 flex flex-col gap-3">
              {timelineLoading ? (
                <div className="space-y-4">
                  {Array.from({ length: 3 }).map((_, i) => (
                    <div key={i} className="flex items-start gap-3 px-4">
                      <Skeleton className="h-8 w-8 rounded-full shrink-0" />
                      <div className="flex-1 space-y-2">
                        <Skeleton className="h-4 w-32" />
                        <Skeleton className="h-16 w-full rounded-lg" />
                      </div>
                    </div>
                  ))}
                </div>
              ) : (() => {
                const topLevel = timeline.filter((e) => e.type === "activity" || !e.parent_id);
                const repliesByParent = new Map<string, TimelineEntry[]>();
                for (const e of timeline) {
                  if (e.type === "comment" && e.parent_id) {
                    const list = repliesByParent.get(e.parent_id) ?? [];
                    list.push(e);
                    repliesByParent.set(e.parent_id, list);
                  }
                }

                // Coalesce: same actor + same action within 2 min → keep last only
                const COALESCE_MS = 2 * 60 * 1000;
                const coalesced: TimelineEntry[] = [];
                for (const entry of topLevel) {
                  if (entry.type === "activity") {
                    const prev = coalesced[coalesced.length - 1];
                    if (
                      prev?.type === "activity" &&
                      prev.action === entry.action &&
                      prev.actor_type === entry.actor_type &&
                      prev.actor_id === entry.actor_id &&
                      Math.abs(new Date(entry.created_at).getTime() - new Date(prev.created_at).getTime()) <= COALESCE_MS
                    ) {
                      // Replace previous with this one (keep the later result)
                      coalesced[coalesced.length - 1] = entry;
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

                return groups.map((group) => {
                  if (group.type === "comment") {
                    const entry = group.entries[0]!;
                    return (
                      <div key={entry.id} id={`comment-${entry.id}`}>
                        <CommentCard
                          issueId={id}
                          entry={entry}
                          allReplies={repliesByParent}
                          currentUserId={user?.id}
                          onReply={submitReply}
                          onEdit={editComment}
                          onDelete={deleteComment}
                          onToggleReaction={handleToggleReaction}
                          highlightedCommentId={highlightedId}
                        />
                      </div>
                    );
                  }

                  return (
                    <div key={group.entries[0]!.id} className="px-4 flex flex-col gap-3">
                      {group.entries.map((entry, idx) => {
                        const details = (entry.details ?? {}) as Record<string, string>;
                        const isStatusChange = entry.action === "status_changed";
                        const isPriorityChange = entry.action === "priority_changed";
                        const isScheduleDateChange = entry.action === "due_date_changed" || entry.action === "start_date_changed" || entry.action === "end_date_changed";

                        let leadIcon: React.ReactNode;
                        if (isStatusChange && details.to) {
                          leadIcon = <StatusIcon status={details.to as IssueStatus} className="h-4 w-4 shrink-0" />;
                        } else if (isPriorityChange && details.to) {
                          leadIcon = <PriorityIcon priority={details.to as IssuePriority} className="h-4 w-4 shrink-0" />;
                        } else if (isScheduleDateChange) {
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
                              <span className="truncate">{formatActivity(entry, getActorName)}</span>
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
                });
              })()}
            </div>

            {/* Bottom comment input — no avatar, full width */}
            <div className="mt-4">
              <CommentInput issueId={id} onSubmit={submitComment} />
            </div>
          </div>
        </div>
        {/* Jump to bottom button */}
        {showScrollBottom && (
          <div className="sticky bottom-4 flex justify-center pointer-events-none">
            <Button
              variant="secondary"
              size="sm"
              className="pointer-events-auto shadow-md"
              onClick={scrollToBottom}
            >
              <ChevronDown className="mr-1 h-3.5 w-3.5" />
              Jump to bottom
            </Button>
          </div>
        )}
        </div>
      </div>
      </ResizablePanel>
      {!isMobile ? (
        <>
          <ResizableHandle />
          <ResizablePanel
            id="sidebar"
            defaultSize={defaultSidebarOpen ? 320 : 0}
            minSize={260}
            maxSize={420}
            collapsible
            groupResizeBehavior="preserve-pixel-size"
            panelRef={sidebarRef}
            onResize={(size) => setSidebarOpen(size.inPixels > 0)}
          >
          {/* RIGHT: Properties sidebar */}
          <div className="h-full overflow-y-auto border-l">
            <div className="space-y-5 p-4">
              <IssueSidebarSections
                issue={issue}
                propertiesOpen={propertiesOpen}
                detailsOpen={detailsOpen}
                onToggleProperties={() => setPropertiesOpen(!propertiesOpen)}
                onToggleDetails={() => setDetailsOpen(!detailsOpen)}
                onUpdateField={handleUpdateField}
                getActorName={getActorName}
              />
            </div>
          </div>
          </ResizablePanel>
        </>
      ) : null}
    </ResizablePanelGroup>
  );
}
