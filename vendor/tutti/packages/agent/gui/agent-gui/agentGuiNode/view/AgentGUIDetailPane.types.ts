import type { WorkspaceUserProjectI18nRuntime } from "@tutti-os/workspace-user-project/i18n";
import type { WorkspaceLinkAction } from "../../../actions/workspaceLinkActions";
import type { UiLanguage } from "../../../contexts/settings/domain/agentSettings";
import type { AgentMessageMarkdownWorkspaceAppIcon } from "../../../shared/AgentMessageMarkdown";
import type {
  AgentComposerGitBranchLoader,
  AgentComposerProps,
  AgentComposerSlashStatusLimit,
  WorkspaceReferencePickResult
} from "../AgentComposer";
import type { AgentContextMentionItem } from "../agentRichText/agentFileMentionExtension";
import type { AgentGUIComposerEngagement } from "../engagement/agentGUIEngagement.types";
import type {
  AgentGUIComposerViewModel,
  AgentGUIDetailViewModel,
  AgentGUIInteractionViewModel,
  AgentGUIOperationsViewModel,
  AgentGUIRailViewModel,
  AgentGUIReadinessViewModel,
  AgentGUIShellViewModel
} from "../model/agentGuiNodeTypes";
import type { AgentGUIManagedHomeTargetProjection } from "../model/agentGuiProviderRailOrder";
import type { AgentGUISessionLaunchMode } from "../model/agentSessionLaunchMode";
import type {
  AgentGUIComposerEmptySubmit,
  AgentGUIComposerFooterAccessoryRenderer
} from "./AgentGUIComposerFooterAccessory.types";
import type {
  AgentGUINodeViewProps,
  AgentGUIViewLabels
} from "./AgentGUINodeView.types";

type AgentGUIDetailExternalPromptProps = Pick<
  AgentComposerProps,
  | "resolveExternalPromptEntries"
  | "prepareExternalPromptFiles"
  | "resolvePastedPath"
  | "promptAssetLimit"
>;

export interface AgentGUIDetailPaneProps
  extends AgentGUIDetailExternalPromptProps {
  shell: AgentGUIShellViewModel;
  rail: AgentGUIRailViewModel;
  detail: AgentGUIDetailViewModel;
  composer: AgentGUIComposerViewModel;
  interaction: AgentGUIInteractionViewModel;
  readiness: AgentGUIReadinessViewModel;
  operations: AgentGUIOperationsViewModel;
  homeTargetProjection: AgentGUIManagedHomeTargetProjection;
  referenceProvenanceFilters?: AgentComposerProps["referenceProvenanceFilters"];
  hiddenMentionFilterIds?: readonly string[];
  sessionInputHistoryEnabled?: boolean;
  sessionWorktreeEnabled?: boolean;
  sessionLaunchModesByProjectSectionKey?: Readonly<
    Record<string, AgentGUISessionLaunchMode>
  >;
  onSessionLaunchModePreferenceChange?: (input: {
    mode: AgentGUISessionLaunchMode;
    projectSectionKey: string;
  }) => void | Promise<void>;
  composerEngagement?: AgentGUIComposerEngagement;
  actions: AgentGUINodeViewProps["actions"];
  labels: AgentGUIViewLabels;
  workspaceUserProjectI18n: WorkspaceUserProjectI18nRuntime;
  uiLanguage: UiLanguage;
  isActive: boolean;
  isVisible: boolean;
  workspaceReferencePickerOpen: boolean;
  composerFocusRequestSequence: number | null;
  slashStatusLimits: readonly AgentComposerSlashStatusLimit[];
  slashStatusLimitsLoading: boolean;
  slashStatusLimitsUnavailable: boolean;
  slashStatusOverride?: AgentComposerProps["slashStatus"];
  onSlashStatusOpen?: AgentComposerProps["onSlashStatusOpen"];
  onSlashStatusClose?: AgentComposerProps["onSlashStatusClose"];
  onSlashStatusRefresh?: AgentComposerProps["onSlashStatusRefresh"];
  onLinkAction?: (action: WorkspaceLinkAction) => void;
  onHandoffConversation?: AgentGUINodeViewProps["onHandoffConversation"];
  showHandoffTargetOwnershipLabels?: boolean;
  capabilityMenuState?: AgentComposerProps["capabilityMenuState"];
  capabilityControlsReadOnly?: AgentComposerProps["capabilityControlsReadOnly"];
  onCapabilitySettingsRequest?: AgentComposerProps["onCapabilitySettingsRequest"];
  onAgentProviderLogin?: (provider?: string | null) => void;
  onRequestWorkspaceReferences?:
    | ((
        entity?: AgentContextMentionItem | null
      ) => Promise<WorkspaceReferencePickResult>)
    | null;
  selectProjectDirectory?: () => Promise<{ path: string } | null>;
  projectSelectOptions?: AgentComposerProps["projectSelectOptions"];
  onRequestGitBranches?: AgentComposerGitBranchLoader | null;
  onRequestComposerFocus: () => void;
  workspaceAppIcons?: readonly AgentMessageMarkdownWorkspaceAppIcon[];
  renderComposerFooterAccessory?: AgentGUIComposerFooterAccessoryRenderer;
  composerEmptySubmit?: AgentGUIComposerEmptySubmit | null;
}
