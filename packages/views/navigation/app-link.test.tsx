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
    hash: "",
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
  it("renders the public web URL on desktop so native copy-link affordances never expose the renderer URL", () => {
    const adapter = makeAdapter({
      openInNewTab: vi.fn(),
      getShareableUrl: (path) => `https://app.example${path}`,
    });

    renderLink(adapter, { href: "/acme/issues/MUL-7" });

    expect(screen.getByRole("link", { name: "go" })).toHaveAttribute(
      "href",
      "https://app.example/acme/issues/MUL-7",
    );
  });

  it("keeps web anchors route-relative for SSR and native browser navigation", () => {
    const adapter = makeAdapter({
      getShareableUrl: (path) => `https://app.example${path}`,
    });

    renderLink(adapter, { href: "/acme/issues/MUL-7" });

    expect(screen.getByRole("link", { name: "go" })).toHaveAttribute(
      "href",
      "/acme/issues/MUL-7",
    );
  });

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

  it("cmd+shift-click opens a FOREGROUND tab (activate) — the spec's 'take me there' modifier", () => {
    const push = vi.fn();
    const openInNewTab = vi.fn();
    const adapter = makeAdapter({ push, openInNewTab });

    renderLink(adapter);
    fireEvent.click(screen.getByText("go"), { metaKey: true, shiftKey: true });
    expect(openInNewTab).toHaveBeenCalledWith("/issues", undefined, {
      activate: true,
    });
    expect(push).not.toHaveBeenCalled();
  });

  it("shift-alone click on desktop is a plain click (in-place push) — shift is not a spec modifier", () => {
    const push = vi.fn();
    const openInNewTab = vi.fn();
    const adapter = makeAdapter({ push, openInNewTab });

    renderLink(adapter);
    fireEvent.click(screen.getByText("go"), { shiftKey: true });
    expect(push).toHaveBeenCalledWith("/issues");
    expect(openInNewTab).not.toHaveBeenCalled();
  });

  it("shift-alone click without an adapter (web) is left to the browser's native new-window handling", () => {
    const push = vi.fn();
    const adapter = makeAdapter({ push });

    renderLink(adapter);
    const defaultNotPrevented = fireEvent.click(screen.getByText("go"), {
      shiftKey: true,
    });
    expect(defaultNotPrevented).toBe(true);
    expect(push).not.toHaveBeenCalled();
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

  describe("middle click (aux click)", () => {
    // `fireEvent` has no auxClick helper, so dispatch the real event — same
    // approach as use-row-link.test.tsx. Returns the event so the caller can
    // assert on defaultPrevented, which is the whole point on desktop.
    function auxClick(el: HTMLElement, button = 1) {
      const event = new MouseEvent("auxclick", {
        bubbles: true,
        button,
        cancelable: true,
      });
      el.dispatchEvent(event);
      return event;
    }

    it("opens a BACKGROUND tab via the adapter (desktop) and does NOT push", () => {
      const push = vi.fn();
      const openInNewTab = vi.fn();
      const adapter = makeAdapter({ push, openInNewTab });

      renderLink(adapter);
      const event = auxClick(screen.getByText("go"));

      // No `activate` — a middle click means "keep me where I am".
      expect(openInNewTab).toHaveBeenCalledWith("/issues", undefined);
      expect(push).not.toHaveBeenCalled();
      // The native window-open must be suppressed: on desktop it is a dead
      // end (denied by setWindowOpenHandler, then dropped by the http/https
      // allowlist because the packaged renderer is file://).
      expect(event.defaultPrevented).toBe(true);
    });

    it("passes newTabTitle through as the tab label", () => {
      const openInNewTab = vi.fn();
      const adapter = makeAdapter({ openInNewTab });

      renderLink(adapter, { href: "/issues", newTabTitle: "MUL-7" });
      auxClick(screen.getByText("go"));
      expect(openInNewTab).toHaveBeenCalledWith("/issues", "MUL-7");
    });

    it("without an adapter (web) neither pushes nor prevents default, so the browser's native background tab still opens", () => {
      const push = vi.fn();
      const adapter = makeAdapter({ push });

      renderLink(adapter);
      const event = auxClick(screen.getByText("go"));

      expect(event.defaultPrevented).toBe(false);
      expect(push).not.toHaveBeenCalled();
    });

    it("stays background even on a target=_blank link, where a plain click would foreground", () => {
      const openInNewTab = vi.fn();
      const adapter = makeAdapter({ openInNewTab });

      renderLink(adapter, {
        href: "/issues",
        target: "_blank",
        newTabTitle: "MUL-7",
      });
      auxClick(screen.getByText("go"));
      expect(openInNewTab).toHaveBeenCalledWith("/issues", "MUL-7");
    });

    it.each([
      ["right button", 2],
      ["browser back button", 3],
      ["browser forward button", 4],
    ])("ignores a non-middle aux click (%s)", (_label, button) => {
      const push = vi.fn();
      const openInNewTab = vi.fn();
      const adapter = makeAdapter({ push, openInNewTab });

      renderLink(adapter);
      const event = auxClick(screen.getByText("go"), button);

      expect(openInNewTab).not.toHaveBeenCalled();
      expect(push).not.toHaveBeenCalled();
      expect(event.defaultPrevented).toBe(false);
    });

    it("runs the caller's onAuxClick, which can opt out of the new tab with preventDefault", () => {
      const openInNewTab = vi.fn();
      const callerAuxClick = vi.fn((e: React.MouseEvent) => e.preventDefault());
      const adapter = makeAdapter({ openInNewTab });

      renderLink(adapter, { href: "/issues", onAuxClick: callerAuxClick });
      auxClick(screen.getByText("go"));

      expect(callerAuxClick).toHaveBeenCalledTimes(1);
      expect(openInNewTab).not.toHaveBeenCalled();
    });

    it("runs the caller's onAuxClick and still opens the tab when it does not preventDefault", () => {
      const openInNewTab = vi.fn();
      const callerAuxClick = vi.fn();
      const adapter = makeAdapter({ openInNewTab });

      renderLink(adapter, { href: "/issues", onAuxClick: callerAuxClick });
      auxClick(screen.getByText("go"));

      expect(callerAuxClick).toHaveBeenCalledTimes(1);
      expect(openInNewTab).toHaveBeenCalledWith("/issues", undefined);
    });

    it("a caller-supplied onAuxClick passed via spread cannot silently override the aux handler", () => {
      const openInNewTab = vi.fn();
      const adapter = makeAdapter({ openInNewTab });
      const spreadAuxClick = vi.fn();

      render(
        <NavigationProvider value={adapter}>
          <AppLink href="/issues" {...{ onAuxClick: spreadAuxClick }}>
            go
          </AppLink>
        </NavigationProvider>,
      );

      auxClick(screen.getByText("go"));
      expect(spreadAuxClick).toHaveBeenCalled();
      expect(openInNewTab).toHaveBeenCalledWith("/issues", undefined);
    });
  });

  it("a caller-supplied onClick passed via spread cannot silently override the navigation handler", () => {
    const push = vi.fn();
    const adapter = makeAdapter({ push });
    const spreadOnClick = vi.fn();

    render(
      <NavigationProvider value={adapter}>
        {/* simulate a caller that passes onClick through a spread bag */}
        <AppLink href="/issues" {...{ onClick: spreadOnClick }}>
          go
        </AppLink>
      </NavigationProvider>,
    );

    fireEvent.click(screen.getByText("go"));
    // Caller still runs (it was hoisted into the named param), and push runs too.
    expect(spreadOnClick).toHaveBeenCalled();
    expect(push).toHaveBeenCalledWith("/issues");
  });

  describe("caller opt-out via preventDefault (drag guards, permission gates)", () => {
    it("onClick calling preventDefault cancels the in-place push", () => {
      const push = vi.fn();
      const adapter = makeAdapter({ push });

      renderLink(adapter, {
        href: "/issues",
        onClick: (e) => e.preventDefault(),
      });
      fireEvent.click(screen.getByText("go"));
      expect(push).not.toHaveBeenCalled();
    });

    it("onClick calling preventDefault cancels a modifier-click new tab too", () => {
      const push = vi.fn();
      const openInNewTab = vi.fn();
      const adapter = makeAdapter({ push, openInNewTab });

      renderLink(adapter, {
        href: "/issues",
        onClick: (e) => e.preventDefault(),
      });
      fireEvent.click(screen.getByText("go"), { metaKey: true });
      expect(openInNewTab).not.toHaveBeenCalled();
      expect(push).not.toHaveBeenCalled();
    });

    it("onClick runs before a modifier-click open, so side effects (close menu) still land", () => {
      const order: string[] = [];
      const adapter = makeAdapter({
        openInNewTab: vi.fn(() => order.push("openInNewTab")),
      });

      renderLink(adapter, {
        href: "/issues",
        onClick: () => order.push("onClick"),
      });
      fireEvent.click(screen.getByText("go"), { metaKey: true });
      expect(order).toEqual(["onClick", "openInNewTab"]);
    });
  });
});
