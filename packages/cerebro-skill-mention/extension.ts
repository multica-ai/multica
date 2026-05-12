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
import {
  CEREBRO_FLAG_DEFAULTS,
  useCerebroFeatureFlagsStore,
} from "@multica/cerebro-feature-flags";
import {
  SkillMentionList,
  type SkillMentionListRef,
} from "./suggestion-list";
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
  let renderer: ReactRenderer<SkillMentionListRef> | null = null;
  let popup: HTMLDivElement | null = null;

  function buildItems(query: string): SkillMentionItem[] {
    if (!isFeatureEnabled()) return [];
    const wsId = getCurrentWsId();
    if (!wsId) return [];
    const skills =
      qc.getQueryData<SkillSummary[]>(workspaceKeys.skills(wsId)) ?? [];
    return skills
      .map(summaryToItem)
      .filter((s) => fuzzyMatches(s, query))
      .sort((a, b) => a.name.localeCompare(b.name))
      .slice(0, MAX_ITEMS);
  }

  return {
    char: "/skill",
    allowSpaces: true,
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
          // Warm the cache so subsequent keystrokes see results even if
          // the user has never opened the Skills page in this session.
          const wsId = getCurrentWsId();
          if (wsId) void qc.prefetchQuery(skillListOptions(wsId));

          renderer = new ReactRenderer(SkillMentionList, {
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

        onUpdate: (props: SuggestionProps<SkillMentionItem>) => {
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

/**
 * TipTap extension that adds the `/skill` trigger.
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
