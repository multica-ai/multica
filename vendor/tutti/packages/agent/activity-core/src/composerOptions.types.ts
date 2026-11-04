import type { AgentActivityComposerModelConfiguration } from "./composerModelConfiguration.types.ts";
import type { AgentActivitySessionCapabilities } from "./sessionCapabilities.types.ts";

export interface AgentActivityComposerSettingOption {
  value: string;
  label: string;
  description?: string;
  consumptionMultiplier?: string;
  supportsImageInput?: boolean;
  /** True when the entry mirrors the requested/current selection, not the provider catalog. */
  requested?: boolean;
}

export interface AgentActivityComposerCommandOption {
  name: string;
  description?: string;
  inputHint?: string;
}

export interface AgentActivityComposerSkillOption {
  name: string;
  trigger: string;
  invocation?: "promptItem" | "textTrigger";
  sourceKind:
    | "project"
    | "personal"
    | "bundled"
    | "plugin"
    | "system"
    | "tutti-injected"
    | "connector";
  description?: string;
  pluginName?: string;
  path?: string;
  kind?: "skill" | "connector";
}

export interface AgentActivityComposerCapabilityOption {
  id: string;
  kind: "skill" | "plugin" | "connector" | "mcpServer" | "mcpTool";
  name: string;
  label: string;
  status:
    | "available"
    | "disabled"
    | "authRequired"
    | "setupRequired"
    | "unsupported";
  invocation: "promptItem" | "textTrigger" | "none";
  description?: string;
  iconUrl?: string;
  installedAtUnixMs?: number;
  source?: string;
  pluginName?: string;
  serverName?: string;
  toolName?: string;
  trigger?: string;
  path?: string;
}

export interface AgentActivityComposerPermissionModeOption {
  id: string;
  label?: string;
  description?: string;
  semantic?: string;
}

export interface AgentActivityComposerPermissionConfig {
  configurable: boolean;
  defaultValue?: string | null;
  modes: AgentActivityComposerPermissionModeOption[];
}

export interface AgentActivityComposerSettings {
  codexSaverMode?: boolean | null;
  rtkSaverMode?: boolean | null;
  model?: string | null;
  reasoningEffort?: string | null;
  speed?: string | null;
  planMode?: boolean | null;
  permissionModeId?: string | null;
}

export type AgentActivitySlashCommandEffect =
  | "submitImmediate"
  | "showReviewPicker"
  | "togglePlanMode"
  | "showStatus"
  | "toggleSpeed";

export interface AgentActivitySlashCommandPolicy {
  fallbackCommands: readonly string[];
  commandCatalogAuthoritative?: boolean;
  commandEffects: readonly {
    command: string;
    effect: AgentActivitySlashCommandEffect;
  }[];
}

export interface AgentActivityComposerBehavior {
  collapseModelOptionsToLatest: boolean;
  modelOptionsAuthoritative: boolean;
  refreshModelOptionsAfterSettings: boolean;
  prewarmDraftSession: boolean;
  planModeExclusiveWithPermissionMode: boolean;
}

export interface AgentActivityComposerOptions {
  codexSaverModeSupported?: boolean;
  rtkSaverModeSupported?: boolean;
  provider: string;
  /** Typed capabilities available before a session exists. */
  capabilities: AgentActivitySessionCapabilities | null;
  models: AgentActivityComposerSettingOption[];
  reasoningEfforts: AgentActivityComposerSettingOption[];
  reasoningOptionsByModel?: Record<
    string,
    {
      defaultValue?: string | null;
      options: AgentActivityComposerSettingOption[];
    }
  >;
  /** Orthogonal speed tiers (e.g. standard/fast); empty when unsupported. */
  speeds: AgentActivityComposerSettingOption[];
  /** Mirrors tuttid modelConfig.configurable; false when absent. */
  modelConfigurable?: boolean;
  /**
   * Provider-resolved runtime model. This remains separate from the selected
   * model so an inherited `default` choice stays inherited.
   */
  effectiveModel?: string | null;
  /** Mirrors tuttid reasoningConfig.configurable; false when absent. */
  reasoningConfigurable?: boolean;
  /** Mirrors tuttid speedConfig.configurable; false when absent. */
  speedConfigurable?: boolean;
  /** Effective pre-session settings paired with this options snapshot. */
  effectiveSettings?: AgentActivityComposerSettings | null;
  permissionConfig?: AgentActivityComposerPermissionConfig | null;
  draftAgentSessionId?: string | null;
  modelOptionsLoading?: boolean;
  skills: AgentActivityComposerSkillOption[];
  /** Commands advertised by the live provider session and reusable after event replay gaps. */
  commands?: readonly AgentActivityComposerCommandOption[];
  capabilityCatalog?: AgentActivityComposerCapabilityOption[];
  behavior: AgentActivityComposerBehavior;
  slashCommandPolicy?: AgentActivitySlashCommandPolicy | null;
  /** Credential-free model-plan identity projected from daemon runtime context. */
  modelPlan?: {
    id: string;
    name: string;
    protocol?: string | null;
  } | null;
  /** Authoritative model default identity for the selected agent target. */
  modelConfiguration?: AgentActivityComposerModelConfiguration | null;
  loadedAtUnixMs: number;
}

export interface AgentActivityLoadComposerOptionsInput {
  /**
   * Agent target id — the daemon-facing identity of the composer target.
   * activity-core treats it as an opaque targetKey. This field name reflects
   * that to the daemon it is an agent target id. Optional at the adapter
   * boundary to mirror the daemon's optional request field; the engine command
   * port always supplies a non-empty value.
   */
  agentTargetId?: string | null;
  agentSessionId?: string | null;
  workspaceId: string;
  provider: string;
  section?: "full" | "core" | "capabilities" | "connectors";
  /** Wait only when the user explicitly opens the model picker. */
  waitForFreshModelCatalog?: boolean;
  cwd?: string | null;
  settings?: AgentActivityComposerSettings | null;
  signal?: AbortSignal;
}

export type AgentActivityComposerOptionsLoadStatus =
  | "loading"
  | "ready"
  | "error";
