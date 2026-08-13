"use client";

import { useEffect } from "react";
import { useSidebar } from "@multica/ui/components/ui/sidebar";
import {
  getShortcut,
  isEditableShortcutTarget,
  shortcutMatchesEvent,
  SHORTCUT_ACTION_BY_ID,
  useShortcutStore,
  type ShortcutActionId,
} from "@multica/core/shortcuts";
import { useChatStore } from "@multica/core/chat";
import { openCreateIssueWithPreference } from "@multica/core/issues/stores";
import { useModalStore } from "@multica/core/modals";
import { useWorkspacePaths } from "@multica/core/paths";
import { isImeComposing } from "@multica/core/utils";
import { isFloatingChatRouteSuppressed } from "../chat/floating-chat-visibility";
import { useNavigation } from "../navigation";
import { useSearchStore } from "../search/search-store";

const GLOBAL_ACTIONS: readonly ShortcutActionId[] = [
  "openSearch",
  "createIssue",
  "toggleSidebar",
  "toggleChat",
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

/** Executes configurable product-level shortcuts inside the dashboard shell. */
export function GlobalShortcuts() {
  const { toggleSidebar } = useSidebar();
  const navigation = useNavigation();
  const workspacePaths = useWorkspacePaths();

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
    const canToggleFloatingChat = () =>
      useChatStore.getState().floatingChatEnabled &&
      !isFloatingChatRouteSuppressed(navigation.pathname, chatPath);

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
        if (candidate === "toggleChat" && !canToggleFloatingChat()) return false;
        return shortcutMatchesEvent(getShortcut(candidate), event);
      });
      if (!actionId) return;

      event.preventDefault();
      if (actionId === "openSearch") {
        useSearchStore.getState().toggle();
        return;
      }
      if (actionId === "toggleChat") {
        useChatStore.getState().toggle();
        return;
      }
      if (actionId === "toggleSidebar") {
        toggleSidebar();
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
        const projectMatch = navigation.pathname.match(
          /^\/[^/]+\/projects\/([^/]+)$/,
        );
        const data = projectMatch
          ? { project_id: projectMatch[1] }
          : undefined;
        openCreateIssueWithPreference(data);
        return;
      }

      const destination = destinations[actionId];
      if (destination && destination !== navigation.pathname) {
        navigation.push(destination);
      }
    };

    document.addEventListener("keydown", handleKeyDown);
    return () => document.removeEventListener("keydown", handleKeyDown);
  }, [navigation, overrides, toggleSidebar, workspacePaths]);

  return null;
}
