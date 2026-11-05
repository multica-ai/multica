import type { RichTextMentionService } from "@tutti-os/ui-rich-text/service";
import type { WorkspaceFileEntry } from "@tutti-os/workspace-file-manager/services";
import type {
  ReferenceProvenanceCatalog,
  WorkspaceFileReference,
  WorkspaceFileReferenceAdapter
} from "@tutti-os/workspace-file-reference/contracts";
import type { ReferenceSourceAggregator } from "@tutti-os/workspace-file-reference/core";
import type { ReferenceSourcePickerProps } from "@tutti-os/workspace-file-reference/ui";
import type { ReactNode } from "react";
import type { WorkspaceLinkAction } from "../../actions/workspaceLinkActions";
import type { AgentSettings } from "../../contexts/settings/domain/agentSettings";
import type { OpenAgentEnvPanelInput } from "../../shared/agentEnv";
import type {
  AgentVisibleErrorOverrides,
  AgentVisibleErrorPresentationScope
} from "../../shared/agentEnv/agentErrorPresentation";
import type { AgentMessageMarkdownWorkspaceAppIcon } from "../../shared/AgentMessageMarkdown";
import type {
  AgentGUIAgentOwnership,
  AgentGUIAgentTarget,
  AgentGUIAgentTargetInfoRenderer,
  AgentGUIHomeSuggestionId,
  AgentGUIInteractionReadinessSource,
  AgentGUINodeData,
  AgentGUIObservationGapSource,
  AgentGUIProvider,
  AgentGUIProviderRailMode,
  AgentGUIProviderReadinessGate,
  AgentGUITargetConnectionSource,
  NodeFrame,
  Point
} from "../../types";
import type { AgentGuiWorkbenchCommandBridge } from "../../workbench/commands";
import type { DesktopSize } from "../workspaceDesktop/types";
import type {
  AgentComposerCapabilityMenuState,
  AgentComposerCapabilitySettingsTarget,
  AgentComposerGitBranchLoader,
  AgentComposerProps
} from "./AgentComposer";
import type { AgentProjectDropdownOptions } from "./AgentComposerProjectMenu";
import type {
  AgentGUIComposerEmptySubmit,
  AgentGUIComposerFooterAccessoryRenderer,
  AgentGUIConversationRailLayout,
  AgentGUISidebarFooterContext,
  AgentMentionReferenceTargetResolver,
  AgentWorkspaceReferenceInitialTargetResolver
} from "./AgentGUINodeView";
import type {
  AgentGUIOpenSessionRequest,
  AgentGUIPrefillPromptRequest,
  AgentGUIRememberComposerDefaultsInput,
  AgentGUIRememberComposerDefaultsResult
} from "./controller/agentGuiController";
import type { AgentStatusController } from "./controller/AgentStatusController";
import type { AgentGUIComposerAppendRequest } from "./controller/useAgentGUIComposerAppendRequest";
import type { AgentGUIEngagementEventSink } from "./engagement/agentGUIEngagement.types";
import type { AgentGUISessionLaunchMode } from "./model/agentSessionLaunchMode";

export interface AgentGUINodeIdentity {
  nodeId: string;
  workspaceId: string;
  currentUserId?: string | null;
  title: string;
}

export interface AgentGUINodeWorkspace {
  path: string;
  fileReferenceAdapter?: WorkspaceFileReferenceAdapter | null;
  onRequestGitBranches?: AgentComposerGitBranchLoader | null;
  selectProjectDirectory?: () => Promise<{ path: string } | null>;
  resolveExternalPromptEntries?: AgentComposerProps["resolveExternalPromptEntries"];
  prepareExternalPromptFiles?: AgentComposerProps["prepareExternalPromptFiles"];
  resolvePastedPath?: AgentComposerProps["resolvePastedPath"];
  promptAssetLimit?: number | null;
  projectDirectorySourceAggregator?: ReferenceSourceAggregator | null;
  referenceSourceAggregator?: ReferenceSourceAggregator | null;
  resolveReferenceContentErrorAction?: ReferenceSourcePickerProps["resolveContentErrorAction"];
  resolveReferenceEntryIconUrl?: (
    entry: WorkspaceFileEntry
  ) => Promise<string | null | undefined>;
  resolveMentionReferenceTarget?: AgentMentionReferenceTargetResolver | null;
  resolveReferenceInitialTarget?: AgentWorkspaceReferenceInitialTargetResolver | null;
  onFileReferencesAdded?: (input: {
    provider: AgentGUIProvider;
    references: readonly WorkspaceFileReference[];
  }) => void | Promise<void>;
  agentSettings: Pick<AgentSettings, "avoidGroupingEdits">;
}

