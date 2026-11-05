import { Slot } from "radix-ui";
import { useMemo } from "react";
import { usePersistCallback } from "@multica/ui/hooks/use-persist-callback";
import { isValidElement, useId, useState, type ReactNode } from "react";
import { Gauge, Wrench } from "lucide-react";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from "@tutti-os/ui-system";
import { MoreHorizontalIcon } from "@tutti-os/ui-system/icons";
import { AgentProbeUsageFreshness } from "../AgentProbeUsageFreshness";
import { AgentUsageMeter } from "../AgentUsageMeter";
import { SettingsLinedIcon } from "../../../app/renderer/components/icons/SettingsLinedIcon";
import { resolveAgentGuiSessionProviderFlatIconUrl } from "../../../agentGuiSessionProviderIconUrls";
import styles from "../AgentGUINode.styles";
import type { AgentComposerSlashStatusLimit } from "../AgentComposer";
import type { AgentGUIViewLabels } from "../AgentGUINodeView";
import { observer } from "mobx-react-lite";
export const AgentGUIConfigMenu = observer(AgentGUIConfigMenuRender);

interface AgentGUIConfigMenuProps {
  accountContent?: ReactNode;
  systemActionsContent?: ReactNode;
  environmentSetupVisible: boolean;
  labels: AgentGUIViewLabels;
  providerScopedActionsVisible: boolean;
  slashStatusLimits: readonly AgentComposerSlashStatusLimit[];
  slashStatusLimitsLoading: boolean;
  slashStatusLimitsResolvedEmpty: boolean;
  slashStatusUsageCapturedAtUnixMs: number | null;
  slashStatusUsageDidFail: boolean;
  slashStatusUsageErrorMessage?: string | null;
  slashStatusUsageAttempted: boolean;
  provider?: string | null;
  providerIconUrl?: string | null;
  providerMaskIconUrl?: string | null;
  providerLabel?: string | null;
  providerAuthAccountLabel?: string | null;
  onAgentConfigMenuOpen?: () => void;
  onAgentConfigMenuClose?: () => void;
  onAgentUsageRefresh?: () => void;
  usageRefreshInline?: boolean;
  onOpenAgentEnvSetup: () => void;
  onOpenAgentSettings: () => void;
}

export const AgentGUIConfigAccountFallbackSuppressed = observer(
  function AgentGUIConfigAccountFallbackSuppressed(): null {
    return null;
  }
);

