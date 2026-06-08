"use client";

import { useState, useEffect, useCallback, useMemo, useRef } from "react";
import { useDefaultLayout } from "react-resizable-panels";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { useModalStore } from "@multica/core/modals";
import { useIssueDraftStore } from "@multica/core/issues/stores/draft-store";
import {
  issueAttentionListOptions,
  isIssueApprovalRequired,
} from "@multica/core/issues/queries";
import {
  inboxListOptions,
  deduplicateInboxItems,
  useInboxUnreadCount,
} from "@multica/core/inbox/queries";
import {
  useMarkInboxRead,
  useArchiveInbox,
  useMarkAllInboxRead,
  useArchiveAllInbox,
  useArchiveAllReadInbox,
  useArchiveCompletedInbox,
} from "@multica/core/inbox/mutations";

import { IssueDetail } from "../../issues/components/issue-detail";
import { ErrorBoundary } from "@multica/ui/components/common/error-boundary";
import { AppLink, useNavigation } from "../../navigation";
import { toast } from "sonner";
import MoreHorizontal from "lucide-react/dist/esm/icons/more-horizontal.mjs";
import Inbox from "lucide-react/dist/esm/icons/inbox.mjs";
import CheckCheck from "lucide-react/dist/esm/icons/check-check.mjs";
import Archive from "lucide-react/dist/esm/icons/archive.mjs";
import BookCheck from "lucide-react/dist/esm/icons/book-check.mjs";
import ListChecks from "lucide-react/dist/esm/icons/list-checks.mjs";
import ArrowLeft from "lucide-react/dist/esm/icons/arrow-left.mjs";
import ListFilter from "lucide-react/dist/esm/icons/list-filter.mjs";
import type { InboxItem, Issue } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import {
  ResizablePanelGroup,
  ResizablePanel,
  ResizableHandle,
} from "@multica/ui/components/ui/resizable";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
} from "@multica/ui/components/ui/dropdown-menu";
import { useIsMobile } from "@multica/ui/hooks/use-mobile";
import { PageHeader } from "../../layout/page-header";
import { InboxListItem, useTimeAgo } from "./inbox-list-item";
import { useTypeLabels } from "./inbox-detail-label";
import { getInboxDisplayTitle } from "./inbox-display";
import { useT } from "../../i18n";

const APPROVAL_GATE_INBOX_PREFIX = "approval-gate:";

function isApprovalGateInboxItem(item: InboxItem): boolean {
  return item.id.startsWith(APPROVAL_GATE_INBOX_PREFIX);
}

function issueCreatorActorType(issue: Issue): InboxItem["actor_type"] {
  return issue.creator_type === "member" || issue.creator_type === "agent"
    ? issue.creator_type
    : "system";
}

function issueToApprovalGateInboxItem(issue: Issue): InboxItem {
  const actorType = issueCreatorActorType(issue);
  return {
    id: `${APPROVAL_GATE_INBOX_PREFIX}${issue.id}`,
    workspace_id: issue.workspace_id,
    recipient_type: "member",
    recipient_id: issue.creator_id,
    actor_type: actorType,
    actor_id: actorType === "system" ? null : issue.creator_id,
    type: "review_requested",
    severity: "action_required",
    issue_id: issue.id,
    title: issue.title,
    body: issue.description,
    issue_status: issue.status,
    read: false,
    archived: false,
    created_at: issue.updated_at || issue.created_at,
    details: { identifier: issue.identifier },
  };
}

