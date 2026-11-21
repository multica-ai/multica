import type { CSSProperties, ReactNode } from "react";
import {
  ArrowBigUp,
  ArrowDown,
  ArrowLeft,
  ArrowRight,
  ArrowRightToLine,
  ArrowUp,
  Command,
  CornerDownLeft,
  Delete,
  Option,
  Space,
  type IconComponent,
} from "@multica/ui/icons";
import { Kbd } from "@multica/ui/components/ui/kbd";
import { cn } from "@multica/ui/lib/utils";
import { OpticalKbdGlyph, type OpticalGlyphName } from "./kbd-glyphs";

export type DemoPlatform = "macos" | "windows";
export type DemoVariant = "current" | "quick-win" | "optical-pill" | "optical-split";
export type DemoSize = "sm" | "md";
export type DemoTone = "default" | "on-primary" | "tooltip" | "subtle";

export interface DemoChord {
  key: string;
  modifiers?: {
    primary?: boolean;
    control?: boolean;
    alt?: boolean;
    shift?: boolean;
  };
}

export function chord(
  key: string,
  modifiers: DemoChord["modifiers"] = {},
): DemoChord {
  return { key, modifiers };
}

type CurrentToken = {
  id: string;
  label: string;
  text?: string;
  icon?: IconComponent;
};

const CURRENT_KEY_TOKENS: Record<string, Omit<CurrentToken, "id">> = {
  Enter: { label: "Enter", icon: CornerDownLeft },
  Backspace: { label: "Backspace", icon: Delete },
  Delete: { label: "Delete", icon: Delete },
  Up: { label: "Up Arrow", icon: ArrowUp },
  Down: { label: "Down Arrow", icon: ArrowDown },
  Left: { label: "Left Arrow", icon: ArrowLeft },
  Right: { label: "Right Arrow", icon: ArrowRight },
  Space: { label: "Space", icon: Space },
  Tab: { label: "Tab", icon: ArrowRightToLine },
  Escape: { label: "Esc" },
  Plus: { label: "+" },
  Minus: { label: "−" },
  Equals: { label: "=" },
  Underscore: { label: "_" },
};

/** Exact token builder mirroring packages/views/common/shortcut-keycaps.tsx. */
function buildCurrentTokens(shortcut: DemoChord, platform: DemoPlatform): CurrentToken[] {
  const m = shortcut.modifiers ?? {};
  const tokens: CurrentToken[] = [];
  if (platform === "macos") {
    if (m.primary) tokens.push({ id: "primary", label: "Command", icon: Command });
    if (m.control) tokens.push({ id: "control", label: "Control", text: "⌃" });
    if (m.alt) tokens.push({ id: "alt", label: "Option", icon: Option });
    if (m.shift) tokens.push({ id: "shift", label: "Shift", icon: ArrowBigUp });
  } else {
    if (m.primary) tokens.push({ id: "primary", label: "Ctrl" });
    if (m.control) tokens.push({ id: "control", label: "Control" });
    if (m.alt) tokens.push({ id: "alt", label: "Alt" });
    if (m.shift) tokens.push({ id: "shift", label: "Shift", icon: ArrowBigUp });
  }
  const keyToken = CURRENT_KEY_TOKENS[shortcut.key] ?? { label: shortcut.key };
  tokens.push({ id: `key-${shortcut.key}`, ...keyToken });
  return tokens;
}

type OpticalToken = {
  id: string;
  label: string;
  kind: "glyph" | "text";
  glyph?: OpticalGlyphName;
  text?: string;
  /** Keep legacy 24px icon in quick-win mode except Control & ForwardDelete. */
  legacyIcon?: IconComponent;
};

