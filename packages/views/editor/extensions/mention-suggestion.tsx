"use client";

import {
  forwardRef,
  useCallback,
  useEffect,
  useImperativeHandle,
  useMemo,
  useRef,
  useState,
} from "react";
import { ReactRenderer } from "@tiptap/react";
import { computePosition, offset, flip, shift } from "@floating-ui/dom";
import type { QueryClient } from "@tanstack/react-query";
import { getCurrentWsId } from "@multica/core/platform";
import { flattenIssueBuckets, issueKeys } from "@multica/core/issues/queries";
import { workspaceKeys } from "@multica/core/workspace/queries";
import { useAuthStore } from "@multica/core/auth";
import { canAssignAgentToIssue } from "@multica/core/permissions";
import { api } from "@multica/core/api";
import { isImeComposing } from "@multica/core/utils";
import type {
  Issue,
  ListIssuesCache,
  MemberWithUser,
  Agent,
  Squad,
} from "@multica/core/types";
import { ActorAvatar } from "../../common/actor-avatar";
import { StatusIcon } from "../../issues/components/status-icon";
import { useT } from "../../i18n";
import { Badge } from "@multica/ui/components/ui/badge";
import type { IssueStatus } from "@multica/core/types";
import type { SuggestionOptions, SuggestionProps } from "@tiptap/suggestion";
import {
  getRecencyMap,
  recordMentionUsage,
  sortUserItemsByRecency,
} from "./mention-recency";
import { matchesPinyin } from "./pinyin-match";

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export interface MentionItem {
  id: string;
  label: string;
  type: "member" | "agent" | "squad" | "issue" | "all";
  /** Secondary text shown beside the label (e.g. issue title) */
  description?: string;
  /** Issue status for StatusIcon rendering */
  status?: IssueStatus;
}

interface MentionListProps {
  items: MentionItem[];
  query: string;
  command: (item: MentionItem) => void;
}

export interface MentionListRef {
  onKeyDown: (props: { event: KeyboardEvent }) => boolean;
}

// ---------------------------------------------------------------------------
// Group items by section
// ---------------------------------------------------------------------------

interface MentionGroup {
  label: string;
  items: MentionItem[];
}

function groupItems(items: MentionItem[]): MentionGroup[] {
  const users: MentionItem[] = [];
  const issues: MentionItem[] = [];

  for (const item of items) {
    if (item.type === "issue") {
      issues.push(item);
    } else {
      users.push(item);
    }
  }

  const groups: MentionGroup[] = [];
  if (users.length > 0) groups.push({ label: "Users", items: users });
  if (issues.length > 0) groups.push({ label: "Issues", items: issues });
  return groups;
}

// ---------------------------------------------------------------------------
// MentionList — the popup rendered inside the editor
// ---------------------------------------------------------------------------

const MAX_ITEMS = 20;
const SERVER_ISSUE_SEARCH_LIMIT = 20;
const SERVER_SEARCH_DEBOUNCE_MS = 150;

function mentionItemKey(item: MentionItem): string {
  return `${item.type}:${item.id}`;
}

function mergeMentionItems(
  syncItems: MentionItem[],
  serverIssueItems: MentionItem[],
): MentionItem[] {
  const seen = new Set<string>();
  const merged: MentionItem[] = [];

  for (const item of [...syncItems, ...serverIssueItems]) {
    const key = mentionItemKey(item);
    if (seen.has(key)) continue;
    seen.add(key);
    merged.push(item);
  }

  return merged;
}

