import { act, fireEvent } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { SidebarProvider } from "@multica/ui/components/ui/sidebar";
import { renderWithI18n } from "../test/i18n";
import { AppShellProvider, useAppShell } from "./app-shell-context";
import { ListColumn } from "./list-column";

vi.mock("../navigation", () => ({
  useNavigation: () => ({ pathname: "/acme/inbox" }),
}));

let shell: ReturnType<typeof useAppShell> = null;
function Reader() {
  shell = useAppShell();
  return null;
}

/** No stored width: the theme's, or 240px when it sets none. */
const UNSTORED_SIDEBAR_WIDTH = "clamp(200px, var(--sidebar-panel-width, 240px), 320px)";

describe("ListColumn", () => {
  it("collapses the list column from its edge, like the space panel", () => {
    const { container } = renderWithI18n(
      <SidebarProvider>
        <AppShellProvider>
          <Reader />
          <ListColumn>list</ListColumn>
        </AppShellProvider>
      </SidebarProvider>,
    );

    expect(container.querySelector("[data-slot='list-column']")).toHaveClass(
      "w-(--sidebar-width)",
    );
    fireEvent.click(container.querySelector("[data-slot='space-panel-resize']")!);
    expect(shell?.listCollapsed).toBe(true);
  });

  it("sizes a list column by its own width and commits drags there", () => {
    localStorage.clear();
    const { container } = renderWithI18n(
      <SidebarProvider>
        <AppShellProvider>
          <ListColumn width="list">list</ListColumn>
        </AppShellProvider>
      </SidebarProvider>,
    );
    const wrapper = container.querySelector<HTMLElement>("[data-slot='sidebar-wrapper']")!;
    const column = container.querySelector<HTMLElement>("[data-slot='list-column']")!;
    expect(column).toHaveClass("w-[min(var(--list-column-width),40vw)]");
    expect(wrapper.style.getPropertyValue("--list-column-width")).toBe("320px");

    const handle = container.querySelector<HTMLElement>("[data-slot='space-panel-resize']")!;
    vi.spyOn(column, "getBoundingClientRect").mockReturnValue({ width: 320 } as DOMRect);
    fireEvent.pointerDown(handle, { button: 0, isPrimary: true, pointerId: 1, clientX: 320 });
    act(() => {
      document.dispatchEvent(new PointerEvent("pointermove", { pointerId: 1, clientX: 700 }));
    });
    expect(column.style.getPropertyValue("--list-column-width")).toBe("480px");
    act(() => {
      document.dispatchEvent(new PointerEvent("pointerup", { pointerId: 1, clientX: 700 }));
    });

    expect(localStorage.getItem("list_column_width")).toBe("480");
    expect(localStorage.getItem("sidebar_width")).toBeNull();
    expect(wrapper.style.getPropertyValue("--list-column-width")).toBe("480px");
    expect(wrapper.style.getPropertyValue("--sidebar-width")).toBe(UNSTORED_SIDEBAR_WIDTH);
  });

  it("restores a stored list width within its bounds", () => {
    localStorage.setItem("list_column_width", "900");
    const { container } = renderWithI18n(
      <SidebarProvider>
        <ListColumn width="list">list</ListColumn>
      </SidebarProvider>,
    );
    const wrapper = container.querySelector<HTMLElement>("[data-slot='sidebar-wrapper']")!;
    expect(wrapper.style.getPropertyValue("--list-column-width")).toBe("480px");
    localStorage.clear();
  });

  it("renders without a resize edge outside the shell", () => {
    const { container } = renderWithI18n(<ListColumn>list</ListColumn>);
    expect(container.querySelector("[data-slot='space-panel-resize']")).toBeNull();
  });

  // Collapsing animates the width rather than unmounting, like the space panel.
  it("stays mounted and inert while collapsed", () => {
    const { container } = renderWithI18n(<ListColumn collapsed>list</ListColumn>);
    const column = container.querySelector("[data-slot='list-column']");
    expect(column).toHaveAttribute("data-state", "collapsed");
    expect(column).toHaveAttribute("inert");
    expect(column).toHaveClass("w-0", "transition-[width]");
    expect(column).toHaveTextContent("list");
  });

  it("floats over the page while its rail item is hovered, then tucks back", () => {
    vi.useFakeTimers();
    try {
      const { container } = renderWithI18n(
        <SidebarProvider>
          <AppShellProvider>
            <Reader />
            <ListColumn collapsed>list</ListColumn>
          </AppShellProvider>
        </SidebarProvider>,
      );
      const column = container.querySelector("[data-slot='list-column']");
      act(() => shell!.requestPeek("inbox"));
      act(() => vi.advanceTimersByTime(200));
      expect(column).not.toHaveAttribute("inert");
      expect(column).toHaveClass("w-0");
      expect(container.querySelector("[data-slot='list-column-peek']")).toHaveTextContent("list");

      act(() => shell!.closePeek());
      expect(column).toHaveAttribute("inert");
      expect(container.querySelector("[data-slot='list-column-peek']")).toBeNull();
    } finally {
      vi.useRealTimers();
    }
  });
});