export interface AgentGUINodeFrameLayout {
  position: Point;
  width: number;
  height: number;
  desktopSize: DesktopSize;
  isMaximized?: boolean;
  isActive: boolean;
  /** Host-projected presentation visibility. Independent from node focus. */
  isVisible?: boolean;
  embedded?: boolean;
  /**
   * Standalone windows preserve the middle conversation width and collapse
   * the conversation Rail before it can be compressed. Other surfaces retain
   * the default responsive policy.
   */
  conversationRailAutoCollapseMode?: "preserve-middle-content";
}

export interface AgentGUINodeRuntimeRequests {
  composerAppend?: AgentGUIComposerAppendRequest | null;
  composerFocusSequence?: number | null;
  workbench?: AgentGuiWorkbenchCommandBridge | null;
  openSession?: AgentGUIOpenSessionRequest | null;
  prefillPrompt?: AgentGUIPrefillPromptRequest | null;
  /** On-demand status capability. Transport and owner resolution stay host-owned. */
  agentStatusController?: AgentStatusController | null;
}

export interface AgentGUINodeHostCapabilities {
  /**
   * Complete host-owned catalog for reference provenance filtering. Supplying
   * it explicitly opts the host into the dimensions declared by the catalog.
   * Omit it to keep filtering disabled unless the legacy Agent-only flag is
   * enabled.
   */
  referenceProvenanceFilterCatalog?: ReferenceProvenanceCatalog | null;
  /** Legacy Tutti Agent-only opt-in. Prefer an explicit catalog in new hosts. */
  referenceProvenanceFilterEnabled?: boolean;
  /** Host-owned experimental opt-in for current-Session composer history. */
  sessionInputHistoryEnabled?: boolean;
  /** Host-owned experimental opt-in for Side and transcript selection actions. */
  /** Optional presentation-only bridge for rendering Side outside AgentGUI. */
  /** Host-owned opt-in for launching self-owned local Sessions in git worktrees. */
  sessionWorktreeEnabled?: boolean;
  /** Host-owned durable launch preference projection for this workspace. */
  sessionLaunchModesByProjectSectionKey?: Readonly<
    Record<string, AgentGUISessionLaunchMode>
  >;
  /** Host-owned experimental opt-in for the Codex saver-mode composer entry. */
  codexSaverModeEntryEnabled?: boolean;
  /** Host-owned experimental opt-in for the provider-neutral RTK saver mode. */
  rtkSaverModeEntryEnabled?: boolean;
  capabilityMenuState?: AgentComposerCapabilityMenuState;
  /**
   * Keeps owner-supported Browser/Computer capability entries visible while
   * preventing this host from mutating device-owned capability settings.
   */
  capabilityControlsReadOnly?: boolean;
  /**
   * Host-owned product copy and external action for structured run errors.
   * AgentGUI owns the generic card; product domains own product semantics.
   */
  visibleErrorPresentationOverrides?: AgentVisibleErrorOverrides | null;
  /**
   * Presentation-only remediation authority for visible errors. Omission
   * retains local-owner behavior for backwards compatibility.
   */
  visibleErrorPresentationScope?: AgentVisibleErrorPresentationScope;
  agentTargets?: readonly AgentGUIAgentTarget[];
  agentTargetsLoading?: boolean;
  /** Complete presentation-only catalog for resolving Agent mention identity. */
  mentionAgentTargets?: readonly AgentGUIAgentTarget[];
  /** Host-owned mention categories to omit from the palette. */
  hiddenMentionFilterIds?: readonly string[];
  /** Launch-only targets for active-conversation handoff. */
  handoffAgentTargets?: readonly AgentGUIAgentTarget[];
  handoffAgentTargetsLoading?: boolean;
  /** Hidden by default; hosts may opt into ownership copy for collaborative products. */
  showHandoffTargetOwnershipLabels?: boolean;
  providerRailMode?: AgentGUIProviderRailMode;
  comingSoonProviders?: readonly AgentGUIProvider[];
  providerReadinessGates?: Partial<
    Record<AgentGUIProvider, AgentGUIProviderReadinessGate | null>
  > | null;
  /** Tutti-only presentation opt-in for placing usage refresh in the limits header. */
  accountUsageRefreshInline?: boolean;
  /** Target-level connection for new-conversation and ordinary Composer admission. */
  targetConnectionSource?: AgentGUITargetConnectionSource | null;
  /**
   * Host-owned write readiness keyed by exact pending Interaction identity.
   * When present for the displayed prompt, it takes precedence over target
   * connection and exact-Turn observation-gap presentation.
   */
  interactionReadinessSource?: AgentGUIInteractionReadinessSource | null;
  /** Host-owned, ephemeral projection gap keyed by exact Session and Turn. */
  observationGapSource?: AgentGUIObservationGapSource | null;
  defaultAgentTargetId?: string | null;
  providerAuthAccountLabels?: Partial<Record<string, string>>;
  mentionService?: RichTextMentionService;
  workspaceAppIcons?: readonly AgentMessageMarkdownWorkspaceAppIcon[];
  disabledHomeSuggestions?: readonly AgentGUIHomeSuggestionId[];
}

