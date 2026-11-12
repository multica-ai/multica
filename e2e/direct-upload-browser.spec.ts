/**
 * E2E: a real browser uploads an attachment straight to object storage.
 *
 * This is the browser counterpart of `scripts/direct-upload-smoke.sh` (which
 * drives the CLI). It exists because the two failures that matter here cannot
 * be reproduced without a browser: a CORS preflight the bucket refuses, and a
 * request header the browser will not let a script set. A jsdom test proves
 * neither — it has no preflight, no forbidden-header list, and no cross-origin
 * policy.
 *
 * Run it through `bash scripts/direct-upload-browser-smoke.sh`, which provisions
 * the isolated stack (its own database, its own S3-compatible server, a proxy
 * that rejects API bodies above 1 MiB, and the web app) and passes the
 * environment below. Anywhere else it skips: without its own bucket and proxy,
 * a pass would prove nothing.
 */
import { test, expect, type Page } from "@playwright/test";
import { createHash } from "node:crypto";
import { readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";

const SMOKE_DIR = process.env.DIRECT_UPLOAD_SMOKE_DIR ?? "";
const WEB_ORIGIN = process.env.DIRECT_UPLOAD_WEB_ORIGIN ?? "";
const STORAGE_ORIGIN = process.env.DIRECT_UPLOAD_STORAGE_ORIGIN ?? "";
// An http origin that is NOT the app (so not in the bucket's CORS rule) and NOT
// the bucket (so the request really is cross-origin). It serves a plain page, so
// the probe can run there.
const FOREIGN_ORIGIN = process.env.DIRECT_UPLOAD_FOREIGN_ORIGIN ?? "";
const DEV_CODE = process.env.MULTICA_DEV_VERIFICATION_CODE ?? "888888";

test.skip(
  !SMOKE_DIR || !WEB_ORIGIN || !STORAGE_ORIGIN || !FOREIGN_ORIGIN,
  "set by scripts/direct-upload-browser-smoke.sh; it provisions the isolated stack",
);

// A machine that already has a Chromium of a different Playwright revision can
// point at it instead of downloading one. CORS and the forbidden-header list are
// enforced by the browser engine, not by one build of it, so this does not weaken
// what the run proves.
if (process.env.DIRECT_UPLOAD_CHROME_PATH) {
  test.use({ launchOptions: { executablePath: process.env.DIRECT_UPLOAD_CHROME_PATH } });
}

/** A PNG of incompressible pixels: a real image the renderer can preview. */
function writeNoisePng(path: string, width: number, height: number): { bytes: number; sha256: string } {
  const raw = Buffer.alloc(height * (1 + width * 3));
  for (let y = 0; y < height; y += 1) {
    const rowStart = y * (1 + width * 3);
    raw[rowStart] = 0; // filter type: none
    for (let x = 0; x < width * 3; x += 1) raw[rowStart + 1 + x] = (y * 31 + x * 17 + ((x * y) % 251)) & 0xff;
  }
  const png = Buffer.concat([
    Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]),
    pngChunk("IHDR", (() => {
      const ihdr = Buffer.alloc(13);
      ihdr.writeUInt32BE(width, 0);
      ihdr.writeUInt32BE(height, 4);
      ihdr[8] = 8; // bit depth
      ihdr[9] = 2; // colour type: truecolour
      return ihdr;
    })()),
    // level 0 keeps the pixels' entropy: a deflated noise image still lands well
    // above the proxy's 1 MiB body limit.
    pngChunk("IDAT", deflateStored(raw)),
    pngChunk("IEND", Buffer.alloc(0)),
  ]);
  writeFileSync(path, png);
  return { bytes: png.length, sha256: createHash("sha256").update(png).digest("hex") };
}

/** deflate with stored blocks, so the output is large and the CRC still valid. */
function deflateStored(input: Buffer): Buffer {
  const out: Buffer[] = [Buffer.from([0x78, 0x01])];
  const maxBlock = 0xffff;
  for (let offset = 0; offset < input.length; offset += maxBlock) {
    const block = input.subarray(offset, Math.min(offset + maxBlock, input.length));
    const final = offset + maxBlock >= input.length ? 1 : 0;
    const header = Buffer.alloc(5);
    header[0] = final;
    header.writeUInt16LE(block.length, 1);
    header.writeUInt16LE(~block.length & 0xffff, 3);
    out.push(header, block);
  }
  // Adler-32 of the uncompressed data.
  let a = 1;
  let b = 0;
  for (const byte of input) {
    a = (a + byte) % 65521;
    b = (b + a) % 65521;
  }
  const adler = Buffer.alloc(4);
  adler.writeUInt32BE(((b << 16) | a) >>> 0, 0);
  out.push(adler);
  return Buffer.concat(out);
}

