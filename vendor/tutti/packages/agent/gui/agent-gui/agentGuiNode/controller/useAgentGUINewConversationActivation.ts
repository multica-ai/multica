import {
  type AgentActivityComposerOptions,
  type AgentActivityRailPlacement,
  isPendingActivationViable
} from "@tutti-os/agent-activity-core";
import { useCallback } from "react";
import { translate } from "../../../i18n/index";
import { deriveAgentGUIOptimisticConversationTitle } from "../../../shared/agentConversationTitleProjection";
import type { AgentSessionComposerSettings } from "../../../shared/agentSessionTypes";
import type { AgentPromptContentBlock } from "../../../shared/contracts/dto";
import type { AgentComposerSubmitOptions } from "../composer/AgentComposer.types";
import {
  agentPromptContentDisplayText,
  emptyAgentComposerDraft,
  normalizeAgentPromptContentBlocks,
  snapshotAgentComposerDraft,
  textPromptContent
} from "../model/agentComposerDraft";
import { resolveAgentComposerDraftScopeKey } from "../model/agentComposerDraftScope";
import {
  type AgentGUIConversationUserProject,
  resolveAgentGUISelectedUserProject
} from "../model/agentGuiConversationProjectResolver";
import {
  normalizePermissionModeId,
  permissionConfigFromComposerOptions,
  readNodeDefaultDraftSettings
} from "./agentGuiController.composerHelpers";
import { effectiveComposerSettingsFromOptions } from "./agentGuiController.composerPresentation";
import { toRuntimeSendContent } from "./agentGuiController.draftMessageHelpers";
import {
  createAgentGUIConversationId,
  normalizeOptionalPrompt,
  normalizeOptionalText
} from "./agentGuiController.promptHelpers";
import {
  agentSubmitTraceDiagnostics,
  createAgentSubmitTraceState,
  reportAgentSubmitTraceDiagnostic
} from "./agentGuiController.reporting";
import { draftAgentSessionIdFromComposerOptions } from "./agentGuiController.stableHelpers";
import {
  type AgentGUINewConversationActivationResult,
  type UseAgentGUINewConversationActivationInput
} from "./agentGuiNewConversationActivation.types";

function firstResolvedComposerText(
  ...values: Array<string | null | undefined>
): string | null {
  for (const value of values) {
    const normalized = normalizeOptionalText(value);
    if (normalized) {
      return normalized;
    }
  }
  return null;
}

/**
 * Resolve the sparse Create payload to match the home composer presentation.
 *
 * Remembered defaults can retire out of the optimistic draft after authority
 * confirmation while the UI still shows them via effectiveSettings /
 * permissionConfig.defaultValue. Create must send those presented values so
 * the first turn does not fall back to the provider default (for Codex: auto /
 * "Approve for me").
 */
export function resolveSparseNewConversationActivationSettings(input: {
  draftSettings: AgentSessionComposerSettings;
  composerOptions: AgentActivityComposerOptions | null;
  requiredSettingsPatch?: Partial<AgentSessionComposerSettings> | null;
  codexSaverModeEntryEnabled?: boolean;
  rtkSaverModeEntryEnabled?: boolean;
}): AgentSessionComposerSettings {
  const draft = input.draftSettings ?? {};
  const patch = input.requiredSettingsPatch ?? {};
  const effective = effectiveComposerSettingsFromOptions(input.composerOptions);
  const permissionConfig = permissionConfigFromComposerOptions(
    input.composerOptions
  );
  const model = firstResolvedComposerText(
    typeof patch.model === "string" ? patch.model : null,
    draft.model,
    effective?.model
  );
  const explicitReasoningEffort = firstResolvedComposerText(
    typeof patch.reasoningEffort === "string" ? patch.reasoningEffort : null
  );
  const inheritedReasoningEffort = firstResolvedComposerText(
    draft.reasoningEffort,
    effective?.reasoningEffort
  );
  const reasoningEffort =
    explicitReasoningEffort ??
    resolveInheritedReasoningEffortForModel(
      input.composerOptions,
      model,
      inheritedReasoningEffort
    );
  const speed = firstResolvedComposerText(
    typeof patch.speed === "string" ? patch.speed : null,
    draft.speed,
    effective?.speed
  );
  const permissionModeId = firstResolvedComposerText(
    typeof patch.permissionModeId === "string" ? patch.permissionModeId : null,
    draft.permissionModeId,
    effective?.permissionModeId,
    permissionConfig?.defaultValue
  );
  return {
    ...draft,
    ...patch,
    ...(model ? { model } : {}),
    reasoningEffort: reasoningEffort ?? undefined,
    ...(speed ? { speed } : {}),
    ...(permissionModeId
      ? { permissionModeId: normalizePermissionModeId(permissionModeId) }
      : {}),
    // Fail closed at the activation boundary. Presentation gating alone is
    // insufficient because a remembered draft can outlive the lab flag.
    codexSaverMode:
      input.codexSaverModeEntryEnabled === true &&
      input.composerOptions?.codexSaverModeSupported === true &&
      (patch.codexSaverMode ??
        draft.codexSaverMode ??
        input.composerOptions.effectiveSettings?.codexSaverMode) === true,
    rtkSaverMode:
      input.rtkSaverModeEntryEnabled === true &&
      input.composerOptions?.rtkSaverModeSupported === true &&
      (patch.rtkSaverMode ??
        draft.rtkSaverMode ??
        input.composerOptions.effectiveSettings?.rtkSaverMode) === true
  };
}