function AgentGUIConfigMenuRender({
  accountContent,
  systemActionsContent,
  environmentSetupVisible,
  labels,
  providerScopedActionsVisible,
  slashStatusLimits,
  slashStatusLimitsLoading,
  slashStatusLimitsResolvedEmpty,
  slashStatusUsageCapturedAtUnixMs,
  slashStatusUsageDidFail,
  slashStatusUsageErrorMessage,
  slashStatusUsageAttempted,
  provider,
  providerIconUrl,
  providerMaskIconUrl: targetMaskIconUrl,
  providerLabel,
  providerAuthAccountLabel,
  onAgentConfigMenuOpen,
  onAgentConfigMenuClose,
  onAgentUsageRefresh,
  usageRefreshInline = false,
  onOpenAgentEnvSetup,
  onOpenAgentSettings
}: AgentGUIConfigMenuProps): React.JSX.Element {
  const [open, setOpen] = useState(false);
  const usageDescriptionId = useId();
  const builtInMaskIconUrl = resolveAgentGuiSessionProviderFlatIconUrl(
    provider ?? undefined
  );
  const providerImageIconUrl = builtInMaskIconUrl
    ? null
    : (providerIconUrl?.trim() ?? null);
  const providerMaskIconUrl =
    builtInMaskIconUrl ??
    (providerImageIconUrl ? null : (targetMaskIconUrl?.trim() ?? null));
  const providerDisplayName = providerLabel?.trim() || provider?.trim();
  const providerDisplayTitle = providerDisplayName
    ? labels.slashStatusProviderAccount(providerDisplayName)
    : null;
  const accountTitle = providerDisplayTitle ?? labels.slashStatusAccount;
  const accountFallbackSuppressed =
    isValidElement(accountContent) &&
    accountContent.type === AgentGUIConfigAccountFallbackSuppressed;
  const hasAccountContent =
    !accountFallbackSuppressed &&
    Boolean(accountContent) &&
    typeof accountContent !== "boolean";
  const providerAccountFallbackVisible =
    !accountFallbackSuppressed && !hasAccountContent;
  const labelsProp1Value1 = labels.slashStatusUsageJustUpdated;
  const labelsProp1Value2 = labels.slashStatusUsageMinutesAgo;
  const labelsProp1Value3 = labels.slashStatusUsageHoursAgo;
  const labelsProp1Value4 = labels.slashStatusUsageUpdating;
  const labelsProp1Value5 = labels.slashStatusUsageRefreshFailed;
  const labelsProp1Value6 = labels.slashStatusUsageRefreshAria;
  const labelsProp1 = useMemo<
    NonNullable<React.ComponentProps<typeof AgentProbeUsageFreshness>["labels"]>
  >(
    () => ({
      justUpdated: labelsProp1Value1,
      minutesAgo: labelsProp1Value2,
      hoursAgo: labelsProp1Value3,
      updating: labelsProp1Value4,
      refreshFailed: labelsProp1Value5,
      refreshAria: labelsProp1Value6
    }),
    [
      labelsProp1Value1,
      labelsProp1Value2,
      labelsProp1Value3,
      labelsProp1Value4,
      labelsProp1Value5,
      labelsProp1Value6
    ]
  );
  const onRefreshProp2 = usePersistCallback<
    NonNullable<
      React.ComponentProps<typeof AgentProbeUsageFreshness>["onRefresh"]
    >
  >(() => onAgentUsageRefresh?.());
  const usageFreshness = (
    <AgentProbeUsageFreshness
      ariaDescribedBy={usageDescriptionId}
      capturedAtUnixMs={slashStatusUsageCapturedAtUnixMs}
      didFail={slashStatusUsageDidFail}
      disabled={!onAgentUsageRefresh}
      isLoading={slashStatusLimitsLoading}
      labels={labelsProp1}
      onRefresh={onRefreshProp2}
      presentation={usageRefreshInline ? "button" : "menu-item"}
      testId="agent-gui-config-usage-refresh"
    />
  );
  const onOpenChangeProp3 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof DropdownMenu>["onOpenChange"]>
  >((nextOpen) => {
    setOpen(nextOpen);
    // Refresh the underlying probe on open, the same way the window-title
    // info tooltip does; otherwise a stale/empty fetch can sit here until
    // something unrelated refreshes it.
    if (nextOpen) {
      onAgentConfigMenuOpen?.();
    } else {
      onAgentConfigMenuClose?.();
    }
  });

  const styleProp4 = useMemo<
    NonNullable<React.ComponentProps<typeof DropdownMenuContent>["style"]>
  >(() => ({ zIndex: "var(--z-panel-popover)" }), []);
  const onSelectProp5 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof DropdownMenuItem>["onSelect"]>
  >(() => onOpenAgentSettings());
  return (
    <DropdownMenu open={open} onOpenChange={onOpenChangeProp3}>
      <DropdownMenuTrigger asChild>
        <button
          type="button"
          aria-label={labels.agentConfig}
          className={`${styles.providerRailConfigButton} nodrag tsh-desktop-no-drag`}
          title={labels.agentConfig}
        >
          <MoreHorizontalIcon aria-hidden="true" width={18} height={18} />
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent
        side="right"
        align="end"
        className="nodrag w-[300px] max-w-[calc(100vw-32px)] p-1 text-xs [-webkit-app-region:no-drag]"
        data-testid="agent-gui-config-menu"
        style={styleProp4}
      >
        <DropdownMenuGroup className="min-w-0">
          {hasAccountContent ? (
            <>
              {accountContent}
              <DropdownMenuSeparator />
            </>
          ) : null}
          {providerAccountFallbackVisible &&
          providerScopedActionsVisible &&
          providerAuthAccountLabel ? (
            <>
              <DropdownMenuLabel className="flex min-w-0 flex-col gap-2 p-2 text-[var(--text-primary)]">
                <div className="flex min-w-0 items-center gap-2">
                  {providerMaskIconUrl ? (
                    <SpanBinding1 providerMaskIconUrl={providerMaskIconUrl} />
                  ) : providerImageIconUrl ? (
                    <img
                      alt=""
                      aria-hidden="true"
                      className="size-4 shrink-0 object-contain"
                      src={providerImageIconUrl}
                    />
                  ) : null}
                  <span className="text-[13px] font-semibold leading-4">
                    {accountTitle}
                  </span>
                </div>
                <span className="text-[13px] leading-5 text-[var(--text-secondary)]">
                  {providerAuthAccountLabel}
                </span>
              </DropdownMenuLabel>
              {slashStatusLimits.length > 0 ||
              slashStatusUsageAttempted ||
              slashStatusLimitsLoading ? (
                <DropdownMenuSeparator />
              ) : null}
            </>
          ) : null}
          {providerAccountFallbackVisible &&
          providerScopedActionsVisible &&
          (slashStatusLimits.length > 0 ||
            slashStatusUsageAttempted ||
            slashStatusLimitsLoading) ? (
            <>
              <DropdownMenuLabel
                className="flex min-w-0 flex-col gap-2 p-2 text-[var(--text-primary)]"
                id={usageRefreshInline ? undefined : usageDescriptionId}
              >
                <div className="flex min-w-0 items-center justify-between gap-2">
                  <div
                    className="flex min-w-0 items-center gap-2"
                    id={usageRefreshInline ? usageDescriptionId : undefined}
                  >
                    <Gauge
                      aria-hidden="true"
                      className="shrink-0"
                      size={16}
                      strokeWidth={1.8}
                    />
                    <span className="shrink-0 text-[13px] font-semibold leading-4">
                      {labels.slashStatusLimits}
                    </span>
                    {slashStatusLimits.length === 0 &&
                    !slashStatusLimitsLoading &&
                    !slashStatusUsageErrorMessage ? (
                      <span
                        className="min-w-0 truncate text-[var(--text-tertiary)]"
                        data-testid="agent-gui-config-usage-unavailable"
                      >
                        {slashStatusLimitsResolvedEmpty
                          ? labels.slashStatusEmptyValue
                          : labels.slashStatusLimitsUnavailable}
                      </span>
                    ) : null}
                  </div>
                  {usageRefreshInline ? usageFreshness : null}
                </div>
                {slashStatusLimits.length > 0
                  ? slashStatusLimits.map((limit) => (
                      <AgentUsageMeter
                        key={limit.id}
                        label={limit.label}
                        value={`${limit.value}${limit.reset ? ` (${limit.reset})` : ""}`}
                        percent={
                          typeof limit.percentRemaining === "number" &&
                          Number.isFinite(limit.percentRemaining)
                            ? limit.percentRemaining
                            : null
                        }
                      />
                    ))
                  : null}
                {slashStatusUsageErrorMessage ? (
                  <div
                    className="rounded-[6px] border border-[var(--on-danger-hover)] bg-[var(--on-danger)] px-2 py-1.5 text-[12px] leading-4 text-[var(--state-danger)]"
                    data-testid="agent-gui-config-usage-error"
                    role="alert"
                  >
                    {slashStatusUsageErrorMessage}
                  </div>
                ) : null}
              </DropdownMenuLabel>
              {usageRefreshInline ? null : usageFreshness}
              <DropdownMenuSeparator />
            </>
          ) : null}
          <DropdownMenuGroup>
            {providerScopedActionsVisible && environmentSetupVisible ? (
              <DropdownMenuItemBinding2
                onOpenAgentEnvSetup={onOpenAgentEnvSetup}
                labels={labels}
              />
            ) : null}
            <DropdownMenuItem
              data-testid="agent-gui-config-settings"
              className="h-7"
              onSelect={onSelectProp5}
            >
              <SettingsLinedIcon aria-hidden="true" width={16} height={16} />
              <span>{labels.agentSettingsMenu}</span>
            </DropdownMenuItem>
          </DropdownMenuGroup>
          {systemActionsContent ? (
            <>
              <DropdownMenuSeparator />
              {systemActionsContent}
            </>
          ) : null}
        </DropdownMenuGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

const SpanBinding1 = observer(function SpanBinding1({
  providerMaskIconUrl,
  ...slotProps
}: {
  providerMaskIconUrl: string;
} & Omit<
  React.ComponentProps<typeof Slot.Root>,
  keyof {
    providerMaskIconUrl: string;
  }
>) {
  const styleProp6Value1 = `url("${providerMaskIconUrl}") center / contain no-repeat`;
  const styleProp6Value2 = `url("${providerMaskIconUrl}") center / contain no-repeat`;
  const styleProp6 = useMemo<
    NonNullable<React.ComponentProps<"span">["style"]>
  >(
    () => ({ mask: styleProp6Value1, WebkitMask: styleProp6Value2 }),
    [styleProp6Value1, styleProp6Value2]
  );
  return (
    <Slot.Root {...slotProps}>
      <span
        aria-hidden="true"
        className="size-4 shrink-0 bg-current"
        style={styleProp6}
      />
    </Slot.Root>
  );
});

const DropdownMenuItemBinding2 = observer(function DropdownMenuItemBinding2({
  onOpenAgentEnvSetup,
  labels,
  ...slotProps
}: {
  onOpenAgentEnvSetup: () => void;
  labels: AgentGUIViewLabels;
} & Omit<
  React.ComponentProps<typeof Slot.Root>,
  keyof {
    onOpenAgentEnvSetup: () => void;
    labels: AgentGUIViewLabels;
  }
>) {
  const onSelectProp7 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof DropdownMenuItem>["onSelect"]>
  >(() => onOpenAgentEnvSetup());
  return (
    <Slot.Root {...slotProps}>
      <DropdownMenuItem
        data-testid="agent-gui-config-env-setup"
        className="h-7"
        onSelect={onSelectProp7}
      >
        <Wrench aria-hidden="true" size={16} strokeWidth={1.8} />
        <span>{labels.agentEnvSetup}</span>
      </DropdownMenuItem>
    </Slot.Root>
  );
});
