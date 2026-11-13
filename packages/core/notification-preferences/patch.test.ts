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

  it("derives Google Chat webhook changes independently of mute groups", () => {
    expect(
      deriveNotificationPreferencePatch(
        { comments: "muted" },
        {
          comments: "muted",
          google_chat_webhook:
            "https://chat.googleapis.com/v1/spaces/AAAA/messages?key=k&token=t",
        },
      ),
    ).toEqual({
      google_chat_webhook:
        "https://chat.googleapis.com/v1/spaces/AAAA/messages?key=k&token=t",
    });

    expect(
      deriveNotificationPreferencePatch(
        { google_chat_webhook: "https://chat.googleapis.com/••••" },
        {},
      ),
    ).toEqual({ google_chat_webhook: "" });
  });

  it("never places a raw Google Chat webhook in the optimistic cache", () => {
    const raw =
      "https://chat.googleapis.com/v1/spaces/AAAA/messages?key=k&token=t";
    const applied = applyNotificationPreferencePatch({}, {
      google_chat_webhook: raw,
    });

    expect(applied.google_chat_webhook).toBe("••••");
    expect(JSON.stringify(applied)).not.toContain("token=t");
    expect(
      rollbackNotificationPreferencePatch(
        applied,
        { google_chat_webhook: raw },
        {},
      ),
    ).toEqual({});
  });

  it("keeps a later Google Chat credential change when an earlier write fails", () => {
    expect(
      rollbackNotificationPreferencePatch(
        {},
        {
          google_chat_webhook:
            "https://chat.googleapis.com/v1/spaces/AAAA/messages?key=k&token=t",
        },
        { google_chat_webhook: "https://chat.googleapis.com/old-mask" },
      ),
    ).toEqual({});
  });
});