const QUICK_WIN_KEY_TOKENS: Record<string, Omit<OpticalToken, "id">> = {
  Enter: { label: "Enter", kind: "glyph", legacyIcon: CornerDownLeft },
  Backspace: { label: "Backspace", kind: "glyph", legacyIcon: Delete },
  Delete: { label: "Delete", kind: "glyph", glyph: "forwardDelete" },
  Up: { label: "Up Arrow", kind: "glyph", legacyIcon: ArrowUp },
  Down: { label: "Down Arrow", kind: "glyph", legacyIcon: ArrowDown },
  Left: { label: "Left Arrow", kind: "glyph", legacyIcon: ArrowLeft },
  Right: { label: "Right Arrow", kind: "glyph", legacyIcon: ArrowRight },
  Space: { label: "Space", kind: "text", text: "Space" },
  Tab: { label: "Tab", kind: "glyph", legacyIcon: ArrowRightToLine },
  Escape: { label: "Esc", kind: "text", text: "Esc" },
  Plus: { label: "+", kind: "text", text: "+" },
  Minus: { label: "−", kind: "text", text: "−" },
  Equals: { label: "=", kind: "text", text: "=" },
  Underscore: { label: "_", kind: "text", text: "_" },
};

const OPTICAL_KEY_TOKENS: Record<string, Omit<OpticalToken, "id">> = {
  Enter: { label: "Enter", kind: "glyph", glyph: "enter" },
  Backspace: { label: "Backspace", kind: "glyph", glyph: "backspace" },
  Delete: { label: "Delete", kind: "glyph", glyph: "forwardDelete" },
  Up: { label: "Up Arrow", kind: "glyph", glyph: "arrowUp" },
  Down: { label: "Down Arrow", kind: "glyph", glyph: "arrowDown" },
  Left: { label: "Left Arrow", kind: "glyph", glyph: "arrowLeft" },
  Right: { label: "Right Arrow", kind: "glyph", glyph: "arrowRight" },
  Space: { label: "Space", kind: "text", text: "Space" },
  Tab: { label: "Tab", kind: "glyph", glyph: "tab" },
  Escape: { label: "Esc", kind: "text", text: "Esc" },
  Plus: { label: "+", kind: "text", text: "+" },
  Minus: { label: "−", kind: "text", text: "−" },
  Equals: { label: "=", kind: "text", text: "=" },
  Underscore: { label: "_", kind: "text", text: "_" },
};

/** Canonical Apple HIG modifier order: Control → Option → Shift → Command. */
function buildQuickWinTokens(shortcut: DemoChord, platform: DemoPlatform): OpticalToken[] {
  const m = shortcut.modifiers ?? {};
  const tokens: OpticalToken[] = [];
  if (platform === "macos") {
    if (m.control) tokens.push({ id: "control", label: "Control", kind: "glyph", glyph: "control" });
    if (m.alt) tokens.push({ id: "alt", label: "Option", kind: "glyph", legacyIcon: Option });
    if (m.shift) tokens.push({ id: "shift", label: "Shift", kind: "glyph", legacyIcon: ArrowBigUp });
    if (m.primary) tokens.push({ id: "primary", label: "Command", kind: "glyph", legacyIcon: Command });
  } else {
    if (m.primary) tokens.push({ id: "primary", label: "Ctrl", kind: "text", text: "Ctrl" });
    if (m.control) tokens.push({ id: "control", label: "Control", kind: "text", text: "Control" });
    if (m.alt) tokens.push({ id: "alt", label: "Alt", kind: "text", text: "Alt" });
    if (m.shift) tokens.push({ id: "shift", label: "Shift", kind: "glyph", legacyIcon: ArrowBigUp });
  }
  const keyToken = QUICK_WIN_KEY_TOKENS[shortcut.key] ?? {
    label: shortcut.key,
    kind: "text" as const,
    text: shortcut.key,
  };
  tokens.push({ id: `key-${shortcut.key}`, ...keyToken });
  return tokens;
}

function buildOpticalTokens(shortcut: DemoChord, platform: DemoPlatform): OpticalToken[] {
  const m = shortcut.modifiers ?? {};
  const tokens: OpticalToken[] = [];
  if (platform === "macos") {
    if (m.control) tokens.push({ id: "control", label: "Control", kind: "glyph", glyph: "control" });
    if (m.alt) tokens.push({ id: "alt", label: "Option", kind: "glyph", glyph: "option" });
    if (m.shift) tokens.push({ id: "shift", label: "Shift", kind: "glyph", glyph: "shift" });
    if (m.primary) tokens.push({ id: "primary", label: "Command", kind: "glyph", glyph: "command" });
  } else {
    if (m.primary) tokens.push({ id: "primary", label: "Ctrl", kind: "text", text: "Ctrl" });
    if (m.control) tokens.push({ id: "control", label: "Control", kind: "text", text: "Control" });
    if (m.alt) tokens.push({ id: "alt", label: "Alt", kind: "text", text: "Alt" });
    if (m.shift) tokens.push({ id: "shift", label: "Shift", kind: "glyph", glyph: "shift" });
  }
  const keyToken = OPTICAL_KEY_TOKENS[shortcut.key] ?? {
    label: shortcut.key,
    kind: "text" as const,
    text: shortcut.key,
  };
  tokens.push({ id: `key-${shortcut.key}`, ...keyToken });
  return tokens;
}

