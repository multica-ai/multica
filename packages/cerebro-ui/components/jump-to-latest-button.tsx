"use client";

// CEREBRO-PATCH(ui-jump-to-latest-button): cerebro modification of upstream file

import React from "react";
import { ArrowUp, ChevronsUp, ChevronDown, MessageSquare } from "lucide-react";
import {
  Tooltip,
  TooltipTrigger,
  TooltipContent,
} from "@multica/ui/components/ui/tooltip";
import { cn } from "@multica/ui/lib/utils";

interface JumpToLatestButtonProps {
  visible: boolean;
  /** 0–100: how far the scroll container is scrolled (for the scroll indicator). */
  scrollPercent?: number;
  /** True when the tabs section (Comments / Agent Runs / Sessions) is in the viewport. */
  isInTabsArea?: boolean;
  onScrollToLatest?: () => void;
  /** Scroll to the tabs section header. Hidden when already in tabs area. */
  onScrollToTabs?: () => void;
  /** Always scroll to page top. Always shown when visible. */
  onScrollToPageTop?: () => void;
  /** Scroll to the top of the current comment thread. Shown only when in tabs area. */
  onScrollToTop?: () => void;
  className?: string;
  /** @deprecated — use onScrollToLatest. Kept for call-site compatibility. */
  onClick?: () => void;
  /** @deprecated — no longer rendered as a text label. */
  label?: string;
}

function NavBtn({
  icon: Icon,
  label,
  onClick,
  primary,
  scrollFill,
  stateClassName,
}: {
  icon: React.ComponentType<{ className?: string }>;
  label: string;
  onClick: () => void;
  primary?: boolean;
  scrollFill?: number;
  stateClassName?: string;
}) {
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <button
            type="button"
            aria-label={label}
            onClick={onClick}
            className={cn(
              "relative overflow-hidden inline-flex items-center justify-center rounded-full transition-colors",
              primary
                ? "h-9 w-9 bg-primary text-primary-foreground hover:bg-primary/90 shadow-md"
                : "h-7 w-7 bg-primary text-primary-foreground opacity-75 hover:opacity-100 shadow-sm",
              stateClassName,
            )}
          >
            {/* Scroll-position indicator: subtle fill from bottom, primary button only. */}
            {primary && scrollFill !== undefined && scrollFill > 0 && scrollFill < 100 && (
              <span
                aria-hidden
                className="pointer-events-none absolute inset-x-0 bottom-0 bg-primary-foreground/15 transition-[height] duration-200"
                style={{ height: `${scrollFill}%` }}
              />
            )}
            <Icon className={cn("relative z-10", primary ? "h-4 w-4" : "h-3.5 w-3.5")} />
          </button>
        }
      />
      <TooltipContent side="left">{label}</TooltipContent>
    </Tooltip>
  );
}

/**
 * Multi-function navigation overlay — redesigned Latest button (JEH-1518).
 * Renders a vertical group of small blue circular buttons:
 *   - Page top (ChevronsUp): always shown when visible
 *   - Thread top (ArrowUp): only shown when in tabs area
 *   - Go to comments (MessageSquare): only shown above tabs section
 *   - Latest (primary, ChevronDown): always the bottom button, with scroll-position fill
 */
export function JumpToLatestButton({
  visible,
  scrollPercent = 0,
  isInTabsArea = false,
  onScrollToLatest,
  onScrollToTabs,
  onScrollToPageTop,
  onScrollToTop,
  className,
  onClick,
  label,
}: JumpToLatestButtonProps) {
  const handleLatest = onScrollToLatest ?? onClick ?? (() => {});
  const latestLabel = label ?? "Latest";
  const stateClassName = visible
    ? "opacity-100 translate-y-0 pointer-events-auto"
    : "opacity-0 translate-y-2 pointer-events-none";

  return (
    <div
      className={cn(
        "absolute top-3 right-3 z-10 flex flex-col items-center gap-1.5 transition-all",
        stateClassName,
        className,
      )}
    >
      {onScrollToPageTop && (
        <NavBtn
          icon={ChevronsUp}
          label="Page top"
          onClick={onScrollToPageTop}
          stateClassName={stateClassName}
        />
      )}
      {onScrollToTop && isInTabsArea && (
        <NavBtn
          icon={ArrowUp}
          label="Thread top"
          onClick={onScrollToTop}
          stateClassName={stateClassName}
        />
      )}
      {onScrollToTabs && !isInTabsArea && (
        <NavBtn
          icon={MessageSquare}
          label="Go to comments"
          onClick={onScrollToTabs}
          stateClassName={stateClassName}
        />
      )}
      <NavBtn
        icon={ChevronDown}
        label={latestLabel}
        onClick={handleLatest}
        primary
        scrollFill={scrollPercent}
        stateClassName={stateClassName}
      />
    </div>
  );
}
