"use client";

import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { createWorkspaceAwareStorage, registerForWorkspaceRehydration } from "../../platform/workspace-storage";
import { defaultStorage } from "../../platform/storage";
import { registerDraftCleanup } from "../../drafts/cleanup-registry";

export type QuickCreateActorType = "agent" | "squad";

// Per-workspace memory of the last actor (agent or squad) the user picked in
// the Quick Create modal. Defaulted on next open so frequent users skip the
// picker entirely. Persisted with the workspace-aware StateStorage so
// switching workspaces shows the right default automatically. Per-user
// scoping comes for free from localStorage being browser-profile-local —
// matches how draft-store / issues-scope-store / comment-collapse-store
// already namespace themselves.
//
// The last project is deliberately NOT remembered (MUL-5862). Actor and
// project look symmetrical but aren't: an issue's target project is a
// property of the issue being filed, not a standing preference, so carrying
// the previous one forward guesses wrong as soon as the user moves on — and
// silently files the next issue into a project they never picked. The two
// seeds that survive both have the user's intent behind them: the project
// page they opened the modal from, and their own unfinished draft.
//
// lastActorType + lastActorId replace the prior `lastAgentId` field once
// squads became selectable. Users who had a persisted agent preference
// land back on whatever the picker shows first; a one-time re-pick is
// preferable to the type-tag ambiguity of overloading a single UUID.
//
// The in-progress agent prompt no longer lives here — it moved into the
// unified issue-create draft's `agent` slot (draft-store) so it shares one
// lifecycle with the manual draft (MUL-5181). This store keeps the
// last-successful actor, how many creates each actor has filed (which ranks
// the quick-switch chips, MUL-10012), and the shared keep-open toggle.
interface QuickCreateState {
  lastActorType: QuickCreateActorType | null;
  lastActorId: string | null;
  setLastActor: (type: QuickCreateActorType | null, id: string | null) => void;
  /** Successful creates per actor, keyed by `actorCountKey`. */
  actorCreateCounts: Record<string, number>;
  countActorCreate: (type: QuickCreateActorType, id: string) => void;
  keepOpen: boolean;
  setKeepOpen: (v: boolean) => void;
}

export interface QuickCreateActorRef {
  type: QuickCreateActorType;
  id: string;
}

export function actorCountKey(type: QuickCreateActorType, id: string): string {
  return `${type}:${id}`;
}

/**
 * The actors shown as quick-switch chips: most creates first, ties and the
 * slots no count fills kept in `candidates` order. Callers pass only actors
 * the user can pick right now, so counts left behind by a deleted or
 * archived agent never take a slot.
 */
export function rankFrequentActors(
  counts: Record<string, number>,
  candidates: readonly QuickCreateActorRef[],
  limit: number,
): QuickCreateActorRef[] {
  const countOf = (a: QuickCreateActorRef) => counts[actorCountKey(a.type, a.id)] ?? 0;
  return candidates
    .map((actor, index) => ({ actor, index, count: countOf(actor) }))
    .sort((a, b) => b.count - a.count || a.index - b.index)
    .slice(0, limit)
    .map(({ actor }) => actor);
}

export const useQuickCreateStore = create<QuickCreateState>()(
  persist(
    (set) => ({
      lastActorType: null,
      lastActorId: null,
      setLastActor: (type, id) => set({ lastActorType: type, lastActorId: id }),
      actorCreateCounts: {},
      countActorCreate: (type, id) =>
        set((s) => {
          const key = actorCountKey(type, id);
          return {
            actorCreateCounts: { ...s.actorCreateCounts, [key]: (s.actorCreateCounts[key] ?? 0) + 1 },
          };
        }),
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
      actorCreateCounts: {},
      keepOpen: false,
    }),
});
