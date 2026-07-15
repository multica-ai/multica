import { act, fireEvent } from "@testing-library/react";
import type { HTMLAttributes, ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import {
  Sidebar,
  SidebarProvider,
  SidebarRail,
  useSidebar,
} from "@multica/ui/components/ui/sidebar";
import { renderWithI18n } from "../test/i18n";

vi.mock("motion/react", () => ({
  motion: {
    div: ({
      animate: _animate,
      children,
      initial: _initial,
      transition,
      ...props
    }: HTMLAttributes<HTMLDivElement> & {
      animate?: unknown;
      children?: ReactNode;
      initial?: unknown;
      transition?: unknown;
    }) => (
      <div data-motion-transition={JSON.stringify(transition)} {...props}>
        {children}
      </div>
    ),
  },
  useReducedMotion: () => false,
}));

describe("left sidebar resizing", () => {
  let animationFrames: Map<number, FrameRequestCallback>;
  let nextAnimationFrameId: number;

  beforeEach(() => {
    localStorage.clear();
    animationFrames = new Map();
    nextAnimationFrameId = 1;

    vi.stubGlobal("requestAnimationFrame", (callback: FrameRequestCallback) => {
      const id = nextAnimationFrameId++;
      animationFrames.set(id, callback);
      return id;
    });
    vi.stubGlobal("cancelAnimationFrame", (id: number) => {
      animationFrames.delete(id);
    });
  });

  afterEach(() => {
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
  });

  it("updates width once per frame without rerendering stable sidebar consumers", () => {
    const stableConsumerRender = vi.fn();
    const setItem = vi.spyOn(Storage.prototype, "setItem");

    function StableSidebarConsumer() {
      useSidebar();
      stableConsumerRender();
      return null;
    }

    const { container } = renderWithI18n(
      <SidebarProvider>
        <Sidebar>
          <StableSidebarConsumer />
          <SidebarRail />
        </Sidebar>
      </SidebarProvider>,
    );

    const wrapper = container.querySelector<HTMLElement>("[data-slot='sidebar-wrapper']")!;
    const sidebarContainer = container.querySelector<HTMLElement>("[data-slot='sidebar-container']")!;
    const sidebarGap = container.querySelector<HTMLElement>("[data-slot='sidebar-gap']")!;
    const rail = container.querySelector<HTMLButtonElement>("[data-slot='sidebar-rail']")!;

    vi.spyOn(sidebarContainer, "getBoundingClientRect").mockReturnValue({
      bottom: 0,
      height: 0,
      left: 0,
      right: 256,
      top: 0,
      width: 256,
      x: 0,
      y: 0,
      toJSON: () => ({}),
    });

    fireEvent.mouseDown(rail, { clientX: 256 });

    expect(sidebarGap).toHaveAttribute("data-motion-transition", JSON.stringify({ duration: 0 }));

    fireEvent.mouseMove(document, { clientX: 280 });
    fireEvent.mouseMove(document, { clientX: 300 });

    expect(animationFrames).toHaveLength(1);
    expect(wrapper.style.getPropertyValue("--sidebar-width")).toBe("256px");
    expect(setItem).not.toHaveBeenCalled();

    act(() => {
      const frame = animationFrames.entries().next().value;
      if (!frame) throw new Error("Expected a scheduled sidebar resize frame");
      const [id, callback] = frame;
      animationFrames.delete(id);
      callback(16);
    });

    expect(wrapper.style.getPropertyValue("--sidebar-width")).toBe("300px");
    expect(stableConsumerRender).toHaveBeenCalledTimes(1);
    expect(setItem).not.toHaveBeenCalled();

    fireEvent.mouseUp(document);

    expect(setItem).toHaveBeenCalledTimes(1);
    expect(setItem).toHaveBeenCalledWith("sidebar_width", "300");
    expect(sidebarGap).toHaveAttribute(
      "data-motion-transition",
      expect.stringContaining('"type":"spring"'),
    );
  });
});
