import { describe, it, expect } from "vitest";
import { pinnedBucketizer, type PinnedGroup, type InboxBucket } from "./pin-selected-group";

type Row = { id: string; action: string };

const keyOf = (r: Row) => r.id;

// A live bucketizer that reads the row's *current* action — i.e. it changes
// when activity changes the row's action, exactly like the real Action group.
const liveBucketize = (mode: string, r: Row): InboxBucket =>
  mode === "action"
    ? { key: r.action, label: r.action, isFallback: false }
    : { key: "__none__", label: "", isFallback: true };

const ref = () => ({ current: null as PinnedGroup | null });

describe("pinnedBucketizer", () => {
  it("no selection → buckets every row live", () => {
    const pin = ref();
    const b = pinnedBucketizer(liveBucketize, keyOf, "", undefined, ["action", "none"], pin);
    expect(b("action", { id: "a", action: "waiting" }).key).toBe("waiting");
    expect(pin.current).toBeNull();
  });

  it("freezes the open row's bucket so incoming activity can't move it", () => {
    const pin = ref();
    const open = { id: "a", action: "waiting" };
    const rows = [open, { id: "b", action: "running" }];
    // Open row "a" while it is in the "waiting" bucket.
    let b = pinnedBucketizer(liveBucketize, keyOf, "a", rows[0], ["action", "none"], pin);
    expect(b("action", open).key).toBe("waiting");

    // Activity lands: the row's live action flips to "running". Re-run the
    // bucketizer with the same open selection — the bucket must stay frozen.
    const bumped = { id: "a", action: "running" };
    b = pinnedBucketizer(liveBucketize, keyOf, "a", bumped, ["action", "none"], pin);
    expect(b("action", bumped).key).toBe("waiting");
    // Other rows still bucket live.
    expect(b("action", { id: "b", action: "running" }).key).toBe("running");
  });

  it("releases the pin when the message is closed (selection cleared)", () => {
    const pin = ref();
    const open = { id: "a", action: "waiting" };
    pinnedBucketizer(liveBucketize, keyOf, "a", open, ["action", "none"], pin);
    expect(pin.current).not.toBeNull();

    const b = pinnedBucketizer(liveBucketize, keyOf, "", undefined, ["action", "none"], pin);
    const nowRunning = { id: "a", action: "running" };
    expect(b("action", nowRunning).key).toBe("running");
    expect(pin.current).toBeNull();
  });

  it("re-captures when the selection moves to a different row", () => {
    const pin = ref();
    pinnedBucketizer(liveBucketize, keyOf, "a", { id: "a", action: "waiting" }, ["action", "none"], pin);
    const b = pinnedBucketizer(liveBucketize, keyOf, "b", { id: "b", action: "running" }, ["action", "none"], pin);
    expect(pin.current?.key).toBe("b");
    expect(b("action", { id: "b", action: "done" }).key).toBe("running");
  });

  it("re-captures when the user changes the grouping while a row is open", () => {
    const pin = ref();
    const open = { id: "a", action: "waiting" };
    pinnedBucketizer(liveBucketize, keyOf, "a", open, ["action", "none"], pin);
    const sigBefore = pin.current?.sig;
    // User switches grouping; signature changes → fresh capture.
    pinnedBucketizer(liveBucketize, keyOf, "a", open, ["type", "none"], pin);
    expect(pin.current?.sig).not.toBe(sigBefore);
  });

  it("waits rather than freezing a wrong bucket when the open row isn't in the list yet", () => {
    const pin = ref();
    // selectedKey set but entry not present (deep-link race) → no capture.
    const b = pinnedBucketizer(liveBucketize, keyOf, "a", undefined, ["action", "none"], pin);
    expect(pin.current).toBeNull();
    // A live row still buckets normally.
    expect(b("action", { id: "b", action: "running" }).key).toBe("running");
  });

  it("freezes both primary and secondary group dimensions", () => {
    const pin = ref();
    const open = { id: "a", action: "waiting" };
    const twoDim = (mode: string, r: Row): InboxBucket => ({
      key: `${mode}:${r.action}`,
      label: mode,
      isFallback: false,
    });
    let b = pinnedBucketizer(twoDim, keyOf, "a", open, ["action", "type"], pin);
    expect(b("action", open).key).toBe("action:waiting");
    expect(b("type", open).key).toBe("type:waiting");

    const bumped = { id: "a", action: "running" };
    b = pinnedBucketizer(twoDim, keyOf, "a", bumped, ["action", "type"], pin);
    expect(b("action", bumped).key).toBe("action:waiting");
    expect(b("type", bumped).key).toBe("type:waiting");
  });
});