export interface AgentGUINodeHostActions {
  /** Confirms that AgentGUI applied one host-issued composer append request. */
  onComposerAppendHandled?: (sequence: number) => void;
  onLinkAction?: (action: WorkspaceLinkAction) => void;
  onHandoffConversation?: (input: {
    agentTargetId?: string | null;
    draftPrompt: string;
    provider: AgentGUIProvider;
    sourceAgentSessionId: string;
    userProjectPath?: string | null;
  }) => void | Promise<void>;
  onCapabilitySettingsRequest?: (
    capability: AgentComposerCapabilitySettingsTarget
  ) => void | Promise<void>;
  onAgentProviderLogin?: (provider: AgentGUIProvider) => void;
  onAgentEnvPanelOpen?: (input?: OpenAgentEnvPanelInput) => void;
  /**
   * Notifies the Host when the exact target's config menu opens. Account and
   * Commerce refreshes remain Host-owned and must not enter Agent status.
   */
  onAgentConfigMenuOpen?: (context: AgentGUIAgentConfigMenuContext) => void;
  onOpenConversationWindow?: (agentSessionId: string) => void;
  onClose: () => void;
  onResize: (frame: NodeFrame) => void;
  onUpdateNode: (
    updater: (current: AgentGUINodeData) => AgentGUINodeData
  ) => void;
  onRememberComposerDefaults?: (
    input: AgentGUIRememberComposerDefaultsInput
  ) => void | Promise<AgentGUIRememberComposerDefaultsResult>;
  onSessionLaunchModePreferenceChange?: (input: {
    mode: AgentGUISessionLaunchMode;
    projectSectionKey: string;
  }) => void | Promise<void>;
  isMuted?: boolean;
  onMinimize?: () => void;
  onToggleMaximize?: () => void;
  onShowMessage?: (
    message: string,
    tone?: "info" | "warning" | "error"
  ) => void;
  onEngagementEvent?: AgentGUIEngagementEventSink;
  /**
   * Reports live left-side rail layout width while the conversation rail is
   * being resized. Hosts with external chrome aligned to the rail can consume
   * this instead of observing package DOM/style mutations.
   */
  onConversationRailLayoutChange?: (
    layout: AgentGUIConversationRailLayout
  ) => void;
}

export interface AgentGUIAgentConfigMenuContext {
  agentTargetId: string;
  provider: AgentGUIProvider;
  label: string;
  ownership?: AgentGUIAgentOwnership;
  /** The Host must render interactive account controls as ui-system menu items. */
  presentation: "menu";
}

export interface AgentGUIConfigMenuPresentationContext {
  /** Interactive slot content must use ui-system DropdownMenuItem/Sub primitives. */
  presentation: "menu";
}

