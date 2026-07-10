"use client";

import { useCallback, useState } from "react";
import { useCommentDraftStore, type CommentDraftKey } from "@multica/core/issues/stores";
// useFlagValue (store selector) instead of useFeatureFlag (query hook): mirrors
// @multica/cerebro-comment-drafts — reads the zustand store with the registry
// default applied, needs no QueryClient/route providers.
import { useFlagValue } from "@multica/cerebro-feature-flags";

export interface DescriptionDraftHandle {
  /**
   * True when a locally-stored draft exists, differs from the server
   * description, and hasn't already been restored or discarded this mount —
   * drives the recovery banner.
   */
  hasRecoverableDraft: boolean;
  /** The stored draft's markdown, to preview or restore into the editor. */
  draftValue: string;
  /** Persist the current editor markdown for this issue. Call on every onUpdate, independent of save success. */
  save: (markdown: string) => void;
  /** Drop the stored draft and hide the banner — call after the user picks Discard. */
  discard: () => void;
  /** Hide the banner without touching storage — call after Restore (the editor remount + save() keep the key fresh). */
  dismissBanner: () => void;
}

function draftKey(issueId: string): CommentDraftKey {
  return `desc:${issueId}`;
}

interface DraftState {
  issueId: string;
  draftValue: string;
  bannerVisible: boolean;
}

/**
 * FIR-2648 — recover an issue description edit that never reached the
 * server, most commonly because the Cloudflare Access session expired
 * mid-type and the debounced save silently failed. Mirrors the TECH-3491
 * comment-draft pattern: every keystroke is mirrored to per-device
 * localStorage (via the shared `useCommentDraftStore`), independent of
 * whether the save mutation succeeds.
 *
 * Unlike a comment composer, the description field always has existing
 * server content, so a stored draft is never used to silently overwrite the
 * editor. It is only surfaced once, on mount, as a dismissible "Restore or
 * discard" banner when it differs from the current server value.
 */
export function useDescriptionDraft(issueId: string, serverValue: string): DescriptionDraftHandle {
  const enabled = useFlagValue("cerebro_description_drafts");
  const setDraft = useCommentDraftStore((s) => s.setDraft);
  const clearDraft = useCommentDraftStore((s) => s.clearDraft);

  const seed = (id: string, current: string): DraftState => {
    const stored = enabled ? useCommentDraftStore.getState().getDraft(draftKey(id)) : undefined;
    return {
      issueId: id,
      draftValue: stored ?? "",
      bannerVisible: !!stored && stored !== current,
    };
  };

  const [state, setState] = useState<DraftState>(() => seed(issueId, serverValue));
  // Re-seed when the editor is pointed at a new issue (React-sanctioned
  // "derive state from a changed prop during render" pattern).
  if (state.issueId !== issueId) {
    setState(seed(issueId, serverValue));
  }

  const save = useCallback(
    (markdown: string) => {
      if (!enabled) return;
      const key = draftKey(issueId);
      if (markdown.trim().length > 0) setDraft(key, markdown);
      else clearDraft(key);
    },
    [enabled, issueId, setDraft, clearDraft],
  );

  const discard = useCallback(() => {
    clearDraft(draftKey(issueId));
    setState((s) => (s.issueId === issueId ? { ...s, bannerVisible: false } : s));
  }, [issueId, clearDraft]);

  const dismissBanner = useCallback(() => {
    setState((s) => (s.issueId === issueId ? { ...s, bannerVisible: false } : s));
  }, [issueId]);

  return {
    hasRecoverableDraft: state.bannerVisible,
    draftValue: state.draftValue,
    save,
    discard,
    dismissBanner,
  };
}
