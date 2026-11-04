import { useMemo, type JSX } from "react";
import type {
  AgentActivityComposerOptions,
  AgentPromptContentBlock
} from "@tutti-os/agent-activity-core";
import type { I18nRuntime } from "@tutti-os/ui-i18n-runtime";
import { TooltipProvider } from "@tutti-os/ui-system";
import type { RichTextMentionService } from "@tutti-os/ui-rich-text/service";
import type { WorkspaceUserProjectApi } from "@tutti-os/workspace-user-project/contracts";
import { createWorkspaceUserProjectI18nRuntime } from "@tutti-os/workspace-user-project/i18n";
import { AgentComposer } from "./agent-gui/agentGuiNode/AgentComposer";
import { AgentGUIMentionServiceBoundary } from "./agent-gui/agentGuiNode/AgentGUIMentionServiceBoundary";
import { useAgentGUIViewLabels } from "./agent-gui/agentGuiNode/AgentGUINode.labels";
import type { AgentComposerProps } from "./agent-gui/agentGuiNode/composer/AgentComposer.types";
import type {
  AgentGUIComposerGate,
  AgentGUIComposerSettingsVM
} from "./agent-gui/agentGuiNode/model/agentGuiNodeTypes";
import {
  agentComposerDraftPrompt,
  agentComposerDraftToPromptContent,
  agentPromptContentToComposerDraft
} from "./agent-gui/agentGuiNode/model/agentComposerDraft";
import {
  AgentGuiI18nProvider,
  type AgentGuiI18nLocale,
  useTranslation
} from "./i18n/index";
import type { AgentGUIAgentTarget } from "./types";
import {
  pickQuickComposerSettings,
  projectQuickComposerSettings,
  type AgentGUIQuickComposerSettings
} from "./quickComposerSettings";

export type { AgentGUIQuickComposerSettings } from "./quickComposerSettings";

const readyGate: AgentGUIComposerGate = {
  conversationBusy: false,
  isAwaitingTurnStart: false,
  editor: { reason: null, status: "editable" },
  runtime: {
    reason: null,
    sessionRuntimeReason: null,
    status: "ready"
  },
  submission: { reason: null, status: "ready" }
};
const emptyQuickComposerSettings: AgentGUIQuickComposerSettings = Object.freeze(
  {}
);

export type AgentGUIQuickComposerAgentTarget = Omit<
  AgentGUIAgentTarget,
  "agentTargetId" | "ref" | "targetId"
> & {
  agentTargetId: string;
};

export interface AgentGUIQuickComposerTargetCapabilities {
  imageInput: boolean;
  workspaceReferences: boolean;
}

export interface AgentGUIQuickComposerSettingsCapability {
  /** Host-owned authoritative options for the exact selected target/cwd. */
  options: AgentActivityComposerOptions | null;
  loading: boolean;
  /** Controlled sparse pre-session settings selected in this draft. */
  value: AgentGUIQuickComposerSettings;
  onChange(patch: AgentGUIQuickComposerSettings): void;
}

export interface AgentGUIQuickComposerSubmission {
  agentTargetId: string;
  content: AgentPromptContentBlock[];
  displayPrompt?: string;
  settings?: AgentGUIQuickComposerSettings;
}

export interface AgentGUIQuickComposerProps {
  agentTargets: readonly AgentGUIQuickComposerAgentTarget[];
  capabilitiesByAgentTargetId: Readonly<
    Record<string, AgentGUIQuickComposerTargetCapabilities | undefined>
  >;
  composerActionAccessory?: AgentComposerProps["composerActionAccessory"];
  composerActionPlacement?: AgentComposerProps["composerActionPlacement"];
  /** Omit the whole capability when the host cannot persist settings changes. */
  composerSettings?: AgentGUIQuickComposerSettingsCapability;
  content: readonly AgentPromptContentBlock[];
  disabled?: boolean;
  /** Fill a height explicitly assigned by the embedding host. */
  fillAvailableHeight?: boolean;
  /** Visual treatment for the canonical Composer input surface. */
  inputSurfaceVariant?: "default" | "borderless";
  /** Host i18n runtime, matching the full AgentGUI embedding contract. */
  i18n?: I18nRuntime<string> | null;
  locale?: AgentGuiI18nLocale;
  /** Mention providers supplied by the embedding host. */
  mentionService?: RichTextMentionService;
  /** Reserved host chrome above portaled composer menus. */
  menuViewportTopInset?: number;
  onAgentTargetChange(agentTargetId: string): void;
  onContentChange(content: AgentPromptContentBlock[]): void;
  onProjectPathChange?: AgentComposerProps["onProjectPathChange"];
  onRequestWorkspaceReferences?: AgentComposerProps["onRequestWorkspaceReferences"];
  onSubmit(submission: AgentGUIQuickComposerSubmission): void;
  placeholder?: string;
  selectedProjectPath?: string | null;
  selectedAgentTargetId: string;
  selectProjectDirectory?: AgentComposerProps["selectProjectDirectory"];
  /** Real project catalog and registration capability supplied by the host. */
  userProjectApi?: WorkspaceUserProjectApi | null;
  workspaceId: string;
}