export interface AgentGUINodeRenderSlots {
  /** Host-owned controls appended to the composer footer. */
  composerFooterAccessory?: AgentGUIComposerFooterAccessoryRenderer;
  /** Optional Host action for the send button while the composer is empty. */
  composerEmptySubmit?: AgentGUIComposerEmptySubmit | null;
  /**
   * Optional Host-owned information for an exact Agent target. AgentGUI owns
   * tooltip mechanics and invokes this renderer lazily for supported surfaces.
   */
  agentTargetInfo?: AgentGUIAgentTargetInfoRenderer;
  /**
   * Optional Host chrome for the exact target's account/Commerce presentation.
   * Returning null preserves AgentGUI's provider account and quota content.
   * Interactive content must honor the supplied menu presentation contract.
   */
  agentConfigAccount?: (context: AgentGUIAgentConfigMenuContext) => ReactNode;
  /**
   * Optional Host-owned system actions appended to the Agent config menu.
   * Actions must be ui-system DropdownMenuItem/Sub primitives.
   */
  agentConfigSystemActions?: (
    context: AgentGUIConfigMenuPresentationContext
  ) => ReactNode;
  projectDirectoryPickerHeaderActions?: ReferenceSourcePickerProps["renderHeaderActions"];
  projectSelectOptions?: AgentProjectDropdownOptions;
  referencePickerSidebarActions?: (
    context: Parameters<
      NonNullable<ReferenceSourcePickerProps["renderSidebarActions"]>
    >[0] & { purpose: "directory" | "reference" }
  ) => ReactNode;
  sidebarFooter?: (ctx: AgentGUISidebarFooterContext) => ReactNode;
}

export interface AgentGUINodeProps {
  identity: AgentGUINodeIdentity;
  workspace: AgentGUINodeWorkspace;
  frame: AgentGUINodeFrameLayout;
  state: AgentGUINodeData;
  runtimeRequests: AgentGUINodeRuntimeRequests;
  hostCapabilities: AgentGUINodeHostCapabilities;
  hostActions: AgentGUINodeHostActions;
  renderSlots: AgentGUINodeRenderSlots;
}

function agentGuiStateEquals(
  left: AgentGUINodeData,
  right: AgentGUINodeData
): boolean {
  return (
    left === right ||
    (left.provider === right.provider &&
      (left.agentTargetId ?? null) === (right.agentTargetId ?? null) &&
      left.lastActiveAgentSessionId === right.lastActiveAgentSessionId &&
      stringRecordsEqual(
        left.lastActiveAgentSessionIdByAgentTargetId,
        right.lastActiveAgentSessionIdByAgentTargetId
      ) &&
      left.conversationRailWidthPx === right.conversationRailWidthPx &&
      left.conversationRailCollapsed === right.conversationRailCollapsed &&
      (left.composerOverrides?.model ?? null) ===
        (right.composerOverrides?.model ?? null) &&
      left.composerOverrides?.codexSaverMode ===
        right.composerOverrides?.codexSaverMode &&
      left.composerOverrides?.rtkSaverMode ===
        right.composerOverrides?.rtkSaverMode &&
      (left.composerOverrides?.reasoningEffort ?? null) ===
        (right.composerOverrides?.reasoningEffort ?? null) &&
      (left.composerOverrides?.planMode ?? null) ===
        (right.composerOverrides?.planMode ?? null) &&
      (left.composerOverrides?.permissionModeId ?? null) ===
        (right.composerOverrides?.permissionModeId ?? null) &&
      composerOverridesByProviderEqual(
        left.composerOverridesByProvider,
        right.composerOverridesByProvider
      ) &&
      composerOverridesByAgentTargetIdEqual(
        left.composerOverridesByAgentTargetId,
        right.composerOverridesByAgentTargetId
      ))
  );
}

function composerOverridesByProviderEqual(
  left: AgentGUINodeData["composerOverridesByProvider"],
  right: AgentGUINodeData["composerOverridesByProvider"]
): boolean {
  const providers = new Set([
    ...Object.keys(left ?? {}),
    ...Object.keys(right ?? {})
  ]);
  for (const provider of providers) {
    const key = provider as keyof NonNullable<
      AgentGUINodeData["composerOverridesByProvider"]
    >;
    const leftSettings = left?.[key] ?? null;
    const rightSettings = right?.[key] ?? null;
    if (
      leftSettings?.codexSaverMode !== rightSettings?.codexSaverMode ||
      leftSettings?.rtkSaverMode !== rightSettings?.rtkSaverMode ||
      (leftSettings?.model ?? null) !== (rightSettings?.model ?? null) ||
      (leftSettings?.reasoningEffort ?? null) !==
        (rightSettings?.reasoningEffort ?? null) ||
      (leftSettings?.planMode ?? null) !== (rightSettings?.planMode ?? null) ||
      (leftSettings?.permissionModeId ?? null) !==
        (rightSettings?.permissionModeId ?? null)
    ) {
      return false;
    }
  }
  return true;
}

