import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  Switch,
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger
} from "@tutti-os/ui-system";
import { ListChecks, X } from "lucide-react";
import { observer } from "mobx-react-lite";
import { type ReactNode } from "react";
import atLinedIconUrl from "../../../app/renderer/assets/icons/@-lined.svg";
import addLinedIconUrl from "../../../app/renderer/assets/icons/add-lined.svg";
import { cn } from "../../../app/renderer/lib/utils";
import type { AgentGUIAgentTarget } from "../../../types";
import {
  AgentModelReasoningDropdown,
  AgentPermissionModeDropdown
} from "../AgentComposerSettingsMenus";
import styles from "../AgentGUINode.styles";
import { textPromptContent } from "../model/agentComposerDraft";
import type {
  AgentComposerRenderProps as AgentComposerProps,
  AgentComposerUsage
} from "./AgentComposer.types";
import {
  AgentComposerMaskIcon,
  AgentUsageChip,
  composerStyles,
  resolveComposerProviderTargetIconUrl
} from "./AgentComposerChrome";
import { AgentHandoffMenu } from "./AgentHandoffMenu";
import { ComposerPrimaryCapabilityControl } from "./ComposerPrimaryCapabilityControl";

interface Props {
  workspaceId: string;
  labels: AgentComposerProps["labels"];
  provider: AgentComposerProps["provider"];
  composerSettings: AgentComposerProps["composerSettings"];
  usage: AgentComposerUsage | null;
  compactSupported: boolean | null;
  hasCompactableContext: boolean;
  composerControlsHardDisabled: boolean;
  inputDisabled: boolean;
  settingsControlsDisabled: boolean;
  codexSaverModeDisabled: boolean;
  rtkSaverModeDisabled: boolean;
  permissionModeControlsDisabled: boolean;
  isSendingTurn: boolean;
  showComposerAction: boolean;
  isPlanModeActive: boolean;
  connectorsVisible: boolean;
  connectorsReadOnly?: boolean;
  showConnectorViewMore?: boolean;
  composerAction: ReactNode;
  projectControl?: ReactNode;
  quickPromptControl?: ReactNode;
  footerAccessory?: ReactNode;
  showHandoffSelect: boolean;
  handoffDisabled: boolean;
  effectiveHandoffLabel: string;
  effectiveHandoffMenuLabel: string;
  handoffMenuTargets: readonly AgentGUIAgentTarget[];
  onHandoffConversation?: (target: AgentGUIAgentTarget) => void;
  showHandoffTargetOwnershipLabels?: boolean;
  showProviderSelect: boolean;
  selectedProviderSwitchTarget: AgentGUIAgentTarget | null;
  providerSelectDisabled: boolean;
  providerSelectLabel: string;
  selectedProviderLabel: string;
  providerMenuTargets: readonly AgentGUIAgentTarget[];
  menuViewportTopInset?: number;
  onProviderSelect: AgentComposerProps["onProviderSelect"];
  onLinkAction: AgentComposerProps["onLinkAction"];
  availableSkills: AgentComposerProps["availableSkills"];
  selectedConnectorKeys: readonly string[];
  onConnectorSelected: (connectorKey: string, selected: boolean) => void;
  onRetryComposerOptions?: AgentComposerProps["onRetryComposerOptions"];
  onCapabilitySettingsRequest: AgentComposerProps["onCapabilitySettingsRequest"];
  onRequestWorkspaceReferences: AgentComposerProps["onRequestWorkspaceReferences"];
  onWorkspaceReferencePicker: () => void;
  onMentionPaletteButton: () => void;
  onSettingsChange: AgentComposerProps["onSettingsChange"];
  onSubmit: AgentComposerProps["onSubmit"];
  draftPrompt: string;
  onClearPlanMode: () => void;
}

