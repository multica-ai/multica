import type { AgentActivityUsage } from "@tutti-os/agent-activity-core";
import { useMemo, useRef } from "react";
import type { ActiveConversationState } from "../../../state/ActiveConversationState";
import {
  ComposerCapabilitiesState,
  type ComposerCapabilitiesInput
} from "../../../state/ComposerCapabilitiesState";

type Input = Omit<
  ComposerCapabilitiesInput,
  | "activeConversationId"
  | "activeCanonicalSession"
  | "activeSessionState"
  | "optimisticComposerTarget"
> & { native: ActiveConversationState };
export function useAgentGUIComposerCapabilities({
  native,
  data,
  draftSettingsBySessionId,
  selectedComposerTargetData,
  composer
}: Input) {
  const usageHistory = useRef(new Map<string, AgentActivityUsage>());
  return useMemo(
    () =>
      new ComposerCapabilitiesState(
        {
          data,
          draftSettingsBySessionId,
          selectedComposerTargetData,
          composer,
          get activeConversationId() {
            return native.activeConversationId;
          },
          get activeCanonicalSession() {
            return native.activeCanonicalSession;
          },
          get activeSessionState() {
            return native.activeSessionState;
          },
          get optimisticComposerTarget() {
            const activation = native.activePendingActivation;
            return native.isCreatingConversation &&
              activation?.mode === "new" &&
              activation.agentSessionId === native.activeConversationId &&
              activation.agentTargetId ===
                selectedComposerTargetData.agentTargetId
              ? {
                  agentSessionId: activation.agentSessionId,
                  target: selectedComposerTargetData
                }
              : null;
          }
        },
        usageHistory.current
      ),
    [
      native,
      data,
      draftSettingsBySessionId,
      selectedComposerTargetData,
      composer
    ]
  );
}
