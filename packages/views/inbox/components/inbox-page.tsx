"use client";

import {
  useState,
  useEffect,
  useCallback,
  useDeferredValue,
  useMemo,
  useRef,
  type ReactNode,
} from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { ApiError, errorCode } from "@multica/core/api";
import { useWorkspacePaths } from "@multica/core/paths";
import { useModalStore } from "@multica/core/modals";
import { useIssueDraftStore } from "@multica/core/issues/stores/draft-store";
import { cachedIssueRouteRef } from "@multica/core/issues/canonical-id";
import { useInboxSelectionStore, type LastInboxCleared } from "@multica/core/inbox/selection-store";
import { inboxRowKey, nextInboxRow } from "@multica/core/inbox/open-row";
import { useIssueStatuses } from "@multica/core/issue-statuses/hooks";
import {
  inboxListOptions,
  archivedInboxLookupOptions,
  deduplicateInboxItems,
  deduplicateArchivedInboxItems,
  findSnoozedInboxIssue,
  useInboxUnreadCount,
} from "@multica/core/inbox/queries";
import {
  useMarkInboxRead,
  useMarkInboxUnread,
  useMarkAllInboxRead,
  useArchiveAllInbox,
  useArchiveAllReadInbox,
  useArchiveCompletedInbox,
  useRetrySourceContextQuickCreate,
  type InboxUnsubscribeScope,
} from "@multica/core/inbox/mutations";
import {
  effectiveInboxFilters,
  filterInboxItems,
  inboxFilterCount,
  inboxIssueFieldsSupport,
  inboxPriorityFilterSupport,
  useInboxFilters,
  useInboxFilterStore,
  type InboxAgentFilter,
} from "@multica/core/inbox/filter-store";

import { IssueDetail, issueHighlightMementoKey, type IssueDetailInboxHost } from "../../issues/components/issue-detail";
import { IssueInboxBar } from "../../issues/actions/issue-inbox-actions";
import { ActionShortcutKeycaps } from "../../common/shortcut-keycaps";
import { useRegisterCommandBarContext } from "../../search/register-context";
import { IssuePeekHost } from "../../issues/components/issue-peek";
import { useIssueSurfaceActivity } from "../../issues/surface/activity";
import { useViewStateWriter } from "../../platform";
import { ErrorBoundary } from "@multica/ui/components/common/error-boundary";
import { AppLink, useNavigation, useReportNavigating } from "../../navigation";
import { toast } from "sonner";
import {
  MoreHorizontal,
  Inbox,
  CheckCheck,
  Archive,
  ArchiveRestore,
  BookCheck,
  ListChecks,
  ArrowLeft,
  AlarmClock,
} from "@multica/ui/icons";
import type { InboxItem } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { IconButton } from "@multica/ui/components/common/icon-button";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
} from "@multica/ui/components/ui/dropdown-menu";
import { useIsCompact } from "@multica/ui/hooks/use-mobile";
import { cn } from "@multica/ui/lib/utils";
import { CollapsedNavTrigger, PAGE_GUTTER, PageHeader } from "../../layout/page-header";
import { PanelHeader } from "../../layout/panel-nav";
import { InboxDeleteDialog } from "./inbox-delete-dialog";
import { useTimeAgo } from "./inbox-list-item";
import { InboxList } from "./inbox-list";
import { InboxFilterMenu } from "./inbox-filter-menu";
import { InboxFilterChipsBar } from "./inbox-filter-options";
import { InboxContextMenuProvider } from "./inbox-context-menu";
import { useInboxUnsubscribable, useUnsubscribeArchive } from "./use-unsubscribe-archive";
import { inboxTabFromParam, inboxTabHref, type InboxTab, type InboxView } from "./inbox-view";
import { InboxTabs } from "./inbox-tabs";
import { InboxArchivedBar } from "./inbox-bar";
import { SnoozedInboxList } from "./snoozed-inbox-list";
import { useArchivedInboxRows, useSnoozedInboxRows } from "./use-inbox-tab-rows";
import { useTypeLabels } from "./inbox-detail-label";
import {
  getInboxDisplayTitle,
  inboxRowHref,
  isAutopilotQuotaNotice,
  isQuickCreateOutcome,
  resolveDetailItem,
} from "./inbox-display";
import { AutopilotQuotaNotice } from "./autopilot-quota-notice";
import { InboxEmptyState } from "./inbox-empty-state";
import { InboxKeptActions, useKeptInboxRowText } from "./inbox-kept-actions";
import { inboxListLoad, useFilteredInboxRows } from "./use-filtered-inbox-rows";
import { useKeptOpenInboxRow } from "./use-kept-open-inbox-row";
import { useMoveOnAfterAutoArchive } from "./use-move-on-after-auto-archive";
import {
  clearedLabelForItem,
  collectBatchArchiveIds,
  useArchiveInboxBatchWithUndo,
  useArchiveInboxWithUndo,
  useRestoreInbox,
} from "./use-leave-inbox-row";
import { useInboxIssuePrefetch } from "./use-inbox-issue-prefetch";
import { useInboxShortcut } from "./use-inbox-shortcut";
import { InboxAppRequest } from "../../common/app-requests";
import { useT } from "../../i18n";
import { useLimitRecoveryPrompt } from "../../modals/use-limit-recovery-prompt";


/** The phone's Inbox list, apart from the Work panel section's. */
const INBOX_PAGE_LIST = "page";

