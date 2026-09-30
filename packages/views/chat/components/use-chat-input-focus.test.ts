import { afterEach, describe, expect, it } from "vitest";
import { act, renderHook } from "@testing-library/react";
import type { RefObject } from "react";
import { isEditableShortcutTarget } from "@multica/core/shortcuts";
import { useChatInputFocus } from "./use-chat-input-focus";

/** Stands in for the floating window: a container holding the composer. */
function mountWindow() {
  const container = document.createElement("div");
  const composer = document.createElement("input");
  composer.type = "text";
  container.append(composer);
  const outside = document.createElement("button");
  document.body.append(container, outside);
  const ref: RefObject<HTMLElement | null> = { current: container };
  return { container, composer, outside, ref };
}

afterEach(() => {
  document.body.replaceChildren();
});

/** For the cases where the window's element plays no part. */
const NO_CONTAINER: RefObject<HTMLElement | null> = { current: null };

describe("useChatInputFocus", () => {
  it("stays inert on mount, whether the window starts closed or open", () => {
    expect(
      renderHook(() => useChatInputFocus(false, NO_CONTAINER)).result.current.focusRequest,
    ).toBe(0);
    // A persisted "open" preference must not steal focus from the page the user
    // just loaded — ChatWindow is mounted (hidden) even while closed.
    expect(
      renderHook(() => useChatInputFocus(true, NO_CONTAINER)).result.current.focusRequest,
    ).toBe(0);
  });

  it("requests focus on every closed → open transition", () => {
    const { result, rerender } = renderHook(
      ({ isOpen }: { isOpen: boolean }) => useChatInputFocus(isOpen, NO_CONTAINER),
      { initialProps: { isOpen: false } },
    );

    rerender({ isOpen: true });
    expect(result.current.focusRequest).toBe(1);

    // Re-renders that don't change `isOpen` must not re-focus: the user may
    // have clicked into the message list or the agent picker since.
    rerender({ isOpen: true });
    expect(result.current.focusRequest).toBe(1);

    rerender({ isOpen: false });
    expect(result.current.focusRequest).toBe(1);

    rerender({ isOpen: true });
    expect(result.current.focusRequest).toBe(2);
  });

  it("lets callers bump the nonce for new chats and agent switches", () => {
    const { result } = renderHook(() => useChatInputFocus(true, NO_CONTAINER));

    act(() => result.current.requestInputFocus());
    expect(result.current.focusRequest).toBe(1);
    act(() => result.current.requestInputFocus());
    expect(result.current.focusRequest).toBe(2);
  });
});

// Issue #8994: the window is only hidden while closed (opacity + pointer
// events), so without an explicit hand-back the composer keeps
// `document.activeElement` and every global shortcut that defers to an
// editable target — `C` for a new issue — stays dead.
describe("useChatInputFocus — releasing the composer on close", () => {
  it("hands focus back to whatever held it before the window opened", () => {
    const { composer, outside, ref } = mountWindow();
    outside.focus();
    const { rerender } = renderHook(
      ({ isOpen }: { isOpen: boolean }) => useChatInputFocus(isOpen, ref),
      { initialProps: { isOpen: false } },
    );

    rerender({ isOpen: true });
    // What ChatInput does once `focusRequest` lands.
    composer.focus();
    expect(document.activeElement).toBe(composer);

    rerender({ isOpen: false });
    expect(document.activeElement).toBe(outside);
    expect(isEditableShortcutTarget(document.activeElement)).toBe(false);
  });

  it("releases the composer when the element that had focus is gone", () => {
    const { composer, outside, ref } = mountWindow();
    outside.focus();
    const { rerender } = renderHook(
      ({ isOpen }: { isOpen: boolean }) => useChatInputFocus(isOpen, ref),
      { initialProps: { isOpen: false } },
    );

    rerender({ isOpen: true });
    composer.focus();
    outside.remove();

    rerender({ isOpen: false });
    expect(document.activeElement).not.toBe(composer);
    expect(isEditableShortcutTarget(document.activeElement)).toBe(false);
  });

  it("leaves focus alone when the window no longer owns it", () => {
    const { outside, ref } = mountWindow();
    const { rerender } = renderHook(
      ({ isOpen }: { isOpen: boolean }) => useChatInputFocus(isOpen, ref),
      { initialProps: { isOpen: false } },
    );

    rerender({ isOpen: true });
    // The user clicked into the page behind the overlay before closing.
    outside.focus();

    rerender({ isOpen: false });
    expect(document.activeElement).toBe(outside);
  });
});
