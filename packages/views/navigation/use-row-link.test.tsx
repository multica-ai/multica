import { afterEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { NavigationProvider } from "./context";
import { rowLinkInteractiveProps, useRowLink } from "./use-row-link";
import type { NavigationAdapter } from "./types";

function makeAdapter(
  overrides: Partial<NavigationAdapter> = {},
): NavigationAdapter {
  return {
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    pathname: "/",
    searchParams: new URLSearchParams(),
    getShareableUrl: (p) => p,
    ...overrides,
  };
}

function Probe({ href = "/acme/projects/p1" }: { href?: string }) {
  const rowLink = useRowLink();
  return (
    <div role="row" {...rowLink(href)}>
      row
    </div>
  );
}

function renderProbe(adapter: NavigationAdapter) {
  return render(
    <NavigationProvider value={adapter}>
      <Probe />
    </NavigationProvider>,
  );
}

describe("useRowLink", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("pushes on plain left click", () => {
    const push = vi.fn();
    const adapter = makeAdapter({ push });

    renderProbe(adapter);
    fireEvent.click(screen.getByRole("row"));

    expect(push).toHaveBeenCalledWith("/acme/projects/p1");
    expect(push).toHaveBeenCalledTimes(1);
  });

  it("uses openInNewTab instead of push for cmd/ctrl click when available", () => {
    const push = vi.fn();
    const openInNewTab = vi.fn();
    const adapter = makeAdapter({ push, openInNewTab });

    renderProbe(adapter);
    fireEvent.click(screen.getByRole("row"), { metaKey: true });
    fireEvent.click(screen.getByRole("row"), { ctrlKey: true });

    expect(openInNewTab).toHaveBeenCalledTimes(2);
    expect(openInNewTab).toHaveBeenNthCalledWith(1, "/acme/projects/p1", undefined);
    expect(openInNewTab).toHaveBeenNthCalledWith(2, "/acme/projects/p1", undefined);
    expect(push).not.toHaveBeenCalled();
  });

  it("opens a foreground tab for cmd+shift click (spec's 'take me there' modifier)", () => {
    const push = vi.fn();
    const openInNewTab = vi.fn();
    const adapter = makeAdapter({ push, openInNewTab });

    renderProbe(adapter);
    fireEvent.click(screen.getByRole("row"), { metaKey: true, shiftKey: true });

    expect(openInNewTab).toHaveBeenCalledWith("/acme/projects/p1", undefined, {
      activate: true,
    });
    expect(push).not.toHaveBeenCalled();
  });

  it("passes newTabTitle through as the desktop tab label", () => {
    const openInNewTab = vi.fn();
    const adapter = makeAdapter({ openInNewTab });

    function TitledProbe() {
      const rowLink = useRowLink();
      return (
        <div role="row" {...rowLink("/acme/projects/p1", "Roadmap")}>
          row
        </div>
      );
    }
    render(
      <NavigationProvider value={adapter}>
        <TitledProbe />
      </NavigationProvider>,
    );
    fireEvent.click(screen.getByRole("row"), { metaKey: true });

    expect(openInNewTab).toHaveBeenCalledWith("/acme/projects/p1", "Roadmap");
  });

  // Web has no adapter, and the row is a <div> — there is no native
  // modifier-click behaviour to inherit, so without an explicit window.open
  // the row would navigate in place and swallow the user's intent (MUL-5456).
  it("opens a browser tab against the shareable URL for cmd/ctrl click without openInNewTab (web)", () => {
    const push = vi.fn();
    const open = vi.spyOn(window, "open").mockReturnValue(null);
    const adapter = makeAdapter({
      push,
      getShareableUrl: (p) => `https://app.example${p}`,
    });

    renderProbe(adapter);
    fireEvent.click(screen.getByRole("row"), { metaKey: true });
    fireEvent.click(screen.getByRole("row"), { ctrlKey: true });

    expect(open).toHaveBeenCalledTimes(2);
    expect(open).toHaveBeenNthCalledWith(
      1,
      "https://app.example/acme/projects/p1",
      "_blank",
      "noopener,noreferrer",
    );
    expect(open).toHaveBeenNthCalledWith(
      2,
      "https://app.example/acme/projects/p1",
      "_blank",
      "noopener,noreferrer",
    );
    expect(push).not.toHaveBeenCalled();
  });

  it("opens a background tab and prevents default for middle click when available", () => {
    const push = vi.fn();
    const openInNewTab = vi.fn();
    const adapter = makeAdapter({ push, openInNewTab });

    renderProbe(adapter);
    const event = new MouseEvent("auxclick", {
      bubbles: true,
      button: 1,
      cancelable: true,
    });
    screen.getByRole("row").dispatchEvent(event);

    expect(event.defaultPrevented).toBe(true);
    expect(openInNewTab).toHaveBeenCalledWith("/acme/projects/p1", undefined);
    expect(push).not.toHaveBeenCalled();
  });

  it("opens a browser tab for middle click without openInNewTab (web)", () => {
    const push = vi.fn();
    const open = vi.spyOn(window, "open").mockReturnValue(null);
    const adapter = makeAdapter({
      push,
      getShareableUrl: (p) => `https://app.example${p}`,
    });

    renderProbe(adapter);
    const event = new MouseEvent("auxclick", {
      bubbles: true,
      button: 1,
      cancelable: true,
    });
    screen.getByRole("row").dispatchEvent(event);

    expect(event.defaultPrevented).toBe(true);
    expect(open).toHaveBeenCalledWith(
      "https://app.example/acme/projects/p1",
      "_blank",
      "noopener,noreferrer",
    );
    expect(push).not.toHaveBeenCalled();
  });

  // Nested interactive elements must stop auxclick too, not just click: the
  // row's auxclick handler calls preventDefault(), which would cancel a
  // nested anchor's native middle-click open and background-tab the row
  // instead of the link's destination.
  it("rowLinkInteractiveProps shields a nested anchor from the row's handlers", () => {
    const push = vi.fn();
    const openInNewTab = vi.fn();
    const adapter = makeAdapter({ push, openInNewTab });

    function NestedProbe() {
      const rowLink = useRowLink();
      return (
        <div role="row" {...rowLink("/acme/projects/p1")}>
          <a
            href="https://example.com"
            target="_blank"
            rel="noopener noreferrer"
            {...rowLinkInteractiveProps}
          >
            source
          </a>
        </div>
      );
    }
    render(
      <NavigationProvider value={adapter}>
        <NestedProbe />
      </NavigationProvider>,
    );

    const anchor = screen.getByRole("link");
    fireEvent.click(anchor);
    const aux = new MouseEvent("auxclick", {
      bubbles: true,
      button: 1,
      cancelable: true,
    });
    anchor.dispatchEvent(aux);

    // Neither event reached the row, and the anchor's native behaviour
    // survives (no preventDefault from the row's middle-click handler).
    expect(aux.defaultPrevented).toBe(false);
    expect(push).not.toHaveBeenCalled();
    expect(openInNewTab).not.toHaveBeenCalled();
  });
});
