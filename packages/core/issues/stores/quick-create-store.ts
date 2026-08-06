"use client";

import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { createWorkspaceAwareStorage, registerForWorkspaceRehydration } from "../../platform/workspace-storage";
import { defaultStorage } from "../../platform/storage";
import { registerDraftCleanup } from "../../drafts/cleanup-registry";

export type QuickCreateActorType = "agent" | "squad";

// Per-workspace memory of the last actor (agent or squad) and project the
// user picked in the Quick Create modal. Defaulted to those values on next
// open so frequent users skip the pickers entirely — without this, anyone
// targeting a single project ends up retyping "in project A" on every
// prompt. Persisted with the workspace-aware StateStorage so switching
// workspaces shows the right default automatically. Per-user scoping comes
// for free from localStorage being browser-profile-local — matches how
// draft-store / issues-scope-store / comment-collapse-store already
// namespace themselves.
//
// lastActorType + lastActorId replace the prior `lastAgentId` field once
// squads became selectable. Users who had a persisted agent preference
// land back on whatever the picker shows first; a one-time re-pick is
// preferable to the type-tag ambiguity of overloading a single UUID.
//
// The in-progress agent prompt no longer lives here — it moved into the
// unified issue-create draft's `agent` slot (draft-store) so it shares one
// lifecycle with the manual draft (MUL-5181). This store keeps only the
// last-successful preferences (actor, project) and the shared keep-open toggle.
interface QuickCreateState {
  lastActorType: QuickCreateActorType | null;
  lastActorId: string | null;
  setLastActor: (type: QuickCreateActorType | null, id: string | null) => void;
  lastProjectId: string | null;
  setLastProjectId: (id: string | null) => void;
  keepOpen: boolean;
  setKeepOpen: (v: boolean) => void;
}

export const useQuickCreateStore = create<QuickCreateState>()(
  persist(
    (set) => ({
      lastActorType: null,
      lastActorId: null,
      setLastActor: (type, id) => set({ lastActorType: type, lastActorId: id }),
      lastProjectId: null,
      setLastProjectId: (id) => set({ lastProjectId: id }),
      keepOpen: false,
      setKeepOpen: (v) => set({ keepOpen: v }),
    }),
    {
      name: "multica_quick_create",
      storage: createJSONStorage(() => createWorkspaceAwareStorage(defaultStorage)),
    },
  ),
);

registerForWorkspaceRehydration(() => useQuickCreateStore.persist.rehydrate());

registerDraftCleanup({
  storageKey: "multica_quick_create",
  workspaceScoped: true,
  // Reset the per-user picker memory so it does not survive into the next
  // login on the same tab. (The prompt draft lives in draft-store now.)
  resetInMemory: () =>
    useQuickCreateStore.setState({
      lastActorType: null,
      lastActorId: null,
      lastProjectId: null,
      keepOpen: false,
    }),
});
