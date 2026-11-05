// @vitest-environment node
import { createServer, type Server } from "node:http";
import { mkdtemp, writeFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { chromium } from "playwright";
import { expect, it } from "vitest";
import { Bindings } from "../sources/bindings.js";
import { installReadOnlyRoutes } from "./read-only.js";
async function listen(server: Server) {
  await new Promise<void>((done) => server.listen(0, "127.0.0.1", done));
  const address = server.address();
  if (!address || typeof address === "string") throw new Error("missing port");
  return `http://127.0.0.1:${address.port}`;
}
it("renders authorized real responses while preventing page writes, cross-workspace reads and external traffic", async () => {
  const root = await mkdtemp(join(tmpdir(), "verification-read-only-"));
  let mutations = 0,
    reads = 0;
  const source = createServer((req, res) => {
    if (req.method !== "GET") mutations++;
    else reads++;
    res.setHeader("Content-Type", "application/json");
    res.end(
      JSON.stringify(
        req.url === "/api/me" ? { id: "user" } : { value: "source-content" },
      ),
    );
  });
  const app = createServer((_req, res) => {
    res.setHeader("Content-Type", "text/html");
    res.end('<input aria-label="Search"><div style="height:2000px">App</div>');
  });
  const browser = await chromium.launch({ headless: true });
  try {
    const sourceOrigin = await listen(source),
      webOrigin = await listen(app),
      tokenFile = join(root, "source.token");
    await writeFile(tokenFile, "test-created-token", { mode: 0o600 });
    const bindings = new Bindings([
      {
        ref: "source",
        kind: "backend",
        baseUrl: sourceOrigin,
        scope: {
          apiOrigin: sourceOrigin,
          userId: "user",
          workspaceId: "workspace",
        },
        tokenFile,
        access: "readOnly",
        identity: {},
      },
    ]);
    const context = await browser.newContext({ serviceWorkers: "block" });
    await installReadOnlyRoutes(context, webOrigin, bindings, "source");
    const page = await context.newPage();
    await page.goto(webOrigin);
    await page.getByLabel("Search").fill("query");
    await page.mouse.wheel(0, 500);
    expect(
      await page.evaluate(async () => (await fetch("/api/me")).json()),
    ).toEqual({ id: "user" });
    expect(
      await page.evaluate(async () => (await fetch("/api/projects")).json()),
    ).toEqual({ value: "source-content" });
    const outcomes = await page.evaluate(async () =>
      Promise.all([
        fetch("/api/projects", { method: "POST", body: "{}" }).then(
          () => "sent",
          () => "blocked",
        ),
        fetch("https://external.invalid/collect").then(
          () => "sent",
          () => "blocked",
        ),
        fetch("/api/workspaces/another").then((response) => response.status),
      ]),
    );
    expect(outcomes).toEqual(["blocked", "blocked", 403]);
    expect(mutations).toBe(0);
    expect(reads).toBe(2);
    expect(await page.getByLabel("Search").inputValue()).toBe("query");
  } finally {
    await browser.close();
    await Promise.all([
      new Promise<void>((done) => source.close(() => done())),
      new Promise<void>((done) => app.close(() => done())),
    ]);
    await rm(root, { recursive: true, force: true });
  }
});
