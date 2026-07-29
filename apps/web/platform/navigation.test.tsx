/**
 * MUL-5208 — the web half of the `multica:navigate` bridge.
 *
 * Shared content (comments, chat, issue descriptions) fires this event whenever
 * a link resolves to an in-app destination, including an absolute URL on this
 * deployment's own origin. Desktop answers it by opening a tab; the web must
 * answer it with a router push, or those links silently do nothing.
 */
import { describe, expect, it, vi, beforeEach } from "vitest";
import { render } from "@testing-library/react";

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

function navigate(path: string) {
  window.dispatchEvent(
    new CustomEvent("multica:navigate", { detail: { path } }),
  );
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
