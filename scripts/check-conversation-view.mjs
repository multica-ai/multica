import { createServer } from "node:http";
import { readFile, mkdir, writeFile } from "node:fs/promises";
import { resolve, extname, join } from "node:path";
import { chromium } from "@playwright/test";

const publicDir = resolve(process.argv[2] ?? ".tmp/conversation-view-browser");
const baselinePath = process.argv[4];
const baseline = baselinePath ? JSON.parse(await readFile(baselinePath, "utf8")) : null;
const outputDir = resolve(process.argv[3] ?? ".tmp/conversation-view-check");
await mkdir(outputDir, { recursive: true });
const mime = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".woff2": "font/woff2", ".svg": "image/svg+xml", ".png": "image/png" };
const server = createServer(async (request, response) => {
  const target = resolve(publicDir, `.${decodeURIComponent(new URL(request.url, "http://localhost").pathname)}`);
  if (!target.startsWith(publicDir + "/")) { response.writeHead(403).end(); return; }
  try { response.setHeader("content-type", mime[extname(target)] ?? "application/octet-stream"); response.end(await readFile(target)); }
  catch { response.writeHead(404).end(); }
});
await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
const port = server.address().port;
let browser;
const errors = [], costs = [], failures = [];
const check = (ok, message) => { if (!ok) failures.push(message); };
try {
  browser = await chromium.launch({ channel: "chrome", headless: true });
  const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
  page.on("pageerror", (error) => errors.push(error.message));
  page.on("console", (message) => {
    const prefix = "[local-turn-timing] ";
    if (message.text().startsWith(prefix)) { try { costs.push(JSON.parse(message.text().slice(prefix.length))); } catch {} }
  });
  await page.goto(`http://127.0.0.1:${port}/performance.html`);
  await page.getByText("Live reply", { exact: true }).waitFor({ timeout: 30_000 });
  const timeline = page.getByTestId("agent-gui-timeline");
  const editor = page.locator('[contenteditable="true"]').first();
  await editor.waitFor();
  await page.waitForTimeout(500);
  const gap = () => timeline.evaluate((element) => element.scrollHeight - element.clientHeight - element.scrollTop);
  const openingGap = await gap();
  check(openingGap <= 8, `Opening did not follow the end: ${openingGap}`);
  await editor.click();
  await page.evaluate(() => { window.originalComposer = document.querySelector('[contenteditable="true"]'); });
  const cdp = await page.context().newCDPSession(page);
  await cdp.send("Performance.enable");
  const metrics = async () => Object.fromEntries((await cdp.send("Performance.getMetrics")).metrics.map(({ name, value }) => [name, value]));
  const before = await metrics();
  const started = performance.now();
  const stream = page.evaluate(() => window.conversationFixture.stream(360, 120));
  const typing = editor.pressSequentially("Typing during a live conversation stays responsive.", { delay: 8 });
  await typing;
  const typingMs = performance.now() - started;
  const afterTypingGap = await gap();
  const incoming = await stream;
  await page.waitForFunction(() => document.body.textContent.includes("streamed-token-0360"));
  await page.waitForTimeout(300);
  const after = await metrics();
  await cdp.send("HeapProfiler.collectGarbage");
  const retainedHeapBytes = (await metrics()).JSHeapUsedSize;
  const followingGap = await gap();
  const composerRetained = await page.evaluate(() => window.originalComposer === document.querySelector('[contenteditable="true"]') && window.originalComposer.textContent.includes("Typing during a live conversation stays responsive."));
  check(composerRetained, "Streaming replaced the composer or lost typed text");
  check(followingGap <= 8, `Streaming lost the end: ${followingGap} (baseline ${baseline?.followingGap ?? "unavailable"})`);
  await timeline.hover(); await page.mouse.wheel(0, -700); await page.waitForTimeout(300);
  const detachedTop = await timeline.evaluate((element) => element.scrollTop);
  check(await gap() >= 100, "The scroll did not detach");
  await page.evaluate(() => window.conversationFixture.stream(120, 120));
  await page.waitForFunction(() => document.body.textContent.includes("streamed-token-0480"));
  await page.waitForTimeout(200);
  const detachedDrift = await timeline.evaluate((element, before) => Math.abs(element.scrollTop - before), detachedTop);
  check(detachedDrift <= 8, `Streaming moved a detached reader: ${detachedDrift}`);
  await page.screenshot({ path: join(outputDir, "light.png") });
  await page.evaluate(() => window.conversationFixture.select("second"));
  await page.getByText("Prompt 119 in second", { exact: true }).waitFor();
  await page.evaluate(() => window.conversationFixture.select("first"));
  await page.waitForFunction(() => document.body.textContent.includes("streamed-token-0480"));
  await page.waitForTimeout(300);
  const returnDrift = await timeline.evaluate((element, before) => Math.abs(element.scrollTop - before), detachedTop);
  check(returnDrift <= 8, `Returning did not restore the scroll position: ${returnDrift}`);
  await page.evaluate(() => window.conversationFixture.theme("dark"));
  await page.waitForTimeout(200);
  await page.screenshot({ path: join(outputDir, "dark.png") });
  if (errors.length) throw new Error(errors.join("\n"));
  const seconds = name => Math.round((after[name] - before[name]) * 1000);
  const report = { fixtureVersion: 1, turnCount: 120, requestedEventsPerSecond: 120, incoming, typingMs: Math.round(typingMs),
    scriptMs: seconds("ScriptDuration"), taskMs: seconds("TaskDuration"), layoutMs: seconds("LayoutDuration"), styleMs: seconds("RecalcStyleDuration"), heapBytes: after.JSHeapUsedSize,
    retainedHeapBytes,
    baselinePath: baselinePath ?? null, knownFollowingGap: followingGap > 8, openingGap, afterTypingGap, followingGap, detachedDrift, returnDrift, composerRetained, errors, failures, costs };
  await writeFile(join(outputDir, "report.json"), JSON.stringify(report, null, 2) + "\n");
  console.log(JSON.stringify({ ...report, costs: `${costs.length} timing records` }, null, 2));
  if (failures.length) process.exitCode = 1;
} finally {
  await browser?.close();
  await new Promise((resolve) => server.close(resolve));
}