export const MentionList = forwardRef<MentionListRef, MentionListProps>(
  function MentionList({ items, query, command }, ref) {
    const { t } = useT("editor");
    const [selectedIndex, setSelectedIndex] = useState(0);
    const [serverIssueItems, setServerIssueItems] = useState<MentionItem[]>([]);
    const [isSearchingIssues, setIsSearchingIssues] = useState(false);
    const [searchedIssueQuery, setSearchedIssueQuery] = useState("");
    const itemRefs = useRef<(HTMLButtonElement | null)[]>([]);
    const normalizedQuery = query.trim();

    useEffect(() => {
      const q = normalizedQuery;
      setServerIssueItems([]);

      if (!q) {
        setIsSearchingIssues(false);
        setSearchedIssueQuery("");
        return;
      }

      const wsId = getCurrentWsId();
      if (!wsId) {
        setIsSearchingIssues(false);
        setSearchedIssueQuery(q);
        return;
      }

      let cancelled = false;
      const controller = new AbortController();
      setIsSearchingIssues(true);

      const timer = setTimeout(() => {
        void (async () => {
          try {
            const res = await api.searchIssues({
              q,
              limit: SERVER_ISSUE_SEARCH_LIMIT,
              include_closed: true,
              signal: controller.signal,
            });
            if (!cancelled && !controller.signal.aborted) {
              setServerIssueItems(res.issues.map(issueToMention));
            }
          } catch {
            // Aborted or network error: keep the synchronous cache results.
          } finally {
            if (!cancelled && !controller.signal.aborted) {
              setSearchedIssueQuery(q);
              setIsSearchingIssues(false);
            }
          }
        })();
      }, SERVER_SEARCH_DEBOUNCE_MS);

      return () => {
        cancelled = true;
        clearTimeout(timer);
        controller.abort();
      };
    }, [normalizedQuery]);

    const displayItems = useMemo(() => {
      const currentServerIssueItems =
        searchedIssueQuery === normalizedQuery ? serverIssueItems : [];
      return mergeMentionItems(items, currentServerIssueItems).slice(0, MAX_ITEMS);
    }, [items, normalizedQuery, searchedIssueQuery, serverIssueItems]);

    useEffect(() => {
      setSelectedIndex(0);
    }, [displayItems]);

    useEffect(() => {
      itemRefs.current[selectedIndex]?.scrollIntoView({ block: "nearest" });
    }, [selectedIndex]);

    const selectItem = useCallback(
      (index: number) => {
        const item = displayItems[index];
        if (!item) return;
        const wsId = getCurrentWsId();
        if (wsId) recordMentionUsage(wsId, item);
        command(item);
      },
      [displayItems, command],
    );

    useImperativeHandle(ref, () => ({
      onKeyDown: ({ event }) => {
        // IME is composing — don't intercept Enter/Arrow as picker actions;
        // those keys belong to the IME (Enter commits composition, etc).
        if (isImeComposing(event)) return false;
        if (event.key === "ArrowUp") {
          if (displayItems.length === 0) return true;
          setSelectedIndex(
            (i) => (i + displayItems.length - 1) % displayItems.length,
          );
          return true;
        }
        if (event.key === "ArrowDown") {
          if (displayItems.length === 0) return true;
          setSelectedIndex((i) => (i + 1) % displayItems.length);
          return true;
        }
        if (event.key === "Enter") {
          if (displayItems.length === 0) return true;
          selectItem(selectedIndex);
          return true;
        }
        return false;
      },
    }));

    if (displayItems.length === 0) {
      const isWaitingForServer =
        normalizedQuery !== "" &&
        (isSearchingIssues || searchedIssueQuery !== normalizedQuery);

      return (
        <div className="rounded-md border bg-popover p-2 text-xs text-muted-foreground shadow-md">
          {isWaitingForServer
            ? t(($) => $.mention.searching)
            : t(($) => $.mention.no_results)}
        </div>
      );
    }

    const groups = groupItems(displayItems);
    const groupLabel = (label: string): string => {
      if (label === "Users") return t(($) => $.mention.group_users);
      if (label === "Issues") return t(($) => $.mention.group_issues);
      return label;
    };

    // Build a flat index mapping: globalIndex → item
    let globalIndex = 0;

    return (
      <div className="rounded-md border bg-popover py-1 shadow-md w-72 max-h-[300px] overflow-y-auto">
        {groups.map((group) => (
          <div key={group.label}>
            <div className="px-3 py-1.5 text-xs font-medium text-muted-foreground">
              {groupLabel(group.label)}
            </div>
            {group.items.map((item) => {
              const idx = globalIndex++;
              return (
                <MentionRow
                  key={`${item.type}-${item.id}`}
                  item={item}
                  selected={idx === selectedIndex}
                  onSelect={() => selectItem(idx)}
                  buttonRef={(el) => { itemRefs.current[idx] = el; }}
                />
              );
            })}
          </div>
        ))}
      </div>
    );
  },
);

