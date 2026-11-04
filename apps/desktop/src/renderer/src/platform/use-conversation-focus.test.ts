import { renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { ConversationStore } from "./conversation-store";
import { useConversationFocus } from "./use-conversation-focus";

function client() {
  const release = vi.fn();
  const focus = vi.fn(() => release);
  return { client: { focus } as unknown as ConversationStore, focus, release };
}

const setVisibility = (state: DocumentVisibilityState) => {
  Object.defineProperty(document, "visibilityState", { configurable: true, get: () => state });
  document.dispatchEvent(new Event("visibilitychange"));
};

afterEach(() => setVisibility("visible"));

describe("useConversationFocus", () => {
  it("holds only the selected conversation of the visible tab", () => {
    const c = client();
    const { rerender, unmount } = renderHook(({ id, active }) => useConversationFocus(c.client, id, active), { initialProps: { id: null as string | null, active: true } });
    expect(c.focus).not.toHaveBeenCalled();
    rerender({ id: "a", active: false });
    expect(c.focus).not.toHaveBeenCalled();
    rerender({ id: "a", active: true });
    expect(c.focus).toHaveBeenCalledWith("a");
    unmount();
    expect(c.release).toHaveBeenCalledTimes(1);
  });

  it("follows the window's visibility", () => {
    const c = client();
    renderHook(() => useConversationFocus(c.client, "a", true));
    expect(c.focus).toHaveBeenCalledTimes(1);
    setVisibility("hidden");
    expect(c.release).toHaveBeenCalledTimes(1);
    setVisibility("visible");
    expect(c.focus).toHaveBeenCalledTimes(2);
  });
});
