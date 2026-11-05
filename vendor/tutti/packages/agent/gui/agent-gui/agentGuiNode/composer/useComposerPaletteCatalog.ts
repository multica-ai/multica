import { useMemo, type RefObject } from "react";
import type { UiLanguage } from "../../../contexts/settings/domain/agentSettings";
import type { AgentSessionCommand } from "../../../shared/agentSessionTypes";
import type { AgentSlashPaletteEntry } from "../AgentSlashCommandPalette";
import type { AgentRichTextEditorHandle } from "../agentRichText/AgentRichTextEditor";
import type { AgentCapabilityTokenOption } from "../agentRichText/agentCapabilityTokenExtension";
import {
  filterProviderSkillsForTrigger,
  getAgentComposerTriggerQueryMatch,
  getPromptStartSlashCommandQuery
} from "../model/agentComposerTriggerQueries";
import type {
  AgentGUIComposerSettingsVM,
  AgentGUIProviderSkillOption
} from "../model/agentGuiNodeTypes";
import {
  labelForProviderSkill,
  skillDescriptionForDisplay,
  skillTriggerForPrefix
} from "../model/agentSkillOptions";
import {
  resolveSlashCommandsForProvider,
  type AgentSlashCommand,
  type AgentSlashCommandCapability
} from "../model/agentSlashCommandProviderPolicy";
import {
  filterSlashCommands,
  labelForSlashCommand
} from "../model/agentSlashCommands";
import type {
  AgentComposerCapabilityMenuState,
  AgentComposerRenderProps as AgentComposerProps
} from "./AgentComposer.types";
import {
  slashCommandDescriptionForDisplay,
  slashCommandLabelForDisplay
} from "./slashCommandDisplay";

interface UseComposerPaletteCatalogInput {
  provider: string;
  paletteDraftPrompt: string;
  availableCommands: readonly AgentSessionCommand[];
  availableSkills: readonly AgentGUIProviderSkillOption[];
  hasCompactableContext: boolean;
  compactSupported: boolean | null;
  composerSettings: AgentGUIComposerSettingsVM;
  capabilityMenuState?: AgentComposerCapabilityMenuState;
  capabilityControlsReadOnly: boolean;
  labels: AgentComposerProps["labels"];
  uiLanguage: UiLanguage;
  editorHandleRef: RefObject<AgentRichTextEditorHandle | null>;
}

function isSlashCommandCapability(
  command: AgentSlashCommand
): command is AgentSlashCommandCapability {
  return "kind" in command && command.kind === "capability";
}

