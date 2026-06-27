export type NotificationGroupKey =
  | "assignments"
  | "status_changes"
  | "comments"
  | "updates"
  | "agent_activity"
  | "system_notifications";

export type NotificationGroupValue = "all" | "muted";

/**
 * Sound-related preference keys. These are the 9 keys defined in the server's
 * `validNotifGroups` for sound preferences. Boolean-style keys use "all"/"muted";
 * `sound_volume` is a number 0–100 and `sound_theme` is a string.
 */
export type SoundPreferenceKey =
  | "sound_enabled"
  | "sound_issue_done"
  | "sound_child_blocked"
  | "sound_blocked"
  | "sound_in_review"
  | "sound_mention_decision"
  | "sound_task_failed"
  | "sound_volume"
  | "sound_theme";

/**
 * Decision-bottleneck notification scenario keys. Each maps to an
 * "all" / "muted" value controlling whether the user receives system
 * notifications for that category.
 */
export type BottleneckKey =
  | "notify_issue_blocked"
  | "notify_in_review"
  | "notify_child_blocked"
  | "notify_parent_chain_done"
  | "notify_task_failed";

/**
 * All preference keys that use the "all" | "muted" value space.
 * Combined from existing inbox groups, sound toggles, and bottleneck keys.
 */
type BooleanPreferenceKey =
  | NotificationGroupKey
  | Exclude<SoundPreferenceKey, "sound_volume" | "sound_theme">
  | BottleneckKey;

/**
 * User notification preferences.
 *
 * Boolean-style keys default to "all" (enabled) when absent from the record;
 * "muted" explicitly disables the corresponding notification / sound.
 *
 * `sound_volume` (0–100, default 70) and `sound_theme` ("default" | "soft" | "alert")
 * are the only non-boolean preference values.
 */
export type NotificationPreferences = Partial<Record<BooleanPreferenceKey, NotificationGroupValue>> & {
  sound_volume?: number;
  sound_theme?: string;
};

export interface NotificationPreferenceResponse {
  workspace_id: string;
  preferences: NotificationPreferences;
}
