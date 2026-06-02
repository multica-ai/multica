"use client";

import {
  forwardRef,
  useCallback,
  useEffect,
  useImperativeHandle,
  useRef,
  useState,
} from "react";
import { computePosition, flip, offset, shift } from "@floating-ui/dom";
import { ReactRenderer } from "@tiptap/react";
import type { QueryClient } from "@tanstack/react-query";
import type { SuggestionOptions, SuggestionProps } from "@tiptap/suggestion";
import { useAuthStore } from "@multica/core/auth";
import { useChatStore } from "@multica/core/chat";
import { getCurrentWsId } from "@multica/core/platform";
import { canAssignAgentToIssue } from "@multica/core/permissions";
import { isImeComposing } from "@multica/core/utils";
import { workspaceKeys } from "@multica/core/workspace/queries";
import type { Agent, MemberWithUser } from "@multica/core/types";
import { useT } from "../../i18n";

const MAX_ITEMS = 20;

export interface SlashCommandItem {
  id: string;
  label: string;
  description: string;
}

interface SlashCommandListProps {
  items: SlashCommandItem[];
  query: string;
  command: (item: SlashCommandItem) => void;
}

export interface SlashCommandListRef {
  onKeyDown: (props: { event: KeyboardEvent }) => boolean;
}

export const SlashCommandList = forwardRef<
  SlashCommandListRef,
  SlashCommandListProps
>(function SlashCommandList({ items, query, command }, ref) {
  const { t } = useT("editor");
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
      if (!item) return;
      command(item);
    },
    [items, command],
  );

  useImperativeHandle(ref, () => ({
    onKeyDown: ({ event }) => {
      if (isImeComposing(event)) return false;
      if (event.key === "ArrowUp") {
        if (items.length === 0) return false;
        setSelectedIndex((i) => (i + items.length - 1) % items.length);
        return true;
      }
      if (event.key === "ArrowDown") {
        if (items.length === 0) return false;
        setSelectedIndex((i) => (i + 1) % items.length);
        return true;
      }
      if (event.key === "Enter") {
        if (items.length === 0) return false;
        selectItem(selectedIndex);
        return true;
      }
      return false;
    },
  }));

  if (items.length === 0) {
    return (
      <div className="rounded-md border bg-popover p-2 text-xs text-muted-foreground shadow-md">
        {t(($) =>
          query.trim()
            ? $.slash_command.no_results
            : $.slash_command.no_skills_configured,
        )}
      </div>
    );
  }

  return (
    <div className="rounded-md border bg-popover py-1 shadow-md w-72 max-h-[300px] overflow-y-auto">
      {items.map((item, index) => (
        <button
          key={item.id}
          ref={(el) => {
            itemRefs.current[index] = el;
          }}
          className={`flex w-full flex-col gap-0.5 px-3 py-1.5 text-left text-xs transition-colors ${
            selectedIndex === index ? "bg-accent" : "hover:bg-accent/50"
          }`}
          onClick={() => selectItem(index)}
        >
          <span className="font-medium">/{item.label}</span>
          {item.description && (
            <span className="truncate text-muted-foreground">
              {item.description}
            </span>
          )}
        </button>
      ))}
    </div>
  );
});

function buildItems(qc: QueryClient, query: string): SlashCommandItem[] {
  const wsId = getCurrentWsId();
  if (!wsId) return [];

  const agents: Agent[] = qc.getQueryData(workspaceKeys.agents(wsId)) ?? [];
  const members: MemberWithUser[] =
    qc.getQueryData(workspaceKeys.members(wsId)) ?? [];
  // Tiptap calls suggestion items outside React render, so direct store reads
  // are intentional here.
  const { selectedAgentId } = useChatStore.getState();
  const userId = useAuthStore.getState().user?.id ?? null;
  const memberRole = members.find((m) => m.user_id === userId)?.role ?? null;

  const availableAgents = agents.filter(
    (a) =>
      !a.archived_at &&
      canAssignAgentToIssue(a, { userId, role: memberRole }).allowed,
  );
  const activeAgent =
    availableAgents.find((a) => a.id === selectedAgentId) ??
    availableAgents[0] ??
    null;

  const q = query.toLowerCase();
  return (activeAgent?.skills ?? [])
    .filter(
      (s) =>
        !q ||
        s.name.toLowerCase().includes(q) ||
        (s.description ?? "").toLowerCase().includes(q),
    )
    .slice(0, MAX_ITEMS)
    .map((s) => ({ id: s.id, label: s.name, description: s.description ?? "" }));
}

export function createSlashCommandSuggestion(qc: QueryClient): Omit<
  SuggestionOptions<SlashCommandItem>,
  "editor"
> {
  let renderer: ReactRenderer<SlashCommandListRef> | null = null;
  let popup: HTMLDivElement | null = null;

  return {
    char: "/",
    items: ({ query }) => buildItems(qc, query),
    command: ({ editor, range, props }) => {
      const nodeAfter = editor.view.state.selection.$to.nodeAfter;
      const overrideSpace = nodeAfter?.text?.startsWith(" ");
      if (overrideSpace) {
        range.to += 1;
      }

      editor
        .chain()
        .focus()
        .insertContentAt(range, [
          {
            type: "slashCommand",
            attrs: {
              id: props.id,
              label: props.label,
              mentionSuggestionChar: "/",
            },
          },
          { type: "text", text: " " },
        ])
        .run();

      window.getSelection()?.collapseToEnd();
    },
    render: () => {
      return {
        onStart: (props: SuggestionProps<SlashCommandItem>) => {
          renderer = new ReactRenderer(SlashCommandList, {
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
        onUpdate: (props: SuggestionProps<SlashCommandItem>) => {
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
}
