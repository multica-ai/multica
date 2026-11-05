import { useMemo } from "react";
import { useExternalStoreSelector } from "../../../shared/useExternalStoreSelector";
import type {
  AgentGUITargetConnectionSource,
  AgentGUITargetConnectionState
} from "../../../types";
import {
  AgentGUITargetConnectionController,
  type AgentGUITargetConnectionBinding
} from "./AgentGUITargetConnectionController";

export function useAgentGUITargetConnectionState(input: {
  agentTargetId?: string | null;
  source?: AgentGUITargetConnectionSource | null;
}): {
  blocked: boolean;
  visibleState: AgentGUITargetConnectionState | null;
} {
  const binding = useMemo<AgentGUITargetConnectionBinding>(
    () => ({
      getSnapshot: () => {
        const agentTargetId = input.agentTargetId?.trim() ?? "";
        return agentTargetId
          ? (input.source?.getConnectionState(agentTargetId) ?? null)
          : null;
      },
      subscribe: (listener) =>
        input.source?.subscribe(listener) ?? (() => undefined)
    }),
    [input.agentTargetId, input.source]
  );
  const state = useExternalStoreSelector(
    binding,
    identityTargetConnectionState,
    targetConnectionStatesEqual
  );
  const controller = useMemo(
    () => new AgentGUITargetConnectionController({ source: binding }),
    [binding]
  );
  const visibleState = useExternalStoreSelector(
    controller,
    identityTargetConnectionState,
    targetConnectionStatesEqual
  );
  return {
    blocked: state?.status === "connecting" || state?.status === "unavailable",
    visibleState
  };
}

function identityTargetConnectionState(
  state: AgentGUITargetConnectionState | null
): AgentGUITargetConnectionState | null {
  return state;
}

function targetConnectionStatesEqual(
  left: AgentGUITargetConnectionState | null,
  right: AgentGUITargetConnectionState | null
): boolean {
  return (
    left?.status === right?.status && left?.retryAttempt === right?.retryAttempt
  );
}
