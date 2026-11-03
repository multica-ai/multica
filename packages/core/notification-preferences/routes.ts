import type {
  NotificationLegacyGroupKey,
  NotificationPreferences,
  NotificationRoute,
  NotificationRouteKey,
} from "../types";

export interface NotificationRouteKind {
  key: NotificationRouteKey;
  default: Exclude<NotificationRoute, "muted">;
  /** Pre-routing group whose "muted" still applies while `key` is unset. */
  legacy?: NotificationLegacyGroupKey;
}

/**
 * Routable kinds in settings order. Mirrors `RouteKinds` in
 * server/internal/inbox/routing.go, which applies them when notifications are
 * created; keep the two in step.
 *
 * Comments route by who they are written to, not who wrote them: one that
 * @mentions nobody, or one that @mentions others but not the recipient. A
 * comment that @mentions the recipient follows `mentions`.
 */
export const NOTIFICATION_ROUTE_KINDS: readonly NotificationRouteKind[] = [
  { key: "assigned", default: "action", legacy: "assignments" },
  { key: "mentions", default: "action" },
  { key: "comments_without_mentions", default: "action", legacy: "comments" },
  { key: "agent_failures", default: "action", legacy: "agent_activity" },
  { key: "status_changes", default: "activity" },
  { key: "assignee_changes", default: "activity", legacy: "assignments" },
  { key: "updates", default: "activity" },
  { key: "comments_mentioning_others", default: "activity", legacy: "comments" },
];

function isRoute(value: unknown): value is NotificationRoute {
  return value === "action" || value === "activity" || value === "muted";
}

/** Where `kind` currently lands for these preferences. */
export function resolveNotificationRoute(
  preferences: NotificationPreferences,
  kind: NotificationRouteKind,
): NotificationRoute {
  const value = preferences[kind.key];
  if (isRoute(value)) return value;
  if (kind.legacy && preferences[kind.legacy] === "muted") return "muted";
  return kind.default;
}

/** True when every kind lands in its default section. */
export function notificationRoutesAreDefault(preferences: NotificationPreferences): boolean {
  return NOTIFICATION_ROUTE_KINDS.every(
    (kind) => resolveNotificationRoute(preferences, kind) === kind.default,
  );
}

/**
 * Preferences after routing `kind` to `route`. A default choice is stored as
 * "all" so it follows future default changes. If the kind's legacy group was
 * muted, the group is retired and every other kind it covered keeps its muted
 * route explicitly, so changing one kind never unmutes another.
 */
export function withNotificationRoute(
  preferences: NotificationPreferences,
  kind: NotificationRouteKind,
  route: NotificationRoute,
): NotificationPreferences {
  const next: NotificationPreferences = { ...preferences };
  if (kind.legacy && preferences[kind.legacy] === "muted") {
    for (const sibling of NOTIFICATION_ROUTE_KINDS) {
      if (sibling.legacy !== kind.legacy || sibling.key === kind.key) continue;
      next[sibling.key] = resolveNotificationRoute(preferences, sibling);
      if (next[sibling.key] === sibling.default) delete next[sibling.key];
    }
    delete next[kind.legacy];
  }
  if (route === kind.default) delete next[kind.key];
  else next[kind.key] = route;
  return next;
}

/** Preferences with every kind back on its default; other settings are kept. */
export function withDefaultNotificationRoutes(
  preferences: NotificationPreferences,
): NotificationPreferences {
  const next: NotificationPreferences = { ...preferences };
  for (const kind of NOTIFICATION_ROUTE_KINDS) {
    delete next[kind.key];
    if (kind.legacy) delete next[kind.legacy];
  }
  return next;
}
