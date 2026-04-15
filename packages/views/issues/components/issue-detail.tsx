"use client";

import { useState, useEffect, useCallback, useRef } from "react";
import { useDefaultLayout, usePanelRef } from "react-resizable-panels";
import { AppLink } from "../../navigation";
import { useNavigation } from "../../navigation";
import {
  ArrowDown,
  ArrowUp,
  Calendar,
  ChevronDown,
  ChevronLeft,
  ChevronRight,
  Link2,
  MoreHorizontal,
  PanelRight,
  Pin,
  PinOff,
  Plus,
  Trash2,
  UserMinus,
  Users,
} from "lucide-react";
import { toast } from "sonner";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
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
import { Button } from "@multica/ui/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuSub,
  DropdownMenuSubTrigger,
  DropdownMenuSubContent,
} from "@multica/ui/components/ui/dropdown-menu";
import { ResizablePanelGroup, ResizablePanel, ResizableHandle } from "@multica/ui/components/ui/resizable";
import { ContentEditor, type ContentEditorRef, TitleEditor, useFileDropZone, FileDropOverlay } from "../../editor";
import { FileUploadButton } from "@multica/ui/components/common/file-upload-button";
import {
  Tooltip,
  TooltipTrigger,
  TooltipContent,
} from "@multica/ui/components/ui/tooltip";
import { Popover, PopoverTrigger, PopoverContent } from "@multica/ui/components/ui/popover";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { Command, CommandDialog, CommandInput, CommandList, CommandEmpty, CommandGroup, CommandItem } from "@multica/ui/components/ui/command";
import { AvatarGroup, AvatarGroupCount } from "@multica/ui/components/ui/avatar";
import { ActorAvatar } from "../../common/actor-avatar";
import type { UpdateIssueRequest, IssueStatus, IssuePriority, TimelineEntry, Issue } from "@multica/core/types";
import { ALL_STATUSES, STATUS_CONFIG, PRIORITY_ORDER, PRIORITY_CONFIG } from "@multica/core/issues/config";
import { StatusIcon, PriorityIcon, StatusPicker, PriorityPicker, DueDatePicker, AssigneePicker, canAssignAgent } from ".";
import { ProjectPicker } from "../../projects/components/project-picker";
import { CommentCard } from "./comment-card";
import { CommentInput } from "./comment-input";
import { AgentLiveCard, TaskRunHistory } from "./agent-live-card";
import { useQuery } from "@tanstack/react-query";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceStore } from "@multica/core/workspace";
import { useActorName } from "@multica/core/workspace/hooks";
import { useWorkspaceId } from "@multica/core/hooks";
import { issueListOptions, issueDetailOptions, childIssuesOptions, issueUsageOptions } from "@multica/core/issues/queries";
import { memberListOptions, agentListOptions } from "@multica/core/workspace/queries";
import { useUpdateIssue, useDeleteIssue } from "@multica/core/issues/mutations";
import { useRecentIssuesStore } from "@multica/core/issues/stores";
import { useIssueTimeline } from "../hooks/use-issue-timeline";
import { useIssueReactions } from "../hooks/use-issue-reactions";
import { useIssueSubscribers } from "../hooks/use-issue-subscribers";
import { ReactionBar } from "@multica/ui/components/common/reaction-bar";
import { useFileUpload } from "@multica/core/hooks/use-file-upload";
import { api } from "@multica/core/api";
import { useModalStore } from "@multica/core/modals";
import { timeAgo } from "@multica/core/utils";
import { cn } from "@multica/ui/lib/utils";
import { pinListOptions } from "@multica/core/pins";
import { useCreatePin, useDeletePin } from "@multica/core/pins";

import { ProgressRing } from "./progress-ring";

