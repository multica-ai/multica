"use client";

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
  type LucideIcon,
} from "lucide-react";
import {
  formatShortcut,
  getShortcutPlatform,
  type ShortcutChord,
  type ShortcutPlatform,
} from "@multica/core/shortcuts";
import { Kbd } from "@multica/ui/components/ui/kbd";
import { cn } from "@multica/ui/lib/utils";

type ShortcutToken = {
  id: string;
  label: string;
  text?: string;
  icon?: LucideIcon;
};

const KEY_TOKENS: Record<string, Omit<ShortcutToken, "id">> = {
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

function shortcutTokens(
  shortcut: ShortcutChord,
  platform: ShortcutPlatform,
): ShortcutToken[] {
  const { modifiers } = shortcut;
  const tokens: ShortcutToken[] = [];

  if (platform === "macos") {
    if (modifiers.primary) tokens.push({ id: "primary", label: "Command", icon: Command });
    if (modifiers.control) tokens.push({ id: "control", label: "Control", text: "⌃" });
    if (modifiers.alt) tokens.push({ id: "alt", label: "Option", icon: Option });
    if (modifiers.shift) tokens.push({ id: "shift", label: "Shift", icon: ArrowBigUp });
    if (modifiers.meta) tokens.push({ id: "meta", label: "Meta" });
  } else {
    if (modifiers.primary) tokens.push({ id: "primary", label: "Ctrl" });
    if (modifiers.control) tokens.push({ id: "control", label: "Control" });
    if (modifiers.meta) {
      tokens.push({
        id: "meta",
        label: platform === "windows" ? "Win" : "Super",
      });
    }
    if (modifiers.alt) tokens.push({ id: "alt", label: "Alt" });
    if (modifiers.shift) tokens.push({ id: "shift", label: "Shift", icon: ArrowBigUp });
  }

  const keyToken = KEY_TOKENS[shortcut.key] ?? { label: shortcut.key };
  tokens.push({ id: `key-${shortcut.key}`, ...keyToken });
  return tokens;
}

export function ShortcutKeycaps({
  shortcut,
  platform = getShortcutPlatform(),
  size = "sm",
  decorative = false,
  className,
  keyClassName,
}: {
  shortcut: ShortcutChord;
  platform?: ShortcutPlatform;
  size?: "sm" | "md";
  /** Hide a redundant hint when its parent already names the action. */
  decorative?: boolean;
  className?: string;
  keyClassName?: string;
}) {
  const accessibleLabel = formatShortcut(shortcut, platform);

  return (
    <span
      className={cn("inline-flex items-center gap-1", className)}
      role={decorative ? undefined : "img"}
      aria-label={decorative ? undefined : accessibleLabel}
      aria-hidden={decorative || undefined}
      data-slot="shortcut-keycaps"
    >
      {shortcutTokens(shortcut, platform).map(({ id, label, text, icon: Icon }) => (
        <Kbd
          key={id}
          aria-hidden="true"
          title={label}
          data-shortcut-key={id}
          className={cn(
            "border border-border/70 bg-muted/80 shadow-[0_1px_0_0_color-mix(in_oklab,var(--border)_70%,transparent)]",
            size === "md"
              ? "h-7 min-w-7 rounded-md px-1.5 text-xs [&_svg]:size-3.5"
              : "h-5 min-w-5 px-1 text-[10px] [&_svg]:size-3",
            keyClassName,
          )}
        >
          {Icon ? <Icon aria-hidden="true" strokeWidth={1.8} /> : (text ?? label)}
        </Kbd>
      ))}
    </span>
  );
}
