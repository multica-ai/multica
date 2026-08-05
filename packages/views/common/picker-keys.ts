// ---------------------------------------------------------------------------
// Picker key policy — one answer to "what moves the highlight" and "what
// accepts", shared by every list-with-a-highlight in the product.
// ---------------------------------------------------------------------------
//
// Lives here rather than next to the editor's suggestion popups because the
// consumers are no longer only editor pickers: the issue detail's thread
// navigator uses the same navigation policy, and importing a Tiptap extension
// module to reach a pure keyboard predicate would drag the whole editor
// dependency graph into a header popover.

/**
 * Keys that accept the currently highlighted suggestion row.
 *
 * `Enter` is the canonical accept (WAI-ARIA combobox guidance). Plain `Tab` is
 * an additive convenience that matches terminal / CLI / editor completion
 * muscle memory (MUL-3685). `Shift+Tab` and any `Ctrl/Cmd/Alt + Tab` are
 * deliberately NOT accept keys: they stay reverse focus navigation / OS window
 * switching, so standard keyboard accessibility is preserved.
 *
 * Centralizing the rule here keeps every picker built on
 * `createSuggestionPopupRender` (mention, slash-skill, builtin command, and any
 * future suggestion list) consistent instead of each list re-deciding what
 * counts as "accept". Callers use it in place of a bare `event.key === "Enter"`
 * check, so `Tab` becomes a strict alias of `Enter` inside their accept branch.
 */
export function isPickerAcceptKey(event: KeyboardEvent): boolean {
  if (event.key === "Enter") return true;
  return (
    event.key === "Tab" &&
    !event.shiftKey &&
    !event.ctrlKey &&
    !event.metaKey &&
    !event.altKey
  );
}

/** Which way a key press moves the highlight in a suggestion list. */
export type PickerNavigationDirection = "next" | "prev";

/**
 * Resolves a key press to a suggestion-list move, or `null` when the key is not
 * navigation.
 *
 * Arrow keys are the canonical bindings. `Ctrl+N`/`Ctrl+J` (down) and
 * `Ctrl+P`/`Ctrl+K` (up) are additive Emacs/readline aliases that the command
 * bar already accepts for free: it is built on cmdk, whose `vimBindings` option
 * (on by default) maps exactly this set. Mirroring it here means the pickers
 * stop being the one place in the product where that muscle memory dies
 * (MUL-5495).
 *
 * `Cmd`-based aliases are deliberately absent. cmdk does not bind them either,
 * so the command bar never supported them, and `Cmd+P`/`Cmd+N` are browser and
 * OS accelerators (print, new window) that a web page cannot own — the same
 * rule `isReservedShortcut` in `@multica/core/shortcuts` already encodes.
 *
 * The letter aliases require Ctrl *alone*. With another modifier the chord
 * belongs to the browser or OS (`Ctrl+Shift+N` opens an incognito window), so
 * those fall through untouched. Arrow keys stay modifier-agnostic, exactly as
 * before.
 *
 * Centralizing this next to `isPickerAcceptKey` keeps every picker built on
 * `createSuggestionPopupRender` navigating identically instead of each list
 * re-deciding what counts as "move down".
 */
export function pickerNavigationDirection(
  event: KeyboardEvent,
): PickerNavigationDirection | null {
  if (event.key === "ArrowDown") return "next";
  if (event.key === "ArrowUp") return "prev";
  if (!event.ctrlKey || event.metaKey || event.altKey || event.shiftKey) {
    return null;
  }
  // Lowercase because Caps Lock reports "N" without setting shiftKey.
  switch (event.key.toLowerCase()) {
    case "n":
    case "j":
      return "next";
    case "p":
    case "k":
      return "prev";
    default:
      return null;
  }
}
