import { expect, test } from "@playwright/test";
import { buildSurfaceDocument } from "../packages/views/plugins/surface-document";

/**
 * Real Chromium coverage for behavior jsdom does not implement: executing a
 * sandboxed srcdoc document, reporting errors from a dynamically inserted
 * script, and firing page lifecycle events when the host rewrites srcdoc.
 */

test.describe("plugin surface document (real Chromium, sandboxed srcdoc)", () => {
  test("a host-authored srcdoc reload is not reported as hostile navigation", async ({ page }) => {
    const first = buildSurfaceDocument({
      code: "parent.postMessage({ type: 'plugin-test-ran' }, '*');",
      grantedScopes: [],
      theme: {},
    });
    const themed = buildSurfaceDocument({
      code: "parent.postMessage({ type: 'plugin-test-ran' }, '*');",
      grantedScopes: [],
      theme: { "--background": "white" },
    });

    await page.setContent("<!doctype html><body></body>");
    await page.evaluate((srcdoc) => {
      const state = { ran: 0, navigated: 0 };
      (window as unknown as { __surfaceState: typeof state }).__surfaceState = state;
      window.addEventListener("message", (event) => {
        const type = (event.data as { type?: string } | null)?.type;
        if (type === "plugin-test-ran") state.ran++;
        if (type === "multica:plugin-surface-navigated") state.navigated++;
      });
      const frame = document.createElement("iframe");
      frame.id = "surface";
      frame.sandbox.add("allow-scripts");
      frame.srcdoc = srcdoc;
      document.body.appendChild(frame);
    }, first);

    await expect.poll(() => page.evaluate(() =>
      (window as unknown as { __surfaceState: { ran: number } }).__surfaceState.ran,
    )).toBe(1);

    await page.evaluate((srcdoc) => {
      document.querySelector<HTMLIFrameElement>("#surface")!.srcdoc = srcdoc;
    }, themed);
    await expect.poll(() => page.evaluate(() =>
      (window as unknown as { __surfaceState: { ran: number } }).__surfaceState.ran,
    )).toBe(2);

    expect(await page.evaluate(() =>
      (window as unknown as { __surfaceState: { navigated: number } }).__surfaceState.navigated,
    )).toBe(0);
  });

  test("reports a synchronous plugin error even when the host listener mounts late", async ({ page }) => {
    const documentWithError = buildSurfaceDocument({
      code: "throw new Error('plugin failed during bootstrap');",
      grantedScopes: [],
      theme: {},
    });

    await page.setContent("<!doctype html><body></body>");
    await page.evaluate((srcdoc) => {
      const frame = document.createElement("iframe");
      frame.id = "surface";
      frame.sandbox.add("allow-scripts");
      frame.srcdoc = srcdoc;
      document.body.appendChild(frame);
    }, documentWithError);

    // The guest has already executed and failed before the host starts
    // listening. A one-shot postMessage is lost in this ordering.
    await page.waitForTimeout(300);
    await page.evaluate(() => {
      (window as unknown as { __surfaceErrors: number }).__surfaceErrors = 0;
      window.addEventListener("message", (event) => {
        if ((event.data as { type?: string } | null)?.type !== "multica:plugin-surface-error") return;
        const frame = document.querySelector<HTMLIFrameElement>("#surface")!;
        if (event.source !== frame.contentWindow) return;
        (window as unknown as { __surfaceErrors: number }).__surfaceErrors++;
        frame.contentWindow!.postMessage({ type: "multica:plugin-surface-error-ack" }, "*");
      });
    });

    await expect.poll(() => page.evaluate(() =>
      (window as unknown as { __surfaceErrors: number }).__surfaceErrors,
    )).toBe(1);
  });
});
