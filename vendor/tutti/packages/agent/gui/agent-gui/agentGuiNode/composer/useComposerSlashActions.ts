import {
  useCallback,
  useMemo,
  useRef,
  type Dispatch,
  type FormEvent,
  type RefObject,
  type SetStateAction
} from "react";
import { useOptionalConversationServices } from "../../../conversationServices";
import { useOptionalAgentHostApi } from "../../../agentActivityHost";
import type { AgentSessionCommand } from "../../../shared/agentSessionTypes";
import type { AgentSlashPaletteEntry } from "../AgentSlashCommandPalette";
import type { AgentRichTextEditorHandle } from "../agentRichText/AgentRichTextEditor";
import { agentComposerFileMentionReferences } from "../agentRichText/agentMentionMarkdown";
import {
  agentComposerDraftHasContent,
  agentComposerDraftPreservingConnectors,
  projectAgentComposerDraftSubmission,
  textPromptContent,
  updateAgentComposerDraft
} from "../model/agentComposerDraft";
import {
  draftForProviderSkillTrigger,
  getAgentComposerTriggerQueryMatch
} from "../model/agentComposerTriggerQueries";
import type {
  AgentComposerDraft,
  AgentComposerDraftFile,
  AgentComposerDraftImage,
  AgentComposerDraftLargeText,
  AgentGUIProviderSkillOption
} from "../model/agentGuiNodeTypes";
import { skillTriggerForPrefix } from "../model/agentSkillOptions";
import type {
  AgentSlashCommand,
  AgentSlashCommandCapability,
  SlashCommandSelectionEffect
} from "../model/agentSlashCommandProviderPolicy";
import {
  resolveSlashCommandSelectionEffect,
  resolveSlashCommandSubmitEffect
} from "../model/agentSlashCommandProviderPolicy";
import { moveSlashCommandHighlight, parseSlashCommandInvocation } from "../model/agentSlashCommands";
import { resolvePermissionModeControlsDisabled } from "../model/composerModeSelection";
import type { AgentComposerRenderProps as AgentComposerProps } from "./AgentComposer.types";
import { reportAgentComposerDiagnostic } from "./agentComposerDiagnostics";

type TriggerMatch = ReturnType<typeof getAgentComposerTriggerQueryMatch>;

type Props = Pick<
  AgentComposerProps,
  | "workspaceId"
  | "provider"
  | "isSendingTurn"
  | "isSubmittingPrompt"
  | "showStopButton"
  | "promptImagesSupported"
  | "availableSkills"
  | "composerSettings"
  | "capabilityControlsReadOnly"
  | "onDraftContentChange"
  | "onSettingsChange"
  | "onSubmit"
  | "onSubmitEmpty"
  | "onSubmitGuidance"
  | "onCapabilitySettingsRequest"
  | "onSlashStatusOpen"
  | "onSlashStatusClose"
  | "onPromptImagesUnsupported"
  | "onRequestGitBranches"
> & {
  disabled: boolean;
  submitDisabled: boolean;
  canQueueWhileBusy: boolean;
};

interface UseComposerSlashActionsInput extends Props {
  computerExecutable: boolean;
  draftContent: AgentComposerDraft;
  selectedProjectPath: string;
  slashStatusAgentSessionId: string | null;
  isSlashStatusPanelOpen: boolean;
  slashCommandPolicy: AgentComposerProps["composerSettings"]["slashCommandPolicy"];
  skillQueryMatch: TriggerMatch;
  promptBeforeSelection: string;
  resolvedSlashCommands: readonly AgentSlashCommand[];
  slashPaletteEntries: readonly AgentSlashPaletteEntry[];
  activeHighlight: number;
  showSlashPalette: boolean;
  showCommandMenuPanel: boolean;
  isSelectedProjectMissing: boolean;
  editorHandleRef: RefObject<AgentRichTextEditorHandle | null>;
  draftPromptRef: RefObject<string>;
  draftImagesRef: RefObject<AgentComposerDraftImage[]>;
  draftFilesRef: RefObject<AgentComposerDraftFile[]>;
  draftLargeTextsRef: RefObject<AgentComposerDraftLargeText[]>;
  setPaletteDraftPrompt: Dispatch<SetStateAction<string>>;
  setIsPaletteOpen: Dispatch<SetStateAction<boolean>>;
  setIsReviewPickerOpen: Dispatch<SetStateAction<boolean>>;
  setIsSlashStatusPanelOpen: Dispatch<SetStateAction<boolean>>;
  setHighlightedIndex: Dispatch<SetStateAction<number>>;
}

