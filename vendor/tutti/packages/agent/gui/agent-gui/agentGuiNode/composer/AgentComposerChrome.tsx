import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type FocusEvent as ReactFocusEvent,
  type JSX
} from "react";
import {
  Button,
  Popover,
  PopoverAnchor,
  PopoverContent
} from "@tutti-os/ui-system";
import { cn } from "../../../app/renderer/lib/utils";
import { AgentUsageMeter, agentUsageBarColor } from "../AgentUsageMeter";
import styles from "../AgentGUINode.styles";
import type {
  AgentComposerPromptTip,
  AgentComposerProps
} from "./AgentComposer.types";
import type { AgentGUIProviderSkillOption } from "../model/agentGuiNodeTypes";
import type { AgentMessageMarkdownWorkspaceAppIcon } from "../../../shared/AgentMessageMarkdown";
import { formatSlashStatusTokenCount } from "../AgentSlashStatusPanel";
import type { AgentGUIProvider, AgentGUIAgentTarget } from "../../../types";
import { normalizeManagedAgentProvider } from "../../../shared/managedAgentProviders";
import {
  MANAGED_AGENT_ICON_FALLBACK_URL,
  MANAGED_AGENT_ICON_URLS
} from "../../../shared/managedAgentIcons";
import { resolveAgentGUIProviderCatalogIdentity } from "../../../providerIdentityCatalog";
import { resolveProviderIconAsset } from "../../../providerIconAssets";
import handoffClapAnimationUrl from "../../../app/renderer/assets/animations/handoff-clap.png";
import handoffLinedIconUrl from "../../../app/renderer/assets/icons/handoff-lined.svg";
import {
  USAGE_CRITICAL_PERCENT,
  USAGE_WARN_PERCENT
} from "../model/agentUsageThresholds";

const USAGE_POPOVER_HOVER_DELAY_MS = 120;

type AgentUsageChipLevel = "normal" | "warning" | "critical";

function agentUsageChipLevel(percentUsed: number): AgentUsageChipLevel {
  if (percentUsed >= USAGE_CRITICAL_PERCENT) {
    return "critical";
  }
  if (percentUsed >= USAGE_WARN_PERCENT) {
    return "warning";
  }
  return "normal";
}

function agentUsageRingColor(level: AgentUsageChipLevel): string {
  if (level === "critical") {
    return "var(--state-danger)";
  }
  if (level === "warning") {
    return "var(--state-warning)";
  }
  return "var(--text-secondary)";
}

