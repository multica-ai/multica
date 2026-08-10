"use client";

// CEREBRO-PATCH(image-phone-controls): FIR-4699 — shared right-click and touch long-press controls.

import type { CSSProperties, ReactElement } from "react";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuGroup,
  ContextMenuItem,
  ContextMenuLabel,
  ContextMenuSeparator,
  ContextMenuTrigger,
} from "@multica/ui/components/ui/context-menu";
import { cn } from "@multica/ui/lib/utils";

type ImageAlign = "left" | "center" | "right";

export interface ImageMenuLabels {
  view: string;
  download: string;
  copyLink: string;
  size: string;
  small: string;
  medium: string;
  fullWidth: string;
  alignLeft: string;
  alignCenter: string;
  alignRight: string;
  moveToBottom: string;
  delete: string;
}

export function inlineImageFigureStyle(widthPct: number | null): CSSProperties {
  if (widthPct == null || !Number.isFinite(widthPct)) return { maxWidth: "100%" };
  return {
    width: `${Math.min(100, Math.max(10, widthPct))}%`,
    maxWidth: "100%",
  };
}

export function CerebroImageContextMenu({
  trigger,
  widthPct,
  align,
  editable,
  disabled = false,
  labels,
  canMoveToBottom,
  onWidthChange,
  onAlignChange,
  onView,
  onDownload,
  onCopyLink,
  onMoveToBottom,
  onDelete,
}: {
  trigger: ReactElement;
  widthPct: number | null;
  align: ImageAlign | null;
  editable: boolean;
  disabled?: boolean;
  labels: ImageMenuLabels;
  canMoveToBottom: boolean;
  onWidthChange: (widthPct: number) => void;
  onAlignChange: (align: ImageAlign | null) => void;
  onView: () => void;
  onDownload: () => void;
  onCopyLink: () => void;
  onMoveToBottom: () => void;
  onDelete: () => void;
}) {
  const targetClass = "min-h-11";
  const widthPresets = [
    { label: labels.small, value: 33 },
    { label: labels.medium, value: 66 },
    { label: labels.fullWidth, value: 100 },
  ] as const;

  return (
    <ContextMenu disabled={disabled}>
      <ContextMenuTrigger render={trigger} />
      <ContextMenuContent className="w-56 max-w-[calc(100vw-1rem)]">
        <ContextMenuItem className={targetClass} onClick={onView}>
          {labels.view}
        </ContextMenuItem>
        <ContextMenuItem className={targetClass} onClick={onDownload}>
          {labels.download}
        </ContextMenuItem>
        <ContextMenuItem className={targetClass} onClick={onCopyLink}>
          {labels.copyLink}
        </ContextMenuItem>
        {editable && (
          <>
            <ContextMenuSeparator />
            <ContextMenuGroup>
              <ContextMenuLabel>{labels.size}</ContextMenuLabel>
              {widthPresets.map(({ label, value }) => {
                const active = widthPct === value;
                return (
                  <ContextMenuItem
                    key={value}
                    className={cn(targetClass, active && "bg-accent")}
                    aria-current={active ? "true" : undefined}
                    onClick={() => onWidthChange(value)}
                  >
                    {label}
                  </ContextMenuItem>
                );
              })}
            </ContextMenuGroup>
            <ContextMenuSeparator />
            {(
              [
                ["left", labels.alignLeft],
                ["center", labels.alignCenter],
                ["right", labels.alignRight],
              ] as const
            ).map(([value, label]) => (
              <ContextMenuItem
                key={value}
                className={cn(targetClass, align === value && "bg-accent")}
                aria-current={align === value ? "true" : undefined}
                onClick={() => onAlignChange(align === value ? null : value)}
              >
                {label}
              </ContextMenuItem>
            ))}
            {canMoveToBottom && (
              <ContextMenuItem className={targetClass} onClick={onMoveToBottom}>
                {labels.moveToBottom}
              </ContextMenuItem>
            )}
            <ContextMenuSeparator />
            <ContextMenuItem
              className={targetClass}
              variant="destructive"
              onClick={onDelete}
            >
              {labels.delete}
            </ContextMenuItem>
          </>
        )}
      </ContextMenuContent>
    </ContextMenu>
  );
}