function useStableEventCallback<Args extends unknown[], Result>(
  callback: (...args: Args) => Result
): (...args: Args) => Result {
  const callbackRef = useRef(callback);
  callbackRef.current = callback;
  return useCallback((...args: Args) => callbackRef.current(...args), []);
}

export function useComposerSlashActions(input: UseComposerSlashActionsInput) {
  const hostApi = useOptionalAgentHostApi();
  const agentActivityRuntime = useOptionalConversationServices();
  const {
    workspaceId,
    provider,
    disabled,
    submitDisabled,
    canQueueWhileBusy,
    isSendingTurn,
    isSubmittingPrompt,
    showStopButton,
    promptImagesSupported,
    availableSkills = [],
    composerSettings,
    computerExecutable,
    capabilityControlsReadOnly = false,
    onDraftContentChange,
    onSettingsChange,
    onSubmit,
    onSubmitEmpty,
    onSubmitGuidance,
    onCapabilitySettingsRequest,
    onSlashStatusOpen,
    onSlashStatusClose,
    onPromptImagesUnsupported,
    onRequestGitBranches,
    draftContent,
    selectedProjectPath,
    slashStatusAgentSessionId,
    isSlashStatusPanelOpen,
    slashCommandPolicy,
    skillQueryMatch,
    promptBeforeSelection,
    resolvedSlashCommands,
    slashPaletteEntries,
    activeHighlight,
    showSlashPalette,
    showCommandMenuPanel,
    isSelectedProjectMissing,
    editorHandleRef,
    draftPromptRef,
    draftImagesRef,
    draftFilesRef,
    draftLargeTextsRef,
    setPaletteDraftPrompt,
    setIsPaletteOpen,
    setIsReviewPickerOpen,
    setIsSlashStatusPanelOpen,
    setHighlightedIndex
  } = input;
  const clearSlashCommandDraft = useCallback((): void => {
    draftPromptRef.current = "";
    setPaletteDraftPrompt("");
    setIsPaletteOpen(false);
    onDraftContentChange(agentComposerDraftPreservingConnectors(draftContent));
  }, [draftContent, onDraftContentChange]);

  const closeSlashStatusPanel = useCallback((): void => {
    setIsSlashStatusPanelOpen(false);
    onSlashStatusClose?.();
  }, [onSlashStatusClose, setIsSlashStatusPanelOpen]);

  const settingsControlsDisabled =
    isSendingTurn || isSubmittingPrompt || showStopButton;
  const permissionModeControlsDisabled = resolvePermissionModeControlsDisabled({
    isSendingTurn,
    isSubmittingPrompt,
    showStopButton
  });
  const composerControlsHardDisabled =
    isSelectedProjectMissing ||
    isSubmittingPrompt ||
    (disabled && !isSendingTurn && !showStopButton);

  const closeReviewPicker = useCallback((): void => {
    setIsReviewPickerOpen(false);
  }, []);

  const closeSlashFloatingMenu = useCallback((): void => {
    if (isSlashStatusPanelOpen) {
      onSlashStatusClose?.();
    }
    setIsSlashStatusPanelOpen(false);
    setIsReviewPickerOpen(false);
    setIsPaletteOpen(false);
  }, [
    isSlashStatusPanelOpen,
    onSlashStatusClose,
    setIsPaletteOpen,
    setIsReviewPickerOpen,
    setIsSlashStatusPanelOpen
  ]);

  const submitReviewCommand = useCallback(
    (command: string): void => {
      setIsReviewPickerOpen(false);
      clearSlashCommandDraft();
      onSubmit(textPromptContent(command));
    },
    [clearSlashCommandDraft, onSubmit]
  );

  // Bind the branch loader to this composer's session so the picker can fetch
  // branches without the caller having to know the active agent session id.
  const reviewBranchLoader = useMemo(() => {
    if (!onRequestGitBranches) {
      return null;
    }
    // Prefer the live agent session (its daemon-resolved cwd); fall back to the
    // selected project path so the review picker still lists branches in the
    // empty-hero composer before any session exists.
    if (slashStatusAgentSessionId) {
      return () =>
        onRequestGitBranches({ agentSessionId: slashStatusAgentSessionId });
    }
    if (selectedProjectPath) {
      return () =>
        onRequestGitBranches({ workingDirectory: selectedProjectPath });
    }
    return null;
  }, [onRequestGitBranches, selectedProjectPath, slashStatusAgentSessionId]);

  const executeSlashCommandEffect = useCallback(
    (effect: SlashCommandSelectionEffect): void => {
      if (effect.kind === "submitPrompt") {
        clearSlashCommandDraft();
        const submitOptions = effect.requiredSettingsPatch
          ? { requiredSettingsPatch: effect.requiredSettingsPatch }
          : undefined;
        if (effect.displayPrompt) {
          onSubmit(
            textPromptContent(effect.prompt),
            effect.displayPrompt,
            submitOptions
          );
        } else {
          onSubmit(textPromptContent(effect.prompt), undefined, submitOptions);
        }
        return;
      }
      if (effect.kind === "showStatus") {
        clearSlashCommandDraft();
        setIsReviewPickerOpen(false);
        if (!isSlashStatusPanelOpen) {
          onSlashStatusOpen?.();
        } else {
          onSlashStatusClose?.();
        }
        setIsSlashStatusPanelOpen((current) => !current);
        return;
      }
      if (effect.kind === "showReviewPicker") {
        clearSlashCommandDraft();
        if (isSlashStatusPanelOpen) {
          onSlashStatusClose?.();
          setIsSlashStatusPanelOpen(false);
        }
        setIsReviewPickerOpen(true);
        return;
      }
      if (effect.kind === "enablePlanMode") {
        clearSlashCommandDraft();
        if (!settingsControlsDisabled) {
          onSettingsChange({ planMode: true });
        }
        return;
      }
      if (effect.kind === "enableBrowserUse") {
        const nextDraft = effect.draft;
        draftPromptRef.current = nextDraft;
        setPaletteDraftPrompt(nextDraft);
        onDraftContentChange(
          updateAgentComposerDraft(draftContent, { prompt: nextDraft })
        );
        setIsPaletteOpen(false);
        if (!settingsControlsDisabled) {
          onSettingsChange({ browserUse: true });
        }
        return;
      }
      if (effect.kind === "enableComputerUse") {
        const nextDraft = effect.draft;
        draftPromptRef.current = nextDraft;
        setPaletteDraftPrompt(nextDraft);
        onDraftContentChange(
          updateAgentComposerDraft(draftContent, { prompt: nextDraft })
        );
        setIsPaletteOpen(false);
        if (!settingsControlsDisabled) {
          onSettingsChange({ computerUse: true });
        }
        return;
      }
      if (effect.kind === "toggleSpeed") {
        clearSlashCommandDraft();
        if (composerSettings.supportsSpeed) {
          const currentSpeed =
            composerSettings.selectedSpeedValue ??
            composerSettings.draftSettings.speed ??
            "standard";
          onSettingsChange({
            speed: currentSpeed === "fast" ? "standard" : "fast"
          });
        }
        return;
      }
      const nextDraft = effect.draft;
      draftPromptRef.current = nextDraft;
      setPaletteDraftPrompt(nextDraft);
      onDraftContentChange(
        updateAgentComposerDraft(draftContent, { prompt: nextDraft })
      );
      setIsPaletteOpen(false);
    },
    [
      clearSlashCommandDraft,
      composerSettings.draftSettings.planMode,
      composerSettings.draftSettings.speed,
      composerSettings.selectedSpeedValue,
      composerSettings.supportsSpeed,
      draftContent,
      isSlashStatusPanelOpen,
      onDraftContentChange,
      onSlashStatusClose,
      onSlashStatusOpen,
      onSettingsChange,
      onSubmit,
      settingsControlsDisabled
    ]
  );

  const selectCommand = useCallback(
    (command: AgentSessionCommand): void => {
      if (hostApi?.composerCommands?.commands.some((entry) => entry.name === command.name)) {
        draftPromptRef.current = "";
        setPaletteDraftPrompt("");
        setIsPaletteOpen(false);
        onDraftContentChange(
          updateAgentComposerDraft(draftContent, { prompt: "" })
        );
        void Promise.resolve().then(() => hostApi.composerCommands!.execute({
          name: command.name,
          agentSessionId: slashStatusAgentSessionId
        })).catch((error: unknown) => hostApi.toast?.error?.(
          error instanceof Error ? error.message : String(error)
        ));
        return;
      }
      const selectionEffect = resolveSlashCommandSelectionEffect({
        provider,
        policy: slashCommandPolicy,
        command,
        currentDraft: draftPromptRef.current
      });
      if (selectionEffect) {
        executeSlashCommandEffect(selectionEffect);
      }
    },
    [
      draftContent,
      onDraftContentChange,
      hostApi,
      slashStatusAgentSessionId,
      executeSlashCommandEffect,
      provider,
      slashCommandPolicy
    ]
  );

  const selectCapability = useCallback(
    (capability: AgentSlashCommandCapability): void => {
      if (capabilityControlsReadOnly) {
        return;
      }
      const selectionEffect = resolveSlashCommandSelectionEffect({
        provider,
        policy: slashCommandPolicy,
        command: capability,
        currentDraft: draftPromptRef.current
      });
      if (selectionEffect) {
        executeSlashCommandEffect(selectionEffect);
      }
    },
    [
      capabilityControlsReadOnly,
      executeSlashCommandEffect,
      provider,
      slashCommandPolicy
    ]
  );

  const selectCapabilitySettings = useCallback(
    (capability: AgentSlashCommandCapability): void => {
      if (capabilityControlsReadOnly) {
        return;
      }
      onCapabilitySettingsRequest?.(capability.capability);
      setIsPaletteOpen(false);
    },
    [capabilityControlsReadOnly, onCapabilitySettingsRequest]
  );

  const selectSkill = useCallback(
    (skill: AgentGUIProviderSkillOption): void => {
      const isConnector =
        skill.sourceKind === "connector" || skill.kind === "connector";
      if (isConnector && skill.status !== "available") {
        if (skill.status !== "unsupported" && skill.connectorKey) {
          onCapabilitySettingsRequest?.({
            kind: "connector",
            connectorKey: skill.connectorKey
          });
        }
        setIsPaletteOpen(false);
        return;
      }
      const trigger = skillTriggerForPrefix(skill, skillQueryMatch?.prefix);
      const replacedDraft =
        trigger && skillQueryMatch && promptBeforeSelection !== ""
          ? editorHandleRef.current?.replaceTextBeforeSelection(
              skillQueryMatch.end - skillQueryMatch.start,
              `${trigger} `
            )
          : null;
      const nextDraft =
        replacedDraft ??
        draftForProviderSkillTrigger({
          skill,
          currentDraft: draftPromptRef.current,
          match: skillQueryMatch
        });
      draftPromptRef.current = nextDraft;
      setPaletteDraftPrompt(nextDraft);
      onDraftContentChange(
        updateAgentComposerDraft(draftContent, { prompt: nextDraft })
      );
      setIsPaletteOpen(false);
    },
    [
      draftContent,
      onCapabilitySettingsRequest,
      onDraftContentChange,
      promptBeforeSelection,
      skillQueryMatch
    ]
  );

  const submitCurrentPrompt = useStableEventCallback(
    (options?: { guidance?: boolean }): void => {
      const canSubmitWhileSending = canQueueWhileBusy && isSendingTurn;
      const currentDraftImages = draftImagesRef.current;
      const currentDraftFiles = draftFilesRef.current;
      const currentDraftLargeTexts = draftLargeTextsRef.current;
      const hasUploadingImages = currentDraftImages.some(
        (image) => image.uploading
      );
      const hasFailedImages = currentDraftImages.some(
        (image) => image.uploadError
      );
      const hasUploadingFiles = currentDraftFiles.some(
        (file) => file.uploading
      );
      const hasFailedFiles = currentDraftFiles.some((file) => file.uploadError);
      const hasUploadingLargeTexts = currentDraftLargeTexts.some(
        (item) => item.uploading
      );
      const hasFailedLargeTexts = currentDraftLargeTexts.some(
        (item) => item.uploadError
      );
      if (
        isSelectedProjectMissing ||
        submitDisabled ||
        hasUploadingImages ||
        hasFailedImages ||
        hasUploadingFiles ||
        hasFailedFiles ||
        hasUploadingLargeTexts ||
        hasFailedLargeTexts ||
        (disabled && !canQueueWhileBusy) ||
        (isSendingTurn && !canSubmitWhileSending)
      ) {
        return;
      }
      const nextPrompt = draftPromptRef.current;
      const nextDraftContent = updateAgentComposerDraft(draftContent, {
        prompt: nextPrompt,
        images: currentDraftImages,
        files: currentDraftFiles,
        largeTexts: currentDraftLargeTexts
      });
      if (!agentComposerDraftHasContent(nextDraftContent)) {
        // Empty-send override (e.g. plan review accept): only for a plain
        // send, never for guidance, and only after the guards above agreed
        // a submit is currently allowed at all.
        if (options?.guidance !== true) {
          onSubmitEmpty?.();
        }
        return;
      }
      if (currentDraftImages.length > 0 && !promptImagesSupported) {
        onPromptImagesUnsupported?.();
        return;
      }
      if (options?.guidance !== true) {
        const invocation = parseSlashCommandInvocation(nextPrompt);
        const hostCommand = invocation?.args.trim() === ""
          ? hostApi?.composerCommands?.commands.find((entry) => entry.name === invocation.commandName)
          : undefined;
        if (hostCommand) {
          selectCommand(hostCommand);
          return;
        }
        const slashCommandEffect = resolveSlashCommandSubmitEffect({
          browserSupported: Boolean(composerSettings.supportsBrowser),
          computerSupported: computerExecutable,
          commands: resolvedSlashCommands,
          draft: nextPrompt,
          provider,
          policy: slashCommandPolicy
        });
        if (slashCommandEffect) {
          if (
            capabilityControlsReadOnly &&
            slashCommandEffect.kind === "submitPrompt" &&
            (slashCommandEffect.requiredSettingsPatch?.browserUse !==
              undefined ||
              slashCommandEffect.requiredSettingsPatch?.computerUse !==
                undefined)
          ) {
            return;
          }
          executeSlashCommandEffect(slashCommandEffect);
          return;
        }
      }
      setIsPaletteOpen(false);
      // workspace-reference 保持为单条 mention，由 skill+CLI 按需解析。
      const submission = projectAgentComposerDraftSubmission({
        draft: nextDraftContent,
        skills: availableSkills
      });
      const submitOptions = { submittedDraft: nextDraftContent };
      const fileReferences = agentComposerFileMentionReferences(nextPrompt);
      const draftFileIds = new Set(currentDraftFiles.map((file) => file.id));
      reportAgentComposerDiagnostic(agentActivityRuntime, {
        details: {
          contentBlockCount: submission.content.length,
          contentFileBlockCount: submission.content.filter(
            (block) => block.type === "file" && block.kind === "file"
          ).length,
          contentTypes: submission.content.map((block) => block.type),
          draftFileCount: currentDraftFiles.length,
          failedDraftFileCount: currentDraftFiles.filter((file) =>
            Boolean(file.uploadError)
          ).length,
          fileMentionCount: fileReferences.length,
          missingDraftFileCount: fileReferences.filter(
            (reference) => !draftFileIds.has(reference.id)
          ).length,
          uploadingDraftFileCount: currentDraftFiles.filter(
            (file) => file.uploading
          ).length
        },
        event: "agent.gui.composer.file_preparation.submission_projection",
        level: "info",
        source: "agent-gui",
        workspaceId
      });
      if (options?.guidance === true) {
        if (!onSubmitGuidance) {
          return;
        }
        if (submission.displayPrompt) {
          onSubmitGuidance(
            submission.content,
            submission.displayPrompt,
            submitOptions
          );
        } else {
          onSubmitGuidance(submission.content, undefined, submitOptions);
        }
      } else {
        if (submission.displayPrompt) {
          onSubmit(submission.content, submission.displayPrompt, submitOptions);
        } else {
          onSubmit(submission.content, undefined, submitOptions);
        }
      }
      // The controller owns draft clearing: an in-session send clears the
      // composer optimistically at hand-off, and a rejected send restores this
      // exact content, so a later edit is never overwritten by an in-flight
      // submission.
    }
  );

  const submit = useCallback(
    (event: FormEvent<HTMLFormElement>): void => {
      event.preventDefault();
      submitCurrentPrompt();
    },
    [submitCurrentPrompt]
  );

  const handleSlashPaletteKeyDown = useStableEventCallback(
    (event: KeyboardEvent): boolean => {
      if (!showSlashPalette) {
        return false;
      }
      if (event.key === "ArrowDown") {
        event.preventDefault();
        setHighlightedIndex((current) =>
          moveSlashCommandHighlight(current, slashPaletteEntries.length, 1)
        );
        return true;
      }
      if (event.key === "ArrowUp") {
        event.preventDefault();
        setHighlightedIndex((current) =>
          moveSlashCommandHighlight(current, slashPaletteEntries.length, -1)
        );
        return true;
      }
      if (event.key === "Escape") {
        event.preventDefault();
        setIsPaletteOpen(false);
        return true;
      }
      if (event.key === "Tab" || event.key === "Enter") {
        event.preventDefault();
        const activeEntry = slashPaletteEntries[activeHighlight];
        if (activeEntry?.type === "capability" && activeEntry.disabled) {
          return true;
        }
        if (activeEntry?.type === "command") {
          selectCommand(activeEntry.command);
        } else if (activeEntry?.type === "capability") {
          if (activeEntry.selectAction === "settings") {
            selectCapabilitySettings(activeEntry.capability);
          } else {
            selectCapability(activeEntry.capability);
          }
        } else if (activeEntry?.type === "skill") {
          selectSkill(activeEntry.skill);
        }
        return true;
      }
      return false;
    }
  );

  const handleSlashCommandMenuKeyDown = useStableEventCallback(
    (event: KeyboardEvent): boolean => {
      if (!showCommandMenuPanel || event.key !== "Escape") {
        return false;
      }
      event.preventDefault();
      closeSlashFloatingMenu();
      return true;
    }
  );

  return {
    clearSlashCommandDraft,
    closeReviewPicker,
    closeSlashFloatingMenu,
    closeSlashStatusPanel,
    composerControlsHardDisabled,
    executeSlashCommandEffect,
    handleSlashCommandMenuKeyDown,
    handleSlashPaletteKeyDown,
    permissionModeControlsDisabled,
    reviewBranchLoader,
    selectCapability,
    selectCapabilitySettings,
    selectCommand,
    selectSkill,
    settingsControlsDisabled,
    submit,
    submitCurrentPrompt,
    submitReviewCommand
  };
}
