import type { AutoArchiveKey, NotificationPreferences } from "../types";
import { notificationPreferenceValue } from "./patch";

export const AUTO_ARCHIVE_KEYS = [
  "auto_archive_completed",
  "auto_archive_on_comment",
  "auto_archive_on_status",
  "auto_archive_on_assignee",
  "auto_archive_on_priority",
  "auto_archive_on_project",
  "auto_archive_on_dates",
  "auto_archive_on_labels",
] as const satisfies readonly AutoArchiveKey[];

export type AutoArchivePreset = "never" | "completed" | "handled";

/**
 * The actions each preset archives on. `handled`, the default for members who
 * never chose, is what the server does with no stored keys.
 */
export const AUTO_ARCHIVE_PRESETS: Record<AutoArchivePreset, readonly AutoArchiveKey[]> = {
  never: [],
  completed: ["auto_archive_completed"],
  handled: [
    "auto_archive_completed",
    "auto_archive_on_comment",
    "auto_archive_on_status",
    "auto_archive_on_assignee",
  ],
};

/** The auto-archive actions turned on in preferences. */
export function autoArchiveActionsOn(preferences: NotificationPreferences): AutoArchiveKey[] {
  return AUTO_ARCHIVE_KEYS.filter((key) => notificationPreferenceValue(preferences, key) === "all");
}

/** The preset preferences match, or "custom" when they match none. */
export function autoArchivePreset(preferences: NotificationPreferences): AutoArchivePreset | "custom" {
  const on = autoArchiveActionsOn(preferences);
  const match = (Object.keys(AUTO_ARCHIVE_PRESETS) as AutoArchivePreset[]).find((preset) => {
    const actions = AUTO_ARCHIVE_PRESETS[preset];
    return actions.length === on.length && actions.every((key) => on.includes(key));
  });
  return match ?? "custom";
}

/** Every auto-archive key set on or off so exactly `on` archive. */
export function autoArchivePreferences(on: readonly AutoArchiveKey[]): NotificationPreferences {
  return Object.fromEntries(
    AUTO_ARCHIVE_KEYS.map((key) => [key, on.includes(key) ? "all" : "muted"]),
  ) as NotificationPreferences;
}
