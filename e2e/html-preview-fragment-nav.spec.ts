import { expect, test, type Page } from "@playwright/test";
import { createTestApi, loginAsDefault } from "./helpers";
import type { TestApiClient } from "./fixtures";
import { withFragmentNavShim } from "../packages/views/editor/utils/iframe-fragment-nav";
import { withLocationBridge } from "../packages/views/editor/utils/iframe-location-bridge";

const apiBase = process.env.NEXT_PUBLIC_API_URL || `http://localhost:${process.env.PORT || "8080"}`;
const report = `<!doctype html><html lang="en"><head><title>Fragment report</title>
<style>
body { margin: 40px; font: 18px system-ui; color: #172033; background: #fff; }
.view { display: none; padding: 24px; border: 1px solid #b9c8de; border-radius: 12px; }
.view:target, body:not(:has(.view:target)) #page-1 { display: block; }
a { display: inline-block; margin-right: 24px; color: #194eb7; }
</style></head><body>
<section class="view" id="page-1"><h1>Report: page 1</h1>
<p>Fictional report for fragment navigation verification.</p>
<a href="#page-2">Next</a><a href="#detail">Details</a></section>
<section class="view" id="page-2"><h1>Report: page 2</h1>
<a href="#page-1">Previous</a></section>
<section class="view" id="detail"><h1>Report: details</h1>
<a href="#detail">Details again</a><a href="#page-1">Return to home</a></section>
<output id="hash-events" hidden>0</output>
<script>
window.addEventListener('hashchange', function() {
  var output = document.getElementById('hash-events');
  output.textContent = String(Number(output.textContent) + 1);
});
window.addEventListener('keydown', function(e) {
  if (e.key === 'ArrowRight') location.hash = '#page-2';
});
window.addEventListener('click', function(e) {
  document.documentElement.dataset.clickPrevented = String(e.defaultPrevented);
});
</script></body></html>`;

const longReport = `<!doctype html><html><body>
  <nav style="position:fixed;top:0;background:white">
    <a href="#section">Section</a>
    <a href="#%E4%B8%AD%E6%96%87">Encoded</a>
    <a href="#legacy">Legacy</a>
    <a href="#section" onclick="event.preventDefault()">Author handled</a>
    <a href="#/route">Hash router</a>
  </nav>
  <div style="height:2000px"></div><h1 id="section">Section</h1>
  <div style="height:2000px"></div><h1 id="中文">Encoded section</h1>
  <div style="height:2000px"></div><a name="legacy">Legacy section</a>
  <div style="height:1000px"></div>
</body></html>`;

function requireLocal(baseURL: string) {
  const local = (url: string) =>
    ["localhost", "127.0.0.1", "[::1]"].includes(new URL(url).hostname);
  test.skip(!local(baseURL) || !local(apiBase), "Isolated local fixtures only");
}

async function openPreview(page: Page, api: TestApiClient, surface: "standalone" | "modal", html = report) {
  const slug = await loginAsDefault(page);
  const previousUrl = page.url();
  const issue = await api.createIssue("HTML fragment navigation regression");
  const form = new FormData();
  form.append("file", new Blob([html], { type: "text/html" }), "fragment-report.html");
  form.append("issue_id", issue.id);
  const uploaded = await fetch(`${apiBase}/api/upload-file`, {
    method: "POST",
    headers: { Authorization: `Bearer ${api.getToken()}`, "X-Workspace-Slug": slug },
    body: form,
  });
  expect(uploaded.status).toBe(200);
  const attachment = await uploaded.json();
  if (surface === "standalone") {
    await page.goto(`/${slug}/attachments/${attachment.id}/preview`);
  } else {
    await api.updateIssue(issue.id, {
      description: `!file[fragment-report.html](${attachment.markdown_url})`,
    });
    await page.goto(`/${slug}/issues/${issue.identifier}`);
    await page.getByRole("button", { name: "Preview", exact: true }).first().click();
    await expect(page.getByRole("dialog")).toBeVisible();
  }
  const iframe = page.locator('iframe[title="fragment-report.html"]');
  await expect(iframe).toBeVisible();
  return { iframe, previousUrl };
}

