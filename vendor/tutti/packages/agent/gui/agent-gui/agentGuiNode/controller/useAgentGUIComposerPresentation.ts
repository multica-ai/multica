import { useMemo } from "react";
import type { ActiveConversationState } from "../../../state/ActiveConversationState";
import type { ComposerCapabilitiesState } from "../../../state/ComposerCapabilitiesState";
import {
  ComposerSettingsState,
  type ComposerSettingsInput
} from "../../../state/ComposerSettingsState";

type CapabilityFields =
  | "composerSupport"
  | "composerOptionsLoadStatus"
  | "composerOptionsLoading"
  | "connectorOptionsLoading"
  | "composerTargetProvider"
  | "defaultReasoningEffort"
  | "providerComposerOptions"
  | "composerTargetData";
type Input = Omit<
  ComposerSettingsInput,
  | CapabilityFields
  | "activeConversationId"
  | "activeCanonicalSession"
  | "activeSessionState"
  | "activeConversation"
> & {
  native: ActiveConversationState;
  capabilities: ComposerCapabilitiesState;
  conversation(): ComposerSettingsInput["activeConversation"];
};
export function useAgentGUIComposerPresentation(input: Input) {
  const {
    native,
    capabilities,
    conversation,
    agentActivityRuntime,
    codexSaverModeEntryEnabled,
    rtkSaverModeEntryEnabled,
    data,
    draftSettingsBySessionId,
    selectedComposerTargetData,
    selectedProjectPath,
    shouldApplyPreparedProjectSelection,
    userProjects
  } = input;
  return useMemo(
    () =>
      new ComposerSettingsState({
        agentActivityRuntime,
        codexSaverModeEntryEnabled,
        rtkSaverModeEntryEnabled,
        data,
        draftSettingsBySessionId,
        selectedComposerTargetData,
        selectedProjectPath,
        shouldApplyPreparedProjectSelection,
        userProjects,
        get activeConversation() {
          return conversation();
        },
        get activeConversationId() {
          return native.activeConversationId;
        },
        get activeCanonicalSession() {
          return native.activeCanonicalSession;
        },
        get activeSessionState() {
          return native.activeSessionState;
        },
        get composerSupport() {
          return capabilities.composerSupport;
        },
        get composerOptionsLoadStatus() {
          return capabilities.composerOptionsLoadStatus;
        },
        get composerOptionsLoading() {
          return capabilities.composerOptionsLoading;
        },
        get connectorOptionsLoading() {
          return capabilities.connectorOptionsLoading;
        },
        get composerTargetProvider() {
          return capabilities.composerTargetData.provider;
        },
        get defaultReasoningEffort() {
          return capabilities.defaultReasoningEffort;
        },
        get providerComposerOptions() {
          return capabilities.providerComposerOptions;
        },
        get composerTargetData() {
          return capabilities.composerTargetData;
        }
      }),
    [
      native,
      capabilities,
      conversation,
      agentActivityRuntime,
      codexSaverModeEntryEnabled,
      rtkSaverModeEntryEnabled,
      data,
      draftSettingsBySessionId,
      selectedComposerTargetData,
      selectedProjectPath,
      shouldApplyPreparedProjectSelection,
      userProjects
    ]
  );
}
