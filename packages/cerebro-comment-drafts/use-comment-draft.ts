"use client";

import { useCallback, useState } from "react";
import { useCommentDraftStore, type CommentDraftKey } from "@multica/core/issues/stores";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";

export interface CommentDraftHandle {
  /** Text to seed the editor with on mount — the stored draft, or "" when none. */
  defaultValue: string;
  /** Persist the current editor markdown for this composer. Empty input clears it. */
  save: (markdown: string) => void;
  /** Drop the stored draft — call after a successful send or an explicit cancel. */
  clear: () => void;
  /** True while a non-empty draft is stored — drives the "Kladde gemt" hint. */
  saved: boolean;
}

interface DraftState {
  key: CommentDraftKey;
  defaultValue: string;
  saved: boolean;
}

/**
 * TECH-3491 — per-device draft persistence for the comment / channel / DM
 * composers.
 *
 * The store (`useCommentDraftStore`) already existed, but only the inline-edit
 * composer was ever wired to it; the new-comment and reply composers dropped
 * whatever you had typed the moment you navigated away. This hook is the shared
 * glue those composers use so a half-written message survives leaving the view,
 * a reload, or a virtualization unmount.
 *
 * Storage is the workspace-scoped localStorage the store already uses — i.e.
 * per device (option A, approved by Jesper on TECH-3491). Cross-device sync is
 * intentionally out of scope; it would need server-side draft storage.
 *
 * `key` changes when the composer is pointed at a new surface (issue → channel
 * → DM → thread). Switching surfaces re-renders this hook with a new key, and
 * we re-seed the editor's initial value from that surface's stored draft — the
 * composers re-key their `ContentEditor` on the same id, so it remounts and
 * reads the fresh `defaultValue`.
 */
export function useCommentDraft(key: CommentDraftKey): CommentDraftHandle {
  const enabled = useFeatureFlag("cerebro_comment_drafts");
  const setDraft = useCommentDraftStore((s) => s.setDraft);
  const clearDraft = useCommentDraftStore((s) => s.clearDraft);

  const seed = (k: CommentDraftKey): DraftState => {
    const value = enabled ? (useCommentDraftStore.getState().getDraft(k) ?? "") : "";
    return { key: k, defaultValue: value, saved: value.trim().length > 0 };
  };

  const [state, setState] = useState<DraftState>(() => seed(key));
  // Re-seed when the surface changes (React-sanctioned "derive state from a
  // changed prop during render" pattern — cheaper than an effect + no flash of
  // the previous surface's draft).
  if (state.key !== key) {
    setState(seed(key));
  }

  const save = useCallback(
    (markdown: string) => {
      if (!enabled) return;
      const has = markdown.trim().length > 0;
      if (has) setDraft(key, markdown);
      else clearDraft(key);
      setState((s) => (s.key === key && s.saved === has ? s : { ...s, key, saved: has }));
    },
    [enabled, key, setDraft, clearDraft],
  );

  const clear = useCallback(() => {
    clearDraft(key);
    setState((s) => ({ ...s, key, saved: false }));
  }, [key, clearDraft]);

  return { defaultValue: state.defaultValue, save, clear, saved: state.saved };
}
