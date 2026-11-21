import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import * as Y from "yjs";
import { Awareness } from "y-protocols/awareness";
import { coalesceAwarenessChanges } from "./coalesced-awareness";

describe("coalesceAwarenessChanges", () => {
  let awareness: Awareness;

  beforeEach(() => {
    vi.useFakeTimers();
    awareness = new Awareness(new Y.Doc());
  });

  afterEach(() => {
    awareness.destroy();
    vi.useRealTimers();
  });

  it("delivers a burst of changes as one call", () => {
    const view = coalesceAwarenessChanges(awareness, 50);
    const listener = vi.fn();
    view.on("change", listener);

    for (let i = 0; i < 10; i++) view.setLocalStateField("cursor", i);
    expect(listener).not.toHaveBeenCalled();

    vi.advanceTimersByTime(50);
    expect(listener).toHaveBeenCalledTimes(1);
    expect(awareness.getLocalState()?.cursor).toBe(9);
  });

  it("passes other events and reads straight through", () => {
    const view = coalesceAwarenessChanges(awareness, 50);
    const onUpdate = vi.fn();
    view.on("update", onUpdate);

    view.setLocalStateField("user", { name: "Ada" });
    expect(onUpdate).toHaveBeenCalledTimes(1);
    expect(view.clientID).toBe(awareness.clientID);
    expect(view.getStates().get(awareness.clientID)?.user).toEqual({ name: "Ada" });
  });

  it("drops a pending call once the listener is removed", () => {
    const view = coalesceAwarenessChanges(awareness, 50);
    const listener = vi.fn();
    view.on("change", listener);

    view.setLocalStateField("cursor", 1);
    view.off("change", listener);
    vi.advanceTimersByTime(50);
    view.setLocalStateField("cursor", 2);
    vi.advanceTimersByTime(50);

    expect(listener).not.toHaveBeenCalled();
  });
});
