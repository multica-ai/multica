// @vitest-environment jsdom
import { describe, expect, it, vi } from "vitest";
import { listenForInboxDefaultTab, requestInboxDefaultTab } from "./navigation-intent";

describe("inbox navigation intent", () => {
  it("notifies listeners when the inbox link asks for the default tab", () => {
    const listener = vi.fn();
    const cleanup = listenForInboxDefaultTab(listener);

    requestInboxDefaultTab();

    expect(listener).toHaveBeenCalledTimes(1);
    cleanup();
    requestInboxDefaultTab();
    expect(listener).toHaveBeenCalledTimes(1);
  });

  it("replays a pending default-tab request when the inbox mounts after the click", () => {
    requestInboxDefaultTab();

    const listener = vi.fn();
    const cleanup = listenForInboxDefaultTab(listener);

    expect(listener).toHaveBeenCalledTimes(1);
    cleanup();
  });
});