export function AgentUsageChip({
  percentUsed,
  usedTokens,
  totalTokens,
  labels,
  tooltipsEnabled = true,
  onCompact,
  compactSupported,
  compactDisabled
}: {
  percentUsed: number;
  usedTokens: number | null;
  totalTokens: number | null;
  tooltipsEnabled?: boolean;
  onCompact?: () => void;
  compactSupported?: boolean;
  compactDisabled?: boolean;
  labels: Pick<
    AgentComposerProps["labels"],
    | "usageChipLabel"
    | "usageTooltipLabel"
    | "usagePopoverTitle"
    | "usageContextWindowLabel"
    | "usageCompactAction"
  >;
}): React.JSX.Element {
  "use memo";

  const [usagePopoverOpen, setUsagePopoverOpen] = useState(false);
  const usagePopoverHoverTimerRef = useRef<ReturnType<
    typeof setTimeout
  > | null>(null);
  const usagePopoverContentRef = useRef<HTMLDivElement | null>(null);
  const clampedPercent = Math.max(0, Math.min(100, percentUsed));
  const chipLabel = labels.usageChipLabel({ percent: clampedPercent });
  const showTokens = usedTokens !== null && totalTokens !== null;
  const usageLevel = agentUsageChipLevel(clampedPercent);
  const ringColor = agentUsageRingColor(usageLevel);
  const usagePopoverCloseTimerRef = useRef<ReturnType<
    typeof setTimeout
  > | null>(null);
  const clearUsagePopoverHoverTimer = useCallback(() => {
    if (usagePopoverHoverTimerRef.current) {
      clearTimeout(usagePopoverHoverTimerRef.current);
      usagePopoverHoverTimerRef.current = null;
    }
  }, []);
  const clearUsagePopoverCloseTimer = useCallback(() => {
    if (usagePopoverCloseTimerRef.current) {
      clearTimeout(usagePopoverCloseTimerRef.current);
      usagePopoverCloseTimerRef.current = null;
    }
  }, []);
  const openUsagePopover = useCallback(() => {
    clearUsagePopoverHoverTimer();
    clearUsagePopoverCloseTimer();
    setUsagePopoverOpen(true);
  }, [clearUsagePopoverCloseTimer, clearUsagePopoverHoverTimer]);
  const openUsagePopoverAfterHoverDelay = useCallback(() => {
    clearUsagePopoverHoverTimer();
    clearUsagePopoverCloseTimer();
    // timing: delay opening the usage popover so brief hovers don't trigger it
    usagePopoverHoverTimerRef.current = setTimeout(() => {
      usagePopoverHoverTimerRef.current = null;
      setUsagePopoverOpen(true);
    }, USAGE_POPOVER_HOVER_DELAY_MS);
  }, [clearUsagePopoverCloseTimer, clearUsagePopoverHoverTimer]);
  const closeUsagePopover = useCallback(() => {
    clearUsagePopoverHoverTimer();
    clearUsagePopoverCloseTimer();
    setUsagePopoverOpen(false);
  }, [clearUsagePopoverCloseTimer, clearUsagePopoverHoverTimer]);
  const scheduleUsagePopoverClose = useCallback(() => {
    clearUsagePopoverHoverTimer();
    clearUsagePopoverCloseTimer();
    // timing: delay closing so pointer can move from trigger into popover content
    usagePopoverCloseTimerRef.current = setTimeout(() => {
      usagePopoverCloseTimerRef.current = null;
      setUsagePopoverOpen(false);
    }, 140);
  }, [clearUsagePopoverCloseTimer, clearUsagePopoverHoverTimer]);
  const handleUsagePopoverOpenChange = useCallback(
    (open: boolean) => {
      if (open) {
        openUsagePopover();
        return;
      }
      closeUsagePopover();
    },
    [closeUsagePopover, openUsagePopover]
  );
  const handleUsageTriggerBlur = useCallback(
    (event: ReactFocusEvent<HTMLButtonElement>) => {
      const nextFocusTarget = event.relatedTarget;
      if (
        nextFocusTarget instanceof Node &&
        usagePopoverContentRef.current?.contains(nextFocusTarget)
      ) {
        clearUsagePopoverHoverTimer();
        clearUsagePopoverCloseTimer();
        return;
      }
      closeUsagePopover();
    },
    [
      clearUsagePopoverCloseTimer,
      clearUsagePopoverHoverTimer,
      closeUsagePopover
    ]
  );

  useEffect(
    () => () => {
      clearUsagePopoverHoverTimer();
      clearUsagePopoverCloseTimer();
    },
    [clearUsagePopoverCloseTimer, clearUsagePopoverHoverTimer]
  );
  const trigger = (
    <button
      type="button"
      aria-label={chipLabel}
      className={cn(
        "nodrag relative mr-2 inline-flex size-4 shrink-0 items-center justify-center rounded-full p-0 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[color:color-mix(in_srgb,var(--text-primary)_34%,transparent)] [-webkit-app-region:no-drag]",
        tooltipsEnabled ? "cursor-pointer" : "cursor-default"
      )}
      data-testid="agent-gui-usage-chip"
      data-usage-level={usageLevel}
      onBlur={tooltipsEnabled ? handleUsageTriggerBlur : undefined}
      onFocus={tooltipsEnabled ? openUsagePopoverAfterHoverDelay : undefined}
      onPointerEnter={(event) => {
        if (tooltipsEnabled && event.pointerType !== "touch") {
          openUsagePopoverAfterHoverDelay();
        }
      }}
      onPointerLeave={tooltipsEnabled ? scheduleUsagePopoverClose : undefined}
      title={chipLabel}
      style={{
        background: `conic-gradient(${ringColor} ${clampedPercent}%, color-mix(in srgb, ${ringColor} 16%, transparent) 0)`
      }}
    >
      <span
        aria-hidden="true"
        className="absolute inset-0.5 rounded-full bg-[var(--agent-gui-surface-raised,var(--background-fronted))]"
      />
    </button>
  );

  if (!tooltipsEnabled) {
    return trigger;
  }

  return (
    <Popover
      open={usagePopoverOpen}
      onOpenChange={handleUsagePopoverOpenChange}
    >
      <PopoverAnchor asChild>{trigger}</PopoverAnchor>
      {usagePopoverOpen ? (
        <PopoverContent
          ref={usagePopoverContentRef}
          side="top"
          align="center"
          sideOffset={8}
          collisionPadding={16}
          className="w-[320px] max-w-[calc(100vw-32px)] gap-3 text-xs"
          data-testid="agent-gui-usage-popover"
          onOpenAutoFocus={(event) => event.preventDefault()}
          onPointerEnter={openUsagePopover}
          onPointerLeave={scheduleUsagePopoverClose}
        >
          <div className="flex min-w-0 flex-col gap-3">
            <span className="text-[13px] font-semibold leading-4">
              {labels.usagePopoverTitle}
            </span>
            {showTokens ? (
              <AgentUsageMeter
                label={labels.usageContextWindowLabel}
                value={`${formatSlashStatusTokenCount(usedTokens)} / ${formatSlashStatusTokenCount(totalTokens)} (${clampedPercent}%)`}
                percent={clampedPercent}
                barColor={agentUsageBarColor(clampedPercent)}
                testId="agent-gui-usage-context-meter"
              />
            ) : null}
            {compactSupported && onCompact ? (
              <Button
                type="button"
                data-testid="agent-gui-compact-button"
                disabled={compactDisabled}
                className="nodrag w-full font-medium [-webkit-app-region:no-drag]"
                size="sm"
                variant="secondary"
                onClick={onCompact}
              >
                {labels.usageCompactAction}
              </Button>
            ) : null}
          </div>
        </PopoverContent>
      ) : null}
    </Popover>
  );
}

