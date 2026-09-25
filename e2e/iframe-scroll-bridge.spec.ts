import { expect, test, type Frame } from "@playwright/test";
import { buildScrollBridge } from "../packages/views/editor/utils/iframe-scroll-bridge";

/**
 * Self-contained Chromium tests for the in-iframe scroll bridge
 * (multica-ai#6405). No app server: `page.setContent` with a real
 * `sandbox="allow-scripts"` srcdoc iframe, injecting the exact bridge
 * script that ships in the bundle. These pin facts jsdom cannot verify:
 * sandboxed srcdoc throws on sessionStorage, and a real ResizeObserver
 * re-positions after a delayed content append.
 */

const TOKEN = "chrometok";

function pageWithIframe(bodyHtml: string): string {
  // p{height:1200px} plus a 5000px bottom spacer so scroll targets like 600
  // are reachable regardless of the test viewport's height.
  const userHtml = `<style>body{margin:0} p{height:1200px}</style>${bodyHtml}<div style="height:5000px"></div>`;
  const srcDoc = userHtml + buildScrollBridge(TOKEN);
  const attr = srcDoc.replace(/&/g, "&amp;").replace(/"/g, "&quot;");
  return `<!doctype html><html><body style="margin:0">
<iframe id="f" sandbox="allow-scripts" srcdoc="${attr}" style="width:100vw;height:100vh;border:0"></iframe>
<script>
  window.__messages = [];
  window.addEventListener("message", (e) => {
    if (e.data && e.data.__multica === "__multica") window.__messages.push(e.data);
  });
</script></body></html>`;
}

async function attachPage(page: import("@playwright/test").Page): Promise<Frame> {
  await page.setContent(pageWithIframe("<p>loaded</p>"));
  // Wait until the iframe's document is parsed (the bridge announces ready).
  await expect
    .poll(async () => {
      const msgs = await page.evaluate(() =>
        (window as unknown as { __messages: unknown[] }).__messages.filter(
          (m) => (m as { kind?: string }).kind === "ready",
        ),
      );
      return msgs.length;
    })
    .toBeGreaterThan(0);
  const handle = await page.$("#f");
  const frame = await handle!.contentFrame();
  expect(frame).toBeTruthy();
  return frame!;
}

test.describe("iframe scroll bridge (real Chromium, sandboxed srcdoc)", () => {
  test("sandbox without allow-same-origin blocks sessionStorage (documents why we cannot use it)", async ({
    page,
  }) => {
    await page.setContent(
      `<!doctype html><body><iframe id=f sandbox="allow-scripts" srcdoc="<script>try{sessionStorage.getItem('x');parent.postMessage('ok','*')}catch(e){parent.postMessage('SECURITY_ERROR','*')}</scr` +
        `ipt>"></iframe><script>window.__result=null;window.addEventListener('message',e=>{window.__result=e.data})</script></body>`,
    );
    await expect
      .poll(async () => page.evaluate(() => (window as unknown as { __result: unknown }).__result), {
        timeout: 10000,
      })
      .toBe("SECURITY_ERROR");
  });

  test("reports scroll position to the parent with the correct token", async ({
    page,
  }) => {
    const frame = await attachPage(page);
    await frame.evaluate(() => window.scrollTo(0, 400));
    await expect
      .poll(
        async () => {
          const all = await page.evaluate(() =>
            (window as unknown as { __messages: Record<string, unknown>[] }).__messages,
          );
          return all.find((m) => m.kind === "scroll" && (m.y as number) >= 400);
        },
        { timeout: 5000 },
      )
      .toMatchObject({ y: 400, token: TOKEN, kind: "scroll" });
  });

  test("restores scroll position on a parent restore message", async ({
    page,
  }) => {
    const frame = await attachPage(page);
    await page.evaluate(
      ([token]) => {
        const w = document.querySelector<HTMLIFrameElement>("#f")!.contentWindow!;
        w.postMessage(
          { __multica: "__multica", kind: "restore", y: 600, token },
          "*",
        );
      },
      [TOKEN],
    );
    await expect
      .poll(async () => frame.evaluate(() => window.scrollY), { timeout: 5000 })
      .toBe(600);
  });

  test("holds position across a 1s-delayed content append, then yields on user wheel (v4 guarantee)", async ({
    page,
  }) => {
    const frame = await attachPage(page);

    await page.evaluate(
      ([token]) => {
        const w = document.querySelector<HTMLIFrameElement>("#f")!.contentWindow!;
        w.postMessage(
          { __multica: "__multica", kind: "restore", y: 500, token },
          "*",
        );
      },
      [TOKEN],
    );
    // Wait > 33ms so a "2 stable frames then end session" (v3 bug) would
    // already have terminated the restoring session.
    await page.waitForTimeout(250);

    // Append content 1s after restore; ResizeObserver must re-apply target.
    await frame.evaluate(() =>
      window.setTimeout(() => {
        const div = document.createElement("div");
        div.style.height = "3000px";
        document.body.appendChild(div);
      }, 1000),
    );
    // Allow the append + RO callback to run.
    await page.waitForTimeout(1500);
    expect(await frame.evaluate(() => window.scrollY)).toBe(500);

    // User takeover: dispatch a real wheel event over the iframe.
    await page.mouse.move(50, 50);
    await page.mouse.wheel(0, 120);
    await page.waitForTimeout(150);

    // After takeover, further growth + programmatic scroll must not be
    // forced back to 500.
    await frame.evaluate(() => {
      const div = document.createElement("div");
      div.style.height = "2000px";
      document.body.appendChild(div);
      window.scrollTo(0, 900);
    });
    await page.waitForTimeout(200);
    expect(await frame.evaluate(() => window.scrollY)).toBe(900);
  });

  test("ignores restore messages with a mismatched token", async ({
    page,
  }) => {
    const frame = await attachPage(page);
    await page.evaluate(() => {
      const w = document.querySelector<HTMLIFrameElement>("#f")!.contentWindow!;
      w.postMessage(
        { __multica: "__multica", kind: "restore", y: 777, token: "old" },
        "*",
      );
    });
    await page.waitForTimeout(200);
    expect(await frame.evaluate(() => window.scrollY)).toBe(0);
  });

  test("parent handshake is bounded (no ready→request-sync→ready loop) and restores once", async ({
    page,
  }) => {
    // Faithful in-page port of the PARENT side of useHtmlPreviewScrollRestore:
    //   - request-sync is sent only from the ref/onLoad handshake points;
    //   - on `ready` the parent records position and issues restore AT MOST
    //     ONCE per token — it never replies with another request-sync.
    // Before the fix, ready → request-sync → ready looped forever.
    const targetTop = 540;
    await page.setContent(pageWithIframe("<p>loaded</p>"));
    const counts = await page.evaluate(
      ([token, target]) =>
        new Promise<{
          ready: number;
          requestSync: number;
          restore: number;
          scroll: number;
        }>((resolve) => {
          const MARK = "__multica";
          const iframe = document.querySelector<HTMLIFrameElement>("#f")!;
          let restoreIssued = false;
          let requestSync = 0;
          let restore = 0;
          let ready = 0;
          let scroll = 0;

          function post(msg: Record<string, unknown>) {
            iframe.contentWindow!.postMessage({ [MARK]: MARK, token, ...msg }, "*");
          }
          function sendRequestSync() {
            requestSync++;
            post({ kind: "request-sync" });
          }
          function sendRestore() {
            if (restoreIssued) return;
            if (target <= 0) return;
            restoreIssued = true;
            restore++;
            post({ kind: "restore", y: target });
          }

          window.addEventListener("message", (e) => {
            if (e.source !== iframe.contentWindow) return;
            const d = e.data as
              | { __multica?: string; token?: string; kind?: string }
              | undefined;
            if (!d || d.__multica !== MARK || d.token !== token) return;
            if (d.kind === "ready") {
              ready++;
              // THE FIX: record + restore, but do NOT send request-sync here.
              sendRestore();
            } else if (d.kind === "scroll") {
              scroll++;
            }
          });

          // Ref callback (element attached) and onLoad each fire exactly one
          // request-sync — the only two legitimate request-sync moments.
          sendRequestSync();
          iframe.addEventListener("load", () => {
            sendRequestSync();
            sendRestore();
          });

          // Let any would-be loop run; then snapshot the counts twice.
          window.setTimeout(() => {
            const first = { ready, requestSync, restore, scroll };
            window.setTimeout(() => {
              resolve({
                ready: ready - first.ready,
                requestSync: requestSync - first.requestSync,
                restore: restore - first.restore,
                scroll: scroll - first.scroll,
              });
              // We resolve with the DELTA — it must be all zeros (no loop).
            }, 400);
          }, 600);
        }),
      [TOKEN, targetTop],
    );

    // No messages were added in the 400ms observation window → no loop.
    expect(counts).toEqual({ ready: 0, requestSync: 0, restore: 0, scroll: 0 });

    // Restore actually happened in the iframe (proves the handshake still
    // completes, not just that it went quiet).
    const handle = await page.$("#f");
    const frame = await handle!.contentFrame();
    await expect
      .poll(async () => frame!.evaluate(() => window.scrollY), { timeout: 5000 })
      .toBe(targetTop);
  });
});