export function resolveNewConversationSettingProvenance(
  requiredSettingsPatch?: Partial<AgentSessionComposerSettings> | null
): { modelExplicit: boolean; reasoningEffortExplicit: boolean } {
  return {
    modelExplicit: typeof requiredSettingsPatch?.model === "string",
    reasoningEffortExplicit:
      typeof requiredSettingsPatch?.reasoningEffort === "string"
  };
}

function resolveInheritedReasoningEffortForModel(
  options: AgentActivityComposerOptions | null,
  model: string | null,
  selected: string | null
): string | null {
  if (options?.provider?.trim().toLowerCase() !== "opencode" || !model) {
    return selected;
  }
  const profile = options.reasoningOptionsByModel?.[model];
  if (!profile) {
    // OpenCode's strict catalog is authoritative. Missing per-model metadata
    // means there is no safe inherited dependent value to forward.
    return null;
  }
  const supported = new Set(profile.options.map((option) => option.value));
  if (selected && supported.has(selected)) return selected;
  const advertisedDefault = profile.defaultValue?.trim() ?? "";
  if (advertisedDefault && supported.has(advertisedDefault)) {
    return advertisedDefault;
  }
  return profile.options[0]?.value ?? null;
}

export function resolveInitialRailPlacement(input: {
  selectedProjectPath: string | null | undefined;
  userProjects: readonly AgentGUIConversationUserProject[];
}): AgentActivityRailPlacement | null {
  const selectedProjectPath = input.selectedProjectPath?.trim() ?? "";
  if (!selectedProjectPath) {
    return {
      version: 1,
      kind: "conversations",
      sectionKey: "conversations"
    };
  }
  const selectedProject = resolveAgentGUISelectedUserProject(
    selectedProjectPath,
    input.userProjects
  );
  if (!selectedProject) {
    return null;
  }
  const sectionKey = selectedProject.sectionKey?.trim() ?? "";
  if (!sectionKey) {
    return null;
  }
  return {
    version: 1,
    kind: "project",
    projectPath: selectedProject.path.trim(),
    sectionKey
  };
}

