"use client";

import { useState, useEffect, useCallback, useRef } from "react";
import { useDefaultLayout, usePanelRef } from "react-resizable-panels";
import Link from "next/link";
import { useRouter } from "next/navigation";
import {
  Bot,
  Calendar,
  Check,
  ChevronLeft,
  ChevronRight,
  Link2,
  MoreHorizontal,
  PanelRight,
  Trash2,
  UserMinus,
  Users,
  X,
} from "lucide-react";
import { toast } from "sonner";
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
import { Input } from "@/components/ui/input";
import { RichTextEditor } from "@/components/common/rich-text-editor";
import {
  Tooltip,
  TooltipTrigger,
  TooltipContent,
} from "@/components/ui/tooltip";
import { Popover, PopoverTrigger, PopoverContent } from "@/components/ui/popover";
import { Checkbox } from "@/components/ui/checkbox";
import { Command, CommandInput, CommandList, CommandEmpty, CommandGroup, CommandItem } from "@/components/ui/command";
import { Avatar, AvatarFallback, AvatarGroup, AvatarGroupCount } from "@/components/ui/avatar";
import { ActorAvatar } from "@/components/common/actor-avatar";
import type { Issue, Comment, IssueSubscriber, UpdateIssueRequest, IssueStatus, IssuePriority, TimelineEntry } from "@/shared/types";
import { ALL_STATUSES, STATUS_CONFIG, PRIORITY_ORDER, PRIORITY_CONFIG } from "@/features/issues/config";
import { StatusIcon, PriorityIcon, DueDatePicker } from "@/features/issues/components";
import { CommentCard } from "./comment-card";
import { CommentInput } from "./comment-input";
import { api } from "@/shared/api";
import { useAuthStore } from "@/features/auth";
import { useWorkspaceStore, useActorName } from "@/features/workspace";
import { useWSEvent } from "@/features/realtime";
import { useIssueStore } from "@/features/issues";
import type { CommentCreatedPayload, CommentUpdatedPayload, CommentDeletedPayload, SubscriberAddedPayload, SubscriberRemovedPayload, ActivityCreatedPayload } from "@/shared/types";
import { timeAgo } from "@/shared/utils";

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