// ---------------------------------------------------------------------------
// MentionRow — single item in the list
// ---------------------------------------------------------------------------

function MentionRow({
  item,
  selected,
  onSelect,
  buttonRef,
}: {
  item: MentionItem;
  selected: boolean;
  onSelect: () => void;
  buttonRef: (el: HTMLButtonElement | null) => void;
}) {
  const { t } = useT("editor");
  if (item.type === "issue") {
    // Visually dim closed issues (done/cancelled) so they're distinguishable
    // from active ones in the suggestion list — they're still selectable.
    const isClosed = item.status === "done" || item.status === "cancelled";
    return (
      <button
        ref={buttonRef}
        className={`flex w-full items-center gap-2.5 px-3 py-1.5 text-left text-xs transition-colors ${
          selected ? "bg-accent" : "hover:bg-accent/50"
        } ${isClosed ? "opacity-60" : ""}`}
        onClick={onSelect}
      >
        {item.status && (
          <StatusIcon status={item.status} className="h-3.5 w-3.5 shrink-0" />
        )}
        <span className="shrink-0 text-muted-foreground">{item.label}</span>
        {item.description && (
          <span
            className={`truncate text-muted-foreground ${isClosed ? "line-through" : ""}`}
          >
            {item.description}
          </span>
        )}
      </button>
    );
  }

  return (
    <button
      ref={buttonRef}
      className={`flex w-full items-center gap-2.5 px-3 py-1.5 text-left text-xs transition-colors ${
        selected ? "bg-accent" : "hover:bg-accent/50"
      }`}
      onClick={onSelect}
    >
      <ActorAvatar
        actorType={item.type === "all" ? "member" : item.type}
        actorId={item.id}
        size={20}
        showStatusDot
      />
      <span className="truncate font-medium">
        {item.type === "all" ? t(($) => $.mention.all_members) : item.label}
      </span>
      {item.type === "agent" && (
        // "Agent" is a glossary-protected product term — kept un-translated.
        // eslint-disable-next-line i18next/no-literal-string
        <Badge variant="outline" className="ml-auto text-[10px] h-4 px-1.5">Agent</Badge>
      )}
      {item.type === "squad" && (
        // "Squad" is a glossary-protected product term — kept un-translated.
        // eslint-disable-next-line i18next/no-literal-string
        <Badge variant="outline" className="ml-auto text-[10px] h-4 px-1.5">Squad</Badge>
      )}
    </button>
  );
}

// ---------------------------------------------------------------------------
// Suggestion config factory
// ---------------------------------------------------------------------------

function issueToMention(i: Pick<Issue, "id" | "identifier" | "title" | "status">): MentionItem {
  return {
    id: i.id,
    label: i.identifier,
    type: "issue" as const,
    description: i.title,
    status: i.status as IssueStatus,
  };
}

export function createMentionSuggestion(qc: QueryClient): Omit<
  SuggestionOptions<MentionItem>,
  "editor"