export function useComposerPaletteCatalog({
  provider,
  paletteDraftPrompt,
  availableCommands,
  availableSkills,
  hasCompactableContext,
  compactSupported,
  composerSettings,
  capabilityMenuState,
  capabilityControlsReadOnly,
  labels,
  uiLanguage,
  editorHandleRef
}: UseComposerPaletteCatalogInput) {
  // Desktop status is refreshed independently from Composer options. Prefer a
  // fresh, authorized host probe when an older Composer snapshot still says
  // the driver is unavailable; launch remains protected by the daemon clamp.
  const computerHostReady =
    capabilityMenuState?.computerUse?.installed === true &&
    capabilityMenuState.computerUse.authorization === "authorized";
  const computerExecutable =
    Boolean(composerSettings.supportsComputerUse) || computerHostReady;
  const computerPresentationSupported =
    computerExecutable ||
    capabilityMenuState?.computerUse?.presentationSupported === true;
  const slashQuery = getPromptStartSlashCommandQuery(paletteDraftPrompt);
  const slashCommandPolicy = composerSettings.slashCommandPolicy;
  const promptBeforeSelection =
    editorHandleRef.current?.getPromptTextBeforeSelection() ?? "";
  const skillQueryDraft = promptBeforeSelection || paletteDraftPrompt;
  const skillQueryMatch = getAgentComposerTriggerQueryMatch(skillQueryDraft);
  const presentationSkills = useMemo(
    () =>
      capabilityMenuState?.connectors?.enabled === false
        ? availableSkills.filter(
            (skill) =>
              skill.sourceKind !== "connector" && skill.kind !== "connector"
          )
        : availableSkills,
    [availableSkills, capabilityMenuState?.connectors?.enabled]
  );
  const resolvedSlashCommands = useMemo(
    () =>
      resolveSlashCommandsForProvider({
        provider,
        policy: slashCommandPolicy,
        commands: availableCommands,
        hasCompactableContext,
        compactSupported,
        planSupported: composerSettings.supportsPlanMode,
        browserSupported: Boolean(composerSettings.supportsBrowser),
        computerSupported: computerPresentationSupported
      }),
    [
      availableCommands,
      compactSupported,
      composerSettings.supportsPlanMode,
      composerSettings.supportsBrowser,
      computerPresentationSupported,
      hasCompactableContext,
      provider,
      slashCommandPolicy
    ]
  );
  const filteredCommands = useMemo(
    () =>
      slashQuery === null
        ? []
        : filterSlashCommands(resolvedSlashCommands, slashQuery),
    [resolvedSlashCommands, slashQuery]
  );
  const filteredSkills = useMemo(
    () =>
      skillQueryMatch === null
        ? []
        : filterProviderSkillsForTrigger({
            skills: presentationSkills,
            query: skillQueryMatch.query,
            triggerPrefix: skillQueryMatch.prefix
          }),
    [presentationSkills, skillQueryMatch]
  );
  const availableCapabilities = useMemo<AgentCapabilityTokenOption[]>(() => {
    if (capabilityControlsReadOnly) {
      return [];
    }
    const entries: AgentCapabilityTokenOption[] = [];
    if (composerSettings.supportsBrowser) {
      entries.push({
        capability: "browserUse",
        label: labels.browserUseCapabilityLabel,
        name: "browser",
        trigger: "/browser"
      });
    }
    if (computerPresentationSupported) {
      entries.push({
        capability: "computerUse",
        label: labels.computerUseCapabilityLabel,
        name: "computer",
        trigger: "/computer"
      });
    }
    return entries;
  }, [
    capabilityControlsReadOnly,
    composerSettings.supportsBrowser,
    computerPresentationSupported,
    labels.browserUseCapabilityLabel,
    labels.computerUseCapabilityLabel
  ]);
  const slashPaletteEntries = useMemo<AgentSlashPaletteEntry[]>(() => {
    const commandEntries: AgentSlashPaletteEntry[] =
      filteredCommands.flatMap<AgentSlashPaletteEntry>((command) => {
        if (isSlashCommandCapability(command)) {
          const browserConnectionMode =
            capabilityMenuState?.browserUse?.connectionMode ?? null;
          const computerUseInstalled =
            capabilityMenuState?.computerUse?.installed ?? null;
          const computerUseAuthorization =
            capabilityMenuState?.computerUse?.authorization ?? null;
          const capLabel =
            command.capability === "computerUse"
              ? labels.computerUseCapabilityLabel
              : labels.browserUseCapabilityLabel;
          const capDescription =
            command.capability === "computerUse"
              ? computerUseInstalled === false
                ? labels.computerUseCapabilitySetupRequiredDescription
                : computerUseAuthorization === "needs-authorization"
                  ? labels.computerUseCapabilityAuthorizationRequiredDescription
                  : computerUseAuthorization === "unknown"
                    ? labels.computerUseCapabilityAuthorizationUnknownDescription
                    : labels.computerUseCapabilityDescription
              : browserConnectionMode === "autoConnect"
                ? labels.browserUseCapabilityDescriptionAutoConnect
                : browserConnectionMode === "isolated"
                  ? labels.browserUseCapabilityDescriptionIsolated
                  : labels.browserUseCapabilityDescription;
          const capSettingsLabel =
            command.capability === "computerUse"
              ? labels.computerUseCapabilitySettingsLabel
              : labels.browserUseCapabilitySettingsLabel;
          const capabilityEntry: AgentSlashPaletteEntry = {
            type: "capability",
            key: `capability:${command.capability}`,
            label: capLabel,
            description: capDescription,
            settingsAriaLabel: capSettingsLabel,
            settingsLabel: labels.capabilityInlineSettingsLabel,
            disabled: capabilityControlsReadOnly,
            selectAction:
              command.capability === "computerUse" &&
              (!computerExecutable ||
                computerUseInstalled === false ||
                (computerUseInstalled === true &&
                  (computerUseAuthorization === "needs-authorization" ||
                    computerUseAuthorization === "unknown")))
                ? "settings"
                : "capability",
            capability: command
          };
          return [capabilityEntry];
        }
        const commandDescription = slashCommandDescriptionForDisplay(
          command,
          labels
        );
        const commandEntry: AgentSlashPaletteEntry = {
          type: "command",
          key: `command:${command.name}`,
          label: labelForSlashCommand(command),
          ...slashCommandLabelForDisplay(command, labels, uiLanguage),
          ...(commandDescription ? { description: commandDescription } : {}),
          command
        };
        return [commandEntry];
      });
    const skillEntries: AgentSlashPaletteEntry[] = filteredSkills.map(
      (skill) => {
        const trigger = skillTriggerForPrefix(skill, skillQueryMatch?.prefix);
        return {
          type: "skill",
          key: `skill:${trigger}`,
          label: labelForProviderSkill(skill, skillQueryMatch?.prefix),
          ...(skillDescriptionForDisplay(skill.description)
            ? { description: skillDescriptionForDisplay(skill.description) }
            : {}),
          skill
        };
      }
    );
    const connectorEntries = skillEntries.filter(
      (entry) =>
        entry.type === "skill" &&
        (entry.skill.sourceKind === "connector" ||
          entry.skill.kind === "connector")
    );
    const nonConnectorSkillEntries = skillEntries.filter(
      (entry) => !connectorEntries.includes(entry)
    );
    return [
      ...commandEntries,
      ...connectorEntries,
      ...nonConnectorSkillEntries
    ];
  }, [
    capabilityMenuState?.browserUse?.connectionMode,
    capabilityMenuState?.computerUse?.authorization,
    capabilityMenuState?.computerUse?.installed,
    computerExecutable,
    capabilityControlsReadOnly,
    filteredCommands,
    filteredSkills,
    labels.browserUseCapabilityDescription,
    labels.browserUseCapabilityDescriptionAutoConnect,
    labels.browserUseCapabilityDescriptionIsolated,
    labels.browserUseCapabilityLabel,
    labels.capabilityInlineSettingsLabel,
    labels.browserUseCapabilitySettingsLabel,
    labels.computerUseCapabilityDescription,
    labels.computerUseCapabilityAuthorizationRequiredDescription,
    labels.computerUseCapabilityAuthorizationUnknownDescription,
    labels.computerUseCapabilitySetupRequiredDescription,
    labels.computerUseCapabilityLabel,
    labels.computerUseCapabilitySettingsLabel,
    labels.slashCommandPresentation,
    labels.slashCommandCompactLabel,
    labels.slashCommandContextLabel,
    labels.slashCommandFastLabel,

    labels.slashCommandInitLabel,
    labels.slashCommandPlanLabel,
    labels.slashCommandReviewLabel,
    labels.slashCommandStatusLabel,
    labels.slashCommandUsageLabel,
    labels.slashCommandCompactDescription,
    labels.slashCommandContextDescription,
    labels.slashCommandFastDescription,

    labels.slashCommandInitDescription,
    labels.slashCommandPlanDescription,
    labels.slashCommandReviewDescription,
    labels.slashCommandStatusDescription,
    labels.slashCommandUsageDescription,
    uiLanguage,
    skillQueryMatch?.prefix
  ]);
  return {
    availableCapabilities,
    computerExecutable,
    filteredSkills,
    resolvedSlashCommands,
    skillQueryMatch,
    slashPaletteEntries,
    slashQuery,
    slashCommandPolicy,
    promptBeforeSelection
  };
}