function composerOverridesByAgentTargetIdEqual(
  left: AgentGUINodeData["composerOverridesByAgentTargetId"],
  right: AgentGUINodeData["composerOverridesByAgentTargetId"]
): boolean {
  const keys = new Set([
    ...Object.keys(left ?? {}),
    ...Object.keys(right ?? {})
  ]);
  for (const key of keys) {
    const leftSettings = left?.[key] ?? null;
    const rightSettings = right?.[key] ?? null;
    if (
      leftSettings?.codexSaverMode !== rightSettings?.codexSaverMode ||
      leftSettings?.rtkSaverMode !== rightSettings?.rtkSaverMode ||
      (leftSettings?.model ?? null) !== (rightSettings?.model ?? null) ||
      (leftSettings?.reasoningEffort ?? null) !==
        (rightSettings?.reasoningEffort ?? null) ||
      (leftSettings?.planMode ?? null) !== (rightSettings?.planMode ?? null) ||
      (leftSettings?.permissionModeId ?? null) !==
        (rightSettings?.permissionModeId ?? null)
    ) {
      return false;
    }
  }
  return true;
}

function stringRecordsEqual(
  left: Record<string, string> | null | undefined,
  right: Record<string, string> | null | undefined
): boolean {
  const leftKeys = Object.keys(left ?? {}).sort();
  const rightKeys = Object.keys(right ?? {}).sort();
  return (
    leftKeys.length === rightKeys.length &&
    leftKeys.every(
      (key, index) => key === rightKeys[index] && left?.[key] === right?.[key]
    )
  );
}