for (const surface of ["standalone", "modal"] as const) {
  test(`HTML fragment links preserve pagination without growing history in ${surface}`, async ({ page, baseURL }, testInfo) => {
    test.setTimeout(120000);
    requireLocal(baseURL!);
    await page.setViewportSize({ width: 1440, height: 900 });
    const api = await createTestApi();
    try {
      const { iframe } = await openPreview(page, api, surface);
      await expect(iframe).toHaveAttribute("sandbox", "allow-scripts");
      const frame = iframe.contentFrame();
      await expect(frame.getByRole("heading", { name: "Report: page 1" })).toBeVisible();
      const documentFrame = (await (await iframe.elementHandle())!.contentFrame())!;
      const timeOrigin = await documentFrame.evaluate(() => performance.timeOrigin);
      const historyLength = await page.evaluate(() => history.length);

      await frame.getByRole("link", { name: "Next", exact: true }).click();
      await testInfo.attach("after-next-state", {
        body: Buffer.from(JSON.stringify(await documentFrame.evaluate(() => ({
          hash: location.hash,
          target: document.querySelector(":target")?.id ?? null,
          visible: [...document.querySelectorAll(".view")]
            .filter((el) => getComputedStyle(el).display !== "none").map((el) => el.id),
          defaultPrevented: document.documentElement.dataset.clickPrevented,
        })))),
        contentType: "application/json",
      });
      const afterNext = testInfo.outputPath("after-next.png");
      await page.screenshot({ path: afterNext });
      await testInfo.attach("after-next", { path: afterNext, contentType: "image/png" });
      await expect(frame.getByRole("heading", { name: "Report: page 2" })).toBeVisible();
      await expect(page.getByRole("textbox", { name: "Address", exact: true })).toHaveValue("#page-2");
      await expect(frame.locator("#hash-events")).toHaveText("1");
      expect(await page.evaluate(() => history.length)).toBe(historyLength);

      await frame.getByRole("link", { name: "Previous", exact: true }).click();
      await expect(frame.getByRole("heading", { name: "Report: page 1" })).toBeVisible();
      await frame.getByRole("link", { name: "Details", exact: true }).click();
      await expect(frame.getByRole("heading", { name: "Report: details" })).toBeVisible();
      await expect(frame.locator("#hash-events")).toHaveText("3");
      await expect(page.getByRole("textbox", { name: "Address", exact: true })).toHaveValue("#detail");
      expect(await page.evaluate(() => history.length)).toBe(historyLength);
      await frame.getByRole("link", { name: "Details again", exact: true }).click();
      expect(await documentFrame.evaluate(() => history.length)).toBe(historyLength);
      await expect(frame.locator("#hash-events")).toHaveText("3");

      const details = testInfo.outputPath("details.png");
      await page.screenshot({ path: details });
      await testInfo.attach("details", { path: details, contentType: "image/png" });
      await frame.getByRole("link", { name: "Return to home", exact: true }).click();
      await expect(frame.getByRole("heading", { name: "Report: page 1" })).toBeVisible();
      expect(await page.evaluate(() => history.length)).toBe(historyLength);
      expect(await documentFrame.evaluate(() => performance.timeOrigin)).toBe(timeOrigin);

      // Restoring navigation must not grant uploaded HTML access to its host.
      expect(await documentFrame.evaluate(() => {
        const blocked = (read: () => unknown) => { try { read(); return false; } catch { return true; } };
        return {
          parent: blocked(() => parent.document),
          cookies: blocked(() => document.cookie),
          storage: blocked(() => localStorage.length),
        };
      })).toEqual({ parent: true, cookies: true, storage: true });
    } finally {
      await api.cleanup();
    }
  });
}