export function InboxPage() {
  const queryClient = useQueryClient();
  const { t } = useT("inbox");
  const { t: tCommon } = useT("common");
  const showAutopilotQuotaRecoveryPrompt = useLimitRecoveryPrompt(
    "autopilot_quota",
  );
  const { searchParams, replace, push } = useNavigation();
  const urlIssue = searchParams.get("issue") ?? "";
  const urlTab = inboxTabFromParam(searchParams.get("view"));
  const wsPaths = useWorkspacePaths();

  const [selectedKey, setSelectedKeyState] = useState(() => urlIssue);
  const [tab, setTabState] = useState<InboxTab>(() => urlTab);
  // The row the user opened under the unread filter. Opening it marks it read,
  // so the filter would otherwise drop the row before the detail finishes
  // mounting. Scoped to the selection: moving away releases it.
  const pinnedUnreadKeyRef = useRef<string | null>(null);
  // The row the user opened under the agent filter. Replying from the detail
  // can start or finish an agent on its issue, which would otherwise drop the
  // row the user is working in. Set once the open row has passed the agent
  // filter, so turning the filter on over a non-matching row still filters
  // it out; it only holds for the agent state it was pinned under.
  const pinnedAgentRef = useRef<{ key: string; agent: InboxAgentFilter } | null>(null);

  // Follow the URL when it changes (a navigation, or an issue's bar moving on
  // to the next notification) in the same render, so no effect acts on the
  // selection the URL just left.
  const [seenUrl, setSeenUrl] = useState({ issue: urlIssue, tab: urlTab });
  if (seenUrl.issue !== urlIssue || seenUrl.tab !== urlTab) {
    setSeenUrl({ issue: urlIssue, tab: urlTab });
    if (seenUrl.issue !== urlIssue) setSelectedKeyState(urlIssue);
    if (seenUrl.tab !== urlTab) setTabState(urlTab);
  }

  const wsId = useWorkspaceId();
  const isCompact = useIsCompact();
  const isArchivedView = tab === "archived";
  // The Snoozed tab lists issues, not notifications: what it opens is the
  // issue, with the bar that says when it comes back.
  const isSnoozedView = tab === "snoozed";
  // Which list a notification row belongs to; the Snoozed tab has none.
  const view: InboxView = isArchivedView ? "archived" : "inbox";
  const filters = useInboxFilters(wsId);
  const clearFilters = useInboxFilterStore((state) => state.clearFilters);
  const inboxQuery = useQuery({
    ...inboxListOptions(wsId), enabled: !isArchivedView,
  });
  const { data: rawItems = [], isLoading: loading } = inboxQuery;
  // The live list never loaded (a refresh that fails keeps the loaded one).
  // An empty list is not an empty Inbox then.
  const liveFailed = !isArchivedView && inboxListLoad(inboxQuery) === "failed";
  const items = useMemo(() => deduplicateInboxItems(rawItems), [rawItems]);
  // The filters read every notification of an issue, not just its newest.
  const activeRows = useMemo(() => rawItems.filter((item) => !item.archived), [rawItems]);
  const {
    query: archiveQuery,
    items: archivedItems,
    filters: archiveFilters,
    issueFieldsSupport: archiveIssueFields,
  } = useArchivedInboxRows(wsId, isArchivedView);
  const fetchNextArchivedPage = archiveQuery.fetchNextPage;
  const loadNextArchivedPage = useCallback(() => { void fetchNextArchivedPage(); }, [fetchNextArchivedPage]);
  const archivedLoading = archiveQuery.isLoading;
  const archivedError = archiveQuery.isError && !archiveQuery.data;
  const snoozed = useSnoozedInboxRows(wsId, isSnoozedView || isCompact);
  const viewItems = isArchivedView ? archivedItems : items;
  // The paginated endpoint guarantees the priority projection, including on
  // empty pages.
  const priorityFilterSupport = isArchivedView ? "supported" : inboxPriorityFilterSupport(rawItems);
  const issueFieldsSupport = isArchivedView ? archiveIssueFields : inboxIssueFieldsSupport(rawItems);
  const activeFilters = useMemo(
    () => effectiveInboxFilters(filters, { priority: priorityFilterSupport, issueFields: issueFieldsSupport }),
    [filters, priorityFilterSupport, issueFieldsSupport],
  );
  const effectiveFilters = isArchivedView ? archiveFilters : activeFilters;
  // "Active" is the same running-or-queued rule the row's activity badge uses.
  const { activityByIssueId } = useIssueSurfaceActivity();
  const activeIssueIds = useMemo(() => new Set(activityByIssueId.keys()), [activityByIssueId]);
  // Only an agent selection reads the snapshot, so task churn leaves the
  // list alone while it is off.
  const agentFilterIssueIds = effectiveFilters.agent ? activeIssueIds : undefined;
  // The open row survives the unread and agent dimensions while it stays
  // selected; see the pin refs above. Each pin is passed separately so it
  // only crosses its own dimension.
  const pinnedUnreadKey =
    effectiveFilters.unreadOnly && pinnedUnreadKeyRef.current === selectedKey
      ? selectedKey
      : null;
  const pinnedAgentKey =
    effectiveFilters.agent &&
    pinnedAgentRef.current?.key === selectedKey &&
    pinnedAgentRef.current.agent === effectiveFilters.agent
      ? selectedKey
      : null;
  const pinnedKey = pinnedUnreadKey ?? pinnedAgentKey;
  const filterContext = useMemo(
    () => ({ activeIssueIds: agentFilterIssueIds, pinnedUnreadKey, pinnedAgentKey }),
    [agentFilterIssueIds, pinnedUnreadKey, pinnedAgentKey],
  );
  const filterRows = isArchivedView ? archivedItems : activeRows;
  const filteredItems = useMemo(
    () => filterInboxItems(filterRows, effectiveFilters, filterContext),
    [filterRows, effectiveFilters, filterContext],
  );
  const hasActiveFilters = inboxFilterCount(effectiveFilters) > 0;
  const selectedOnPage = viewItems.find((i) => (i.issue_id ?? i.id) === selectedKey);
  // A deep link can point beyond every loaded page. Resolve its group directly
  // without adding it to the cursor chain or mistaking a page miss for a 404.
  // It asks with the view's filters, so the server answers with the group's
  // newest MATCHING notification — the row the list showed — rather than its
  // newest one. Unread is left out: the open row is exempt from it (MUL-8073).
  const lookupFilters = useMemo(() => ({ ...archiveFilters, unreadOnly: false }), [archiveFilters]);
  const lookupFiltered = inboxFilterCount(lookupFilters) > 0;
  const lookupEnabled = isArchivedView && !!selectedKey && !selectedOnPage && !archivedLoading && !archivedError;
  const lookup = useQuery({
    ...archivedInboxLookupOptions(wsId, selectedKey, lookupFilters),
    enabled: lookupEnabled,
  });
  // A filtered lookup that comes back empty cannot tell a filtered-out group
  // from a missing one. Ask without filters only then, so a filtered-out deep
  // link clears the selection instead of redirecting to the issue page.
  const lookupMissed = lookupFiltered && lookup.isSuccess && !lookup.isPlaceholderData && lookup.data.items.length === 0;
  const presence = useQuery({
    ...archivedInboxLookupOptions(wsId, selectedKey),
    enabled: lookupEnabled && lookupMissed,
  });
  const lookupItems = useMemo(() => deduplicateArchivedInboxItems(lookup.data?.items ?? []), [lookup.data]);
  const presenceItems = lookupMissed ? presence.data?.items ?? [] : [];
  // On desktop the live list is the Work panel's Inbox section, which applies
  // the filters itself. The page only opens the notification the URL names,
  // so a filter can never refuse to open a row the section offered.
  const opensAnyLiveRow = !isCompact && tab === "inbox";
  // The open notification stays in place when something other than an Inbox
  // action takes it out of the live list (`keepOpenInboxRow`). On desktop
  // that list is the Work panel section's; a phone keeps it in the page's own.
  const sectionList = useFilteredInboxRows(wsId, selectedKey || null, opensAnyLiveRow);
  const pageList = useKeptOpenInboxRow(
    wsId,
    INBOX_PAGE_LIST,
    filteredItems,
    activeRows,
    selectedKey || null,
    effectiveFilters,
    isCompact && tab === "inbox",
  );
  // Leaving the page leaves its open row.
  useEffect(() => () => useInboxSelectionStore.getState().dropRow(wsId, INBOX_PAGE_LIST), [wsId]);
  const keptRow = tab !== "inbox" ? null : opensAnyLiveRow ? sectionList.kept : pageList.kept;
  // Left the Inbox rather than only the filters, so there is nothing left to act on.
  const settledRow = keptRow && keptRow.reason !== "filtered" ? keptRow : null;
  const selectionItems = useMemo(() => {
    if (isSnoozedView) return [];
    if (opensAnyLiveRow) return settledRow ? [...items, settledRow.item] : items;
    if (!isArchivedView) return pageList.rows;
    if (selectedOnPage) return filteredItems;
    return deduplicateArchivedInboxItems([
      ...filteredItems,
      ...filterInboxItems(lookupItems, effectiveFilters, filterContext),
    ]);
  }, [isSnoozedView, opensAnyLiveRow, settledRow, items, isArchivedView, pageList.rows, selectedOnPage, filteredItems, lookupItems, effectiveFilters, filterContext]);
  const visibleItems =
    isArchivedView && pinnedKey ? selectionItems : isArchivedView || opensAnyLiveRow ? filteredItems : pageList.rows;

  // Inbox zero: the bleeding m rises once, at the moment the last notification
  // leaves; opening an already-empty inbox shows it at rest. Archived and
  // filtered views are not inbox zero, so they never arm it.
  const inboxZeroCount = tab !== "inbox" || hasActiveFilters || liveFailed ? -1 : visibleItems.length;
  const [prevInboxZeroCount, setPrevInboxZeroCount] = useState(inboxZeroCount);
  const [inboxZeroRises, setInboxZeroRises] = useState(false);
  if (inboxZeroCount !== prevInboxZeroCount) {
    setPrevInboxZeroCount(inboxZeroCount);
    setInboxZeroRises(prevInboxZeroCount > 0 && inboxZeroCount === 0);
  }
  const selected = selectionItems.find((i) => (i.issue_id ?? i.id) === selectedKey) ?? null;
  const selectedInView = selectedOnPage ?? (isArchivedView
    ? [...lookupItems, ...presenceItems].find((i) => (i.issue_id ?? i.id) === selectedKey)
    : null);
  const selectionFilteredOut = !isSnoozedView && !!selectedKey && !!selectedInView && !selected;

  // What the DETAIL pane shows, one React transition behind the click.
  //
  // Mounting `IssueDetail` is the expensive half of switching rows, and driven
  // straight off `selectedKey` it ran as an urgent update: the main thread
  // blocked from the click until the new detail was ready, which is the
  // "click, freeze, jump" the desktop shell showed (MUL-6404). Deferred, the
  // click commits the row highlight and the URL right away, React renders the
  // new detail at transition priority — interruptible, so the shell keeps
  // painting — and the previous issue stays on screen until it is ready.
  const detailKey = useDeferredValue(selectedKey);
  const detailSwapping = detailKey !== selectedKey;
  const detailItem = resolveDetailItem(selectionItems, selectedKey, detailKey);

  // The gap above is invisible to the navigation adapter — the inbox stays on
  // the same route and only rewrites `?issue=` — so report it explicitly and
  // the shell's progress bar covers the swap on both platforms.
  useReportNavigating(detailSwapping);

  // Keep recognition across page mounts and query-cache eviction. A remembered
  // selection that disappears stays in Inbox; an unknown shared link still
  // falls back to the issue page.
  useEffect(() => {
    if (selected) useInboxSelectionStore.getState().rememberResolvedKey(wsId, selectedKey);
  }, [wsId, selected, selectedKey]);

  // Both the tab and the selection live in the URL, so every write carries
  // the tab: a bare `?issue=` would silently drop the user out of it on the
  // next selection.
  const setSelectedKey = useCallback((key: string) => {
    setSelectedKeyState(key);
    replace(inboxTabHref(wsPaths, tab, key));
  }, [replace, wsPaths, tab]);

  // Switching tabs always clears the selection: the lists are mutually
  // exclusive, so a key carried across would never resolve, and the fallback
  // effect below would bounce the user to the issue page.
  const setTab = useCallback((nextTab: InboxTab) => {
    setTabState(nextTab);
    setSelectedKeyState("");
    replace(inboxTabHref(wsPaths, nextTab));
  }, [replace, wsPaths]);

  // A snoozed issue that is back in the Inbox (brought back, or its time came)
  // opens there. Until the Inbox lists it, it stays open here.
  const snoozedOpen = isSnoozedView && !!findSnoozedInboxIssue(snoozed.rows, selectedKey);
  const backInInbox = isSnoozedView && !!selectedKey && !snoozedOpen && items.some((item) => inboxRowKey(item) === selectedKey);
  useEffect(() => {
    if (backInInbox) replace(inboxTabHref(wsPaths, "inbox", selectedKey));
  }, [backInInbox, replace, wsPaths, selectedKey]);

  // Applying a filter can remove the open row from the list. Clear that local
  // selection instead of treating it as a broken deep link and redirecting to
  // the issue page; the notification still exists, it is simply filtered out.
  useEffect(() => {
    if (selectionFilteredOut) setSelectedKey("");
  }, [selectionFilteredOut, setSelectedKey]);

  // A targeted lookup must not hide pages that have already loaded. Its
  // pending/error states only block resolution of the off-page selection.
  // A list that failed and is being tried again keeps its error, not a skeleton.
  const viewLoading = isArchivedView
    ? archivedLoading
    : isSnoozedView
      ? snoozed.load === "loading"
      : loading && !liveFailed;
  const viewError = isArchivedView ? archivedError : isSnoozedView ? snoozed.load === "failed" : liveFailed;
  const needsLookup = isArchivedView && !!selectedKey && !selectedOnPage;
  const lookupLoading = needsLookup && (lookup.isLoading || presence.isLoading);
  const lookupError = needsLookup && (lookup.isError || presence.isError) && !selected;

  // Shared inbox links (?issue=<id>) may point to notifications not in this
  // user's inbox (archived, or never received). Fall back to the issue page
  // so the URL still resolves to something meaningful. But if the key was
  // previously resolvable (e.g. the issue was just deleted in another tab
  // and `onInboxIssueDeleted` pruned the cache), the issue detail would 404
  // too — clear the selection and stay on /inbox instead.
  useEffect(() => {
    if (isSnoozedView) return;
    if (viewLoading || viewError || lookupLoading || lookupError) return;
    if (!selectedKey) return;
    if (selected) return;
    if (selectionFilteredOut) return;
    const store = useInboxSelectionStore.getState();
    // An Inbox action that took the open row opens what comes next itself.
    if (store.leftRowKeyByWorkspace[wsId] === selectedKey) return;
    if (store.lastResolvedKeyByWorkspace[wsId] === selectedKey) {
      setSelectedKey("");
      return;
    }
    replace(wsPaths.issueDetail(cachedIssueRouteRef(queryClient, wsId, selectedKey)));
  }, [
    isSnoozedView,
    viewLoading,
    viewError,
    lookupLoading,
    lookupError,
    selectedKey,
    selected,
    selectionFilteredOut,
    replace,
    wsPaths,
    wsId,
    queryClient,
    setSelectedKey,
  ]);

  const unreadCount = useInboxUnreadCount(wsId);

  const markReadMutation = useMarkInboxRead();
  const [deleteTarget, setDeleteTarget] = useState<InboxItem | null>(null);
  const markUnreadMutation = useMarkInboxUnread();
  const archiveWithUndo = useArchiveInboxWithUndo();
  const archiveBatchWithUndo = useArchiveInboxBatchWithUndo();
  const restoreInbox = useRestoreInbox();
  const keptText = useKeptInboxRowText(wsId);
  const { categoryOf } = useIssueStatuses(wsId);
  const markAllReadMutation = useMarkAllInboxRead();
  const archiveAllMutation = useArchiveAllInbox();
  const archiveAllReadMutation = useArchiveAllReadInbox();
  const archiveCompletedMutation = useArchiveCompletedInbox();
  const retrySourceContextMutation = useRetrySourceContextQuickCreate();
  const timeAgo = useTimeAgo();
  const typeLabels = useTypeLabels();


  // An explicit "mark as unread" on the row that is currently open has to
  // survive the auto-read effect below, which would otherwise fire on the very
  // next commit and silently undo it. Holds that one item's id, and only while
  // it stays selected: moving the selection elsewhere releases the guard, so
  // re-opening the row later marks it read again like any other open.
  const manualUnreadIdRef = useRef<string | null>(null);

  // Read once on opening, even if the visible row is already read: older
  // unread notifications can still be hidden under it. Also read new unread
  // arrivals while the row stays open. Track the opening separately from the
  // optimistic cache patch so a read row cannot cause a request loop.
  const openedItemIdRef = useRef<string | null>(null);
  const markReadMutate = markReadMutation.mutate;
  const selectedId = selected?.id;
  const selectedRead = selected?.read;
  useEffect(() => {
    if (!selectedId) {
      openedItemIdRef.current = null;
      return;
    }
    const justOpened = openedItemIdRef.current !== selectedId;
    openedItemIdRef.current = selectedId;
    if (!justOpened && selectedRead) return;
    if (manualUnreadIdRef.current === selectedId) return;
    if (filters.unreadOnly) {
      pinnedUnreadKeyRef.current = selectedKey;
      if (isArchivedView && selected) {
        // The unread page drops this group on refetch. Seed its lookup before
        // the mutation so cache patches, rollback and deletion apply to it too.
        queryClient.setQueryData(archivedInboxLookupOptions(wsId, selectedKey, lookupFilters).queryKey, {
          items: [selected], hasMore: false, nextCursor: null,
        });
      }
    }
    markReadMutate(selectedId, {
      onError: (err) =>
        toast.error(
          err instanceof Error && err.message
            ? err.message
            : t(($) => $.errors.mark_read_failed),
        ),
    });
  }, [selectedId, selectedRead, selectedKey, selected, filters.unreadOnly, isArchivedView, queryClient, wsId, lookupFilters, markReadMutate, t]);

  // Release the guards as soon as the selection moves off their row.
  useEffect(() => {
    if (manualUnreadIdRef.current && manualUnreadIdRef.current !== selectedId) {
      manualUnreadIdRef.current = null;
    }
    if (pinnedUnreadKeyRef.current && pinnedUnreadKeyRef.current !== selectedKey) {
      pinnedUnreadKeyRef.current = null;
    }
    if (pinnedAgentRef.current && pinnedAgentRef.current.key !== selectedKey) {
      pinnedAgentRef.current = null;
    }
  }, [selectedId, selectedKey]);

  // `selected` only exists once the open row passed every filter, so under
  // the agent filter this pins a row that matched when the user opened it.
  const agentFilter = effectiveFilters.agent;
  const unreadOnlyActive = effectiveFilters.unreadOnly;
  const hasSelected = selected != null;
  useEffect(() => {
    if (agentFilter && hasSelected && selectedKey) {
      pinnedAgentRef.current = { key: selectedKey, agent: agentFilter };
    }
  }, [agentFilter, hasSelected, selectedKey]);

  // Turning a filter off releases its pin, so turning it back on judges the
  // open row by its current state instead of an exemption from earlier.
  useEffect(() => {
    if (!agentFilter) pinnedAgentRef.current = null;
  }, [agentFilter]);
  useEffect(() => {
    if (!unreadOnlyActive) pinnedUnreadKeyRef.current = null;
  }, [unreadOnlyActive]);

  const writeViewState = useViewStateWriter();
  // Bumped when the already-open notification row is re-clicked; threaded to
  // IssueDetail so it replays the comment-highlight landing without a
  // remount. Selection changes don't need it — they remount the detail (key
  // by issue) and a fresh mount with a cleared memento entry lands by itself.
  const [highlightRequestToken, setHighlightRequestToken] = useState(0);
  // A Work panel row clicked again for the notification already open asks
  // for the landing through the store; the page neither remounts the detail
  // nor sees a new URL.
  const landingRequest = useInboxSelectionStore((s) => s.landing);
  const completeLanding = useInboxSelectionStore((s) => s.completeLanding);
  const selectedIssueId = selected?.issue_id ?? null;
  const [answeredLanding, setAnsweredLanding] = useState(landingRequest?.token ?? 0);
  if (landingRequest && landingRequest.token !== answeredLanding && landingRequest.issueId === selectedIssueId) {
    setAnsweredLanding(landingRequest.token);
    setHighlightRequestToken((token) => token + 1);
  }
  const handleSelect = (item: InboxItem) => {
    const nextKey = item.issue_id ?? item.id;
    // Every click on a notification row is a fresh deep-link intent: clear
    // the "highlight already landed" memento entry so the comment jump runs
    // again even if this issue's detail was opened (and its landing
    // consumed) before. Selection restored from the URL on a remount goes
    // through the effects above, not through here, so a tab switch back
    // keeps the entry and does NOT re-jump.
    if (item.issue_id) {
      writeViewState(issueHighlightMementoKey(item.issue_id), undefined);
      if (nextKey === selectedKey) setHighlightRequestToken((t) => t + 1);
    }
    setSelectedKey(nextKey);
  };

  // The comment a notification opens at is chosen once per opening or
  // re-click. Opening reads the issue, which moves the row's anchor from the
  // oldest unread comment to the newest; following it would land a second
  // time, past the comments the reader came for. Taken from the selected row,
  // not the deferred detail, which renders only after the read has landed.
  const [landing, setLanding] = useState<{ key: string; token: number; commentId: string | undefined } | null>(null);
  if (selected && (landing?.key !== selectedKey || landing.token !== highlightRequestToken)) {
    setLanding({ key: selectedKey, token: highlightRequestToken, commentId: selected.details?.comment_id ?? undefined });
  } else if (landing && !selectedKey && !detailKey) {
    // Back on the list: opening the row again chooses afresh.
    setLanding(null);
  }
  const detailItemKey = detailItem ? (detailItem.issue_id ?? detailItem.id) : null;
  const landingCommentId =
    landing?.key === detailItemKey ? landing.commentId : (detailItem?.details?.comment_id ?? undefined);

  const handleMarkRead = (id: string) => {
    // Reading it back explicitly cancels an earlier park on the same row.
    if (manualUnreadIdRef.current === id) manualUnreadIdRef.current = null;
    markReadMutation.mutate(id, {
      onError: (err) =>
        toast.error(
          err instanceof Error && err.message
            ? err.message
            : t(($) => $.errors.mark_read_failed),
        ),
    });
  };

  const handleMarkUnread = (id: string) => {
    // Only the open row needs the guard — the auto-read effect never touches
    // the others, and arming it for a background row would suppress the very
    // first open of that row later.
    if (selected?.id === id) manualUnreadIdRef.current = id;
    markUnreadMutation.mutate(id, {
      onError: (err) =>
        toast.error(
          err instanceof Error && err.message
            ? err.message
            : t(($) => $.errors.mark_unread_failed),
        ),
    });
  };

  // Both archive and unarchive remove the row from the list it was actioned
  // from, so both have to move the selection off it first. `list` is whichever
  // list the row came from — the main one for archive, the archived one for
  // unarchive.
  // The rows in the order the user sees them: on desktop the Work panel
  // section's, which applies the same filters.
  const listedItems = opensAnyLiveRow ? sectionList.rows : selectionItems;
  // Warm the next two rows around the active selection on Archived & compact
  // lists so unarchive/archive auto-advance renders from cache without flashing.
  useInboxIssuePrefetch(
    wsId,
    isArchivedView || isCompact ? listedItems : [],
    (item) => inboxRowHref(wsPaths, item),
    selectedKey || null,
  );
  const leaveRow = useInboxSelectionStore((s) => s.leaveRow);
  const recordLastCleared = useInboxSelectionStore((s) => s.recordLastCleared);
  const recordTransition = useInboxSelectionStore((s) => s.recordTransition);
  // Moving the selection off a row (an Inbox action on it) lets it go rather
  // than keep it in place.
  const advanceSelectionPast = (id: string, list: InboxItem[], cleared?: LastInboxCleared) => {
    const target = list.find((i) => i.id === id);
    const wasSelected = !!target && inboxRowKey(target) === selectedKey;
    if (!wasSelected) return;
    leaveRow(wsId, selectedKey);
    // Only clear the selection when nothing else is left.
    const next = nextInboxRow(list, selectedKey);
    if (!next && cleared) recordLastCleared(wsId, cleared);
    const nextKey = next ? inboxRowKey(next) : "";
    recordTransition(wsId, nextKey, "next");
    setSelectedKey(nextKey);
  };
  // A batch archive that takes the open row moves on to the next one it leaves.
  const archiveBatch = (
    archive: () => Promise<{ count: number }>,
    archives: (item: InboxItem) => boolean,
    failed: string,
  ) => {
    const open = listedItems.find((i) => inboxRowKey(i) === selectedKey);
    const takesOpen = !!open && archives(open);
    const takenIds = takesOpen ? collectBatchArchiveIds(queryClient, wsId, archives) : [];
    void archiveBatchWithUndo(archive, archives, failed, takesOpen ? selectedKey : null);
    if (!takesOpen) return;
    leaveRow(wsId, selectedKey);
    const next = nextInboxRow(listedItems, selectedKey, archives);
    if (!next && takenIds.length > 0) {
      recordLastCleared(wsId, {
        kind: "batch_archived",
        count: takenIds.length,
        itemIds: takenIds,
        key: selectedKey,
        clearedAt: Date.now(),
      });
    }
    setSelectedKey(next ? inboxRowKey(next) : "");
  };

  // Toasts live in these shared handlers so every archive surface confirms alike.
  const handleArchive = (id: string) => {
    const target = listedItems.find((i) => i.id === id);
    const cleared: LastInboxCleared | undefined = target
      ? {
          kind: "archived",
          itemId: target.id,
          key: inboxRowKey(target),
          label: clearedLabelForItem(target),
          href: inboxRowHref(wsPaths, target),
          clearedAt: Date.now(),
        }
      : undefined;
    advanceSelectionPast(id, listedItems, cleared);
    // Archived already: moving on is all that is left.
    if (settledRow?.item.id === id) return;
    archiveWithUndo(id, target ? inboxRowKey(target) : id);
  };

  const handleUnarchive = (id: string) => {
    advanceSelectionPast(id, listedItems);
    void restoreInbox(id);
  };

  // An issue's bar (`IssueInboxBar`) says, and moves on, when its row left
  // the Inbox on its own; the page does for a notification without an issue.
  const settledNotice = settledRow && !settledRow.item.issue_id ? settledRow : null;
  const settledCause = useMoveOnAfterAutoArchive({
    settled: settledNotice,
    next: settledNotice ? nextInboxRow(listedItems, settledNotice.key) : null,
    onNext: () => {
      if (!settledNotice) return;
      advanceSelectionPast(settledNotice.item.id, listedItems, {
        kind: "archived",
        itemId: settledNotice.item.id,
        key: settledNotice.key,
        label: clearedLabelForItem(settledNotice.item),
        href: inboxRowHref(wsPaths, settledNotice.item),
        clearedAt: Date.now(),
      });
    },
  });

  const unsubscribable = useInboxUnsubscribable(wsId);
  const unsubscribeArchive = useUnsubscribeArchive();
  const handleUnsubscribeArchive = (id: string, scope: InboxUnsubscribeScope = "issue") => {
    const item = listedItems.find((i) => i.id === id);
    if (!item?.issue_id) return;
    advanceSelectionPast(id, listedItems, {
      kind: "unsubscribed",
      itemId: item.id,
      issueId: item.issue_id,
      key: inboxRowKey(item),
      label: clearedLabelForItem(item),
      href: inboxRowHref(wsPaths, item),
      undoable: scope === "issue",
      clearedAt: Date.now(),
    });
    unsubscribeArchive({ id, issueId: item.issue_id }, scope);
  };

  // An open issue's E belongs to its bar (`IssueInboxBar`).
  useInboxShortcut(
    "archiveInboxItem",
    selected && (isArchivedView || !selected.issue_id)
      ? () => (isArchivedView ? handleUnarchive(selected.id) : handleArchive(selected.id))
      : null,
  );

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

  const handleArchiveAll = () =>
    archiveBatch(() => archiveAllMutation.mutateAsync(), () => true, t(($) => $.errors.archive_all_failed));

  const handleArchiveAllRead = () =>
    archiveBatch(
      () => archiveAllReadMutation.mutateAsync(),
      (item) => item.read === true,
      t(($) => $.errors.archive_all_read_failed),
    );

  // The command bar calls the page's own handlers, so archiving from it moves
  // the selection and toasts exactly like the row actions do.
  const commandBarHandlersRef = useRef({
    handleArchive,
    handleMarkUnread,
    handleMarkAllRead,
    handleArchiveAllRead,
  });
  useEffect(() => {
    commandBarHandlersRef.current = {
      handleArchive,
      handleMarkUnread,
      handleMarkAllRead,
      handleArchiveAllRead,
    };
  });
  const commandBarItem = isArchivedView ? null : selected;
  useRegisterCommandBarContext(
    useMemo(
      () => ({
        kind: "inbox",
        item: commandBarItem,
        archive: (id: string) => commandBarHandlersRef.current.handleArchive(id),
        markUnread: (id: string) => commandBarHandlersRef.current.handleMarkUnread(id),
        markAllRead: () => commandBarHandlersRef.current.handleMarkAllRead(),
        archiveAllRead: () => commandBarHandlersRef.current.handleArchiveAllRead(),
      }),
      [commandBarItem],
    ),
  );

  const handleArchiveCompleted = () =>
    archiveBatch(
      () => archiveCompletedMutation.mutateAsync(),
      (item) => {
        const category = item.issue_status ? categoryOf(item.issue_status) : null;
        return category === "done" || category === "closed";
      },
      t(($) => $.errors.archive_completed_failed),
    );

  // -- Shared sub-components --------------------------------------------------

  const tabLabel = {
    inbox: t(($) => $.page.title),
    snoozed: t(($) => $.list.snoozed_title),
    archived: t(($) => $.list.archived_title),
  }[tab];

  // A phone's list header: the Inbox's tabs, as the Work panel's section has them.
  const listHeader = (
    <PanelHeader
      as="h1"
      title={
        <InboxTabs
          tab={tab}
          onSelect={setTab}
          unread={unreadCount}
          snoozed={snoozed.rows.length}
          size="page"
          className="text-body font-medium"
        />
      }
      leading={<CollapsedNavTrigger />}
    >
      {!isSnoozedView && (
        <InboxFilterMenu
          wsId={wsId}
          items={filterRows}
          priorityFilterSupport={priorityFilterSupport}
          issueFieldsSupport={issueFieldsSupport}
          activeIssueIds={activeIssueIds}
          archived={isArchivedView}
        />
      )}
      {/* Batch actions are the live Inbox's only. Every entry archives from it,
          so offering them while another list is on screen reads as "archive
          all of these" and does the opposite of what it looks like. */}
      {tab === "inbox" && (
      <DropdownMenu>
        <DropdownMenuTrigger
          render={
            <IconButton
              label={tCommon(($) => $.more_actions)}
              tooltipSide="bottom"
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
      )}
    </PanelHeader>
  );

  const liveLoadFailed = (
    <LoadFailed
      icon={<Inbox className="mb-3 h-8 w-8 text-faint-foreground" />}
      message={t(($) => $.errors.load_failed)}
      retryLabel={t(($) => $.list.retry)}
      retrying={inboxQuery.isFetching}
      onRetry={() => void inboxQuery.refetch()}
    />
  );

  const list = isSnoozedView ? (
    snoozed.load === "failed" ? (
      <div className="flex-1 min-h-0 overflow-y-auto">
        <LoadFailed
          icon={<AlarmClock className="mb-3 h-8 w-8 text-faint-foreground" />}
          message={t(($) => $.errors.load_failed)}
          retryLabel={t(($) => $.list.retry)}
          retrying={snoozed.retrying}
          onRetry={snoozed.retry}
        />
      </div>
    ) : (
      <SnoozedInboxList wsId={wsId} rows={snoozed.rows} selectedKey={selectedKey} onSelect={setSelectedKey} />
    )
  ) : isArchivedView && archivedError ? (
    <div className="flex-1 min-h-0 overflow-y-auto">
      <LoadFailed
        icon={<Archive className="mb-3 h-8 w-8 text-faint-foreground" />}
        message={t(($) => $.errors.archived_load_failed)}
        retryLabel={t(($) => $.list.retry)}
        retrying={archiveQuery.isFetching}
        onRetry={() => void archiveQuery.refetch()}
      />
    </div>
  ) : liveFailed ? (
    <div className="flex-1 min-h-0 overflow-y-auto">{liveLoadFailed}</div>
  ) : (
    <InboxContextMenuProvider
      view={view}
      actions={{
        onMarkRead: handleMarkRead,
        onMarkUnread: handleMarkUnread,
        onAction: isArchivedView ? handleUnarchive : handleArchive,
        onDelete: (id) => setDeleteTarget(selectionItems.find((item) => item.id === id) ?? null),
        onUnsubscribeArchive: handleUnsubscribeArchive,
        canUnsubscribe: unsubscribable,
      }}
    >
      <InboxList
        items={visibleItems}
        view={view}
        selectedKey={selectedKey}
        onLoadMore={isArchivedView && archiveQuery.hasNextPage ? loadNextArchivedPage : undefined}
        loadingMore={archiveQuery.isFetchingNextPage}
        loadMoreError={archiveQuery.isFetchNextPageError}
        onSelect={handleSelect}
        onAction={isArchivedView ? handleUnarchive : handleArchive}
        emptyLabel={
          hasActiveFilters && visibleItems.length === 0
            ? t(($) => $.filters.empty)
            : undefined
        }
        emptyAction={
          hasActiveFilters && visibleItems.length === 0 ? (
            <Button
              variant="outline"
              size="sm"
              onClick={() => clearFilters(wsId)}
            >
              {t(($) => $.filters.clear)}
            </Button>
          ) : undefined
        }
      />
      {deleteTarget && (
        <InboxDeleteDialog
          key={deleteTarget.id}
          item={deleteTarget}
          view={view}
          onClose={() => setDeleteTarget(null)}
          onDeleted={(id) => {
            advanceSelectionPast(id, listedItems);
            toast.success(t(($) => $.toasts.deleted));
          }}
        />
      )}
    </InboxContextMenuProvider>
  );

  const listPanel = (
    <>
      {listHeader}
      {!isSnoozedView && <InboxFilterChipsBar wsId={wsId} filters={effectiveFilters} />}
      {lookupLoading && (
        <p role="status" className="shrink-0 border-b px-3 py-2 text-caption text-muted-foreground">
          {t(($) => $.list.loading_selection)}
        </p>
      )}
      {lookupError && (
        <div role="alert" className="flex shrink-0 items-center gap-2 border-b px-3 py-2">
          <p className="flex-1 text-caption text-muted-foreground">
            {t(($) => $.errors.archived_lookup_failed)}
          </p>
          <Button variant="outline" size="sm" disabled={lookup.isFetching || presence.isFetching} onClick={() => void (lookup.isError ? lookup.refetch() : presence.refetch())}>
            {t(($) => $.list.retry)}
          </Button>
        </div>
      )}
      {list}
    </>
  );

  // Compact widths only: the detail fills the screen there, so the trip back to
  // the list has to live inside it. On desktop the list is still on screen in
  // the left panel and needs no back control at all.
  //
  // The detail owns the whole compact screen in four states — loaded, loading,
  // not-found and crashed — so this control is threaded into all four below.
  // Miss one and that state strands the reader with no way out.
  const compactBackAction = isCompact ? (
    <Button
      variant="ghost"
      size="sm"
      onClick={() => setSelectedKey("")}
      className="-ml-2 shrink-0 gap-1.5 text-muted-foreground"
    >
      <ArrowLeft className="h-4 w-4" />
      {/* Back goes to the list the user came FROM, so the label has to
          name it — "Inbox" here would be a lie about the destination. */}
      {tab === "inbox" ? t(($) => $.page.back) : tabLabel}
    </Button>
  ) : undefined;

  const compactBackBar = compactBackAction ? (
    <div className={cn("flex h-12 shrink-0 items-center gap-2 border-b", PAGE_GUTTER)}>
      {compactBackAction}
    </div>
  ) : null;

  // A notification without an issue that left the Inbox on its own: say so
  // above it, with the way back and the way on. An issue's bar does this for
  // an issue's notification.
  const keptNotice =
    settledNotice && detailItem && inboxRowKey(detailItem) === settledNotice.key ? (
      <InboxKeptActions
        label={keptText.notice(settledNotice, settledCause)}
        onUndo={() => void restoreInbox(settledNotice.item.id, { quiet: true })}
        next={nextInboxRow(listedItems, settledNotice.key)}
        onNext={() => advanceSelectionPast(settledNotice.item.id, listedItems)}
      />
    ) : null;

  // The issue the detail shows: the open notification's, or on the Snoozed
  // tab the snoozed issue itself.
  const detailIssueId = isSnoozedView ? detailKey || null : (detailItem?.issue_id ?? null);
  const inboxHost = (issueId: string): IssueDetailInboxHost => ({
    // A phone's back control already names the list.
    crumb: isCompact
      ? undefined
      : {
          href: inboxTabHref(wsPaths, tab),
          className: "flex min-w-0 items-center gap-1",
          label: (
            <>
              <Inbox className="size-3.5 shrink-0" />
              <span className="truncate">{t(($) => $.page.title)}</span>
            </>
          ),
        },
    item: tab === "inbox" ? (detailItem ?? undefined) : undefined,
    bar: (issueActions) =>
      isArchivedView && detailItem ? (
        <InboxArchivedBar item={detailItem} onRestore={() => handleUnarchive(detailItem.id)} />
      ) : (
        <IssueInboxBar issueId={issueId} issueActions={issueActions} movingOn={detailSwapping} />
      ),
  });

  const detailContent = detailIssueId ? (
    // Key by issue_id (not inbox-item id): a new comment/reaction generates a
    // new inbox notification for the same issue, and the dedup helper picks the
    // newest one — keying on its id would remount IssueDetail on every event,
    // wiping the comment composer draft and resetting scroll position.
    <ErrorBoundary
      resetKeys={[detailIssueId]}
      // The default fallback is a bare message card. On a phone it would be the
      // only thing on screen, so it has to carry the way back too — the bar is
      // the point here, the message is whatever the boundary caught.
      fallback={compactBackAction ? ({ error }) => (
        <div className="flex flex-1 min-h-0 flex-col">
          {compactBackBar}
          <div className="flex flex-1 min-h-0 items-center justify-center px-4 text-center text-body text-muted-foreground">
            {error.message}
          </div>
        </div>
      ) : undefined}
    >
      {/* Shift+Click on an issue mention peeks it over the detail pane. */}
      <IssuePeekHost key={detailIssueId}>
        <IssueDetail
          key={detailIssueId}
          issueId={detailIssueId}
          defaultSidebarOpen={false}
          layoutId="multica_inbox_issue_detail_layout"
          highlightCommentId={landingCommentId}
          highlightRequestToken={highlightRequestToken}
          // A row clicked on another page could not clear this page's record
          // that it already landed there.
          highlightRequestPending={
            !!landingRequest && !landingRequest.landed && landingRequest.issueId === detailIssueId
          }
          onHighlightLanded={() => {
            if (landingRequest) completeLanding(landingRequest.token);
          }}
          highlightAppRequestId={
            detailItem?.type.startsWith("app_") ? (detailItem.details?.request_id ?? undefined) : undefined
          }
          // The inbox reads the row it selected itself (see the auto-read
          // effect above), and must keep a row marked unread while it is open.
          readsInbox={false}
          leadingAction={compactBackAction}
          inbox={inboxHost(detailIssueId)}
          onDelete={() => {
            // Issue deletion CASCADE-deletes the inbox item server-side, and the
            // issue:deleted WS event prunes it from the inbox cache. Just clear
            // the selection — calling archive here would 404 on a row that no
            // longer exists.
            setSelectedKey("");
          }}
        />
      </IssuePeekHost>
    </ErrorBoundary>
  ) : detailItem ? (
    <>
    {keptNotice}
    <div className="p-6">
      <h2 className="text-title font-semibold">
        {isAutopilotQuotaNotice(detailItem.type)
          ? typeLabels[detailItem.type]
          : getInboxDisplayTitle(detailItem)}
      </h2>
      <p className="mt-1 text-body text-muted-foreground">
        {typeLabels[detailItem.type]} · {timeAgo(detailItem.created_at)}
      </p>
      {isAutopilotQuotaNotice(detailItem.type) ? (
        <AutopilotQuotaNotice
          item={detailItem}
          onOpenRecovery={showAutopilotQuotaRecoveryPrompt}
        />
      ) : detailItem.type === "project_owner_joined" && detailItem.details?.joined_by ? (
        <div className="mt-4 text-body leading-relaxed text-foreground">
          {t(($) => $.detail.project_owner_joined_body, { name: detailItem.details.joined_by })}
        </div>
      ) : detailItem.body ? (
        <div className="mt-4 whitespace-pre-wrap text-body leading-relaxed text-foreground">
          {detailItem.body}
        </div>
      ) : null}
      {detailItem.details?.request_id && detailItem.type.startsWith("app_") && (
        <InboxAppRequest
          requestId={detailItem.details.request_id}
          chatSessionId={detailItem.details.chat_session_id}
        />
      )}
      {isQuickCreateOutcome(detailItem.type) && detailItem.details?.original_prompt && (
        <div className="mt-4 rounded-md border bg-muted/40 p-3">
          <p className="text-caption font-medium text-muted-foreground">
            {t(($) => $.detail.original_input)}
          </p>
          <p className="mt-1 whitespace-pre-wrap text-body">{detailItem.details.original_prompt}</p>
        </div>
      )}
      <div className="mt-4 flex gap-2">
        {(detailItem.type === "doc_comment" || detailItem.type === "doc_mentioned") &&
          detailItem.details?.doc_id && (
            <Button
              size="sm"
              variant="outline"
              render={
                <AppLink
                  href={`${wsPaths.docDetail(detailItem.details.doc_id)}${
                    detailItem.details.thread_id
                      ? `?thread=${encodeURIComponent(detailItem.details.thread_id)}`
                      : ""
                  }`}
                />
              }
              nativeButton={false}
            >
              {t(($) => $.detail.open_doc)}
            </Button>
          )}
        {detailItem.type === "project_owner_joined" && detailItem.details?.project_id && (
          <Button
            size="sm"
            variant="outline"
            render={<AppLink href={wsPaths.projectDetail(detailItem.details.project_id)} />}
            nativeButton={false}
          >
            {t(($) => $.detail.open_project)}
          </Button>
        )}
        {detailItem.type === "quick_create_failed" &&
          detailItem.details?.source_context_id &&
          detailItem.details?.task_id && (
            <Button
              size="sm"
              data-testid="retry-source-context"
              disabled={retrySourceContextMutation.isPending}
              onClick={async () => {
                try {
                  await retrySourceContextMutation.mutateAsync(detailItem.details!.task_id!);
                  toast.success(t(($) => $.toasts.source_context_retry_started));
                } catch (error) {
                  toast.error(
                    error instanceof ApiError &&
                      errorCode(error) === "source_context_retry_unavailable"
                      ? t(($) => $.errors.source_context_retry_unavailable)
                      : t(($) => $.errors.source_context_retry_failed),
                  );
                }
              }}
            >
              {t(($) => $.detail.retry_with_context)}
            </Button>
          )}
        {isQuickCreateOutcome(detailItem.type) && (
          <Button
            size="sm"
            onClick={() => {
              // Seed the legacy advanced form with the original prompt so the
              // user can recover their input in the full editor instead of
              // retyping. The agent picker hint becomes the assignee
              // candidate (still editable).
              const prompt = detailItem.details?.original_prompt ?? "";
              const agentId = detailItem.details?.agent_id;
              useIssueDraftStore.getState().setManual({
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
        {/* Mirrors the row action: the button always reverses the view the
            item is being read in. */}
        <Tooltip>
          <TooltipTrigger
            render={
              isArchivedView ? (
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => handleUnarchive(detailItem.id)}
                >
                  <ArchiveRestore className="mr-1.5 h-3.5 w-3.5" />
                  {t(($) => $.detail.unarchive)}
                </Button>
              ) : (
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => handleArchive(detailItem.id)}
                >
                  <Archive className="mr-1.5 h-3.5 w-3.5" />
                  {t(($) => $.detail.archive)}
                </Button>
              )
            }
          />
          <TooltipContent side="bottom">
            {isArchivedView ? t(($) => $.detail.unarchive) : t(($) => $.detail.archive)}
            <ActionShortcutKeycaps action="archiveInboxItem" />
          </TooltipContent>
        </Tooltip>
      </div>
    </div>
    </>
  ) : null;

  // -- Compact layout: list / detail toggle -----------------------------------

  if (isCompact) {
    if (viewLoading) {
      return (
        <div className="flex flex-1 flex-col min-h-0">
          <div className="flex h-12 shrink-0 items-center pl-5 pr-2.5">
            <Skeleton className="h-5 w-16" />
          </div>
          <div className="flex-1 min-h-0 overflow-y-auto space-y-1 p-2">
            {Array.from({ length: 5 }).map((_, i) => (
              <div key={i} className="flex items-center gap-3 px-2 py-2.5">
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

    // Compact: show detail full-screen when an item is selected. The two kinds
    // of selection get their chrome from different places, so they render
    // differently — `InboxItem.issue_id` is nullable and a null one is a plain
    // notification (a failed quick-create, say), not an issue.
    if (detailIssueId) {
      // No scroll container and no back bar of our own: `IssueDetail` owns
      // both, and takes the way back through `leadingAction`. Wrapping it in
      // an `overflow-y-auto` used to collapse its inner scroller to content
      // height, which took its header (and the done/pin/more/sidebar actions
      // in it) out of the pinned position, made `position: sticky` inside it a
      // no-op, and pointed both scroll restoration and the timeline
      // virtualizer at an element that never scrolls. This wrapper only has to
      // give the detail a definite height to fill.
      return <div className="flex flex-1 flex-col min-h-0">{detailContent}</div>;
    }

    if (detailItem) {
      // A notification body is a plain block with no header to host a leading
      // slot and no scroller of its own, so this branch keeps supplying both.
      return (
        <div className="flex flex-1 flex-col min-h-0">
          {compactBackBar}
          <div className="flex-1 min-h-0 overflow-y-auto">{detailContent}</div>
        </div>
      );
    }

    // Compact: full-screen list
    return <div className="flex flex-1 flex-col min-h-0">{listPanel}</div>;
  }

  // -- Desktop layout ----------------------------------------------------------
  // Every list is the Inbox section of the Work panel beside this page, so the
  // page shows only the detail.

  const handleClearFiltersAndContinue = () => {
    clearFilters(wsId);
    const first = items[0];
    if (!isCompact && first) push?.(inboxRowHref(wsPaths, first));
  };

  const emptyDetail = (
    <>
      <PageHeader>
        <h1 className="text-body font-semibold">{tabLabel}</h1>
      </PageHeader>
      {liveFailed ? (
        <div className="flex min-h-0 flex-1 flex-col justify-center">{liveLoadFailed}</div>
      ) : (
        <InboxEmptyState
          wsId={wsId}
          tab={tab}
          visibleCount={isSnoozedView ? snoozed.rows.length : visibleItems.length}
          totalActiveCount={items.length}
          hasActiveFilters={hasActiveFilters}
          inboxZeroRises={inboxZeroRises}
          isCompact={isCompact}
          onClearFiltersAndContinue={handleClearFiltersAndContinue}
        />
      )}
    </>
  );

  if (viewLoading) {
    return (
      <div className="min-w-0 flex-1 p-6">
        <Skeleton className="h-6 w-48" />
        <Skeleton className="mt-4 h-4 w-32" />
      </div>
    );
  }

  return (
    <div className="flex h-full min-h-0 min-w-0 flex-1 flex-col">
      {detailContent ?? emptyDetail}
    </div>
  );
}

/** A list that could not load, with another try; never an empty list. */
function LoadFailed({
  icon,
  message,
  retryLabel,
  retrying,
  onRetry,
}: {
  icon: ReactNode;
  message: string;
  retryLabel: string;
  retrying: boolean;
  onRetry: () => void;
}) {
  return (
    <div role="alert" className="flex flex-col items-center justify-center py-16 text-muted-foreground">
      {icon}
      <p className="text-body">{message}</p>
      <Button variant="outline" size="sm" className="mt-3" disabled={retrying} onClick={onRetry}>
        {retryLabel}
      </Button>
    </div>
  );
}
