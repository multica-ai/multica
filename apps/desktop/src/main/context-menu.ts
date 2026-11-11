import {
  clipboard,
  type ContextMenuParams,
  type WebContents,
} from "electron";
import { isSafeExternalHttpUrl, openExternalSafely } from "./external-url";
import {
  CONTEXT_MENU_COMMAND_CHANNEL,
  CONTEXT_MENU_REQUEST_CHANNEL,
  isOfferedCommand,
  parseContextMenuCommand,
  type ContextMenuCommand,
  type ContextMenuRequest,
} from "../shared/context-menu";

// Electron ships with no default right-click menu. The app draws every menu
// in the renderer, in the app's own language and look: object menus (an
// issue, a comment, a project) handle the DOM `contextmenu` event and
// preventDefault it, which keeps Electron from firing `context-menu` at all.
// What reaches this handler is the content layer the page left alone — text
// fields, selected text, links, images, code, tables, attachments — and main
// forwards every one to the renderer, which adds what only the page knows
// (the code, the comment around it) and skips a menu with nothing in it.
export function installContextMenu(webContents: WebContents): void {
  // The latest right-click and the params it came with. A command answers
  // only this one, once; anything older or repeated is dropped.
  let pending: { request: ContextMenuRequest; params: ContextMenuParams } | null = null;
  let nextId = 0;

  webContents.on("context-menu", (_event, params) => {
    nextId += 1;
    const request = buildContextMenuRequest(nextId, params);
    pending = { request, params };
    webContents.send(CONTEXT_MENU_REQUEST_CHANNEL, request);
  });

  webContents.ipc.on(CONTEXT_MENU_COMMAND_CHANNEL, (_event, raw: unknown) => {
    const command = parseContextMenuCommand(raw);
    if (!command || !pending || !isOfferedCommand(pending.request, command)) return;
    const { params } = pending;
    pending = null;
    runCommand(webContents, params, command);
  });
}

export function buildContextMenuRequest(
  id: number,
  params: ContextMenuParams,
): ContextMenuRequest {
  const { editFlags, isEditable, misspelledWord } = params;
  const hasSelection = params.selectionText.trim().length > 0;
  const spelling = isEditable && !!misspelledWord;
  return {
    id,
    x: params.x,
    y: params.y,
    isEditable,
    canCut: isEditable && editFlags.canCut,
    canCopy: hasSelection && editFlags.canCopy,
    canPaste: isEditable && editFlags.canPaste,
    canSelectAll: isEditable && editFlags.canSelectAll,
    suggestions: spelling ? [...params.dictionarySuggestions] : [],
    canAddToDictionary: spelling,
    // params.linkURL is the resolved absolute URL of the anchor under the
    // cursor. Only http(s) links are offered: other schemes (mailto:,
    // javascript:, the app's own file:// routes) would route through the OS
    // handler, which is outside what this menu promises.
    hasLink: !!params.linkURL && isSafeExternalHttpUrl(params.linkURL),
    // Copies the decoded bitmap Chromium already holds, so it needs pixels.
    hasImage: params.mediaType === "image" && params.hasImageContents,
  };
}

function runCommand(
  webContents: WebContents,
  params: ContextMenuParams,
  command: ContextMenuCommand,
): void {
  switch (command.type) {
    case "cut":
      webContents.cut();
      return;
    case "copy":
      webContents.copy();
      return;
    case "paste":
      webContents.paste();
      return;
    case "selectAll":
      webContents.selectAll();
      return;
    case "replaceMisspelling": {
      const suggestion = params.dictionarySuggestions[command.index];
      if (suggestion !== undefined) webContents.replaceMisspelling(suggestion);
      return;
    }
    case "addToDictionary":
      webContents.session.addWordToSpellCheckerDictionary(params.misspelledWord);
      return;
    case "openLink":
      // openExternalSafely re-validates the scheme.
      void openExternalSafely(params.linkURL);
      return;
    case "copyLink":
      // Electron 44 made clipboard writes async; catch so a failed write
      // can't surface as an unhandled rejection in the main process.
      clipboard.writeText(params.linkURL).catch((err: unknown) => {
        console.warn("[context-menu] copy link address failed", err);
      });
      return;
    case "copyImage":
      webContents.copyImageAt(params.x, params.y);
      return;
  }
}
