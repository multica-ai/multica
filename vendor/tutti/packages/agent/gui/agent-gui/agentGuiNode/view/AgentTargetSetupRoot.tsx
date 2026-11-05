import { observer } from "mobx-react-lite";
import { useCallback, useMemo, type ReactNode } from "react";
import { resolveAgentGUIProviderCatalogIdentity } from "../../../providerIdentityCatalog.ts";
import {
  AgentEnvPanelActionProvider,
  type OpenAgentEnvPanelAction
} from "../../../shared/agentEnv";
import {
  AgentTargetSetupControllerProvider,
  useCreateAgentTargetSetupController,
  type AgentTargetSetupController
} from "../../../shared/agentEnv/agentTargetSetupController.tsx";
import type { AgentGUIAgentTarget } from "../../../types.ts";
import {
  projectAgentGUIManagedHomeTargets,
  type AgentGUIManagedHomeTargetProjection
} from "../model/agentGuiProviderRailOrder.ts";
import { AgentTargetSetupGate } from "./AgentTargetSetupGate.tsx";
import { useAgentTargetPreferences } from "./useAgentTargetPreferences.ts";

export function useAgentTargetSetupRoot(input: useAgentTargetSetupRootProps): {
  controller: AgentTargetSetupController;
  environmentSetupVisible: boolean;
  homeTargetProjection: AgentGUIManagedHomeTargetProjection;
  openAgentEnvSetup: () => void;
} {
  const { preferences } = useAgentTargetPreferences();
  const homeTargetProjection = useMemo(
    () =>
      projectAgentGUIManagedHomeTargets({
        agentTargets: input.agentTargets,
        preferences,
        selectedAgentTarget: input.selectedAgentTarget
      }),
    [input.agentTargets, input.selectedAgentTarget, preferences]
  );
  const effectiveSelectedTarget =
    input.activeConversationId === null
      ? (homeTargetProjection.selectedAgentTarget ?? input.selectedAgentTarget)
      : input.selectedAgentTarget;
  const controller = useCreateAgentTargetSetupController(
    effectiveSelectedTarget
  );
  const targetRuntimeSetupVisible =
    effectiveSelectedTarget.ref.setupKind === "target_runtime";
  const environmentSetupVisible =
    targetRuntimeSetupVisible ||
    !!resolveAgentGUIProviderCatalogIdentity(input.environmentProvider ?? "");
  const openAgentEnvSetup = useCallback(() => {
    if (targetRuntimeSetupVisible) {
      controller.setDialogOpen(true);
      return;
    }
    input.openEnvironmentSetup?.({
      provider: input.environmentProvider,
      focus: null
    });
  }, [
    controller,
    input.environmentProvider,
    input.openEnvironmentSetup,
    targetRuntimeSetupVisible
  ]);
  return {
    controller,
    environmentSetupVisible,
    homeTargetProjection,
    openAgentEnvSetup
  };
}

export const AgentTargetSetupRoot = observer(function AgentTargetSetupRoot({
  children,
  controller,
  openEnvironmentSetup
}: {
  children: ReactNode;
  controller: AgentTargetSetupController;
  openEnvironmentSetup?: OpenAgentEnvPanelAction;
}): React.JSX.Element {
  return (
    <AgentEnvPanelActionProvider openPanel={openEnvironmentSetup}>
      <AgentTargetSetupControllerProvider controller={controller}>
        {children}
        <AgentTargetSetupGate
          carouselMountedExternally={false}
          dialogOwner
          gateVisible={false}
        />
      </AgentTargetSetupControllerProvider>
    </AgentEnvPanelActionProvider>
  );
});

type useAgentTargetSetupRootProps = {
  activeConversationId: string | null;
  agentTargets: readonly AgentGUIAgentTarget[];
  environmentProvider: string | null | undefined;
  openEnvironmentSetup?: OpenAgentEnvPanelAction;
  selectedAgentTarget: AgentGUIAgentTarget;
};
