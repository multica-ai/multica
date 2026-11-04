import type { AgentSessionCommand } from "../../../shared/agentSessionTypes";
import type {
  AgentActivityComposerOptions,
  AgentActivitySubmitSettingsPatch,
  AgentActivitySlashCommandPolicy
} from "@tutti-os/agent-activity-core";
import {
  agentCapabilityUseDisplayPrompt,
  buildAgentCapabilityUseSubmitPrompt,
  parseAgentCapabilityUseInvocation
} from "./agentCapabilityUseSubmit";
import type { AgentCapabilityUse } from "./agentCapabilityUseSubmit";
import {
  draftForSlashCommand,
  mergeSlashCommands,
  parseSlashCommandInvocation,
  promptForSlashCommand
} from "./agentSlashCommands";

export type AgentSlashCommandProvider = string;

export interface AgentSlashCommandCapability {
  aliases?: readonly string[];
  capability: AgentCapabilityUse;
  kind: "capability";
  name: string;
}

export type AgentSlashCommand =
  | AgentSessionCommand
  | AgentSlashCommandCapability;

export type AgentSlashCommandPolicy = AgentActivitySlashCommandPolicy;

export type SlashCommandSelectionEffect =
  | {
      kind: "fillDraft";
      draft: string;
    }
  | {
      kind: "submitPrompt";
      prompt: string;
      displayPrompt?: string;
      requiredSettingsPatch?: AgentActivitySubmitSettingsPatch;
    }
  | {
      kind: "showStatus";
    }
  | {
      kind: "enablePlanMode";
    }
  | {
      kind: "enableBrowserUse";
      draft: string;
    }
  | {
      kind: "enableComputerUse";
      draft: string;
    }
  | {
      kind: "showReviewPicker";
    }
  | {
      kind: "toggleSpeed";
    };

interface ResolveSlashCommandSelectionEffectInput {
  provider: AgentSlashCommandProvider;
  policy?: AgentSlashCommandPolicy | null;
  command: AgentSlashCommand;
  currentDraft: string;
}

interface ResolveSlashCommandSubmitEffectInput {
  browserSupported?: boolean;
  computerSupported?: boolean;
  provider: AgentSlashCommandProvider;
  policy?: AgentSlashCommandPolicy | null;
  commands: readonly AgentSlashCommand[];
  draft: string;
}

interface ProviderSlashPolicy {
  /** Commands submitted immediately on selection instead of filling a draft. */
  immediateCommands: ReadonlySet<string>;
  /** Commands that open the shared review target picker when invoked bare. */
  reviewPickerCommands: ReadonlySet<string>;
  /** Commands surfaced when the agent advertises none of its own. */
  fallbackCommands: readonly AgentSessionCommand[];
  localPlanCommands: ReadonlySet<string>;
  localStatusCommands: ReadonlySet<string>;
  localSpeedCommands: ReadonlySet<string>;
}

// `/plan` enables plan mode locally (a negotiated capability) rather than
// reaching the agent as a prompt; removing its badge disables it again.
// `/fast` toggles the descriptor-advertised speed dimension locally.
const BROWSER_USE_CAPABILITY_COMMAND: AgentSlashCommandCapability = {
  kind: "capability",
  capability: "browserUse",
  name: "browser",
  aliases: ["浏览器"]
};
const COMPUTER_USE_CAPABILITY_COMMAND: AgentSlashCommandCapability = {
  kind: "capability",
  capability: "computerUse",
  name: "computer",
  aliases: ["电脑"]
};
const PLAN_MODE_COMMAND: AgentSessionCommand = { name: "plan" };

