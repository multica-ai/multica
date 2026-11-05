import {
createContext,
useContext,
useState,
useSyncExternalStore,
type ReactNode
} from "react";
import {
createComposerDraftStore,
type ComposerDraftStore
} from "./composer-draft-store";
import type { AgentGuiInitSnapshot } from "./protocol";

const ComposerDraftContext = createContext<ComposerDraftStore | null>(null);

export function ComposerDraftProvider({
  initial,
  children,
  legacyAliases = {}
}: {
  initial: AgentGuiInitSnapshot;
  children: ReactNode;
  legacyAliases?: Readonly<Record<string, string>>;
}) {
  const [store] = useState(() => {
    if (!initial.local || !initial.composerDraftScope)
      return createComposerDraftStore();
    const namespace = JSON.stringify([
      initial.composerDraftScope,
      initial.userId,
      initial.workspaceId
    ]);
    return createComposerDraftStore(
      window.sessionStorage,
      namespace,
      legacyAliases
    );
  });
  return (
    <ComposerDraftContext.Provider value={store}>
      {children}
    </ComposerDraftContext.Provider>
  );
}

/** Replaces the imported GUI's component-local draft owner in the host build. */
export function useComposerDrafts() {
  const store = useContext(ComposerDraftContext);
  if (!store) throw new Error("ComposerDraftProvider is required");
  const state = useSyncExternalStore(
    store.subscribe,
    store.getState,
    store.getInitialState
  );
  return [state.drafts, state.setDrafts] as const;
}
