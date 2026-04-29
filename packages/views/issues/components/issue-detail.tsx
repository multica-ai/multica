"use client";

import { useState, useEffect, useCallback, useRef } from "react";
import { useDefaultLayout, usePanelRef } from "react-resizable-panels";
import { AppLink } from "../../navigation";
import { useNavigation } from "../../navigation";
import {
  Calendar,
  ChevronDown,
  ChevronLeft,
  ChevronRight,
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
import type { IssueStatus, IssuePriority, TimelineEntry } from "@multica/core/types";
import { STATUS_CONFIG, PRIORITY_CONFIG } from "@multica/core/issues/config";
import { StatusIcon, PriorityIcon, StatusPicker, PriorityPicker, DueDatePicker, AssigneePicker, LabelPicker } from ".";
import { IssueActionsDropdown, useIssueActions } from "../actions";
import { ProjectPicker } from "../../projects/components/project-picker";
import { CommentCard } from "./comment-card";
import { CommentInput } from "./comment-input";
import { AgentLiveCard, TaskRunHistory } from "./agent-live-card";
import { useQuery } from "@tanstack/react-query";
import { useAuthStore } from "@multica/core/auth";
import { useCurrentWorkspace, useWorkspacePaths } from "@multica/core/paths";
import { useActorName } from "@multica/core/workspace/hooks";
import { useWorkspaceId } from "@multica/core/hooks";
import { issueListOptions, issueDetailOptions, childIssuesOptions, issueUsageOptions } from "@multica/core/issues/queries";
import { memberListOptions, agentListOptions } from "@multica/core/workspace/queries";
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
  const workspace = useCurrentWorkspace();
  const paths = useWorkspacePaths();

  // Issue navigation — read from TQ list cache
  const wsId = useWorkspaceId();
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: allIssues = [] } = useQuery(issueListOptions(wsId));
  const { getActorName } = useActorName();
  const { uploadWithToast } = useFileUpload(api);
  const { defaultLayout, onLayoutChanged } = useDefaultLayout({
    id: layoutId,
  });
  const sidebarRef = usePanelRef();
  const isMobile = useIsMobile();
  const [sidebarOpen, setSidebarOpen] = useState(defaultSidebarOpen);

  useEffect(() => {
    if (isMobile) {
      setSidebarOpen(false);
      sidebarRef.current?.collapse();
    }
  }, [isMobile]);
  const [propertiesOpen, setPropertiesOpen] = useState(true);
  const [detailsOpen, setDetailsOpen] = useState(true);
  const [parentIssueOpen, setParentIssueOpen] = useState(true);
  const [tokenUsageOpen, setTokenUsageOpen] = useState(true);
  const scrollContainerRef = useRef<HTMLDivElement>(null);
  const [showGoToEnd, setShowGoToEnd] = useState(false);
  const [highlightedId, setHighlightedId] = useState<string | null>(null);
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
      recordVisit(issue.id);
    }
  }, [issue?.id]); // eslint-disable-line react-hooks/exhaustive-deps

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
    timeline, submitComment, submitReply,
    editComment, deleteComment, toggleReaction: handleToggleReaction,
  } = useIssueTimeline(id, user?.id);

  const {
    reactions: issueReactions,
    toggleReaction: handleToggleIssueReaction,
  } = useIssueReactions(id, user?.id);

  const {
    subscribers, isSubscribed, toggleSubscribe: handleToggleSubscribe, toggleSubscriber,
  } = useIssueSubscribers(id, user?.id);

  // Token usage
  const { data: usage } = useQuery(issueUsageOptions(id));

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
        <p>This issue does not exist or has been deleted in this workspace.</p>
        {!onDelete && (
          <Button variant="outline" size="sm" onClick={() => router.push(paths.issues())}>
            <ChevronLeft className="mr-1 h-3.5 w-3.5" />
            Back to Issues
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
          Properties
          <ChevronRight className={`!size-3 shrink-0 stroke-[2.5] text-muted-foreground transition-transform ${propertiesOpen ? "rotate-90" : ""}`} />
        </button>
        {propertiesOpen && <div className="space-y-0.5 pl-2">
          <PropRow label="Status">
            <StatusPicker status={issue.status} onUpdate={handleUpdateField} align="start" />
          </PropRow>
          <PropRow label="Priority">
            <PriorityPicker priority={issue.priority} onUpdate={handleUpdateField} align="start" />
          </PropRow>
          <PropRow label="Assignee">
            <AssigneePicker assigneeType={issue.assignee_type} assigneeId={issue.assignee_id} onUpdate={handleUpdateField} align="start" />
          </PropRow>
          <PropRow label="Due date">
            <DueDatePicker dueDate={issue.due_date} onUpdate={handleUpdateField} />
          </PropRow>
          <PropRow label="Project">
            <ProjectPicker projectId={issue.project_id} onUpdate={handleUpdateField} />
          </PropRow>
          <PropRow label="Labels">
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
            Parent issue
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
          Details
          <ChevronRight className={`!size-3 shrink-0 stroke-[2.5] text-muted-foreground transition-transform ${detailsOpen ? "rotate-90" : ""}`} />
        </button>
        {detailsOpen && <div className="space-y-0.5 pl-2">
          <PropRow label="Created by">
            <ActorAvatar actorType={issue.creator_type} actorId={issue.creator_id} size={18} enableHoverCard />
            <span className="cursor-pointer truncate">{getActorName(issue.creator_type, issue.creator_id)}</span>
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
          <button
            className={`flex w-full items-center gap-1 rounded-md px-2 py-1 text-xs font-medium transition-colors mb-2 hover:bg-accent/70 ${tokenUsageOpen ? "" : "text-muted-foreground hover:text-foreground"}`}
            onClick={() => setTokenUsageOpen(!tokenUsageOpen)}
          >
            Token usage
            <ChevronRight className={`!size-3 shrink-0 stroke-[2.5] text-muted-foreground transition-transform ${tokenUsageOpen ? "rotate-90" : ""}`} />
          </button>
          {tokenUsageOpen && <div className="space-y-0.5 pl-2">
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
          </div>}
        </div>
      )}
    </div>
  );

  return (
    <ResizablePanelGroup orientation="horizontal" className="flex-1 min-h-0" defaultLayout={defaultLayout} onLayoutChanged={onLayoutChanged}>
      <ResizablePanel id="content" minSize="50%">
      <div className="flex h-full flex-col">
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
            <span className="shrink-0 text-muted-foreground">
              {issue.identifier}
            </span>
            <span className="truncate font-medium text-foreground">
              {issue.title}
            </span>
          </div>
          <div className="flex items-center gap-1 shrink-0">
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
              <TooltipContent side="bottom">{actions.isPinned ? "Unpin from sidebar" : "Pin to sidebar"}</TooltipContent>
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
                    onClick={() => {
                      if (isMobile) {
                        setSidebarOpen(!sidebarOpen);
                      } else {
                        const panel = sidebarRef.current;
                        if (!panel) return;
                        if (panel.isCollapsed()) panel.expand();
                        else panel.collapse();
                      }
                    }}
                  >
                    <PanelRight />
                  </Button>
                }
              />
              <TooltipContent side="bottom">Toggle sidebar</TooltipContent>
            </Tooltip>
          </div>
        </PageHeader>

        {/* Content — scrollable */}
        <div
          ref={scrollContainerRef}
          className="relative flex-1 overflow-y-auto"
          onScroll={() => {
            const el = scrollContainerRef.current;
            if (!el) return;
            setShowGoToEnd(el.scrollHeight - el.scrollTop - el.clientHeight > 200);
          }}
        >
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
              href={paths.issueDetail(parentIssue.id)}
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
              onUpdate={(md) => handleUpdateField({ description: md })}
              onUploadFile={handleDescriptionUpload}
              debounceMs={1500}
              currentIssueId={id}
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
                          href={paths.issueDetail(child.id)}
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
                              enableHoverCard
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

            {/* Agent live output — sticky inside the Activity section so it
                stays pinned while scrolling through TaskRunHistory + comments.
                Keyed by issue id so switching issues remounts the card and
                clears any in-flight task state from the previous issue. */}
            <AgentLiveCard key={id} issueId={id} />

            {/* Agent execution history */}
            <div className="mt-3">
              <TaskRunHistory key={id} issueId={id} />
            </div>

            {/* Timeline entries */}
            <div className="mt-4 flex flex-col gap-3">
              {(() => {
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

          {showGoToEnd && (
            <div className="sticky bottom-4 z-10 flex justify-center pointer-events-none">
              <button
                onClick={() => {
                  const el = scrollContainerRef.current;
                  if (el) {
                    el.scrollTo({ top: el.scrollHeight, behavior: "smooth" });
                  }
                }}
                className="pointer-events-auto flex items-center gap-1 rounded-full bg-background border shadow-md px-3 py-1.5 text-xs text-muted-foreground hover:text-foreground transition-colors"
              >
                <ArrowDown className="h-3 w-3" />
                Go to end
              </button>
            </div>
          )}
        </div>
      </div>
      </ResizablePanel>
      {!isMobile && <ResizableHandle />}
      {!isMobile && (
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
      <div className="overflow-y-auto border-l h-full">
        <div className="p-4">
          {sidebarContent}
        </div>
      </div>
      </ResizablePanel>
      )}
      {isMobile && (
        <Sheet open={sidebarOpen} onOpenChange={setSidebarOpen}>
          <SheetContent side="right" showCloseButton={false} className="w-[320px] overflow-y-auto p-4">
            {sidebarContent}
          </SheetContent>
        </Sheet>
      )}
    </ResizablePanelGroup>
  );
}