function providerSlashPolicy(
  policy?: AgentSlashCommandPolicy | null
): ProviderSlashPolicy | undefined {
  if (policy) {
    const commandEffects = Array.isArray(policy.commandEffects)
      ? policy.commandEffects
      : [];
    const fallbackCommands = Array.isArray(policy.fallbackCommands)
      ? policy.fallbackCommands
      : [];
    const commandsForEffect = (
      effect: AgentSlashCommandPolicy["commandEffects"][number]["effect"]
    ) =>
      normalizedCommandSet(
        commandEffects.flatMap((entry) =>
          entry.effect === effect ? [entry.command] : []
        )
      );
    return {
      fallbackCommands: fallbackCommands.map((name) => ({ name })),
      immediateCommands: commandsForEffect("submitImmediate"),
      reviewPickerCommands: commandsForEffect("showReviewPicker"),
      localPlanCommands: commandsForEffect("togglePlanMode"),
      localStatusCommands: commandsForEffect("showStatus"),
      localSpeedCommands: commandsForEffect("toggleSpeed")
    };
  }
  return undefined;
}

export function resolveSlashCommandsForProvider({
  provider: _provider,
  policy,
  commands,
  hasCompactableContext = true,
  compactSupported,
  planSupported = false,
  browserSupported = false,
  computerSupported = false
}: {
  provider: AgentSlashCommandProvider;
  policy?: AgentSlashCommandPolicy | null;
  commands: readonly AgentSessionCommand[];
  hasCompactableContext?: boolean;
  /**
   * Negotiated `compact` capability. `false` drops the command entirely
   * (including provider fallbacks); `undefined`/`null` means unknown and
   * keeps the legacy `hasCompactableContext` behavior.
   */
  compactSupported?: boolean | null;
  planSupported?: boolean;
  browserSupported?: boolean;
  computerSupported?: boolean;
}): AgentSlashCommand[] {
  // Provider-advertised commands are runtime capabilities. Open extension
  // providers do not have a built-in Tutti policy descriptor, but their ACP
  // command catalog is still authoritative and must remain visible.
  const descriptorCommands = commands;
  const mergedEntries = mergeSlashCommands(
    filterUnavailableSlashCommands(descriptorCommands, {
      compactSupported,
      hasCompactableContext
    }),
    filterUnavailableSlashCommands(fallbackCommandsForPolicy(policy), {
      compactSupported,
      hasCompactableContext
    })
  );
  const localPlanAvailable =
    planSupported &&
    (providerSlashPolicy(policy)?.localPlanCommands.has("plan") ?? false);
  // `/plan` is a local modifier, not an agent prompt. Drop provider entries
  // that would shadow its reserved exact name, then re-surface the local entry
  // when the corresponding host support exists.
  const commandEntries = mergedEntries.filter((entry) => {
    const commandName = normalizedCommandName(entry);
    return (
      !isLocalTogglePlanCommand(commandName, policy) &&
      (!localPlanAvailable || !slashCommandMatchesInvocation(entry, "plan")) &&
      isSlashPaletteCommandVisible(commandName, policy)
    );
  });
  const planEntries = localPlanAvailable ? [PLAN_MODE_COMMAND] : [];
  const capabilityEntries: AgentSlashCommandCapability[] = [];
  if (browserSupported) {
    capabilityEntries.push(BROWSER_USE_CAPABILITY_COMMAND);
  }
  if (computerSupported) {
    capabilityEntries.push(COMPUTER_USE_CAPABILITY_COMMAND);
  }
  return [...commandEntries, ...planEntries, ...capabilityEntries];
}

export function resolveSlashCommandSelectionEffect({
  provider: _provider,
  policy,
  command,
  currentDraft
}: ResolveSlashCommandSelectionEffectInput): SlashCommandSelectionEffect | null {
  if (isBrowserUseCapability(command)) {
    return {
      kind: "enableBrowserUse",
      draft: draftForSlashCommand(command, currentDraft)
    };
  }
  if (isComputerUseCapability(command)) {
    return {
      kind: "enableComputerUse",
      draft: draftForSlashCommand(command, currentDraft)
    };
  }
  if (!policy) {
    return {
      kind: "fillDraft",
      draft: draftForSlashCommand(command, currentDraft)
    };
  }
  const commandName = normalizedCommandName(command);
  if (isLocalTogglePlanCommand(commandName, policy)) {
    return { kind: "enablePlanMode" };
  }
  if (isLocalToggleSpeedCommand(commandName, policy)) {
    return { kind: "toggleSpeed" };
  }
  if (isLocalStatusCommand(commandName, policy)) {
    return { kind: "showStatus" };
  }
  // A descriptor may route a bare palette command to a local picker.
  if (isReviewPickerCommand(commandName, policy)) {
    return { kind: "showReviewPicker" };
  }
  if (isImmediateCommand(commandName, policy)) {
    return {
      kind: "submitPrompt",
      prompt: promptForSlashCommand(command)
    };
  }
  return {
    kind: "fillDraft",
    draft: draftForSlashCommand(command, currentDraft)
  };
}

