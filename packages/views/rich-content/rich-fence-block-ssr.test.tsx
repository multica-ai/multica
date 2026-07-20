/**
 * Cached reserved height must not reach the first frame (MUL-4922).
 *
 * The Mermaid layout cache lives in sessionStorage, which a server does not
 * have. Reading it during render yields the skeleton default on the server and
 * the cached real height in a browser whose cache is warm — a different
 * `style="min-height:…"` on the exact frame React hydrates. React reports that
 * as an attribute mismatch and does NOT repair it.
 *
 * These tests drive the REAL RichFenceBlock with a REAL prefilled
 * sessionStorage entry. An earlier version of the SSR suite passed a fixed
 * `reservedHeightPx` straight to the lazy shell, which bypassed this path
 * entirely and passed against the buggy code.
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act } from "@testing-library/react";
import { renderToString } from "react-dom/server";
import { createRoot, hydrateRoot } from "react-dom/client";
import { flushSync } from "react-dom";

vi.mock("../i18n", async () => {
  const editor = (await import("../locales/en/editor.json")).default;
  return {
    useT: () => ({
      t: (select: (bundle: typeof editor) => string) => select(editor),
    }),
    useTimeAgo: () => "just now",
  };
});

vi.mock("mermaid", () => ({
  default: {
    initialize: vi.fn(),
    render: vi.fn().mockResolvedValue({
      svg: '<svg viewBox="0 0 800 412"><g><text>diagram</text></g></svg>',
    }),
  },
}));

import { RichFenceBlock } from "./rich-code-block";
import { resetMountedBlocks } from "./mounted-block-registry";

const CHART = "flowchart LR\n  A --> B";
const CACHED_HEIGHT = 412;
const SKELETON_HEIGHT = 280;

/** Mirrors the DJB2 key derivation in editor/mermaid-diagram.tsx. */
function cacheKey(chart: string): string {
  let hash = 5381;
  for (let i = 0; i < chart.length; i++) {
    hash = ((hash << 5) + hash) ^ chart.charCodeAt(i);
  }
  return `multica:mermaid:layout:${(hash >>> 0).toString(36)}`;
}

beforeEach(() => {
  resetMountedBlocks();
  window.sessionStorage.clear();
  // A warm cache: this exact chart already rendered earlier in the session.
  window.sessionStorage.setItem(
    cacheKey(CHART),
    JSON.stringify({ width: 800, height: CACHED_HEIGHT }),
  );
  vi.stubGlobal("IntersectionObserver", undefined);
});

afterEach(() => {
  vi.unstubAllGlobals();
  window.sessionStorage.clear();
});

/**
 * Render the way a server does: no sessionStorage (and no IntersectionObserver).
 * jsdom provides both, so without removing them the "server" render would take
 * the browser's branch and the mismatch would be invisible.
 */
function serverRender(ui: Parameters<typeof renderToString>[0]): string {
  const storage = window.sessionStorage;
  vi.stubGlobal("sessionStorage", undefined);
  try {
    return renderToString(ui);
  } finally {
    vi.stubGlobal("sessionStorage", storage);
  }
}

const block = () => <RichFenceBlock language="mermaid" body={CHART} />;

describe("RichFenceBlock reserved height", () => {
  it("reserves the skeleton height on the server, not the cached height", () => {
    const html = serverRender(block());

    expect(html).toContain(`min-height:${SKELETON_HEIGHT}px`);
    expect(html).not.toContain(`min-height:${CACHED_HEIGHT}px`);
  });

  it("uses the skeleton height on the client's first frame despite a warm cache", () => {
    const container = document.createElement("div");
    document.body.appendChild(container);
    const root = createRoot(container);

    // No act(): the first committed frame is exactly what must be compared,
    // before any effect reads the cache.
    flushSync(() => root.render(block()));

    const shell = container.querySelector("[data-rich-block-shell]") as HTMLElement;
    expect(shell.style.minHeight).toBe(`${SKELETON_HEIGHT}px`);

    act(() => root.unmount());
    container.remove();
  });

  it("hydrates a warm-cache client against server markup without a mismatch", async () => {
    const html = serverRender(block());

    const container = document.createElement("div");
    container.innerHTML = html;
    document.body.appendChild(container);

    const errors: string[] = [];
    const errorSpy = vi.spyOn(console, "error").mockImplementation((...args) => {
      errors.push(args.map(String).join(" "));
    });

    let root: ReturnType<typeof hydrateRoot> | undefined;
    await act(async () => {
      root = hydrateRoot(container, block());
    });

    expect(errors.filter((e) => /hydrat|did not match|mismatch/i.test(e))).toEqual([]);

    errorSpy.mockRestore();
    await act(async () => root?.unmount());
    container.remove();
  });

  it("adopts the cached height after mount, so the zero-shift benefit survives", async () => {
    const container = document.createElement("div");
    document.body.appendChild(container);
    const root = createRoot(container);

    await act(async () => {
      root.render(block());
    });

    const shell = container.querySelector("[data-rich-block-shell]") as HTMLElement;
    // The point of the cache is preserved: after hydration the block reserves
    // the real height rather than a generic skeleton.
    expect(shell.style.minHeight).toBe(`${CACHED_HEIGHT}px`);

    await act(async () => root.unmount());
    container.remove();
  });

  it("falls back to the skeleton height when the cache is cold", async () => {
    window.sessionStorage.clear();

    const container = document.createElement("div");
    document.body.appendChild(container);
    const root = createRoot(container);

    await act(async () => {
      root.render(block());
    });

    const shell = container.querySelector("[data-rich-block-shell]") as HTMLElement;
    expect(shell.style.minHeight).toBe(`${SKELETON_HEIGHT}px`);

    await act(async () => root.unmount());
    container.remove();
  });
});