export const composerStyles = {
  footerGroup: styles.composerFooterLeft,
  footerGroupRight: styles.composerFooterRight,
  dropdownSurface:
    "nodrag isolate rounded-[12px] border border-hairline bg-background-fronted p-[4px] text-foreground shadow-[var(--shadow-panel)] [-webkit-app-region:no-drag]"
};

export const workspaceReferenceSelectValue =
  "__tutti_workspace_reference_idle__";
export const workspaceReferenceOptionValue =
  "__tutti_workspace_reference_add__";
export const composerPaletteZIndex = "var(--z-popover)";
export const SLASH_PALETTE_HEIGHT_PX = 280;
export const MENTION_PALETTE_MIN_HEIGHT_PX = 280;
export const MENTION_PALETTE_MAX_HEIGHT_PX = 320;
export const MENTION_PALETTE_GAP_PX = 8;
export const MENTION_PALETTE_VIEWPORT_PADDING_PX = 8;
export const DRAFT_IMAGE_PREVIEW_BASE_HEIGHT_PX = 72;
export const DRAFT_IMAGE_PREVIEW_MIN_WIDTH_PX = 56;
export const DRAFT_IMAGE_PREVIEW_MAX_WIDTH_PX = 180;
export const DRAFT_IMAGE_PREVIEW_MIN_RATIO = 0.5;
export const DRAFT_IMAGE_PREVIEW_MAX_RATIO = 3;
export const EMPTY_PROMPT_TIPS: readonly AgentComposerPromptTip[] = [];
export const EMPTY_PROVIDER_SKILLS: readonly AgentGUIProviderSkillOption[] = [];
export const EMPTY_WORKSPACE_APP_ICONS: readonly AgentMessageMarkdownWorkspaceAppIcon[] =
  [];