export function useAgentGUINewConversationActivation(
  input: UseAgentGUINewConversationActivationInput
) {
  const {
    codexSaverModeEntryEnabled,
    rtkSaverModeEntryEnabled,
    getCachedComposerOptions,
    selectedAgentTargetRef,
    selectedComposerTargetDataRef,
    agentTargetsProvidedRef,
    selectedAgentTargetIsExplicitRef,
    setDetailError,
    isCreatingConversationRef,
    onDataChangeRef,
    selectedProjectPathRef,
    userProjectsRef,
    draftByScopeKeyRef,
    submittedDraftSnapshotsRef,
    draftSettingsBySessionIdRef,
    agentActivityRuntime,
    workspaceId,
    activeConversationIdRef,
    isComposerHomeRef,
    activeSessionState,
    conversationServices,
    activation,
    currentUserId,
    data,
    defaultReasoningEffort,
    requestRailReveal,
    setActiveConversationId,
    setIntent,
    setIsComposerHome,
    setIsLoadingMessages,
    conversationListQuery,
    isCurrentConversation,
    isConversationStale
  } = input;
  const startConversation = useCallback(
    (
      initialContentInput?: unknown,
      displayPrompt?: string,
      submitOptions?: AgentComposerSubmitOptions,
      initialTurnExpected?: boolean
    ): AgentGUINewConversationActivationResult | null => {
      const target = selectedAgentTargetRef.current;
      const targetData = selectedComposerTargetDataRef.current;
      if (target.disabled === true) {
        return null;
      }
      const agentTargetId = targetData.agentTargetId ?? "";
      if (
        !agentTargetId ||
        (agentTargetsProvidedRef.current &&
          !selectedAgentTargetIsExplicitRef.current)
      ) {
        setDetailError(translate("agentHost.agentGui.agentTargetRequired"));
        return null;
      }
      const normalizedInitialContent = Array.isArray(initialContentInput)
        ? normalizeAgentPromptContentBlocks(
            initialContentInput as AgentPromptContentBlock[]
          )
        : textPromptContent(normalizeOptionalPrompt(initialContentInput));
      const initialDisplayPrompt =
        displayPrompt && displayPrompt.trim() ? displayPrompt : undefined;
      // bundle 折叠时,标题/回显用 displayPrompt(单 chip),而非展开后的文件列表。
      const normalizedInitialPrompt =
        initialDisplayPrompt ??
        agentPromptContentDisplayText(normalizedInitialContent);
      isCreatingConversationRef.current = true;
      setDetailError(null);
      const provider = targetData.provider;
      onDataChangeRef.current((current) =>
        current.provider === provider &&
        (current.agentTargetId ?? null) === agentTargetId
          ? current
          : {
              ...current,
              provider,
              agentTargetId
            }
      );
      const selectedProjectPath = selectedProjectPathRef.current;
      const railPlacement = resolveInitialRailPlacement({
        selectedProjectPath,
        userProjects: userProjectsRef.current
      });
      if (railPlacement === null) {
        return null;
      }
      const railSectionKey = railPlacement.sectionKey;
      const initialNodeSettings = readNodeDefaultDraftSettings({
        data: targetData.data,
        defaultReasoningEffort,
        drafts: draftSettingsBySessionIdRef.current
      });
      const snapshotComposerOptions = getCachedComposerOptions();
      const requiredSettingsPatch = submitOptions?.requiredSettingsPatch as
        | Partial<AgentSessionComposerSettings>
        | undefined;
      // Sparse Create settings must match the home presentation. Draft fields
      // retired after remembered-default acknowledgement still appear in
      // effectiveSettings / permissionConfig.defaultValue; resolve them here
      // so the first turn does not fall back to the provider default.
      const settings = resolveSparseNewConversationActivationSettings({
        draftSettings: initialNodeSettings,
        composerOptions: snapshotComposerOptions,
        requiredSettingsPatch,
        codexSaverModeEntryEnabled,
        rtkSaverModeEntryEnabled
      });
      const settingProvenance = resolveNewConversationSettingProvenance(
        requiredSettingsPatch
      );
      const prewarmedSessionId =
        normalizedInitialContent.length > 0 &&
        snapshotComposerOptions?.behavior?.prewarmDraftSession === true
          ? draftAgentSessionIdFromComposerOptions(snapshotComposerOptions)
          : null;
      const agentSessionId =
        prewarmedSessionId &&
        activation.stateFor(prewarmedSessionId) === "inactive" &&
        isPendingActivationViable(
          conversationServices.activations.latest(prewarmedSessionId)
        )
          ? prewarmedSessionId
          : createAgentGUIConversationId();
      const submitTrace = createAgentSubmitTraceState({
        agentSessionId,
        content: normalizedInitialContent,
        prompt: normalizedInitialPrompt,
        queued: false,
        startedAtUnixMs: Date.now()
      });
      const sourceScopeKey = resolveAgentComposerDraftScopeKey({});
      const submittedDraft =
        submitOptions?.submittedDraft ??
        draftByScopeKeyRef.current[sourceScopeKey] ??
        emptyAgentComposerDraft();
      submittedDraftSnapshotsRef.current[submitTrace.clientSubmitId] = {
        sourceScopeKey,
        content: snapshotAgentComposerDraft(submittedDraft)
      };
      reportAgentSubmitTraceDiagnostic({
        event: "activation.requested",
        runtime: agentActivityRuntime,
        trace: submitTrace,
        workspaceId,
        fields: { mode: "new" }
      });
      const requestId = activation.activate({
        mode: "new",
        agentSessionId,
        agentTargetId,
        ...(submitOptions?.isolation
          ? { isolation: submitOptions.isolation }
          : {}),
        ...settingProvenance,
        ...(submitOptions?.capabilityRefs?.length
          ? { capabilityRefs: submitOptions.capabilityRefs }
          : {}),
        clientSubmitId: submitTrace.clientSubmitId,
        cwd: selectedProjectPath ?? "",
        railPlacement,
        railSectionKey,
        initialContent: normalizedInitialContent,
        ...(initialTurnExpected !== undefined ? { initialTurnExpected } : {}),
        initialDisplayPrompt,
        runtimeContent: toRuntimeSendContent(normalizedInitialContent),
        submitDiagnostics: agentSubmitTraceDiagnostics(submitTrace),
        settings,
        optimisticTitle: deriveAgentGUIOptimisticConversationTitle(
          normalizedInitialPrompt
        )
      });
      if (requestId === null) return null;
      activeConversationIdRef.current = agentSessionId;
      setActiveConversationId(agentSessionId);
      requestRailReveal(agentSessionId, "created");
      isComposerHomeRef.current = false;
      setIsComposerHome(false);
      setIntent({
        id: agentSessionId,
        source: "activation",
        tag: "active"
      });
      setIsLoadingMessages(false);
      return { agentSessionId, requestId };
    },
    [
      activeSessionState,
      codexSaverModeEntryEnabled,
      rtkSaverModeEntryEnabled,
      currentUserId,
      data,
      defaultReasoningEffort,
      getCachedComposerOptions,
      requestRailReveal,
      activation,
      conversationListQuery,
      isCurrentConversation,
      agentActivityRuntime,
      isConversationStale,
      conversationServices,
      workspaceId
    ]
  );

  return startConversation;
}
