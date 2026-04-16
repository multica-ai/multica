"use client";

import {
  forwardRef,
  useCallback,
  useEffect,
  useImperativeHandle,
  useRef,
  useState,
} from "react";
import { ReactRenderer } from "@tiptap/react";
import { computePosition, offset, flip, shift } from "@floating-ui/dom";
import type { QueryClient } from "@tanstack/react-query";
import { getCurrentWsId } from "@multica/core/platform";
import { issueKeys } from "@multica/core/issues/queries";
import { workspaceKeys } from "@multica/core/workspace/queries";
import { api } from "@multica/core/api";
import type { Issue, ListIssuesResponse, MemberWithUser, Agent } from "@multica/core/types";
import { ActorAvatar } from "../../common/actor-avatar";
import { StatusIcon } from "../../issues/components/status-icon";
import { Badge } from "@multica/ui/components/ui/badge";
import type { IssueStatus } from "@multica/core/types";
import type { SuggestionOptions, SuggestionProps } from "@tiptap/suggestion";

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export interface MentionItem {
  id: string;
  label: string;
  type: "member" | "agent" | "issue" | "all";
  /** Secondary text shown beside the label (e.g. issue title) */
  description?: string;
  /** Issue status for StatusIcon rendering */
  status?: IssueStatus;
}

interface MentionListProps {
  items: MentionItem[];
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

const MentionList = forwardRef<MentionListRef, MentionListProps>(
  function MentionList({ items, command }, ref) {
    const [selectedIndex, setSelectedIndex] = useState(0);
    const itemRefs = useRef<(HTMLButtonElement | null)[]>([]);

    useEffect(() => {
      setSelectedIndex(0);
    }, [items]);

    useEffect(() => {
      itemRefs.current[selectedIndex]?.scrollIntoView({ block: "nearest" });
    }, [selectedIndex]);

    const selectItem = useCallback(
      (index: number) => {
        const item = items[index];
        if (item) command(item);
      },
      [items, command],
    );

    useImperativeHandle(ref, () => ({
      onKeyDown: ({ event }) => {
        if (event.key === "ArrowUp") {
          setSelectedIndex((i) => (i + items.length - 1) % items.length);
          return true;
        }
        if (event.key === "ArrowDown") {
          setSelectedIndex((i) => (i + 1) % items.length);
          return true;
        }
        if (event.key === "Enter") {
          selectItem(selectedIndex);
          return true;
        }
        return false;
      },
    }));

    if (items.length === 0) {
      return (
        <div className="rounded-md border bg-popover p-2 text-xs text-muted-foreground shadow-md">
          No results
        </div>
      );
    }

    const groups = groupItems(items);

    // Build a flat index mapping: globalIndex → item
    let globalIndex = 0;

    return (
      <div className="rounded-md border bg-popover py-1 shadow-md w-72 max-h-[300px] overflow-y-auto">
        {groups.map((group) => (
          <div key={group.label}>
            <div className="px-3 py-1.5 text-xs font-medium text-muted-foreground">
              {group.label}
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
      />
      <span className="truncate font-medium">{item.label}</span>
      {item.type === "agent" && (
        <Badge variant="outline" className="ml-auto text-[10px] h-4 px-1.5">Agent</Badge>
      )}
    </button>
  );
}

// ---------------------------------------------------------------------------
// Suggestion config factory
// ---------------------------------------------------------------------------

// Module-scoped state coordinates debounce/abort across successive items()
// calls so that only the latest query's server-side issue search resolves.
let issueSearchSeq = 0;
let issueSearchAbort: AbortController | null = null;

function issueToMention(i: Pick<Issue, "id" | "identifier" | "title" | "status">): MentionItem {
  return {
    id: i.id,
    label: i.identifier,
    type: "issue" as const,
    description: i.title,
    status: i.status as IssueStatus,
  };
}

async function searchIssueMentions(
  qc: QueryClient,
  wsId: string,
  q: string,
): Promise<MentionItem[]> {
  // Each call supersedes the previous one — bump the seq and abort any
  // in-flight fetch so a stale response can't overwrite newer suggestions.
  if (issueSearchAbort) issueSearchAbort.abort();
  const seq = ++issueSearchSeq;

  // Empty query: surface recently-touched cached issues (instant, no fetch)
  if (q === "") {
    const cached: Issue[] =
      qc.getQueryData<ListIssuesResponse>(issueKeys.list(wsId))?.issues ?? [];
    return cached.slice(0, 10).map(issueToMention);
  }

  // Server-side search includes done/cancelled via include_closed=true,
  // so done issues are findable even when not in the local cache.
  // Debounce: skip the fetch if a newer keystroke arrives within 150ms.
  await new Promise((r) => setTimeout(r, 150));
  if (seq !== issueSearchSeq) return [];

  const controller = new AbortController();
  issueSearchAbort = controller;
  try {
    const res = await api.searchIssues({
      q,
      limit: 10,
      include_closed: true,
      signal: controller.signal,
    });
    if (seq !== issueSearchSeq) return [];
    return res.issues.map(issueToMention);
  } catch {
    return [];
  }
}

export function createMentionSuggestion(qc: QueryClient): Omit<
  SuggestionOptions<MentionItem>,
  "editor"
> {
  return {
    items: async ({ query }) => {
      // Read workspace id imperatively because this runs in TipTap factory scope
      // (outside React render). getCurrentWsId() is the non-React
      // singleton set by the URL-driven workspace layout.
      const wsId = getCurrentWsId();
      const members: MemberWithUser[] = wsId ? qc.getQueryData(workspaceKeys.members(wsId)) ?? [] : [];
      const agents: Agent[] = wsId ? qc.getQueryData(workspaceKeys.agents(wsId)) ?? [] : [];

      const q = query.toLowerCase();

      // Show "All members" option when query is empty or matches "all"
      const allItem: MentionItem[] =
        "all members".includes(q) || "all".includes(q)
          ? [{ id: "all", label: "All members", type: "all" as const }]
          : [];

      const memberItems: MentionItem[] = members
        .filter((m) => m.name.toLowerCase().includes(q))
        .map((m) => ({
          id: m.user_id,
          label: m.name,
          type: "member" as const,
        }));

      const agentItems: MentionItem[] = agents
        .filter((a) => !a.archived_at && a.name.toLowerCase().includes(q))
        .map((a) => ({ id: a.id, label: a.name, type: "agent" as const }));

      const issueItems = wsId ? await searchIssueMentions(qc, wsId, q) : [];

      return [...allItem, ...memberItems, ...agentItems, ...issueItems].slice(0, 15);
    },

    render: () => {
      let renderer: ReactRenderer<MentionListRef> | null = null;
      let popup: HTMLDivElement | null = null;

      return {
        onStart: (props: SuggestionProps<MentionItem>) => {
          renderer = new ReactRenderer(MentionList, {
            props: { items: props.items, command: props.command },
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
