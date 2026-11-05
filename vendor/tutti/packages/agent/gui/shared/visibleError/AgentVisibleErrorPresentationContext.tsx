import {
  createContext,
  useContext,
  useMemo,
  type JSX,
  type PropsWithChildren
} from "react";
import type {
  AgentVisibleErrorOverrides,
  AgentVisibleErrorPresentationScope
} from "../agentEnv/agentErrorPresentation";
import { observer } from "mobx-react-lite";
export const AgentVisibleErrorPresentationProvider = observer(
  AgentVisibleErrorPresentationProviderRender
);

interface AgentVisibleErrorPresentationContextValue {
  overrides: AgentVisibleErrorOverrides | null;
  scope: AgentVisibleErrorPresentationScope;
}

const AgentVisibleErrorPresentationContext =
  createContext<AgentVisibleErrorPresentationContextValue>({
    overrides: null,
    scope: "local_owner"
  });

function AgentVisibleErrorPresentationProviderRender({
  children,
  scope = "local_owner",
  value
}: PropsWithChildren<{
  scope?: AgentVisibleErrorPresentationScope;
  value?: AgentVisibleErrorOverrides | null;
}>): JSX.Element {
  const contextValue = useMemo(
    () => ({ overrides: value ?? null, scope }),
    [scope, value]
  );
  return (
    <AgentVisibleErrorPresentationContext.Provider value={contextValue}>
      {children}
    </AgentVisibleErrorPresentationContext.Provider>
  );
}

export function useAgentVisibleErrorPresentation(): AgentVisibleErrorPresentationContextValue {
  return useContext(AgentVisibleErrorPresentationContext);
}
