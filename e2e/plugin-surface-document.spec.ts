import { expect, test, type Page } from "@playwright/test";
import { buildSurfaceFrameDocument } from "../packages/views/plugins/surface-document";

/**
 * Real Chromium coverage for plugin surface lifecycle behavior jsdom does not
 * implement: reloading a sandboxed srcdoc wrapper, page lifecycle events in
 * the hosted document, and errors thrown by a dynamically inserted script.
 */

const CONTENT_ORIGIN = "https://plugin-content.example.test";

// Mirrors the hosted document's bootstrap: report the first error once, report
// pagehide as navigation, hand over one bridge port, then run plugin code from
// a dynamically inserted script.
function hostedDocument(challenge: string, pluginCode: string): string {
  return `<!doctype html><body><script>
    let failed = false;
    function reportSurfaceError() {
      if (failed) return;
      failed = true;
      parent.postMessage({ type: "multica:plugin-surface-error" }, "*");
    }
    addEventListener("error", reportSurfaceError);
    addEventListener("pagehide", () => parent.postMessage({ type: "multica:plugin-surface-navigated" }, "*"));
    const channel = new MessageChannel();
    parent.postMessage({
      type: "multica:plugin-bridge-connect",
      version: 2,
      challenge: ${JSON.stringify(challenge)}
    }, "*", [channel.port1]);
    const plugin = document.createElement("script");
    plugin.textContent = ${JSON.stringify(pluginCode)};
    document.body.appendChild(plugin);
  </script></body>`;
}

async function routeHostedDocuments(page: Page, pluginCode: string) {
  await page.route(`${CONTENT_ORIGIN}/**`, async (route) => {
    const challenge = new URL(route.request().url()).pathname.split("/").pop() ?? "";
    await route.fulfill({
      contentType: "text/html",
      headers: { "Content-Security-Policy": "default-src 'none'; script-src 'unsafe-inline'" },
      body: hostedDocument(challenge, pluginCode),
    });
  });
}

function launch(bridgeToken: string): string {
  return buildSurfaceFrameDocument({ url: `${CONTENT_ORIGIN}/plugin-surfaces/${bridgeToken}`, bridgeToken });
}

// Like PluginSurfaceFrame, the host arms its listener before assigning srcdoc
// and accepts terminal events only from its own frame.
async function mountHost(page: Page) {
  await page.setContent(`<script>
    window.events = { bridge: 0, error: 0, navigated: 0, blocked: 0 };
    addEventListener("message", (event) => {
      const frame = document.getElementById("host");
      if (!frame.contentWindow || event.source !== frame.contentWindow) return;
      const type = event.data?.type;
      if (type === "multica:plugin-bridge-connect" && event.ports[0]) window.events.bridge += 1;
      if (type === "multica:plugin-surface-error") window.events.error += 1;
      if (type === "multica:plugin-surface-navigated") window.events.navigated += 1;
      if (type === "multica:plugin-surface-navigation-blocked") window.events.blocked += 1;
    });
  </script><iframe id="host" sandbox="allow-scripts allow-same-origin"></iframe>`);
}

type SurfaceEvents = { bridge: number; error: number; navigated: number; blocked: number };

function surfaceEvents(page: Page): Promise<SurfaceEvents> {
  return page.evaluate(() => (window as unknown as { events: SurfaceEvents }).events);
}

async function assignWrapper(page: Page, srcdoc: string) {
  await page.locator("#host").evaluate((frame, value) => {
    (frame as HTMLIFrameElement).srcdoc = value as string;
  }, srcdoc);
}

test.describe("plugin surface document lifecycle (real Chromium)", () => {
  test("replacing the launch reconnects without a navigation signal", async ({ page }) => {
    await routeHostedDocuments(page, "");
    await mountHost(page);

    await assignWrapper(page, launch("first-launch"));
    await expect.poll(async () => (await surfaceEvents(page)).bridge).toBe(1);

    // The old hosted document fires pagehide while the host replaces its
    // wrapper. That host-authored reload must not mark the new launch as
    // navigated away.
    await assignWrapper(page, launch("second-launch"));
    await expect.poll(async () => (await surfaceEvents(page)).bridge).toBe(2);
    await page.waitForTimeout(300);

    expect(await surfaceEvents(page)).toEqual({ bridge: 2, error: 0, navigated: 0, blocked: 0 });
  });

  test("reports a first-line plugin error to the host exactly once", async ({ page }) => {
    // The plugin queues a later lifecycle event, then fails on its first line.
    // Once the error is terminal, the wrapper must not relay anything else.
    await routeHostedDocuments(
      page,
      "setTimeout(() => parent.postMessage({ type: 'multica:plugin-surface-navigated' }, '*'), 50);" +
        "throw new Error('plugin failed during bootstrap');",
    );
    await mountHost(page);

    await assignWrapper(page, launch("failing-launch"));
    await expect.poll(async () => (await surfaceEvents(page)).error).toBe(1);
    await page.waitForTimeout(300);

    expect(await surfaceEvents(page)).toEqual({ bridge: 1, error: 1, navigated: 0, blocked: 0 });
  });
});