export const MENTION_PALETTE_DISMISS_INTERACTION_SELECTOR = [
  "[data-node-drag-handle]",
  '[data-workbench-drag-handle="true"]',
  ".workspace-node-window__resizer",
  ".workbench-window__resize-handle",
  "#agent-gui-conversation-rail-resize"
].join(",");

export function resolveComposerProviderIconUrl(
  provider: AgentGUIProvider
): string {
  return (
    MANAGED_AGENT_ICON_URLS[normalizeManagedAgentProvider(provider)] ??
    MANAGED_AGENT_ICON_FALLBACK_URL
  );
}

export function resolveComposerProviderTargetIconUrl(
  target: AgentGUIAgentTarget
): string {
  const identity = resolveAgentGUIProviderCatalogIdentity(target.provider);
  return (
    target.iconUrl ??
    (identity ? resolveProviderIconAsset(identity.iconKey, "rounded") : null) ??
    resolveComposerProviderIconUrl(target.provider)
  );
}

export interface MentionPaletteFrame {
  height: number;
  left: number;
  portalTarget: Element;
  top: number;
  width: number;
  zIndex: number | string;
}

export function resolveMentionPaletteZIndex(
  anchor: HTMLElement
): number | string {
  let current: HTMLElement | null = anchor;
  while (current) {
    if (
      current.matches(
        "[data-workbench-window-id], [data-workspace-node-window-root='true']"
      )
    ) {
      const windowZIndex = Number.parseInt(
        window.getComputedStyle(current).zIndex,
        10
      );
      if (Number.isFinite(windowZIndex)) {
        return windowZIndex + 1;
      }
    }
    current = current.parentElement;
  }
  return composerPaletteZIndex;
}

export function AgentComposerMaskIcon({
  iconUrl,
  marker
}: {
  iconUrl: string;
  marker?: "reference-add";
}): JSX.Element {
  return (
    <span
      aria-hidden
      className="inline-block size-4 bg-[var(--text-secondary)] transition-colors group-hover:bg-[var(--text-primary)] group-focus-visible:bg-[var(--text-primary)]"
      data-agent-reference-add-icon={
        marker === "reference-add" ? "true" : undefined
      }
      style={{
        WebkitMaskImage: `url("${iconUrl}")`,
        WebkitMaskPosition: "center",
        WebkitMaskRepeat: "no-repeat",
        WebkitMaskSize: "contain",
        maskImage: `url("${iconUrl}")`,
        maskPosition: "center",
        maskRepeat: "no-repeat",
        maskSize: "contain"
      }}
    />
  );
}

export const HANDOFF_SELECT_IDLE_VALUE = "__agent-handoff-idle__";

function AgentComposerHandoffAnimation(): JSX.Element {
  const [isLoaded, setIsLoaded] = useState(false);

  return (
    <img
      alt=""
      aria-hidden="true"
      className={styles.composerHandoffAnimatedIcon}
      data-active={isLoaded ? "true" : undefined}
      decoding="async"
      draggable={false}
      src={handoffClapAnimationUrl}
      onLoad={() => {
        setIsLoaded(true);
      }}
    />
  );
}

export function AgentComposerHandoffIcon({
  disabled,
  isPlaying
}: {
  disabled: boolean;
  isPlaying: boolean;
}): JSX.Element {
  const shouldPlayAnimation = !disabled && isPlaying;

  return (
    <span
      aria-hidden="true"
      className={styles.composerHandoffIcon}
      data-disabled={disabled ? "true" : undefined}
      data-playing={shouldPlayAnimation ? "true" : undefined}
    >
      <span
        className={styles.composerHandoffStaticIcon}
        style={{
          WebkitMaskImage: `url("${handoffLinedIconUrl}")`,
          WebkitMaskPosition: "center",
          WebkitMaskRepeat: "no-repeat",
          WebkitMaskSize: "contain",
          maskImage: `url("${handoffLinedIconUrl}")`,
          maskPosition: "center",
          maskRepeat: "no-repeat",
          maskSize: "contain"
        }}
      />
      {shouldPlayAnimation ? <AgentComposerHandoffAnimation /> : null}
    </span>
  );
}
