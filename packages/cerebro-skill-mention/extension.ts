"use client";

import { Extension } from "@tiptap/core";
import { ReactRenderer } from "@tiptap/react";
import Suggestion, {
  type SuggestionOptions,
  type SuggestionProps,
} from "@tiptap/suggestion";
import { computePosition, offset, flip, shift } from "@floating-ui/dom";
import type { QueryClient } from "@tanstack/react-query";
import { getCurrentWsId } from "@multica/core/platform";
import { workspaceKeys, skillListOptions } from "@multica/core/workspace/queries";
import type { SkillSummary } from "@multica/core/types";
import { isImeComposing } from "@multica/core/utils";
import {
  CEREBRO_FLAG_DEFAULTS,
  useCerebroFeatureFlagsStore,
} from "@multica/cerebro-feature-flags";
import { SkillMentionList } from "./suggestion-list";
import { findSkillSuggestionMatch } from "./find-suggestion-match";
import type { SkillMentionItem } from "./types";

const MAX_ITEMS = 20;

function isFeatureEnabled(): boolean {
  return (
    useCerebroFeatureFlagsStore.getState().overrides.cerebro_skill_mention ??
    CEREBRO_FLAG_DEFAULTS.cerebro_skill_mention
  );
}

function summaryToItem(s: SkillSummary): SkillMentionItem {
  return { id: s.id, name: s.name, description: s.description };
}

function fuzzyMatches(item: SkillMentionItem, query: string): boolean {
  if (!query) return true;
  const q = query.toLowerCase();
  return (
    item.name.toLowerCase().includes(q) ||
    (item.description?.toLowerCase().includes(q) ?? false)
  );
}

