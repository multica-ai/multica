// @vitest-environment node
import { describe, expect, it, vi } from "vitest";
import {
  MainRendererMessageQueue,
  parseMainRendererChannelState,
  parseTabSelectionShortcutKey,
  TAB_SELECTION_SHORTCUT_CHANNEL,
} from "./main-renderer-messages";

function createQueue(live: Set<string> = new Set(["a", "b"])) {
  const send = vi.fn();
  const queue = new MainRendererMessageQueue<string>(send, (window) =>
    live.has(window),
  );
  return { queue, send, live };
}

describe("MainRendererMessageQueue", () => {
  it("holds messages until their matching listener is ready", () => {
    const { queue, send } = createQueue();

    queue.enqueue("a", "auth:token", "token-a");
    queue.setReady("a", "invite:open", true);
    expect(send).not.toHaveBeenCalled();

    queue.setReady("a", "auth:token", true);
    expect(send).toHaveBeenCalledWith("a", "auth:token", "token-a");
  });

  it("delivers immediately while the target's channel is ready", () => {
    const { queue, send } = createQueue();

    queue.setReady("a", "inbox:open", true);
    queue.enqueue("a", "inbox:open", { itemId: "item-1" });

    expect(send).toHaveBeenCalledOnce();
  });

  it("keeps a message for its own window when another window becomes ready", () => {
    const { queue, send } = createQueue();

    queue.enqueue("a", "settings:open", null);
    queue.setReady("b", "settings:open", true);
    expect(send).not.toHaveBeenCalled();

    queue.setReady("a", "settings:open", true);
    expect(send).toHaveBeenCalledWith("a", "settings:open", null);
  });

  it("hands a closed window's pending messages to its successor", () => {
    const { queue, send, live } = createQueue();

    queue.setReady("b", "inbox:open", true);
    queue.enqueue("a", "inbox:open", { itemId: "item-1" });
    live.delete("a");
    queue.release("a", "b");

    expect(send).toHaveBeenCalledWith("b", "inbox:open", { itemId: "item-1" });
  });

  it("delivers to a recreated window when the target closed with no successor", () => {
    const { queue, send, live } = createQueue(new Set(["a"]));

    queue.setReady("a", "invite:open", true);
    live.delete("a");
    queue.release("a", null);
    queue.enqueue("a", "invite:open", "invite-1");
    expect(send).not.toHaveBeenCalled();

    live.add("c");
    queue.setReady("c", "invite:open", true);
    expect(send).toHaveBeenCalledWith("c", "invite:open", "invite-1");
  });

  it("delivers an untargeted message to the first window that becomes ready", () => {
    const { queue, send } = createQueue();

    queue.enqueue(null, "auth:token", "token-1");
    expect(send).not.toHaveBeenCalled();

    queue.setReady("b", "auth:token", true);
    queue.setReady("a", "auth:token", true);
    expect(send).toHaveBeenCalledTimes(1);
    expect(send).toHaveBeenCalledWith("b", "auth:token", "token-1");
  });

  it("can discard account-scoped pending messages", () => {
    const { queue, send } = createQueue();

    queue.enqueue("a", "inbox:open", { itemId: "old-account-item" });
    queue.clear("inbox:open");
    queue.setReady("a", "inbox:open", true);

    expect(send).not.toHaveBeenCalled();
  });
});

describe("parseMainRendererChannelState", () => {
  it("accepts only allowlisted channels with an explicit boolean", () => {
    expect(
      parseMainRendererChannelState({ channel: "auth:token", ready: true }),
    ).toEqual({ channel: "auth:token", ready: true });
    expect(
      parseMainRendererChannelState({ channel: "shell:openExternal", ready: true }),
    ).toBeNull();
    expect(
      parseMainRendererChannelState({ channel: "auth:token", ready: "yes" }),
    ).toBeNull();
    expect(
      parseMainRendererChannelState({
        channel: TAB_SELECTION_SHORTCUT_CHANNEL,
        ready: true,
      }),
    ).toEqual({ channel: TAB_SELECTION_SHORTCUT_CHANNEL, ready: true });
  });
});

describe("parseTabSelectionShortcutKey", () => {
  it.each([1, 2, 3, 4, 5, 6, 7, 8, 9])("accepts shortcut key %i", (key) => {
    expect(parseTabSelectionShortcutKey(key)).toBe(key);
  });

  it.each([0, 10, 1.5, "1", null, undefined])(
    "rejects invalid shortcut payload %j",
    (value) => {
      expect(parseTabSelectionShortcutKey(value)).toBeNull();
    },
  );
});
