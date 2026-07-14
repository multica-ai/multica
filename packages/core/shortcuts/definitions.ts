import {
  getShortcutPlatform,
  getShortcutRuntime,
  type ShortcutPlatform,
  type ShortcutRuntime,
} from "./platform";

export type ShortcutActionId =
  | "openSearch"
  | "createIssue"
  | "toggleSidebar"
  | "findInIssue"
  | "send"
  | "goInbox"
  | "goChat"
  | "goMyIssues"
  | "goIssues"
  | "goProjects"
  | "goAutopilots"
  | "goAgents"
  | "goSquads"
  | "goUsage"
  | "goRuntimes"
  | "goSkills"
  | "goSettings";

export type ShortcutCategory = "general" | "navigation";

export interface ShortcutModifiers {
  /** Command on macOS, Control on Windows/Linux. */
  primary: boolean;
  /** Literal Control, distinct from Command on macOS. */
  control: boolean;
  /** Literal Meta/Windows/Super, distinct from Control on Windows/Linux. */
  meta: boolean;
  alt: boolean;
  shift: boolean;
}

export interface ShortcutChord {
  /** Logical key (`KeyboardEvent.key`), not physical keyboard position. */
  key: string;
  modifiers: ShortcutModifiers;
}

export interface ShortcutActionDefinition {
  id: ShortcutActionId;
  category: ShortcutCategory;
  defaultShortcut: ShortcutChord | null;
  /** Whether the global handler may run while focus is inside an editor/control. */
  allowInEditable: boolean;
}

export function createShortcutChord(
  key: string,
  modifiers: Partial<ShortcutModifiers> = {},
): ShortcutChord {
  return {
    key,
    modifiers: {
      primary: false,
      control: false,
      meta: false,
      alt: false,
      shift: false,
      ...modifiers,
    },
  };
}

const primary = (key: string) =>
  createShortcutChord(key, { primary: true });

export const SHORTCUT_ACTIONS: readonly ShortcutActionDefinition[] = [
  { id: "openSearch", category: "general", defaultShortcut: primary("K"), allowInEditable: true },
  { id: "createIssue", category: "general", defaultShortcut: createShortcutChord("C"), allowInEditable: false },
  { id: "toggleSidebar", category: "general", defaultShortcut: primary("B"), allowInEditable: false },
  { id: "findInIssue", category: "general", defaultShortcut: primary("F"), allowInEditable: true },
  { id: "send", category: "general", defaultShortcut: primary("Enter"), allowInEditable: true },
  { id: "goInbox", category: "navigation", defaultShortcut: null, allowInEditable: false },
  { id: "goChat", category: "navigation", defaultShortcut: null, allowInEditable: false },
  { id: "goMyIssues", category: "navigation", defaultShortcut: null, allowInEditable: false },
  { id: "goIssues", category: "navigation", defaultShortcut: null, allowInEditable: false },
  { id: "goProjects", category: "navigation", defaultShortcut: null, allowInEditable: false },
  { id: "goAutopilots", category: "navigation", defaultShortcut: null, allowInEditable: false },
  { id: "goAgents", category: "navigation", defaultShortcut: null, allowInEditable: false },
  { id: "goSquads", category: "navigation", defaultShortcut: null, allowInEditable: false },
  { id: "goUsage", category: "navigation", defaultShortcut: null, allowInEditable: false },
  { id: "goRuntimes", category: "navigation", defaultShortcut: null, allowInEditable: false },
  { id: "goSkills", category: "navigation", defaultShortcut: null, allowInEditable: false },
  { id: "goSettings", category: "navigation", defaultShortcut: null, allowInEditable: false },
] as const;

export const SHORTCUT_ACTION_BY_ID = Object.fromEntries(
  SHORTCUT_ACTIONS.map((action) => [action.id, action]),
) as Record<ShortcutActionId, ShortcutActionDefinition>;

const MODIFIER_KEYS = new Set([
  "Alt", "AltGraph", "CapsLock", "Control", "Fn", "FnLock", "Hyper",
  "Meta", "NumLock", "ScrollLock", "Shift", "Super", "Symbol", "SymbolLock",
]);
const NON_ACTIONABLE_KEYS = new Set(["Dead", "Process", "Unidentified"]);
const KEY_LABELS: Record<string, string> = {
  " ": "Space",
  ArrowUp: "Up",
  ArrowDown: "Down",
  ArrowLeft: "Left",
  ArrowRight: "Right",
  Esc: "Escape",
  "+": "Plus",
  "-": "Minus",
  "=": "Equals",
  "_": "Underscore",
};

