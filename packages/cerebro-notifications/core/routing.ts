import type { InboxItemType } from "@multica/core/types";

// Channels are independent destinations a notification can land on. A single
// event can flow to multiple channels (e.g. inbox + mobile push) — they are
// not mutually exclusive. Mirror of the constants in
// server/cmd/server/notification_routing.go.
export type Channel =
  | "inbox"          // persistent inbox queue
  | "notifications"  // lightweight notifications page in the sidebar bottom
  | "mobile"         // Web Push to phone / installed PWA
  | "desktop"        // Native banner / dock badge in the Electron app
  | "mail";          // Email digest (forward-compat — transport not built)

// Per-event choice within a channel — a row-level on/off toggle.
export type ChannelChoice = "on" | "off";

export const CHANNELS: ReadonlyArray<Channel> = [
  "inbox",
  "notifications",
  "mobile",
  "desktop",
  "mail",
];

// Notification types that are emitted by the server today. The TS InboxItemType
// union also lists future types (review_requested, agent_blocked, etc.) but the
// settings UI should only show types we actually deliver.
export type EmittedNotificationType = Extract<
  InboxItemType,
  | "issue_assigned"
  | "mentioned"
  | "task_failed"
  | "unassigned"
  | "reaction_added"
  | "new_comment"
  | "assignee_changed"
  | "status_changed"
  | "priority_changed"
  | "due_date_changed"
>;

// Two types route differently depending on whether the recipient is the
// assignee on the issue ("mine") or just a subscriber ("following").
// Keep this in sync with `splitTypes` in server/cmd/server/notification_routing.go.
export const SPLIT_TYPES: ReadonlySet<EmittedNotificationType> = new Set([
  "due_date_changed",
  "priority_changed",
]);

// Routing key used inside `user.preferences.notifications`. Keep in sync with
// `routeKey` in server/cmd/server/notification_routing.go.
export type RoutingKey =
  | "issue_assigned"
  | "mentioned"
  | "task_failed"
  | "unassigned"
  | "reaction_added"
  | "new_comment"
  | "assignee_changed"
  | "status_changed"
  | "due_date_changed.assignee"
  | "due_date_changed.follower"
  | "priority_changed.assignee"
  | "priority_changed.follower";

// Per-channel default when the user has no override for a given key. Mirrors
// `defaultChannelChoices` on the server — keep in sync.
export const DEFAULT_CHANNEL_CHOICES: Record<
  Channel,
  Record<RoutingKey, ChannelChoice>
> = {
  inbox: {
    issue_assigned: "on",
    mentioned: "on",
    task_failed: "on",
    unassigned: "on",
    reaction_added: "off",
    new_comment: "off",
    assignee_changed: "off",
    status_changed: "off",
    "due_date_changed.assignee": "on",
    "due_date_changed.follower": "off",
    "priority_changed.assignee": "on",
    "priority_changed.follower": "off",
  },
  notifications: {
    issue_assigned: "off",
    mentioned: "off",
    task_failed: "off",
    unassigned: "off",
    reaction_added: "on",
    new_comment: "on",
    assignee_changed: "on",
    status_changed: "on",
    "due_date_changed.assignee": "off",
    "due_date_changed.follower": "on",
    "priority_changed.assignee": "off",
    "priority_changed.follower": "on",
  },
  mobile: {
    issue_assigned: "on",
    mentioned: "on",
    task_failed: "off",
    unassigned: "off",
    reaction_added: "off",
    new_comment: "off",
    assignee_changed: "off",
    status_changed: "off",
    "due_date_changed.assignee": "on",
    "due_date_changed.follower": "off",
    "priority_changed.assignee": "on",
    "priority_changed.follower": "off",
  },
  desktop: {
    issue_assigned: "on",
    mentioned: "on",
    task_failed: "on",
    unassigned: "off",
    reaction_added: "off",
    new_comment: "off",
    assignee_changed: "off",
    status_changed: "off",
    "due_date_changed.assignee": "on",
    "due_date_changed.follower": "off",
    "priority_changed.assignee": "on",
    "priority_changed.follower": "off",
  },
  // Mail is forward-compatible; no events fire by default until the
  // transport is built.
  mail: {
    issue_assigned: "off",
    mentioned: "off",
    task_failed: "off",
    unassigned: "off",
    reaction_added: "off",
    new_comment: "off",
    assignee_changed: "off",
    status_changed: "off",
    "due_date_changed.assignee": "off",
    "due_date_changed.follower": "off",
    "priority_changed.assignee": "off",
    "priority_changed.follower": "off",
  },
};

// Channel-level transport settings — apply to the entire channel (badge,
// banner, sound, digest), independent of per-event toggles.
export interface ChannelTransport {
  badge?: boolean;
  sound?: boolean;
  banner?: boolean;
  digest?: "instant" | "daily" | "weekly";
}

export const DEFAULT_CHANNEL_TRANSPORT: Record<Channel, ChannelTransport> = {
  inbox: {},
  notifications: {},
  mobile: { badge: true, sound: true },
  desktop: { badge: true, banner: true, sound: false },
  mail: { digest: "daily" },
};

// Read the master "send a mobile push for everything that lands in inbox"
// toggle out of the preferences blob. When true, the server resolves mobile
// routing by mirroring the inbox channel's per-key resolution — see
// `resolveChannelChoice` in server/cmd/server/notification_routing.go (the
// JEH-737 master toggle). Mirror of the JSON path
// `preferences.notifications.notify_all_mobile_inbox`.
export function getNotifyAllMobileInbox(
  prefs: Record<string, unknown> | undefined,
): boolean {
  const notif = (prefs?.notifications ?? {}) as Record<string, unknown>;
  return notif.notify_all_mobile_inbox === true;
}

// Pull a user's per-channel choice out of the preferences blob, falling
// back to the per-channel default when missing or invalid.
export function getChannelChoice(
  prefs: Record<string, unknown> | undefined,
  channel: Channel,
  key: RoutingKey,
): ChannelChoice {
  const notif = (prefs?.notifications ?? {}) as Record<string, unknown>;
  const block = (notif[channel] ?? {}) as Record<string, unknown>;
  const value = block[key];
  if (value === "on" || value === "off") {
    return value;
  }
  return DEFAULT_CHANNEL_CHOICES[channel][key];
}

// Pull the channel-level transport settings (badge/sound/banner/digest),
// overlaying user-specified fields on top of the channel default.
export function getChannelTransport(
  prefs: Record<string, unknown> | undefined,
  channel: Channel,
): ChannelTransport {
  const base = DEFAULT_CHANNEL_TRANSPORT[channel];
  const notif = (prefs?.notifications ?? {}) as Record<string, unknown>;
  const channels = (notif.channels ?? {}) as Record<string, unknown>;
  const override = (channels[channel] ?? {}) as Record<string, unknown>;
  const merged: ChannelTransport = { ...base };
  if (typeof override.badge === "boolean") merged.badge = override.badge;
  if (typeof override.sound === "boolean") merged.sound = override.sound;
  if (typeof override.banner === "boolean") merged.banner = override.banner;
  if (
    override.digest === "instant" ||
    override.digest === "daily" ||
    override.digest === "weekly"
  ) {
    merged.digest = override.digest;
  }
  return merged;
}