export function InboxPage() {
  const { t } = useT("inbox");
  const { searchParams, replace } = useNavigation();
  const urlIssue = searchParams.get("issue") ?? "";
  const approvalOnly = searchParams.get("filter") === "approvals";
  const wsPaths = useWorkspacePaths();

  const [selectedKey, setSelectedKeyState] = useState(() => urlIssue);

  // Sync from URL when searchParams change (e.g. navigation)
  useEffect(() => {
    setSelectedKeyState(urlIssue);
  }, [urlIssue]);

  const wsId = useWorkspaceId();
  const { data: rawItems = [], isLoading: loading } = useQuery(inboxListOptions(wsId));
  const items = useMemo(() => deduplicateInboxItems(rawItems), [rawItems]);
  const { data: approvalIssues = [], isLoading: approvalLoading } = useQuery({
    ...issueAttentionListOptions(wsId),
    enabled: !!wsId,
  });
  const approvalItems = useMemo(
    () => approvalIssues.filter(isIssueApprovalRequired).map(issueToApprovalGateInboxItem),
    [approvalIssues],
  );
  const displayItems = useMemo(
    () => approvalOnly ? approvalItems : deduplicateInboxItems([...approvalItems, ...items]),
    [approvalItems, approvalOnly, items],
  );

  const selected = displayItems.find((i) => (i.issue_id ?? i.id) === selectedKey) ?? null;

  // Track the last key we actually resolved against the inbox list. Lets the
  // fallback effect distinguish "shared-link to a notification not in our
  // inbox" (never resolved → redirect to the issue page) from "item was in
  // our inbox and just got removed" (was resolved → stay on /inbox).
  const lastResolvedKeyRef = useRef<string>("");
  useEffect(() => {
    if (selected) lastResolvedKeyRef.current = selectedKey;
  }, [selected, selectedKey]);

  const setSelectedKey = useCallback((key: string) => {
    setSelectedKeyState(key);
    const inboxPath = wsPaths.inbox();
    const params = new URLSearchParams();
    if (approvalOnly) params.set("filter", "approvals");
    if (key) params.set("issue", key);
    const qs = params.toString();
    const url = qs ? `${inboxPath}?${qs}` : inboxPath;
    replace(url);
  }, [approvalOnly, replace, wsPaths]);

  // Shared inbox links (?issue=<id>) may point to notifications not in this
  // user's inbox (archived, or never received). Fall back to the issue page
  // so the URL still resolves to something meaningful. But if the key was
  // previously resolvable (e.g. the issue was just deleted in another tab
  // and `onInboxIssueDeleted` pruned the cache), the issue detail would 404
  // too — clear the selection and stay on /inbox instead.
  useEffect(() => {
    if (loading) return;
    if (approvalOnly) return;
    if (!selectedKey) return;
    if (selected) return;
    if (lastResolvedKeyRef.current === selectedKey) {
      setSelectedKey("");
      return;
    }
    replace(wsPaths.issueDetail(selectedKey));
  }, [approvalOnly, loading, selectedKey, selected, replace, wsPaths, setSelectedKey]);

  useEffect(() => {
    if (!approvalOnly) return;
    if (approvalLoading) return;
    if (selected) return;
    const next = approvalItems[0] ?? null;
    setSelectedKey(next ? (next.issue_id ?? next.id) : "");
  }, [approvalItems, approvalLoading, approvalOnly, selected, setSelectedKey]);

  const { defaultLayout, onLayoutChanged } = useDefaultLayout({
    id: "multica_inbox_layout",
  });

  const isMobile = useIsMobile();
  const unreadCount = useInboxUnreadCount(wsId);

  const markReadMutation = useMarkInboxRead();
  const archiveMutation = useArchiveInbox();
  const markAllReadMutation = useMarkAllInboxRead();
  const archiveAllMutation = useArchiveAllInbox();
  const archiveAllReadMutation = useArchiveAllReadInbox();
  const archiveCompletedMutation = useArchiveCompletedInbox();
  const timeAgo = useTimeAgo();
  const typeLabels = useTypeLabels();


  // Auto-mark-read whenever a selected item is unread — covers both click-
  // to-select and URL-param-select (e.g. OS notification click on desktop).
  // The mutation flips `read: true` optimistically, so this effect settles
  // in one pass and can't loop. Kept in a `useEffect` rather than inlined
  // in handleSelect so URL-driven selection triggers it too.
  const markReadMutate = markReadMutation.mutate;
  const selectedId = selected?.id;
  const selectedRead = selected?.read;
  const selectedIsApprovalGate = selected ? isApprovalGateInboxItem(selected) : false;
  useEffect(() => {
    if (!selectedId || selectedRead) return;
    if (selectedIsApprovalGate) return;
    markReadMutate(selectedId, {
      onError: (err) =>
        toast.error(
          err instanceof Error && err.message
            ? err.message
            : t(($) => $.errors.mark_read_failed),
        ),
    });
  }, [selectedId, selectedRead, selectedIsApprovalGate, markReadMutate, t]);

  const handleSelect = (item: InboxItem) => {
    setSelectedKey(item.issue_id ?? item.id);
  };

  const approvalFilterHref = approvalOnly
    ? wsPaths.inbox()
    : `${wsPaths.inbox()}?filter=approvals`;

  const handleArchive = (id: string) => {
    const idx = items.findIndex((i) => i.id === id);
    const archived = idx >= 0 ? items[idx] : null;
    const wasSelected =
      !!archived && (archived.issue_id ?? archived.id) === selectedKey;
    if (wasSelected) {
      // List is sorted newest-first; prefer the next (older) item, fall back
      // to the previous (newer) one when archiving at the bottom, and only
      // clear the selection when nothing else is left.
      const next = items[idx + 1] ?? items[idx - 1] ?? null;
      setSelectedKey(next ? (next.issue_id ?? next.id) : "");
    }
    archiveMutation.mutate(id, {
      onError: (err) =>
        toast.error(
          err instanceof Error && err.message
            ? err.message
            : t(($) => $.errors.archive_failed),
        ),
    });
  };

  // Batch operations
  const handleMarkAllRead = () => {
    markAllReadMutation.mutate(undefined, {
      onError: (err) =>
        toast.error(
          err instanceof Error && err.message
            ? err.message
            : t(($) => $.errors.mark_all_read_failed),
        ),
    });
  };

  const handleArchiveAll = () => {
    setSelectedKey("");
    archiveAllMutation.mutate(undefined, {
      onError: (err) =>
        toast.error(
          err instanceof Error && err.message
            ? err.message
            : t(($) => $.errors.archive_all_failed),
        ),
    });
  };

  const handleArchiveAllRead = () => {
    const readKeys = items.filter((i) => i.read).map((i) => i.issue_id ?? i.id);
    if (readKeys.includes(selectedKey)) setSelectedKey("");
    archiveAllReadMutation.mutate(undefined, {
      onError: (err) =>
        toast.error(
          err instanceof Error && err.message
            ? err.message
            : t(($) => $.errors.archive_all_read_failed),
        ),
    });
  };

  const handleArchiveCompleted = () => {
    setSelectedKey("");
    archiveCompletedMutation.mutate(undefined, {
      onError: (err) =>
        toast.error(
          err instanceof Error && err.message
            ? err.message
            : t(($) => $.errors.archive_completed_failed),
        ),
    });
  };

  // -- Shared sub-components --------------------------------------------------

  const listHeader = (
    <PageHeader className="justify-between">
      <div className="flex items-center gap-2">
        <h1 className="text-sm font-semibold">{t(($) => $.page.title)}</h1>
        {unreadCount > 0 && (
          <span className="text-xs text-muted-foreground">
            {unreadCount}
          </span>
        )}
      </div>
      <div className="flex items-center gap-1">
        <Button
          variant={approvalOnly ? "secondary" : "ghost"}
          size="sm"
          render={<AppLink href={approvalFilterHref} />}
          nativeButton={false}
          className="h-7 gap-1.5 rounded-md px-2 text-xs text-muted-foreground data-[state=on]:text-foreground"
          data-state={approvalOnly ? "on" : "off"}
          aria-pressed={approvalOnly}
        >
          <ListFilter className="h-3.5 w-3.5" />
          {t(($) => $.filters.approvals_only)}
        </Button>
        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <Button
                variant="ghost"
                size="icon-sm"
                className="text-muted-foreground"
              />
            }
          >
            <MoreHorizontal className="h-4 w-4" />
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="w-auto">
            <DropdownMenuItem onClick={handleMarkAllRead}>
              <CheckCheck className="h-4 w-4" />
              {t(($) => $.menu.mark_all_read)}
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem onClick={handleArchiveAll}>
              <Archive className="h-4 w-4" />
              {t(($) => $.menu.archive_all)}
            </DropdownMenuItem>
            <DropdownMenuItem onClick={handleArchiveAllRead}>
              <BookCheck className="h-4 w-4" />
              {t(($) => $.menu.archive_all_read)}
            </DropdownMenuItem>
            <DropdownMenuItem onClick={handleArchiveCompleted}>
              <ListChecks className="h-4 w-4" />
              {t(($) => $.menu.archive_completed)}
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
    </PageHeader>
  );

  const listBody = approvalOnly && approvalLoading ? (
    <div className="space-y-1 p-2">
      {Array.from({ length: 5 }).map((_, i) => (
        <div key={i} className="flex items-center gap-3 px-4 py-2.5">
          <Skeleton className="h-7 w-7 shrink-0 rounded-full" />
          <div className="flex-1 space-y-2">
            <Skeleton className="h-4 w-3/4" />
            <Skeleton className="h-3 w-1/2" />
          </div>
        </div>
      ))}
    </div>
  ) : displayItems.length === 0 ? (
    <div className="flex flex-col items-center justify-center py-16 text-muted-foreground">
      <Inbox className="mb-3 h-8 w-8 text-muted-foreground/50" />
      <p className="text-sm">
        {approvalOnly ? t(($) => $.list.empty_approvals) : t(($) => $.list.empty)}
      </p>
    </div>
  ) : (
    <div>
      {displayItems.map((item) => (
        <InboxListItem
          key={item.id}
          item={item}
          isSelected={(item.issue_id ?? item.id) === selectedKey}
          onClick={() => handleSelect(item)}
          onArchive={() => handleArchive(item.id)}
          canArchive={!isApprovalGateInboxItem(item)}
        />
      ))}
    </div>
  );

  const emptyDetailText = displayItems.length === 0
    ? approvalOnly
      ? t(($) => $.list.empty_approvals)
      : t(($) => $.detail.empty)
    : t(($) => $.detail.select_prompt);

  const detailContent = selected?.issue_id ? (
    // Key by issue_id (not inbox-item id): a new comment/reaction generates a
    // new inbox notification for the same issue, and the dedup helper picks the
    // newest one — keying on its id would remount IssueDetail on every event,
    // wiping the comment composer draft and resetting scroll position.
    <ErrorBoundary resetKeys={[selected.issue_id]}>
      <IssueDetail
        key={selected.issue_id}
        issueId={selected.issue_id}
        defaultSidebarOpen={false}
        layoutId="multica_inbox_issue_detail_layout"
        highlightCommentId={selected.details?.comment_id ?? undefined}
        onDelete={() => {
          // Issue deletion CASCADE-deletes the inbox item server-side, and the
          // issue:deleted WS event prunes it from the inbox cache. Just clear
          // the selection — calling archive here would 404 on a row that no
          // longer exists.
          setSelectedKey("");
        }}
        onDone={() => {
          if (isApprovalGateInboxItem(selected)) setSelectedKey("");
          else handleArchive(selected.id);
        }}
      />
    </ErrorBoundary>
  ) : selected ? (
    <div className="p-6">
      <h2 className="text-lg font-semibold">{getInboxDisplayTitle(selected)}</h2>
      <p className="mt-1 text-sm text-muted-foreground">
        {typeLabels[selected.type]} · {timeAgo(selected.created_at)}
      </p>
      {selected.body && (
        <div className="mt-4 whitespace-pre-wrap text-sm leading-relaxed text-foreground/80">
          {selected.body}
        </div>
      )}
      {selected.type === "quick_create_failed" && selected.details?.original_prompt && (
        <div className="mt-4 rounded-md border bg-muted/40 p-3">
          <p className="text-xs font-medium text-muted-foreground">
            {t(($) => $.detail.original_input)}
          </p>
          <p className="mt-1 whitespace-pre-wrap text-sm">{selected.details.original_prompt}</p>
        </div>
      )}
      <div className="mt-4 flex gap-2">
        {selected.type === "quick_create_failed" && (
          <Button
            size="sm"
            onClick={() => {
              // Seed the legacy advanced form with the original prompt so the
              // user can recover their input in the full editor instead of
              // retyping. The agent picker hint becomes the assignee
              // candidate (still editable).
              const prompt = selected.details?.original_prompt ?? "";
              const agentId = selected.details?.agent_id;
              useIssueDraftStore.getState().setDraft({
                description: prompt,
                ...(agentId
                  ? { assigneeType: "agent" as const, assigneeId: agentId }
                  : {}),
              });
              useModalStore.getState().open("create-issue");
            }}
          >
            {t(($) => $.detail.edit_advanced)}
          </Button>
        )}
        <Button
          variant="outline"
          size="sm"
          onClick={() => handleArchive(selected.id)}
        >
          <Archive className="mr-1.5 h-3.5 w-3.5" />
          {t(($) => $.detail.archive)}
        </Button>
      </div>
    </div>
  ) : null;

  // -- Mobile layout: list / detail toggle -----------------------------------

  if (isMobile) {
    if (loading) {
      return (
        <div className="flex flex-1 flex-col min-h-0">
          <div className="flex h-12 shrink-0 items-center border-b px-4">
            <Skeleton className="h-5 w-16" />
          </div>
          <div className="flex-1 min-h-0 overflow-y-auto space-y-1 p-2">
            {Array.from({ length: 5 }).map((_, i) => (
              <div key={i} className="flex items-center gap-3 px-4 py-2.5">
                <Skeleton className="h-7 w-7 shrink-0 rounded-full" />
                <div className="flex-1 space-y-2">
                  <Skeleton className="h-4 w-3/4" />
                  <Skeleton className="h-3 w-1/2" />
                </div>
              </div>
            ))}
          </div>
        </div>
      );
    }

    // Mobile: show detail full-screen when an item is selected
    if (selected) {
      return (
        <div className="flex flex-1 flex-col min-h-0">
          <div className="flex h-12 shrink-0 items-center border-b px-2">
            <Button
              variant="ghost"
              size="sm"
              onClick={() => setSelectedKey("")}
              className="gap-1.5 text-muted-foreground"
            >
              <ArrowLeft className="h-4 w-4" />
              {t(($) => $.page.back)}
            </Button>
          </div>
          <div className="flex-1 min-h-0 overflow-y-auto">
            {detailContent}
          </div>
        </div>
      );
    }

    // Mobile: full-screen list
    return (
      <div className="flex flex-1 flex-col min-h-0">
        {listHeader}
        <div className="flex-1 min-h-0 overflow-y-auto">
          {listBody}
        </div>
      </div>
    );
  }

  // -- Desktop layout: resizable two-panel -----------------------------------

  if (loading) {
    return (
      <ResizablePanelGroup orientation="horizontal" className="flex-1 min-h-0" defaultLayout={defaultLayout} onLayoutChanged={onLayoutChanged}>
        <ResizablePanel id="list" defaultSize={320} minSize={240} maxSize={480} groupResizeBehavior="preserve-pixel-size">
          <div className="flex flex-col border-r h-full">
            <div className="flex h-12 shrink-0 items-center border-b px-4">
              <Skeleton className="h-5 w-16" />
            </div>
            <div className="flex-1 min-h-0 overflow-y-auto space-y-1 p-2">
              {Array.from({ length: 5 }).map((_, i) => (
                <div key={i} className="flex items-center gap-3 px-4 py-2.5">
                  <Skeleton className="h-7 w-7 shrink-0 rounded-full" />
                  <div className="flex-1 space-y-2">
                    <Skeleton className="h-4 w-3/4" />
                    <Skeleton className="h-3 w-1/2" />
                  </div>
                </div>
              ))}
            </div>
          </div>
        </ResizablePanel>
        <ResizableHandle />
        <ResizablePanel id="detail" minSize="40%">
          <div className="p-6">
            <Skeleton className="h-6 w-48" />
            <Skeleton className="mt-4 h-4 w-32" />
          </div>
        </ResizablePanel>
      </ResizablePanelGroup>
    );
  }

  return (
    <ResizablePanelGroup orientation="horizontal" className="flex-1 min-h-0" defaultLayout={defaultLayout} onLayoutChanged={onLayoutChanged}>
      <ResizablePanel id="list" defaultSize={320} minSize={240} maxSize={480} groupResizeBehavior="preserve-pixel-size">
      <div className="flex flex-col border-r h-full">
        {listHeader}
        <div className="flex-1 min-h-0 overflow-y-auto">
          {listBody}
        </div>
      </div>
      </ResizablePanel>
      <ResizableHandle />
      <ResizablePanel id="detail" minSize="40%">
      <div className="flex flex-col min-h-0 h-full">
        {detailContent ?? (
          <div className="flex h-full flex-col items-center justify-center text-muted-foreground">
            <Inbox className="mb-3 h-10 w-10 text-muted-foreground/30" />
            <p className="text-sm">{emptyDetailText}</p>
          </div>
        )}
      </div>
      </ResizablePanel>
    </ResizablePanelGroup>
  );
}
