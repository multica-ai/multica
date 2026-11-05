import { createContext, useContext, type PropsWithChildren } from "react";
import type { AgentGUIShellViewModel } from "./model/agentGuiNodeTypes";
import { observer } from "mobx-react-lite";
export const AgentGUIShellProvider = observer(AgentGUIShellProviderRender);

const AgentGUIShellContext = createContext<AgentGUIShellViewModel | null>(null);

function AgentGUIShellProviderRender({
  value,
  children
}: PropsWithChildren<{ value: AgentGUIShellViewModel }>) {
  return (
    <AgentGUIShellContext.Provider value={value}>
      {children}
    </AgentGUIShellContext.Provider>
  );
}

export function useAgentGUIShell(): AgentGUIShellViewModel {
  const shell = useContext(AgentGUIShellContext);
  if (!shell) throw new Error("AgentGUIShellProvider is required");
  return shell;
}
