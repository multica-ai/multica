// @vitest-environment node

import { describe, expect, it } from "vitest";
import {
  CONTENT_NOTIFICATION_GROUPS,
  notificationContentGroupState,
  setNotificationContentGroup,
  notificationDeliveryEnabled,
  setNotificationDelivery,
} from "./groups";

describe("notification preference compatibility groups", () => {
  it("exposes exactly four content groups and a separate delivery channel", () => {
    expect(CONTENT_NOTIFICATION_GROUPS.map((group) => group.key)).toEqual([
      "needs_attention",
      "task_agent_progress",
      "comments_mentions",
      "system_health",
    ]);

    expect(notificationDeliveryEnabled({ system_notifications: "muted" })).toBe(
      false,
    );
    expect(notificationDeliveryEnabled({ browser_push: "all" })).toBe(true);
  });

  it("preserves mixed legacy choices until the user explicitly changes a collapsed group", () => {
    const legacy = {
      status_changes: "muted",
      updates: "all",
      agent_activity: "muted",
    } as const;

    expect(notificationContentGroupState(legacy, "task_agent_progress")).toBe(
      "mixed",
    );

    expect(
      setNotificationContentGroup(legacy, "task_agent_progress", true),
    ).toEqual({
      ...legacy,
      task_agent_progress: "all",
    });
  });

  it("maps legacy comments and mentions independently before a canonical override", () => {
    expect(
      notificationContentGroupState({ comments: "muted" }, "comments_mentions"),
    ).toBe("mixed");
    expect(
      notificationContentGroupState(
        { comments: "muted", mentions: "muted" },
        "comments_mentions",
      ),
    ).toBe("muted");
    expect(
      notificationContentGroupState(
        { comments_mentions: "all", comments: "muted", mentions: "muted" },
        "comments_mentions",
      ),
    ).toBe("all");
  });

  it("writes delivery independently of all content groups", () => {
    expect(
      setNotificationDelivery({ needs_attention: "muted" }, false),
    ).toEqual({ needs_attention: "muted", browser_push: "muted" });
  });
});
