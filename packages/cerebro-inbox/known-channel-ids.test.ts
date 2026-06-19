// FIR-1576 — the property that fixes the archived-DM-reappears bug: once an id
// has been seen as a channel, it stays known even after it leaves the list.
import { describe, expect, it } from "vitest";
import { accumulateChannelIds } from "./known-channel-ids";

describe("accumulateChannelIds", () => {
  it("adds new ids and reports that something changed", () => {
    const known = new Set<string>();
    const added = accumulateChannelIds(known, [{ id: "a" }, { id: "b" }]);
    expect(added).toBe(true);
    expect([...known].sort()).toEqual(["a", "b"]);
  });

  it("is a no-op (added=false) when every id is already known", () => {
    const known = new Set(["a", "b"]);
    expect(accumulateChannelIds(known, [{ id: "a" }, { id: "b" }])).toBe(false);
    expect(known.size).toBe(2);
  });

  it("retains an id after the channel leaves the list (archive / re-surface gap)", () => {
    const known = new Set<string>();
    accumulateChannelIds(known, [{ id: "dm-1" }, { id: "dm-2" }]);
    // The channel list refetches WITHOUT dm-1 (it was archived).
    accumulateChannelIds(known, [{ id: "dm-2" }]);
    // dm-1 is still recognised as a channel, so its notification stays folded
    // and a click on it still opens ChannelDetail in the inbox pane.
    expect(known.has("dm-1")).toBe(true);
  });

  it("only adds the genuinely new id on a partial refresh", () => {
    const known = new Set(["dm-1"]);
    const added = accumulateChannelIds(known, [{ id: "dm-1" }, { id: "dm-3" }]);
    expect(added).toBe(true);
    expect([...known].sort()).toEqual(["dm-1", "dm-3"]);
  });
});
