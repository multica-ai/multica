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
import { useWorkspaceStore } from "@multica/core/workspace";
import { issueKeys } from "@multica/core/issues/queries";
import { workspaceKeys } from "@multica/core/workspace/queries";
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
    return (
      <button
        ref={buttonRef}
        className={`flex w-full items-center gap-2.5 px-3 py-1.5 text-left text-xs transition-colors ${
          selected ? "bg-accent" : "hover:bg-accent/50"
        }`}
        onClick={onSelect}
      >
        {item.status && (
          <StatusIcon status={item.status} className="h-3.5 w-3.5 shrink-0" />
        )}
        <span className="shrink-0 text-muted-foreground">{item.label}</span>
        {item.description && (
          <span className="truncate text-muted-foreground">{item.description}</span>
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

export function createMentionSuggestion(qc: QueryClient): Omit<
  SuggestionOptions<MentionItem>,
  "editor"
> {
  return {
    items: ({ query }) => {
      const wsId = useWorkspaceStore.getState().workspace?.id;
      const members: MemberWithUser[] = wsId ? qc.getQueryData(workspaceKeys.members(wsId)) ?? [] : [];
      const agents: Agent[] = wsId ? qc.getQueryData(workspaceKeys.agents(wsId)) ?? [] : [];
      const issues: Issue[] = wsId
        ? qc.getQueryData<ListIssuesResponse>(issueKeys.list(wsId))?.issues ?? []
        : [];

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

      const issueItems: MentionItem[] = issues
        .filter(
          (i) =>
            i.identifier.toLowerCase().includes(q) ||
            i.title.toLowerCase().includes(q),
        )
        .map((i) => ({
          id: i.id,
          label: i.identifier,
          type: "issue" as const,
          description: i.title,
          status: i.status as IssueStatus,
        }));

      return [...allItem, ...memberItems, ...agentItems, ...issueItems].slice(0, 10);
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
