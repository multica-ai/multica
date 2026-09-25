"use client";

import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type Dispatch,
  type SetStateAction,
} from "react";
import {
  fromStoredAgentDraft,
  storedAgentDraftsEqual,
  toStoredAgentDraft,
  type AgentDraft,
  type StoredAgentDraft,
} from "@multica/core/agents";
import { api } from "@multica/core/api";

/** How long editing pauses before the configuration is written back. */
const AUTOSAVE_DELAY_MS = 800;

interface BuilderDraftSync {
  /**
   * False until the stored configuration has been applied (or ruled out). The
   * page must not merge an assistant `<agent_draft>` before this flips, or the
   * reply would land on an empty draft and then be overwritten by the restore.
   */
  restored: boolean;
  /** The assistant message whose draft is already reflected in the form. */
  appliedMessageId: string | null;
  markApplied: (messageId: string) => void;
}

/**
 * Keeps one conversation's configuration in sync with the server: restore it on
 * arrival, then write it back as the user edits.
 *
 * The ordering is the whole point. Autosave is armed only after the restore has
 * run — a save fired while the form still holds its empty initial state would
 * overwrite the stored configuration with nothing, turning "reopen my draft"
 * into "lose my draft". `restored` also gates the assistant-draft merge for the
 * same reason.
 *
 * `appliedMessageId` is persisted alongside the configuration because it is
 * what stops a restore from re-applying the last reply over edits made after
 * it. Held only in memory it died with the page, which is precisely when the
 * next restore needs it.
 */
export function useBuilderDraftSync(options: {
  sessionId: string;
  /** The stored configuration, or null when none was ever saved. */
  stored: StoredAgentDraft | null | undefined;
  /** False while the stored configuration is still being fetched. */
  storedSettled: boolean;
  /** The carrier's runtime — server-owned, never taken from the stored copy. */
  runtimeId: string;
  draft: AgentDraft;
  setDraft: Dispatch<SetStateAction<AgentDraft>>;
}): BuilderDraftSync {
  const { sessionId, stored, storedSettled, runtimeId, draft, setDraft } =
    options;

  const [restored, setRestored] = useState(false);
  const appliedMessageIdRef = useRef<string | null>(null);
  // The last shape known to be on the server, so an unchanged draft never
  // produces a request. Also seeded by the restore, which must not immediately
  // save back what it just read.
  const savedRef = useRef<StoredAgentDraft | null>(null);

  useEffect(() => {
    if (!sessionId || restored || !storedSettled) return;
    if (stored) {
      appliedMessageIdRef.current = stored.applied_message_id;
      savedRef.current = stored;
      setDraft(fromStoredAgentDraft(stored, runtimeId));
    }
    setRestored(true);
  }, [restored, runtimeId, sessionId, setDraft, stored, storedSettled]);

  // What the debounce is holding, so the unmount below can send it. Without
  // this, navigating away inside the debounce window silently dropped the last
  // edits — the exact keystrokes the user is most likely to have just made.
  const pendingRef = useRef<StoredAgentDraft | null>(null);
  const save = useCallback(
    (next: StoredAgentDraft) => {
      savedRef.current = next;
      pendingRef.current = null;
      void api.saveAgentBuilderDraft(sessionId, next).catch(() => {
        // Best-effort: the next edit retries, and one lost write costs the user
        // nothing they can see. An error banner here would interrupt typing for
        // something that self-heals.
        savedRef.current = null;
      });
    },
    [sessionId],
  );
  // Read through a ref by the unmount effect below, which must not re-run — and
  // therefore must not depend on anything.
  const saveRef = useRef(save);
  saveRef.current = save;

  useEffect(() => {
    if (!sessionId || !restored) return;
    const next = toStoredAgentDraft(draft, appliedMessageIdRef.current);
    if (savedRef.current && storedAgentDraftsEqual(savedRef.current, next)) {
      return;
    }
    pendingRef.current = next;
    const timer = setTimeout(() => save(next), AUTOSAVE_DELAY_MS);
    return () => clearTimeout(timer);
  }, [draft, restored, save, sessionId]);

  // Unmount is the deadline: a tab switch, a route change or a session switch
  // all tear this down, and the debounce timer dies with it.
  useEffect(
    () => () => {
      const pending = pendingRef.current;
      if (pending) saveRef.current(pending);
    },
    [],
  );

  return {
    restored,
    appliedMessageId: appliedMessageIdRef.current,
    markApplied: (messageId: string) => {
      appliedMessageIdRef.current = messageId;
    },
  };
}
