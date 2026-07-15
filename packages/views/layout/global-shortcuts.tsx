"use client";

import { useEffect } from "react";
import { useQuery } from "@tanstack/react-query";
import { useSidebar } from "@multica/ui/components/ui/sidebar";
import {
  getShortcut,
  isEditableShortcutTarget,
  shortcutMatchesEvent,
  SHORTCUT_ACTION_BY_ID,
  useShortcutStore,
  type ShortcutActionId,
} from "@multica/core/shortcuts";
import { openCreateIssueWithPreference } from "@multica/core/issues/stores";
import { useModalStore } from "@multica/core/modals";
import { useWorkspacePaths } from "@multica/core/paths";
import { useWorkspaceId } from "@multica/core/hooks";
import { mySpaceListOptions } from "@multica/core/spaces/queries";
import type { Space } from "@multica/core/types";
import { isImeComposing } from "@multica/core/utils";
import { useNavigation } from "../navigation";
import { useSearchStore } from "../search/search-store";

const GLOBAL_ACTIONS: readonly ShortcutActionId[] = [
  "openSearch",
  "createIssue",
  "toggleSidebar",
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

export function resolveCreateIssueDefaults(
  pathname: string,
  spaces: Pick<Space, "id" | "key">[],
): { project_id: string } | { space_id: string } | undefined {
  const projectId = pathname.match(/^\/[^/]+\/projects\/([^/]+)$/)?.[1];
  if (projectId) return { project_id: projectId };

  const spaceKey = pathname.match(/^\/[^/]+\/space\/([^/]+)/)?.[1];
  if (!spaceKey) return undefined;
  const spaceId = spaces.find(
    (space) => space.key.toLowerCase() === spaceKey.toLowerCase(),
  )?.id;
  return spaceId ? { space_id: spaceId } : undefined;
}

/** Executes configurable product-level shortcuts inside the dashboard shell. */
export function GlobalShortcuts() {
  const { toggleSidebar } = useSidebar();
  const navigation = useNavigation();
  const workspacePaths = useWorkspacePaths();
  const workspaceId = useWorkspaceId();
  const { data: spaces = [] } = useQuery(mySpaceListOptions(workspaceId));

  // Subscribe so changing a binding in Settings immediately refreshes the
  // listener closure; getShortcut remains useful to non-React call sites.
  const overrides = useShortcutStore((state) => state.overrides);

  useEffect(() => {
    const destinations: Partial<Record<ShortcutActionId, string>> = {
      goInbox: workspacePaths.inbox(),
      goChat: workspacePaths.chat(),
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
        return shortcutMatchesEvent(getShortcut(candidate), event);
      });
      if (!actionId) return;

      event.preventDefault();
      if (actionId === "openSearch") {
        useSearchStore.getState().toggle();
        return;
      }
      if (actionId === "toggleSidebar") {
        toggleSidebar();
        return;
      }
      if (actionId === "createIssue") {
        if (useModalStore.getState().modal) return;
        openCreateIssueWithPreference(
          resolveCreateIssueDefaults(navigation.pathname, spaces),
        );
        return;
      }

      const destination = destinations[actionId];
      if (destination && destination !== navigation.pathname) {
        navigation.push(destination);
      }
    };

    document.addEventListener("keydown", handleKeyDown);
    return () => document.removeEventListener("keydown", handleKeyDown);
  }, [navigation, overrides, spaces, toggleSidebar, workspacePaths]);

  return null;
}
