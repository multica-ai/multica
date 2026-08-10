import { act, fireEvent, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it } from "vitest";

import {
  Sidebar,
  SidebarProvider,
  useSidebar,
} from "@multica/ui/components/ui/sidebar";
import { renderWithI18n } from "../test/i18n";

// A width-driven `matchMedia`: the provider watches the `lg`–`xl` band and
// `useIsCompact` watches `max-width: 1023px`, so both have to answer against
// the same viewport and both have to be notified when it changes.
type Listener = (e: MediaQueryListEvent) => void;

let width = 1440;
const queries = new Set<{ query: string; listeners: Set<Listener> }>();
const realMatchMedia = window.matchMedia;

function evaluate(query: string, viewport: number) {
  const min = /min-width:\s*(\d+)px/.exec(query);
  const max = /max-width:\s*(\d+)px/.exec(query);
  if (min && viewport < Number(min[1])) return false;
  if (max && viewport > Number(max[1])) return false;
  return true;
}

function setWidth(next: number) {
  width = next;
  Object.defineProperty(window, "innerWidth", {
    configurable: true,
    value: next,
    writable: true,
  });
  act(() => {
    for (const entry of queries) {
      const event = { matches: evaluate(entry.query, next) } as MediaQueryListEvent;
      for (const listener of entry.listeners) listener(event);
    }
  });
}

function Probe() {
  const { state, toggleSidebar } = useSidebar();
  return (
    <button type="button" data-testid="state" onClick={toggleSidebar}>
      {state}
    </button>
  );
}

function state() {
  return screen.getByTestId("state").textContent;
}

describe("sidebar auto-collapse between lg and xl", () => {
  beforeEach(() => {
    queries.clear();
    window.matchMedia = ((query: string) => {
      const entry = { query, listeners: new Set<Listener>() };
      queries.add(entry);
      return {
        get matches() {
          return evaluate(query, width);
        },
        media: query,
        onchange: null,
        addListener: () => {},
        removeListener: () => {},
        addEventListener: (_: string, listener: Listener) =>
          entry.listeners.add(listener),
        removeEventListener: (_: string, listener: Listener) =>
          entry.listeners.delete(listener),
        dispatchEvent: () => false,
      } as unknown as MediaQueryList;
    }) as typeof window.matchMedia;
    setWidth(1440);
  });

  afterEach(() => {
    window.matchMedia = realMatchMedia;
  });

  it("leaves the nav alone at desktop widths", () => {
    renderWithI18n(
      <SidebarProvider>
        <Probe />
      </SidebarProvider>,
    );

    expect(state()).toBe("expanded");
  });

  it("starts collapsed when the page opens inside the band", () => {
    setWidth(1100);
    renderWithI18n(
      <SidebarProvider>
        <Probe />
      </SidebarProvider>,
    );

    expect(state()).toBe("collapsed");
  });

  it("collapses on the way in and restores on the way out", () => {
    renderWithI18n(
      <SidebarProvider>
        <Probe />
      </SidebarProvider>,
    );

    setWidth(1100);
    expect(state()).toBe("collapsed");

    setWidth(1440);
    expect(state()).toBe("expanded");
  });

  it("keeps a manual re-open until the next crossing", () => {
    // Automating the crossings must not mean fighting the user inside the
    // band: once they bring the nav back it stays until the width changes
    // bands again.
    renderWithI18n(
      <SidebarProvider>
        <Probe />
      </SidebarProvider>,
    );

    setWidth(1100);
    fireEvent.click(screen.getByTestId("state"));
    expect(state()).toBe("expanded");

    setWidth(1200);
    expect(state()).toBe("expanded");
  });

  it("gates the in-flow sidebar on the same breakpoint the hook uses", () => {
    // `useIsCompact()` resolves after the first paint, so between the two
    // breakpoints the CSS gate alone decides that frame. While it said `md`
    // and the hook said `lg`, every load in 768–1023 painted a 256px sidebar
    // before collapsing it into a sheet. jsdom applies no stylesheet, so the
    // agreement is asserted on the class itself.
    const { container } = renderWithI18n(
      <SidebarProvider>
        <Sidebar />
      </SidebarProvider>,
    );

    const root = container.querySelector<HTMLElement>("[data-slot='sidebar']")!;
    const inner = container.querySelector<HTMLElement>(
      "[data-slot='sidebar-container']",
    )!;

    expect(root.className).toContain("lg:block");
    expect(root.className).not.toContain("md:block");
    expect(inner.className).toContain("lg:flex");
    expect(inner.className).not.toContain("md:flex");
  });

  it("does not touch the collapsed state below the band", () => {
    // Under 1024 the nav is a sheet, which has its own open state — the
    // in-flow state must be left where the user last put it.
    renderWithI18n(
      <SidebarProvider>
        <Probe />
      </SidebarProvider>,
    );

    setWidth(851);

    expect(state()).toBe("expanded");
  });

  it("never persists the open state, however it changed", () => {
    // The guard for the trap upstream shadcn sets: it writes a `sidebar_state`
    // cookie in `setOpen` and seeds `defaultOpen` from it server-side. Because
    // the auto-collapse above drives `setOpen(false)` from the viewport rather
    // than from the user, wiring that pair back up would carry a collapse
    // nobody asked for out of this band and into the next session on a wider
    // display. Asserted for both paths — a real toggle and a crossing — since
    // it is the crossing that makes persistence wrong here.
    renderWithI18n(
      <SidebarProvider>
        <Probe />
      </SidebarProvider>,
    );

    fireEvent.click(screen.getByTestId("state"));
    expect(document.cookie).not.toContain("sidebar_state");

    setWidth(1100);
    expect(state()).toBe("collapsed");
    expect(document.cookie).not.toContain("sidebar_state");
  });
});