function eventKey(event: KeyboardEvent): string | null {
  if (MODIFIER_KEYS.has(event.key) || NON_ACTIONABLE_KEYS.has(event.key)) return null;
  const key = KEY_LABELS[event.key] ?? event.key;
  if (key.length === 1 && /[a-z]/i.test(key)) return key.toUpperCase();
  return key;
}

export function isShortcutChordActionable(shortcut: ShortcutChord): boolean {
  return shortcut.key.length > 0 &&
    !MODIFIER_KEYS.has(shortcut.key) &&
    !NON_ACTIONABLE_KEYS.has(shortcut.key);
}

/** Convert a key event into a platform-aware, structured chord. */
export function shortcutFromEvent(
  event: KeyboardEvent,
  platform: ShortcutPlatform = getShortcutPlatform(),
): ShortcutChord | null {
  if (event.getModifierState?.("AltGraph")) return null;
  const key = eventKey(event);
  if (!key) return null;
  const mac = platform === "macos";
  return createShortcutChord(key, {
    primary: mac ? event.metaKey : event.ctrlKey,
    control: mac ? event.ctrlKey : false,
    meta: mac ? false : event.metaKey,
    alt: event.altKey,
    shift: event.shiftKey,
  });
}

export function shortcutChordEquals(
  left: ShortcutChord | null,
  right: ShortcutChord | null,
): boolean {
  if (left === null || right === null) return left === right;
  return (
    left.key === right.key &&
    left.modifiers.primary === right.modifiers.primary &&
    left.modifiers.control === right.modifiers.control &&
    left.modifiers.meta === right.modifiers.meta &&
    left.modifiers.alt === right.modifiers.alt &&
    left.modifiers.shift === right.modifiers.shift
  );
}

/** Exact matcher: any additional modifier intentionally makes the event differ. */
export function shortcutMatchesEvent(
  shortcut: ShortcutChord | null,
  event: KeyboardEvent,
  platform: ShortcutPlatform = getShortcutPlatform(),
): boolean {
  // `null` means two different things at this boundary: an action without an
  // assigned shortcut, or an event that contains no actionable key (for
  // example pressing Command/Control/Shift by itself). Neither may execute an
  // action, even though `shortcutChordEquals(null, null)` is useful elsewhere
  // when comparing persisted values.
  if (!shortcut) return false;
  const eventShortcut = shortcutFromEvent(event, platform);
  return eventShortcut !== null && shortcutChordEquals(shortcut, eventShortcut);
}

export function isPlainShortcut(
  shortcut: ShortcutChord | null,
  key: string,
): boolean {
  return shortcutChordEquals(shortcut, createShortcutChord(key));
}

export function formatShortcut(
  shortcut: ShortcutChord | null,
  platform: ShortcutPlatform = getShortcutPlatform(),
): string {
  if (!shortcut) return "—";
  const { modifiers } = shortcut;
  const keyLabels: Record<string, string> = {
    Enter: platform === "macos" ? "↵" : "Enter",
    Backspace: platform === "macos" ? "⌫" : "Backspace",
    Delete: platform === "macos" ? "⌦" : "Delete",
    Escape: "Esc",
    Up: "↑",
    Down: "↓",
    Left: "←",
    Right: "→",
    Plus: "+",
    Minus: platform === "macos" ? "−" : "-",
    Equals: "=",
    Underscore: "_",
    Space: "Space",
  };
  const key = keyLabels[shortcut.key] ?? shortcut.key;

  if (platform === "macos") {
    return [
      modifiers.primary ? "⌘" : "",
      modifiers.control ? "⌃" : "",
      modifiers.alt ? "⌥" : "",
      modifiers.shift ? "⇧" : "",
      modifiers.meta ? "Meta" : "",
      key,
    ].join("");
  }

  return [
    modifiers.primary ? "Ctrl" : null,
    modifiers.control ? "Control" : null,
    modifiers.meta ? (platform === "windows" ? "Win" : "Super") : null,
    modifiers.alt ? "Alt" : null,
    modifiers.shift ? "Shift" : null,
    key,
  ].filter(Boolean).join("+");
}

export function isEditableShortcutTarget(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false;
  return (
    target.isContentEditable ||
    target.tagName === "INPUT" ||
    target.tagName === "TEXTAREA" ||
    target.tagName === "SELECT" ||
    target.closest("[contenteditable='true']") !== null
  );
}

const PRIMARY_RESERVED_KEYS = new Set([
  // Window operations the app itself owns on every runtime: W closes the
  // tab, R/F5 is the reload guard, Q quits.
  "W", "R", "Q",
  // Fundamental editing operations should never become product actions.
  "A", "C", "V", "X", "Y", "Z",
  // Zoom accelerators: fixed app shortcuts on desktop, browser zoom on web.
  "Equals", "Plus", "Minus", "Underscore", "0",
]);

