import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { NavigationProvider } from "./context";
import { useRowLink } from "./use-row-link";
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
    expect(openInNewTab).toHaveBeenNthCalledWith(1, "/acme/projects/p1");
    expect(openInNewTab).toHaveBeenNthCalledWith(2, "/acme/projects/p1");
    expect(push).not.toHaveBeenCalled();
  });

  it("falls back to a single push for cmd/ctrl click without openInNewTab", () => {
    const push = vi.fn();
    const adapter = makeAdapter({ push });

    renderProbe(adapter);
    fireEvent.click(screen.getByRole("row"), { metaKey: true });
    fireEvent.click(screen.getByRole("row"), { ctrlKey: true });

    expect(push).toHaveBeenCalledTimes(2);
    expect(push).toHaveBeenNthCalledWith(1, "/acme/projects/p1");
    expect(push).toHaveBeenNthCalledWith(2, "/acme/projects/p1");
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
    expect(openInNewTab).toHaveBeenCalledWith("/acme/projects/p1");
    expect(push).not.toHaveBeenCalled();
  });

  it("falls back to one prevented push for middle click without openInNewTab", () => {
    const push = vi.fn();
    const adapter = makeAdapter({ push });

    renderProbe(adapter);
    const event = new MouseEvent("auxclick", {
      bubbles: true,
      button: 1,
      cancelable: true,
    });
    screen.getByRole("row").dispatchEvent(event);

    expect(event.defaultPrevented).toBe(true);
    expect(push).toHaveBeenCalledWith("/acme/projects/p1");
    expect(push).toHaveBeenCalledTimes(1);
  });
});
