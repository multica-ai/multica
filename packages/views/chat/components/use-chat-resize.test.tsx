import { useRef } from "react";
import { act, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createChatStore, registerChatStore, useChatStore } from "@multica/core/chat";
import { useChatResize } from "./use-chat-resize";

const memory = new Map<string, string>();
registerChatStore(
  createChatStore({
    storage: {
      getItem: (k) => memory.get(k) ?? null,
      setItem: (k, v) => void memory.set(k, v),
      removeItem: (k) => void memory.delete(k),
    },
  }),
);

const observers: Array<() => void> = [];
const parentSize = { width: 1200, height: 900 };
let renders = 0;

function Harness() {
  const ref = useRef<HTMLDivElement>(null);
  const { renderWidth } = useChatResize(ref);
  renders += 1;
  return (
    <div data-testid="parent">
      <div ref={ref} data-testid="window" data-width={renderWidth} />
    </div>
  );
}

function resizeParent(width: number) {
  parentSize.width = width;
  act(() => {
    for (const fire of observers) fire();
  });
}

beforeEach(() => {
  observers.length = 0;
  parentSize.width = 1200;
  parentSize.height = 900;
  renders = 0;
  vi.stubGlobal(
    "ResizeObserver",
    class {
      constructor(callback: () => void) {
        observers.push(callback);
      }
      observe() {}
      disconnect() {}
    },
  );
  vi.spyOn(HTMLElement.prototype, "clientWidth", "get").mockImplementation(() => parentSize.width);
  vi.spyOn(HTMLElement.prototype, "clientHeight", "get").mockImplementation(() => parentSize.height);
  useChatStore.setState({ chatWidth: 380, chatHeight: 600, isExpanded: false });
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("useChatResize", () => {
  it("does not re-render while the container resizes around a window that fits", () => {
    render(<Harness />);
    const settled = renders;

    resizeParent(1000);
    resizeParent(800);

    expect(renders).toBe(settled);
    expect(screen.getByTestId("window")).toHaveAttribute("data-width", "380");
  });

  it("re-renders once the container clamps the window", () => {
    render(<Harness />);

    resizeParent(400);

    expect(screen.getByTestId("window")).toHaveAttribute("data-width", "360");
  });

  it("follows the container while expanded", () => {
    useChatStore.setState({ isExpanded: true });
    render(<Harness />);
    expect(screen.getByTestId("window")).toHaveAttribute("data-width", "1080");

    resizeParent(1000);

    expect(screen.getByTestId("window")).toHaveAttribute("data-width", "900");
  });
});