const CASE_STYLE: CSSProperties = {
  fontFeatureSettings: '"case" 1',
};

const TONE_CLASSES: Record<DemoTone, string> = {
  default:
    "border border-input bg-card text-muted-foreground shadow-[0_1px_0_0_color-mix(in_oklab,var(--input)_85%,transparent)] dark:border-white/12 dark:bg-surface-raised dark:shadow-[0_1px_0_0_rgb(0_0_0/0.5)]",
  subtle:
    "border border-input/85 bg-muted/70 text-muted-foreground shadow-[0_1px_0_0_color-mix(in_oklab,var(--input)_75%,transparent)]",
  "on-primary":
    "border border-primary-foreground/25 bg-primary-foreground/14 text-primary-foreground shadow-none",
  tooltip:
    "border border-white/14 bg-white/10 text-tooltip-foreground/90 shadow-none",
};

const CURRENT_TONE_CLASSES: Record<DemoTone, string> = {
  default:
    "border border-border/70 bg-muted/80 shadow-[0_1px_0_0_color-mix(in_oklab,var(--border)_70%,transparent)]",
  subtle:
    "border border-border/70 bg-muted/80 shadow-[0_1px_0_0_color-mix(in_oklab,var(--border)_70%,transparent)]",
  "on-primary":
    "border-background/30 bg-background/15 text-primary-foreground shadow-none",
  tooltip:
    "border border-border/70 bg-foreground/12 shadow-[0_1px_0_0_color-mix(in_oklab,var(--border)_70%,transparent)]",
};

/**
 * Optical vertical centering tweaks for punctuation keys that still sit low
 * in proportional sans even with OpenType `case`.
 */
function renderOpticalText(text: string): ReactNode {
  if (text === ",") {
    return <span className="inline-block -translate-y-[0.04em]">{text}</span>;
  }
  return <span>{text}</span>;
}