function pngChunk(type: string, data: Buffer): Buffer {
  const length = Buffer.alloc(4);
  length.writeUInt32BE(data.length, 0);
  const typed = Buffer.concat([Buffer.from(type, "ascii"), data]);
  const crc = Buffer.alloc(4);
  crc.writeUInt32BE(crc32(typed), 0);
  return Buffer.concat([length, typed, crc]);
}

function crc32(buf: Buffer): number {
  let crc = 0xffffffff;
  for (const byte of buf) {
    crc ^= byte;
    for (let bit = 0; bit < 8; bit += 1) crc = crc & 1 ? 0xedb88320 ^ (crc >>> 1) : crc >>> 1;
  }
  return (crc ^ 0xffffffff) >>> 0;
}

/**
 * A same-origin JSON fetch from inside the page, mirroring what the app's
 * ApiClient sends for a write: the session cookie rides along automatically,
 * and the CSRF cookie is echoed in `X-CSRF-Token` — a cookie-mode write without
 * it is rejected with 403 by the server's CSRF middleware.
 *
 * Installed once per page and reused, so a helper cannot drift from the real
 * request shape between calls.
 */
async function installPageApi(page: Page): Promise<void> {
  await page.addInitScript(() => {
    const readCookie = (name: string): string | null => {
      const prefix = `${name}=`;
      const hit = document.cookie.split("; ").find((entry) => entry.startsWith(prefix));
      return hit ? hit.slice(prefix.length) || null : null;
    };
    // Session-bound value first, matching packages/core/api/client.ts: it
    // survives a sliding renewal, and the token-bound one is only the fallback.
    (window as unknown as { smokeApi: unknown }).smokeApi = async (
      path: string,
      init: { method?: string; body?: unknown; workspaceSlug?: string } = {},
    ) => {
      const headers: Record<string, string> = { "Content-Type": "application/json" };
      const csrf = readCookie("multica_csrf_session") ?? readCookie("multica_csrf");
      if (csrf) headers["X-CSRF-Token"] = csrf;
      if (init.workspaceSlug) headers["X-Workspace-Slug"] = init.workspaceSlug;
      const res = await fetch(path, {
        method: init.method ?? "GET",
        headers,
        body: init.body === undefined ? undefined : JSON.stringify(init.body),
      });
      const text = await res.text();
      return { status: res.status, body: text ? (JSON.parse(text) as unknown) : null };
    };
  });
}

interface SignupIdentity {
  email: string;
  slug: string;
}

/**
 * Signs in through the real login form, so the session is the app's own cookie.
 *
 * The account setup after login — creating the workspace and finishing
 * onboarding — goes through the same session's own API rather than the
 * questionnaire UI. Onboarding is not what this file tests, and driving it would
 * add a dozen unrelated assertions between the login and the upload. Everything
 * the upload depends on (the session cookie, the workspace header, the CSRF
 * cookie) is still exactly what the app itself uses.
 */
async function signInAndPrepare(page: Page, identity: SignupIdentity): Promise<void> {
  await page.goto(`${WEB_ORIGIN}/login`, { waitUntil: "load" });
  const field = page.locator("#login-email");
  await field.waitFor({ state: "visible", timeout: 30_000 });
  // The form is server-rendered, so the input exists before React attaches to
  // it. Typing into the un-hydrated DOM updates the element but not the state
  // that enables the submit button, and the click then waits forever. The React
  // fiber property is the direct signal that hydration reached this input.
  await page.waitForFunction(() => {
    const el = document.querySelector("#login-email");
    return !!el && Object.keys(el).some((key) => key.startsWith("__reactFiber"));
  }, undefined, { timeout: 30_000 });

  await field.click();
  await field.pressSequentially(identity.email, { delay: 5 });
  await expect
    .poll(() => page.locator('button[type="submit"]').isEnabled(), { timeout: 15_000 })
    .toBe(true);
  await page.locator('button[type="submit"]').click();

  const codeInput = page.locator("input").first();
  await codeInput.waitFor({ state: "visible", timeout: 30_000 });
  await codeInput.click();
  await codeInput.pressSequentially(DEV_CODE, { delay: 10 });
  // A first-time account lands on onboarding once the code is accepted.
  await page.waitForURL((url) => url.pathname.startsWith("/onboarding"), { timeout: 30_000 });

  const prepared = await page.evaluate(async ({ slug }) => {
    const api = (window as unknown as {
      smokeApi: (path: string, init?: { method?: string; body?: unknown }) => Promise<{ status: number; body: any }>;
    }).smokeApi;
    const ws = await api("/api/workspaces", { method: "POST", body: { name: "Direct Upload Smoke", slug } });
    if (ws.status >= 300) return { error: `workspace ${ws.status}: ${JSON.stringify(ws.body)}` };
    const done = await api("/api/me/onboarding/complete", {
      method: "POST",
      body: { completion_path: "skip_existing", workspace_id: ws.body.id },
    });
    if (done.status >= 300) return { error: `onboarding ${done.status}: ${JSON.stringify(done.body)}` };
    return { workspaceId: ws.body.id as string };
  }, { slug: identity.slug });
  expect(prepared.error, `preparing the account failed: ${prepared.error}`).toBeUndefined();
}

