/**
 * Action-sheet option parsing. Kept free of `react-native` so Node vitest
 * can cover cancel / destructive indexing without Hermes shims.
 */

export type ActionSheetOptions = {
  title?: string;
  message?: string;
  options: string[];
  cancelButtonIndex?: number;
  destructiveButtonIndex?: number | number[];
  disabledButtonIndices?: number[];
};

export type ActionSheetRow = {
  index: number;
  label: string;
  role: "default" | "cancel" | "destructive" | "disabled";
};

export type ParsedActionSheet = {
  title?: string;
  message?: string;
  actions: ActionSheetRow[];
  cancel: ActionSheetRow | null;
};

export function parseActionSheet(options: ActionSheetOptions): ParsedActionSheet {
  const destructive = new Set(
    options.destructiveButtonIndex === undefined
      ? []
      : Array.isArray(options.destructiveButtonIndex)
        ? options.destructiveButtonIndex
        : [options.destructiveButtonIndex],
  );
  const disabled = new Set(options.disabledButtonIndices ?? []);
  const cancelIndex = options.cancelButtonIndex;
  const actions: ActionSheetRow[] = [];
  let cancel: ActionSheetRow | null = null;

  options.options.forEach((label, index) => {
    const role: ActionSheetRow["role"] =
      index === cancelIndex
        ? "cancel"
        : disabled.has(index)
          ? "disabled"
          : destructive.has(index)
            ? "destructive"
            : "default";
    const row: ActionSheetRow = { index, label, role };
    if (role === "cancel") cancel = row;
    else actions.push(row);
  });

  return {
    title: options.title,
    message: options.message,
    actions,
    cancel,
  };
}
