"use client";

import { useCallback, useEffect } from "react";
import { ChevronLeft, ChevronRight } from "lucide-react";
import { useActiveTabHistory, useTabStore } from "@multica/core/tabs";
import { useT } from "../i18n";

/**
 * Per-tab back/forward. In-tab clicks push a virtual stack; the router
 * only `replace`s the address bar so browser Back is not "previous page
 * in this tab" (and not "previous tab"). Desktop already has these
 * buttons next to the traffic lights. Web's tab strip had none, so a
 * finger or a mouse without side buttons could not walk the stack.
 */
export function SessionHistoryControls() {
  const { t } = useT("layout");
  const { historyIndex, historyLength } = useActiveTabHistory();
  const canGoBack = historyIndex > 0;
  const canGoForward = historyIndex < historyLength - 1;

  const goBack = useCallback(() => {
    useTabStore.getState().goBack();
  }, []);
  const goForward = useCallback(() => {
    useTabStore.getState().goForward();
  }, []);

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      const mod = event.metaKey || event.ctrlKey;
      if (!mod || event.altKey || event.shiftKey) return;
      const back = event.key === "ArrowLeft";
      const forward = event.key === "ArrowRight";
      if (!back && !forward) return;
      if (isEditableTarget(event.target)) return;
      event.preventDefault();
      if (back) goBack();
      else goForward();
    };
    const onMouseUp = (event: MouseEvent) => {
      if (event.button !== 3 && event.button !== 4) return;
      if (event.button === 3) goBack();
      else goForward();
    };
    window.addEventListener("keydown", onKeyDown);
    window.addEventListener("mouseup", onMouseUp);
    return () => {
      window.removeEventListener("keydown", onKeyDown);
      window.removeEventListener("mouseup", onMouseUp);
    };
  }, [goBack, goForward]);

  const buttonClassName =
    "relative flex size-7 items-center justify-center rounded-md text-faint-foreground transition-colors hover:bg-muted/50 hover:text-muted-foreground disabled:pointer-events-none disabled:opacity-30 before:absolute before:inset-[-8px] before:content-[''] [@media(hover:hover)_and_(pointer:fine)]:before:content-none";

  return (
    <div className="flex h-full shrink-0 items-center gap-0.5 pl-2 pr-1">
      <button
        type="button"
        onClick={goBack}
        disabled={!canGoBack}
        aria-label={t(($) => $.tab_history.back)}
        title={t(($) => $.tab_history.back)}
        className={buttonClassName}
      >
        <ChevronLeft className="size-4" />
      </button>
      <button
        type="button"
        onClick={goForward}
        disabled={!canGoForward}
        aria-label={t(($) => $.tab_history.forward)}
        title={t(($) => $.tab_history.forward)}
        className={buttonClassName}
      >
        <ChevronRight className="size-4" />
      </button>
    </div>
  );
}

function isEditableTarget(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false;
  if (target.isContentEditable) return true;
  const tag = target.tagName;
  return tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT";
}
