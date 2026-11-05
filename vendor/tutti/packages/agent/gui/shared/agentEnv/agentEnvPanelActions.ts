import { observer } from "mobx-react-lite";
import {
  createContext,
  createElement,
  useContext,
  type ReactElement,
  type ReactNode
} from "react";

/** Setup section requested by an AgentGUI remediation action. */
export type AgentEnvPanelFocus =
  | "detect"
  | "install"
  | "repair"
  | "upgrade"
  | "auth"
  | "network"
  | "registry";

export interface OpenAgentEnvPanelInput {
  provider?: string | null;
  focus?: AgentEnvPanelFocus | null;
}

export type OpenAgentEnvPanelAction = (input?: OpenAgentEnvPanelInput) => void;

const AgentEnvPanelActionContext =
  createContext<OpenAgentEnvPanelAction | null>(null);

export const AgentEnvPanelActionProvider = observer(
  function AgentEnvPanelActionProvider({
    children,
    openPanel
  }: {
    children: ReactNode;
    openPanel?: OpenAgentEnvPanelAction;
  }): ReactElement {
    return createElement(
      AgentEnvPanelActionContext.Provider,
      { value: openPanel ?? null },
      children
    );
  }
);

/** Host-injected command; AgentGUI never owns the panel workflow state. */
export function useOpenAgentEnvPanel(): OpenAgentEnvPanelAction | null {
  return useContext(AgentEnvPanelActionContext);
}
