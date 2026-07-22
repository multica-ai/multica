import { describe, expect, test } from "vitest";
import {
  CHANNELS,
  DEFAULT_CHANNEL_CHOICES,
  DEFAULT_CHANNEL_TRANSPORT,
  SPLIT_TYPES,
  getChannelChoice,
  getChannelTransport,
  getDMExcerpt,
  getNotifyAllMobileInbox,
} from "./routing";

describe("getChannelChoice", () => {
  test("falls back to per-channel default when no preferences", () => {
    expect(getChannelChoice(undefined, "inbox", "issue_assigned")).toBe("on");
    expect(getChannelChoice(undefined, "inbox", "reminder")).toBe("on");
    expect(getChannelChoice(undefined, "notifications", "issue_assigned")).toBe(
      "off",
    );
    expect(getChannelChoice(undefined, "inbox", "new_comment")).toBe("on");
    expect(getChannelChoice(undefined, "mobile", "new_comment")).toBe("on");
    // FIR-308: DM push fires on phone + browser by default, never on mail.
    expect(getChannelChoice(undefined, "mobile", "dm_message")).toBe("on");
    expect(getChannelChoice(undefined, "desktop", "dm_message")).toBe("on");
    expect(getChannelChoice(undefined, "inbox", "dm_message")).toBe("on");
    expect(getChannelChoice(undefined, "mail", "dm_message")).toBe("off");
    expect(getChannelChoice(undefined, "mail", "issue_assigned")).toBe("off");
    expect(getChannelChoice(undefined, "inbox", "system_notification")).toBe(
      "on",
    );
    expect(
      getChannelChoice(undefined, "notifications", "system_notification"),
    ).toBe("off");
  });

  test("falls back to default when notifications block is missing", () => {
    expect(getChannelChoice({ other: 1 }, "inbox", "mentioned")).toBe("on");
  });

  test("honours user override on the right channel + key", () => {
    const prefs = {
      notifications: {
        mobile: { new_comment: "on" },
        inbox: { issue_assigned: "off" },
      },
    };
    expect(getChannelChoice(prefs, "mobile", "new_comment")).toBe("on");
    expect(getChannelChoice(prefs, "inbox", "issue_assigned")).toBe("off");
    // Untouched key on overridden channel still falls back to default.
    expect(getChannelChoice(prefs, "mobile", "mentioned")).toBe("on");
  });

  test("falls back to default for invalid override values", () => {
    const prefs = {
      notifications: {
        mobile: { mentioned: "garbage" },
      },
    };
    expect(getChannelChoice(prefs, "mobile", "mentioned")).toBe("on");
  });

  test("inherits a legacy explicit choice for both split variants", () => {
    const prefs = {
      notifications: { mobile: { new_comment: "off" } },
    };
    expect(
      getChannelChoice(prefs, "mobile", "new_comment.assignee"),
    ).toBe("off");
    expect(
      getChannelChoice(prefs, "mobile", "new_comment.follower"),
    ).toBe("off");
  });

  test("split keys are looked up with the .assignee / .follower suffix as-is", () => {
    expect(
      getChannelChoice(undefined, "inbox", "due_date_changed.assignee"),
    ).toBe("on");
    expect(
      getChannelChoice(undefined, "inbox", "due_date_changed.follower"),
    ).toBe("off");

    expect(
      getChannelChoice(
        undefined,
        "mobile",
        "new_comment.assignee",
      ),
    ).toBe("on");
    expect(
      getChannelChoice(
        undefined,
        "mobile",
        "new_comment.follower",
      ),
    ).toBe("off");
  });
});

describe("SPLIT_TYPES", () => {
  test("separates noisy issue events for assignees and followers", () => {
    expect([...SPLIT_TYPES]).toEqual(
      expect.arrayContaining([
        "new_comment",
        "status_changed",
        "agent_comment_no_tag",
        "agent_comment_member_tag",
        "agent_comment_agent_tag",
      ]),
    );
  });
});

describe("getChannelTransport", () => {
  test("returns the channel default when no preferences", () => {
    expect(getChannelTransport(undefined, "mobile")).toEqual(
      DEFAULT_CHANNEL_TRANSPORT.mobile,
    );
    expect(getChannelTransport(undefined, "desktop")).toEqual(
      DEFAULT_CHANNEL_TRANSPORT.desktop,
    );
  });

  test("merges user overrides on top of the default", () => {
    const prefs = {
      notifications: {
        channels: {
          mobile: { badge: false },
          desktop: { banner: false },
        },
      },
    };
    const mobile = getChannelTransport(prefs, "mobile");
    expect(mobile.badge).toBe(false);
    // sound was not overridden — default still applies.
    expect(mobile.sound).toBe(true);

    const desktop = getChannelTransport(prefs, "desktop");
    expect(desktop.banner).toBe(false);
    expect(desktop.badge).toBe(true);
  });

  test("ignores invalid digest values", () => {
    const prefs = {
      notifications: { channels: { mail: { digest: "garbage" } } },
    };
    expect(getChannelTransport(prefs, "mail").digest).toBe("daily");
  });
});

describe("CHANNELS / DEFAULT_CHANNEL_CHOICES coverage", () => {
  test("every channel has a default entry for every routing key", () => {
    // Derive the canonical key set from the union across all channels, then
    // assert each channel exposes exactly that set. This catches a routing key
    // added to some channels but forgotten on another, and — unlike a hardcoded
    // count — never goes stale when a new notification type is introduced.
    const expectedKeys = [
      ...new Set(
        CHANNELS.flatMap((channel) =>
          Object.keys(DEFAULT_CHANNEL_CHOICES[channel]),
        ),
      ),
    ].sort();
    for (const channel of CHANNELS) {
      const keys = Object.keys(DEFAULT_CHANNEL_CHOICES[channel]).sort();
      expect(keys).toEqual(expectedKeys);
    }
  });
});

describe("getNotifyAllMobileInbox", () => {
  test("returns false when prefs are missing", () => {
    expect(getNotifyAllMobileInbox(undefined)).toBe(false);
    expect(getNotifyAllMobileInbox({})).toBe(false);
  });

  test("returns false when notifications block is missing", () => {
    expect(getNotifyAllMobileInbox({ other: 1 })).toBe(false);
  });

  test("returns false when toggle is unset, false, or non-boolean", () => {
    expect(getNotifyAllMobileInbox({ notifications: {} })).toBe(false);
    expect(
      getNotifyAllMobileInbox({
        notifications: { notify_all_mobile_inbox: false },
      }),
    ).toBe(false);
    expect(
      getNotifyAllMobileInbox({
        notifications: { notify_all_mobile_inbox: "true" },
      }),
    ).toBe(false);
  });

  test("returns true only for the literal boolean true", () => {
    expect(
      getNotifyAllMobileInbox({
        notifications: { notify_all_mobile_inbox: true },
      }),
    ).toBe(true);
  });
});

describe("getDMExcerpt", () => {
  test("defaults to false (privacy) when prefs or toggle are missing", () => {
    expect(getDMExcerpt(undefined)).toBe(false);
    expect(getDMExcerpt({})).toBe(false);
    expect(getDMExcerpt({ other: 1 })).toBe(false);
    expect(getDMExcerpt({ notifications: {} })).toBe(false);
    expect(getDMExcerpt({ notifications: { dm_excerpt: "true" } })).toBe(false);
    expect(getDMExcerpt({ notifications: { dm_excerpt: false } })).toBe(false);
  });

  test("returns true only for the literal boolean true", () => {
    expect(getDMExcerpt({ notifications: { dm_excerpt: true } })).toBe(true);
  });
});
