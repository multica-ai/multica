import { describe, it, expect, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { AppLink } from "./app-link";
import { NavigationProvider } from "./context";
import type { NavigationAdapter } from "./types";

function makeAdapter(overrides: Partial<NavigationAdapter> = {}): NavigationAdapter {
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

function renderLink(
  adapter: NavigationAdapter,
  props: React.ComponentProps<typeof AppLink> = { href: "/issues" },
) {
  return render(
    <NavigationProvider value={adapter}>
      <AppLink {...props}>go</AppLink>
    </NavigationProvider>,
  );
}

describe("AppLink", () => {
  it("calls caller onClick BEFORE push so synchronous side effects (close menu, etc) commit before the transition starts", () => {
    const order: string[] = [];
    const adapter = makeAdapter({
      push: vi.fn(() => order.push("push")),
    });
    renderLink(adapter, {
      href: "/issues",
      onClick: () => order.push("onClick"),
    });

    fireEvent.click(screen.getByText("go"));
    expect(order).toEqual(["onClick", "push"]);
  });

  it("calls adapter.prefetch on hover, alongside the caller's onMouseEnter — neither is overridden by {...props}", () => {
    const prefetch = vi.fn();
    const callerMouseEnter = vi.fn();
    const adapter = makeAdapter({ prefetch });

    renderLink(adapter, {
      href: "/issues",
      onMouseEnter: callerMouseEnter,
    });

    fireEvent.mouseEnter(screen.getByText("go"));
    expect(prefetch).toHaveBeenCalledWith("/issues");
    expect(callerMouseEnter).toHaveBeenCalledTimes(1);
  });

  it("calls adapter.prefetch on focus, alongside the caller's onFocus", () => {
    const prefetch = vi.fn();
    const callerFocus = vi.fn();
    const adapter = makeAdapter({ prefetch });

    renderLink(adapter, {
      href: "/issues",
      onFocus: callerFocus,
    });

    fireEvent.focus(screen.getByText("go"));
    expect(prefetch).toHaveBeenCalledWith("/issues");
    expect(callerFocus).toHaveBeenCalledTimes(1);
  });

  it("is a no-op when adapter does not implement prefetch (desktop)", () => {
    const adapter = makeAdapter();
    renderLink(adapter);
    expect(() => fireEvent.mouseEnter(screen.getByText("go"))).not.toThrow();
    expect(() => fireEvent.focus(screen.getByText("go"))).not.toThrow();
  });

  it("modifier-click (cmd / ctrl) delegates to openInNewTab and does NOT push", () => {
    const push = vi.fn();
    const openInNewTab = vi.fn();
    const adapter = makeAdapter({ push, openInNewTab });

    renderLink(adapter);
    fireEvent.click(screen.getByText("go"), { metaKey: true });
    expect(openInNewTab).toHaveBeenCalledWith("/issues", undefined);
    expect(push).not.toHaveBeenCalled();
  });

  it("modifier-click passes newTabTitle through as the tab label", () => {
    const openInNewTab = vi.fn();
    const adapter = makeAdapter({ openInNewTab });

    renderLink(adapter, { href: "/issues", newTabTitle: "MUL-7" });
    fireEvent.click(screen.getByText("go"), { metaKey: true });
    expect(openInNewTab).toHaveBeenCalledWith("/issues", "MUL-7");
  });

  describe("target=_blank (open-in-new-tab links)", () => {
    it("plain click opens a foreground new tab via the adapter (desktop) and does NOT push", () => {
      const push = vi.fn();
      const openInNewTab = vi.fn();
      const adapter = makeAdapter({ push, openInNewTab });

      renderLink(adapter, {
        href: "/issues",
        target: "_blank",
        newTabTitle: "MUL-7",
      });
      fireEvent.click(screen.getByText("go"));
      expect(openInNewTab).toHaveBeenCalledWith("/issues", "MUL-7", {
        activate: true,
      });
      expect(push).not.toHaveBeenCalled();
    });

    it("plain click without an adapter (web) neither pushes nor prevents default, so the browser's native target=_blank handling runs", () => {
      const push = vi.fn();
      const adapter = makeAdapter({ push });

      renderLink(adapter, { href: "/issues", target: "_blank" });
      // fireEvent returns false when preventDefault was called.
      const defaultNotPrevented = fireEvent.click(screen.getByText("go"));
      expect(defaultNotPrevented).toBe(true);
      expect(push).not.toHaveBeenCalled();
    });

    it("caller onClick still runs before the new-tab open", () => {
      const order: string[] = [];
      const adapter = makeAdapter({
        openInNewTab: vi.fn(() => order.push("openInNewTab")),
      });

      renderLink(adapter, {
        href: "/issues",
        target: "_blank",
        onClick: () => order.push("onClick"),
      });
      fireEvent.click(screen.getByText("go"));
      expect(order).toEqual(["onClick", "openInNewTab"]);
    });

    it("renders the target and a rel guard on the anchor", () => {
      renderLink(makeAdapter(), { href: "/issues", target: "_blank" });
      const anchor = screen.getByText("go");
      expect(anchor).toHaveAttribute("target", "_blank");
      expect(anchor).toHaveAttribute("rel", "noopener noreferrer");
    });

    it("renders no target or rel when target is not set", () => {
      renderLink(makeAdapter());
      const anchor = screen.getByText("go");
      expect(anchor).not.toHaveAttribute("target");
      expect(anchor).not.toHaveAttribute("rel");
    });
  });

  it("a caller-supplied onClick passed via spread cannot silently override the navigation handler", () => {
    const push = vi.fn();
    const adapter = makeAdapter({ push });
    const spreadOnClick = vi.fn((e: React.MouseEvent) => e.preventDefault());

    render(
      <NavigationProvider value={adapter}>
        {/* simulate a caller that passes onClick through a spread bag */}
        <AppLink href="/issues" {...{ onClick: spreadOnClick }}>
          go
        </AppLink>
      </NavigationProvider>,
    );

    fireEvent.click(screen.getByText("go"));
    // Caller still runs (it was hoisted into the named param), but push runs too.
    expect(spreadOnClick).toHaveBeenCalled();
    expect(push).toHaveBeenCalledWith("/issues");
  });
});
