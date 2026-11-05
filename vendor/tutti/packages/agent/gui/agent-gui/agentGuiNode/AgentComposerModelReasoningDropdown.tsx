import { Slot } from "radix-ui";
import { usePersistCallback } from "@multica/ui/hooks/use-persist-callback";
import {
  Fragment,
  cloneElement,
  useState,
  type HTMLAttributes,
  type ReactElement
} from "react";
import { ChevronDown, Star, ZapIcon } from "lucide-react";
import {
  CheckIcon,
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
  RefreshIcon,
  RoomsHintIcon,
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
  cn
} from "@tutti-os/ui-system";
import type { AgentGUIComposerSettingsVM } from "./model/agentGuiNodeTypes";
import {
  buildComposerModelMenuModel,
  type AgentComposerSettingsMenuLabels,
  type ComposerMenuOption
} from "./model/composerSettingsMenuModel";
import { useComposerModelChoiceHistory } from "./controller/useComposerModelChoiceHistory";
import { translate } from "../../i18n/index";
import styles from "./AgentGUINode.styles";
import { observer } from "mobx-react-lite";
export const ComposerOptionInfoTooltip = observer(
  ComposerOptionInfoTooltipRender
);
export const AgentModelReasoningDropdown = observer(
  AgentModelReasoningDropdownRender
);
const ComposerMenuOptionItems = observer(ComposerMenuOptionItemsRender);
const ComposerModelOptionTooltip = observer(ComposerModelOptionTooltipRender);

function ComposerOptionInfoTooltipRender({
  description,
  tooltipsEnabled = true
}: {
  description: string;
  tooltipsEnabled?: boolean;
}): React.JSX.Element {
  const stopSelect = (event: React.SyntheticEvent): void => {
    event.preventDefault();
    event.stopPropagation();
  };
  const stopSelectCallback = usePersistCallback(stopSelect);

  const trigger = (
    <span
      className="pointer-events-none inline-flex shrink-0 cursor-help text-[var(--agent-gui-text-tertiary)] opacity-0 transition-opacity group-hover/composer-option:pointer-events-auto group-hover/composer-option:opacity-100 group-data-[highlighted]/composer-option:pointer-events-auto group-data-[highlighted]/composer-option:opacity-100"
      data-agent-composer-option-info-trigger="true"
      onClick={stopSelectCallback}
      onPointerDown={stopSelectCallback}
    >
      <RoomsHintIcon aria-hidden className="size-3" />
    </span>
  );

  if (!tooltipsEnabled) {
    return trigger;
  }

  return (
    <Tooltip>
      <TooltipTrigger asChild>{trigger}</TooltipTrigger>
      <TooltipContent side="right" className="max-w-[240px] whitespace-normal">
        {description}
      </TooltipContent>
    </Tooltip>
  );
}

