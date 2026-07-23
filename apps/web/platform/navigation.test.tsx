import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render } from "@testing-library/react";
import { useNavigation } from "@multica/views/navigation";

// next/navigation hooks are not available in jsdom — stub the three the web
// adapter reads. These tests cover the browser-specific adapter wiring.
vi.mock("next/navigation", () => ({
  useRouter: () => ({
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    prefetch: vi.fn(),
  }),
  usePathname: () => "/acme/issues",
  useSearchParams: () => new URLSearchParams(""),
}));

import { WebNavigationProvider } from "./navigation";

function Probe({ onAdapter }: { onAdapter: (nav: ReturnType<typeof useNavigation>) => void }) {
  const nav = useNavigation();
  onAdapter(nav);
  return null;
}

describe("WebNavigationProvider", () => {
  let openSpy: ReturnType<typeof vi.spyOn>;

  beforeEach(() => {
    openSpy = vi.spyOn(window, "open").mockImplementation(() => null);
  });

  afterEach(() => {
    openSpy.mockRestore();
    window.history.replaceState(null, "", "/");
  });

  it("exposes openInNewTab so modifier-click handlers do not silently no-op on web", () => {
    let nav: ReturnType<typeof useNavigation> | undefined;
    render(
      <WebNavigationProvider>
        <Probe onAdapter={(n) => (nav = n)} />
      </WebNavigationProvider>,
    );

    // The bug: web's adapter omitted openInNewTab, so issue-mention handlers
    // that preventDefault() then `if (openInNewTab) openInNewTab(...)` did
    // nothing on Cmd+click. The adapter must now provide it.
    expect(nav?.openInNewTab).toBeTypeOf("function");

    nav?.openInNewTab?.("/acme/issues/abc-123");
    expect(openSpy).toHaveBeenCalledWith(
      "/acme/issues/abc-123",
      "_blank",
      "noopener,noreferrer",
    );
  });

  it("preserves Next router state when replacing the URL silently", () => {
    const routerState = { __NA: true, tree: ["", {}] };
    window.history.replaceState(
      routerState,
      "",
      "/acme/inbox?chat=new-chat&agent=agent-1",
    );

    let nav: ReturnType<typeof useNavigation> | undefined;
    render(
      <WebNavigationProvider>
        <Probe onAdapter={(n) => (nav = n)} />
      </WebNavigationProvider>,
    );

    nav?.replaceSilent?.("/acme/inbox");

    expect(window.location.pathname).toBe("/acme/inbox");
    expect(window.location.search).toBe("");
    expect(window.history.state).toEqual(routerState);
  });
});
