// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { addWindowListener, removeWindowListener } from "./window-events";

describe("window listeners", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("attaches and detaches on a DOM window", () => {
    const listener = vi.fn();
    addWindowListener("online", listener);
    window.dispatchEvent(new Event("online"));
    removeWindowListener("online", listener);
    window.dispatchEvent(new Event("online"));
    expect(listener).toHaveBeenCalledTimes(1);
  });

  it("does nothing where `window` is a global object without DOM events", () => {
    vi.stubGlobal("window", {});
    expect(() => addWindowListener("online", () => {})).not.toThrow();
    expect(() => removeWindowListener("online", () => {})).not.toThrow();
  });
});