export function resolveSlashCommandSubmitEffect({
  browserSupported = false,
  computerSupported = false,
  provider: _provider,
  policy,
  commands,
  draft
}: ResolveSlashCommandSubmitEffectInput): SlashCommandSelectionEffect | null {
  for (const [capability, supported] of [
    ["browserUse", browserSupported],
    ["computerUse", computerSupported]
  ] as const) {
    const capabilityEffect = resolveCapabilitySubmitEffect({
      capability,
      commands,
      draft,
      supported
    });
    if (capabilityEffect) {
      return capabilityEffect;
    }
  }
  const invocation = parseSlashCommandInvocation(draft);
  if (!invocation) {
    return null;
  }
  const command = commands.find((candidate) =>
    slashCommandMatchesInvocation(candidate, invocation.commandName)
  );
  if (!command) {
    return null;
  }
  if (isAgentSlashCommandCapability(command)) {
    return null;
  }
  const commandName = normalizedCommandName(command);
  if (isLocalTogglePlanCommand(commandName, policy)) {
    return invocation.args.trim() === "" ? { kind: "enablePlanMode" } : null;
  }
  if (isLocalToggleSpeedCommand(commandName, policy)) {
    return { kind: "toggleSpeed" };
  }
  if (isLocalStatusCommand(commandName, policy)) {
    return { kind: "showStatus" };
  }
  // Bare `/review` opens the target picker; `/review <text>` keeps the legacy
  // behavior of submitting the text straight through as a custom review.
  if (isReviewPickerCommand(commandName, policy)) {
    return invocation.args.trim() === "" ? { kind: "showReviewPicker" } : null;
  }
  if (isImmediateCommand(commandName, policy)) {
    return {
      kind: "submitPrompt",
      prompt: invocation.normalizedPrompt
    };
  }
  return null;
}

function resolveCapabilitySubmitEffect({
  capability,
  commands,
  draft,
  supported
}: {
  capability: AgentCapabilityUse;
  commands: readonly AgentSlashCommand[];
  draft: string;
  supported: boolean;
}): SlashCommandSelectionEffect | null {
  if (!supported) {
    return null;
  }
  const invocation = parseAgentCapabilityUseInvocation(draft, capability);
  if (!invocation) {
    return null;
  }
  const command = commands.find((candidate) =>
    slashCommandMatchesInvocation(candidate, invocation.commandName)
  );
  if (!command || !isCapabilityCommand(command, capability)) {
    return null;
  }
  return {
    kind: "submitPrompt",
    prompt: buildAgentCapabilityUseSubmitPrompt(capability, invocation.args),
    displayPrompt: agentCapabilityUseDisplayPrompt(capability, invocation.args),
    requiredSettingsPatch: { [capability]: true }
  };
}

function isLocalTogglePlanCommand(
  commandName: string,
  policy?: AgentSlashCommandPolicy | null
): boolean {
  return (
    providerSlashPolicy(policy)?.localPlanCommands.has(
      commandName.trim().toLowerCase()
    ) ?? false
  );
}

function isReviewPickerCommand(
  commandName: string,
  policy?: AgentSlashCommandPolicy | null
): boolean {
  return (
    providerSlashPolicy(policy)?.reviewPickerCommands.has(commandName) ?? false
  );
}