for (const [kind, html, links] of [
  ["pagination", report, ["Next", "Previous", "Details", "Return to home"]],
  ["ordinary anchors", longReport, ["Section", "Encoded", "Legacy"]],
] as const) {
  test(`closing ${kind} preview leaves with one browser Back`, async ({ page, baseURL }, testInfo) => {
    test.setTimeout(120000);
    requireLocal(baseURL!);
    const api = await createTestApi();
    try {
      const { iframe, previousUrl } = await openPreview(page, api, "modal", html);
      const historyBefore = await page.evaluate(() => history.length);
      for (const name of links) {
        await iframe.contentFrame().getByRole("link", { name, exact: true }).click();
      }
      const historyBeforeClose = await page.evaluate(() => history.length);
      await page.getByRole("dialog").getByRole("button", { name: "Close", exact: true }).click();
      await expect(page.getByRole("dialog")).toBeHidden();
      await page.goBack({ waitUntil: "domcontentloaded" });
      await testInfo.attach("one-back-state", {
        body: Buffer.from(JSON.stringify({ kind, historyBefore, historyBeforeClose, previousUrl, afterOneBack: page.url() })),
        contentType: "application/json",
      });
      await expect(page).toHaveURL(previousUrl);
      await expect(page.getByRole("button", { name: "New issue", exact: true }).first()).toBeVisible();
      await expect(page.getByText("HTML fragment navigation regression", { exact: true })).toBeVisible();
      const screenshot = testInfo.outputPath("after-one-back.png");
      await page.screenshot({ path: screenshot });
      await testInfo.attach("after-one-back", { path: screenshot, contentType: "image/png" });
      expect(historyBeforeClose).toBe(historyBefore);
    } finally {
      await api.cleanup();
    }
  });
}

test("report-authored hash writes keep their own history behavior", async ({ page, baseURL, browserName }) => {
  test.setTimeout(120000);
  test.skip(browserName !== "chromium", "Chromium retains removed iframe history");
  requireLocal(baseURL!);
  const api = await createTestApi();
  try {
    const { iframe, previousUrl } = await openPreview(page, api, "modal");
    const issueUrl = page.url();
    const historyBefore = await page.evaluate(() => history.length);
    await iframe.contentFrame().getByRole("heading", { name: "Report: page 1" }).click();
    // The report's own key handler assigns location.hash, outside the shim.
    await page.keyboard.press("ArrowRight");
    await expect(iframe.contentFrame().getByRole("heading", { name: "Report: page 2" })).toBeVisible();
    await expect(page.getByRole("textbox", { name: "Address", exact: true })).toHaveValue("#page-2");
    expect(await page.evaluate(() => history.length)).toBe(historyBefore + 1);
    await iframe.contentFrame().getByRole("link", { name: "Previous", exact: true }).click();
    await page.getByRole("dialog").getByRole("button", { name: "Close", exact: true }).click();
    await expect(page.getByRole("dialog")).toBeHidden();
    await page.goBack({ waitUntil: "domcontentloaded" });
    await expect(page).toHaveURL(issueUrl);
    await page.goBack({ waitUntil: "domcontentloaded" });
    await expect(page).toHaveURL(previousUrl);
  } finally {
    await api.cleanup();
  }
});

// Browser-level coverage for the shipped shim and bridge; no product fixtures.
test("fragment shim keeps long-document anchors and author handlers working", async ({ page }) => {
  const html = withLocationBridge(withFragmentNavShim(longReport), "");
  await page.setContent('<iframe sandbox="allow-scripts" title="Long document" style="width:800px;height:400px"></iframe>');
  await page.locator("iframe").evaluate((element, source) => { element.setAttribute("srcdoc", source); }, html);
  const frame = page.frameLocator("iframe");
  const documentFrame = (await (await page.locator("iframe").elementHandle())!.contentFrame())!;
  for (const [label, hash, selector] of [
    ["Section", "#section", "#section"],
    ["Encoded", "#%E4%B8%AD%E6%96%87", "#中文"],
    ["Legacy", "#legacy", 'a[name="legacy"]'],
  ]) {
    await frame.getByRole("link", { name: label, exact: true }).click();
    await expect.poll(() => documentFrame.evaluate(() => location.hash)).toBe(hash);
    await expect.poll(() => frame.locator(selector).evaluate((el) => Math.abs(el.getBoundingClientRect().top))).toBeLessThan(2);
  }
  await frame.getByRole("link", { name: "Author handled", exact: true }).click();
  expect(await documentFrame.evaluate(() => location.hash)).toBe("#legacy");
  await frame.getByRole("link", { name: "Hash router", exact: true }).click();
  await expect.poll(() => documentFrame.evaluate(() => location.hash)).toBe("#/route");
});
