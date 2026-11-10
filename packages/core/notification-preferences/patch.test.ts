import { describe, expect, it } from "vitest";
import {
  applyNotificationPreferencePatch,
  deriveNotificationPreferencePatch,
  rollbackNotificationPreferencePatch,
} from "./patch";

describe("notification preference patches", () => {
  it("persists, enables and rolls back chat replies independently of comments", () => {
    const current = { comments: "muted" } as const;
    const muted = { ...current, chat_replies: "muted" } as const;
    expect(deriveNotificationPreferencePatch(current, muted)).toEqual({ chat_replies: "muted" });
    expect(applyNotificationPreferencePatch(current, { chat_replies: "muted" })).toEqual(muted);
    expect(deriveNotificationPreferencePatch(muted, current)).toEqual({ chat_replies: "all" });
    expect(applyNotificationPreferencePatch(muted, { chat_replies: "all" })).toEqual(current);
    expect(rollbackNotificationPreferencePatch(muted, { chat_replies: "muted" }, current)).toEqual(current);
  });

  it("sends only the changed key when another muted preference is present", () => {
    expect(
      deriveNotificationPreferencePatch(
        { status_changes: "muted" },
        { status_changes: "muted", comments: "muted" },
      ),
    ).toEqual({ comments: "muted" });
  });

  it("uses an explicit all value when enabling a previously muted group", () => {
    expect(
      deriveNotificationPreferencePatch(
        { status_changes: "muted", comments: "muted" },
        { comments: "muted" },
      ),
    ).toEqual({ status_changes: "all" });
  });

  it("merges optimistic patches without dropping unrelated preferences", () => {
    expect(
      applyNotificationPreferencePatch(
        { status_changes: "muted" },
        { comments: "muted" },
      ),
    ).toEqual({ status_changes: "muted", comments: "muted" });
  });

  it("carries the mentions group independently of comments", () => {
    // The point of the split (#6468): muting comment volume must not travel
    // to mentions. A missing key in NOTIFICATION_GROUP_KEYS would make the
    // toggle silently un-sendable, so derive and apply are both covered.
    expect(
      deriveNotificationPreferencePatch(
        { comments: "muted" },
        { comments: "muted", mentions: "muted" },
      ),
    ).toEqual({ mentions: "muted" });

    expect(
      deriveNotificationPreferencePatch({ comments: "muted" }, {}),
    ).toEqual({ comments: "all" });

    expect(
      applyNotificationPreferencePatch(
        { comments: "muted" },
        { mentions: "muted" },
      ),
    ).toEqual({ comments: "muted", mentions: "muted" });
  });

  it("does not roll back a key that a later mutation already changed", () => {
    expect(
      rollbackNotificationPreferencePatch(
        { comments: "muted" },
        { comments: "muted" },
        {},
      ),
    ).toEqual({});

    expect(
      rollbackNotificationPreferencePatch(
        {},
        { comments: "muted" },
        {},
      ),
    ).toEqual({});
  });
  it("diffs inbox mode and focused keys against their own defaults", () => {
    expect(
      deriveNotificationPreferencePatch(
        { comments: "muted" },
        { comments: "muted", inbox_mode: "focused" },
      ),
    ).toEqual({ inbox_mode: "focused" });

    expect(
      deriveNotificationPreferencePatch(
        { inbox_mode: "focused" },
        { inbox_mode: "focused", focused_blocked: "off" },
      ),
    ).toEqual({ focused_blocked: "off" });

    expect(
      deriveNotificationPreferencePatch(
        { inbox_mode: "focused", focused_blocked: "off" },
        {},
      ),
    ).toEqual({ inbox_mode: "all", focused_blocked: "on" });
  });

  it("keeps inbox mode and focused defaults sparse when applied", () => {
    expect(
      applyNotificationPreferencePatch(
        { comments: "muted", focused_mentioned: "off" },
        { inbox_mode: "focused", focused_mentioned: "on" },
      ),
    ).toEqual({ comments: "muted", inbox_mode: "focused" });

    expect(
      applyNotificationPreferencePatch(
        { inbox_mode: "focused" },
        { inbox_mode: "all", focused_assigned: "off" },
      ),
    ).toEqual({ focused_assigned: "off" });
  });

  it("rolls back a focused key to its previous value", () => {
    expect(
      rollbackNotificationPreferencePatch(
        { inbox_mode: "focused", focused_blocked: "off" },
        { focused_blocked: "off" },
        { inbox_mode: "focused" },
      ),
    ).toEqual({ inbox_mode: "focused" });

    expect(
      rollbackNotificationPreferencePatch(
        { inbox_mode: "focused" },
        { inbox_mode: "focused" },
        { focused_thread_reply: "off" },
      ),
    ).toEqual({});
  });
});
