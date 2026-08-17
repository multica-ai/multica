"use client";

import {
  useEffect,
  useState,
  type Dispatch,
  type SetStateAction,
} from "react";
import {
  fromStoredAgentDraft,
  manualDraftOwner,
  setManualDraftEntry,
  toStoredAgentDraft,
  useManualAgentDraftStore,
  type AgentDraft,
} from "@multica/core/agents";

/**
 * Keeps the manual creation form across navigation (#6287).
 *
 * The reported bug is a tab switch: the desktop shell mounts only the active
 * tab, so coming back is a remount and everything typed was gone. A route
 * change and a reload lose it the same way, so the fix has to be persistence,
 * not a guard on one exit.
 *
 * Two orderings matter here, and both were bugs before they were rules:
 *
 *   - Restore runs before save is armed. A write fired while the form still
 *     held its empty initial state would overwrite the stored draft with
 *     nothing — turning "come back to my form" into "lose my form".
 *   - Writes are scoped to this flow's own slot. Opening a blank form must not
 *     erase a half-finished copy of some agent, and vice versa; the store keys
 *     drafts by owner precisely so one flow cannot answer for another.
 */
export function useManualDraftSync(options: {
  duplicateId: string | null;
  draft: AgentDraft;
  setDraft: Dispatch<SetStateAction<AgentDraft>>;
  /** False until a duplicate has finished seeding; saving before then would
   *  persist the empty form the seed is about to replace. */
  ready: boolean;
}): void {
  const { duplicateId, draft, setDraft, ready } = options;
  const owner = manualDraftOwner(duplicateId);

  const setStored = useManualAgentDraftStore((state) => state.setDraft);
  const [restored, setRestored] = useState(false);

  useEffect(() => {
    if (restored || !ready) return;
    // Read at apply time rather than subscribing: this runs once, and a
    // subscription would re-fire it on every keystroke's own write.
    const persisted = useManualAgentDraftStore.getState().draft.byOwner[owner];
    if (persisted) {
      setDraft(fromStoredAgentDraft(persisted.draft, persisted.runtimeId));
    }
    setRestored(true);
  }, [owner, ready, restored, setDraft]);

  useEffect(() => {
    if (!restored) return;
    const current = useManualAgentDraftStore.getState().draft;
    setStored(
      setManualDraftEntry(current, owner, {
        runtimeId: draft.runtimeId,
        draft: toStoredAgentDraft(draft, null),
      }),
    );
  }, [draft, owner, restored, setStored]);
}

/** Drops this flow's slot once its agent exists, leaving other flows alone. */
export function clearManualDraftForOwner(duplicateId: string | null): void {
  const store = useManualAgentDraftStore.getState();
  store.setDraft(
    setManualDraftEntry(store.draft, manualDraftOwner(duplicateId), null),
  );
}
