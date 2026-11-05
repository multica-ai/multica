import {
  createContext,
  useContext,
  useMemo,
  type JSX,
  type PropsWithChildren
} from "react";
import {
  ConversationServicesProvider,
  type ConversationServices
} from "./conversationServices";
import {
  toAgentHostRuntimeApi,
  type AgentHostInputApi,
  type AgentHostRuntimeApi
} from "./host/agentHostApi";
import { observer } from "mobx-react-lite";
export const AgentGUIActivityHostProvider = observer(
  AgentGUIActivityHostProviderRender
);

const AgentActivityHostContext = createContext<AgentHostRuntimeApi | null>(
  null
);

let currentAgentHostApi: AgentHostRuntimeApi | null = null;

export interface AgentGUIActivityHostProviderProps extends PropsWithChildren {
  agentActivityRuntime?: ConversationServices | null;
  agentHostApi?: AgentHostInputApi | null;
}

function AgentGUIActivityHostProviderRender({
  agentActivityRuntime,
  agentHostApi,
  children
}: AgentGUIActivityHostProviderProps): JSX.Element {
  const resolvedAgentHostApi = useMemo(
    () => (agentHostApi ? toAgentHostRuntimeApi(agentHostApi) : null),
    [agentHostApi]
  );
  currentAgentHostApi = resolvedAgentHostApi;
  return (
    <ConversationServicesProvider runtime={agentActivityRuntime}>
      <AgentActivityHostContext.Provider value={resolvedAgentHostApi}>
        {children}
      </AgentActivityHostContext.Provider>
    </ConversationServicesProvider>
  );
}

export function useAgentHostApi(): AgentHostRuntimeApi {
  const agentHostApi =
    useContext(AgentActivityHostContext) ?? getTestAgentHostApi();
  if (!agentHostApi) {
    throw new Error(
      "AgentGUIActivityHostProvider is missing an agentHostApi instance."
    );
  }
  return agentHostApi;
}

export function useOptionalAgentHostApi(): AgentHostRuntimeApi | null {
  return useContext(AgentActivityHostContext) ?? getTestAgentHostApi();
}

export function getOptionalAgentHostApi(): AgentHostRuntimeApi | null {
  return (
    getExplicitWindowTestAgentHostApi() ??
    currentAgentHostApi ??
    getTestAgentHostApi()
  );
}

export function resetAgentHostApiForTests(): void {
  if (process.env.NODE_ENV === "test") {
    currentAgentHostApi = null;
  }
}

export function setAgentHostApiForTests(
  agentHostApi: AgentHostInputApi | AgentHostRuntimeApi | null
): void {
  if (process.env.NODE_ENV === "test") {
    currentAgentHostApi = agentHostApi
      ? toAgentHostRuntimeApi(agentHostApi)
      : null;
  }
}

function getTestAgentHostApi(): AgentHostRuntimeApi | null {
  if (process.env.NODE_ENV !== "test") {
    return null;
  }
  if (typeof window === "undefined") {
    return null;
  }
  const explicitAgentHostApi = getExplicitWindowTestAgentHostApi();
  if (explicitAgentHostApi) {
    return explicitAgentHostApi;
  }
  if (currentAgentHostApi) {
    return currentAgentHostApi;
  }
  const testAgentHostApi = (
    window as unknown as Window & {
      agentHostApi?: AgentHostInputApi | AgentHostRuntimeApi;
    }
  ).agentHostApi;
  return testAgentHostApi ? toAgentHostRuntimeApi(testAgentHostApi) : null;
}

function getExplicitWindowTestAgentHostApi(): AgentHostRuntimeApi | null {
  if (process.env.NODE_ENV !== "test" || typeof window === "undefined") {
    return null;
  }
  const testDescriptor = Object.getOwnPropertyDescriptor(
    window,
    "agentHostApi"
  );
  if (!testDescriptor || !("value" in testDescriptor)) {
    return null;
  }
  const testAgentHostApi = testDescriptor.value as
    | AgentHostInputApi
    | AgentHostRuntimeApi
    | undefined;
  return testAgentHostApi ? toAgentHostRuntimeApi(testAgentHostApi) : null;
}
