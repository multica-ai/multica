"use client";

/**
 * Controls for the top bar of the full-window viewers — the attachment viewer
 * and the code change viewer (MUL-7651). Both bars wear the `dark` token set
 * over a near-black stage whatever the app theme is.
 */

import type { CSSProperties, ReactNode } from "react";
import { Columns2, Rows2, type LucideIcon } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../i18n";

// Desktop window chrome. A viewer covers the whole window, including the top
// bar's drag region, so it declares its own: the viewer is `no-drag` (a drag
// region underneath would otherwise swallow clicks on its controls), its top
// bar drags the window, and the controls in that bar opt back out.
// Chromium-only CSS; browsers ignore it.
export const NO_DRAG = { WebkitAppRegion: "no-drag" } as CSSProperties;
export const DRAG = { WebkitAppRegion: "drag" } as CSSProperties;

// Top-bar icon button. Sits inside the `dark` header, so the semantic tokens
// resolve to the dark set whatever the app theme is.
export function ChromeButton({
  label,
  shortcut,
  pressed,
  disabled,
  onClick,
  children,
}: {
  label: string;
  /** Single-key shortcut, shown in the tooltip and announced to AT. */
  shortcut?: string;
  /** Toggle buttons pass their state; plain actions leave it unset. */
  pressed?: boolean;
  disabled?: boolean;
  onClick: () => void;
  children: ReactNode;
}) {
  return (
    <button
      type="button"
      className={cn(
        "flex size-8 items-center justify-center rounded-md transition-colors enabled:hover:bg-secondary enabled:hover:text-foreground disabled:opacity-40",
        pressed ? "bg-secondary text-foreground" : "text-muted-foreground",
      )}
      title={shortcut ? `${label} (${shortcut})` : label}
      aria-label={label}
      aria-keyshortcuts={shortcut}
      aria-pressed={pressed}
      disabled={disabled}
      onClick={onClick}
    >
      {children}
    </button>
  );
}

// A one-of-several choice in the top bar (viewport width, tree / raw). Same
// pressed look as the toggle buttons beside it. Labels show once the bar has
// room for them; below that the icons carry the choice, named by tooltip.
export function ChromeSegmented<T extends string>({
  label,
  value,
  options,
  disabled,
  onChange,
}: {
  label: string;
  value: T;
  options: ReadonlyArray<{ value: T; label: string; icon: LucideIcon }>;
  disabled?: boolean;
  onChange: (value: T) => void;
}) {
  return (
    <div role="group" aria-label={label} className="flex items-center gap-0.5">
      {options.map((option) => {
        const pressed = option.value === value;
        const Icon = option.icon;
        return (
          <button
            key={option.value}
            type="button"
            className={cn(
              "flex h-8 min-w-8 items-center justify-center gap-1.5 rounded-md px-2 text-label transition-colors enabled:hover:bg-secondary enabled:hover:text-foreground disabled:opacity-40",
              pressed ? "bg-secondary text-foreground" : "text-muted-foreground",
            )}
            title={option.label}
            aria-label={option.label}
            aria-pressed={pressed}
            disabled={disabled}
            onClick={() => onChange(option.value)}
          >
            <Icon className="size-4 shrink-0" />
            <span className="hidden @7xl:inline" aria-hidden>
              {option.label}
            </span>
          </button>
        );
      })}
    </div>
  );
}

export function ChromeDivider() {
  return <span className="mx-1.5 h-4 w-px bg-input" aria-hidden />;
}

export type DiffLayout = "unified" | "split";

/** Unified or side by side, for a diff. */
export function DiffLayoutToggle({
  layout,
  onChange,
}: {
  layout: DiffLayout;
  onChange: (layout: DiffLayout) => void;
}) {
  const { t } = useT("editor");
  return (
    <ChromeSegmented
      label={t(($) => $.diff.layout)}
      value={layout}
      options={[
        { value: "unified", label: t(($) => $.diff.unified), icon: Rows2 },
        { value: "split", label: t(($) => $.diff.split), icon: Columns2 },
      ]}
      onChange={onChange}
    />
  );
}