function AgentModelReasoningDropdownRender({
  composerSettings,
  disabled = false,
  labels,
  modelHistoryTargetId,
  onRetryComposerOptions,
  onSettingsChange
}: AgentModelReasoningDropdownProps): React.JSX.Element {
  "use memo";
  const [menuOpen, setMenuOpen] = useState(false);
  const [modelSearchQuery, setModelSearchQuery] = useState("");
  const projectedModelHistory = composerSettings.modelChoiceHistory;
  const modelHistory = useComposerModelChoiceHistory({
    targetId:
      modelHistoryTargetId === undefined
        ? (projectedModelHistory?.targetId ?? null)
        : modelHistoryTargetId,
    catalog: projectedModelHistory?.catalog ?? null
  });
  const {
    enabled: modelHistoryEnabled,
    favoriteModelIds,
    recentModelIds
  } = modelHistory;
  const handleMenuOpenChange = (open: boolean): void => {
    if (open) {
      // Opening the picker is the explicit consumer action that is allowed to
      // wait for an authoritative catalog. Background composer loads keep the
      // last successful list and refresh it asynchronously.
      onRetryComposerOptions?.({ waitForFreshModelCatalog: true });
      // Pick up writes from other windows and clear the previous filter.
      modelHistory.refreshFromStorage();
      setModelSearchQuery("");
    }
    setMenuOpen(open);
  };
  const handleMenuOpenChangeCallback = usePersistCallback(handleMenuOpenChange);
  const menu = buildComposerModelMenuModel(composerSettings, labels, {
    favoriteModelIds,
    recentModelIds,
    searchQuery: modelSearchQuery
  });
  const menuDisabled = disabled || menu.disabled;
  // While the model list is still loading the trigger shows a placeholder
  // ("Default") that reads like a real selection. Surface a hover hint so the
  // user knows the list is still loading rather than already resolved.
  const isModelLoading =
    composerSettings.isModelOptionsLoading ||
    composerSettings.isSettingsLoading;
  const composerOptionsError = composerSettings.composerOptionsError === true;
  const retryLabel = labels.retry ?? labels.loadingOptions;
  const optionsLoadFailed = labels.optionsLoadFailed ?? retryLabel;
  const retryTooltip = labels.retryTooltip ?? retryLabel;
  const retryDisabled =
    disabled ||
    composerSettings.composerOptionsLoadStatus === "loading" ||
    !onRetryComposerOptions;
  const triggerDisabled = composerOptionsError ? retryDisabled : menuDisabled;
  const applySettingsChange = (patch: {
    model?: string;
    reasoningEffort?: string;
    speed?: string;
  }): void => {
    onSettingsChange(patch);
    setMenuOpen(false);
  };
  const applySettingsChangeCallback = usePersistCallback(applySettingsChange);
  const applyModelSelection = (value: string): void => {
    modelHistory.recordRecentModel(value);
    applySettingsChange({ model: value });
  };
  const applyModelSelectionCallback = usePersistCallback(applyModelSelection);
  const handleToggleFavoriteModel = (value: string): void => {
    modelHistory.toggleFavoriteModel(value);
  };
  const toggleFavoriteModel = modelHistoryEnabled
    ? handleToggleFavoriteModel
    : undefined;
  const favoriteValueSet = new Set(menu.model.favoriteValues);
  const modelDescriptionPresentation = menu.model.optionDescriptionInline
    ? ("inline" as const)
    : ("model-tooltip" as const);
  const modelListEmpty =
    menu.model.options.length === 0 &&
    menu.model.favoriteOptions.length === 0 &&
    menu.model.recentOptions.length === 0;
  const onClickProp1 = usePersistCallback<
    NonNullable<React.ComponentProps<"button">["onClick"]>
  >(() => onRetryComposerOptions?.());
  const trigger = (
    <button
      type="button"
      className={cn(
        "w-auto",
        styles.composerMenuTrigger,
        triggerDisabled &&
          "cursor-not-allowed text-[var(--agent-gui-text-tertiary)] opacity-60 hover:text-[var(--agent-gui-text-tertiary)]",
        composerOptionsError &&
          !retryDisabled &&
          "!text-[var(--state-warning)] hover:!text-[var(--state-warning)]",
        !composerOptionsError &&
          (composerSettings.isSettingsLoading ||
            composerSettings.isModelOptionsLoading) &&
          "animate-pulse"
      )}
      aria-label={
        composerOptionsError
          ? retryLabel
          : `${labels.modelLabel} / ${labels.reasoningLabel}`
      }
      disabled={triggerDisabled}
      onClick={
        composerOptionsError && !retryDisabled ? onClickProp1 : undefined
      }
      data-agent-model-reasoning-trigger="true"
      data-agent-composer-options-state={
        composerOptionsError ? "error" : undefined
      }
    >
      <span
        className={cn(
          "flex min-w-0 items-center gap-2 overflow-hidden",
          composerOptionsError ? "flex-none" : "flex-1"
        )}
      >
        {composerOptionsError ? (
          <>
            <RefreshIcon
              aria-hidden
              className={cn(
                "size-3.5 shrink-0",
                retryDisabled
                  ? "text-[var(--agent-gui-text-tertiary)]"
                  : "text-[var(--state-warning)]"
              )}
              data-agent-composer-options-retry-icon="true"
            />
            <span className="shrink-0 whitespace-nowrap">
              {optionsLoadFailed}
            </span>
          </>
        ) : (
          <>
            {menu.speed.show && menu.trigger.isFast ? (
              <ZapIcon
                aria-hidden
                className="size-3.5 shrink-0"
                data-agent-speed-indicator="fast"
                strokeWidth={2.5}
              />
            ) : null}
            {menu.trigger.showCombined ? (
              <span className="min-w-0 truncate">
                {menu.trigger.combinedLabel}
              </span>
            ) : (
              <>
                <span className="min-w-0 truncate">
                  {menu.trigger.modelLabel}
                </span>
                <span className="shrink-0">{menu.trigger.reasoningLabel}</span>
              </>
            )}
          </>
        )}
      </span>
      {!composerOptionsError ? (
        <ChevronDown aria-hidden="true" className="shrink-0" size={16} />
      ) : null}
    </button>
  );

  if (composerOptionsError) {
    return (
      <TooltipProvider delayDuration={120}>
        <Tooltip>
          <TooltipTrigger asChild>
            <span
              className="inline-flex"
              tabIndex={retryDisabled ? 0 : undefined}
            >
              {trigger}
            </span>
          </TooltipTrigger>
          <TooltipContent side="top">{retryTooltip}</TooltipContent>
        </Tooltip>
      </TooltipProvider>
    );
  }

  return (
    <DropdownMenu open={menuOpen} onOpenChange={handleMenuOpenChangeCallback}>
      {isModelLoading ? (
        // The trigger is disabled while loading, so pointer events never reach
        // it. Target the tooltip at a focusable wrapper span (Radix's pattern
        // for disabled triggers) so hover/focus reliably surfaces the hint.
        <Tooltip>
          <TooltipTrigger asChild>
            <span className="inline-flex" tabIndex={0}>
              <DropdownMenuTrigger asChild disabled={menuDisabled}>
                {trigger}
              </DropdownMenuTrigger>
            </span>
          </TooltipTrigger>
          <TooltipContent side="top">{labels.loadingOptions}</TooltipContent>
        </Tooltip>
      ) : (
        <DropdownMenuTrigger asChild disabled={menuDisabled}>
          {trigger}
        </DropdownMenuTrigger>
      )}
      <DropdownMenuContent
        align="end"
        side="top"
        sideOffset={4}
        collisionPadding={16}
        className={cn(
          styles.composerMenuContent,
          "w-max min-w-[360px] max-w-[calc(100vw-32px)] data-[side=top]:!translate-y-0"
        )}
        data-agent-composer-settings-layout="model-primary"
      >
        {menu.model.show ? (
          <>
            <DropdownMenuLabel className="flex min-w-0 items-center gap-2">
              <span className="min-w-0 truncate">
                {labels.modelSelectionLabel}
              </span>
              {menu.model.plan ? (
                <span
                  data-agent-model-plan-badge="true"
                  className="min-w-0 shrink truncate rounded-full border border-[var(--line-2)] px-1.5 text-[10px] font-normal leading-4 text-[var(--agent-gui-text-tertiary)]"
                >
                  {translate("agentHost.agentGui.composerModelPlanBadge", {
                    name: menu.model.plan.name
                  })}
                </span>
              ) : null}
            </DropdownMenuLabel>
            {menu.model.searchEnabled ? (
              <DivBinding1
                modelSearchQuery={modelSearchQuery}
                setModelSearchQuery={setModelSearchQuery}
              />
            ) : null}
            {menu.model.favoriteOptions.length > 0 ? (
              <>
                <DropdownMenuLabel
                  data-agent-model-favorites-group="true"
                  className="text-xs text-[var(--agent-gui-text-tertiary)]"
                >
                  {translate("agentHost.agentGui.composerModelFavoritesGroup")}
                </DropdownMenuLabel>
                <ComposerMenuOptionItems
                  options={menu.model.favoriteOptions}
                  selectedValue={menu.model.selectedValue}
                  descriptionPresentation={modelDescriptionPresentation}
                  tooltipsEnabled
                  favoriteValues={favoriteValueSet}
                  onToggleFavorite={toggleFavoriteModel}
                  onSelect={applyModelSelectionCallback}
                />
              </>
            ) : null}
            {menu.model.recentOptions.length > 0 ? (
              <>
                <DropdownMenuLabel
                  data-agent-model-recents-group="true"
                  className="text-xs text-[var(--agent-gui-text-tertiary)]"
                >
                  {translate("agentHost.agentGui.composerModelRecentsGroup")}
                </DropdownMenuLabel>
                <ComposerMenuOptionItems
                  options={menu.model.recentOptions}
                  selectedValue={menu.model.selectedValue}
                  descriptionPresentation={modelDescriptionPresentation}
                  tooltipsEnabled
                  favoriteValues={favoriteValueSet}
                  onToggleFavorite={toggleFavoriteModel}
                  onSelect={applyModelSelectionCallback}
                />
              </>
            ) : null}
            {(menu.model.favoriteOptions.length > 0 ||
              menu.model.recentOptions.length > 0) &&
            menu.model.options.length > 0 ? (
              <DropdownMenuSeparator />
            ) : null}
            {menu.model.groups.length > 0 ? (
              menu.model.groups.map((group, index) => (
                <Fragment key={group.label ?? `ungrouped-${index}`}>
                  {group.label !== null ? (
                    <DropdownMenuLabel className="text-xs text-[var(--agent-gui-text-tertiary)]">
                      {group.label}
                    </DropdownMenuLabel>
                  ) : null}
                  <ComposerMenuOptionItems
                    options={group.options}
                    selectedValue={menu.model.selectedValue}
                    descriptionPresentation={modelDescriptionPresentation}
                    tooltipsEnabled
                    favoriteValues={favoriteValueSet}
                    onToggleFavorite={toggleFavoriteModel}
                    onSelect={applyModelSelectionCallback}
                  />
                </Fragment>
              ))
            ) : (
              <ComposerMenuOptionItems
                options={menu.model.options}
                selectedValue={menu.model.selectedValue}
                descriptionPresentation={modelDescriptionPresentation}
                tooltipsEnabled
                favoriteValues={favoriteValueSet}
                onToggleFavorite={toggleFavoriteModel}
                onSelect={applyModelSelectionCallback}
              />
            )}
            {menu.model.searchEnabled &&
            menu.model.searchQuery &&
            modelListEmpty ? (
              <div
                data-agent-model-search-empty="true"
                className="px-2 py-1.5 text-[12px] text-[var(--agent-gui-text-tertiary)]"
              >
                {translate("agentHost.agentGui.composerModelSearchEmpty")}
              </div>
            ) : null}
          </>
        ) : null}
        {menu.model.show && (menu.reasoning.show || menu.speed.show) ? (
          <DropdownMenuSeparator />
        ) : null}
        {menu.reasoning.show ? (
          <DropdownMenuSub>
            <DropdownMenuSubTrigger
              className={cn(styles.composerMenuItem, "[&>svg]:!ml-0.5")}
              data-agent-reasoning-submenu-trigger="true"
            >
              <span className="min-w-0 flex-1 truncate">
                {labels.reasoningLabel}
              </span>
              <span className="text-[var(--text-tertiary)]">
                {menu.reasoning.selectedLabel}
              </span>
            </DropdownMenuSubTrigger>
            <DropdownMenuSubContent
              className={cn(styles.composerMenuContent, "min-w-[132px]")}
              data-agent-composer-settings-layout="model-submenu"
            >
              <ComposerMenuOptionItemsBinding2
                menu={menu}
                applySettingsChange={applySettingsChangeCallback}
              />
            </DropdownMenuSubContent>
          </DropdownMenuSub>
        ) : null}
        {menu.speed.show ? (
          <DropdownMenuSub>
            <DropdownMenuSubTrigger
              className={cn(styles.composerMenuItem, "[&>svg]:!ml-0.5")}
              data-agent-speed-submenu-trigger="true"
            >
              <span className="min-w-0 flex-1 truncate">
                {labels.speedLabel}
              </span>
              <span className="text-[var(--text-tertiary)]">
                {menu.speed.selectedLabel}
              </span>
            </DropdownMenuSubTrigger>
            <DropdownMenuSubContent
              className={cn(styles.composerMenuContent, "w-[200px]")}
              data-agent-composer-settings-layout="model-submenu"
            >
              <ComposerMenuOptionItemsBinding3
                menu={menu}
                applySettingsChange={applySettingsChangeCallback}
              />
            </DropdownMenuSubContent>
          </DropdownMenuSub>
        ) : null}
        {menu.switchEffectHint ? (
          <>
            <DropdownMenuSeparator />
            <div
              data-agent-model-switch-hint="true"
              className="px-2 py-1.5 text-[11px] text-[var(--agent-gui-text-tertiary)]"
            >
              {translate("agentHost.agentGui.composerModelSwitchNextTurnHint")}
            </div>
          </>
        ) : null}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

// Renders a list of pick-to-apply menu items. Pointer activation applies
// directly because runtime evidence showed submenu items can receive
// pointerdown while Radix onSelect never fires in this embedded menu. onSelect
// remains for keyboard activation and normal Radix paths.
function ComposerMenuOptionItemsRender({
  options,
  selectedValue,
  descriptionPresentation = "none",
  tooltipsEnabled = true,
  favoriteValues,
  onToggleFavorite,
  onSelect
}: ComposerMenuOptionItemsProps): React.JSX.Element {
  return (
    <>
      {options.map((option) => {
        const hasDescription = Boolean(option.description);
        const showInlineDescription =
          descriptionPresentation === "inline" && hasDescription;
        const showModelTooltip = descriptionPresentation === "model-tooltip";
        const showTooltipDescription =
          descriptionPresentation === "tooltip" && hasDescription;
        const isFavorite = favoriteValues?.has(option.value) ?? false;
        const favoriteToggle = onToggleFavorite ? (
          <ButtonBinding4
            isFavorite={isFavorite}
            onToggleFavorite={onToggleFavorite}
            option={option}
          />
        ) : null;
        const item = (
          <DropdownMenuItemBinding5
            key={option.value}
            option={option}
            showModelTooltip={showModelTooltip}
            showInlineDescription={showInlineDescription}
            selectedValue={selectedValue}
            onSelect={onSelect}
            showTooltipDescription={showTooltipDescription}
            tooltipsEnabled={tooltipsEnabled}
            favoriteToggle={favoriteToggle}
          />
        );
        return showModelTooltip ? (
          <ComposerModelOptionTooltip
            key={option.value}
            option={option}
            tooltipsEnabled={tooltipsEnabled}
          >
            {item}
          </ComposerModelOptionTooltip>
        ) : (
          item
        );
      })}
    </>
  );
}

function ComposerModelOptionTooltipRender({
  children,
  option,
  tooltipsEnabled = true
}: {
  children: ReactElement<HTMLAttributes<HTMLElement>>;
  option: ComposerMenuOption;
  tooltipsEnabled?: boolean;
}): React.JSX.Element {
  if (!tooltipsEnabled || !option.tooltip) {
    return children;
  }
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        {cloneElement(children, {
          "data-agent-model-option-tooltip-trigger": "true"
        } as Partial<HTMLAttributes<HTMLElement>> &
          Record<"data-agent-model-option-tooltip-trigger", string>)}
      </TooltipTrigger>
      <TooltipContent
        side="right"
        align="start"
        sideOffset={8}
        className="flex w-[320px] max-w-[calc(100vw-32px)] flex-col items-start gap-0 whitespace-normal rounded-lg border border-[var(--line-2)] bg-[var(--background-fronted)] px-4 py-3 text-[13px] leading-[1.3] text-[var(--text-primary)] shadow-lg"
        data-agent-model-option-tooltip="true"
      >
        <span className="block text-[15px] font-semibold leading-[1.2]">
          {option.tooltip.title}
        </span>
        {option.tooltip.description ? (
          <span className="mt-1.5 block text-[13px] leading-[1.35] text-[var(--text-tertiary)]">
            {option.tooltip.description}
          </span>
        ) : null}
        {option.tooltip.consumptionMultiplier ? (
          <span className="mt-3 flex w-full items-center justify-between gap-6 border-t border-[var(--line-2)] pt-3">
            <span>
              {translate("agentHost.agentGui.modelConsumptionSpeedLabel")}
            </span>
            <span className="shrink-0 tabular-nums">
              {option.tooltip.consumptionMultiplier}{" "}
              {translate("agentHost.agentGui.modelConsumptionMultiplierSuffix")}
            </span>
          </span>
        ) : null}
        {option.tooltip.contextWindow ? (
          <span className="mt-4 block">{option.tooltip.contextWindow}</span>
        ) : null}
        {option.tooltip.version ? (
          <span className="mt-4 block italic">{option.tooltip.version}</span>
        ) : null}
      </TooltipContent>
    </Tooltip>
  );
}

const InputBinding1 = observer(function InputBinding1({
  modelSearchQuery,
  setModelSearchQuery,
  ...slotProps
}: InputBinding1Props &
  Omit<React.ComponentProps<typeof Slot.Root>, keyof InputBinding1Props>) {
  const onChangeProp1 = usePersistCallback<
    NonNullable<React.ComponentProps<"input">["onChange"]>
  >((event) => setModelSearchQuery(event.target.value));
  return (
    <Slot.Root {...slotProps}>
      <input
        type="text"
        value={modelSearchQuery}
        data-agent-model-search-input="true"
        placeholder={translate(
          "agentHost.agentGui.composerModelSearchPlaceholder"
        )}
        className="box-border h-7 w-full min-w-0 rounded-[6px] border border-[var(--line-2)] bg-transparent px-2 text-[12px] text-[var(--text-primary)] outline-none placeholder:text-[var(--agent-gui-text-tertiary)] focus:border-[var(--tutti-purple)]"
        onChange={onChangeProp1}
      />
    </Slot.Root>
  );
});

const ComposerMenuOptionItemsBinding2 = observer(
  function ComposerMenuOptionItemsBinding2({
    menu,
    applySettingsChange
  }: {
    menu: import("./model/composerSettingsMenuModel").ComposerModelMenuModel;
    applySettingsChange: (patch: {
      model?: string;
      reasoningEffort?: string;
      speed?: string;
    }) => void;
  }) {
    const onSelectProp2 = usePersistCallback<
      NonNullable<
        React.ComponentProps<typeof ComposerMenuOptionItems>["onSelect"]
      >
    >((value) => applySettingsChange({ reasoningEffort: value }));
    return (
      <ComposerMenuOptionItems
        options={menu.reasoning.options}
        selectedValue={menu.reasoning.selectedValue}
        tooltipsEnabled
        onSelect={onSelectProp2}
      />
    );
  }
);

const ComposerMenuOptionItemsBinding3 = observer(
  function ComposerMenuOptionItemsBinding3({
    menu,
    applySettingsChange
  }: {
    menu: import("./model/composerSettingsMenuModel").ComposerModelMenuModel;
    applySettingsChange: (patch: {
      model?: string;
      reasoningEffort?: string;
      speed?: string;
    }) => void;
  }) {
    const onSelectProp3 = usePersistCallback<
      NonNullable<
        React.ComponentProps<typeof ComposerMenuOptionItems>["onSelect"]
      >
    >((value) => applySettingsChange({ speed: value }));
    return (
      <ComposerMenuOptionItems
        options={menu.speed.options}
        selectedValue={menu.speed.selectedValue}
        descriptionPresentation="inline"
        tooltipsEnabled
        onSelect={onSelectProp3}
      />
    );
  }
);

const ButtonBinding4 = observer(function ButtonBinding4({
  isFavorite,
  onToggleFavorite,
  option,
  ...slotProps
}: {
  isFavorite: boolean;
  onToggleFavorite: (value: string) => void;
  option: ComposerMenuOption;
} & Omit<
  React.ComponentProps<typeof Slot.Root>,
  keyof {
    isFavorite: boolean;
    onToggleFavorite: (value: string) => void;
    option: ComposerMenuOption;
  }
>) {
  const onPointerUpProp4 = usePersistCallback<
    NonNullable<React.ComponentProps<"button">["onPointerUp"]>
  >((event) => {
    const shouldToggle = event.button === 0 && !event.ctrlKey;
    if (shouldToggle) {
      event.stopPropagation();
      onToggleFavorite(option.value);
    }
  });
  const onClickProp5 = usePersistCallback<
    NonNullable<React.ComponentProps<"button">["onClick"]>
  >((event) => {
    event.preventDefault();
    event.stopPropagation();
    const isKeyboardActivation = event.detail === 0;
    if (isKeyboardActivation) {
      onToggleFavorite(option.value);
    }
  });
  return (
    <Slot.Root {...slotProps}>
      <button
        type="button"
        data-agent-model-favorite-toggle="true"
        data-favorited={isFavorite ? "true" : "false"}
        aria-label={translate(
          isFavorite
            ? "agentHost.agentGui.composerModelFavoriteRemove"
            : "agentHost.agentGui.composerModelFavoriteAdd"
        )}
        className={cn(
          "ml-1 inline-flex size-5 shrink-0 items-center justify-center rounded-[4px] text-[var(--agent-gui-text-tertiary)] transition-opacity hover:text-[var(--text-secondary)]",
          isFavorite
            ? "text-[var(--tutti-purple)] opacity-100 hover:text-[var(--tutti-purple)]"
            : "opacity-0 group-hover/composer-option:opacity-100 group-data-[highlighted]/composer-option:opacity-100"
        )}
        // Pointer activation is applied on pointerup because embedded
        // Radix menu controls do not reliably receive a compatibility
        // click. The parent row ignores this control below.
        onPointerUp={onPointerUpProp4}
        onClick={onClickProp5}
      >
        <Star
          aria-hidden
          className="size-3.5"
          fill={isFavorite ? "currentColor" : "none"}
          strokeWidth={2}
        />
      </button>
    </Slot.Root>
  );
});

const DropdownMenuItemBinding5 = observer(function DropdownMenuItemBinding5({
  option,
  showModelTooltip,
  showInlineDescription,
  selectedValue,
  onSelect,
  showTooltipDescription,
  tooltipsEnabled,
  favoriteToggle,
  ...slotProps
}: {
  option: ComposerMenuOption;
  showModelTooltip: boolean;
  showInlineDescription: boolean;
  selectedValue: NonNullable<ComposerMenuOptionItemsProps>["selectedValue"];
  onSelect: NonNullable<ComposerMenuOptionItemsProps>["onSelect"];
  showTooltipDescription: boolean;
  tooltipsEnabled: boolean;
  favoriteToggle: import("react").JSX.Element | null;
} & Omit<
  React.ComponentProps<typeof Slot.Root>,
  keyof {
    option: ComposerMenuOption;
    showModelTooltip: boolean;
    showInlineDescription: boolean;
    selectedValue: NonNullable<ComposerMenuOptionItemsProps>["selectedValue"];
    onSelect: NonNullable<ComposerMenuOptionItemsProps>["onSelect"];
    showTooltipDescription: boolean;
    tooltipsEnabled: boolean;
    favoriteToggle: import("react").JSX.Element | null;
  }
>) {
  const onPointerDownProp6 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof DropdownMenuItem>["onPointerDown"]>
  >((event) => {
    if (
      event.target instanceof Element &&
      event.target.closest('[data-agent-model-favorite-toggle="true"]')
    ) {
      return;
    }
    if (event.button === 0 && !event.ctrlKey) {
      event.preventDefault();
      onSelect(option.value);
    }
  });
  const onSelectProp7 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof DropdownMenuItem>["onSelect"]>
  >(() => {
    onSelect(option.value);
  });
  return (
    <Slot.Root {...slotProps}>
      <DropdownMenuItem
        key={option.value}
        className={cn(
          styles.composerMenuItem,
          "group/composer-option",
          showModelTooltip && "min-h-[40px] max-w-full items-center px-3 py-2",
          showInlineDescription && "items-start"
        )}
        data-agent-model-option={showModelTooltip ? "true" : undefined}
        data-testid="agent-gui-model-option"
        data-agent-model-selected={
          option.value === selectedValue ? "true" : "false"
        }
        data-agent-model-value={option.value}
        onPointerDown={onPointerDownProp6}
        onSelect={onSelectProp7}
      >
        {showModelTooltip ? (
          <span className="flex min-w-0 flex-1 items-baseline gap-2 overflow-hidden">
            <span className="min-w-0 truncate leading-[1.15]">
              {option.label}
            </span>
            {option.summary && option.summary.length > 0 ? (
              <span className="flex min-w-0 shrink-0 items-baseline gap-1.5 overflow-hidden text-[var(--agent-gui-text-tertiary)]">
                {option.summary.map((summary) => (
                  <span
                    key={summary}
                    className="max-w-[64px] truncate leading-[1.15]"
                  >
                    {summary}
                  </span>
                ))}
              </span>
            ) : null}
          </span>
        ) : (
          <span
            className={cn(
              "flex min-w-0 flex-1 flex-col",
              showInlineDescription ? "gap-0.5" : "gap-0"
            )}
          >
            <span className="flex min-w-0 items-center gap-1.5">
              <span className="min-w-0 truncate leading-[1.15]">
                {option.label}
              </span>
              {showTooltipDescription && option.description ? (
                <ComposerOptionInfoTooltip
                  description={option.description}
                  tooltipsEnabled={tooltipsEnabled}
                />
              ) : null}
            </span>
            {showInlineDescription && option.description ? (
              <span className="whitespace-normal text-[11px] leading-[1.2] text-[var(--text-tertiary)]">
                {option.description}
              </span>
            ) : null}
          </span>
        )}
        {favoriteToggle}
        <CheckIcon
          aria-hidden
          className={cn(
            "ml-2 size-3.5 shrink-0 text-[var(--tutti-purple)]",
            option.value !== selectedValue && "invisible"
          )}
        />
      </DropdownMenuItem>
    </Slot.Root>
  );
});

const DivBinding1 = observer(function DivBinding1({
  modelSearchQuery,
  setModelSearchQuery,
  ...slotProps
}: {
  modelSearchQuery: string;
  setModelSearchQuery: import("react").Dispatch<
    import("react").SetStateAction<string>
  >;
} & Omit<
  React.ComponentProps<typeof Slot.Root>,
  keyof {
    modelSearchQuery: string;
    setModelSearchQuery: import("react").Dispatch<
      import("react").SetStateAction<string>
    >;
  }
>) {
  const onKeyDownProp8 = usePersistCallback<
    NonNullable<React.ComponentProps<"div">["onKeyDown"]>
  >((event) => event.stopPropagation());
  return (
    <Slot.Root {...slotProps}>
      <div
        className="px-2 pb-1.5"
        // Keep typing local to the input: Radix menu typeahead and
        // arrow-key focus handling must not steal these key events.
        onKeyDown={onKeyDownProp8}
      >
        <InputBinding1
          modelSearchQuery={modelSearchQuery}
          setModelSearchQuery={setModelSearchQuery}
        />
      </div>
    </Slot.Root>
  );
});

type AgentModelReasoningDropdownProps = {
  composerSettings: AgentGUIComposerSettingsVM;
  disabled?: boolean;
  labels: AgentComposerSettingsMenuLabels;
  /**
   * Optional test/embedding override. Omission uses the controller-projected
   * exact target; explicit null disables model history.
   */
  modelHistoryTargetId?: string | null;
  onRetryComposerOptions?: (options?: {
    force?: boolean;
    section?: "core" | "capabilities" | "connectors";
    waitForFreshModelCatalog?: boolean;
  }) => void;
  onSettingsChange: (patch: {
    model?: string;
    reasoningEffort?: string;
    speed?: string;
  }) => void;
};

type ComposerMenuOptionItemsProps = {
  options: ComposerMenuOption[];
  selectedValue: string;
  descriptionPresentation?: "inline" | "model-tooltip" | "none" | "tooltip";
  tooltipsEnabled?: boolean;
  /** Values currently favorited; enables the per-option star toggle. */
  favoriteValues?: ReadonlySet<string>;
  onToggleFavorite?: (value: string) => void;
  onSelect: (value: string) => void;
};

type InputBinding1Props = {
  modelSearchQuery: NonNullable<{
    modelSearchQuery: string;
    setModelSearchQuery: import("react").Dispatch<
      import("react").SetStateAction<string>
    >;
  }>["modelSearchQuery"];
  setModelSearchQuery: NonNullable<{
    modelSearchQuery: string;
    setModelSearchQuery: import("react").Dispatch<
      import("react").SetStateAction<string>
    >;
  }>["setModelSearchQuery"];
};

type DropdownMenuItemBinding5Props = {
  option: ComposerMenuOption;
  showModelTooltip: boolean;
  showInlineDescription: boolean;
  selectedValue: string;
  onSelect: (value: string) => void;
  showTooltipDescription: boolean;
  tooltipsEnabled: boolean;
  favoriteToggle: import("react").JSX.Element | null;
};