function fallbackCommandsForPolicy(
  policy?: AgentSlashCommandPolicy | null
): readonly AgentSessionCommand[] {
  return providerSlashPolicy(policy)?.fallbackCommands ?? [];
}

function isSlashPaletteCommandVisible(
  commandName: string,
  policy?: AgentSlashCommandPolicy | null
): boolean {
  // AgentGUI injects /side only when the host provides the provider-neutral
  // Side runtime. It is a host command and therefore is not expected to be in
  // a provider's authoritative command catalog.
  if (commandName === "side") {
    return true;
  }
  if (policy?.commandCatalogAuthoritative === true) {
    const fallbackCommands = Array.isArray(policy.fallbackCommands)
      ? policy.fallbackCommands
      : [];
    const commandEffects = Array.isArray(policy.commandEffects)
      ? policy.commandEffects
      : [];
    return (
      fallbackCommands.some(
        (command) => command.trim().toLowerCase() === commandName
      ) ||
      commandEffects.some(
        (descriptor) => descriptor.command.trim().toLowerCase() === commandName
      )
    );
  }
  return true;
}

function isLocalStatusCommand(
  commandName: string,
  policy?: AgentSlashCommandPolicy | null
): boolean {
  return (
    providerSlashPolicy(policy)?.localStatusCommands.has(commandName) ?? false
  );
}

function isLocalToggleSpeedCommand(
  commandName: string,
  policy?: AgentSlashCommandPolicy | null
): boolean {
  return (
    providerSlashPolicy(policy)?.localSpeedCommands.has(commandName) ?? false
  );
}

function isImmediateCommand(
  commandName: string,
  policy?: AgentSlashCommandPolicy | null
): boolean {
  return (
    providerSlashPolicy(policy)?.immediateCommands.has(commandName) ?? false
  );
}

function normalizedCommandSet(values: readonly string[]): ReadonlySet<string> {
  return new Set(
    values.map((value) => value.trim().toLowerCase()).filter(Boolean)
  );
}

export function slashCommandPolicyFromComposerOptions(
  options: AgentActivityComposerOptions | null
): AgentSlashCommandPolicy | null {
  return options?.slashCommandPolicy ?? null;
}

function normalizedCommandName(command: { name: string }): string {
  return command.name.trim().toLowerCase();
}

function isBrowserUseCapability(
  command: AgentSlashCommand
): command is AgentSlashCommandCapability {
  return (
    "kind" in command &&
    command.kind === "capability" &&
    command.capability === "browserUse"
  );
}

function isComputerUseCapability(
  command: AgentSlashCommand
): command is AgentSlashCommandCapability {
  return (
    "kind" in command &&
    command.kind === "capability" &&
    command.capability === "computerUse"
  );
}

function isCapabilityCommand(
  command: AgentSlashCommand,
  capability: AgentSlashCommandCapability["capability"]
): command is AgentSlashCommandCapability {
  return (
    "kind" in command &&
    command.kind === "capability" &&
    command.capability === capability
  );
}

function isAgentSlashCommandCapability(
  command: AgentSlashCommand
): command is AgentSlashCommandCapability {
  return "kind" in command && command.kind === "capability";
}

function slashCommandMatchesInvocation(
  command: AgentSlashCommand,
  commandName: string
): boolean {
  const normalizedInvocation = commandName.trim().toLowerCase();
  if (normalizedCommandName(command) === normalizedInvocation) {
    return true;
  }
  const aliases = "aliases" in command ? (command.aliases ?? []) : [];
  return aliases.some(
    (alias) => alias.trim().toLowerCase() === normalizedInvocation
  );
}

function filterUnavailableSlashCommands(
  commands: readonly AgentSessionCommand[],
  input: {
    compactSupported?: boolean | null;
    hasCompactableContext: boolean;
  }
): AgentSessionCommand[] {
  return commands.filter((command) => {
    const commandName = normalizedCommandName(command);
    if (commandName === "compact") {
      if (input.compactSupported === false) {
        return false;
      }
      return input.hasCompactableContext;
    }
    return true;
  });
}