function shortDate(date: string | null): string {
  if (!date) return "—";
  return new Date(date).toLocaleDateString("en-US", {
    month: "short",
    day: "numeric",
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
// Helpers
// ---------------------------------------------------------------------------

function formatTokenCount(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}k`;
  return String(n);
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


// ---------------------------------------------------------------------------
// Issue Picker Dialog
// ---------------------------------------------------------------------------

function IssuePickerDialog({
  open,
  onOpenChange,
  title,
  description,
  excludeIds,
  onSelect,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  description: string;
  excludeIds: string[];
  onSelect: (issue: Issue) => void;
}) {
  const [query, setQuery] = useState("");
  const [results, setResults] = useState<Issue[]>([]);
  const [isLoading, setIsLoading] = useState(false);
  const debounceRef = useRef<ReturnType<typeof setTimeout>>(undefined);
  const abortRef = useRef<AbortController>(undefined);

  // Reset state when dialog opens/closes
  useEffect(() => {
    if (!open) {
      setQuery("");
      setResults([]);
      setIsLoading(false);
    }
  }, [open]);

  const search = useCallback(
    (q: string) => {
      if (debounceRef.current) clearTimeout(debounceRef.current);
      if (abortRef.current) abortRef.current.abort();

      if (!q.trim()) {
        setResults([]);
        setIsLoading(false);
        return;
      }

      setIsLoading(true);
      debounceRef.current = setTimeout(async () => {
        const controller = new AbortController();
        abortRef.current = controller;
        try {
          const res = await api.searchIssues({
            q: q.trim(),
            limit: 20,
            include_closed: true,
            signal: controller.signal,
          });
          if (!controller.signal.aborted) {
            setResults(
              res.issues.filter((i) => !excludeIds.includes(i.id)),
            );
            setIsLoading(false);
          }
        } catch {
          if (!controller.signal.aborted) {
            setIsLoading(false);
          }
        }
      }, 300);
    },
    [excludeIds],
  );

  return (
    <CommandDialog
      open={open}
      onOpenChange={onOpenChange}
      title={title}
      description={description}
    >
      <Command shouldFilter={false}>
        <CommandInput
          placeholder="Search issues..."
          value={query}
          onValueChange={(v) => {
            setQuery(v);
            search(v);
          }}
        />
        <CommandList>
          {isLoading && (
            <div className="py-6 text-center text-sm text-muted-foreground">
              Searching...
            </div>
          )}
          {!isLoading && query.trim() && results.length === 0 && (
            <CommandEmpty>No issues found.</CommandEmpty>
          )}
          {!isLoading && !query.trim() && (
            <div className="py-6 text-center text-sm text-muted-foreground">
              Type to search issues
            </div>
          )}
          {results.length > 0 && (
            <CommandGroup>
              {results.map((issue) => (
                <CommandItem
                  key={issue.id}
                  value={issue.id}
                  onSelect={() => {
                    onSelect(issue);
                    onOpenChange(false);
                  }}
                >
                  <StatusIcon status={issue.status} className="h-3.5 w-3.5 shrink-0" />
                  <span className="text-muted-foreground shrink-0">{issue.identifier}</span>
                  <span className="truncate">{issue.title}</span>
                </CommandItem>
              ))}
            </CommandGroup>
          )}
        </CommandList>
      </Command>
    </CommandDialog>
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
  const router = useNavigation();
  const user = useAuthStore((s) => s.user);
  const userId = useAuthStore((s) => s.user?.id);
  const workspace = useWorkspaceStore((s) => s.workspace);

  // Issue navigation — read from TQ list cache
  const wsId = useWorkspaceId();
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const currentMemberRole = members.find((m) => m.user_id === user?.id)?.role;
  const { data: allIssues = [] } = useQuery(issueListOptions(wsId));
  const currentIndex = allIssues.findIndex((i) => i.id === id);
  const prevIssue = currentIndex > 0 ? allIssues[currentIndex - 1] : null;
  const nextIssue = currentIndex < allIssues.length - 1 ? allIssues[currentIndex + 1] : null;
  const { getActorName } = useActorName();
  const { uploadWithToast } = useFileUpload(api);
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
  const [highlightedId, setHighlightedId] = useState<string | null>(null);
  const didHighlightRef = useRef<string | null>(null);
  const [parentPickerOpen, setParentPickerOpen] = useState(false);
  const [childPickerOpen, setChildPickerOpen] = useState(false);

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
      recordVisit(issue.id);
    }
  }, [issue?.id]); // eslint-disable-line react-hooks/exhaustive-deps

  // Custom hooks — encapsulate timeline, reactions, subscribers
  const {
    timeline, loading: timelineLoading, submitComment, submitReply,
    editComment, deleteComment, toggleReaction: handleToggleReaction,
  } = useIssueTimeline(id, user?.id);

  const {
    reactions: issueReactions, loading: reactionsLoading,
    toggleReaction: handleToggleIssueReaction,
  } = useIssueReactions(id, user?.id);

  const {
    subscribers, loading: subscribersLoading, isSubscribed, toggleSubscribe: handleToggleSubscribe, toggleSubscriber,
  } = useIssueSubscribers(id, user?.id);

  // Token usage
  const { data: usage } = useQuery(issueUsageOptions(id));

  // Pinned state
  const { data: pinnedItems = [] } = useQuery({
    ...pinListOptions(wsId, userId ?? ""),
    enabled: !!userId,
  });
  const isPinned = pinnedItems.some((p) => p.item_type === "issue" && p.item_id === id);
  const createPin = useCreatePin();
  const deletePin = useDeletePin();

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

  const loading = issueLoading;

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

  // Issue field updates via TQ mutation (optimistic update + rollback in mutation hook)
  const updateIssueMutation = useUpdateIssue();
  const handleUpdateField = useCallback(
    (updates: Partial<UpdateIssueRequest>) => {
      if (!issue) return;
      updateIssueMutation.mutate(
        { id, ...updates },
        { onError: () => toast.error("Failed to update issue") },
      );
    },
    [issue, id, updateIssueMutation],
  );

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

  const deleteIssueMutation = useDeleteIssue();
  const handleDelete = async () => {
    setDeleting(true);
    try {
      await deleteIssueMutation.mutateAsync(issue!.id);
      toast.success("Issue deleted");
      if (onDelete) onDelete();
      else router.push("/issues");
    } catch {
      toast.error("Failed to delete issue");
      setDeleting(false);
    }
  };

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
          <Button variant="outline" size="sm" onClick={() => router.push("/issues")}>
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
                <AppLink
                  href="/issues"
                  className="text-muted-foreground hover:text-foreground transition-colors truncate shrink-0"
                >
                  {workspace.name}
                </AppLink>
                <ChevronRight className="h-3 w-3 text-muted-foreground/50 shrink-0" />
              </>
            )}
            {parentIssue && (
              <>
                <AppLink
                  href={`/issues/${parentIssue.id}`}
                  className="text-muted-foreground hover:text-foreground transition-colors truncate shrink-0"
                >
                  {parentIssue.identifier}
                </AppLink>
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
            <Tooltip>
              <TooltipTrigger
                render={
                  <Button
                    variant="ghost"
                    size="icon-xs"
                    className={cn("text-muted-foreground", isPinned && "text-foreground")}
                    onClick={() => {
                      if (isPinned) {
                        deletePin.mutate({ itemType: "issue", itemId: issue.id });
                      } else {
                        createPin.mutate({ item_type: "issue", item_id: issue.id });
                      }
                    }}
                  >
                    {isPinned ? <PinOff /> : <Pin />}
                  </Button>
                }
              />
              <TooltipContent side="bottom">{isPinned ? "Unpin from sidebar" : "Pin to sidebar"}</TooltipContent>
            </Tooltip>
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

                {/* Create sub-issue */}
                <DropdownMenuItem onClick={() => {
                  useModalStore.getState().open("create-issue", {
                    parent_issue_id: issue.id,
                    parent_issue_identifier: issue.identifier,
                  });
                }}>
                  <Plus className="h-3.5 w-3.5" />
                  Create sub-issue
                </DropdownMenuItem>

                {/* Add as sub-issue of another issue */}
                <DropdownMenuItem onClick={() => setParentPickerOpen(true)}>
                  <ArrowUp className="h-3.5 w-3.5" />
                  Set parent issue...
                </DropdownMenuItem>

                {/* Add another issue as sub-issue */}
                <DropdownMenuItem onClick={() => setChildPickerOpen(true)}>
                  <ArrowDown className="h-3.5 w-3.5" />
                  Add sub-issue...
                </DropdownMenuItem>

                {/* Pin / Unpin */}
                <DropdownMenuItem onClick={() => {
                  if (isPinned) {
                    deletePin.mutate({ itemType: "issue", itemId: issue.id });
                  } else {
                    createPin.mutate({ item_type: "issue", item_id: issue.id });
                  }
                }}>
                  {isPinned ? <PinOff className="h-3.5 w-3.5" /> : <Pin className="h-3.5 w-3.5" />}
                  {isPinned ? "Unpin from sidebar" : "Pin to sidebar"}
                </DropdownMenuItem>

                {/* Copy link */}
                <DropdownMenuItem onClick={() => {
                  const url = router.getShareableUrl
                    ? router.getShareableUrl(router.pathname)
                    : window.location.href;
                  navigator.clipboard.writeText(url);
                  toast.success("Link copied");
                }}>
                  <Link2 className="h-3.5 w-3.5" />
                  Copy link
                </DropdownMenuItem>

                <DropdownMenuSeparator />

                {/* Delete */}
                <DropdownMenuItem
                  variant="destructive"
                  onClick={() => setDeleteDialogOpen(true)}
                >
                  <Trash2 className="h-3.5 w-3.5" />
                  Delete issue
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
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
          </div>

            {/* Delete confirmation dialog (controlled by state) */}
            <AlertDialog open={deleteDialogOpen} onOpenChange={setDeleteDialogOpen}>
              <AlertDialogContent>
                <AlertDialogHeader>
                  <AlertDialogTitle>Delete issue</AlertDialogTitle>
                  <AlertDialogDescription>
                    This will permanently delete this issue and all its comments. This action cannot be undone.
                  </AlertDialogDescription>
                </AlertDialogHeader>
                <AlertDialogFooter>
                  <AlertDialogCancel>Cancel</AlertDialogCancel>
                  <AlertDialogAction
                    onClick={handleDelete}
                    disabled={deleting}
                    className="bg-destructive text-white hover:bg-destructive/90"
                  >
                    {deleting ? "Deleting..." : "Delete"}
                  </AlertDialogAction>
                </AlertDialogFooter>
              </AlertDialogContent>
            </AlertDialog>

            {/* Set parent issue picker */}
            <IssuePickerDialog
              open={parentPickerOpen}
              onOpenChange={setParentPickerOpen}
              title="Set parent issue"
              description="Search for an issue to set as the parent of this issue"
              excludeIds={[id, ...childIssues.map((c) => c.id)]}
              onSelect={(selected) => {
                handleUpdateField({ parent_issue_id: selected.id });
                toast.success(`Set ${selected.identifier} as parent issue`);
              }}
            />

            {/* Add sub-issue picker */}
            <IssuePickerDialog
              open={childPickerOpen}
              onOpenChange={setChildPickerOpen}
              title="Add sub-issue"
              description="Search for an issue to add as a sub-issue"
              excludeIds={[id, ...(parentIssueId ? [parentIssueId] : []), ...childIssues.map((c) => c.id)]}
              onSelect={(selected) => {
                updateIssueMutation.mutate(
                  { id: selected.id, parent_issue_id: id },
                  { onError: () => toast.error("Failed to add sub-issue") },
                );
                toast.success(`Added ${selected.identifier} as sub-issue`);
              }}
            />
          </div>

        {/* Content — scrollable */}
        <div ref={scrollContainerRef} className="relative flex-1 overflow-y-auto">
        <div className="mx-auto w-full max-w-4xl px-8 py-8">
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

          {parentIssue && (
            <AppLink
              href={`/issues/${parentIssue.id}`}
              className="mt-2 inline-flex max-w-full items-center gap-1.5 text-xs text-muted-foreground hover:text-foreground transition-colors group/parent"
            >
              <span className="font-medium shrink-0">Sub-issue of</span>
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
              placeholder="Add description..."
              onUpdate={(md) => handleUpdateField({ description: md || undefined })}
              onUploadFile={handleDescriptionUpload}
              debounceMs={1500}
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
                  getActorName={getActorName}
                />
              )}
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
                onClick={() =>
                  useModalStore.getState().open("create-issue", {
                    parent_issue_id: issue.id,
                    parent_issue_identifier: issue.identifier,
                  })
                }
              >
                <Plus className="h-3.5 w-3.5" />
                <span>Add sub-issues</span>
              </button>
            </div>
          )}
          {childIssues.length > 0 && (() => {
            const doneCount = childIssues.filter((c) => c.status === "done").length;
            return (
              <div className="mt-10">
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
                    <span>Sub-issues</span>
                  </button>
                  <div className="inline-flex items-center gap-1.5 rounded-full bg-muted/60 px-2 py-0.5">
                    <ProgressRing done={doneCount} total={childIssues.length} size={11} />
                    <span className="text-[11px] text-muted-foreground tabular-nums font-medium">
                      {doneCount}/{childIssues.length}
                    </span>
                  </div>
                  <Tooltip>
                    <TooltipTrigger
                      render={
                        <button
                          type="button"
                          className="ml-auto inline-flex h-7 w-7 items-center justify-center rounded-md text-muted-foreground hover:bg-accent hover:text-foreground transition-colors"
                          onClick={() =>
                            useModalStore.getState().open("create-issue", {
                              parent_issue_id: issue.id,
                              parent_issue_identifier: issue.identifier,
                            })
                          }
                          aria-label="Add sub-issue"
                        >
                          <Plus className="h-4 w-4" />
                        </button>
                      }
                    />
                    <TooltipContent side="bottom">Add sub-issue</TooltipContent>
                  </Tooltip>
                </div>

                {/* List */}
                {!subIssuesCollapsed && (
                  <div className="overflow-hidden rounded-lg border bg-card/30 divide-y divide-border/60">
                    {childIssues.map((child) => {
                      const isDone =
                        child.status === "done" || child.status === "cancelled";
                      return (
                        <AppLink
                          key={child.id}
                          href={`/issues/${child.id}`}
                          className="flex items-center gap-2.5 px-3 py-2 hover:bg-accent/50 transition-colors group/row"
                        >
                          <StatusIcon
                            status={child.status}
                            className="h-[15px] w-[15px] shrink-0"
                          />
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
                          {child.assignee_type && child.assignee_id ? (
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
                          )}
                        </AppLink>
                      );
                    })}
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

            {/* Agent live output — sticky inside the Activity section so it
                stays pinned while scrolling through TaskRunHistory + comments. */}
            <AgentLiveCard issueId={id} />

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
                      {group.entries.map((entry, _idx) => {
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
        </div>
      </div>
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
        onResize={(size) => setSidebarOpen(size.inPixels > 0)}
      >
      {/* RIGHT: Properties sidebar */}
      <div className="overflow-y-auto border-l h-full">
        <div className="p-4 space-y-5">
          {/* Properties section */}
          <div>
            <button
              className={`flex w-full items-center gap-1 text-xs font-medium transition-colors mb-2 ${propertiesOpen ? "" : "text-muted-foreground hover:text-foreground"}`}
              onClick={() => setPropertiesOpen(!propertiesOpen)}
            >
              <ChevronRight className={`h-3.5 w-3.5 shrink-0 text-muted-foreground transition-transform ${propertiesOpen ? "rotate-90" : ""}`} />
              Properties
            </button>

            {propertiesOpen && <div className="space-y-0.5 pl-2">
              {/* Status */}
              <PropRow label="Status">
                <StatusPicker
                  status={issue.status}
                  onUpdate={handleUpdateField}
                  align="start"
                />
              </PropRow>

              {/* Priority */}
              <PropRow label="Priority">
                <PriorityPicker
                  priority={issue.priority}
                  onUpdate={handleUpdateField}
                  align="start"
                />
              </PropRow>

              {/* Assignee */}
              <PropRow label="Assignee">
                <AssigneePicker
                  assigneeType={issue.assignee_type}
                  assigneeId={issue.assignee_id}
                  onUpdate={handleUpdateField}
                  align="start"
                />
              </PropRow>

              {/* Due date */}
              <PropRow label="Due date">
                <DueDatePicker
                  dueDate={issue.due_date}
                  onUpdate={handleUpdateField}
                />
              </PropRow>

              {/* Project */}
              <PropRow label="Project">
                <ProjectPicker
                  projectId={issue.project_id}
                  onUpdate={handleUpdateField}
                />
              </PropRow>
            </div>}
          </div>

          {/* Parent issue */}
          {parentIssue && (
            <div>
              <div className="text-xs font-medium mb-2 flex items-center gap-1">
                <ChevronRight className="h-3.5 w-3.5 shrink-0 text-muted-foreground rotate-90" />
                Parent issue
              </div>
              <div className="pl-2">
                <AppLink
                  href={`/issues/${parentIssue.id}`}
                  className="flex items-center gap-1.5 rounded-md px-2 py-1.5 -mx-2 text-xs hover:bg-accent/50 transition-colors group"
                >
                  <StatusIcon status={parentIssue.status} className="h-3.5 w-3.5 shrink-0" />
                  <span className="text-muted-foreground shrink-0">{parentIssue.identifier}</span>
                  <span className="truncate group-hover:text-foreground">{parentIssue.title}</span>
                </AppLink>
              </div>
            </div>
          )}

          {/* Details section */}
          <div>
            <button
              className={`flex w-full items-center gap-1 text-xs font-medium transition-colors mb-2 ${detailsOpen ? "" : "text-muted-foreground hover:text-foreground"}`}
              onClick={() => setDetailsOpen(!detailsOpen)}
            >
              <ChevronRight className={`h-3.5 w-3.5 shrink-0 text-muted-foreground transition-transform ${detailsOpen ? "rotate-90" : ""}`} />
              Details
            </button>

            {detailsOpen && <div className="space-y-0.5 pl-2">
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
            </div>}
          </div>

          {/* Token usage */}
          {usage && usage.task_count > 0 && (
            <div>
              <div className="text-xs font-medium mb-2 flex items-center gap-1">
                <ChevronRight className="h-3.5 w-3.5 shrink-0 text-muted-foreground rotate-90" />
                Token usage
              </div>
              <div className="space-y-0.5 pl-2">
                <PropRow label="Input">
                  <span className="text-muted-foreground">{formatTokenCount(usage.total_input_tokens)}</span>
                </PropRow>
                <PropRow label="Output">
                  <span className="text-muted-foreground">{formatTokenCount(usage.total_output_tokens)}</span>
                </PropRow>
                {(usage.total_cache_read_tokens > 0 || usage.total_cache_write_tokens > 0) && (
                  <PropRow label="Cache">
                    <span className="text-muted-foreground">
                      {formatTokenCount(usage.total_cache_read_tokens)} read / {formatTokenCount(usage.total_cache_write_tokens)} write
                    </span>
                  </PropRow>
                )}
                <PropRow label="Runs">
                  <span className="text-muted-foreground">{usage.task_count}</span>
                </PropRow>
              </div>
            </div>
          )}

        </div>
      </div>
      </ResizablePanel>
    </ResizablePanelGroup>
  );
}
