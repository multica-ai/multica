// CEREBRO-PATCH(use-submit-on-enter): cerebro modification of upstream file
import { useEffect, useState } from "react";
import { useAuthStore } from "@multica/core/auth";

/**
 * Fork-specific: per-user keybinding preference for message composers.
 *
 * Returns `true` when bare Enter should send a message (Slack-style),
 * `false` when only Cmd/Ctrl+Enter should send (and Enter inserts a newline).
 *
 * Default: `false` (Cmd/Ctrl+Enter sends) — matches the legacy comment-input
 * behaviour. Users opt into Enter-to-send via Settings → Account.
 *
 * On touch-primary devices (`(pointer: coarse)`) Enter never sends regardless
 * of the user's preference: there's no Shift/Cmd+Enter affordance on a soft
 * keyboard, so honouring the preference would make newlines unreachable. The
 * on-screen submit button stays available either way.
 */
export function useSubmitOnEnter(): boolean {
  const user = useAuthStore((s) => s.user);
  const value = user?.preferences?.submit_on_enter;
  const isCoarsePointer = useIsCoarsePointer();
  return value === true && !isCoarsePointer;
}

export const SUBMIT_ON_ENTER_KEY = "submit_on_enter";

function useIsCoarsePointer(): boolean {
  const [coarse, setCoarse] = useState<boolean>(() => readCoarsePointer());
  useEffect(() => {
    if (typeof window === "undefined" || typeof window.matchMedia !== "function") {
      return;
    }
    const mql = window.matchMedia("(pointer: coarse)");
    const onChange = () => setCoarse(mql.matches);
    mql.addEventListener("change", onChange);
    return () => mql.removeEventListener("change", onChange);
  }, []);
  return coarse;
}

function readCoarsePointer(): boolean {
  if (typeof window === "undefined" || typeof window.matchMedia !== "function") {
    return false;
  }
  return window.matchMedia("(pointer: coarse)").matches;
}
