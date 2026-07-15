import { useCallback } from "react";
import { useTabStore, useActiveTabHistory } from "@/stores/tab-store";

/**
 * Shell back/forward for the active tab (MUL-4741 session architecture).
 *
 * Per-tab history is a virtual stack on the tab session — the single app
 * router has no usable history of its own (the Coordinator always navigates
 * with replace). goBack/goForward move the session's history index; the
 * Coordinator then reconciles the router to the newly projected URL. No
 * direction hints, no router.navigate(±1).
 */
export function useTabHistory() {
  const { historyIndex, historyLength } = useActiveTabHistory();

  const canGoBack = historyIndex > 0;
  const canGoForward = historyIndex < historyLength - 1;

  const goBack = useCallback(() => {
    useTabStore.getState().goBack();
  }, []);

  const goForward = useCallback(() => {
    useTabStore.getState().goForward();
  }, []);

  return { canGoBack, canGoForward, goBack, goForward };
}