export function DemoShortcutKeycaps({
  shortcut,
  variant,
  platform = "macos",
  size = "sm",
  tone = "default",
  className,
}: {
  shortcut: DemoChord;
  variant: DemoVariant;
  platform?: DemoPlatform;
  size?: DemoSize;
  tone?: DemoTone;
  className?: string;
}) {
  if (variant === "current") {
    const tokens = buildCurrentTokens(shortcut, platform);
    return (
      <span className={cn("inline-flex items-center gap-1", className)}>
        {tokens.map(({ id, label, text, icon: Icon }) => (
          <kbd
            key={id}
            title={label}
            className={cn(
              "pointer-events-none inline-flex w-fit items-center justify-center gap-1 rounded-sm font-sans font-medium text-muted-foreground select-none",
              CURRENT_TONE_CLASSES[tone],
              size === "md"
                ? "h-7 min-w-7 rounded-md px-1.5 text-caption [&_svg]:size-3.5"
                : "h-5 min-w-5 px-1 text-micro [&_svg]:size-3",
            )}
          >
            {Icon ? <Icon aria-hidden="true" strokeWidth={1.8} /> : (text ?? label)}
          </kbd>
        ))}
      </span>
    );
  }

  if (variant === "quick-win") {
    const tokens = buildQuickWinTokens(shortcut, platform);
    return (
      <span className={cn("inline-flex items-center gap-1", className)}>
        {tokens.map(({ id, label, glyph, legacyIcon: LegacyIcon, text }) => (
          <kbd
            key={id}
            title={label}
            style={CASE_STYLE}
            className={cn(
              "pointer-events-none inline-flex w-fit items-center justify-center font-sans leading-none font-medium select-none",
              TONE_CLASSES[tone],
              size === "md"
                ? "h-[2.15em] min-w-[2.15em] rounded-md px-[0.5em] text-caption"
                : "h-[1.82em] min-w-[1.82em] rounded-sm px-[0.42em] text-micro",
            )}
          >
            {glyph ? (
              <OpticalKbdGlyph name={glyph} />
            ) : LegacyIcon ? (
              <LegacyIcon
                aria-hidden="true"
                strokeWidth={1.9}
                style={{ width: "1.1em", height: "1.1em" }}
              />
            ) : (
              text ?? label
            )}
          </kbd>
        ))}
      </span>
    );
  }

  const tokens = buildOpticalTokens(shortcut, platform);
  const isPill = variant === "optical-pill";

  if (isPill) {
    const isSingleCharOrGlyph =
      tokens.length === 1 &&
      (tokens[0]!.kind === "glyph" || (tokens[0]!.text?.length ?? 0) <= 1);
    return (
      <kbd
        style={CASE_STYLE}
        className={cn(
          "pointer-events-none inline-flex w-fit items-center justify-center font-sans leading-none font-medium tracking-[-0.01em] select-none",
          TONE_CLASSES[tone],
          size === "md"
            ? cn(
                "h-[2.15em] min-w-[2.15em] gap-[0.34em] rounded-md text-caption",
                isSingleCharOrGlyph ? "px-[0.45em]" : "px-[0.62em]",
              )
            : cn(
                "h-[1.85em] min-w-[1.85em] gap-[0.28em] rounded-[5px] text-micro",
                isSingleCharOrGlyph ? "px-[0.36em]" : "px-[0.5em]",
              ),
          className,
        )}
      >
        {tokens.map(({ id, label, glyph, text }) => (
          <span
            key={id}
            title={label}
            className="inline-flex items-center justify-center"
          >
            {glyph ? (
              <OpticalKbdGlyph name={glyph} />
            ) : (
              renderOpticalText(text ?? label)
            )}
          </span>
        ))}
      </kbd>
    );
  }

  // optical-split: bespoke proportional optical glyphs in individual keycaps
  return (
    <span className={cn("inline-flex items-center gap-1", className)}>
      {tokens.map(({ id, label, glyph, text }) => {
        const isWideText = !glyph && (text?.length ?? 0) > 1;
        return (
          <kbd
            key={id}
            title={label}
            style={CASE_STYLE}
            className={cn(
              "pointer-events-none inline-flex w-fit items-center justify-center font-sans leading-none font-medium select-none",
              TONE_CLASSES[tone],
              size === "md"
                ? cn(
                    "h-[2.15em] min-w-[2.15em] rounded-md text-caption",
                    isWideText ? "px-[0.58em]" : "px-[0.4em]",
                  )
                : cn(
                    "h-[1.85em] min-w-[1.85em] rounded-[5px] text-micro",
                    isWideText ? "px-[0.46em]" : "px-[0.32em]",
                  ),
            )}
          >
            {glyph ? (
              <OpticalKbdGlyph name={glyph} />
            ) : (
              renderOpticalText(text ?? label)
            )}
          </kbd>
        );
      })}
    </span>
  );
}

/** Standalone literal keycap (e.g., `>`, `#`, `@`, `Fn`, `Click`) for surface consistency. */
export function DemoLiteralKbd({
  children,
  variant,
  size = "sm",
  tone = "default",
}: {
  children: ReactNode;
  variant: DemoVariant;
  size?: DemoSize;
  tone?: DemoTone;
}) {
  if (variant === "current") {
    return (
      <kbd
        className={cn(
          "pointer-events-none inline-flex w-fit items-center justify-center gap-1 rounded-sm border border-border/70 bg-muted/80 font-sans font-medium text-muted-foreground select-none",
          size === "md"
            ? "h-7 min-w-7 rounded-md px-1.5 text-caption"
            : "h-5 min-w-5 px-1 text-micro",
        )}
      >
        {children}
      </kbd>
    );
  }
  return (
    <Kbd
      size={size}
      tone={tone === "on-primary" ? "on-primary" : "default"}
      compact
      className={tone === "tooltip" ? TONE_CLASSES.tooltip : undefined}
    >
      {children}
    </Kbd>
  );
}
