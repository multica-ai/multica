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
  | "toggleRightSidebar"
  | "toggleChat"
  | "newChat"
  | "findInIssue"
  | "archiveInboxItem"
  | "snoozeInboxItem"
  | "send"
  | "voiceInput"
  | "goBack"
  | "goForward"
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
  {
    id: "toggleRightSidebar",
    category: "general",
    defaultShortcut: primary("/"),
    allowInEditable: false,
  },
  // Mod+J follows the "toggle a docked panel" convention, and is one of the few
  // letters this module's own policy leaves free on every platform and runtime:
  // it is neither app-owned (PRIMARY_RESERVED_VARIANTS) nor browser-owned
  // (BROWSER_ONLY_PRIMARY_RESERVED_KEYS). `allowInEditable` because the point of
  // the binding is reaching — and dismissing — chat without a mouse, which has
  // to keep working while the caret sits in the chat composer itself.
  { id: "toggleChat", category: "general", defaultShortcut: primary("J"), allowInEditable: true },
  // Ask Multica from the keyboard: Shift on the chat toggle, so the two chat
  // chords sit together. Shift+J is outside every reserved set, as is Mod+J.
  {
    id: "newChat",
    category: "general",
    defaultShortcut: createShortcutChord("J", { primary: true, shift: true }),
    allowInEditable: true,
  },
  { id: "findInIssue", category: "general", defaultShortcut: primary("F"), allowInEditable: true },
  {
    id: "archiveInboxItem",
    category: "general",
    defaultShortcut: createShortcutChord("E"),
    allowInEditable: false,
  },
  {
    id: "snoozeInboxItem",
    category: "general",
    defaultShortcut: createShortcutChord("H"),
    allowInEditable: false,
  },
  { id: "send", category: "general", defaultShortcut: primary("Enter"), allowInEditable: true },
  // Voice input works where the caret is, so it must fire inside editors. It
  // is matched on keydown AND keyup (holding it is push-to-talk), so the voice
  // layer handles it rather than the global dispatcher. Unassigned by default:
  // users pick their own chord in Settings.
  {
    id: "voiceInput",
    category: "general",
    defaultShortcut: null,
    allowInEditable: true,
  },
  // Browser-style history navigation (Mod+[ / Mod+]). Neither bracket is
  // app-owned (PRIMARY_RESERVED_VARIANTS) nor browser-owned
  // (BROWSER_ONLY_PRIMARY_RESERVED_KEYS), so both are recordable on every
  // platform and runtime. `allowInEditable` is false so the chord never steps
  // away from the page while the caret sits in an input, textarea, or editor.
  { id: "goBack", category: "navigation", defaultShortcut: primary("["), allowInEditable: false },
  { id: "goForward", category: "navigation", defaultShortcut: primary("]"), allowInEditable: false },
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
  // macOS types a non-breaking space for Option+Space.
  "\u00a0": "Space",
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

const CODE_KEYS: Record<string, string> = {
  Space: "Space",
  Minus: "Minus",
  Equal: "Equals",
  BracketLeft: "[",
  BracketRight: "]",
  Backslash: "\\",
  Semicolon: ";",
  Quote: "'",
  Comma: ",",
  Period: ".",
  Slash: "/",
  Backquote: "`",
};

function keyFromCode(code: unknown): string | null {
  if (typeof code !== "string") return null;
  const match = /^(?:Key([A-Z])|Digit([0-9]))$/.exec(code);
  if (match) return match[1] ?? match[2] ?? null;
  return CODE_KEYS[code] ?? null;
}

// Option is a glyph modifier on macOS: ⌥G arrives as "©" and ⌥E as a dead
// key, even with Command held. Those chords are named by physical key instead.
function isComposedOptionKey(key: string): boolean {
  return key === "Dead" || (key.length === 1 && key.charCodeAt(0) > 0x7e);
}

function eventKey(
  event: KeyboardEvent,
  platform: ShortcutPlatform,
  physicalOption: boolean,
): string | null {
  // Synthetic events (e.g. Chrome autofill) may omit `key` entirely.
  if (typeof event.key !== "string") return null;
  if (MODIFIER_KEYS.has(event.key)) return null;
  if (
    physicalOption &&
    platform === "macos" &&
    event.altKey &&
    isComposedOptionKey(event.key)
  ) {
    return keyFromCode(event.code);
  }
  if (NON_ACTIONABLE_KEYS.has(event.key)) return null;
  const key = KEY_LABELS[event.key] ?? event.key;
  if (key.length === 1 && /[a-z]/i.test(key)) return key.toUpperCase();
  return key;
}

export function isShortcutChordActionable(shortcut: ShortcutChord): boolean {
  return shortcut.key.length > 0 &&
    !MODIFIER_KEYS.has(shortcut.key) &&
    !NON_ACTIONABLE_KEYS.has(shortcut.key);
}

/** Whether `key` is a modifier pressed on its own, on the way to a chord. */
export function isShortcutModifierKey(key: unknown): boolean {
  return typeof key === "string" && MODIFIER_KEYS.has(key);
}

function chordFromEvent(
  event: KeyboardEvent,
  platform: ShortcutPlatform,
  physicalOption: boolean,
): ShortcutChord | null {
  const mac = platform === "macos";
  // Firefox on macOS reports Option as AltGraph; elsewhere AltGraph means the
  // key is typing a character, not forming a chord.
  if (!mac && event.getModifierState?.("AltGraph")) return null;
  const key = eventKey(event, platform, physicalOption);
  if (!key) return null;
  return createShortcutChord(key, {
    primary: mac ? event.metaKey : event.ctrlKey,
    control: mac ? event.ctrlKey : false,
    meta: mac ? false : event.metaKey,
    alt: event.altKey,
    shift: event.shiftKey,
  });
}

