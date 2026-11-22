/** Backend sentinel when no explicit model is configured. */
export const RUNTIME_DEFAULT_MODEL_SENTINEL = "Default" as const;

const LEGACY_RUNTIME_DEFAULT_MODEL_LABELS = new Set([
  RUNTIME_DEFAULT_MODEL_SENTINEL,
  "Runtime default model",
  "Runtime default",
]);

export function isRuntimeDefaultModelLabel(
  label: string | null | undefined,
): boolean {
  const trimmed = label?.trim() ?? "";
  return trimmed === "" || LEGACY_RUNTIME_DEFAULT_MODEL_LABELS.has(trimmed);
}

export function resolveRuntimeModelDisplayLabel(
  label: string | null | undefined,
  defaultLabel: string,
): string {
  if (isRuntimeDefaultModelLabel(label)) return defaultLabel;
  return label!.trim();
}
