import { useCallback, useEffect } from "react";
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

function isEditableTarget(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false;
  if (target.isContentEditable) return true;
  const tag = target.tagName;
  return tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT";
}

/**
 * Keyboard and mouse back/forward for the active tab's history:
 * Cmd/Ctrl+[ / ] and Cmd/Ctrl+←/→ (browser convention), plus mouse side
 * buttons 3/4. The renderer's mouseup is the ONE cross-platform source for
 * side buttons — the main process deliberately does not forward
 * `app-command` (Windows/Linux emit both, which would double-navigate).
 */
export function useNavigationInputBindings() {
  const { goBack, goForward } = useTabHistory();

  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      const mod = e.metaKey || e.ctrlKey;
      if (!mod || e.altKey || e.shiftKey) return;
      const back = e.key === "[" || e.key === "ArrowLeft";
      const forward = e.key === "]" || e.key === "ArrowRight";
      if (!back && !forward) return;
      // In editable contexts these chords belong to the text field:
      // cmd+arrow moves the caret to the line edge, cmd+bracket indents.
      if (isEditableTarget(e.target)) return;
      e.preventDefault();
      if (back) goBack();
      else goForward();
    };
    const onMouseUp = (e: MouseEvent) => {
      if (e.button !== 3 && e.button !== 4) return;
      if (e.button === 3) goBack();
      else goForward();
    };
    window.addEventListener("keydown", onKeyDown);
    window.addEventListener("mouseup", onMouseUp);
    return () => {
      window.removeEventListener("keydown", onKeyDown);
      window.removeEventListener("mouseup", onMouseUp);
    };
  }, [goBack, goForward]);
}