/** Convert a key event into a platform-aware, structured chord. */
export function shortcutFromEvent(
  event: KeyboardEvent,
  platform: ShortcutPlatform = getShortcutPlatform(),
): ShortcutChord | null {
  return chordFromEvent(event, platform, true);
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
  if (eventShortcut !== null && shortcutChordEquals(shortcut, eventShortcut)) {
    return true;
  }
  // Option chords recorded before they were named by physical key persisted
  // the composed symbol (⌘⌥G as "©") and must keep firing.
  const composedShortcut = chordFromEvent(event, platform, false);
  return composedShortcut !== null && shortcutChordEquals(shortcut, composedShortcut);
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

const PORTAL_LAYER_SELECTOR =
  '[role="menu"], [role="dialog"], [role="alertdialog"], [role="listbox"]';

/**
 * Whether an open popup (menu, dialog, listbox) owns the keyboard. Popups are
 * portaled to the body, so page-level listeners still see their keypresses;
 * the `data-base-ui-inert` marker catches modal layers even when focus never
 * left the page.
 */
export function isPortalLayerShortcutTarget(target: EventTarget | null): boolean {
  if (typeof document === "undefined") return false;
  if (document.querySelector("[data-base-ui-inert]") !== null) return true;
  return target instanceof Element && target.closest(PORTAL_LAYER_SELECTOR) !== null;
}

/**
 * Which modifier combinations on top of Command/Ctrl are taken for a key:
 * "any", or the exact extra modifiers ("" bare, "shift", "alt+shift", ...)
 * in the order produced by `extraModifiers`. Everything not listed stays
 * free, so ⌘⇧X is recordable even though ⌘X is Cut.
 */
type ReservedVariants = "any" | readonly string[];

const PRIMARY_RESERVED_VARIANTS: Record<string, ReservedVariants> = {
  // Window operations the app itself owns on every runtime: W closes the tab
  // (Shift+W the window), R/F5 is the reload guard, Q quits (Shift+Q logs out
  // on macOS).
  W: "any", R: "any", Q: "any",
  // Preferences: the desktop app opens Settings from the main process, and on
  // web the browser opens its own settings — neither leaves the chord free for
  // a product action.
  ",": "any",
  // Zoom accelerators: fixed app shortcuts on desktop, browser zoom on web.
  Equals: "any", Plus: "any", Minus: "any", Underscore: "any", "0": "any",
  // Browser-style direct tab selection: browsers own these on web and the
  // desktop main process owns them for product tabs (Shift+Cmd+3/4/5 are also
  // macOS screenshots). Keeping them reserved also sanitizes any conflicting
  // shortcut override persisted by an older app version.
  "1": "any", "2": "any", "3": "any", "4": "any", "5": "any",
  "6": "any", "7": "any", "8": "any", "9": "any",
  // Editing operations, plus the editing variants the OS and Electron's
  // default menu assign: Shift+V / Alt+Shift+V paste as plain text, Shift+Z
  // redoes. Browser DevTools chords are not listed — a page can claim them.
  A: [""],
  C: [""],
  V: ["", "shift", "alt+shift"],
  X: [""],
  Y: [""],
  Z: ["", "shift"],
};

const MACOS_PRIMARY_RESERVED_VARIANTS: Record<string, ReservedVariants> = {
  // Spotlight, input-source switching, emoji picker, app switcher.
  Space: "any", Tab: "any",
  // Hide / Hide Others and Minimize / Minimize All.
  H: ["", "alt"], M: ["", "alt"],
  // Control+Command+F toggles full screen.
  F: ["control"],
};

// Toggle Developer Tools in Electron's default menu, whose accelerators fire
// even when the page handles the keydown. Browsers let a page claim the same
// chord, so these stay recordable on web.
const MACOS_DESKTOP_PRIMARY_RESERVED_VARIANTS: Record<string, ReservedVariants> = {
  I: ["alt"],
};

const OTHER_DESKTOP_PRIMARY_RESERVED_VARIANTS: Record<string, ReservedVariants> = {
  I: ["shift"],
};

function extraModifiers({ control, meta, alt, shift }: ShortcutModifiers): string {
  return [control && "control", meta && "meta", alt && "alt", shift && "shift"]
    .filter(Boolean)
    .join("+");
}

function isReservedIn(
  table: Record<string, ReservedVariants>,
  shortcut: ShortcutChord,
): boolean {
  if (!Object.prototype.hasOwnProperty.call(table, shortcut.key)) return false;
  const variants = table[shortcut.key];
  return variants === "any" || variants!.includes(extraModifiers(shortcut.modifiers));
}

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
  if (modifiers.primary) {
    if (isReservedIn(PRIMARY_RESERVED_VARIANTS, shortcut)) return true;
    if (BROWSER_ONLY_PRIMARY_RESERVED_KEYS.has(key)) {
      if (runtime !== "desktop" || extraModifiers(modifiers) !== "") return true;
    }
    const mac = platform === "macos";
    if (mac && isReservedIn(MACOS_PRIMARY_RESERVED_VARIANTS, shortcut)) return true;
    if (runtime === "desktop") {
      const desktopVariants = mac
        ? MACOS_DESKTOP_PRIMARY_RESERVED_VARIANTS
        : OTHER_DESKTOP_PRIMARY_RESERVED_VARIANTS;
      if (isReservedIn(desktopVariants, shortcut)) return true;
    }
  }

  if (platform === "macos") {
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
  // Held while talking, so Option/Alt chords are allowed too: they leave
  // typing alone as long as the key is not a printable character.
  if (actionId === "voiceInput") {
    return hasCommand || (shortcut.modifiers.alt && (shortcut.key === "Space" || shortcut.key.length > 1));
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