> {
  // Renderer/popup instances live in this closure so each ContentEditor owns
  // its own TipTap suggestion popup lifecycle.
  let renderer: ReactRenderer<MentionListRef> | null = null;
  let popup: HTMLDivElement | null = null;

  function buildSyncItems(query: string): MentionItem[] {
    // Read workspace id imperatively because this runs in TipTap factory scope
    // (outside React render). getCurrentWsId() is the non-React singleton set
    // by the URL-driven workspace layout.
    const wsId = getCurrentWsId();
    if (!wsId) return [];

    const members: MemberWithUser[] = qc.getQueryData(workspaceKeys.members(wsId)) ?? [];
    const agents: Agent[] = qc.getQueryData(workspaceKeys.agents(wsId)) ?? [];
    const squads: Squad[] = qc.getQueryData(workspaceKeys.squads(wsId)) ?? [];
    const cachedResponse = qc.getQueryData<ListIssuesCache>(issueKeys.list(wsId));
    const cachedIssues: Issue[] = cachedResponse ? flattenIssueBuckets(cachedResponse) : [];

    // Read current user identity imperatively — this factory runs outside
    // React render so we can't useAuthStore() as a hook here. The Proxy in
    // packages/core/auth/index.ts forwards `.getState()` to the registered
    // store. Used to gate personal agents in the @mention list so members
    // don't see (or auto-complete) agents they couldn't assign anyway.
    const userId = useAuthStore.getState().user?.id ?? null;
    const myRole =
      members.find((m) => m.user_id === userId)?.role ?? null;

    const q = query.toLowerCase();

    const allItem: MentionItem[] =
      "all members".includes(q) || "all".includes(q)
        ? [{ id: "all", label: "All members", type: "all" as const }]
        : [];

    const memberItems: MentionItem[] = members
      .filter((m) => m.name.toLowerCase().includes(q) || matchesPinyin(m.name, q))
      .map((m) => ({
        id: m.user_id,
        label: m.name,
        type: "member" as const,
      }));

    const agentItems: MentionItem[] = agents
      .filter(
        (a) =>
          !a.archived_at &&
          (a.name.toLowerCase().includes(q) || matchesPinyin(a.name, q)) &&
          canAssignAgentToIssue(a, { userId, role: myRole }).allowed,
      )
      .map((a) => ({ id: a.id, label: a.name, type: "agent" as const }));

    const squadItems: MentionItem[] = squads
      .filter((s) => !s.archived_at && (s.name.toLowerCase().includes(q) || matchesPinyin(s.name, q)))
      .map((s) => ({ id: s.id, label: s.name, type: "squad" as const }));

    // Members and agents share a single ranked list — recently mentioned
    // targets come first regardless of type, with an alphabetical fallback
    // for everyone the user hasn't mentioned yet on this device.
    const recency = getRecencyMap(wsId);
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

    return [...allItem, ...userItems, ...issueItems];
  }

  return {
    items: ({ query }) => {
      const syncItems = buildSyncItems(query);
      return syncItems;
    },

    render: () => {
      return {
        onStart: (props: SuggestionProps<MentionItem>) => {
          renderer = new ReactRenderer(MentionList, {
            props: {
              items: props.items,
              query: props.query,
              command: props.command,
            },
            editor: props.editor,
          });

          popup = document.createElement("div");
          popup.style.position = "fixed";
          popup.style.zIndex = "50";
          popup.appendChild(renderer.element);
          document.body.appendChild(popup);

          updatePosition(popup, props.clientRect);
        },

        onUpdate: (props: SuggestionProps<MentionItem>) => {
          renderer?.updateProps({
            items: props.items,
            query: props.query,
            command: props.command,
          });
          if (popup) updatePosition(popup, props.clientRect);
        },

        onKeyDown: (props: { event: KeyboardEvent }) => {
          if (props.event.key === "Escape") {
            cleanup();
            return true;
          }
          return renderer?.ref?.onKeyDown(props) ?? false;
        },

        onExit: () => {
          cleanup();
        },
      };

      function updatePosition(
        el: HTMLDivElement,
        clientRect: (() => DOMRect | null) | null | undefined,
      ) {
        if (!clientRect) return;
        const virtualEl = {
          getBoundingClientRect: () => clientRect() ?? new DOMRect(),
        };
        computePosition(virtualEl, el, {
          placement: "bottom-start",
          strategy: "fixed",
          middleware: [offset(4), flip(), shift({ padding: 8 })],
        }).then(({ x, y }) => {
          el.style.left = `${x}px`;
          el.style.top = `${y}px`;
        });
      }

      function cleanup() {
        renderer?.destroy();
        renderer = null;
        popup?.remove();
        popup = null;
      }
    },
  };
}
