/**
 * MUL-5208 — the web half of the `multica:navigate` bridge.
 *
 * Shared content (comments, chat, issue descriptions) fires this event whenever
 * a link resolves to an in-app destination, including an absolute URL on this
 * deployment's own origin. Desktop answers it by opening a tab; the web must
 * answer it with a router push, or those links silently do nothing.
 */
import { describe, expect, it, vi, afterEach, beforeEach } from "vitest";
import { act, render } from "@testing-library/react";

const router = vi.hoisted(() => ({
  push: vi.fn(),
  replace: vi.fn(),
  back: vi.fn(),
  prefetch: vi.fn(),
}));

vi.mock("next/navigation", () => ({
  useRouter: () => router,
  usePathname: () => "/acme/issues",
  useSearchParams: () => new URLSearchParams(),
}));

import { WebNavigationProvider } from "./navigation";
import { useNavigation, type NavigationAdapter } from "@multica/views/navigation";

function navigate(path: string) {
  window.dispatchEvent(
    new CustomEvent("multica:navigate", { detail: { path } }),
  );
}

function renderAdapter(): () => NavigationAdapter {
  let adapter: NavigationAdapter | null = null;
  function Probe() {
    adapter = useNavigation();
    return null;
  }
  render(
    <WebNavigationProvider>
      <Probe />
    </WebNavigationProvider>,
  );
  return () => adapter!;
}

beforeEach(() => {
  router.push.mockReset();
});

describe("WebNavigationProvider internal link bridge", () => {
  it("pushes the path a content link resolved to", () => {
    render(<WebNavigationProvider>{null}</WebNavigationProvider>);

    navigate("/acme/issues/MUL-1");

    expect(router.push).toHaveBeenCalledWith("/acme/issues/MUL-1");
  });

  it("ignores an event without a path", () => {
    render(<WebNavigationProvider>{null}</WebNavigationProvider>);

    window.dispatchEvent(new CustomEvent("multica:navigate", { detail: {} }));

    expect(router.push).not.toHaveBeenCalled();
  });

  it("stops listening once unmounted", () => {
    const { unmount } = render(
      <WebNavigationProvider>{null}</WebNavigationProvider>,
    );

    unmount();
    navigate("/acme/issues/MUL-1");

    expect(router.push).not.toHaveBeenCalled();
  });
});

/**
 * `canGoBack` decides whether a page whose subject was just deleted steps back
 * or replaces with a fallback. A wrong `true` walks the user out of Multica,
 * so the adapter must expose the browser's own answer and nothing derived.
 */
describe("WebNavigationProvider canGoBack", () => {
  const win = window as unknown as { navigation?: unknown };

  afterEach(() => {
    delete win.navigation;
  });

  it("passes through the Navigation API's answer", () => {
    win.navigation = { canGoBack: true };

    expect(renderAdapter()().canGoBack!()).toBe(true);
  });

  it("reads the answer live rather than freezing it at render", () => {
    win.navigation = { canGoBack: true };
    const adapter = renderAdapter();

    win.navigation = { canGoBack: false };

    expect(adapter().canGoBack!()).toBe(false);
  });

  // Regression (PR review, twice): a count of `push` calls stood in for real
  // history here. It could not — Next drops a push to `replaceState` when the
  // canonical URL is unchanged, so a push that committed no entry still made
  // this claim `true`. Calling push must not move this answer at all.
  it("is unmoved by a push that committed no history entry", () => {
    win.navigation = { canGoBack: false };
    const adapter = renderAdapter();

    adapter().push("/acme/issues");

    expect(router.push).toHaveBeenCalledWith("/acme/issues");
    expect(adapter().canGoBack!()).toBe(false);
  });

  it("reports false where the browser cannot answer, so callers use the fallback", () => {
    expect(renderAdapter()().canGoBack!()).toBe(false);
  });
});

/**
 * MUL-6784 — the fragment is the only part of the current location Next.js
 * never hands to React: `usePathname()` drops it. Shared views rebuild share
 * and feedback links from the adapter, so a missing `hash` silently turns a
 * `#comment-…` deep link into a link to the whole page.
 */
describe("WebNavigationProvider hash", () => {
  afterEach(() => {
    window.location.hash = "";
  });

  it("reports the fragment of the page being rendered", () => {
    window.location.hash = "#comment-c1";

    expect(renderAdapter()().hash).toBe("#comment-c1");
  });

  it('reports "" when the location has no fragment', () => {
    expect(renderAdapter()().hash).toBe("");
  });

  it("re-reads the fragment when it changes after mount", async () => {
    const adapter = renderAdapter();

    await act(async () => {
      window.location.hash = "#comment-c2";
      // jsdom dispatches hashchange in a later task, like the browser.
      await new Promise((resolve) => setTimeout(resolve, 0));
    });

    expect(adapter().hash).toBe("#comment-c2");
  });
});
