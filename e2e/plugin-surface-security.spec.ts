import { expect, test } from "@playwright/test";
import { buildSurfaceFrameDocument } from "../packages/views/plugins/surface-document";

test.describe("plugin surface browser boundary", () => {
  test("a normal hosted document relays exactly one guest-created port", async ({ page }) => {
    await page.route("https://plugin-content.example.test/**", async (route) => {
      await route.fulfill({
        contentType: "text/html",
        headers: { "Content-Security-Policy": "default-src 'none'; script-src 'unsafe-inline'" },
        body: `<!doctype html><script>
          const channel = new MessageChannel();
          parent.postMessage({
            type: "multica:plugin-bridge-connect",
            version: 2,
            challenge: "proof"
          }, "*", [channel.port1]);
        </script>`,
      });
    });
    const wrapper = buildSurfaceFrameDocument({
      url: "https://plugin-content.example.test/plugin-surfaces/opaque",
      bridgeToken: "proof",
    });
    await page.setContent(`<script>window.bridgeCount = 0; addEventListener("message", event => {
      if (event.data?.type === "multica:plugin-bridge-connect" && event.ports[0]) window.bridgeCount += 1;
    });</script><iframe id="host" sandbox="allow-scripts allow-same-origin"></iframe>`);
    await page.locator("#host").evaluate((frame, srcdoc) => {
      (frame as HTMLIFrameElement).srcdoc = srcdoc as string;
    }, wrapper);

    await expect.poll(() => page.evaluate(() => (window as unknown as { bridgeCount: number }).bridgeCount)).toBe(1);
  });

  test("a first-line external navigation sends no request and cannot retain a bridge", async ({ page }) => {
    let attackerRequests = 0;
    await page.route("https://plugin-content.example.test/**", async (route) => {
      await route.fulfill({
        contentType: "text/html",
        headers: {
          "Content-Security-Policy": "default-src 'none'; script-src 'unsafe-inline'",
        },
        body: `<!doctype html><script>
          const channel = new MessageChannel();
          channel.port2.onmessage = () => channel.port2.postMessage("still-alive");
          parent.postMessage({
            type: "multica:plugin-bridge-connect",
            version: 2,
            challenge: "proof"
          }, "*", [channel.port1]);
          location.replace("https://attacker.example.test/stolen");
          const blockedUntil = performance.now() + 150;
          while (performance.now() < blockedUntil) {}
        </script>`,
      });
    });
    await page.route("https://attacker.example.test/**", async (route) => {
      attackerRequests += 1;
      await route.fulfill({ body: "should never load" });
    });

    const wrapper = buildSurfaceFrameDocument({
      url: "https://plugin-content.example.test/plugin-surfaces/opaque",
      bridgeToken: "proof",
    });
    await page.setContent(`<script>window.bridgeCount = 0; window.blockedCount = 0; window.bridgeReply = false; addEventListener("message", event => {
      if (event.data?.type === "multica:plugin-bridge-connect" && event.ports[0]) {
        window.bridgeCount += 1;
        window.bridgePort = event.ports[0];
        window.bridgePort.onmessage = () => { window.bridgeReply = true; };
        window.bridgePort.start();
      }
      if (event.data?.type === "multica:plugin-surface-navigation-blocked") {
        window.blockedCount += 1;
        window.bridgePort?.close();
      }
    });</script><iframe id="host" sandbox="allow-scripts allow-same-origin"></iframe>`);
    await page.locator("#host").evaluate((frame, srcdoc) => {
      (frame as HTMLIFrameElement).srcdoc = srcdoc as string;
    }, wrapper);

    await expect.poll(() => page.evaluate(() => (window as unknown as { blockedCount: number }).blockedCount)).toBe(1);
    await page.evaluate(() => (window as unknown as { bridgePort?: MessagePort }).bridgePort?.postMessage("ping"));
    await page.waitForTimeout(100);
    expect(await page.evaluate(() => (window as unknown as { bridgeReply: boolean }).bridgeReply)).toBe(false);
    expect(attackerRequests).toBe(0);
  });

  test("two script-capable plugin frames cannot inspect each other", async ({ page }) => {
    await page.setContent(`
      <script>
        window.results = [];
        addEventListener("message", event => window.results.push(event.data));
      </script>
      <iframe name="pluginA" sandbox="allow-scripts" srcdoc="<script>
        try { parent.frames[1].document.body; parent.postMessage('leaked', '*'); }
        catch (error) { parent.postMessage(error.name, '*'); }
      <\/script>"></iframe>
      <iframe name="pluginB" sandbox="allow-scripts" srcdoc="<div id='private'>secret</div>"></iframe>
    `);

    await expect.poll(() => page.evaluate(() => (window as unknown as { results: string[] }).results)).toContain("SecurityError");
    expect(await page.evaluate(() => (window as unknown as { results: string[] }).results)).not.toContain("leaked");
  });
});
