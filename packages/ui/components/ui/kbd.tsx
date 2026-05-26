"use client";

import { cn } from "@multica/ui/lib/utils";

// ---------------------------------------------------------------------------
// Platform helpers (minimal — no runtime deps outside React)
// ---------------------------------------------------------------------------

const IS_MAC =
  typeof navigator !== "undefined" && /Mac/.test(navigator.platform);

/** Map TanStack-style tokens to platform-correct display symbols. */
const SYMBOL_MAP: Record<string, string> = IS_MAC
  ? {
      mod: "⌘",
      ctrl: "⌃",
      alt: "⌥",
      shift: "⇧",
      meta: "⌘",
      enter: "↵",
      return: "↵",
      backspace: "⌫",
      delete: "⌦",
      escape: "Esc",
      tab: "⇥",
      space: "␣",
      up: "↑",
      down: "↓",
      left: "←",
      right: "→",
    }
  : {
      mod: "Ctrl",
      ctrl: "Ctrl",
      alt: "Alt",
      shift: "Shift",
      meta: "Win",
      enter: "Enter",
      return: "Enter",
      backspace: "Backspace",
      delete: "Delete",
      escape: "Esc",
      tab: "Tab",
      space: "Space",
      up: "↑",
      down: "↓",
      left: "←",
      right: "→",
    };

function tokenToSymbol(token: string): string {
  return SYMBOL_MAP[token.toLowerCase()] ?? token.toUpperCase();
}

// ---------------------------------------------------------------------------
// Component
// ---------------------------------------------------------------------------

interface KbdProps {
  /**
   * Hotkey string in TanStack format, e.g. "Mod+K", "Shift+Enter".
   * Split on "+" to produce individual key chips.
   */
  keys: string;
  /** Additional class names on the outer wrapper. */
  className?: string;
}

/**
 * Renders a keyboard shortcut as a sequence of muted, rounded key chips.
 *
 * Uses platform-correct symbols: ⌘ on Mac, Ctrl elsewhere.
 *
 * @example
 *   <Kbd keys="Mod+K" />       // ⌘ K on Mac, Ctrl K elsewhere
 *   <Kbd keys="Shift+Enter" /> // ⇧ ↵ on Mac, Shift Enter elsewhere
 */
function Kbd({ keys, className }: KbdProps) {
  const parts = keys.split("+").map(tokenToSymbol);

  return (
    <span className={cn("inline-flex items-center gap-0.5", className)}>
      {parts.map((part, i) => (
        <kbd
          key={i}
          className={cn(
            "inline-flex items-center justify-center",
            "min-w-5 h-5 px-1 rounded",
            "bg-muted text-muted-foreground",
            "text-[11px] font-medium leading-none",
            "select-none",
          )}
        >
          {part}
        </kbd>
      ))}
    </span>
  );
}

export { Kbd, type KbdProps };
