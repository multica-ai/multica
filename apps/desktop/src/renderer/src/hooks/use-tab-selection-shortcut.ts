import { useEffect } from "react";
import type { TabSelectionShortcutKey } from "../../../shared/main-renderer-messages";
import { useTabStore } from "@/stores/tab-store";
import { useWindowOverlayStore } from "@/stores/window-overlay-store";

/**
 * Select a product tab using browser numbering semantics:
 *
 * - 1..8 select that exact one-based position.
 * - 9 always selects the final tab, even when more than nine tabs exist.
 *
 * Missing positions deliberately no-op. Activation goes through the tab store
 * so the coordinator updates the visible route while every session keeps its
 * own history and memento.
 */
export function selectTabByShortcut(key: TabSelectionShortcutKey): void {
  if (useWindowOverlayStore.getState().overlay) return;

  const store = useTabStore.getState();
  const slug = store.activeWorkspaceSlug;
  if (!slug) return;

  const group = store.byWorkspace[slug];
  if (!group) return;

  const tab = key === 9 ? group.tabs.at(-1) : group.tabs[key - 1];
  if (!tab) return;

  store.setActiveTab(tab.id);
}

/**
 * Receive fixed Cmd/Ctrl+1..9 accelerators from Electron's main process.
 * Only the tabbed main window subscribes because main routes the shortcut
 * there regardless of which window or renderer control currently has focus.
 */
export function useTabSelectionShortcut(): void {
  useEffect(() => {
    if (window.desktopAPI.windowContext?.kind === "issue") return undefined;
    // Optional call keeps renderer HMR safe while an old preload remains
    // attached to a refreshed React tree.
    return window.desktopAPI.onSelectTabShortcut?.(selectTabByShortcut);
  }, []);
}
