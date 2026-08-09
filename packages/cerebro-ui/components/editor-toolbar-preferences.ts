export const EDITOR_TOOLBAR_ORDER_KEY = "cerebro_editor_toolbar_order";
export const EDITOR_TOOLBAR_LIST_TYPE_KEY = "cerebro_editor_list_type";

/**
 * The toolbar's actions, grouped the way the row draws them: text style ·
 * marks · link · lists · quote, with Comment last.
 *
 * `bulletList`, `orderedList` and `taskList` collapsed into one `lists`
 * control, and `indent` / `outdent` moved behind its chevron, so they are no
 * longer actions the row can hold. Values saved before that are still mapped
 * on read — see readEditorToolbarRow.
 */
export const DEFAULT_EDITOR_TOOLBAR_ORDER = [
  "heading",
  "bold",
  "italic",
  "strike",
  "highlight",
  "code",
  "link",
  "lists",
  "blockquote",
  "comment",
] as const;

export type EditorToolbarActionId =
  (typeof DEFAULT_EDITOR_TOOLBAR_ORDER)[number];

export interface EditorToolbarRow {
  order: EditorToolbarActionId[];
  hidden: EditorToolbarActionId[];
}

const VALID_ACTIONS = new Set<string>(DEFAULT_EDITOR_TOOLBAR_ORDER);

/**
 * Rows saved by the build that shipped before the contract narrowed. These
 * mappings are permanent: the stored value is only rewritten the next time the
 * user saves, so a row written today must keep loading forever.
 */
const REMOVED_ACTIONS: Record<string, EditorToolbarActionId | null> = {
  bulletList: "lists",
  orderedList: "lists",
  taskList: "lists",
  indent: null,
  outdent: null,
};

function normalize(value: unknown): EditorToolbarActionId[] {
  if (!Array.isArray(value)) return [];
  const mapped = value.flatMap((entry) => {
    if (typeof entry !== "string") return [];
    if (entry in REMOVED_ACTIONS) {
      const replacement = REMOVED_ACTIONS[entry];
      return replacement ? [replacement] : [];
    }
    return VALID_ACTIONS.has(entry) ? [entry as EditorToolbarActionId] : [];
  });
  return mapped.filter((action, index) => mapped.indexOf(action) === index);
}

/**
 * The row as the user configured it: the order they chose plus the actions
 * they hid. Accepts the bare array saved by the first build and the
 * {order, hidden} object, and never throws — a malformed value falls back to
 * the default row.
 */
export function readEditorToolbarRow(value: unknown): EditorToolbarRow {
  const source =
    value && !Array.isArray(value) && typeof value === "object"
      ? (value as { order?: unknown; hidden?: unknown })
      : { order: value, hidden: [] };

  const saved = normalize(source.order);

  return {
    order: [
      ...saved,
      ...DEFAULT_EDITOR_TOOLBAR_ORDER.filter(
        (action) => !saved.includes(action),
      ),
    ],
    hidden: normalize(source.hidden),
  };
}

/**
 * The three list types the lists split control can toggle. The left half of the
 * control toggles whichever of these the user reached for last, so the type is
 * persisted per user under EDITOR_TOOLBAR_LIST_TYPE_KEY.
 */
export const EDITOR_LIST_TYPES = [
  "bulletList",
  "orderedList",
  "taskList",
] as const;

export type EditorListType = (typeof EDITOR_LIST_TYPES)[number];

export const DEFAULT_EDITOR_LIST_TYPE: EditorListType = "bulletList";

export function readEditorListType(value: unknown): EditorListType {
  return EDITOR_LIST_TYPES.includes(value as EditorListType)
    ? (value as EditorListType)
    : DEFAULT_EDITOR_LIST_TYPE;
}