// Accelerators owned by the browser UI around a tab: print, address bar,
// new tab/window, bookmark, view source. A web page cannot reliably own
// them, but the Electron renderer receives the bare primary chords as plain
// keydowns — neither Electron's default menu nor the desktop shell binds
// any of them — so exactly those are recordable on desktop (MUL-4457).
// Variants with extra modifiers stay reserved on both runtimes: several
// belong to the OS or window manager (Option+Cmd+D toggles the macOS Dock,
// Ctrl+Alt+T opens a terminal on common Linux desktops), which even the
// desktop app cannot own.
const BROWSER_ONLY_PRIMARY_RESERVED_KEYS = new Set([
  "P", "L", "T", "N", "D", "U",
]);

/** Browser/window/OS accelerators the app cannot reliably own in `runtime`. */
export function isReservedShortcut(
  shortcut: ShortcutChord,
  platform: ShortcutPlatform = getShortcutPlatform(),
  runtime: ShortcutRuntime = getShortcutRuntime(),
): boolean {
  const { modifiers, key } = shortcut;
  if (key === "F5") return true;
  if (modifiers.primary && PRIMARY_RESERVED_KEYS.has(key)) return true;
  if (modifiers.primary && BROWSER_ONLY_PRIMARY_RESERVED_KEYS.has(key)) {
    const barePrimary =
      !modifiers.control && !modifiers.meta && !modifiers.alt && !modifiers.shift;
    if (runtime !== "desktop" || !barePrimary) return true;
  }

  if (platform === "macos") {
    if (modifiers.primary && (key === "Space" || key === "Tab" || key === "M" || key === "H")) return true;
    if (modifiers.control && ["Up", "Down", "Left", "Right"].includes(key)) return true;
  } else {
    // Windows/Super shortcuts are owned by the shell/window manager and often
    // never reach the browser. Reject all of them instead of pretending a
    // recorded binding will be dependable.
    if (modifiers.meta) return true;
    if (modifiers.alt && (key === "Tab" || key === "F4")) return true;
  }

  return false;
}

const PLAIN_GLOBAL_RESERVED_KEYS = new Set([
  "Enter", "Space", "Tab", "Escape", "Backspace", "Delete",
  "Up", "Down", "Left", "Right", "Home", "End", "PageUp", "PageDown",
]);

function hasShortcutModifier(shortcut: ShortcutChord): boolean {
  const { modifiers } = shortcut;
  return modifiers.primary || modifiers.control || modifiers.meta ||
    modifiers.alt || modifiers.shift;
}

function hasCommandModifier(shortcut: ShortcutChord): boolean {
  const { modifiers } = shortcut;
  return modifiers.primary || modifiers.control || modifiers.meta;
}

/**
 * Product-level safety policy layered on top of OS/browser reservations.
 * Plain text keys are useful for non-editable navigation (for example `C`),
 * but an action allowed inside editors must not fire while the user types.
 * Send is the one deliberate exception: plain Enter is supported explicitly.
 */
export function isShortcutAllowedForAction(
  actionId: ShortcutActionId,
  shortcut: ShortcutChord,
  platform: ShortcutPlatform = getShortcutPlatform(),
  runtime: ShortcutRuntime = getShortcutRuntime(),
): boolean {
  if (!isShortcutChordActionable(shortcut)) return false;
  if (isReservedShortcut(shortcut, platform, runtime)) return false;
  // Tab is focus navigation (and Ctrl+Tab is browser tab navigation) on every
  // supported platform. It is never a dependable product-level binding.
  if (shortcut.key === "Tab") return false;
  const hasModifier = hasShortcutModifier(shortcut);
  const hasCommand = hasCommandModifier(shortcut);

  if (actionId === "send") {
    return shortcutChordEquals(shortcut, createShortcutChord("Enter")) ||
      shortcutChordEquals(
        shortcut,
        createShortcutChord("Enter", { primary: true }),
      );
  }
  if (SHORTCUT_ACTION_BY_ID[actionId].allowInEditable) {
    return hasCommand;
  }
  return hasModifier || !PLAIN_GLOBAL_RESERVED_KEYS.has(shortcut.key);
}

/** One-time parser for v1 string preferences persisted before structured chords. */
export function parseLegacyShortcut(value: string): ShortcutChord | null {
  if (!value) return null;
  const parts = value.split("+");
  const key = parts.pop();
  if (!key) return null;
  const knownModifiers = new Set(["Mod", "Ctrl", "Meta", "Alt", "Shift"]);
  if (parts.some((part) => !knownModifiers.has(part))) return null;
  return createShortcutChord(key, {
    primary: parts.includes("Mod"),
    control: parts.includes("Ctrl"),
    meta: parts.includes("Meta"),
    alt: parts.includes("Alt"),
    shift: parts.includes("Shift"),
  });
}