function commentToTimelineEntry(c: Comment): TimelineEntry {
  return {
    type: "comment",
    id: c.id,
    actor_type: c.author_type,
    actor_id: c.author_id,
    content: c.content,
    parent_id: c.parent_id,
    created_at: c.created_at,
    updated_at: c.updated_at,
    comment_type: c.type,
  };
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
// Props
// ---------------------------------------------------------------------------

interface IssueDetailProps {
  issueId: string;
  onDelete?: () => void;
  defaultSidebarOpen?: boolean;
  layoutId?: string;
}

// ---------------------------------------------------------------------------
// IssueDetail
// ---------------------------------------------------------------------------

export function IssueDetail({ issueId, onDelete, defaultSidebarOpen = true, layoutId = "multica_issue_detail_layout" }: IssueDetailProps) {
  const id = issueId;
  const router = useRouter();
  const user = useAuthStore((s) => s.user);
  const workspace = useWorkspaceStore((s) => s.workspace);
  const members = useWorkspaceStore((s) => s.members);
  const agents = useWorkspaceStore((s) => s.agents);

  // Issue navigation
  const allIssues = useIssueStore((s) => s.issues);
  const currentIndex = allIssues.findIndex((i) => i.id === id);
  const prevIssue = currentIndex > 0 ? allIssues[currentIndex - 1] : null;
  const nextIssue = currentIndex < allIssues.length - 1 ? allIssues[currentIndex + 1] : null;
  const { getActorName, getActorInitials } = useActorName();
  const { defaultLayout, onLayoutChanged } = useDefaultLayout({
    id: layoutId,
  });
  const sidebarRef = usePanelRef();
  const [sidebarOpen, setSidebarOpen] = useState(defaultSidebarOpen);
  const [issue, setIssue] = useState<Issue | null>(null);
  const [timeline, setTimeline] = useState<TimelineEntry[]>([]);
  const [subscribers, setSubscribers] = useState<IssueSubscriber[]>([]);
  const [loading, setLoading] = useState(true);
  const [submitting, setSubmitting] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [titleDraft, setTitleDraft] = useState("");
  const titleFocusedRef = useRef(false);
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);
  const [propertiesOpen, setPropertiesOpen] = useState(true);
  const [detailsOpen, setDetailsOpen] = useState(true);

  // Watch the global issue store for real-time updates from other users/agents
  const storeIssue = useIssueStore((s) => s.issues.find((i) => i.id === id));

  const wasLoadedRef = useRef(false);

  useEffect(() => {
    if (storeIssue) {
      wasLoadedRef.current = true;
      setIssue(storeIssue);
      if (!titleFocusedRef.current) {
        setTitleDraft(storeIssue.title);
      }
    } else if (wasLoadedRef.current && !loading) {
      // Issue was in the store but is now gone (deleted by another user)
      setIssue(null);
    }
  }, [storeIssue, loading]);

  useEffect(() => {
    wasLoadedRef.current = false;
    setIssue(null);
    setTitleDraft("");
    setTimeline([]);
    setSubscribers([]);
    setLoading(true);
    Promise.all([api.getIssue(id), api.listTimeline(id), api.listIssueSubscribers(id)])
      .then(([iss, entries, subs]) => {
        setIssue(iss);
        setTitleDraft(iss.title);
        setTimeline(entries);
        setSubscribers(subs);
      })
      .catch(console.error)
      .finally(() => setLoading(false));
  }, [id]);

  const handleSubmitComment = async (content: string) => {
    if (!content.trim() || submitting || !user) return;
    const tempId = "temp-" + Date.now();
    const tempEntry: TimelineEntry = {
      type: "comment",
      id: tempId,
      actor_type: "member",
      actor_id: user.id,
      content,
      parent_id: null,
      created_at: new Date().toISOString(),
      updated_at: new Date().toISOString(),
      comment_type: "comment",
    };
    setTimeline((prev) => [...prev, tempEntry]);
    setSubmitting(true);
    try {
      const comment = await api.createComment(id, content);
      setTimeline((prev) => prev.map((e) => (e.id === tempId ? commentToTimelineEntry(comment) : e)));
    } catch {
      setTimeline((prev) => prev.filter((e) => e.id !== tempId));
      toast.error("Failed to send comment");
    } finally {
      setSubmitting(false);
    }
  };

  const handleSubmitReply = async (parentId: string, content: string) => {
    if (!content.trim() || !user) return;
    try {
      const comment = await api.createComment(id, content, "comment", parentId);
      setTimeline((prev) => {
        if (prev.some((e) => e.id === comment.id)) return prev;
        return [...prev, commentToTimelineEntry(comment)];
      });
    } catch {
      toast.error("Failed to send reply");
    }
  };

  const handleEditComment = async (commentId: string, content: string) => {
    try {
      const updated = await api.updateComment(commentId, content);
      setTimeline((prev) => prev.map((e) => (e.id === updated.id ? commentToTimelineEntry(updated) : e)));
    } catch {
      toast.error("Failed to update comment");
    }
  };

  const handleDeleteComment = async (commentId: string) => {
    try {
      await api.deleteComment(commentId);
      setTimeline((prev) => {
        const idsToRemove = new Set<string>([commentId]);
        // Recursively collect all descendant IDs
        let added = true;
        while (added) {
          added = false;
          for (const e of prev) {
            if (e.parent_id && idsToRemove.has(e.parent_id) && !idsToRemove.has(e.id)) {
              idsToRemove.add(e.id);
              added = true;
            }
          }
        }
        return prev.filter((e) => !idsToRemove.has(e.id));
      });
    } catch {
      toast.error("Failed to delete comment");
    }
  };

  const handleUpdateField = useCallback(
    (updates: Partial<UpdateIssueRequest>) => {
      if (!issue) return;
      const prev = issue;
      setIssue((curr) => (curr ? ({ ...curr, ...updates } as Issue) : curr));
      api.updateIssue(id, updates).catch(() => {
        setIssue(prev);
        toast.error("Failed to update issue");
      });
    },
    [issue, id],
  );

  const handleDelete = async () => {
    setDeleting(true);
    try {
      await api.deleteIssue(issue!.id);
      toast.success("Issue deleted");
      if (onDelete) onDelete();
      else router.push("/issues");
    } catch {
      toast.error("Failed to delete issue");
      setDeleting(false);
    }
  };

  // Subscriber state
  const isSubscribed = subscribers.some(
    (s) => s.user_type === "member" && s.user_id === user?.id
  );

  const toggleSubscriber = async (userId: string, userType: "member" | "agent", currentlySubscribed: boolean) => {
    if (!issue) return;
    try {
      if (currentlySubscribed) {
        await api.unsubscribeFromIssue(id, userId, userType);
        setSubscribers((prev) => prev.filter((s) => !(s.user_id === userId && s.user_type === userType)));
      } else {
        await api.subscribeToIssue(id, userId, userType);
        setSubscribers((prev) => {
          // Deduplicate: WS event may have already added this subscriber
          if (prev.some((s) => s.user_id === userId && s.user_type === userType)) return prev;
          return [...prev, { issue_id: id, user_type: userType, user_id: userId, reason: "manual" as const, created_at: new Date().toISOString() }];
        });
      }
    } catch {
      toast.error("Failed to update subscriber");
    }
  };

  const handleToggleSubscribe = () => {
    if (user) toggleSubscriber(user.id, "member", isSubscribed);
  };

  // Real-time comment updates
  useWSEvent(
    "comment:created",
    useCallback((payload: unknown) => {
      const { comment } = payload as CommentCreatedPayload;
      if (comment.issue_id !== id) return;
      // Skip own comments — already added locally via API response
      if (comment.author_type === "member" && comment.author_id === user?.id) return;
      setTimeline((prev) => {
        if (prev.some((e) => e.id === comment.id)) return prev;
        return [...prev, commentToTimelineEntry(comment)];
      });
    }, [id, user?.id]),
  );

  useWSEvent(
    "comment:updated",
    useCallback((payload: unknown) => {
      const { comment } = payload as CommentUpdatedPayload;
      if (comment.issue_id === id) {
        setTimeline((prev) => prev.map((e) => (e.id === comment.id ? commentToTimelineEntry(comment) : e)));
      }
    }, [id]),
  );

  useWSEvent(
    "comment:deleted",
    useCallback((payload: unknown) => {
      const { comment_id, issue_id } = payload as CommentDeletedPayload;
      if (issue_id === id) {
        setTimeline((prev) => {
          const idsToRemove = new Set<string>([comment_id]);
          let added = true;
          while (added) {
            added = false;
            for (const e of prev) {
              if (e.parent_id && idsToRemove.has(e.parent_id) && !idsToRemove.has(e.id)) {
                idsToRemove.add(e.id);
                added = true;
              }
            }
          }
          return prev.filter((e) => !idsToRemove.has(e.id));
        });
      }
    }, [id]),
  );

  useWSEvent(
    "activity:created",
    useCallback((payload: unknown) => {
      const p = payload as ActivityCreatedPayload;
      if (p.issue_id !== id) return;
      const entry = p.entry;
      if (!entry || !entry.id) return;
      setTimeline((prev) => {
        if (prev.some((e) => e.id === entry.id)) return prev;
        return [...prev, entry];
      });
    }, [id]),
  );

  // Real-time subscriber updates
  useWSEvent(
    "subscriber:added",
    useCallback((payload: unknown) => {
      const p = payload as SubscriberAddedPayload;
      if (p.issue_id !== id) return;
      setSubscribers((prev) => {
        if (prev.some((s) => s.user_id === p.user_id && s.user_type === p.user_type)) return prev;
        return [...prev, {
          issue_id: p.issue_id,
          user_type: p.user_type as "member" | "agent",
          user_id: p.user_id,
          reason: p.reason as IssueSubscriber["reason"],
          created_at: new Date().toISOString(),
        }];
      });
    }, [id]),
  );

  useWSEvent(
    "subscriber:removed",
    useCallback((payload: unknown) => {
      const p = payload as SubscriberRemovedPayload;
      if (p.issue_id !== id) return;
      setSubscribers((prev) => prev.filter((s) => !(s.user_id === p.user_id && s.user_type === p.user_type)));
    }, [id]),
  );

  if (loading) {
    return (
      <div className="flex flex-1 min-h-0 items-center justify-center text-sm text-muted-foreground">
        Loading...
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
                <Link
                  href="/issues"
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
                        <PriorityIcon priority={p} />
                        {PRIORITY_CONFIG[p].label}
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
                        <div className="inline-flex size-4 shrink-0 items-center justify-center rounded-full bg-muted text-[8px] font-medium text-muted-foreground">
                          {getActorInitials("member", m.user_id)}
                        </div>
                        {m.name}
                        {issue.assignee_type === "member" && issue.assignee_id === m.user_id && <span className="ml-auto text-xs text-muted-foreground">✓</span>}
                      </DropdownMenuItem>
                    ))}
                    {agents.map((a) => (
                      <DropdownMenuItem
                        key={a.id}
                        onClick={() => handleUpdateField({ assignee_type: "agent", assignee_id: a.id })}
                      >
                        <div className="inline-flex size-4 shrink-0 items-center justify-center rounded-full bg-info/10 text-info">
                          <Bot className="size-2.5" />
                        </div>
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
          </div>

        {/* Content — scrollable */}
        <div className="flex-1 overflow-y-auto">
        <div className="mx-auto w-full max-w-4xl px-8 py-8">
          <input
            value={titleDraft}
            onChange={(e) => setTitleDraft(e.target.value)}
            onFocus={() => { titleFocusedRef.current = true; }}
            onBlur={() => {
              titleFocusedRef.current = false;
              const trimmed = titleDraft.trim();
              if (trimmed && trimmed !== issue.title) handleUpdateField({ title: trimmed });
              else setTitleDraft(issue.title);
            }}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                e.preventDefault();
                (e.target as HTMLInputElement).blur();
              } else if (e.key === "Escape") {
                setTitleDraft(issue.title);
                (e.target as HTMLInputElement).blur();
              }
            }}
            className="w-full bg-transparent text-2xl font-bold leading-snug tracking-tight outline-none placeholder:text-muted-foreground"
          />

          <RichTextEditor
            defaultValue={issue.description || ""}
            placeholder="Add description..."
            onUpdate={(md) => handleUpdateField({ description: md || undefined })}
            debounceMs={1500}
            className="mt-5"
          />

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
                          <Avatar key={`${sub.user_type}-${sub.user_id}`} size="sm">
                            <AvatarFallback>{getActorInitials(sub.user_type, sub.user_id)}</AvatarFallback>
                          </Avatar>
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
                        {agents.length > 0 && (
                          <CommandGroup heading="Agents">
                            {agents.map((a) => {
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
              </div>
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
                      <CommentCard
                        key={entry.id}
                        entry={entry}
                        allReplies={repliesByParent}
                        currentUserId={user?.id}
                        onReply={handleSubmitReply}
                        onEdit={handleEditComment}
                        onDelete={handleDeleteComment}
                      />
                    );
                  }

                  return (
                    <div key={group.entries[0]!.id} className="px-4 flex flex-col gap-3">
                      {group.entries.map((entry, idx) => {
                        const details = (entry.details ?? {}) as Record<string, string>;
                        const isStatusChange = entry.action === "status_changed";
                        const isPriorityChange = entry.action === "priority_changed";
                        const isDueDateChange = entry.action === "due_date_changed";
                        const isLast = idx === group.entries.length - 1;

                        let leadIcon: React.ReactNode;
                        if (isStatusChange && details.to) {
                          leadIcon = <StatusIcon status={details.to as IssueStatus} className="h-3.5 w-3.5 shrink-0" />;
                        } else if (isPriorityChange && details.to) {
                          leadIcon = <PriorityIcon priority={details.to as IssuePriority} className="h-3.5 w-3.5 shrink-0" />;
                        } else if (isDueDateChange) {
                          leadIcon = <Calendar className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />;
                        } else {
                          leadIcon = <ActorAvatar actorType={entry.actor_type} actorId={entry.actor_id} size={14} />;
                        }

                        return (
                          <div key={entry.id} className="relative flex text-xs text-muted-foreground">
                            <div className="mr-2.5 flex w-3.5 shrink-0 justify-center">
                              <div className="flex h-5 items-center">{leadIcon}</div>
                            </div>
                            {!isLast && (
                              <div className="absolute left-[7px] top-5 h-3 w-px bg-border" />
                            )}
                            <div className="flex flex-1 items-baseline gap-1">
                              <span className="font-medium">{getActorName(entry.actor_type, entry.actor_id)}</span>
                              <span>{formatActivity(entry, getActorName)}</span>
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
              <CommentInput onSubmit={handleSubmitComment} />
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
                <DropdownMenu>
                  <DropdownMenuTrigger className="flex items-center gap-1.5 cursor-pointer rounded px-1 -mx-1 hover:bg-accent/30 transition-colors overflow-hidden">
                    <StatusIcon status={issue.status} className="h-3.5 w-3.5 shrink-0" />
                    <span className="truncate">{STATUS_CONFIG[issue.status].label}</span>
                  </DropdownMenuTrigger>
                  <DropdownMenuContent align="start" className="w-44">
                    {ALL_STATUSES.map((s) => (
                      <DropdownMenuItem key={s} onClick={() => handleUpdateField({ status: s })}>
                        <StatusIcon status={s} className="h-3.5 w-3.5" />
                        {STATUS_CONFIG[s].label}
                        {s === issue.status && <Check className="ml-auto h-3.5 w-3.5" />}
                      </DropdownMenuItem>
                    ))}
                  </DropdownMenuContent>
                </DropdownMenu>
              </PropRow>

              {/* Priority */}
              <PropRow label="Priority">
                <DropdownMenu>
                  <DropdownMenuTrigger className="flex items-center gap-1.5 cursor-pointer rounded px-1 -mx-1 hover:bg-accent/30 transition-colors overflow-hidden">
                    <PriorityIcon priority={issue.priority} className="shrink-0" />
                    <span className="truncate">{PRIORITY_CONFIG[issue.priority].label}</span>
                  </DropdownMenuTrigger>
                  <DropdownMenuContent align="start" className="w-44">
                    {PRIORITY_ORDER.map((p) => (
                      <DropdownMenuItem key={p} onClick={() => handleUpdateField({ priority: p })}>
                        <PriorityIcon priority={p} />
                        {PRIORITY_CONFIG[p].label}
                        {p === issue.priority && <Check className="ml-auto h-3.5 w-3.5" />}
                      </DropdownMenuItem>
                    ))}
                  </DropdownMenuContent>
                </DropdownMenu>
              </PropRow>

              {/* Assignee */}
              <PropRow label="Assignee">
                <DropdownMenu>
                  <DropdownMenuTrigger className="flex items-center gap-1.5 cursor-pointer rounded px-1 -mx-1 hover:bg-accent/30 transition-colors overflow-hidden">
                    {issue.assignee_type && issue.assignee_id ? (
                      <>
                        <ActorAvatar
                          actorType={issue.assignee_type}
                          actorId={issue.assignee_id}
                          size={18}
                        />
                        <span className="truncate">{getActorName(issue.assignee_type, issue.assignee_id)}</span>
                      </>
                    ) : (
                      <span className="text-muted-foreground">Unassigned</span>
                    )}
                  </DropdownMenuTrigger>
                  <DropdownMenuContent align="start" className="w-52">
                    <DropdownMenuItem onClick={() => handleUpdateField({ assignee_type: null, assignee_id: null })}>
                      <UserMinus className="h-3.5 w-3.5 text-muted-foreground" />
                      Unassigned
                    </DropdownMenuItem>
                    {members.length > 0 && (
                      <>
                        <DropdownMenuSeparator />
                        <DropdownMenuGroup>
                          <DropdownMenuLabel>Members</DropdownMenuLabel>
                          {members.map((m) => (
                            <DropdownMenuItem key={m.user_id} onClick={() => handleUpdateField({ assignee_type: "member", assignee_id: m.user_id })}>
                              <div className="inline-flex size-4 shrink-0 items-center justify-center rounded-full bg-muted text-[8px] font-medium text-muted-foreground">
                                {getActorInitials("member", m.user_id)}
                              </div>
                              {m.name}
                            </DropdownMenuItem>
                          ))}
                        </DropdownMenuGroup>
                      </>
                    )}
                    {agents.length > 0 && (
                      <>
                        <DropdownMenuSeparator />
                        <DropdownMenuGroup>
                          <DropdownMenuLabel>Agents</DropdownMenuLabel>
                          {agents.map((a) => (
                            <DropdownMenuItem key={a.id} onClick={() => handleUpdateField({ assignee_type: "agent", assignee_id: a.id })}>
                              <div className="inline-flex size-4 shrink-0 items-center justify-center rounded-full bg-info/10 text-info">
                                <Bot className="size-2.5" />
                              </div>
                              {a.name}
                            </DropdownMenuItem>
                          ))}
                        </DropdownMenuGroup>
                      </>
                    )}
                  </DropdownMenuContent>
                </DropdownMenu>
              </PropRow>

              {/* Due date */}
              <PropRow label="Due date">
                <DueDatePicker
                  dueDate={issue.due_date}
                  onUpdate={handleUpdateField}
                />
              </PropRow>
            </div>}
          </div>

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

        </div>
      </div>
      </ResizablePanel>
    </ResizablePanelGroup>
  );
}
