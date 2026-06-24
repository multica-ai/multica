import { describe, it, expect } from "vitest";
import { shouldMarkChannelReadOnScroll } from "./use-channel-scroll-mark-read";

const base = {
  enabled: true,
  channelId: "ch-1",
  hasUnread: true,
  atBottom: true,
  visible: true,
  focused: true,
  last: null as string | null,
  signature: "0:2026-06-25T10:00:00Z",
};

describe("shouldMarkChannelReadOnScroll", () => {
  it("marks read when at the bottom with unread, focused + visible", () => {
    expect(shouldMarkChannelReadOnScroll(base)).toBe(true);
  });

  it("does NOT mark read while not yet scrolled to the bottom", () => {
    expect(shouldMarkChannelReadOnScroll({ ...base, atBottom: false })).toBe(false);
  });

  it("does NOT mark read when there is nothing unread", () => {
    expect(shouldMarkChannelReadOnScroll({ ...base, hasUnread: false })).toBe(false);
  });

  it("does NOT mark read when the feature is disabled (DM / flag off)", () => {
    expect(shouldMarkChannelReadOnScroll({ ...base, enabled: false })).toBe(false);
  });

  it("does NOT mark read in a background tab (mirrors the TECH-3300 guard)", () => {
    expect(shouldMarkChannelReadOnScroll({ ...base, visible: false })).toBe(false);
    expect(shouldMarkChannelReadOnScroll({ ...base, focused: false })).toBe(false);
  });

  it("does not re-fire for the same (channel, signature) already handled", () => {
    expect(
      shouldMarkChannelReadOnScroll({ ...base, last: "ch-1:0:2026-06-25T10:00:00Z" }),
    ).toBe(false);
  });

  it("re-fires when a new message changes the signature", () => {
    expect(
      shouldMarkChannelReadOnScroll({
        ...base,
        signature: "0:2026-06-25T10:05:00Z",
        last: "ch-1:0:2026-06-25T10:00:00Z",
      }),
    ).toBe(true);
  });
});