/**
 * A lifecycle-free AgentGUI entry surface for launchers that already have a
 * workspace Engine owner. It owns only draft presentation; the Host receives
 * the submitted prompt and remains responsible for Session activation.
 */
export function AgentGUIQuickComposer(
  props: AgentGUIQuickComposerProps
): JSX.Element {
  return (
    <AgentGuiI18nProvider locale={props.locale} runtime={props.i18n}>
      <TooltipProvider delayDuration={120} skipDelayDuration={0}>
        <AgentGUIMentionServiceBoundary service={props.mentionService}>
          <AgentGUIQuickComposerInner {...props} />
        </AgentGUIMentionServiceBoundary>
      </TooltipProvider>
    </AgentGuiI18nProvider>
  );
}

function AgentGUIQuickComposerInner({
  agentTargets,
  capabilitiesByAgentTargetId,
  composerActionAccessory,
  composerActionPlacement,
  composerSettings: settingsCapability,
  content,
  disabled = false,
  fillAvailableHeight = false,
  inputSurfaceVariant = "default",
  menuViewportTopInset,
  onAgentTargetChange,
  onContentChange,
  onProjectPathChange,
  onRequestWorkspaceReferences,
  onSubmit,
  placeholder,
  selectedAgentTargetId,
  selectedProjectPath = null,
  selectProjectDirectory,
  userProjectApi,
  workspaceId
}: AgentGUIQuickComposerProps): JSX.Element {
  const { i18n, locale, t } = useTranslation();
  const selectedAgentTarget =
    agentTargets.find(
      (target) => target.agentTargetId === selectedAgentTargetId
    ) ?? null;
  const resolvedAgentTargetId = selectedAgentTarget?.agentTargetId ?? null;
  const selectedTargetCapabilities = selectedAgentTarget
    ? (capabilitiesByAgentTargetId[selectedAgentTarget.agentTargetId] ?? null)
    : null;
  const selectedTargetReady =
    selectedAgentTarget !== null &&
    selectedAgentTarget.disabled !== true &&
    selectedTargetCapabilities !== null;
  const draftHasImages = content.some((block) => block.type === "image");
  const provider = selectedAgentTarget?.provider ?? "";
  const composerOptions = settingsCapability?.options ?? null;
  const composerOptionsLoading = settingsCapability?.loading ?? false;
  const settingsCapabilityValue = settingsCapability?.value;
  const settings = useMemo(
    () =>
      settingsCapabilityValue
        ? pickQuickComposerSettings(settingsCapabilityValue)
        : emptyQuickComposerSettings,
    [settingsCapabilityValue]
  );
  const composerAgentTargets = useMemo<AgentGUIAgentTarget[]>(
    () =>
      agentTargets.map((target) => ({
        ...target,
        agentTargetId: target.agentTargetId,
        ref: { kind: "quick-composer", provider: target.provider },
        targetId: target.agentTargetId
      })),
    [agentTargets]
  );
  const selectedComposerAgentTarget =
    composerAgentTargets.find(
      (target) => target.agentTargetId === selectedAgentTargetId
    ) ?? null;
  const labels = useAgentGUIViewLabels({
    displayProviderLabel: selectedAgentTarget?.label ?? provider,
    fallbackAgentTitle: selectedAgentTarget?.label ?? provider,
    t,
    workspaceAppIcons: [],
    workspaceId
  });
  const workspaceUserProjectI18n = useMemo(
    () => createWorkspaceUserProjectI18nRuntime(i18n),
    [i18n]
  );
  const draftContent = useMemo(
    () => agentPromptContentToComposerDraft(content, "quick-composer"),
    [content]
  );
  const composerSettings = useMemo<AgentGUIComposerSettingsVM>(
    () =>
      projectQuickComposerSettings({
        agentTargetId: resolvedAgentTargetId,
        loading: composerOptionsLoading,
        options: composerOptions,
        projectLocked: disabled,
        provider,
        selectedProjectPath,
        settings
      }),
    [
      composerOptions,
      composerOptionsLoading,
      disabled,
      provider,
      resolvedAgentTargetId,
      selectedProjectPath,
      settings
    ]
  );

  return (
    <div
      className="agent-gui-node__shell"
      data-quick-composer-input-surface={inputSurfaceVariant}
    >
      <AgentComposer
        activePrompt={null}
        agentTargets={composerAgentTargets}
        availableCommands={[]}
        // Quick Composer hosts expose no capability-settings channel, so the
        // connector control could only render as a dead trigger. Hiding it
        // also keeps the footer controls on one alignment baseline.
        capabilityMenuState={{ connectors: { enabled: false } }}
        canUploadAttachment={Boolean(
          selectedTargetCapabilities?.imageInput ||
          selectedTargetCapabilities?.workspaceReferences
        )}
        composerActionAccessory={composerActionAccessory}
        composerActionPlacement={composerActionPlacement}
        composerSettings={composerSettings}
        drainingQueuedPromptId={null}
        draftContent={draftContent}
        gate={readyGate}
        fillAvailableHeight={fillAvailableHeight}
        isInterrupting={false}
        isSendingTurn={false}
        isSubmittingPrompt={disabled}
        layoutMode="embedded"
        menuViewportTopInset={menuViewportTopInset}
        labels={{
          ...labels,
          approvalLead: labels.approvalRequired,
          fileChangeApprovalLead: labels.fileChangeApprovalRequired
        }}
        placeholder={placeholder ?? labels.initialPlaceholder}
        presentationEditorDisabled={disabled}
        presentationSubmitDisabled={
          disabled ||
          composerOptionsLoading ||
          !selectedTargetReady ||
          (draftHasImages && selectedTargetCapabilities?.imageInput !== true)
        }
        projectMissingProbeEnabled={false}
        promptImagesSupported={selectedTargetCapabilities?.imageInput === true}
        provider={provider}
        providerSelectLabel={labels.providerSwitchLabel}
        queuedPrompts={[]}
        selectedAgentTarget={selectedComposerAgentTarget}
        selectProjectDirectory={selectProjectDirectory}
        showProjectSelectorInFooter={Boolean(
          userProjectApi && onProjectPathChange
        )}
        showStopButton={false}
        stopDisabled={true}
        uiLanguage={locale}
        workspaceId={workspaceId}
        workspaceUserProjectI18n={workspaceUserProjectI18n}
        userProjectApi={userProjectApi}
        onDraftContentChange={(nextDraft) => {
          onContentChange(
            agentComposerDraftToPromptContent({
              draft: nextDraft,
              skills: []
            })
          );
        }}
        onEditQueuedPrompt={() => {}}
        onInterruptCurrentTurn={() => {}}
        onProviderSelect={({ agentTargetId }) => {
          if (
            agentTargetId &&
            agentTargets.some(
              (target) =>
                target.agentTargetId === agentTargetId &&
                target.disabled !== true
            )
          ) {
            onAgentTargetChange(agentTargetId);
          }
        }}
        onProjectPathChange={onProjectPathChange}
        onRemoveQueuedPrompt={() => {}}
        onRequestWorkspaceReferences={
          selectedTargetCapabilities?.workspaceReferences === true
            ? onRequestWorkspaceReferences
            : undefined
        }
        onSendQueuedPromptNext={() => {}}
        onSettingsChange={(patch) =>
          settingsCapability?.onChange(pickQuickComposerSettings(patch))
        }
        onSubmit={(nextContent, displayPrompt) => {
          if (
            !selectedTargetReady ||
            !selectedAgentTarget ||
            !selectedTargetCapabilities ||
            (nextContent.some((block) => block.type === "image") &&
              selectedTargetCapabilities.imageInput !== true)
          ) {
            return;
          }
          onSubmit({
            agentTargetId: selectedAgentTarget.agentTargetId,
            content: nextContent,
            displayPrompt:
              displayPrompt ?? agentComposerDraftPrompt(draftContent),
            ...(settingsCapability && Object.keys(settings).length > 0
              ? { settings }
              : {})
          });
        }}
        onSubmitInteractivePrompt={() => false}
      />
    </div>
  );
}