test("a browser uploads an oversized file straight to storage and reads it back", async ({ page }, testInfo) => {
  test.setTimeout(180_000);

  const identity: SignupIdentity = {
    email: process.env.DIRECT_UPLOAD_SMOKE_EMAIL ?? "",
    slug: process.env.DIRECT_UPLOAD_SMOKE_SLUG ?? "",
  };
  expect(identity.email).not.toBe("");

  await installPageApi(page);
  await signInAndPrepare(page, identity);

  // Create the issue through the app's own session, same-origin, so the browser
  // attaches the session cookie and CSRF header exactly as the UI would.
  const created = await page.evaluate(async ({ slug }) => {
    const api = (window as unknown as {
      smokeApi: (path: string, init?: { method?: string; body?: unknown; workspaceSlug?: string }) => Promise<{ status: number; body: any }>;
    }).smokeApi;
    const issue = await api("/api/issues", {
      method: "POST",
      workspaceSlug: slug,
      body: { title: `Browser direct upload ${Date.now()}` },
    });
    if (issue.status >= 300) return { error: `issue ${issue.status}: ${JSON.stringify(issue.body)}` };
    return { identifier: issue.body.identifier as string };
  }, { slug: identity.slug });
  expect(created.error, `creating the fixture failed: ${created.error}`).toBeUndefined();
  const identifier = created.identifier!;

  const pngPath = join(SMOKE_DIR, "browser-upload.png");
  const fixture = writeNoisePng(pngPath, 700, 700);
  // Above the proxy's 1 MiB API body limit: this upload cannot succeed through
  // the multipart path, so a green test is proof the bytes went to the bucket.
  expect(fixture.bytes).toBeGreaterThan(1 << 20);

  const apiBodies: { path: string; bytes: number }[] = [];
  const storagePuts: { url: string; headers: Record<string, string> }[] = [];
  page.on("request", (req) => {
    const url = req.url();
    if (url.startsWith(WEB_ORIGIN) && url.includes("/api/")) {
      // The metadata requests carry JSON, whose body Playwright does expose.
      // A request with no body at all (a GET) has none to measure.
      const body = req.postDataBuffer();
      apiBodies.push({ path: new URL(url).pathname, bytes: body ? body.length : 0 });
    }
    if (url.startsWith(STORAGE_ORIGIN) && req.method() === "PUT") {
      // The PUT's body size is NOT asserted from here: Chromium does not expose
      // a cross-origin fetch's body to the driver (both `headers()["content-length"]`
      // and `sizes().requestBodySize` read as absent/0). The length is proven
      // where it is actually enforced — see the signed-header assertion below.
      storagePuts.push({ url, headers: req.headers() });
    }
  });

  await page.goto(`${WEB_ORIGIN}/${identity.slug}/issues/${identifier}`, { waitUntil: "load" });
  await expect(page.getByText("Properties")).toBeVisible({ timeout: 30_000 });

  // The editor shows a local blob for an in-flight upload, so a visible preview
  // only proves the file was picked. Wait for the completion request this page
  // makes, which is the point at which the attachment exists server-side.
  const completedResponse = page.waitForResponse(
    (res) => /\/api\/attachments\/[^/]+\/complete-upload$/.test(new URL(res.url()).pathname),
    { timeout: 60_000 },
  );
  await page.locator('input[type="file"]').first().setInputFiles(pngPath);
  const completion = await completedResponse;
  expect(completion.status(), "complete-upload must succeed").toBe(200);

  const preview = page.locator(".ProseMirror img").first();
  await expect(preview).toBeVisible({ timeout: 60_000 });
  await expect
    .poll(() => preview.evaluate((el: HTMLImageElement) => el.naturalWidth), { timeout: 60_000 })
    .toBeGreaterThan(0);

  // 1. The file went to storage, not through the API.
  expect(storagePuts).toHaveLength(1);
  const largestApiBody = Math.max(0, ...apiBodies.map((entry) => entry.bytes));
  expect(largestApiBody, `largest API request body was ${largestApiBody} bytes`).toBeLessThan(1 << 20);

  // 2. The storage request carried no Multica credential.
  //
  // The PUT's own body length is deliberately NOT read from the driver:
  // Chromium exposes neither `content-length` nor `sizes()` for a cross-origin
  // fetch from inside the page, so any assertion on them would be vacuous. The
  // length is proven where it is enforced — the credential signs it, the store
  // rejects a mismatch with SignatureDoesNotMatch, and the hash comparison in
  // step 4 shows the published object is byte-for-byte the fixture.
  const putHeaders = storagePuts[0]!.headers;
  expect(putHeaders.authorization).toBeUndefined();
  expect(putHeaders.cookie).toBeUndefined();
  expect(putHeaders["x-workspace-slug"]).toBeUndefined();
  expect(putHeaders["x-request-id"]).toBeUndefined();
  // The only content header a browser forwards is the one the credential names,
  // and it is the media type the presign declared for this exact File — the
  // server signs it, so a mismatch would fail the PUT with SignatureDoesNotMatch
  // rather than reach this assertion.
  expect(putHeaders["content-type"]).toBe("image/png");
  // No chunked framing: the File's length is known, so the request is framed.
  expect(putHeaders["transfer-encoding"]).toBeUndefined();

  const completedId = apiBodies
    .map((entry) => /^\/api\/attachments\/([^/]+)\/complete-upload$/.exec(entry.path)?.[1])
    .find((id) => !!id);
  expect(completedId, "the page must have completed an upload").toBeTruthy();

  // 3. The upload is published, persisted into the description, and the bytes
  //    the app renders after a reload are the uploaded bytes.
  //
  // The reload is what makes this a real read-back: an unsaved editor shows the
  // upload from a local blob URL, which proves only that the file was picked.
  // After the description save, the markdown holds the attachment's link, and a
  // fresh load resolves it to the published object in the bucket.
  // Persist the description (the editor debounces its save), so a reload reads
  // the attachment from the issue rather than from editor state.
  await page.waitForResponse(
    (res) => res.request().method() === "PUT" && /\/api\/issues\/[^/]+$/.test(new URL(res.url()).pathname),
    { timeout: 60_000 },
  );
  await page.reload({ waitUntil: "load" });
  const reloadedPreview = page.locator(".ProseMirror img").first();
  await expect(reloadedPreview).toBeVisible({ timeout: 30_000 });
  await expect
    .poll(() => reloadedPreview.evaluate((el: HTMLImageElement) => el.naturalWidth), { timeout: 30_000 })
    .toBeGreaterThan(0);

  const src = (await reloadedPreview.getAttribute("src")) ?? "";
  // Resolved against the bucket, not the app: the preview loads the published
  // object directly.
  expect(new URL(src, WEB_ORIGIN).origin).toBe(STORAGE_ORIGIN);
  expect(src).not.toContain("blob:");

  const readBack = await page.evaluate(async ({ url, id, slug }) => {
    const res = await fetch(url, { credentials: "omit" });
    if (!res.ok) return { error: `object fetch ${res.status}` };
    const buf = new Uint8Array(await res.arrayBuffer());
    const digest = await crypto.subtle.digest("SHA-256", buf);
    const sha256 = [...new Uint8Array(digest)].map((b) => b.toString(16).padStart(2, "0")).join("");
    const api = (window as unknown as {
      smokeApi: (path: string, init?: { method?: string; workspaceSlug?: string }) => Promise<{ status: number; body: any }>;
    }).smokeApi;
    const meta = await api(`/api/attachments/${encodeURIComponent(id)}`, { workspaceSlug: slug });
    const att = (meta.body ?? {}) as { filename?: string; size_bytes?: number; content_type?: string; url?: string };
    return {
      bytes: buf.length,
      sha256,
      filename: att.filename,
      size_bytes: att.size_bytes,
      content_type: att.content_type,
      objectUrl: att.url ?? "",
    };
  }, { url: src, id: completedId!, slug: identity.slug });
  expect(readBack.error, `reading back the attachment failed: ${readBack.error}`).toBeUndefined();
  expect(readBack.bytes).toBe(fixture.bytes);
  expect(readBack.sha256).toBe(fixture.sha256);
  expect(readBack.filename).toBe("browser-upload.png");
  expect(readBack.size_bytes).toBe(fixture.bytes);
  expect(readBack.content_type).toBe("image/png");
  // The published object sits under the permanent key prefix, and no staging
  // object is ever what a reader resolves.
  expect(readBack.objectUrl).toContain("/workspaces/");
  expect(readBack.objectUrl).not.toContain("/upload-staging/");
  expect(readFileSync(pngPath)).toHaveLength(fixture.bytes);

  // Interaction evidence. Written into the run's evidence directory as well as
  // attached, so the smoke script can hand a reviewer the file directly instead
  // of pointing at a gitignored `test-results/` tree.
  const shot = await page.screenshot({ fullPage: false });
  writeFileSync(join(SMOKE_DIR, "direct-upload-preview.png"), shot);
  await testInfo.attach("direct-upload-preview", {
    body: shot,
    contentType: "image/png",
  });

  // 4. CORS is real: the same-origin page may PUT with the signed header, and
  //    the bucket refuses an unlisted header and a foreign origin.
  const cors = await page.evaluate(async ({ webOrigin, slug }) => {
    const api = (window as unknown as {
      smokeApi: (path: string, init?: { method?: string; body?: unknown; workspaceSlug?: string }) => Promise<{ status: number; body: any }>;
    }).smokeApi;
    const mint = async (name: string) => {
      const res = await api("/api/attachments/presign-upload", {
        method: "POST",
        workspaceSlug: slug,
        body: { filename: name, content_type: "application/octet-stream", size_bytes: 4 },
      });
      return res.body?.upload_url as string | undefined;
    };
    const outcome = async (url: string, headers: Record<string, string>) => {
      try {
        const res = await fetch(url, {
          method: "PUT",
          headers,
          body: new Blob(["abcd"]),
          credentials: "omit",
        });
        return res.status;
      } catch (err) {
        return `blocked: ${(err as Error).message}`;
      }
    };
    const allowed = await mint(`cors-ok-${Date.now()}.bin`);
    const unlisted = await mint(`cors-header-${Date.now()}.bin`);
    if (!allowed || !unlisted) return { error: "presign failed" };
    return {
      webOrigin,
      allowedOriginAndHeaders: await outcome(allowed, { "Content-Type": "application/octet-stream" }),
      unlistedHeader: await outcome(unlisted, { "Content-Type": "application/octet-stream", "X-Unlisted-Header": "1" }),
    };
  }, { webOrigin: WEB_ORIGIN, slug: identity.slug });

  expect(cors.error).toBeUndefined();
  expect(cors.allowedOriginAndHeaders, "an allowed origin and signed header must be accepted").toBe(200);
  expect(String(cors.unlistedHeader), "an unlisted header must fail the preflight").toMatch(/^blocked:/);

  const foreignUrl = await page.evaluate(async ({ slug }) => {
    const api = (window as unknown as {
      smokeApi: (path: string, init?: { method?: string; body?: unknown; workspaceSlug?: string }) => Promise<{ status: number; body: any }>;
    }).smokeApi;
    const res = await api("/api/attachments/presign-upload", {
      method: "POST",
      workspaceSlug: slug,
      body: { filename: `cors-origin-${Date.now()}.bin`, content_type: "application/octet-stream", size_bytes: 4 },
    });
    return res.body?.upload_url as string | undefined;
  }, { slug: identity.slug });
  expect(foreignUrl).toBeTruthy();

  // A real second page on an origin the bucket's rule does not list. Loading it
  // from the bucket itself would be same-origin and prove nothing.
  const foreign = await page.context().newPage();
  await foreign.goto(`${FOREIGN_ORIGIN}/health`, { waitUntil: "domcontentloaded" });
  const foreignOutcome = await foreign.evaluate(async (url) => {
    try {
      const res = await fetch(url, {
        method: "PUT",
        headers: { "Content-Type": "application/octet-stream" },
        body: new Blob(["abcd"]),
        credentials: "omit",
      });
      return res.status;
    } catch (err) {
      return `blocked: ${(err as Error).message}`;
    }
  }, foreignUrl!);
  await foreign.close();
  expect(String(foreignOutcome), "an origin outside the bucket's CORS rule must be refused").toMatch(/^blocked:/);
});
