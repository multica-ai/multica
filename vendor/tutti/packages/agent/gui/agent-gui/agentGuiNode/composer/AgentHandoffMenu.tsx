import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger
} from "@tutti-os/ui-system";
import { observer } from "mobx-react-lite";
import { useState, type JSX } from "react";
import { cn } from "../../../app/renderer/lib/utils";
import type { AgentGUIAgentTarget } from "../../../types";
import styles from "../AgentGUINode.styles";
import { AgentGUIOwnerAvatar } from "../AgentGUIOwnerAvatar";
import {
  AgentComposerHandoffIcon,
  HANDOFF_SELECT_IDLE_VALUE,
  resolveComposerProviderTargetIconUrl
} from "./AgentComposerChrome";
import { resolveHandoffTargetOwnershipLabel } from "./handoffTargetPresentation";

export interface AgentHandoffMenuLabels {
  action: string;
  deviceSource?: (deviceLabel: string) => string;
  menu: string;
  self: string;
  shared: string;
  tooltip: string;
}

export interface AgentHandoffMenuProps {
  align?: "center" | "end" | "start";
  contentClassName?: string;
  disabled?: boolean;
  iconOnly?: boolean;
  isolateTriggerEvents?: boolean;
  labels: AgentHandoffMenuLabels;
  onSelect: (target: AgentGUIAgentTarget) => void;
  showOwnershipLabels?: boolean;
  targets: readonly AgentGUIAgentTarget[];
  testId?: string;
  triggerClassName?: string;
  triggerLabel?: string;
}

/**
 * Provider-neutral handoff target menu shared by AgentGUI and host surfaces.
 * The host owns the authoritative target list and launch behavior; this
 * component owns only temporary menu disclosure, presentation, and icon motion.
 */
export const AgentHandoffMenu = observer(function AgentHandoffMenu({
  align = "start",
  contentClassName,
  disabled = false,
  iconOnly = false,
  isolateTriggerEvents = false,
  labels,
  onSelect,
  showOwnershipLabels = false,
  targets,
  testId,
  triggerClassName,
  triggerLabel = labels.action
}: AgentHandoffMenuProps): JSX.Element {
  const [isIconPlaying, setIsIconPlaying] = useState(false);
  const menuDisabled = disabled || targets.length === 0;
  const tooltip = labels.tooltip.trim();

  const trigger = (
    <span className="inline-flex">
      <SelectTrigger
        size="sm"
        aria-label={labels.action}
        data-testid={testId}
        onClick={
          isolateTriggerEvents
            ? (event) => {
                event.stopPropagation();
              }
            : undefined
        }
        onKeyDown={
          isolateTriggerEvents
            ? (event) => {
                event.stopPropagation();
              }
            : undefined
        }
        onPointerDown={
          isolateTriggerEvents
            ? (event) => {
                event.stopPropagation();
              }
            : undefined
        }
        onBlur={() => {
          setIsIconPlaying(false);
        }}
        onFocus={() => {
          setIsIconPlaying(true);
        }}
        onMouseEnter={() => {
          setIsIconPlaying(true);
        }}
        onMouseLeave={() => {
          setIsIconPlaying(false);
        }}
        className={cn(
          styles.composerMenuTrigger,
          styles.composerProviderSelect,
          styles.composerHandoffTrigger,
          "w-auto max-w-[180px] [&>svg:last-child]:hidden",
          triggerClassName
        )}
      >
        <span className="flex min-w-0 items-center gap-1">
          <AgentComposerHandoffIcon
            disabled={menuDisabled}
            isPlaying={isIconPlaying}
          />
          {!iconOnly ? (
            <span className="min-w-0 truncate">{triggerLabel}</span>
          ) : null}
        </span>
      </SelectTrigger>
    </span>
  );

  return (
    <span
      className="contents"
      onClick={
        isolateTriggerEvents
          ? (event) => {
              event.stopPropagation();
            }
          : undefined
      }
      onKeyDown={
        isolateTriggerEvents
          ? (event) => {
              event.stopPropagation();
            }
          : undefined
      }
      onPointerDown={
        isolateTriggerEvents
          ? (event) => {
              event.stopPropagation();
            }
          : undefined
      }
    >
      <Select
        value={HANDOFF_SELECT_IDLE_VALUE}
        disabled={menuDisabled}
        onValueChange={(nextTargetId) => {
          const target = targets.find(
            (candidate) => candidate.targetId === nextTargetId
          );
          if (!target || target.disabled === true) {
            return;
          }
          onSelect(target);
        }}
      >
        {tooltip ? (
          <TooltipProvider delayDuration={120} disableHoverableContent>
            <Tooltip>
              <TooltipTrigger asChild>{trigger}</TooltipTrigger>
              <TooltipContent side="top">{tooltip}</TooltipContent>
            </Tooltip>
          </TooltipProvider>
        ) : (
          trigger
        )}
        <SelectContent
          align={align}
          className={cn(
            styles.composerMenuContent,
            styles.composerHandoffMenuContent,
            "min-w-[190px]",
            contentClassName
          )}
          aria-label={labels.menu}
        >
          {targets.map((target) => {
            const ownershipLabel = showOwnershipLabels
              ? resolveHandoffTargetOwnershipLabel(target, {
                  self: labels.self,
                  shared: labels.shared
                })
              : null;
            const ownerDeviceLabel = target.ownerDeviceLabel?.trim() ?? "";
            const deviceSourceLabel = ownerDeviceLabel
              ? (labels.deviceSource?.(ownerDeviceLabel) ?? ownerDeviceLabel)
              : null;
            return (
              <SelectItem
                key={`${target.provider}:${target.targetId}`}
                value={target.targetId}
                className={cn(styles.composerMenuItem, "gap-2 py-1.5")}
                disabled={target.disabled === true}
              >
                <span className="flex min-w-0 items-center gap-2">
                  <span className="relative size-5 shrink-0">
                    <img
                      alt=""
                      aria-hidden="true"
                      className="size-5 rounded-[4px]"
                      src={resolveComposerProviderTargetIconUrl(target)}
                    />
                    {target.badge?.iconUrl ? (
                      <AgentGUIOwnerAvatar
                        className="absolute -bottom-0.5 -right-0.5 size-2.5 rounded-full border border-[var(--background-fronted)] bg-[var(--background-fronted)] text-[7px] leading-none object-cover"
                        iconUrl={target.badge.iconUrl}
                        label={target.badge.label}
                      />
                    ) : null}
                  </span>
                  <span className="flex min-w-0 flex-col gap-0.5">
                    <span className="min-w-0 truncate">{target.label}</span>
                    {ownershipLabel || deviceSourceLabel ? (
                      <span className="flex min-w-0 items-center gap-1 truncate text-[11px] leading-none text-[var(--agent-gui-text-secondary)]">
                        {ownershipLabel ? <span>{ownershipLabel}</span> : null}
                        {deviceSourceLabel ? (
                          <span>{deviceSourceLabel}</span>
                        ) : null}
                      </span>
                    ) : null}
                  </span>
                </span>
              </SelectItem>
            );
          })}
        </SelectContent>
      </Select>
    </span>
  );
});
