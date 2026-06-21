import { describe, it, expect } from "vitest";
import { resolveNotifChannel } from "./use-selected-notif-channel";

const NONE: ReadonlySet<string> = new Set();

describe("resolveNotifChannel", () => {
  it("routes a channel-kind issue to the channel pane (no pending)", () => {
    expect(resolveNotifChannel("c1", "channel", false, NONE)).toEqual({
      isChannel: true,
      kindPending: false,
    });
  });

  it("routes a dm-kind issue to the channel pane", () => {
    expect(resolveNotifChannel("d1", "dm", false, NONE)).toEqual({
      isChannel: true,
      kindPending: false,
    });
  });

  it("treats a plain issue as a normal issue, never a channel", () => {
    expect(resolveNotifChannel("i1", "issue", false, NONE)).toEqual({
      isChannel: false,
      kindPending: false,
    });
  });

  it("holds (kindPending) while the kind is still loading and not yet known", () => {
    // This is the back-nav / first-load race: the channel list cache is empty
    // and the issue query is in flight. We must NOT mount IssueDetail yet, or
    // its FIR-2452 redirect would bounce a channel out of the inbox.
    expect(resolveNotifChannel("x1", undefined, true, NONE)).toEqual({
      isChannel: false,
      kindPending: true,
    });
  });

  it("uses knownChannelIds as a zero-latency fast path even while loading", () => {
    expect(resolveNotifChannel("c1", undefined, true, new Set(["c1"]))).toEqual({
      isChannel: true,
      kindPending: false,
    });
  });

  it("does not hold once loading finishes with a non-channel kind", () => {
    expect(resolveNotifChannel("i1", undefined, false, NONE)).toEqual({
      isChannel: false,
      kindPending: false,
    });
  });

  it("no-ops when there is no issue id (chat / reminder selection)", () => {
    expect(resolveNotifChannel(null, undefined, true, NONE)).toEqual({
      isChannel: false,
      kindPending: false,
    });
  });
});
