/**
 * Insert text into the currently-focused text host at its caret position.
 *
 * Supported targets:
 *   - <textarea>
 *   - <input type="text" | "search" | "url" | "tel" | "email" | "password">
 *     (other input types — number, date, file — are not text and are skipped)
 *   - elements with `contenteditable=""` / `="true"` / `="plaintext-only"`
 *
 * Returns true when the insertion ran, false when the target wasn't an
 * editable text host. Dispatches an `input` event with `inputType` set so
 * controlled React forms and rich-text editors (TipTap, ProseMirror) pick
 * up the change instead of rendering stale state.
 */
export function insertAtCaret(target: Element | null, text: string): boolean {
  if (!target || !text) {
    return false;
  }

  if (target instanceof HTMLTextAreaElement) {
    insertIntoTextField(target, text);
    return true;
  }

  if (target instanceof HTMLInputElement && isInsertableInputType(target.type)) {
    insertIntoTextField(target, text);
    return true;
  }

  if (target instanceof HTMLElement && isContentEditable(target)) {
    insertIntoContentEditable(target, text);
    return true;
  }

  return false;
}

const INSERTABLE_INPUT_TYPES = new Set([
  "text",
  "search",
  "url",
  "tel",
  "email",
  "password",
]);

function isInsertableInputType(type: string): boolean {
  return INSERTABLE_INPUT_TYPES.has(type.toLowerCase());
}

function isContentEditable(el: HTMLElement): boolean {
  const value = el.getAttribute("contenteditable");
  return value === "" || value === "true" || value === "plaintext-only";
}

function insertIntoTextField(
  el: HTMLInputElement | HTMLTextAreaElement,
  text: string,
): void {
  const start = el.selectionStart ?? el.value.length;
  const end = el.selectionEnd ?? el.value.length;
  const before = el.value.slice(0, start);
  const after = el.value.slice(end);

  // React tracks the previous value on the DOM node and skips synthetic
  // events when only `.value` changes; calling the native setter and then
  // dispatching `input` is the supported escape hatch.
  const prototype =
    el instanceof HTMLTextAreaElement
      ? HTMLTextAreaElement.prototype
      : HTMLInputElement.prototype;
  const setter = Object.getOwnPropertyDescriptor(prototype, "value")?.set;
  const next = before + text + after;
  if (setter) {
    setter.call(el, next);
  } else {
    el.value = next;
  }

  const caret = start + text.length;
  el.setSelectionRange(caret, caret);
  el.dispatchEvent(
    new InputEvent("input", {
      bubbles: true,
      cancelable: false,
      inputType: "insertText",
      data: text,
    }),
  );
}

function insertIntoContentEditable(el: HTMLElement, text: string): void {
  // Read the existing selection BEFORE calling focus(), because focus() on
  // a contenteditable may collapse the selection to the start in some
  // browsers (and in jsdom). We only want to reset the selection if the
  // caller hasn't placed it inside the editor.
  const selection = el.ownerDocument.getSelection();
  const editorContainsSelection =
    selection !== null &&
    selection.rangeCount > 0 &&
    el.contains(selection.getRangeAt(0).commonAncestorContainer);

  if (!editorContainsSelection) {
    el.focus();
    const range = el.ownerDocument.createRange();
    range.selectNodeContents(el);
    range.collapse(false);
    selection?.removeAllRanges();
    selection?.addRange(range);
  }

  // execCommand is deprecated but remains the only cross-browser way to
  // produce a beforeinput/input event that contenteditable consumers
  // (TipTap, ProseMirror) treat as a real text insertion. The Input Events
  // L2 replacement (`new InputEvent("beforeinput", ...)` + manual range
  // mutation) is not yet supported by Firefox.
  const ok = el.ownerDocument.execCommand("insertText", false, text);
  if (ok) {
    return;
  }

  const sel = el.ownerDocument.getSelection();
  const range = sel && sel.rangeCount > 0 ? sel.getRangeAt(0) : null;
  if (!range) {
    return;
  }
  range.deleteContents();
  const node = el.ownerDocument.createTextNode(text);
  range.insertNode(node);
  range.setStartAfter(node);
  range.setEndAfter(node);
  sel?.removeAllRanges();
  sel?.addRange(range);
  el.dispatchEvent(
    new InputEvent("input", {
      bubbles: true,
      cancelable: false,
      inputType: "insertText",
      data: text,
    }),
  );
}
