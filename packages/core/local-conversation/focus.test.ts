// @vitest-environment node
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { LOCAL_V2_FOCUS_STABLE_MS, LocalV2FocusTracker } from "./focus";

function tracker(online = { value: true }) {
  const sent: [string, boolean][] = [];
  const focus = new LocalV2FocusTracker((id, focused) => {
    if (!online.value) return false;
    sent.push([id, focused]);
    return true;
  });
  return { focus, sent, online };
}

describe("LocalV2FocusTracker", () => {
  beforeEach(() => { vi.useFakeTimers(); });
  afterEach(() => { vi.useRealTimers(); });

  it("says nothing about a conversation passed through", () => {
    const { focus, sent } = tracker();
    const release = focus.hold("a");
    vi.advanceTimersByTime(LOCAL_V2_FOCUS_STABLE_MS - 1);
    release();
    vi.advanceTimersByTime(LOCAL_V2_FOCUS_STABLE_MS);
    expect(sent).toEqual([]);
  });

  it("reports a stable focus once and its end", async () => {
    const { focus, sent } = tracker();
    const release = focus.hold("a");
    vi.advanceTimersByTime(LOCAL_V2_FOCUS_STABLE_MS);
    expect(sent).toEqual([["a", true]]);
    release();
    release();
    await vi.advanceTimersByTimeAsync(0);
    expect(sent).toEqual([["a", true], ["a", false]]);
  });

  it("keeps a conversation focused while any view holds it, whichever order they swap in", () => {
    const { focus, sent } = tracker();
    const first = focus.hold("a");
    vi.advanceTimersByTime(LOCAL_V2_FOCUS_STABLE_MS);
    const second = focus.hold("a");
    first();
    vi.advanceTimersByTime(LOCAL_V2_FOCUS_STABLE_MS);
    expect(sent).toEqual([["a", true]]);
    second();
  });

  it("sends the current focus again after a reconnect", () => {
    const { focus, sent } = tracker();
    focus.hold("a");
    vi.advanceTimersByTime(LOCAL_V2_FOCUS_STABLE_MS);
    focus.reconnected();
    expect(sent).toEqual([["a", true], ["a", true]]);
  });

  it("delivers a blur after the reconnect when the connection was down", async () => {
    const { focus, sent, online } = tracker();
    const release = focus.hold("a");
    vi.advanceTimersByTime(LOCAL_V2_FOCUS_STABLE_MS);
    online.value = false;
    release();
    await vi.advanceTimersByTimeAsync(0);
    online.value = true;
    focus.reconnected();
    await vi.advanceTimersByTimeAsync(0);
    expect(sent).toEqual([["a", true], ["a", false]]);
  });

  it("does not send a stale blur for a conversation focused again", async () => {
    const { focus, sent, online } = tracker();
    const release = focus.hold("a");
    vi.advanceTimersByTime(LOCAL_V2_FOCUS_STABLE_MS);
    online.value = false;
    release();
    await vi.advanceTimersByTimeAsync(0);
    focus.hold("a");
    vi.advanceTimersByTime(LOCAL_V2_FOCUS_STABLE_MS);
    online.value = true;
    focus.reconnected();
    expect(sent).toEqual([["a", true], ["a", true]]);
  });
});