export const ComposerFooter = observer(function ComposerFooter({
  workspaceId: _workspaceId,
  labels,
  provider,
  composerSettings,
  usage,
  compactSupported,
  hasCompactableContext,
  composerControlsHardDisabled,
  inputDisabled,
  settingsControlsDisabled,
  codexSaverModeDisabled,
  rtkSaverModeDisabled,
  permissionModeControlsDisabled,
  isSendingTurn,
  showComposerAction,
  isPlanModeActive,
  connectorsVisible,
  connectorsReadOnly = false,
  showConnectorViewMore = true,
  composerAction,
  projectControl,
  quickPromptControl,
  footerAccessory,
  showHandoffSelect,
  handoffDisabled,
  effectiveHandoffLabel,
  effectiveHandoffMenuLabel,
  handoffMenuTargets,
  onHandoffConversation,
  showHandoffTargetOwnershipLabels = false,
  showProviderSelect,
  selectedProviderSwitchTarget,
  providerSelectDisabled,
  providerSelectLabel,
  selectedProviderLabel,
  providerMenuTargets,
  menuViewportTopInset = 8,
  onProviderSelect,
  onLinkAction,
  availableSkills,
  onRetryComposerOptions,
  selectedConnectorKeys,
  onConnectorSelected,
  onCapabilitySettingsRequest,
  onRequestWorkspaceReferences,
  onWorkspaceReferencePicker: handleWorkspaceReferencePicker,
  onMentionPaletteButton: handleMentionPaletteButton,
  onSettingsChange,
  onSubmit,
  draftPrompt: _draftPrompt,
  onClearPlanMode
}: Props) {
  const showSettingsLoadingPlaceholders = composerSettings.isSettingsLoading;
  return (
    <>
      <div className={styles.composerFooter}>
        <div className={composerStyles.footerGroup}>
          <div className="inline-flex shrink-0 items-center gap-2">
            <TooltipProvider delayDuration={120}>
              <Tooltip>
                <TooltipTrigger asChild>
                  <button
                    type="button"
                    aria-label={labels.addContent}
                    className={cn(
                      styles.composerMenuTrigger,
                      styles.composerReferenceTrigger,
                      "group inline-flex w-auto items-center justify-center text-[var(--agent-gui-text-secondary)] hover:text-[var(--agent-gui-text-primary)] focus-visible:text-[var(--agent-gui-text-primary)] disabled:pointer-events-none disabled:opacity-50"
                    )}
                    data-testid="agent-gui-composer-add-content-trigger"
                    disabled={
                      composerControlsHardDisabled ||
                      !onRequestWorkspaceReferences
                    }
                    onMouseDown={(event) => event.preventDefault()}
                    onClick={() => {
                      void handleWorkspaceReferencePicker();
                    }}
                  >
                    <AgentComposerMaskIcon
                      iconUrl={addLinedIconUrl}
                      marker="reference-add"
                    />
                  </button>
                </TooltipTrigger>
                <TooltipContent side="top">{labels.addContent}</TooltipContent>
              </Tooltip>
            </TooltipProvider>
            <TooltipProvider delayDuration={120}>
              <Tooltip>
                <TooltipTrigger asChild>
                  <button
                    type="button"
                    aria-label={labels.mentionPalette}
                    disabled={composerControlsHardDisabled || inputDisabled}
                    className={cn(
                      styles.composerMenuTrigger,
                      styles.composerReferenceTrigger,
                      "group w-auto justify-center text-[var(--agent-gui-text-secondary)] hover:text-[var(--agent-gui-text-primary)] focus-visible:text-[var(--agent-gui-text-primary)] disabled:pointer-events-none disabled:opacity-50 [&_svg]:shrink-0"
                    )}
                    data-testid="agent-gui-composer-mention-trigger"
                    onMouseDown={(event) => event.preventDefault()}
                    onClick={handleMentionPaletteButton}
                  >
                    <span
                      aria-hidden
                      className="inline-block size-4 bg-[var(--agent-gui-text-secondary)] group-hover:bg-[var(--agent-gui-text-primary)] group-focus-visible:bg-[var(--agent-gui-text-primary)] transition-colors"
                      style={{
                        WebkitMaskImage: `url("${atLinedIconUrl}")`,
                        WebkitMaskPosition: "center",
                        WebkitMaskRepeat: "no-repeat",
                        WebkitMaskSize: "contain",
                        maskImage: `url("${atLinedIconUrl}")`,
                        maskPosition: "center",
                        maskRepeat: "no-repeat",
                        maskSize: "contain"
                      }}
                    />
                  </button>
                </TooltipTrigger>
                <TooltipContent side="top">
                  {labels.mentionPalette}
                </TooltipContent>
              </Tooltip>
            </TooltipProvider>
          </div>
          <ComposerPrimaryCapabilityControl
            availableSkills={availableSkills}
            connectorsVisible={connectorsVisible}
            connectorsReadOnly={connectorsReadOnly}
            disabled={composerControlsHardDisabled}
            labels={labels}
            loading={composerSettings.isConnectorOptionsLoading === true}
            onRetryComposerOptions={onRetryComposerOptions}
            onCapabilitySettingsRequest={onCapabilitySettingsRequest}
            onConnectorSelected={onConnectorSelected}
            selectedConnectorKeys={selectedConnectorKeys}
            showConnectorViewMore={showConnectorViewMore}
          />
          {showHandoffSelect ? (
            <AgentHandoffMenu
              disabled={handoffDisabled}
              labels={{
                action: effectiveHandoffLabel,
                deviceSource: labels.handoffTargetDeviceSource,
                menu: effectiveHandoffMenuLabel,
                self: labels.handoffTargetSelf,
                shared: labels.handoffTargetShared,
                tooltip: labels.handoffConversationTooltip
              }}
              showOwnershipLabels={showHandoffTargetOwnershipLabels}
              targets={handoffMenuTargets}
              triggerLabel={effectiveHandoffLabel}
              onSelect={(target) => {
                onHandoffConversation?.(target);
              }}
            />
          ) : showProviderSelect && selectedProviderSwitchTarget ? (
            <Select
              value={selectedProviderSwitchTarget.targetId}
              disabled={providerSelectDisabled}
              onValueChange={(nextTargetId) => {
                const target = providerMenuTargets.find(
                  (candidate) => candidate.targetId === nextTargetId
                );
                if (!target) {
                  return;
                }
                onProviderSelect?.({
                  provider: target.provider,
                  agentTargetId: target.targetId
                });
              }}
            >
              <SelectTrigger
                size="sm"
                aria-label={providerSelectLabel}
                title={providerSelectLabel}
                className={cn(
                  styles.composerMenuTrigger,
                  styles.composerProviderSelect,
                  "w-auto max-w-[180px]"
                )}
              >
                <span className="flex min-w-0 items-center gap-1">
                  <img
                    alt=""
                    aria-hidden="true"
                    className="size-4 shrink-0 rounded-[4px]"
                    src={resolveComposerProviderTargetIconUrl(
                      selectedProviderSwitchTarget
                    )}
                  />
                  <span className="min-w-0 truncate">
                    {selectedProviderLabel}
                  </span>
                </span>
              </SelectTrigger>
              <SelectContent
                align="start"
                className={cn(styles.composerMenuContent, "min-w-[190px]")}
                collisionPadding={{
                  top: menuViewportTopInset,
                  right: 8,
                  bottom: 8,
                  left: 8
                }}
                side="top"
                sideOffset={6}
              >
                {providerMenuTargets.map((target) => (
                  <SelectItem
                    key={`${target.provider}:${target.targetId}`}
                    value={target.targetId}
                    className={cn(styles.composerMenuItem, "gap-2")}
                  >
                    <span className="flex min-w-0 items-center gap-1">
                      <img
                        alt=""
                        aria-hidden="true"
                        className="size-4 shrink-0 rounded-[4px]"
                        src={resolveComposerProviderTargetIconUrl(target)}
                      />
                      <span className="min-w-0 truncate">{target.label}</span>
                    </span>
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          ) : null}
          {projectControl}
          {quickPromptControl}
          {composerSettings.supportsCodexSaverMode ? (
            <TooltipProvider delayDuration={120}>
              <Tooltip>
                <TooltipTrigger asChild>
                  <div
                    className={cn(
                      styles.composerMenuTrigger,
                      "flex w-auto cursor-pointer items-center gap-2 px-2",
                      codexSaverModeDisabled && "cursor-not-allowed opacity-60"
                    )}
                  >
                    <span className="whitespace-nowrap text-xs">
                      {labels.codexSaverModeLabel}
                    </span>
                    <Switch
                      aria-label={labels.codexSaverModeLabel}
                      checked={
                        composerSettings.draftSettings.codexSaverMode === true
                      }
                      disabled={codexSaverModeDisabled}
                      onCheckedChange={(enabled) =>
                        onSettingsChange({ codexSaverMode: enabled })
                      }
                    />
                  </div>
                </TooltipTrigger>
                <TooltipContent side="top" className="max-w-72">
                  {labels.codexSaverModeDescription}
                </TooltipContent>
              </Tooltip>
            </TooltipProvider>
          ) : null}
          {composerSettings.supportsRTKSaverMode ? (
            <TooltipProvider delayDuration={120}>
              <Tooltip>
                <TooltipTrigger asChild>
                  <div
                    className={cn(
                      styles.composerMenuTrigger,
                      "flex w-auto cursor-pointer items-center gap-2 px-2",
                      rtkSaverModeDisabled && "cursor-not-allowed opacity-60"
                    )}
                  >
                    <span className="whitespace-nowrap text-xs">
                      {labels.rtkSaverModeLabel}
                    </span>
                    <Switch
                      aria-label={labels.rtkSaverModeLabel}
                      checked={
                        composerSettings.draftSettings.rtkSaverMode === true
                      }
                      disabled={rtkSaverModeDisabled}
                      onCheckedChange={(enabled) =>
                        onSettingsChange({ rtkSaverMode: enabled })
                      }
                    />
                  </div>
                </TooltipTrigger>
                <TooltipContent side="top" className="max-w-72">
                  {labels.rtkSaverModeDescription}
                </TooltipContent>
              </Tooltip>
            </TooltipProvider>
          ) : null}
          {composerSettings.supportsPlanMode && isPlanModeActive ? (
            <button
              type="button"
              disabled={settingsControlsDisabled}
              aria-label={labels.planModeLabel}
              title={labels.planModeDescription ?? labels.planModeLabel}
              data-agent-plan-mode-badge="true"
              className={cn(
                styles.composerMenuTrigger,
                "group w-auto",
                "disabled:cursor-not-allowed disabled:opacity-60"
              )}
              onClick={onClearPlanMode}
            >
              <span className="flex min-w-0 items-center gap-1 overflow-hidden">
                <RemovableBadgeIcon
                  icon={<ListChecks className="size-3.5" />}
                />
                <span className="min-w-0 truncate">{labels.planModeLabel}</span>
              </span>
            </button>
          ) : null}
        </div>
        <div className={composerStyles.footerGroupRight}>
          {usage && usage.percentUsed !== null ? (
            <AgentUsageChip
              percentUsed={usage.percentUsed}
              usedTokens={usage.usedTokens}
              totalTokens={usage.totalTokens}
              tooltipsEnabled
              compactSupported={compactSupported ?? false}
              // Only guard against compacting mid-turn: isSendingTurn is
              // the narrow "a turn is actively executing right now"
              // signal. showStopButton alone (e.g. pending approval or
              // interrupting, with isSendingTurn false) must keep this
              // enabled -- that broader gate was the bug fixed by
              // 0e736412 and should not be reintroduced.
              compactDisabled={
                !hasCompactableContext ||
                composerControlsHardDisabled ||
                isSendingTurn
              }
              onCompact={() => onSubmit(textPromptContent("/compact"))}
              labels={{
                usageChipLabel: labels.usageChipLabel,
                usageTooltipLabel: labels.usageTooltipLabel,
                usagePopoverTitle: labels.usagePopoverTitle,
                usageContextWindowLabel: labels.usageContextWindowLabel,
                usageCompactAction: labels.usageCompactAction
              }}
            />
          ) : null}
          {!composerSettings.composerOptionsError &&
          (showSettingsLoadingPlaceholders ||
            composerSettings.supportsPermissionMode) ? (
            <AgentPermissionModeDropdown
              composerSettings={composerSettings}
              disabled={permissionModeControlsDisabled}
              disabledTooltip={
                permissionModeControlsDisabled
                  ? labels.permissionModeChangeUnavailableDuringTurn
                  : undefined
              }
              onLinkAction={onLinkAction}
              provider={provider}
              labels={{
                permissionLabel: labels.permissionLabel,
                loadingOptions: labels.loadingOptions
              }}
              onSettingsChange={(patch) => onSettingsChange(patch)}
            />
          ) : null}
          {showSettingsLoadingPlaceholders ||
          composerSettings.supportsModel ||
          composerSettings.supportsReasoningEffort ||
          composerSettings.composerOptionsError ? (
            <AgentModelReasoningDropdown
              composerSettings={composerSettings}
              disabled={settingsControlsDisabled}
              onRetryComposerOptions={onRetryComposerOptions}
              labels={{
                modelLabel: labels.modelLabel,
                modelSelectionLabel: labels.modelSelectionLabel,
                modelContextWindowSuffix: labels.modelContextWindowSuffix,
                modelTooltipVersionLabel: labels.modelTooltipVersionLabel,
                planModeLabel: labels.planModeLabel,
                reasoningLabel: labels.reasoningLabel,
                reasoningDegreeLabel: labels.reasoningDegreeLabel,
                reasoningOptionDefault: labels.reasoningOptionDefault,
                reasoningOptionMinimal: labels.reasoningOptionMinimal,
                reasoningOptionLow: labels.reasoningOptionLow,
                reasoningOptionMedium: labels.reasoningOptionMedium,
                reasoningOptionHigh: labels.reasoningOptionHigh,
                reasoningOptionXHigh: labels.reasoningOptionXHigh,
                reasoningOptionMax: labels.reasoningOptionMax,
                reasoningOptionUltra: labels.reasoningOptionUltra,
                speedLabel: labels.speedLabel,
                speedSelectionLabel: labels.speedSelectionLabel,
                speedOptionStandard: labels.speedOptionStandard,
                speedOptionStandardDescription:
                  labels.speedOptionStandardDescription,
                speedOptionFast: labels.speedOptionFast,
                speedOptionFastDescription: labels.speedOptionFastDescription,
                permissionLabel: labels.permissionLabel,
                modelDescriptions: labels.modelDescriptions,
                defaultModel: labels.defaultModel,
                loadingOptions: labels.loadingOptions,
                optionsLoadFailed: labels.composerOptionsLoadFailed,
                retry: labels.retry,
                retryTooltip: labels.composerOptionsRetryTooltip,
                inheritedUnavailable: labels.inheritedUnavailable
              }}
              onSettingsChange={onSettingsChange}
            />
          ) : null}
          {showComposerAction ? composerAction : null}
        </div>
        {footerAccessory ? (
          <div className={styles.composerFooterAccessory}>
            {footerAccessory}
          </div>
        ) : null}
      </div>
    </>
  );
});

function RemovableBadgeIcon({ icon }: { icon: ReactNode }) {
  return (
    <span className="relative flex size-3.5 shrink-0 items-center justify-center">
      <span
        aria-hidden
        className="transition-opacity duration-150 group-hover:opacity-0 group-focus-visible:opacity-0"
      >
        {icon}
      </span>
      <span
        aria-hidden
        className="absolute inset-0 flex items-center justify-center rounded-full bg-[var(--text-secondary)] opacity-0 transition-opacity duration-150 group-hover:opacity-100 group-focus-visible:opacity-100 group-disabled:opacity-0"
      >
        <X
          className="size-2.5 text-[var(--background-fronted)]"
          strokeWidth={3}
        />
      </span>
    </span>
  );
}
