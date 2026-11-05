import {
  createContext,
  useContext,
  useMemo,
  type JSX,
  type ReactNode
} from "react";
import type {
  AgentGUIObservationGap,
  AgentGUIObservationGapSource
} from "../../types";
import { useExternalStoreSelector } from "../useExternalStoreSelector";

const AgentObservationGapSourceContext =
  createContext<AgentGUIObservationGapSource | null>(null);

export function AgentObservationGapSourceProvider({
  children,
  source
}: {
  children: ReactNode;
  source?: AgentGUIObservationGapSource | null;
}): JSX.Element {
  return (
    <AgentObservationGapSourceContext.Provider value={source ?? null}>
      {children}
    </AgentObservationGapSourceContext.Provider>
  );
}

export function useAgentObservationGap(
  agentSessionId: string | null | undefined,
  turnId: string | null | undefined,
  explicitSource?: AgentGUIObservationGapSource | null
): AgentGUIObservationGap | null {
  const contextSource = useContext(AgentObservationGapSourceContext);
  const source = explicitSource === undefined ? contextSource : explicitSource;
  const binding = useMemo(
    () => ({
      getSnapshot: () => {
        const normalizedAgentSessionId = agentSessionId?.trim() ?? "";
        const normalizedTurnId = turnId?.trim() ?? "";
        return normalizedAgentSessionId && normalizedTurnId
          ? (source?.getObservationGap(
              normalizedAgentSessionId,
              normalizedTurnId
            ) ?? null)
          : null;
      },
      subscribe: (listener: () => void) =>
        source?.subscribe(listener) ?? (() => undefined)
    }),
    [agentSessionId, source, turnId]
  );
  return useExternalStoreSelector(
    binding,
    identityObservationGap,
    observationGapsEqual
  );
}

function identityObservationGap(
  gap: AgentGUIObservationGap | null
): AgentGUIObservationGap | null {
  return gap;
}

function observationGapsEqual(
  left: AgentGUIObservationGap | null,
  right: AgentGUIObservationGap | null
): boolean {
  return (
    left?.startedAtUnixMs === right?.startedAtUnixMs &&
    left?.presentationState === right?.presentationState
  );
}
