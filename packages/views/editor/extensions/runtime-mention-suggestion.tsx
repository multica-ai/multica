"use client";

import {
  forwardRef,
  useCallback,
  useEffect,
  useImperativeHandle,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { useQuery, useQueries, type QueryClient } from "@tanstack/react-query";
import { getCurrentWsId } from "@multica/core/platform";
import { flattenIssueBuckets, issueKeys, issueTasksOptions } from "@multica/core/issues/queries";
import { runtimeKeys, runtimeListOptions } from "@multica/core/runtimes/queries";
import { issueStatusCategory } from "@multica/core/issues";
import { isBuiltInIssueStatus } from "@multica/core/issue-statuses";
import { useIssueStatuses } from "@multica/core/issue-statuses/hooks";
import { workspaceKeys, agentListOptions, memberListOptions, squadListOptions } from "@multica/core/workspace/queries";
import { useAuthStore } from "@multica/core/auth";
import { useAgentPresenceDetail } from "@multica/core/agents";
import { useCurrentWorkspace } from "@multica/core/paths";
import { canAssignAgentToIssue } from "@multica/core/permissions";
import { searchIssues, searchProjects } from "@multica/core/search-index";
import {
  isIssueDirectHit,
  isProjectDirectHit,
} from "@multica/core/search/cancelled-rank";
import { isImeComposing } from "@multica/core/utils";
import { resolveAgentRuntimeProvider } from "@multica/core/agents";
import { runtimeDisplayName } from "@multica/core/runtimes";
import { isMentionBoundaryAfter } from "@multica/core/markdown";
import type {
  Issue,
  ListIssuesCache,
  MemberWithUser,
  Agent,
  Squad,
  AgentRuntime,
  AgentTask,
  MemberRole,
} from "@multica/core/types";
import { ListTodo } from "@multica/ui/icons";
import { ActorAvatar } from "../../common/actor-avatar";
import { StatusIcon } from "../../issues/components/status-icon";
import { ProjectIcon } from "../../projects/components/project-icon";
import { runtimeOwnerDisplayName } from "../../runtimes/components/runtime-identity";
import { ArchivedProjectTag } from "../../projects/components/project-archive";
import { useT } from "../../i18n";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import { cn } from "@multica/ui/lib/utils";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { AVATAR_SIZE_PX } from "@multica/ui/lib/avatar-size";
import type {
  IssueStatus,
  IssueStatusCategory,
  ProjectStatus,
} from "@multica/core/types";
import { PROJECT_STATUS_CONFIG } from "@multica/core/projects/config";
import { STATUS_CONFIG } from "@multica/core/issues/config";
import type { SuggestionOptions } from "@tiptap/suggestion";
import type { Node as ProseMirrorNode } from "@tiptap/pm/model";
import { PluginKey } from "@tiptap/pm/state";
import {
  getRecencyMap,
  recordMentionUsage,
  sortUserItemsByRecency,
} from "./mention-recency";
import { matchesPinyin } from "./pinyin-match";
import { createSuggestionPopupRender } from "./suggestion-popup";
import {
  isPickerAcceptKey,
  pickerNavigationDirection,
} from "../../common/picker-keys";
import {
  agentEffectiveRuntimeId as agentListRuntimeId,
  isAgentRuntimeBound,
} from "@multica/core/agents";
import {
  isRuntimeDefaultModelLabel,
  resolveRuntimeModelDisplayLabel,
} from "../../agents/runtime-model-label";
import { isTriggerArmedAt } from "./suggestion-trigger-arming";
import { blockedReasonLabel } from "../../issues/blocked-trigger-copy";
import {
  mentionCliFromProvider,
  mentionCliFilterTokens,
  mentionCliTagLabel,
  MentionCliTextTag,
} from "./mention-cli-brand";
import {
  MentionAvatarBadge,
  mentionPresenceState,
  type MentionPresenceState,
} from "./mention-avatar-badge";

// These glossary-protected product terms intentionally stay untranslated.

// Types

import type { MentionItem } from "./mention-suggestion";

interface MentionListProps {
  items: MentionItem[];
  query: string;
  command: (item: MentionItem) => void;
  contextSearch?: boolean;
  /**
   * The editor's contenteditable while the combobox pattern is stamped on
   * it — this list owns the highlight, so it also keeps the
   * aria-activedescendant pointer on the combobox in sync.
   */
  editorDom?: HTMLElement | null;
  taskState?: {
    loading: boolean;
    error: boolean;
    retry: () => void;
  };
}

export interface MentionListRef {
  onKeyDown: (props: { event: KeyboardEvent }) => boolean;
}

// Group items by section

interface MentionGroup {
  label: string;
  items: MentionItem[];
}

// ---------------------------------------------------------------------------
// Type segments
// ---------------------------------------------------------------------------

export type MentionSegment = "all" | "agent" | "member" | "squad" | "issue";

const MENTION_SEGMENTS: MentionSegment[] = [
  "all",
  "agent",
  "member",
  "squad",
  "issue",
];

/** Which segment a row belongs to. Projects count as issue-like entities. */
function segmentOfItem(item: MentionItem): Exclude<MentionSegment, "all"> {
  if (item.type === "agent") return "agent";
  if (item.type === "squad") return "squad";
  if (item.type === "issue" || item.type === "project") return "issue";
  return "member";
}

/**
 * Group order follows the design: current-issue runtime agents → my
 * runtime agents → members → squads → agents → issues, with the pre-existing
 * curated context groups (current page / recently viewed) and the server
 * search bucket kept ahead of the typed sections they came from. Cancelled
 * rows always stay last. A type segment filters rows across every group, so
 * the curated buckets stay reachable from non-"all" segments too.
 */
function groupItems(items: MentionItem[], query: string): MentionGroup[] {
  const issueRuntime: MentionItem[] = [];
  const requesterRuntime: MentionItem[] = [];
  const current: MentionItem[] = [];
  const recent: MentionItem[] = [];
  const search: MentionItem[] = [];
  const members: MentionItem[] = [];
  const squads: MentionItem[] = [];
  const agents: MentionItem[] = [];
  const issues: MentionItem[] = [];
  const cancelled: MentionItem[] = [];

  // an agent that appears in several buckets renders ONCE per
  // runtime context — the current-task bucket wins, then my-runtimes, then
  // the workspace agents bucket. the key is agent id + resolved
  // runtime, not agent id alone. When the current task ran on someone else's
  // runtime, the requester row resolves to the viewer's own runtime; the two
  // rows differ in runtime identity, so dropping the requester row removed the only way to invoke
  // this agent on my runtime from this issue. Rows resolving to the SAME
  // runtime stay deduplicated, current task winning as before.
  const seenRuntimeKeys = new Set<string>();
  const claimAgent = (item: MentionItem): boolean => {
    if (item.type !== "agent") return true;
    const key = `${item.id}:${item.runtime?.id ?? "none"}`;
    if (seenRuntimeKeys.has(key)) return false;
    seenRuntimeKeys.add(key);
    return true;
  };

  for (const item of items) {
    if (isDemotedCancelled(item, query)) {
      cancelled.push(item);
    } else if (item.group === "issue_runtime") {
      if (claimAgent(item)) issueRuntime.push(item);
    } else if (item.group === "requester_runtime") {
      if (claimAgent(item)) requesterRuntime.push(item);
    } else if (item.group === "current") {
      current.push(item);
    } else if (item.group === "recent") {
      recent.push(item);
    } else if (item.group === "search") {
      search.push(item);
    } else if (item.type === "issue" || item.type === "project") {
      issues.push(item);
    } else if (item.type === "squad") {
      squads.push(item);
    } else if (item.type === "agent") {
      if (claimAgent(item)) agents.push(item);
    } else {
      members.push(item);
    }
  }

  const groups: MentionGroup[] = [];
  if (issueRuntime.length > 0) {
    groups.push({ label: "Issue runtime agents", items: issueRuntime });
  }
  if (requesterRuntime.length > 0) {
    groups.push({ label: "My runtime agents", items: requesterRuntime });
  }
  if (current.length > 0) groups.push({ label: "Current", items: current });
  if (recent.length > 0) groups.push({ label: "Recent", items: recent });
  if (members.length > 0) groups.push({ label: "Members", items: members });
  if (squads.length > 0) groups.push({ label: "Squads", items: squads });
  if (agents.length > 0) groups.push({ label: "Agents", items: agents });
  if (search.length > 0) groups.push({ label: "Search", items: search });
  if (issues.length > 0) groups.push({ label: "Issues", items: issues });

  // Always last: no cancelled row of any type may precede a live one.
  if (cancelled.length > 0)
    groups.push({ label: "Cancelled", items: cancelled });
  return groups;
}

// MentionList — the popup rendered inside the editor

const MAX_CONTEXT_ITEMS = 20;
const SERVER_ISSUE_SEARCH_LIMIT = 20;
const SERVER_PROJECT_SEARCH_LIMIT = 6;
const SERVER_CONTEXT_SEARCH_LIMIT = 8;
const SERVER_SEARCH_DEBOUNCE_MS = 150;

function mentionItemKey(item: MentionItem): string {
  if (item.type === "agent" && item.agentRuntimeGroup) {
    return `${item.type}:${item.id}:${item.agentRuntimeGroup}`;
  }
  return `${item.type}:${item.id}`;
}

function mergeMentionItems(...itemGroups: MentionItem[][]): MentionItem[] {
  const seen = new Set<string>();
  const merged: MentionItem[] = [];

  for (const item of itemGroups.flat()) {
    const key = mentionItemKey(item);
    if (seen.has(key)) continue;
    seen.add(key);
    merged.push(item);
  }

  return merged;
}

/**
 * Cancelled work is abandoned work: keep it reachable, but never ahead of a
 * live match and never at the cost of a live match's slot.
 *
 * Two independent reasons the picker needs this even though the search API
 * already ranks cancelled results last:
 *
 * 1. Cached rows are merged BEFORE the server rows and the list is only then
 *    bounded by the context limit, so a locally cached cancelled issue can
 *    outrank backend results and push active work out of the window.
 * 2. Context mentions aggregate two independently ranked responses — issues
 *    then projects, both tagged `search` — so a cancelled project still lands
 *    above a live issue. Per-type ranking cannot fix a cross-type list.
 *
 * Exempt, matching the server and the other search surfaces:
 *
 * - Direct hits. An exact identifier, a bare number, or a full title means the
 *   user is targeting that one record; demoting it hides exactly what they
 *   asked for. Shared with the backend rule via isIssueDirectHit /
 *   isProjectDirectHit so the three surfaces cannot drift.
 * - The curated `current` / `recent` groups. They are explicit context rather
 *   than relevance hits, and "Current" has to survive the truncation even when
 *   the issue being viewed is itself cancelled.
 */
function isDemotedCancelled(item: MentionItem, query: string): boolean {
  if (isPinnedAboveTruncation(item, query)) return false;
  if (item.type === "issue") return item.statusCategory === "closed";
  if (item.type === "project") return item.projectStatus === "cancelled";
  return false;
}

/**
 * Rows that must survive the context limit: curated context and direct hits.
 * Exempting a direct hit from the demotion is not enough on its own —
 * the context limit runs on the merged list, so a direct hit sitting behind
 * 20 cached context candidates would still be cut. Pinning it to the front
 * keeps the record the user typed in full reachable, and it mirrors the
 * server, which already ranks direct hits first.
 *
 * Issue mention rows carry the identifier in `label` and the title in
 * `description`; project rows carry the title in `label`.
 */
function isPinnedAboveTruncation(item: MentionItem, query: string): boolean {
  if (item.group === "current" || item.group === "recent") return true;
  if (!query) return false;
  if (item.type === "issue") {
    return isIssueDirectHit(
      { identifier: item.label, title: item.description },
      query,
    );
  }
  if (item.type === "project") {
    return isProjectDirectHit({ title: item.label }, query);
  }
  return false;
}

/**
 * Stable three-tier partition applied BEFORE the context limit, so
 * cancelled rows give up their slot rather than merely their position:
 *
 *   pinned (curated context + direct hits) → live → cancelled
 *
 * Relative order within each tier is untouched, leaving the backend ranking in
 * charge of everything else. groupItems() then renders the cancelled tier as the
 * trailing section.
 */
function demoteCancelledItems(
  items: MentionItem[],
  query: string,
): MentionItem[] {
  const pinned: MentionItem[] = [];
  const live: MentionItem[] = [];
  const cancelled: MentionItem[] = [];

  for (const item of items) {
    if (isPinnedAboveTruncation(item, query)) pinned.push(item);
    else if (isDemotedCancelled(item, query)) cancelled.push(item);
    else live.push(item);
  }

  return pinned.length > 0 || cancelled.length > 0
    ? [...pinned, ...live, ...cancelled]
    : items;
}

// Tiptap calls items() on input, not when Query caches settle. Keep an open
// issue picker subscribed so cold loads and retries update without typing.
export const IssueMentionList = forwardRef<
  MentionListRef,
  MentionListProps & {
    client: QueryClient;
    issueId: string;
    buildItems: (query: string) => MentionItem[];
  }
>(function IssueMentionList({ client, issueId, buildItems, ...props }, ref) {
  const wsId = getCurrentWsId() ?? "";
  const taskQuery = useQuery({
    ...issueTasksOptions(issueId),
    retry: false,
    notifyOnChangeProps: "all",
  }, client);
  useQueries({
    queries: [
      runtimeListOptions(wsId),
      agentListOptions(wsId),
      memberListOptions(wsId),
      squadListOptions(wsId),
    ].map((options) => ({ ...options, enabled: !!wsId, notifyOnChangeProps: "all" as const })),
  }, client);
  return <MentionList
    {...props}
    ref={ref}
    items={buildItems(props.query)}
    taskState={{
      loading: taskQuery.isPending || taskQuery.isFetching,
      error: taskQuery.isError,
      retry: () => { void taskQuery.refetch(); },
    }}
  />;
});

export const MentionList = forwardRef<MentionListRef, MentionListProps>(
  function MentionList(
    { items, query, command, contextSearch = false, editorDom = null, taskState },
    ref,
  ) {
    const { t } = useT("editor");
    const { colorOf: statusColorOf, iconOf: statusIconOf } = useIssueStatuses(getCurrentWsId() ?? "");
    // Selection is tracked by item identity, NOT by a positional index. The
    // list is re-bucketed by groupItems() and grows asynchronously (server
    // search results), so a slot index is not a stable target — the row under
    // index N changes as the list reorders. selectedKey pins the highlight to
    // a specific item; the numeric index is derived from it against the SAME
    // order the popup renders (orderedItems). null means "no explicit pick yet"
    // → the first rendered row is highlighted by default.
    const [selectedKey, setSelectedKey] = useState<string | null>(null);
    const [serverItems, setServerItems] = useState<MentionItem[]>([]);
    const [isSearching, setIsSearching] = useState(false);
    const [searchedQuery, setSearchedQuery] = useState("");
    const itemRefs = useRef<(HTMLButtonElement | null)[]>([]);
    const normalizedQuery = query.trim();

    useEffect(() => {
      const q = normalizedQuery;
      setServerItems([]);

      if (!q) {
        setIsSearching(false);
        setSearchedQuery("");
        return;
      }

      const wsId = getCurrentWsId();
      if (!wsId) {
        setIsSearching(false);
        setSearchedQuery(q);
        return;
      }

      let cancelled = false;
      const controller = new AbortController();
      setIsSearching(true);

      const timer = setTimeout(() => {
        void (async () => {
          try {
            // One search failing must not discard the other's results.
            const [issues, projects] = await Promise.allSettled([
              searchIssues({
                q,
                limit: contextSearch ? SERVER_CONTEXT_SEARCH_LIMIT : SERVER_ISSUE_SEARCH_LIMIT,
                include_closed: true,
                signal: controller.signal,
              }),
              searchProjects({
                q,
                limit: SERVER_PROJECT_SEARCH_LIMIT,
                include_closed: true,
                include_archived: true,
                signal: controller.signal,
              }),
            ]);
            if (!cancelled && !controller.signal.aborted) {
              const results = [
                ...(issues.status === "fulfilled" ? issues.value.issues.map(issueToMention) : []),
                ...(projects.status === "fulfilled" ? projects.value.projects.map(projectToMention) : []),
              ];
              setServerItems(
                contextSearch
                  ? results.map((item) => ({ ...item, group: "search" as const }))
                  : results,
              );
            }
          } catch {
            // Aborted or network error: keep the synchronous cache results.
          } finally {
            if (!cancelled && !controller.signal.aborted) {
              setSearchedQuery(q);
              setIsSearching(false);
            }
          }
        })();
      }, SERVER_SEARCH_DEBOUNCE_MS);

      return () => {
        cancelled = true;
        clearTimeout(timer);
        controller.abort();
      };
    }, [contextSearch, normalizedQuery]);

    const displayItems = useMemo(() => {
      const currentServerItems =
        searchedQuery === normalizedQuery ? serverItems : [];
      let contextCount = 0;
      return demoteCancelledItems(
        mergeMentionItems(items, currentServerItems),
        normalizedQuery,
      ).filter((item) => {
        // Directory entries must remain complete across all type segments.
        // Only issue/project suggestions share the bounded search window.
        if (item.type !== "issue" && item.type !== "project") return true;
        return contextCount++ < MAX_CONTEXT_ITEMS;
      });
    }, [items, normalizedQuery, searchedQuery, serverItems]);

    // Counts and type segments share the grouped, deduplicated candidate set.
    // A current-task agent can also arrive from the requester's runtime.
    const allGroups = useMemo(
      () => groupItems(displayItems, normalizedQuery),
      [displayItems, normalizedQuery],
    );
    const [activeSegment, setActiveSegment] = useState<MentionSegment>("all");
    const segmentCounts = useMemo(() => {
      const counts: Record<MentionSegment, number> = {
        all: 0, agent: 0, member: 0, squad: 0, issue: 0,
      };
      for (const group of allGroups) {
        for (const item of group.items) {
          counts.all++;
          counts[segmentOfItem(item)]++;
        }
      }
      return counts;
    }, [allGroups]);
    const groups = useMemo(
      () => activeSegment === "all" ? allGroups : allGroups.map((group) => ({
        ...group,
        items: group.items.filter((item) => segmentOfItem(item) === activeSegment),
      })).filter((group) => group.items.length > 0),
      [activeSegment, allGroups],
    );
    // The rendered group order is also the keyboard selection index space.
    const orderedItems = useMemo(
      () => groups.flatMap((g) => g.items),
      [groups],
    );

    // Derive the numeric index from the pinned identity. If the selected item
    // is no longer in the list (query narrowed it away) or nothing is picked
    // yet, fall back to the first row. This self-heals across reorders and
    // async result arrival without ever force-resetting an active selection.
    const selectedIndex = useMemo(() => {
      const firstSelectable = orderedItems.findIndex(
        (item) => !item.disabledReason,
      );
      if (selectedKey === null) return firstSelectable;
      const i = orderedItems.findIndex(
        (it) => mentionItemKey(it) === selectedKey,
      );
      return i === -1 || orderedItems[i]?.disabledReason ? firstSelectable : i;
    }, [orderedItems, selectedKey]);

    useEffect(() => {
      if (selectedIndex >= 0) {
        itemRefs.current[selectedIndex]?.scrollIntoView({ block: "nearest" });
      }
    }, [selectedIndex]);

    // Combobox pattern: the editor element carries aria-activedescendant,
    // pointing at the highlighted option.
    useEffect(() => {
      if (!editorDom) return;
      if (selectedIndex >= 0) {
        editorDom.setAttribute(
          "aria-activedescendant",
          `mention-option-${selectedIndex}`,
        );
      } else {
        editorDom.removeAttribute("aria-activedescendant");
      }
      return () => {
        editorDom.removeAttribute("aria-activedescendant");
      };
    }, [editorDom, selectedIndex]);

    const selectItem = useCallback(
      (item: MentionItem | undefined) => {
        if (!item || item.disabledReason) return;
        const wsId = getCurrentWsId();
        if (wsId) recordMentionUsage(wsId, item);
        command(item);
      },
      [command],
    );

    useImperativeHandle(ref, () => ({
      onKeyDown: ({ event }) => {
        // IME is composing — don't intercept Enter/Arrow as picker actions;
        // those keys belong to the IME (Enter commits composition, etc).
        if (isImeComposing(event)) return false;
        // Arrow keys plus the Ctrl+N/J/P/K aliases the command bar accepts —
        // see pickerNavigationDirection.
        const direction = pickerNavigationDirection(event);
        if (direction !== null) {
          // With no rows, including while remote search is pending, the picker
          // has nothing to navigate. Let the host editor own the key instead.
          if (orderedItems.length === 0) return false;
          const selectableIndexes = orderedItems.flatMap((item, index) =>
            item.disabledReason ? [] : [index],
          );
          // Rows exist but all are disabled: keep the picker inert rather than
          // moving the caret behind the visible popup.
          if (selectableIndexes.length === 0) return true;
          const current = selectableIndexes.indexOf(selectedIndex);
          const delta = direction === "next" ? 1 : selectableIndexes.length - 1;
          const next =
            selectableIndexes[
              ((current === -1 ? 0 : current) + delta) %
                selectableIndexes.length
            ]!;
          setSelectedKey(mentionItemKey(orderedItems[next]!));
          return true;
        }
        // plan C: inside the mention picker plain Tab cycles the type
        // segment (Shift+Tab reverses) instead of accepting. This supersedes
        // the picker-wide Tab-accepts alias (MUL-3685) for this surface only;
        // Enter remains the accept key. Modifier+Tab still falls through to
        // focus navigation.
        if (
          event.key === "Tab" &&
          !event.ctrlKey &&
          !event.metaKey &&
          !event.altKey
        ) {
          event.preventDefault();
          const direction = event.shiftKey ? -1 : 1;
          setActiveSegment((current) => {
            const index = MENTION_SEGMENTS.indexOf(current);
            const next =
              (index + direction + MENTION_SEGMENTS.length) %
              MENTION_SEGMENTS.length;
            return MENTION_SEGMENTS[next] ?? "all";
          });
          return true;
        }
        // Enter is the canonical accept; the Tab alias is consumed above by
        // the segment cycler. Shift/modifier+Tab fall through to focus nav.
        if (isPickerAcceptKey(event)) {
          // An empty picker cannot accept anything, so preserve the editor's
          // newline, submit shortcut, and focus-navigation behavior.
          if (orderedItems.length === 0) return false;
          // A non-empty list can still have no selectable row when every item
          // is disabled. Keep those visible rows inert instead of falling through.
          if (selectedIndex < 0) return true;
          selectItem(orderedItems[selectedIndex]);
          return true;
        }
        return false;
      },
    }));

    const segmentLabels: Record<MentionSegment, string> = {
      all: t(($) => $.mention.segment_all),
      agent: t(($) => $.mention.segment_agents),
      member: t(($) => $.mention.segment_members),
      squad: t(($) => $.mention.segment_squads),
      issue: t(($) => $.mention.segment_issues),
    };

    // A picked type segment with zero matches must NOT swallow the whole
    // panel: the segment bar stays rendered so the user can always cycle or
    // click back to "all".
    const segmentBar = (
      <div
        role="tablist"
        aria-label={t(($) => $.mention.segment_label)}
        className="flex shrink-0 gap-1 px-3 pt-2.5"
      >
        {MENTION_SEGMENTS.map((segment) => {
          const active = segment === activeSegment;
          return (
            <button
              key={segment}
              type="button"
              role="tab"
              aria-selected={active}
              tabIndex={-1}
              className={cn(
                "shrink-0 whitespace-nowrap rounded-[7px] border border-transparent px-2.5 py-1 text-caption transition-colors",
                active
                  ? "bg-accent font-semibold text-foreground"
                  : "text-muted-foreground hover:bg-accent/60",
              )}
              onMouseDown={(event) => event.preventDefault()}
              onClick={() => setActiveSegment(segment)}
            >
              {segmentLabels[segment]}
              <span className="ml-1 font-mono text-[10px] tabular-nums text-muted-foreground">
                {segmentCounts[segment]}
              </span>
            </button>
          );
        })}
      </div>
    );

    const hasTaskRows = groups.some((group) => group.label === "Issue runtime agents");
    const showTaskState = (activeSegment === "all" || activeSegment === "agent") &&
      (taskState?.error || (taskState?.loading && !hasTaskRows));

    // the popup is one fixed-height shell — segment bar on top, a
    // single inner-scrolling list area, key hints pinned at the bottom. Every
    // tab and every state (rows, empty, loading) renders inside this same
    // shell, so switching tabs never resizes the popup. The fixed value is
    // the previous expanded height of the "all" tab, still clamped by the
    // floating-ui viewport budget (`--suggestion-available-height`) so tight
    // viewports keep the popup on-screen; the scroll area absorbs the rest.
    // This is the single height authority; do not add a second max-height
    // above the scroll area or the fixed shell can overflow the viewport.
    const panelClassName = cn(
      "flex flex-col overflow-hidden border bg-popover pt-1",
      "h-[min(420px,var(--suggestion-available-height,420px))] w-[clamp(360px,calc(100vw-24px),440px)] rounded-lg shadow-xl",
    );
    const footerHints = (
      <div className="z-10 flex shrink-0 gap-3.5 border-t border-border bg-popover px-4 py-2 text-micro text-muted-foreground">
        <span>{t(($) => $.mention.footer_move)}</span>
        <span>{t(($) => $.mention.footer_select)}</span>
        <span>{t(($) => $.mention.footer_segment)}</span>
        <span>{t(($) => $.mention.footer_close)}</span>
      </div>
    );

    if (orderedItems.length === 0 && groups.length === 0 && !showTaskState) {
      const isWaitingForServer =
        normalizedQuery !== "" &&
        (isSearching || searchedQuery !== normalizedQuery);

      // empty state: name the query ("No results for this query") and
      // hint at the type words; without a query the generic copy stands.
      const emptyMessage = isWaitingForServer
        ? t(($) => $.mention.searching)
        : normalizedQuery
          ? t(($) => $.mention.no_results_with_query, {
              query: normalizedQuery,
            })
          : t(($) => $.mention.no_results);
      const hintWords = [
        t(($) => $.mention.segment_agents),
        t(($) => $.mention.segment_members),
        t(($) => $.mention.segment_squads),
        t(($) => $.mention.segment_issues),
      ].join(" / ");
      const emptyBody = (
        <>
          <div>{emptyMessage}</div>
          {!isWaitingForServer && normalizedQuery && (
            <div className="mt-0.5 text-faint-foreground">
              {t(($) => $.mention.no_results_hint, { words: hintWords })}
            </div>
          )}
        </>
      );

      // even with zero rows the popup keeps the full fixed-height
      // shell — the segment bar stays reachable (kept it for picked
      // type segments; now "all" keeps it too) and the key hints hold their
      // position, so nothing jumps when a tab empties out.
      return (
        <div
          role="listbox"
          aria-label={t(($) => $.mention.listbox_label)}
          className={panelClassName}
        >
          {segmentBar}
          <div className="min-h-0 flex-1 overflow-y-auto overscroll-contain px-4 py-3 text-caption text-muted-foreground">
            {emptyBody}
          </div>
          {footerHints}
        </div>
      );
    }

    const groupLabel = (label: string): string => {
      if (label === "Issue runtime agents")
        return t(($) => $.mention.group_issue_runtime_agents);
      if (label === "My runtime agents")
        return t(($) => $.mention.group_my_runtime_agents);
      if (label === "Current") return t(($) => $.mention.group_current);
      if (label === "Recent") return t(($) => $.mention.group_recent);
      if (label === "Search") return t(($) => $.mention.group_search);
      if (label === "Members") return t(($) => $.mention.group_members);
      if (label === "Squads") return t(($) => $.mention.group_squads);
      if (label === "Agents") return t(($) => $.mention.group_agents);
      if (label === "Issues") return t(($) => $.mention.group_issues);
      if (label === "Cancelled") return t(($) => $.mention.group_cancelled);
      return label;
    };

    // Use the same complete group order for rendered rows and keyboard selection.
    let globalIndex = 0;

    const renderRows = (group: MentionGroup): ReactNode => {
      return (
        <>
          {group.items.map((item) => {
            const idx = globalIndex++;
            return (
              <MentionRow
                key={mentionItemKey(item)}
                item={item}
                optionId={`mention-option-${idx}`}
                variant={
                  activeSegment === "issue" && item.type === "issue"
                    ? "issue-table"
                    : "default"
                }
                groupLabel={groupLabel(group.label)}
                statusColor={
                  item.type === "issue" && item.status
                    ? statusColorOf(item.status)
                    : null
                }
                statusIcon={
                  item.type === "issue" && item.status
                    ? statusIconOf(item.status)
                    : null
                }
                selected={idx === selectedIndex}
                onSelect={() => selectItem(item)}
                buttonRef={(el) => {
                  itemRefs.current[idx] = el;
                }}
              />
            );
          })}
        </>
      );
    };

    // Only candidate content scrolls. The shell is fixed-height, so
    // `flex-1 min-h-0` gives the scroll area whatever the segment bar and the
    // footer leave over — and the footer never moves when rows come and go.
    // scrollIntoView cannot place the selected row underneath the key hints.
    return (
      <div
        data-mc="editor-menu"
        data-kind="mention"
        role="listbox"
        aria-label={t(($) => $.mention.listbox_label)}
        aria-activedescendant={
          selectedIndex >= 0 ? `mention-option-${selectedIndex}` : undefined
        }
        className={panelClassName}
      >
        {segmentBar}
        <div className="min-h-0 flex-1 overflow-y-auto overscroll-contain">
          {showTaskState && (
            <div className="px-4 pb-2 pt-4 text-caption text-muted-foreground">
              <div className="mb-2 flex items-center gap-2 text-micro font-semibold">
                <span aria-hidden className="size-1.5 bg-[#D97757] rounded-full" />
                {t(($) => $.mention.group_issue_runtime_agents)}
              </div>
              {taskState?.error && !taskState.loading ? (
                <div role="alert" className="flex items-center gap-2">
                  <span>{t(($) => $.mention.task_load_failed)}</span>
                  <button type="button" className="text-brand hover:underline" onMouseDown={(event) => event.preventDefault()} onClick={taskState.retry}>
                    {t(($) => $.mention.retry)}
                  </button>
                </div>
              ) : !hasTaskRows ? (
                <div role="status" aria-label={t(($) => $.mention.task_loading)} className="space-y-2">
                  {[0, 1, 2].map((row) => <Skeleton key={row} className="h-10 w-full" />)}
                </div>
              ) : null}
            </div>
          )}
          {groups.map((group) => {
            // Count is surfaced only for the runtime-agent sections, matching the
            // design where "My runtime agents" carries an explicit tally.
            const showCount =
              group.items.length > 0 &&
              (group.label === "Issue runtime agents" ||
                group.label === "My runtime agents");
            return (
              <div key={group.label}>
                {/*
                  Headings are decorative: grouping reaches the accessibility
                  tree through each row's sentence label instead.
                */}
                <div
                  aria-hidden
                  className="flex items-center gap-2 px-4 pb-2 pt-4 uppercase"
                >
                  {group.label === "Issue runtime agents" && (
                    // Same clay constant as the selected indicator (MENTION_CLAY).
                    <span
                      aria-hidden
                      className="size-1.5 shrink-0 rounded-full bg-[#D97757]"
                    />
                  )}
                  <span className="shrink-0 text-micro font-semibold tracking-[0.08em] text-muted-foreground">
                    {groupLabel(group.label)}
                  </span>
                  {showCount && (
                    <span className="shrink-0 text-micro tabular-nums text-muted-foreground">
                      {group.items.length}
                    </span>
                  )}
                  <span className="h-px flex-1 bg-border/60" />
                </div>
                {group.items.length > 0 ? renderRows(group) : null}
              </div>
            );
          })}
        </div>
        {footerHints}
      </div>
    );
  },
);

// ---------------------------------------------------------------------------
// MentionRow — single item in the list
//
// One shell owns the row chrome: fixed 52px height (56px in the <768px bottom
// sheet via --mention-row-h), three-slot layout (leading avatar column /
// main name+sub-line / fixed side column), gray hover/selected background and
// the 2.5px clay indicator bar. Entity templates only fill the slots. The
// per-type "agent"/"squad" badges are gone — type reads from the row's shape.
// ---------------------------------------------------------------------------

const ROW_HEIGHT_STYLE = { height: "var(--mention-row-h, 52px)" } as const;

function rowShellClass(selected: boolean, dimmed: boolean): string {
  return cn(
    "relative flex w-full items-center gap-3 px-4 text-left transition-colors",
    selected ? "bg-accent" : "hover:bg-accent/60",
    // Hover and selection share one background; the selected row keeps its
    // identity through the clay indicator bar, a dimension hover never
    // touches.
    // pins the selected indicator to clay #D97757 (same constant as
    // MENTION_CLAY / the current-task heading dot) — NOT --brand, which is
    // blue and failed the first-round visual check.
    selected &&
      "before:absolute before:bottom-1.5 before:left-0 before:top-1.5 before:w-[2.5px] before:rounded-r-full before:bg-[#D97757] before:content-['']",
    dimmed && "opacity-55",
  );
}

function MentionRow({
  item,
  optionId,
  statusColor,
  statusIcon,
  selected,
  onSelect,
  buttonRef,
  variant = "default",
  groupLabel = "",
}: {
  item: MentionItem;
  optionId: string;
  statusColor?: string | null;
  statusIcon?: string | null;
  selected: boolean;
  onSelect: () => void;
  buttonRef: (el: HTMLButtonElement | null) => void;
  /** "issue-table" renders the tabular plan-C issue template. */
  variant?: "default" | "issue-table";
  /**
   * Localized heading of the group this row renders in. Group headings are
   * decorative, so rows carry the grouping in their
   * aria-label instead — appended unless the heading already IS the row's
   * type word (members/squads/agents), which would just stutter.
   */
  groupLabel?: string;
}) {
  const { t } = useT("editor");
  const { t: issuesT } = useT("issues");

  if (item.type === "issue") {
    const isClosed =
      item.statusCategory === "done" || item.statusCategory === "closed";
    // Local consts so the narrowing survives into the i18n selector callbacks.
    const statusCategory = item.statusCategory;
    // Built-ins render their own display name; custom statuses fall back to
    // their lifecycle category's label.
    const statusKey = item.status;
    const statusWord =
      statusKey && isBuiltInIssueStatus(statusKey)
        ? issuesT(($) => $.status[statusKey])
        : statusCategory
          ? issuesT(($) => $.status_category[statusCategory])
          : "";
    const statusBadgeClass = statusBadgeClassFor(item.statusCategory);
    // Issue rows carry no type word, so the group heading ("Issues")
    // always adds grouping information to the sentence label.
    const ariaLabel = [item.label, item.description, statusWord, groupLabel]
      .filter(Boolean)
      .join(", ");

    if (variant === "issue-table") {
      // Tabular template: monospace KEY at a fixed left column, then title,
      // then status + priority badges in the side column.
      const issuePriority = item.issuePriority;
      const priorityWord = issuePriority
        ? issuesT(($) => $.priority[issuePriority])
        : "";
      return (
        <button
          type="button"
          id={optionId}
          ref={buttonRef}
          role="option"
          aria-selected={selected}
          aria-label={ariaLabel}
          style={ROW_HEIGHT_STYLE}
          className={rowShellClass(selected, false)}
          onClick={onSelect}
        >
          <span className="w-[72px] shrink-0 font-mono text-caption font-medium text-info">
            {item.label}
          </span>
          <span className="min-w-0 flex-1 truncate text-caption font-medium text-foreground">
            {item.description}
          </span>
          <span className="flex shrink-0 items-center gap-2">
            {statusWord && (
              <span
                className={cn(
                  "rounded-[5px] px-[7px] py-[2px] text-micro font-medium",
                  statusBadgeClass,
                )}
              >
                {statusWord}
              </span>
            )}
            {priorityWord && (
              <span className="rounded-[4px] border border-border bg-muted px-[5px] py-[1px] font-mono text-micro text-muted-foreground">
                {priorityWord}
              </span>
            )}
          </span>
        </button>
      );
    }

    return (
      <button
        type="button"
        id={optionId}
        ref={buttonRef}
        role="option"
        aria-selected={selected}
        aria-label={ariaLabel}
        style={ROW_HEIGHT_STYLE}
        className={rowShellClass(selected, isClosed)}
        onClick={onSelect}
      >
        <span className="flex size-9 shrink-0 items-center justify-center rounded-[9px] bg-info/10">
          {item.status ? (
            <StatusIcon
              status={item.status}
              category={item.statusCategory}
              color={statusColor}
              icon={statusIcon}
              className="h-3.5 w-3.5"
            />
          ) : (
            <ListTodo className="h-4 w-4 text-info" />
          )}
        </span>
        <span className="min-w-0 flex-1">
          {item.description ? (
            <>
              <span className="block truncate text-caption font-semibold text-foreground">
                {item.description}
              </span>
              <span className="block truncate text-caption text-muted-foreground">
                <span className="font-mono text-micro text-info">
                  {item.label}
                </span>
              </span>
            </>
          ) : (
            // No title: the KEY is the main text, status stays in the sub.
            <span className="flex min-w-0 items-center gap-2">
              <span className="truncate font-mono text-micro font-medium text-info">
                {item.label}
              </span>
            </span>
          )}
        </span>
        <span className="flex shrink-0 items-center">
          {statusWord && (
            <span
              className={cn(
                "rounded-[5px] px-[7px] py-[2px] text-micro font-medium",
                statusBadgeClass,
              )}
            >
              {statusWord}
            </span>
          )}
        </span>
      </button>
    );
  }

  if (item.type === "project") {
    const projectStatusCfg = item.projectStatus
      ? PROJECT_STATUS_CONFIG[item.projectStatus]
      : null;
    return (
      <button
        type="button"
        id={optionId}
        ref={buttonRef}
        role="option"
        aria-selected={selected}
        aria-label={[item.label, item.description].filter(Boolean).join(", ")}
        style={ROW_HEIGHT_STYLE}
        className={rowShellClass(selected, false)}
        onClick={onSelect}
      >
        <span className="flex size-9 shrink-0 items-center justify-center">
          <ProjectIcon project={{ icon: item.icon ?? null }} size="sm" />
        </span>
        <span className="min-w-0 flex-1">
          <span className="block truncate text-caption font-semibold text-foreground">
            {item.label}
          </span>
          {item.description && (
            <span className="block truncate text-caption text-muted-foreground">
              {item.description}
            </span>
          )}
        </span>
        {item.projectArchived && <ArchivedProjectTag />}
        {projectStatusCfg && (
          <span
            className={`${projectStatusCfg.dotColor} size-2 shrink-0 rounded-full`}
          />
        )}
      </button>
    );
  }

  if (item.type === "agent") {
    return (
      <AgentMentionRow
        item={item}
        optionId={optionId}
        selected={selected}
        onSelect={onSelect}
        buttonRef={buttonRef}
        groupLabel={groupLabel}
      />
    );
  }

  if (item.type === "squad") {
    return (
      <SquadMentionRow
        item={item}
        optionId={optionId}
        selected={selected}
        onSelect={onSelect}
        buttonRef={buttonRef}
        groupLabel={groupLabel}
      />
    );
  }

  // Member rows (including the synthetic "all members" row). Members have no
  // presence data source today, so no dot renders — an honest absence rather
  // than a fake green dot. ③: no email sub-line either — a
  // member row is avatar + name + at most one role tag.
  const isAllRow = item.type === "all";
  const membersWord = t(($) => $.mention.group_members);
  const ariaLabel = isAllRow
    ? t(($) => $.mention.all_members)
    : [
        item.label,
        membersWord,
        groupLabel && groupLabel !== membersWord ? groupLabel : "",
      ]
        .filter(Boolean)
        .join(", ");
  const memberTagWord =
    item.memberTag === "trigger"
      ? t(($) => $.mention.role_trigger)
      : item.memberTag === "owner"
        ? t(($) => $.mention.role_owner)
        : item.memberTag === "admin"
          ? t(($) => $.mention.role_admin)
          : "";
  return (
    <button
      type="button"
      id={optionId}
      ref={buttonRef}
      role="option"
      aria-selected={selected}
      aria-label={ariaLabel}
      style={ROW_HEIGHT_STYLE}
      className={rowShellClass(selected, false)}
      onClick={onSelect}
    >
      <ActorAvatar
        actorType="member"
        actorId={item.id}
        size="lg"
        profileLink={false}
        className={cn("border border-border", selected && "bg-background")}
      />
      <span className="min-w-0 flex-1">
        <span className="block truncate text-caption font-semibold text-foreground">
          {isAllRow ? t(($) => $.mention.all_members) : item.label}
        </span>
      </span>
      {memberTagWord && !isAllRow && (
        <span className="shrink-0 rounded-[5px] border border-border bg-muted px-[7px] py-[1.5px] text-micro text-muted-foreground">
          {memberTagWord}
        </span>
      )}
    </button>
  );
}

/**
 * Badge tone per issue status category (Rev.3 ⑤): text + background come
 * straight from the workspace status config — the same colours the status
 * picker paints each option with — instead of a hand-picked mapping.
 */
function statusBadgeClassFor(
  category: IssueStatusCategory | undefined,
): string {
  if (!category) return "bg-muted text-muted-foreground";
  const cfg = STATUS_CONFIG[category];
  return `${cfg.hoverBg.replace("hover:", "")} ${cfg.iconColor}`;
}

/**
 * Squad row — plan-A template: square-rounded avatar, name, member-count
 * sub-line, "Squad" ghost tag in the side column. Squads have no CLI and no
 * runtime presence, so no badge/dot stack renders.
 */
function SquadMentionRow({
  item,
  optionId,
  selected,
  onSelect,
  buttonRef,
  groupLabel = "",
}: {
  item: MentionItem;
  optionId: string;
  selected: boolean;
  onSelect: () => void;
  buttonRef: (el: HTMLButtonElement | null) => void;
  groupLabel?: string;
}) {
  const { t } = useT("editor");
  const squadsWord = t(($) => $.mention.group_squads);
  const sizeLabel =
    item.squadSize != null
      ? t(($) => $.mention.squad_member_count, { count: item.squadSize! })
      : "";
  return (
    <button
      type="button"
      id={optionId}
      ref={buttonRef}
      role="option"
      aria-selected={selected}
      aria-label={[
        item.label,
        squadsWord,
        sizeLabel,
        groupLabel && groupLabel !== squadsWord ? groupLabel : "",
      ]
        .filter(Boolean)
        .join(", ")}
      style={ROW_HEIGHT_STYLE}
      className={rowShellClass(selected, false)}
      onClick={onSelect}
    >
      <ActorAvatar
        actorType="squad"
        actorId={item.id}
        size="lg"
        profileLink={false}
        className={cn(
          "rounded-[9px] border border-border",
          selected && "bg-background",
        )}
      />
      <span className="min-w-0 flex-1">
        <span className="block truncate text-caption font-semibold text-foreground">
          {item.label}
        </span>
        {sizeLabel && (
          <span className="block truncate text-caption text-muted-foreground">
            {sizeLabel}
          </span>
        )}
      </span>
      <span className="shrink-0 rounded-[5px] border border-border bg-muted px-[7px] py-[1.5px] text-micro text-muted-foreground">
        {t(($) => $.mention.squad_tag)}
      </span>
    </button>
  );
}

/**
 * Agent row — the plan-A template: 36px avatar with the CLI brand
 * badge top-right and presence dot bottom-right, name, one gray sub-line
 * (owner · runtime host · model), and the CLI text tag in the side column.
 */
function AgentMentionRow({
  item,
  optionId,
  selected,
  onSelect,
  buttonRef,
  groupLabel = "",
}: {
  item: MentionItem;
  optionId: string;
  selected: boolean;
  onSelect: () => void;
  buttonRef: (el: HTMLButtonElement | null) => void;
  groupLabel?: string;
}) {
  const { t } = useT("editor");
  const { t: issuesT } = useT("issues");
  const [tooltipOpen, setTooltipOpen] = useState(false);
  const tooltipAnchorRef = useRef<HTMLButtonElement | null>(null);

  useEffect(() => {
    const anchor = tooltipAnchorRef.current;
    if (!tooltipOpen || !anchor) return;
    const doc = anchor.ownerDocument;
    const opts: AddEventListenerOptions = { capture: true, passive: true };
    const dismissOnScroll = (event: Event) => {
      // The tooltip is portaled. Close it when an ancestor of the row
      // scrolls — typically the candidate list — so it cannot cover the
      // footer after the trigger leaves the visible area. Unrelated
      // scrollers (and the tooltip itself) are ignored.
      if (event.target instanceof Node && event.target.contains(anchor)) {
        setTooltipOpen(false);
      }
    };
    doc.addEventListener("scroll", dismissOnScroll, opts);
    return () => doc.removeEventListener("scroll", dismissOnScroll, opts);
  }, [tooltipOpen]);

  const workspace = useCurrentWorkspace();
  const detail = useAgentPresenceDetail(
    workspace?.id,
    item.id,
  );
  const derivedPresence =
    detail === "loading" ? null : mentionPresenceState(detail.availability);
  // Runtime-group rows carry the runtime's own liveness snapshot — it is the
  // authoritative state for the row and arrives synchronously with the item.
  // The presence hook (agents-bundle derivation) covers rows without one.
  const runtimePresence: MentionPresenceState | null =
    item.runtime?.status === "online"
      ? "online"
      : item.runtime?.status === "offline"
        ? "offline"
        : null;
  // An archived agent renders the disabled shape even when the workspace
  // cache no longer lists it (the per-row presence hook then degrades to
  // offline, which would hide the retirement from the reader).
  const presence: MentionPresenceState | null = item.agentArchived
    ? "disabled"
    : (runtimePresence ?? (item.agentRuntimeGroup === "issue" ? null : derivedPresence));

  const ownerFallbackLabels = {
    unknownOwner: t(($) => $.mention.runtime_owner_unknown),
    workspaceRuntime: t(($) => $.mention.runtime_owner_workspace),
  };
  const cli = item.cli ?? "unknown";
  const cliTagLabel = mentionCliTagLabel(cli, item.cliProvider);

  // runtime rows resolve the owner at item-build time so a run on
  // another member's machine (absent from the viewer's runtime list) still
  // names its owner. An empty ownerLabel means unknown/owner-less — the item
  // layer embeds no fixed-language copy, so the localized fallback applies.
  const runtimeOwnerName =
    item.ownerLabel?.trim()
      ? item.ownerLabel
      : item.runtime
        ? runtimeOwnerDisplayName(
            item.runtime,
            item.runtimeOwner ?? null,
            ownerFallbackLabels,
          )
        : "";
  const runtimeModelLabel = item.runtimeModelLabel?.trim() ?? "";
  const displayRuntimeModelLabel = resolveRuntimeModelDisplayLabel(
    runtimeModelLabel,
    t(($) => $.mention.runtime_default_model),
  );
  const hostText = item.runtimeHost?.trim() ?? "";

  // One gray sub-line (Rev.3 ④): owner · model · runtime name — the model
  // sits ahead of the runtime name because truncation kept hiding it, and
  // no CLI text appears here (the corner badge and the side tag carry the
  // CLI). Parts the data doesn't carry are simply dropped, except the model
  // slot on runtime rows: an empty or sentinel model renders the localized
  // "default model" placeholder, matching the legacy runtime capsule.
  const hasRuntime = item.runtime != null;
  const isRuntimeGroupRow = item.agentRuntimeGroup !== undefined;
  const subParts: string[] = [];
  if (runtimeOwnerName) subParts.push(runtimeOwnerName);
  if (runtimeModelLabel) {
    subParts.push(
      isRuntimeDefaultModelLabel(runtimeModelLabel)
        ? displayRuntimeModelLabel
        : runtimeModelLabel,
    );
  } else if (hasRuntime) {
    subParts.push(displayRuntimeModelLabel);
  }
  if (hostText) subParts.push(hostText);
  // "runtime unavailable" describes a row actually gated on a missing
  // runtime — a resolved-but-foreign machine is neither missing nor gated and
  // must never wear the label.
  if (
    isRuntimeGroupRow &&
    !hasRuntime &&
    item.disabledReason === "agent_runtime_required"
  ) {
    subParts.push(t(($) => $.mention.runtime_unavailable));
  }

  const statusWord =
    presence === "online"
      ? t(($) => $.mention.runtime_online)
      : presence === "offline"
        ? t(($) => $.mention.runtime_offline)
        : presence === "disabled"
          ? t(($) => $.mention.disabled_tag)
          : "";
  const agentsWord = t(($) => $.mention.group_agents);
  const ariaLabel = [
    item.label,
    agentsWord,
    cliTagLabel,
    hostText ? `${t(($) => $.mention.aria_running_on)} ${hostText}` : "",
    statusWord,
    groupLabel && groupLabel !== agentsWord ? groupLabel : "",
  ]
    .filter(Boolean)
    .join(", ");

  const disabledMessage = item.disabledReason
    ? blockedReasonLabel(item.disabledReason, issuesT)
    : null;

  const row = (
    <button
      type="button"
      id={optionId}
      ref={(element) => {
        tooltipAnchorRef.current = element;
        buttonRef(element);
      }}
      role="option"
      aria-selected={selected}
      aria-disabled={disabledMessage ? true : undefined}
      aria-label={
        disabledMessage ? `${item.label}: ${disabledMessage}` : ariaLabel
      }
      style={ROW_HEIGHT_STYLE}
      className={cn(
        rowShellClass(selected, presence === "disabled"),
        disabledMessage && "cursor-not-allowed opacity-50",
      )}
      onClick={onSelect}
    >
      <MentionAvatarBadge
        avatarPx={AVATAR_SIZE_PX.lg}
        cli={cli}
        provider={item.cliProvider}
        presence={presence ?? "offline"}
      >
        <ActorAvatar
          actorType="agent"
          actorId={item.id}
          size="lg"
          profileLink={false}
          className={cn("border border-border", selected && "bg-background")}
        />
      </MentionAvatarBadge>
      <span className="min-w-0 flex-1">
        <span className="flex min-w-0 items-center gap-1.5">
          <span className="truncate text-caption font-semibold text-foreground">
            {item.label}
          </span>
          {presence === "disabled" && (
            <span className="shrink-0 rounded-[5px] border border-border bg-muted px-[6px] py-[0.5px] text-micro text-muted-foreground">
              {t(($) => $.mention.disabled_tag)}
            </span>
          )}
        </span>
        <span
          className="block truncate text-caption text-muted-foreground"
          title={subParts.join(" · ")}
        >
          {subParts.length > 0 ? (
            subParts.join(" · ")
          ) : isRuntimeGroupRow &&
            item.disabledReason === "agent_runtime_required" ? (
            <span className="text-faint-foreground">
              {t(($) => $.mention.runtime_unavailable)}
            </span>
          ) : null}
        </span>
      </span>
      {cliTagLabel && <MentionCliTextTag cli={cli} provider={item.cliProvider} />}
    </button>
  );

  if (!disabledMessage) return row;

  return (
    <Tooltip open={tooltipOpen} onOpenChange={setTooltipOpen}>
      <TooltipTrigger render={row} />
      <TooltipContent side="top" className="max-w-72 text-caption">
        {disabledMessage}
      </TooltipContent>
    </Tooltip>
  );
}

// Suggestion config factory

function issueToMention(
  i: Pick<Issue, "id" | "identifier" | "title" | "status" | "priority"> &
    Partial<Pick<Issue, "status_category">>,
): MentionItem {
  return {
    id: i.id,
    label: i.identifier,
    type: "issue" as const,
    description: i.title,
    status: i.status as IssueStatus,
    statusCategory: issueStatusCategory(i) ?? undefined,
    issuePriority: i.priority,
  };
}

function projectToMention(p: {
  id: string;
  title: string;
  description?: string | null;
  icon?: string | null;
  status?: ProjectStatus;
  archived_at?: string | null;
}): MentionItem {
  return {
    id: p.id,
    label: p.title,
    type: "project" as const,
    description: p.description ?? undefined,
    icon: p.icon ?? null,
    projectStatus: p.status,
    projectArchived: !!p.archived_at,
  };
}

/**
 * True when the `@` at `pos` starts a token instead of continuing one.
 *
 * The rule itself — which characters make an `@` part of the word it follows,
 * and why CJK needs the exception — lives in @multica/core/markdown, shared
 * with the mobile composer so the two clients cannot drift apart.
 */
function isMentionBoundary(doc: ProseMirrorNode, pos: number): boolean {
  if (pos <= 0) return true;
  // Two units wide, so a code point outside the BMP arrives whole; one would
  // hand back a lone surrogate. Across a block boundary this is the separator,
  // which is not a word character either.
  return isMentionBoundaryAfter(doc.textBetween(Math.max(0, pos - 2), pos, "\n", "\n"));
}

function matchesMentionQuery(item: MentionItem, query: string): boolean {
  const q = query.trim().toLowerCase();
  if (!q) return true;
  return (
    item.label.toLowerCase().includes(q) ||
    item.description?.toLowerCase().includes(q) === true ||
    matchesPinyin(item.label, q) ||
    (item.description ? matchesPinyin(item.description, q) : false)
  );
}

interface MentionSuggestionOptions {
  mode?: "default" | "context";
  getContextItems?: () => MentionItem[];
  runtimeMentionIssueId?: string;
  onSelect?: (item: MentionItem) => void;
}

function isSmartRuntimeAgent(agent: Agent): boolean {
  return agent.share_runtime === false;
}

function matchesAgentQuery(
  agent: Pick<Agent, "name">,
  query: string,
  cliTokens: string[] = [],
): boolean {
  const q = query.trim().toLowerCase();
  if (!q) return true;
  // the CLI name is itself a filter word — typing "codex" narrows
  // the list to Codex-backed agents even though no agent is named that.
  return (
    agent.name.toLowerCase().includes(q) ||
    cliTokens.some((token) => token.includes(q)) ||
    matchesPinyin(agent.name, q)
  );
}

function agentTaskSortTime(task: AgentTask): number {
  const timestamp =
    task.started_at ??
    task.dispatched_at ??
    task.completed_at ??
    task.created_at;
  return Date.parse(timestamp ?? "") || 0;
}

function latestTaskByAgent(tasks: AgentTask[]): Map<string, AgentTask> {
  const latest = new Map<string, AgentTask>();
  for (const task of tasks) {
    if (!task.agent_id) continue;
    const prev = latest.get(task.agent_id);
    if (!prev || agentTaskSortTime(task) > agentTaskSortTime(prev)) {
      latest.set(task.agent_id, task);
    }
  }
  return latest;
}

function hasRequesterRuntimeResolution(agent: Agent): boolean {
  return agent.runtime_config_summary?.current !== undefined;
}

function taskRuntimeModelLabel(task: AgentTask | null | undefined): string {
  const hint = task?.runtime_config_hint;
  if (hint?.kind === "agent") {
    const fromHint = hint.model_label?.trim() || hint.model_name?.trim() || "";
    if (fromHint) return fromHint;
  }
  return task?.effective_model?.trim() || "";
}

/**
 * Runtime HOST name for the mention sub-line: the daemon
 * bakes the CLI into the default runtime name ("Codex (IT-LAP… )"), but the
 * sub-line must not repeat the CLI (corner badge + side tag carry it), so
 * only the host part inside the parentheses is shown. A user-set alias
 * (custom_name) is shown verbatim — it is already what the user meant.
 */
function runtimeHostLabel(runtime: AgentRuntime): string {
  const custom = runtime.custom_name?.trim();
  if (custom) return custom;
  const match = runtime.name.match(/^[^(]*\(([^)]*)\)\s*$/);
  return match?.[1]?.trim() || runtimeDisplayName(runtime);
}

/**
 * Machine name inside a "CLI (machine)" runtime label, for sources that only
 * carry the name as a string. Showing the whole label
 * would repeat the CLI the row's badge and tag already carry; a label without
 * parentheses is a plain runtime name and passes through.
 */
function hostInsideRuntimeName(name: string): string {
  const trimmed = name.trim();
  const match = trimmed.match(/^[^(]*\(([^)]*)\)\s*$/);
  return match?.[1]?.trim() || trimmed;
}

/**
 * Run-scoped runtime identity recovered from the task row itself.
 * A smart-runtime run executes on its initiator's machine — a runtime the
 * viewer's own list may not contain. The task carries enough diagnostics to
 * still name the owner, host and CLI instead of reporting the runtime as
 * missing.
 */
interface RecoveredRuntimeInfo {
  /**
   * The task row carries server hydration evidence (runtime_owner_id /
   * runtime_provider) that the run's machine still exists, even when it is
   * another member's private runtime the viewer cannot resolve. A task
   * without it either never landed on a machine (empty runtime id) or points
   * at a deleted runtime — both stay gated.
   */
  hydrated?: boolean;
  /** Machine owner's display name; originator already applied as fallback. */
  ownerName?: string;
  /** The run's originator display name, for the unknown-owner fallback. */
  originatorName?: string;
  hostName?: string;
  provider?: string | null;
}

/**
 * Owner display for a resolved runtime row, following the machine (* ): the runtime's owner, never the run's originator — originator
 * re-stamps to whoever triggered the latest run. Only when the owner id is
 * present but its member can no longer be resolved does the originator name
 * read better than "Unknown member". An empty result means the owner is
 * unknown or the runtime has none: the item layer embeds no fixed-language
 * copy, and the renderer applies its localized fallback label.
 */
function runtimeOwnerDisplayLabel(
  runtime: Pick<AgentRuntime, "owner_id">,
  runtimeOwner: MemberWithUser | null,
  originatorName: string | undefined,
): string {
  const memberName = runtimeOwner?.name.trim();
  if (memberName) return memberName;
  if (runtime.owner_id) {
    return originatorName?.trim() || "";
  }
  return "";
}

function createRuntimeAgentItem(
  agent: Agent,
  group: "issue" | "requester",
  runtime: AgentRuntime | null,
  runtimeOwner: MemberWithUser | null,
  runtimeModelLabel = "",
  recovered?: RecoveredRuntimeInfo,
): MentionItem {
  const ownerLabel = runtime
    ? runtimeOwnerDisplayLabel(runtime, runtimeOwner, recovered?.originatorName)
    : recovered?.ownerName?.trim() || recovered?.originatorName?.trim() || "";
  const insertLabel = [agent.name, ownerLabel].filter(Boolean).join(" · ");
  // the provider slug resolves once here, at item-build time, so the
  // row renders the CLI badge/tag from plain data instead of re-deriving it.
  // an unresolvable runtime falls back to the task's own provider
  // before giving up — the CLI that executed the run, not the agent's current
  // one (only the requester group may fall back to that).
  const cliProvider =
    runtime?.provider?.trim()
    || recovered?.provider?.trim()
    || (group === "requester" ? agent.cli : null)
    || null;
  const archived = agent.archived_at != null;
  return {
    id: agent.id,
    label: agent.name,
    insertLabel,
    type: "agent",
    group: group === "issue" ? "issue_runtime" : "requester_runtime",
    agentRuntimeGroup: group,
    runtime,
    runtimeOwner,
    ownerLabel,
    runtimeModelLabel,
    cli: mentionCliFromProvider(cliProvider),
    cliProvider,
    runtimeHost: runtime
      ? runtimeHostLabel(runtime)
      : recovered?.hostName?.trim() || undefined,
    agentArchived: archived,
    // Rev.3 ①: an archived run record is history — visible, never insertable.
    // the issue group gates on the run's machine being real, never on
    // whether the viewer's runtime list happens to contain it — a smart run
    // executes on its initiator's runtime, which the viewer cannot and need
    // not resolve. Server hydration on the task row proves the machine; a
    // deleted or never-assigned runtime stays gated. The requester group
    // keeps its binding gate.
    disabledReason: archived
      ? "agent_disabled"
      : group === "issue"
        ? runtime || recovered?.hydrated
          ? undefined
          : "agent_runtime_required"
        : isAgentRuntimeBound(agent)
          ? undefined
          : "agent_runtime_required",
  };
}

export function commandItemForMention(item: MentionItem): MentionItem {
  return item.insertLabel && item.insertLabel !== item.label
    ? { ...item, label: item.insertLabel }
    : item;
}

function buildRuntimeAgentMentions({
  qc,
  wsId,
  issueId,
  query,
  members,
  agents,
  userId,
  myRole,
  recency,
}: {
  qc: QueryClient;
  wsId: string;
  issueId?: string;
  query: string;
  members: MemberWithUser[];
  agents: Agent[];
  userId: string | null;
  myRole: MemberRole | null;
  recency: Record<string, number>;
}): { items: MentionItem[]; standardAgentIdsToHide: Set<string> } {
  if (!issueId) {
    return { items: [], standardAgentIdsToHide: new Set() };
  }

  // Rev.3 ①: archived smart agents stay out of "my runtimes" but may still
  // appear in the current-task group — a run record on this issue is history,
  // and history keeps its context (rendered disabled there).
  const allSmartAgents = agents.filter((agent) => isSmartRuntimeAgent(agent));
  const activeSmartAgents = agents.filter(
    (agent) => !agent.archived_at && isSmartRuntimeAgent(agent),
  );
  const standardAgentIdsToHide = new Set(
    allSmartAgents.map((agent) => agent.id),
  );
  const invocableSmartAgents = activeSmartAgents.filter(
    (agent) => canAssignAgentToIssue(agent, { userId, role: myRole }).allowed,
  );

  const runtimeCache = qc.getQueryData<AgentRuntime[]>(runtimeKeys.list(wsId));
  const taskCache = qc.getQueryData<AgentTask[]>(issueKeys.tasks(issueId));
  const hasRuntimeCache = Array.isArray(runtimeCache);
  const hasTaskCache = Array.isArray(taskCache);
  const runtimes = runtimeCache ?? [];
  const tasks = taskCache ?? [];
  const runtimeById = new Map(runtimes.map((runtime) => [runtime.id, runtime]));
  const memberById = new Map(members.map((member) => [member.user_id, member]));
  const ownerFor = (runtime: AgentRuntime | null): MemberWithUser | null =>
    runtime?.owner_id ? (memberById.get(runtime.owner_id) ?? null) : null;

  const items: MentionItem[] = [];

  // Resolves a run's runtime against the viewer's own list. A miss here does
  // NOT mean the run had no machine — only that the machine is not
  // the viewer's; recoveredRuntimeInfo covers that case.
  const taskRuntime = (
    task: AgentTask | null | undefined,
  ): AgentRuntime | null => {
    const runtimeId = task?.runtime_id?.trim();
    return runtimeId ? (runtimeById.get(runtimeId) ?? null) : null;
  };

  // a smart run executes on its initiator's machine — a runtime the
  // viewer's own list may not contain. Recover the row's display identity
  // from the task's own diagnostics instead of reporting the runtime missing.
  const recoveredRuntimeInfo = (
    task: AgentTask | null | undefined,
  ): RecoveredRuntimeInfo => {
    if (!task) return {};
    // The owner display follows the MACHINE: a later run triggered by
    // a different member re-stamps originator_user_id while runtime_owner_id
    // stays with the machine's owner — so runtime_owner_id outranks
    // originator here, and originator is only a fallback when the runtime
    // owner is unknown.
    const ownerMember =
      task.runtime_owner_id ? memberById.get(task.runtime_owner_id) : null;
    const originatorMember =
      task.originator_user_id ? memberById.get(task.originator_user_id) : null;
    const hint = task.runtime_config_hint;
    const provider = task.runtime_provider?.trim() || null;
    return {
      // Mirrors the server's hydrateTaskRuntimeIdentities: owner/provider are
      // stamped only when the runtime row still exists (private included).
      hydrated: Boolean(task.runtime_owner_id?.trim() || provider),
      ownerName: ownerMember?.name?.trim() || undefined,
      originatorName: originatorMember?.name?.trim() || undefined,
      hostName:
        hint?.kind === "agent" && hint.runtime_name?.trim()
          ? hostInsideRuntimeName(hint.runtime_name) || undefined
          : undefined,
      provider,
    };
  };

  // the CLI name filters runtime-agent rows too — resolve the row's
  // effective runtime first, then match name/pinyin/CLI tokens.
  const cliTokensFor = (
    agent: Agent,
    runtime: AgentRuntime | null,
    useCurrentCli = true,
    recoveredProvider: string | null = null,
  ): string[] => {
    const provider =
      runtime?.provider?.trim()
      || recoveredProvider
      || (useCurrentCli ? agent.cli : null)
      || null;
    return mentionCliFilterTokens(
      mentionCliFromProvider(provider),
      provider,
    );
  };

  if (hasTaskCache) {
    const latestTasks = latestTaskByAgent(tasks);
    // A run belongs to this issue regardless of the agent's current routing
    // strategy. Resolve its runtime/model from the task, never the agent's
    // current binding (which may have changed since that run).
    for (const agentId of latestTasks.keys()) {
      standardAgentIdsToHide.add(agentId);
    }
    const issueAgents = agents
      .filter((agent) => latestTasks.has(agent.id))
      .filter((agent) => {
        const task = latestTasks.get(agent.id);
        const runtime = taskRuntime(task);
        return matchesAgentQuery(
          agent,
          query,
          cliTokensFor(agent, runtime, false, task?.runtime_provider ?? null),
        );
      })
      .toSorted((a, b) => {
        const taskA = latestTasks.get(a.id);
        const taskB = latestTasks.get(b.id);
        return agentTaskSortTime(taskB!) - agentTaskSortTime(taskA!);
      });

    items.push(
      ...issueAgents.map((agent) => {
        const task = latestTasks.get(agent.id);
        const runtime = taskRuntime(task);
        return createRuntimeAgentItem(
          agent,
          "issue",
          runtime,
          // Owner display follows the machine: the runtime's own owner, with
          // the recovered task diagnostics covering a runtime the viewer
          // cannot resolve. The run's originator is never the owner subject —
          // it re-stamps to whoever triggered the latest run.
          ownerFor(runtime),
          taskRuntimeModelLabel(task),
          recoveredRuntimeInfo(task),
        );
      }),
    );
  }

  if (hasRuntimeCache) {
    const resolvedAgents = invocableSmartAgents.filter(
      hasRequesterRuntimeResolution,
    );
    const requesterItems = resolvedAgents
      .filter((agent) => {
        const runtimeId = agentListRuntimeId(agent) ?? "";
        const runtime = runtimeById.get(runtimeId) ?? null;
        return matchesAgentQuery(agent, query, cliTokensFor(agent, runtime));
      })
      .map((agent) => {
        const runtimeId = agentListRuntimeId(agent) ?? "";
        const runtime = runtimeById.get(runtimeId) ?? null;
        return createRuntimeAgentItem(
          agent,
          "requester",
          runtime,
          ownerFor(runtime),
          agent.runtime_config_summary?.current.model ?? "",
        );
      });

    items.push(...sortUserItemsByRecency(requesterItems, recency));
  }

  return {
    items,
    standardAgentIdsToHide,
  };
}

export function createRuntimeMentionSuggestion(
  qc: QueryClient,
  options: MentionSuggestionOptions & { runtimeMentionIssueId: string },
): Omit<SuggestionOptions<MentionItem>, "editor"> {
  // The explicit key is passed into Tiptap Suggestion and reused by the
  // shared popup controller when it dispatches exitSuggestion(view, pluginKey).
  const pluginKey = new PluginKey("mentionSuggestion");

  function buildSyncItems(query: string): MentionItem[] {
    // Read workspace id imperatively because this runs in TipTap factory scope
    // (outside React render). getCurrentWsId() is the non-React singleton set
    // by the URL-driven workspace layout.
    const wsId = getCurrentWsId();
    if (!wsId) return [];

    const members: MemberWithUser[] =
      qc.getQueryData(workspaceKeys.members(wsId)) ?? [];
    const agents: Agent[] = qc.getQueryData(workspaceKeys.agents(wsId)) ?? [];
    const squads: Squad[] = qc.getQueryData(workspaceKeys.squads(wsId)) ?? [];
    const listQueries = qc.getQueriesData<ListIssuesCache>({
      queryKey: issueKeys.list(wsId),
    });
    const cachedResponse = listQueries[0]?.[1];
    const cachedIssues: Issue[] = cachedResponse
      ? flattenIssueBuckets(cachedResponse)
      : [];

    // Read current user identity imperatively — this factory runs outside
    // React render so we can't useAuthStore() as a hook here. The Proxy in
    // packages/core/auth/index.ts forwards `.getState()` to the registered
    // store. Used to gate personal agents in the @mention list so members
    // don't see (or auto-complete) agents they couldn't assign anyway.
    const userId = useAuthStore.getState().user?.id ?? null;
    const myRole = members.find((m) => m.user_id === userId)?.role ?? null;

    const q = query.toLowerCase();

    const allItem: MentionItem[] =
      "all members".includes(q) || "all".includes(q)
        ? [{ id: "all", label: "All members", type: "all" as const }]
        : [];

    // member tag priority: initiator > owner > admin > none.
    // The initiator tier needs the current issue's task snapshot; without an
    // issue context (or without a attributable run) the tier is simply
    // skipped — never guessed.
    const tasks: AgentTask[] = options.runtimeMentionIssueId
      ? (qc.getQueryData<AgentTask[]>(
          issueKeys.tasks(options.runtimeMentionIssueId),
        ) ?? [])
      : [];
    const initiatorUserId =
      [...tasks]
        .toSorted((a, b) => agentTaskSortTime(b) - agentTaskSortTime(a))
        .find((task) => task.originator_user_id)?.originator_user_id ?? null;
    const memberTagFor = (
      member: MemberWithUser,
    ): "trigger" | "owner" | "admin" | null => {
      if (initiatorUserId && member.user_id === initiatorUserId) {
        return "trigger";
      }
      if (member.role === "owner") return "owner";
      if (member.role === "admin") return "admin";
      return null;
    };

    const memberItems: MentionItem[] = members
      .filter(
        (m) => m.name.toLowerCase().includes(q) || matchesPinyin(m.name, q),
      )
      .map((m) => ({
        id: m.user_id,
        label: m.name,
        type: "member" as const,
        memberRole: m.role,
        memberTag: memberTagFor(m),
      }));

    const recency = getRecencyMap(wsId);
    const runtimeMentions = buildRuntimeAgentMentions({
      qc,
      wsId,
      issueId: options.runtimeMentionIssueId,
      query,
      members,
      agents,
      userId,
      myRole,
      recency,
    });

    // Runtime cache for CLI resolution of non-runtime-group agents.
    // May be absent — CLI then degrades to "unknown" and the row renders no
    // badge, exactly like AgentRuntimeBadge's missing-provider behaviour.
    const runtimes: AgentRuntime[] =
      qc.getQueryData(runtimeKeys.list(wsId)) ?? [];
    const cliFor = (agent: Agent) => {
      const provider =
        resolveAgentRuntimeProvider(agent, runtimes) ??
        agent.cli ??
        null;
      const runtimeId = agentListRuntimeId(agent);
      const runtime = runtimeId
        ? (runtimes.find((candidate) => candidate.id === runtimeId) ?? null)
        : null;
      return {
        cli: mentionCliFromProvider(provider),
        cliProvider: provider,
        runtimeHost: runtime ? runtimeHostLabel(runtime) : undefined,
      };
    };

    const agentItems: MentionItem[] = agents
      .filter((a) => {
        // ①: archived agents don't surface in the directory
        // bucket at all — the only place one can appear is the current-task
        // group (a run record on this issue), rendered disabled there.
        if (a.archived_at) return false;
        if (runtimeMentions.standardAgentIdsToHide.has(a.id)) return false;
        if (!canAssignAgentToIssue(a, { userId, role: myRole }).allowed) {
          return false;
        }
        const cliInfo = cliFor(a);
        return matchesAgentQuery(
          a,
          query,
          mentionCliFilterTokens(cliInfo.cli, cliInfo.cliProvider),
        );
      })
      .map((a) => ({
        id: a.id,
        label: a.name,
        type: "agent" as const,
        disabledReason: isAgentRuntimeBound(a)
          ? undefined
          : ("agent_runtime_required" as const),
        ...cliFor(a),
      }));
    const activeAgentRuntimeBinding = new Map(
      agents
        .filter((agent) => !agent.archived_at)
        .map((agent) => [agent.id, isAgentRuntimeBound(agent)]),
    );

    const squadItems: MentionItem[] = squads
      .filter(
        (s) =>
          !s.archived_at &&
          (s.name.toLowerCase().includes(q) || matchesPinyin(s.name, q)),
      )
      .map((s) => ({
        id: s.id,
        label: s.name,
        type: "squad" as const,
        squadSize:
          s.member_count ?? s.member_preview?.length ?? null,
        disabledReason:
          activeAgentRuntimeBinding.get(s.leader_id) === false
            ? ("agent_runtime_required" as const)
            : undefined,
      }));

    // Members and agents share a single ranked list — recently mentioned
    // targets come first regardless of type, with an alphabetical fallback
    // for everyone the user hasn't mentioned yet on this device.
    const userItems = sortUserItemsByRecency(
      [...memberItems, ...agentItems, ...squadItems],
      recency,
    );

    // Cached issues give an instant first paint; MentionList adds server
    // matches for done/cancelled and any other issues not in this cache.
    const issueItems: MentionItem[] = cachedIssues
      .filter(
        (i) =>
          i.identifier.toLowerCase().includes(q) ||
          i.title.toLowerCase().includes(q),
      )
      .map(issueToMention);

    return [...runtimeMentions.items, ...allItem, ...userItems, ...issueItems];
  }

  function buildItems(query: string): MentionItem[] {
    if (options.mode === "context") {
      const contextItems = (options.getContextItems?.() ?? []).filter(
        (item) => matchesMentionQuery(item, query),
      );
      return query.trim()
        ? mergeMentionItems(contextItems, buildSyncItems(query))
        : contextItems;
    }
    return buildSyncItems(query);
  }

  const issueId = options.runtimeMentionIssueId;
  const component = issueId
    ? forwardRef<MentionListRef, MentionListProps>(function LiveIssueMentions(props, ref) {
        return <IssueMentionList {...props} ref={ref} client={qc} issueId={issueId} buildItems={buildItems} />;
      })
    : MentionList;

  return {
    pluginKey,
    allowSpaces: true,
    // The boundary rule is isMentionBoundary's, not Tiptap's default of "a
    // half-width space and nothing else" (see the note there).
    allowedPrefixes: null,
    // Only open over an `@` the user actually typed. Tiptap matches on document
    // content alone, so without this a pasted, dropped, undone or server-loaded
    // `@` opens the picker just as readily (MUL-5429).
    shouldShow: ({ editor, range, transaction }) =>
      isTriggerArmedAt(editor, range.from) &&
      isMentionBoundary(transaction.doc, range.from),
    items: ({ query }) => {
      if (options.mode === "context") {
        const normalizedQuery = query.trim();
        const contextItems = (options.getContextItems?.() ?? []).filter((item) => matchesMentionQuery(item, query));
        if (!normalizedQuery) return contextItems;
        return mergeMentionItems(contextItems, buildSyncItems(query));
      }
      return buildSyncItems(query);
    },
    render: createSuggestionPopupRender<
      MentionItem,
      MentionItem,
      MentionListRef,
      MentionListProps
    >({
      pluginKey,
      component,
      maxHeightCap: 420,
      // responsive rule: narrow viewports render the picker as a
      // bottom sheet with 56px rows instead of a caret-anchored panel.
      bottomSheetBelowPx: 768,
      // the editor's contenteditable wears the ARIA combobox
      // pattern while the picker is open.
      combobox: true,
      getProps: (props) => ({
        items: props.items,
        query: props.query,
        editorDom: props.editor.view.dom,
        command: (item) => {
          options.onSelect?.(item);
          props.command(commandItemForMention(item));
        },
        contextSearch: options.mode === "context",
      }),
      onKeyDown: (ref, props) => ref?.onKeyDown(props) ?? false,
    }),
  };
}
