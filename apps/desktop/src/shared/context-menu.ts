/** main → renderer: a right-click the page left unhandled, to draw a menu for. */
export const CONTEXT_MENU_REQUEST_CHANNEL = "context-menu:request";
/** renderer → main: the item the user picked in that menu. */
export const CONTEXT_MENU_COMMAND_CHANNEL = "context-menu:command";

/**
 * What the renderer needs to draw the menu for one right-click. Main builds it
 * from Electron's context-menu params and keeps the params themselves: the
 * link, the misspelled word and the suggestions a command acts on are read
 * from main's copy, never from the renderer.
 */
export interface ContextMenuRequest {
  /** Matches a command to the right-click it answers. */
  id: number;
  /** Electron's cursor position, for when the renderer saw no DOM event. */
  x: number;
  y: number;
  isEditable: boolean;
  canCut: boolean;
  canCopy: boolean;
  canPaste: boolean;
  canSelectAll: boolean;
  /** Spelling suggestions for the misspelled word under the cursor. */
  suggestions: string[];
  canAddToDictionary: boolean;
  /** The cursor is on an http(s) link. */
  hasLink: boolean;
  /** The cursor is on an image whose pixels have loaded. */
  hasImage: boolean;
}

export type ContextMenuAction =
  | "cut"
  | "copy"
  | "paste"
  | "selectAll"
  | "addToDictionary"
  | "openLink"
  | "copyLink"
  | "copyImage";

export type ContextMenuCommand =
  | { id: number; type: ContextMenuAction }
  | { id: number; type: "replaceMisspelling"; index: number };

/** The plain actions a request offers, in menu order. */
export function contextMenuActions(request: ContextMenuRequest): ContextMenuAction[] {
  const actions: ContextMenuAction[] = [];
  if (request.canAddToDictionary) actions.push("addToDictionary");
  if (request.isEditable && request.canCut) actions.push("cut");
  if (request.canCopy) actions.push("copy");
  if (request.isEditable && request.canPaste) actions.push("paste");
  if (request.isEditable && request.canSelectAll) actions.push("selectAll");
  if (request.hasLink) actions.push("openLink", "copyLink");
  if (request.hasImage) actions.push("copyImage");
  return actions;
}

/** Whether a request has anything to show. */
export function hasContextMenuItems(request: ContextMenuRequest): boolean {
  return request.suggestions.length > 0 || contextMenuActions(request).length > 0;
}

/** Whether `command` is one the request's menu actually offered. */
export function isOfferedCommand(
  request: ContextMenuRequest,
  command: ContextMenuCommand,
): boolean {
  if (command.id !== request.id) return false;
  if (command.type === "replaceMisspelling") {
    return command.index >= 0 && command.index < request.suggestions.length;
  }
  return contextMenuActions(request).includes(command.type);
}

const ACTIONS: ReadonlySet<string> = new Set<ContextMenuAction>([
  "cut",
  "copy",
  "paste",
  "selectAll",
  "addToDictionary",
  "openLink",
  "copyLink",
  "copyImage",
]);

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}

/** Validates a command arriving over IPC; null for anything malformed. */
export function parseContextMenuCommand(value: unknown): ContextMenuCommand | null {
  if (!isRecord(value) || !Number.isInteger(value.id)) return null;
  const id = value.id as number;
  if (value.type === "replaceMisspelling") {
    return Number.isInteger(value.index)
      ? { id, type: "replaceMisspelling", index: value.index as number }
      : null;
  }
  if (typeof value.type === "string" && ACTIONS.has(value.type)) {
    return { id, type: value.type as ContextMenuAction };
  }
  return null;
}

/** Validates a request arriving from main before the renderer draws it. */
export function isContextMenuRequest(value: unknown): value is ContextMenuRequest {
  if (!isRecord(value)) return false;
  return (
    Number.isInteger(value.id) &&
    typeof value.x === "number" &&
    typeof value.y === "number" &&
    typeof value.isEditable === "boolean" &&
    typeof value.canCut === "boolean" &&
    typeof value.canCopy === "boolean" &&
    typeof value.canPaste === "boolean" &&
    typeof value.canSelectAll === "boolean" &&
    Array.isArray(value.suggestions) &&
    value.suggestions.every((s) => typeof s === "string") &&
    typeof value.canAddToDictionary === "boolean" &&
    typeof value.hasLink === "boolean" &&
    typeof value.hasImage === "boolean"
  );
}
