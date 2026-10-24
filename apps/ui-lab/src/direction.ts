import type { TextDirection } from "@multica/core/i18n";

export const DIRECTION_STORAGE_KEY = "multica-ui-lab:direction";
export function isTextDirection(value: unknown): value is TextDirection {
  return value === "ltr" || value === "rtl";
}
export function loadDirection(): TextDirection {
  try {
    const saved = localStorage.getItem(DIRECTION_STORAGE_KEY);
    return isTextDirection(saved) ? saved : "ltr";
  } catch {
    return "ltr";
  }
}
