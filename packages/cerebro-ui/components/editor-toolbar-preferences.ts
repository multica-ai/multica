export const EDITOR_TOOLBAR_ORDER_KEY = "cerebro_editor_toolbar_order";

export const DEFAULT_EDITOR_TOOLBAR_ORDER = [
  "bold",
  "link",
  "heading",
  "highlight",
  "taskList",
  "comment",
  "italic",
  "strike",
  "bulletList",
  "orderedList",
  "blockquote",
  "code",
  "indent",
  "outdent",
] as const;

export type EditorToolbarActionId =
  (typeof DEFAULT_EDITOR_TOOLBAR_ORDER)[number];

const VALID_ACTIONS = new Set<string>(DEFAULT_EDITOR_TOOLBAR_ORDER);

export function readEditorToolbarOrder(
  value: unknown,
): EditorToolbarActionId[] {
  if (!Array.isArray(value)) return [...DEFAULT_EDITOR_TOOLBAR_ORDER];

  const saved = value.filter(
    (action, index): action is EditorToolbarActionId =>
      typeof action === "string" &&
      VALID_ACTIONS.has(action) &&
      value.indexOf(action) === index,
  );

  return [
    ...saved,
    ...DEFAULT_EDITOR_TOOLBAR_ORDER.filter(
      (action) => !saved.includes(action),
    ),
  ];
}