export function areAgentGUINodePropsEqual(
  previous: AgentGUINodeProps,
  next: AgentGUINodeProps
): boolean {
  const pi = previous.identity,
    ni = next.identity;
  const pw = previous.workspace,
    nw = next.workspace;
  const pf = previous.frame,
    nf = next.frame;
  const pr = previous.runtimeRequests,
    nr = next.runtimeRequests;
  const pc = previous.hostCapabilities,
    nc = next.hostCapabilities;
  const pa = previous.hostActions,
    na = next.hostActions;
  const ps = previous.renderSlots,
    ns = next.renderSlots;
  return (
    pi.nodeId === ni.nodeId &&
    pi.workspaceId === ni.workspaceId &&
    pi.currentUserId === ni.currentUserId &&
    pi.title === ni.title &&
    pw.path === nw.path &&
    pw.fileReferenceAdapter === nw.fileReferenceAdapter &&
    pw.onRequestGitBranches === nw.onRequestGitBranches &&
    pw.selectProjectDirectory === nw.selectProjectDirectory &&
    pw.resolveExternalPromptEntries === nw.resolveExternalPromptEntries &&
    pw.prepareExternalPromptFiles === nw.prepareExternalPromptFiles &&
    pw.resolvePastedPath === nw.resolvePastedPath &&
    pw.promptAssetLimit === nw.promptAssetLimit &&
    pw.projectDirectorySourceAggregator ===
      nw.projectDirectorySourceAggregator &&
    pw.referenceSourceAggregator === nw.referenceSourceAggregator &&
    pw.resolveReferenceContentErrorAction ===
      nw.resolveReferenceContentErrorAction &&
    pw.resolveReferenceEntryIconUrl === nw.resolveReferenceEntryIconUrl &&
    pw.resolveMentionReferenceTarget === nw.resolveMentionReferenceTarget &&
    pw.resolveReferenceInitialTarget === nw.resolveReferenceInitialTarget &&
    pw.onFileReferencesAdded === nw.onFileReferencesAdded &&
    pw.agentSettings.avoidGroupingEdits ===
      nw.agentSettings.avoidGroupingEdits &&
    pc.referenceProvenanceFilterCatalog ===
      nc.referenceProvenanceFilterCatalog &&
    pc.referenceProvenanceFilterEnabled ===
      nc.referenceProvenanceFilterEnabled &&
    pc.hiddenMentionFilterIds === nc.hiddenMentionFilterIds &&
    pc.sessionInputHistoryEnabled === nc.sessionInputHistoryEnabled &&
    pc.sessionWorktreeEnabled === nc.sessionWorktreeEnabled &&
    pc.sessionLaunchModesByProjectSectionKey ===
      nc.sessionLaunchModesByProjectSectionKey &&
    pc.codexSaverModeEntryEnabled === nc.codexSaverModeEntryEnabled &&
    pc.rtkSaverModeEntryEnabled === nc.rtkSaverModeEntryEnabled &&
    agentGuiStateEquals(previous.state, next.state) &&
    pf.position.x === nf.position.x &&
    pf.position.y === nf.position.y &&
    pf.width === nf.width &&
    pf.height === nf.height &&
    pf.desktopSize.width === nf.desktopSize.width &&
    pf.desktopSize.height === nf.desktopSize.height &&
    pf.isMaximized === nf.isMaximized &&
    pf.isActive === nf.isActive &&
    pf.isVisible === nf.isVisible &&
    pf.embedded === nf.embedded &&
    pf.conversationRailAutoCollapseMode ===
      nf.conversationRailAutoCollapseMode &&
    pr.composerFocusSequence === nr.composerFocusSequence &&
    pr.composerAppend === nr.composerAppend &&
    pr.workbench?.instanceId === nr.workbench?.instanceId &&
    pr.workbench?.onConversationRailToggle ===
      nr.workbench?.onConversationRailToggle &&
    pr.openSession === nr.openSession &&
    pr.prefillPrompt === nr.prefillPrompt &&
    pr.agentStatusController === nr.agentStatusController &&
    pc.capabilityMenuState === nc.capabilityMenuState &&
    pc.capabilityControlsReadOnly === nc.capabilityControlsReadOnly &&
    pc.agentTargets === nc.agentTargets &&
    pc.agentTargetsLoading === nc.agentTargetsLoading &&
    pc.handoffAgentTargets === nc.handoffAgentTargets &&
    pc.handoffAgentTargetsLoading === nc.handoffAgentTargetsLoading &&
    pc.showHandoffTargetOwnershipLabels ===
      nc.showHandoffTargetOwnershipLabels &&
    pc.providerRailMode === nc.providerRailMode &&
    pc.comingSoonProviders === nc.comingSoonProviders &&
    pc.providerReadinessGates === nc.providerReadinessGates &&
    pc.accountUsageRefreshInline === nc.accountUsageRefreshInline &&
    pc.targetConnectionSource === nc.targetConnectionSource &&
    pc.interactionReadinessSource === nc.interactionReadinessSource &&
    pc.observationGapSource === nc.observationGapSource &&
    pc.defaultAgentTargetId === nc.defaultAgentTargetId &&
    pc.providerAuthAccountLabels === nc.providerAuthAccountLabels &&
    pc.mentionService === nc.mentionService &&
    pc.workspaceAppIcons === nc.workspaceAppIcons &&
    pc.disabledHomeSuggestions === nc.disabledHomeSuggestions &&
    pa.onLinkAction === na.onLinkAction &&
    pa.onHandoffConversation === na.onHandoffConversation &&
    pa.onCapabilitySettingsRequest === na.onCapabilitySettingsRequest &&
    pa.onAgentProviderLogin === na.onAgentProviderLogin &&
    pa.onAgentEnvPanelOpen === na.onAgentEnvPanelOpen &&
    pa.onAgentConfigMenuOpen === na.onAgentConfigMenuOpen &&
    pa.onComposerAppendHandled === na.onComposerAppendHandled &&
    pa.onOpenConversationWindow === na.onOpenConversationWindow &&
    pa.onClose === na.onClose &&
    pa.onResize === na.onResize &&
    pa.onUpdateNode === na.onUpdateNode &&
    pa.onRememberComposerDefaults === na.onRememberComposerDefaults &&
    pa.onSessionLaunchModePreferenceChange ===
      na.onSessionLaunchModePreferenceChange &&
    pa.isMuted === na.isMuted &&
    pa.onMinimize === na.onMinimize &&
    pa.onToggleMaximize === na.onToggleMaximize &&
    pa.onShowMessage === na.onShowMessage &&
    pa.onEngagementEvent === na.onEngagementEvent &&
    pa.onConversationRailLayoutChange === na.onConversationRailLayoutChange &&
    ps.agentConfigAccount === ns.agentConfigAccount &&
    ps.agentConfigSystemActions === ns.agentConfigSystemActions &&
    ps.agentTargetInfo === ns.agentTargetInfo &&
    ps.composerFooterAccessory === ns.composerFooterAccessory &&
    ps.composerEmptySubmit === ns.composerEmptySubmit &&
    ps.projectDirectoryPickerHeaderActions ===
      ns.projectDirectoryPickerHeaderActions &&
    ps.projectSelectOptions === ns.projectSelectOptions &&
    ps.referencePickerSidebarActions === ns.referencePickerSidebarActions &&
    ps.sidebarFooter === ns.sidebarFooter
  );
}
