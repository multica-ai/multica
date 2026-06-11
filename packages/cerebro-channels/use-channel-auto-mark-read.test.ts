import { describe, it, expect } from "vitest";
import { shouldMarkChannelRead } from "./use-channel-auto-mark-read";

const base = {
  last: null as { id: string; count: number } | null,
  channelId: "ch-1",
  count: 2,
};

describe("shouldMarkChannelRead", () => {
  it("marks read on a fresh open even when the window is not focused (TECH-3352)", () => {
    // The reported bug: focus flickered at open, so the inbox row stayed unread.
    expect(
      shouldMarkChannelRead({ ...base, freshOpen: true, visible: false, focused: false }),
    ).toBe(true);
  });

  it("marks read on a fresh open when focused", () => {
    expect(
      shouldMarkChannelRead({ ...base, freshOpen: true, visible: true, focused: true }),
    ).toBe(true);
  });

  it("does NOT mark a new message read while unfocused (keeps TECH-3300)", () => {
    expect(
      shouldMarkChannelRead({ ...base, freshOpen: false, visible: true, focused: false }),
    ).toBe(false);
    expect(
      shouldMarkChannelRead({ ...base, freshOpen: false, visible: false, focused: true }),
    ).toBe(false);
  });

  it("marks a new message read while the open thread is focused + visible", () => {
    expect(
      shouldMarkChannelRead({ ...base, freshOpen: false, visible: true, focused: true }),
    ).toBe(true);
  });

  it("does not re-fire for the same (channel, count) already handled", () => {
    expect(
      shouldMarkChannelRead({
        ...base,
        freshOpen: true,
        visible: true,
        focused: true,
        last: { id: "ch-1", count: 2 },
      }),
    ).toBe(false);
  });

  it("re-fires when the unread count changed (new messages arrived)", () => {
    expect(
      shouldMarkChannelRead({
        ...base,
        count: 3,
        freshOpen: false,
        visible: true,
        focused: true,
        last: { id: "ch-1", count: 2 },
      }),
    ).toBe(true);
  });
});
