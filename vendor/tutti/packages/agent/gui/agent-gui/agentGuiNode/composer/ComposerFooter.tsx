import { useMemo } from "react";
import { usePersistCallback } from "@multica/ui/hooks/use-persist-callback";
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
const RemovableBadgeIcon = observer(RemovableBadgeIconRender);

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
  const onMouseDownProp1 = usePersistCallback<
    NonNullable<React.ComponentProps<"button">["onMouseDown"]>
  >((event) => event.preventDefault());
  const onClickProp2 = usePersistCallback<
    NonNullable<React.ComponentProps<"button">["onClick"]>
  >(() => {
    void handleWorkspaceReferencePicker();
  });
  const onMouseDownProp3 = usePersistCallback<
    NonNullable<React.ComponentProps<"button">["onMouseDown"]>
  >((event) => event.preventDefault());
  const styleProp4Value1 = `url("${atLinedIconUrl}")`;
  const styleProp4Value2 = `url("${atLinedIconUrl}")`;
  const styleProp4 = useMemo<
    NonNullable<React.ComponentProps<"span">["style"]>
  >(
    () => ({
      WebkitMaskImage: styleProp4Value1,
      WebkitMaskPosition: "center",
      WebkitMaskRepeat: "no-repeat",
      WebkitMaskSize: "contain",
      maskImage: styleProp4Value2,
      maskPosition: "center",
      maskRepeat: "no-repeat",
      maskSize: "contain"
    }),
    [styleProp4Value1, styleProp4Value2]
  );
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
                    onMouseDown={onMouseDownProp1}
                    onClick={onClickProp2}
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
                    onMouseDown={onMouseDownProp3}
                    onClick={handleMentionPaletteButton}
                  >
                    <span
                      aria-hidden
                      className="inline-block size-4 bg-[var(--agent-gui-text-secondary)] group-hover:bg-[var(--agent-gui-text-primary)] group-focus-visible:bg-[var(--agent-gui-text-primary)] transition-colors"
                      style={styleProp4}
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
            <AgentHandoffMenuBinding1
              handoffDisabled={handoffDisabled}
              effectiveHandoffLabel={effectiveHandoffLabel}
              labels={labels}
              effectiveHandoffMenuLabel={effectiveHandoffMenuLabel}
              showHandoffTargetOwnershipLabels={
                showHandoffTargetOwnershipLabels
              }
              handoffMenuTargets={handoffMenuTargets}
              onHandoffConversation={onHandoffConversation}
            />
          ) : showProviderSelect && selectedProviderSwitchTarget ? (
            <SelectBinding1
              selectedProviderSwitchTarget={selectedProviderSwitchTarget}
              providerSelectDisabled={providerSelectDisabled}
              providerMenuTargets={providerMenuTargets}
              onProviderSelect={onProviderSelect}
              providerSelectLabel={providerSelectLabel}
              selectedProviderLabel={selectedProviderLabel}
              menuViewportTopInset={menuViewportTopInset}
            />
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
                    <SwitchBinding3
                      labels={labels}
                      composerSettings={composerSettings}
                      codexSaverModeDisabled={codexSaverModeDisabled}
                      onSettingsChange={onSettingsChange}
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
                    <SwitchBinding4
                      labels={labels}
                      composerSettings={composerSettings}
                      rtkSaverModeDisabled={rtkSaverModeDisabled}
                      onSettingsChange={onSettingsChange}
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
            <AgentUsageChipBinding5
              usage={usage}
              compactSupported={compactSupported}
              hasCompactableContext={hasCompactableContext}
              composerControlsHardDisabled={composerControlsHardDisabled}
              isSendingTurn={isSendingTurn}
              onSubmit={onSubmit}
              labels={labels}
            />
          ) : null}
          {!composerSettings.composerOptionsError &&
          (showSettingsLoadingPlaceholders ||
            composerSettings.supportsPermissionMode) ? (
            <AgentPermissionModeDropdownBinding6
              composerSettings={composerSettings}
              permissionModeControlsDisabled={permissionModeControlsDisabled}
              labels={labels}
              onLinkAction={onLinkAction}
              provider={provider}
              onSettingsChange={onSettingsChange}
            />
          ) : null}
          {showSettingsLoadingPlaceholders ||
          composerSettings.supportsModel ||
          composerSettings.supportsReasoningEffort ||
          composerSettings.composerOptionsError ? (
            <AgentModelReasoningDropdownBinding7
              composerSettings={composerSettings}
              settingsControlsDisabled={settingsControlsDisabled}
              onRetryComposerOptions={onRetryComposerOptions}
              labels={labels}
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

function RemovableBadgeIconRender({ icon }: { icon: ReactNode }) {
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

const AgentHandoffMenuBinding1 = observer(function AgentHandoffMenuBinding1({
  handoffDisabled,
  effectiveHandoffLabel,
  labels,
  effectiveHandoffMenuLabel,
  showHandoffTargetOwnershipLabels,
  handoffMenuTargets,
  onHandoffConversation
}: AgentHandoffMenuBinding1Props) {
  const labelsProp5Value1 = labels.handoffTargetDeviceSource;
  const labelsProp5Value2 = labels.handoffTargetSelf;
  const labelsProp5Value3 = labels.handoffTargetShared;
  const labelsProp5Value4 = labels.handoffConversationTooltip;
  const labelsProp5 = useMemo<
    NonNullable<React.ComponentProps<typeof AgentHandoffMenu>["labels"]>
  >(
    () => ({
      action: effectiveHandoffLabel,
      deviceSource: labelsProp5Value1,
      menu: effectiveHandoffMenuLabel,
      self: labelsProp5Value2,
      shared: labelsProp5Value3,
      tooltip: labelsProp5Value4
    }),
    [
      effectiveHandoffLabel,
      labelsProp5Value1,
      effectiveHandoffMenuLabel,
      labelsProp5Value2,
      labelsProp5Value3,
      labelsProp5Value4
    ]
  );
  const onSelectProp6 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof AgentHandoffMenu>["onSelect"]>
  >((target) => {
    onHandoffConversation?.(target);
  });
  return (
    <AgentHandoffMenu
      disabled={handoffDisabled}
      labels={labelsProp5}
      showOwnershipLabels={showHandoffTargetOwnershipLabels}
      targets={handoffMenuTargets}
      triggerLabel={effectiveHandoffLabel}
      onSelect={onSelectProp6}
    />
  );
});

const SelectContentBinding2 = observer(function SelectContentBinding2({
  menuViewportTopInset,
  providerMenuTargets
}: SelectContentBinding2Props) {
  const collisionPaddingProp7 = useMemo<
    NonNullable<React.ComponentProps<typeof SelectContent>["collisionPadding"]>
  >(
    () => ({ top: menuViewportTopInset, right: 8, bottom: 8, left: 8 }),
    [menuViewportTopInset]
  );
  return (
    <SelectContent
      align="start"
      className={cn(styles.composerMenuContent, "min-w-[190px]")}
      collisionPadding={collisionPaddingProp7}
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
              draggable={false}
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
  );
});

const SwitchBinding3 = observer(function SwitchBinding3({
  labels,
  composerSettings,
  codexSaverModeDisabled,
  onSettingsChange
}: SwitchBinding3Props) {
  const onCheckedChangeProp8 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof Switch>["onCheckedChange"]>
  >((enabled) => onSettingsChange({ codexSaverMode: enabled }));
  return (
    <Switch
      aria-label={labels.codexSaverModeLabel}
      checked={composerSettings.draftSettings.codexSaverMode === true}
      disabled={codexSaverModeDisabled}
      onCheckedChange={onCheckedChangeProp8}
    />
  );
});

const SwitchBinding4 = observer(function SwitchBinding4({
  labels,
  composerSettings,
  rtkSaverModeDisabled,
  onSettingsChange
}: SwitchBinding4Props) {
  const onCheckedChangeProp9 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof Switch>["onCheckedChange"]>
  >((enabled) => onSettingsChange({ rtkSaverMode: enabled }));
  return (
    <Switch
      aria-label={labels.rtkSaverModeLabel}
      checked={composerSettings.draftSettings.rtkSaverMode === true}
      disabled={rtkSaverModeDisabled}
      onCheckedChange={onCheckedChangeProp9}
    />
  );
});

const AgentUsageChipBinding5 = observer(function AgentUsageChipBinding5({
  usage,
  compactSupported,
  hasCompactableContext,
  composerControlsHardDisabled,
  isSendingTurn,
  onSubmit,
  labels
}: AgentUsageChipBinding5Props) {
  const onCompactProp10 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof AgentUsageChip>["onCompact"]>
  >(() => onSubmit(textPromptContent("/compact")));
  const labelsProp11Value1 = labels.usageChipLabel;
  const labelsProp11Value2 = labels.usageTooltipLabel;
  const labelsProp11Value3 = labels.usagePopoverTitle;
  const labelsProp11Value4 = labels.usageContextWindowLabel;
  const labelsProp11Value5 = labels.usageCompactAction;
  const labelsProp11 = useMemo<
    NonNullable<React.ComponentProps<typeof AgentUsageChip>["labels"]>
  >(
    () => ({
      usageChipLabel: labelsProp11Value1,
      usageTooltipLabel: labelsProp11Value2,
      usagePopoverTitle: labelsProp11Value3,
      usageContextWindowLabel: labelsProp11Value4,
      usageCompactAction: labelsProp11Value5
    }),
    [
      labelsProp11Value1,
      labelsProp11Value2,
      labelsProp11Value3,
      labelsProp11Value4,
      labelsProp11Value5
    ]
  );
  return (
    <AgentUsageChip
      percentUsed={usage.percentUsed!}
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
        !hasCompactableContext || composerControlsHardDisabled || isSendingTurn
      }
      onCompact={onCompactProp10}
      labels={labelsProp11}
    />
  );
});

const AgentPermissionModeDropdownBinding6 = observer(
  function AgentPermissionModeDropdownBinding6({
    composerSettings,
    permissionModeControlsDisabled,
    labels,
    onLinkAction,
    provider,
    onSettingsChange
  }: AgentPermissionModeDropdownBinding6Props) {
    const labelsProp12Value1 = labels.permissionLabel;
    const labelsProp12Value2 = labels.loadingOptions;
    const labelsProp12 = useMemo<
      NonNullable<
        React.ComponentProps<typeof AgentPermissionModeDropdown>["labels"]
      >
    >(
      () => ({
        permissionLabel: labelsProp12Value1,
        loadingOptions: labelsProp12Value2
      }),
      [labelsProp12Value1, labelsProp12Value2]
    );
    const onSettingsChangeProp13 = usePersistCallback<
      NonNullable<
        React.ComponentProps<
          typeof AgentPermissionModeDropdown
        >["onSettingsChange"]
      >
    >((patch) => onSettingsChange(patch));
    return (
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
        labels={labelsProp12}
        onSettingsChange={onSettingsChangeProp13}
      />
    );
  }
);

const AgentModelReasoningDropdownBinding7 = observer(
  function AgentModelReasoningDropdownBinding7({
    composerSettings,
    settingsControlsDisabled,
    onRetryComposerOptions,
    labels,
    onSettingsChange
  }: AgentModelReasoningDropdownBinding7Props) {
    const labelsProp14Value1 = labels.modelLabel;
    const labelsProp14Value2 = labels.modelSelectionLabel;
    const labelsProp14Value3 = labels.modelContextWindowSuffix;
    const labelsProp14Value4 = labels.modelTooltipVersionLabel;
    const labelsProp14Value5 = labels.planModeLabel;
    const labelsProp14Value6 = labels.reasoningLabel;
    const labelsProp14Value7 = labels.reasoningDegreeLabel;
    const labelsProp14Value8 = labels.reasoningOptionDefault;
    const labelsProp14Value9 = labels.reasoningOptionMinimal;
    const labelsProp14Value10 = labels.reasoningOptionLow;
    const labelsProp14Value11 = labels.reasoningOptionMedium;
    const labelsProp14Value12 = labels.reasoningOptionHigh;
    const labelsProp14Value13 = labels.reasoningOptionXHigh;
    const labelsProp14Value14 = labels.reasoningOptionMax;
    const labelsProp14Value15 = labels.reasoningOptionUltra;
    const labelsProp14Value16 = labels.speedLabel;
    const labelsProp14Value17 = labels.speedSelectionLabel;
    const labelsProp14Value18 = labels.speedOptionStandard;
    const labelsProp14Value19 = labels.speedOptionStandardDescription;
    const labelsProp14Value20 = labels.speedOptionFast;
    const labelsProp14Value21 = labels.speedOptionFastDescription;
    const labelsProp14Value22 = labels.permissionLabel;
    const labelsProp14Value23 = labels.modelDescriptions;
    const labelsProp14Value24 = labels.defaultModel;
    const labelsProp14Value25 = labels.loadingOptions;
    const labelsProp14Value26 = labels.composerOptionsLoadFailed;
    const labelsProp14Value27 = labels.retry;
    const labelsProp14Value28 = labels.composerOptionsRetryTooltip;
    const labelsProp14Value29 = labels.inheritedUnavailable;
    const labelsProp14 = useMemo<
      NonNullable<
        React.ComponentProps<typeof AgentModelReasoningDropdown>["labels"]
      >
    >(
      () => ({
        modelLabel: labelsProp14Value1,
        modelSelectionLabel: labelsProp14Value2,
        modelContextWindowSuffix: labelsProp14Value3,
        modelTooltipVersionLabel: labelsProp14Value4,
        planModeLabel: labelsProp14Value5,
        reasoningLabel: labelsProp14Value6,
        reasoningDegreeLabel: labelsProp14Value7,
        reasoningOptionDefault: labelsProp14Value8,
        reasoningOptionMinimal: labelsProp14Value9,
        reasoningOptionLow: labelsProp14Value10,
        reasoningOptionMedium: labelsProp14Value11,
        reasoningOptionHigh: labelsProp14Value12,
        reasoningOptionXHigh: labelsProp14Value13,
        reasoningOptionMax: labelsProp14Value14,
        reasoningOptionUltra: labelsProp14Value15,
        speedLabel: labelsProp14Value16,
        speedSelectionLabel: labelsProp14Value17,
        speedOptionStandard: labelsProp14Value18,
        speedOptionStandardDescription: labelsProp14Value19,
        speedOptionFast: labelsProp14Value20,
        speedOptionFastDescription: labelsProp14Value21,
        permissionLabel: labelsProp14Value22,
        modelDescriptions: labelsProp14Value23,
        defaultModel: labelsProp14Value24,
        loadingOptions: labelsProp14Value25,
        optionsLoadFailed: labelsProp14Value26,
        retry: labelsProp14Value27,
        retryTooltip: labelsProp14Value28,
        inheritedUnavailable: labelsProp14Value29
      }),
      [
        labelsProp14Value1,
        labelsProp14Value2,
        labelsProp14Value3,
        labelsProp14Value4,
        labelsProp14Value5,
        labelsProp14Value6,
        labelsProp14Value7,
        labelsProp14Value8,
        labelsProp14Value9,
        labelsProp14Value10,
        labelsProp14Value11,
        labelsProp14Value12,
        labelsProp14Value13,
        labelsProp14Value14,
        labelsProp14Value15,
        labelsProp14Value16,
        labelsProp14Value17,
        labelsProp14Value18,
        labelsProp14Value19,
        labelsProp14Value20,
        labelsProp14Value21,
        labelsProp14Value22,
        labelsProp14Value23,
        labelsProp14Value24,
        labelsProp14Value25,
        labelsProp14Value26,
        labelsProp14Value27,
        labelsProp14Value28,
        labelsProp14Value29
      ]
    );
    return (
      <AgentModelReasoningDropdown
        composerSettings={composerSettings}
        disabled={settingsControlsDisabled}
        onRetryComposerOptions={onRetryComposerOptions}
        labels={labelsProp14}
        onSettingsChange={onSettingsChange}
      />
    );
  }
);

const SelectBinding1 = observer(function SelectBinding1({
  selectedProviderSwitchTarget,
  providerSelectDisabled,
  providerMenuTargets,
  onProviderSelect,
  providerSelectLabel,
  selectedProviderLabel,
  menuViewportTopInset
}: SelectBinding1Props) {
  const onValueChangeProp15 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof Select>["onValueChange"]>
  >((nextTargetId) => {
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
  });
  return (
    <Select
      value={selectedProviderSwitchTarget.targetId}
      disabled={providerSelectDisabled}
      onValueChange={onValueChangeProp15}
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
            draggable={false}
            alt=""
            aria-hidden="true"
            className="size-4 shrink-0 rounded-[4px]"
            src={resolveComposerProviderTargetIconUrl(
              selectedProviderSwitchTarget
            )}
          />
          <span className="min-w-0 truncate">{selectedProviderLabel}</span>
        </span>
      </SelectTrigger>
      <SelectContentBinding2
        menuViewportTopInset={menuViewportTopInset}
        providerMenuTargets={providerMenuTargets}
      />
    </Select>
  );
});

type AgentHandoffMenuBinding1Props = {
  handoffDisabled: NonNullable<Props>["handoffDisabled"];
  effectiveHandoffLabel: NonNullable<Props>["effectiveHandoffLabel"];
  labels: NonNullable<Props>["labels"];
  effectiveHandoffMenuLabel: NonNullable<Props>["effectiveHandoffMenuLabel"];
  showHandoffTargetOwnershipLabels: boolean;
  handoffMenuTargets: NonNullable<Props>["handoffMenuTargets"];
  onHandoffConversation: NonNullable<Props>["onHandoffConversation"];
};

type SelectContentBinding2Props = {
  menuViewportTopInset: number;
  providerMenuTargets: readonly AgentGUIAgentTarget[];
};

type SwitchBinding3Props = {
  labels: NonNullable<Props>["labels"];
  composerSettings: NonNullable<Props>["composerSettings"];
  codexSaverModeDisabled: NonNullable<Props>["codexSaverModeDisabled"];
  onSettingsChange: NonNullable<Props>["onSettingsChange"];
};

type SwitchBinding4Props = {
  labels: NonNullable<Props>["labels"];
  composerSettings: NonNullable<Props>["composerSettings"];
  rtkSaverModeDisabled: NonNullable<Props>["rtkSaverModeDisabled"];
  onSettingsChange: NonNullable<Props>["onSettingsChange"];
};

type AgentUsageChipBinding5Props = {
  usage: AgentComposerUsage;
  compactSupported: NonNullable<Props>["compactSupported"];
  hasCompactableContext: NonNullable<Props>["hasCompactableContext"];
  composerControlsHardDisabled: NonNullable<Props>["composerControlsHardDisabled"];
  isSendingTurn: NonNullable<Props>["isSendingTurn"];
  onSubmit: NonNullable<Props>["onSubmit"];
  labels: NonNullable<Props>["labels"];
};

type AgentPermissionModeDropdownBinding6Props = {
  composerSettings: NonNullable<Props>["composerSettings"];
  permissionModeControlsDisabled: NonNullable<Props>["permissionModeControlsDisabled"];
  labels: NonNullable<Props>["labels"];
  onLinkAction: NonNullable<Props>["onLinkAction"];
  provider: NonNullable<Props>["provider"];
  onSettingsChange: NonNullable<Props>["onSettingsChange"];
};

type AgentModelReasoningDropdownBinding7Props = {
  composerSettings: NonNullable<Props>["composerSettings"];
  settingsControlsDisabled: NonNullable<Props>["settingsControlsDisabled"];
  onRetryComposerOptions: NonNullable<Props>["onRetryComposerOptions"];
  labels: NonNullable<Props>["labels"];
  onSettingsChange: NonNullable<Props>["onSettingsChange"];
};

type SelectBinding1Props = {
  selectedProviderSwitchTarget: AgentGUIAgentTarget;
  providerSelectDisabled: NonNullable<Props>["providerSelectDisabled"];
  providerMenuTargets: NonNullable<Props>["providerMenuTargets"];
  onProviderSelect: NonNullable<Props>["onProviderSelect"];
  providerSelectLabel: NonNullable<Props>["providerSelectLabel"];
  selectedProviderLabel: NonNullable<Props>["selectedProviderLabel"];
  menuViewportTopInset: number;
};
