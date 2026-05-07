import { describe, expect, test } from "vitest";
import {
  CHANNELS,
  DEFAULT_CHANNEL_CHOICES,
  DEFAULT_CHANNEL_TRANSPORT,
  getChannelChoice,
  getChannelTransport,
} from "./routing";

describe("getChannelChoice", () => {
  test("falls back to per-channel default when no preferences", () => {
    expect(getChannelChoice(undefined, "inbox", "issue_assigned")).toBe("on");
    expect(getChannelChoice(undefined, "notifications", "issue_assigned")).toBe(
      "off",
    );
    expect(getChannelChoice(undefined, "mobile", "new_comment")).toBe("off");
    expect(getChannelChoice(undefined, "mail", "issue_assigned")).toBe("off");
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

  test("split keys are looked up with the .assignee / .follower suffix as-is", () => {
    expect(
      getChannelChoice(undefined, "inbox", "due_date_changed.assignee"),
    ).toBe("on");
    expect(
      getChannelChoice(undefined, "inbox", "due_date_changed.follower"),
    ).toBe("off");
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
    for (const channel of CHANNELS) {
      const defaults = DEFAULT_CHANNEL_CHOICES[channel];
      expect(Object.keys(defaults).length).toBe(12);
    }
  });
});