export function createSkillSuggestion(
  qc: QueryClient,
): Omit<SuggestionOptions<SkillMentionItem>, "editor"> {
  let renderer: ReactRenderer | null = null;
  let popup: HTMLDivElement | null = null;
  // Keyboard navigation state lives here, not in the React list — this is the
  // single source of truth so Enter/ArrowUp/ArrowDown work even before React
  // has mounted the suggestion popup (FIR-2299 follow-up). The list reads
  // these via props.
  let currentItems: SkillMentionItem[] = [];
  let selectedIndex = 0;
  let activeCommand:
    | ((item: SkillMentionItem) => void)
    | null = null;

  function filterItems(
    skills: SkillSummary[] | undefined,
    query: string,
  ): SkillMentionItem[] {
    return (skills ?? [])
      .map(summaryToItem)
      .filter((s) => fuzzyMatches(s, query))
      .sort((a, b) => a.name.localeCompare(b.name))
      .slice(0, MAX_ITEMS);
  }

  async function buildItems(query: string): Promise<SkillMentionItem[]> {
    if (!isFeatureEnabled()) return [];
    const wsId = getCurrentWsId();
    if (!wsId) return [];
    const cached = qc.getQueryData<SkillSummary[]>(workspaceKeys.skills(wsId));
    if (cached) return filterItems(cached, query);
    const skills = await qc.ensureQueryData(skillListOptions(wsId));
    return filterItems(skills, query);
  }

  function rerender() {
    renderer?.updateProps({
      items: currentItems,
      selectedIndex,
      onHover: (index: number) => {
        selectedIndex = index;
        rerender();
      },
      onSelect: (index: number) => {
        const item = currentItems[index];
        if (item) activeCommand?.(item);
      },
    });
  }

  return {
    char: "/",
    allowSpaces: false,
    startOfLine: false,
    findSuggestionMatch: findSkillSuggestionMatch,

    allow: () => isFeatureEnabled(),

    items: ({ query }) => buildItems(query),

    command: ({ editor, range, props }) => {
      // Mirror @tiptap/extension-mention's default command: insert a
      // `mention` node (the upstream node type, which BaseMentionExtension
      // registers) with `type: "skill"`, then a trailing space.
      const nodeAfter = editor.view.state.selection.$to.nodeAfter;
      const overrideSpace = nodeAfter?.text?.startsWith(" ");
      if (overrideSpace) range.to += 1;

      editor
        .chain()
        .focus()
        .insertContentAt(range, [
          {
            type: "mention",
            attrs: { id: props.id, label: props.name, type: "skill" },
          },
          { type: "text", text: " " },
        ])
        .run();

      window.getSelection()?.collapseToEnd();
    },

    render: () => {
      return {
        onStart: (props: SuggestionProps<SkillMentionItem>) => {
          currentItems = props.items;
          selectedIndex = 0;
          activeCommand = props.command;

          renderer = new ReactRenderer(SkillMentionList, {
            props: {
              items: currentItems,
              selectedIndex,
              onHover: (index: number) => {
                selectedIndex = index;
                rerender();
              },
              onSelect: (index: number) => {
                const item = currentItems[index];
                if (item) activeCommand?.(item);
              },
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

        onUpdate: (props: SuggestionProps<SkillMentionItem>) => {
          currentItems = props.items;
          // Clamp the highlight: if filtering removed items below the cursor,
          // move it onto the last available row so a follow-up Enter still
          // selects something visible.
          if (selectedIndex >= currentItems.length) {
            selectedIndex = Math.max(0, currentItems.length - 1);
          }
          activeCommand = props.command;
          rerender();
          if (popup) updatePosition(popup, props.clientRect);
        },

        onKeyDown: (props: { event: KeyboardEvent }) => {
          const { event } = props;
          if (event.key === "Escape") {
            cleanup();
            return true;
          }
          // IME (Chinese pinyin, Japanese kana) — Enter commits composition,
          // ArrowUp/Down moves IME candidates; do not intercept.
          if (isImeComposing(event)) return false;
          if (event.key === "ArrowUp") {
            if (currentItems.length === 0) return true;
            selectedIndex =
              (selectedIndex + currentItems.length - 1) % currentItems.length;
            rerender();
            return true;
          }
          if (event.key === "ArrowDown") {
            if (currentItems.length === 0) return true;
            selectedIndex = (selectedIndex + 1) % currentItems.length;
            rerender();
            return true;
          }
          if (event.key === "Enter") {
            if (currentItems.length === 0) return true;
            const item = currentItems[selectedIndex];
            if (item) activeCommand?.(item);
            return true;
          }
          return false;
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
        currentItems = [];
        selectedIndex = 0;
        activeCommand = null;
      }
    },
  };
}

/**
 * TipTap extension that adds the `/` slash-command trigger for skills.
 *
 * Typing `/` opens the popover; continuing to type filters the list. Space
 * or another `/` closes the popover.
 *
 * Does NOT define its own node — selecting a skill inserts the upstream
 * `mention` node (defined by BaseMentionExtension) with `type: "skill"`,
 * which means the existing markdown tokenizer roundtrips it as
 * `[name](mention://skill/<id>)` for free.
 *
 * Registered alongside BaseMentionExtension in
 * `packages/views/editor/extensions/index.ts` via a CEREBRO-PATCH marker.
 */
export function createSkillMentionExtension(qc: QueryClient) {
  return Extension.create({
    name: "skillMention",

    // Match @tiptap/extension-mention's priority (101). Tiptap processes the
    // plugins of higher-priority extensions first, and the first plugin whose
    // handleKeyDown returns true wins the event. The chat composer's
    // `submitShortcut` extension binds Enter at the default priority (100); at
    // the default priority this suggestion plugin would sit *after* it, so a
    // bare Enter submitted the chat instead of inserting the highlighted skill
    // while the `/` popup was open (FIR-2301). Bumping to 101 — the same value
    // the upstream `@` mention uses to win Enter — puts the suggestion plugin
    // ahead of submitShortcut so Enter selects the skill.
    priority: 101,

    addProseMirrorPlugins() {
      return [
        Suggestion({
          editor: this.editor,
          ...createSkillSuggestion(qc),
        }),
      ];
    },
  });
}
