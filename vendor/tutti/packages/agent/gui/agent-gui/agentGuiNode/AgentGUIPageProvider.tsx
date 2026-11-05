import { observer } from "mobx-react-lite";
import type { ReactNode } from "react";
import type { AgentGUINodeProps } from "./AgentGUINode.types";
import { AgentGUIShellProvider } from "./AgentGUIShellContext";
import { useAgentGUIPage } from "./controller/useAgentGUIPage";

export type AgentGUIFeatureProps = Omit<
  ReturnType<typeof useAgentGUIPage>,
  "shell"
>;

/** Binds controlled selection and draft effects; content observers read their services directly. */
export const AgentGUIPageProvider = observer(function AgentGUIPageProvider({
  node,
  children
}: {
  node: AgentGUINodeProps;
  children(features: AgentGUIFeatureProps): ReactNode;
}) {
  const {
    identity,
    workspace,
    frame,
    state,
    runtimeRequests,
    hostCapabilities,
    hostActions
  } = node;
  const { shell, ...features } = useAgentGUIPage({
    ...hostCapabilities,
    nodeId: identity.nodeId,
    workspaceId: identity.workspaceId,
    currentUserId: identity.currentUserId,
    workspacePath: workspace.path,
    avoidGroupingEdits: workspace.agentSettings.avoidGroupingEdits,
    isSurfaceActive: frame.isActive,
    isSurfaceVisible: frame.isVisible ?? true,
    data: state,
    composerAppendRequest: runtimeRequests.composerAppend,
    openSessionRequest: runtimeRequests.openSession,
    prefillPromptRequest: runtimeRequests.prefillPrompt,
    onDataChange: hostActions.onUpdateNode,
    onComposerAppendHandled: hostActions.onComposerAppendHandled,
    onRememberComposerDefaults: hostActions.onRememberComposerDefaults,
    onShowMessage: hostActions.onShowMessage
  });
  return (
    <AgentGUIShellProvider value={shell}>
      {children(features)}
    </AgentGUIShellProvider>
  );
});
