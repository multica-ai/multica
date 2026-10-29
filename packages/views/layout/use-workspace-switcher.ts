"use client";

import { useCallback, useEffect } from "react";
import { getShortcutPlatform } from "@multica/core/shortcuts";
import { paths } from "@multica/core/paths";
import { isImeComposing } from "@multica/core/utils";
import { useWorkspaceSwitcherStore } from "@multica/core/workspace/switcher-store";
import { useNavigation } from "../navigation";

type SwitcherWorkspace = { id: string; slug: string };

export function useWorkspaceSwitcher(
  workspaces: readonly SwitcherWorkspace[],
  activeWorkspaceId: string | undefined,
) {
  const { push } = useNavigation();
  const isOpen = useWorkspaceSwitcherStore((state) => state.isOpen);
  const setOpen = useWorkspaceSwitcherStore((state) => state.setOpen);
  const requestedIndex = useWorkspaceSwitcherStore((state) => state.requestedIndex);

  const selectWorkspace = useCallback((index: number, close = true) => {
    const workspace = workspaces[index];
    if (!workspace) return;
    if (close) setOpen(false);
    if (workspace.id !== activeWorkspaceId) {
      push(paths.workspace(workspace.slug).issues());
    }
  }, [activeWorkspaceId, push, setOpen, workspaces]);

  useEffect(() => {
    if (requestedIndex === null) return;
    useWorkspaceSwitcherStore.getState().clearSelection();
    if (isOpen) selectWorkspace(requestedIndex);
  }, [isOpen, requestedIndex, selectWorkspace]);

  useEffect(() => {
    if (!isOpen) return;
    const handleKeyDown = (event: KeyboardEvent) => {
      if (isImeComposing(event)) return;
      const mac = getShortcutPlatform() === "macos";
      const primary = mac ? event.metaKey : event.ctrlKey;
      const secondary = mac ? event.ctrlKey : event.metaKey;
      if (!primary || secondary || event.altKey) return;
      const digit = /^[1-9]$/.test(event.key);
      const direction = !event.shiftKey && event.key === "ArrowDown" ? 1
        : !event.shiftKey && event.key === "ArrowUp" ? -1 : 0;
      if (!digit && !direction) return;
      event.preventDefault();
      event.stopPropagation();
      if (event.repeat) return;
      if (digit) {
        selectWorkspace(Number(event.key) - 1);
      } else if (workspaces.length > 0) {
        const current = workspaces.findIndex((workspace) => workspace.id === activeWorkspaceId);
        const next = (current + direction + workspaces.length) % workspaces.length;
        selectWorkspace(next, false);
      }
    };
    window.addEventListener("keydown", handleKeyDown, true);
    return () => window.removeEventListener("keydown", handleKeyDown, true);
  }, [activeWorkspaceId, isOpen, selectWorkspace, workspaces]);

  return { isOpen, setOpen };
}
