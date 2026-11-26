"use client";

import {
  formatShortcut,
  getShortcutPlatform,
  shortcutFirstStep,
  useShortcut,
  type ShortcutActionId,
  type ShortcutChord,
  type ShortcutPlatform,
} from "@multica/core/shortcuts";
import {
  Kbd,
  KbdGlyph,
  type KbdGlyphName,
  type KbdSize,
} from "@multica/ui/components/ui/kbd";
import { cn } from "@multica/ui/lib/utils";

type ShortcutToken = {
  id: string;
  label: string;
  text?: string;
  glyph?: KbdGlyphName;
};

const KEY_TOKENS: Record<string, Omit<ShortcutToken, "id">> = {
  Enter: { label: "Enter", glyph: "enter" },
  Backspace: { label: "Backspace", glyph: "backspace" },
  Delete: { label: "Delete", glyph: "forwardDelete" },
  Up: { label: "Up Arrow", glyph: "arrowUp" },
  Down: { label: "Down Arrow", glyph: "arrowDown" },
  Left: { label: "Left Arrow", glyph: "arrowLeft" },
  Right: { label: "Right Arrow", glyph: "arrowRight" },
  Space: { label: "Space", text: "Space" },
  Tab: { label: "Tab", glyph: "tab" },
  Escape: { label: "Esc", text: "Esc" },
  Plus: { label: "+", text: "+" },
  Minus: { label: "−", text: "−" },
  Equals: { label: "=", text: "=" },
  Underscore: { label: "_", text: "_" },
};

/**
 * Resolve display tokens for a chord. macOS follows Apple HIG canonical
 * modifier order: Control → Option → Shift → Command → Key.
 */
function shortcutTokens(
  shortcut: ShortcutChord,
  platform: ShortcutPlatform,
): ShortcutToken[] {
  const { modifiers } = shortcut;
  const tokens: ShortcutToken[] = [];

  if (platform === "macos") {
    if (modifiers.control) tokens.push({ id: "control", label: "Control", glyph: "control" });
    if (modifiers.alt) tokens.push({ id: "alt", label: "Option", glyph: "option" });
    if (modifiers.shift) tokens.push({ id: "shift", label: "Shift", glyph: "shift" });
    if (modifiers.primary) tokens.push({ id: "primary", label: "Command", glyph: "command" });
    if (modifiers.meta) tokens.push({ id: "meta", label: "Meta", text: "Meta" });
  } else {
    if (modifiers.primary) tokens.push({ id: "primary", label: "Ctrl", text: "Ctrl" });
    if (modifiers.control) tokens.push({ id: "control", label: "Control", text: "Control" });
    if (modifiers.meta) {
      const text = platform === "windows" ? "Win" : "Super";
      tokens.push({ id: "meta", label: text, text });
    }
    if (modifiers.alt) tokens.push({ id: "alt", label: "Alt", text: "Alt" });
    if (modifiers.shift) tokens.push({ id: "shift", label: "Shift", glyph: "shift" });
  }

  const keyToken = KEY_TOKENS[shortcut.key] ?? { label: shortcut.key, text: shortcut.key };
  tokens.push({ id: `key-${shortcut.key}`, ...keyToken });
  return tokens;
}

function renderTokenContent({ label, text, glyph }: ShortcutToken) {
  if (glyph) return <KbdGlyph name={glyph} />;
  const display = text ?? label;
  if (display === ",") {
    return <span className="inline-block -translate-y-[0.04em]">{display}</span>;
  }
  return display;
}

function isSingleCompactToken(token: ShortcutToken): boolean {
  if (token.glyph) return true;
  return (token.text ?? token.label).length <= 1;
}

export type ShortcutKeycapsLayout = "pill" | "split";

export function ShortcutKeycaps({
  shortcut,
  platform = getShortcutPlatform(),
  size = "sm",
  layout = "pill",
  decorative = false,
  thenLabel,
  className,
  keyClassName,
}: {
  shortcut: ShortcutChord;
  platform?: ShortcutPlatform;
  size?: KbdSize;
  /**
   * `pill` (default) groups a chord into one quiet keycap for dense chrome
   * (palettes, tooltips, buttons). `split` separates each key for Settings and
   * shortcut recording where individual physical keys are the subject.
   */
  layout?: ShortcutKeycapsLayout;
  /** Hide a redundant hint when its parent already names the action. */
  decorative?: boolean;
  /** Shown between the keys of a sequence (G then I); without it they sit side by side. */
  thenLabel?: string;
  className?: string;
  keyClassName?: string;
}) {
  const accessibleLabel = formatShortcut(shortcut, platform);

  if (shortcut.then) {
    const step = { platform, size, layout, decorative: true, keyClassName };
    return (
      <span
        className={cn("inline-flex items-center gap-1", className)}
        role={decorative ? undefined : "img"}
        aria-label={decorative ? undefined : accessibleLabel}
        aria-hidden={decorative || undefined}
        data-slot="shortcut-keycaps"
        data-sequence=""
      >
        <ShortcutKeycaps shortcut={shortcutFirstStep(shortcut)} {...step} />
        {thenLabel ? <span className="text-micro text-muted-foreground">{thenLabel}</span> : null}
        <ShortcutKeycaps shortcut={shortcut.then} {...step} />
      </span>
    );
  }
  const tokens = shortcutTokens(shortcut, platform);

  if (layout === "pill") {
    const compact = tokens.length === 1 && isSingleCompactToken(tokens[0]!);
    return (
      <span
        className={cn("inline-flex items-center", className)}
        role={decorative ? undefined : "img"}
        aria-label={decorative ? undefined : accessibleLabel}
        aria-hidden={decorative || undefined}
        data-slot="shortcut-keycaps"
        data-layout="pill"
      >
        <Kbd
          aria-hidden="true"
          size={size}
          compact={compact}
          className={keyClassName}
        >
          {tokens.map((token) => (
            <span
              key={token.id}
              title={token.label}
              data-shortcut-key={token.id}
              className="inline-flex items-center justify-center"
            >
              {renderTokenContent(token)}
            </span>
          ))}
        </Kbd>
      </span>
    );
  }

  return (
    <span
      className={cn("inline-flex items-center gap-1", className)}
      role={decorative ? undefined : "img"}
      aria-label={decorative ? undefined : accessibleLabel}
      aria-hidden={decorative || undefined}
      data-slot="shortcut-keycaps"
      data-layout="split"
    >
      {tokens.map((token) => (
        <Kbd
          key={token.id}
          aria-hidden="true"
          title={token.label}
          data-shortcut-key={token.id}
          size={size}
          compact={isSingleCompactToken(token)}
          className={keyClassName}
        >
          {renderTokenContent(token)}
        </Kbd>
      ))}
    </span>
  );
}

/**
 * Decorative keycaps for a registered action's current binding, for a tooltip
 * whose text already names the action. Renders nothing when it is unbound.
 */
export function ActionShortcutKeycaps({ action }: { action: ShortcutActionId }) {
  const shortcut = useShortcut(action);
  return shortcut ? <ShortcutKeycaps shortcut={shortcut} decorative /> : null;
}
