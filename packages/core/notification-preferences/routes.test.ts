import { describe, expect, it } from "vitest";
import type { NotificationPreferences, NotificationRouteKey } from "../types";
import {
  NOTIFICATION_ROUTE_KINDS,
  notificationRoutesAreDefault,
  resolveNotificationRoute,
  withDefaultNotificationRoutes,
  withNotificationRoute,
} from "./routes";

const kind = (key: NotificationRouteKey) => {
  const found = NOTIFICATION_ROUTE_KINDS.find((k) => k.key === key);
  if (!found) throw new Error(key);
  return found;
};

const resolved = (preferences: NotificationPreferences) =>
  Object.fromEntries(
    NOTIFICATION_ROUTE_KINDS.map((k) => [k.key, resolveNotificationRoute(preferences, k)]),
  );

describe("resolveNotificationRoute", () => {
  it("uses each kind's default when nothing is set", () => {
    expect(resolved({})).toEqual({
      assigned: "action",
      mentions: "action",
      member_comments: "action",
      agent_failures: "action",
      status_changes: "activity",
      assignee_changes: "activity",
      updates: "activity",
      agent_comments: "activity",
    });
    expect(notificationRoutesAreDefault({ status_changes: "all" })).toBe(true);
  });

  it("follows the recipient's choice", () => {
    const prefs: NotificationPreferences = {
      status_changes: "action",
      mentions: "activity",
      updates: "muted",
    };
    expect(resolveNotificationRoute(prefs, kind("status_changes"))).toBe("action");
    expect(resolveNotificationRoute(prefs, kind("mentions"))).toBe("activity");
    expect(resolveNotificationRoute(prefs, kind("updates"))).toBe("muted");
    expect(notificationRoutesAreDefault(prefs)).toBe(false);
  });

  it("keeps a muted legacy group muting both halves until the new key is set", () => {
    const prefs: NotificationPreferences = { comments: "muted", agent_comments: "activity" };
    expect(resolveNotificationRoute(prefs, kind("member_comments"))).toBe("muted");
    expect(resolveNotificationRoute(prefs, kind("agent_comments"))).toBe("activity");
    expect(resolveNotificationRoute(prefs, kind("mentions"))).toBe("action");
    expect(resolveNotificationRoute({ assignments: "muted" }, kind("assignee_changes"))).toBe("muted");
  });
});

describe("withNotificationRoute", () => {
  it("stores a non-default choice and clears a default one", () => {
    const routed = withNotificationRoute({}, kind("status_changes"), "action");
    expect(routed).toEqual({ status_changes: "action" });
    expect(withNotificationRoute(routed, kind("status_changes"), "activity")).toEqual({});
  });

  it("retires a muted legacy group without unmuting the other kind it covered", () => {
    const next = withNotificationRoute(
      { comments: "muted", system_notifications: "muted" },
      kind("member_comments"),
      "action",
    );
    expect(next).toEqual({ agent_comments: "muted", system_notifications: "muted" });
    expect(resolveNotificationRoute(next, kind("member_comments"))).toBe("action");
    expect(resolveNotificationRoute(next, kind("agent_comments"))).toBe("muted");
  });

  it("keeps a sibling's explicit choice when retiring the legacy group", () => {
    const next = withNotificationRoute(
      { assignments: "muted", assignee_changes: "action" },
      kind("assigned"),
      "activity",
    );
    expect(next).toEqual({ assigned: "activity", assignee_changes: "action" });
  });
});

describe("withDefaultNotificationRoutes", () => {
  it("resets every kind and legacy group, keeping system notifications", () => {
    const next = withDefaultNotificationRoutes({
      comments: "muted",
      status_changes: "action",
      mentions: "muted",
      system_notifications: "muted",
    });
    expect(next).toEqual({ system_notifications: "muted" });
    expect(notificationRoutesAreDefault(next)).toBe(true);
  });
});
