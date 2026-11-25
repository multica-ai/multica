"use client";

import { useEffect } from "react";
import { useSidebar } from "@multica/ui/components/ui/sidebar";
import {
  getShortcut,
  isEditableShortcutTarget,
  isPortalLayerShortcutTarget,
  isShortcutModifierKey,
  isShortcutRecorderTarget,
  shortcutChordEquals,
  shortcutFirstStep,
  shortcutMatchesEvent,
  SHORTCUT_ACTION_BY_ID,
  useShortcutStore,
  type ShortcutActionId,
  type ShortcutChord,
} from "@multica/core/shortcuts";
import { useChatStore } from "@multica/core/chat";
import { useSearchStore } from "@multica/core/command-bar";
import { useModalStore } from "@multica/core/modals";
import { useCurrentWorkspace, useWorkspacePaths } from "@multica/core/paths";
import { useIsGuest } from "@multica/core/permissions";
import { isImeComposing } from "@multica/core/utils";
import { isFloatingChatRouteSuppressed } from "../chat/floating-chat-visibility";
import { useAppShell } from "./app-shell-context";
import { openCreateIssueForPath } from "./global-actions";
import { useNavigation } from "../navigation";

const GLOBAL_ACTIONS: readonly ShortcutActionId[] = [
  "openSearch",
  "createIssue",
  "toggleSidebar",
  "toggleChat",
  "newChat",
  "showShortcuts",
  "goBack",
  "goForward",
  "goInbox",
  "goChat",
  "goMyIssues",
  "goIssues",
  "goProjects",
  "goAutopilots",
  "goAgents",
  "goSquads",
  "goUsage",
  "goRuntimes",
  "goSkills",
  "goSettings",
];

export function shouldIgnoreGlobalShortcutEvent(event: KeyboardEvent): boolean {
  return event.defaultPrevented || event.repeat || isImeComposing(event);
}

/**
 * What a guest (MUL-9420) cannot do from the keyboard: it has only the Work
 * pages (any other page sends it back to the Inbox) and edits no issue. Its
 * keys stay unclaimed, and the shortcut sheet leaves them out.
 */
const GUEST_UNAVAILABLE_ACTIONS = new Set<ShortcutActionId>([
  "createIssue",
  "toggleChat",
  "newChat",
  "goChat",
  "goAutopilots",
  "goAgents",
  "goSquads",
  "goUsage",
  "goRuntimes",
  "goSkills",
  "setIssueStatus",
  "assignIssue",
  "setIssuePriority",
  "setIssueLabels",
  "setIssueDueDate",
  "moveIssueToProject",
  "runIssueQuickAction",
]);

export function isShortcutActionAvailable(actionId: ShortcutActionId, guest: boolean): boolean {
  return !guest || !GUEST_UNAVAILABLE_ACTIONS.has(actionId);
}

/** How long the first key of a sequence (G) waits for the second. */
export const SHORTCUT_SEQUENCE_TIMEOUT_MS = 1500;

