import {
  createContext,
  useContext,
  useMemo,
  type JSX,
  type ReactNode
} from "react";
import type {
  AgentGUIAgentTarget,
  AgentGUIAgentTargetInfoRenderer
} from "../types";
import { observer } from "mobx-react-lite";
export const AgentTargetInfoRendererProvider = observer(
  AgentTargetInfoRendererProviderRender
);

interface AgentTargetInfoRendererContextValue {
  renderer: AgentGUIAgentTargetInfoRenderer | null;
  targetsByAgentTargetId: ReadonlyMap<string, AgentGUIAgentTarget>;
}

const EMPTY_TARGETS_BY_AGENT_TARGET_ID = new Map<string, AgentGUIAgentTarget>();

const AgentTargetInfoRendererContext =
  createContext<AgentTargetInfoRendererContextValue>({
    renderer: null,
    targetsByAgentTargetId: EMPTY_TARGETS_BY_AGENT_TARGET_ID
  });

function AgentTargetInfoRendererProviderRender({
  agentTargets,
  children,
  renderer
}: {
  agentTargets: readonly AgentGUIAgentTarget[];
  children: ReactNode;
  renderer?: AgentGUIAgentTargetInfoRenderer | null;
}): JSX.Element {
  const value = useMemo<AgentTargetInfoRendererContextValue>(() => {
    const targetsByAgentTargetId = new Map<string, AgentGUIAgentTarget>();
    for (const target of agentTargets) {
      const agentTargetId = target.agentTargetId?.trim() ?? "";
      if (agentTargetId && !targetsByAgentTargetId.has(agentTargetId)) {
        targetsByAgentTargetId.set(agentTargetId, target);
      }
    }
    return {
      renderer: renderer ?? null,
      targetsByAgentTargetId
    };
  }, [agentTargets, renderer]);

  return (
    <AgentTargetInfoRendererContext.Provider value={value}>
      {children}
    </AgentTargetInfoRendererContext.Provider>
  );
}

export function useAgentTargetInfoRenderer(): AgentGUIAgentTargetInfoRenderer | null {
  return useContext(AgentTargetInfoRendererContext).renderer;
}

export function useAgentTargetInfoTarget(
  agentTargetId: string | null | undefined
): AgentGUIAgentTarget | null {
  const normalizedAgentTargetId = agentTargetId?.trim() ?? "";
  const { targetsByAgentTargetId } = useContext(AgentTargetInfoRendererContext);
  return normalizedAgentTargetId
    ? (targetsByAgentTargetId.get(normalizedAgentTargetId) ?? null)
    : null;
}