/** Executes configurable product-level shortcuts inside the dashboard shell. */
export function GlobalShortcuts() {
  const { toggleSidebar } = useSidebar();
  const toggleNav = useAppShell()?.toggleNav ?? toggleSidebar;
  const navigation = useNavigation();
  const workspacePaths = useWorkspacePaths();
  const guest = useIsGuest(useCurrentWorkspace()?.id);

  // Subscribe so changing a binding in Settings immediately refreshes the
  // listener closure; getShortcut remains useful to non-React call sites.
  const overrides = useShortcutStore((state) => state.overrides);

  useEffect(() => {
    const chatPath = workspacePaths.chat();
    const destinations: Partial<Record<ShortcutActionId, string>> = {
      goInbox: workspacePaths.inbox(),
      goChat: chatPath,
      goMyIssues: workspacePaths.myIssues(),
      goIssues: workspacePaths.issues(),
      goProjects: workspacePaths.projects(),
      goAutopilots: workspacePaths.autopilots(),
      goAgents: workspacePaths.agents(),
      goSquads: workspacePaths.squads(),
      goUsage: workspacePaths.usage(),
      goRuntimes: workspacePaths.runtimes(),
      goSkills: workspacePaths.skills(),
      goSettings: workspacePaths.settings(),
    };

    // Read at press time rather than subscribing: the preference only matters
    // the instant the chord fires, and the overlay is gone from the Chat tab.
    // An unavailable overlay must not claim the chord either — returning false
    // from the finder leaves the keypress its outside-the-app meaning instead of
    // swallowing it for an action that would visibly do nothing.
    const canUseFloatingChat = () =>
      useChatStore.getState().floatingChatEnabled &&
      !isFloatingChatRouteSuppressed(navigation.pathname, chatPath);

    const run = (actionId: ShortcutActionId) => {
      if (actionId === "openSearch") {
        useSearchStore.getState().toggle();
        return;
      }
      if (actionId === "toggleChat") {
        useChatStore.getState().toggle();
        return;
      }
      if (actionId === "newChat") {
        // Like pressing Ask Multica: a second press closes the new chat it opened.
        const chat = useChatStore.getState();
        if (chat.isOpen && chat.activeSessionId === null) chat.setOpen(false);
        else chat.startNewChat();
        return;
      }
      if (actionId === "toggleSidebar") {
        toggleNav();
        return;
      }
      if (actionId === "goBack") {
        navigation.back();
        return;
      }
      if (actionId === "goForward") {
        // Optional on the adapter: an isolated window without a forward stack
        // leaves it undefined, in which case the chord is simply a no-op.
        navigation.forward?.();
        return;
      }
      if (actionId === "createIssue") {
        if (useModalStore.getState().modal) return;
        openCreateIssueForPath(navigation.pathname);
        return;
      }
      if (actionId === "showShortcuts") {
        if (useModalStore.getState().modal) return;
        useModalStore.getState().open("keyboard-shortcuts");
        return;
      }

      const destination = destinations[actionId];
      if (destination && destination !== navigation.pathname) {
        navigation.push(destination);
      }
    };

    const handleKeyDown = (event: KeyboardEvent) => {
      // Component/editor handlers run before this document-level listener.
      // Respect their preventDefault instead of double-triggering a product
      // action after the focused control already consumed the same chord.
      if (shouldIgnoreGlobalShortcutEvent(event)) return;

      const actionId = GLOBAL_ACTIONS.find((candidate) => {
        const action = SHORTCUT_ACTION_BY_ID[candidate];
        if (!action.allowInEditable && isEditableShortcutTarget(event.target)) {
          return false;
        }
        if ((candidate === "toggleChat" || candidate === "newChat") && !canUseFloatingChat()) {
          return false;
        }
        if (!isShortcutActionAvailable(candidate, guest)) return false;
        return shortcutMatchesEvent(getShortcut(candidate), event);
      });
      if (!actionId) return;

      event.preventDefault();
      run(actionId);
    };

    // Sequences (G then I) are followed on the window, capturing: the second key
    // is often a page's own single-key shortcut (P is priority, C creates an
    // issue), and a pending sequence must take it before any of them.
    let pending: { first: ShortcutChord; timer: ReturnType<typeof setTimeout> } | null = null;
    const clearPending = () => {
      if (pending) clearTimeout(pending.timer);
      pending = null;
    };
    const sequences = () =>
      GLOBAL_ACTIONS.flatMap((id) => {
        if (!isShortcutActionAvailable(id, guest)) return [];
        const shortcut = getShortcut(id);
        return shortcut?.then ? [{ id, first: shortcutFirstStep(shortcut), then: shortcut.then }] : [];
      });

    const handleSequenceKey = (event: KeyboardEvent) => {
      if (shouldIgnoreGlobalShortcutEvent(event) || isShortcutModifierKey(event.key)) return;
      // A field, a menu, a dialog or a shortcut being recorded owns its keys,
      // even when the pointer took focus there between the two keys.
      if (
        isShortcutRecorderTarget(event.target) ||
        isEditableShortcutTarget(event.target) ||
        isPortalLayerShortcutTarget(event.target) ||
        useModalStore.getState().modal
      ) {
        clearPending();
        return;
      }
      if (pending) {
        const { first } = pending;
        clearPending();
        const match = sequences().find(
          (sequence) => shortcutChordEquals(sequence.first, first) && shortcutMatchesEvent(sequence.then, event),
        );
        // Any other key ends the sequence and keeps its own meaning.
        if (!match) return;
        event.preventDefault();
        event.stopImmediatePropagation();
        run(match.id);
        return;
      }
      const first = sequences().find((sequence) => shortcutMatchesEvent(sequence.first, event))?.first;
      if (!first) return;
      event.preventDefault();
      event.stopImmediatePropagation();
      pending = { first, timer: setTimeout(clearPending, SHORTCUT_SEQUENCE_TIMEOUT_MS) };
    };

    window.addEventListener("keydown", handleSequenceKey, true);
    document.addEventListener("keydown", handleKeyDown);
    return () => {
      clearPending();
      window.removeEventListener("keydown", handleSequenceKey, true);
      document.removeEventListener("keydown", handleKeyDown);
    };
  }, [guest, navigation, overrides, toggleNav, workspacePaths]);

  return null;
}
