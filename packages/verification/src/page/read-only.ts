import { randomUUID } from "node:crypto";
import { chromium, type BrowserContext, type Browser } from "playwright";
import type { Bindings } from "../sources/bindings.js";
import { problem, VerificationError } from "../host/problem.js";

export type ReadOnlyPageRuntime = {
  browser: Browser;
  context: BrowserContext;
  diagnostic(path: string, environmentId: string): unknown;
  close(): Promise<void>;
};
export async function launchReadOnlyPages(
  webOrigin: string,
  bindings: Bindings,
  sourceRef: string,
  debugPort: number,
): Promise<ReadOnlyPageRuntime> {
  const binding = bindings.get(sourceRef);
  if (binding.kind !== "backend")
    throw new VerificationError(
      problem(
        "unsupportedCapability",
        "Read-only application pages require a backend source; daemon snapshots remain available independently",
      ),
    );
  const browser = await chromium.launch({
    headless: false,
    args: [
      `--remote-debugging-port=${debugPort}`,
      "--remote-debugging-address=127.0.0.1",
    ],
  });
  try {
    const context = await browser.newContext({
      serviceWorkers: "block",
      acceptDownloads: false,
    });
    await installReadOnlyRoutes(context, webOrigin, bindings, sourceRef);
    await context.addCookies([
      { name: "multica_logged_in", value: "1", url: webOrigin },
    ]);
    const generation = randomUUID();
    let cursor = 0;
    const events: {
      cursor: number;
      type: string;
      occurredAt: string;
      payload: unknown;
    }[] = [];
    const record = (type: string, payload: unknown) => {
      events.push({
        cursor: ++cursor,
        type,
        occurredAt: new Date().toISOString(),
        payload,
      });
      if (events.length > 2000) events.shift();
    };
    context.on("page", (page) => {
      record("window.created", { url: page.url() });
      page.on("pageerror", (error) =>
        record("page.error", { message: error.message.slice(0, 8192) }),
      );
      page.on("close", () => record("window.closed", {}));
      page.on("crash", () => record("renderer.crashed", {}));
    });
    const page = await context.newPage();
    await page.goto(`${webOrigin}/login`, {
      waitUntil: "domcontentloaded",
      timeout: 120000,
    });
    return {
      browser,
      context,
      close: () => browser.close(),
      diagnostic(path, environmentId) {
        const url = new URL(path, "http://127.0.0.1");
        if (
          url.pathname === "/snapshot" &&
          url.searchParams.get("source") === "page"
        )
          return {
            schemaVersion: 1,
            environmentId,
            generation,
            cursor,
            payload: {
              applicationKind: "webBrowser",
              windows: context
                .pages()
                .map((page) => ({ url: page.url(), closed: page.isClosed() })),
            },
          };
        if (url.pathname === "/events") {
          const after = Number(url.searchParams.get("after"));
          return {
            schemaVersion: 1,
            generation,
            cursor,
            earliestCursor: events[0]?.cursor ?? cursor + 1,
            hasMore: false,
            events: events.filter((event) => event.cursor > after),
          };
        }
        if (url.pathname === "/capabilities")
          return {
            schemaVersion: 1,
            applicationKind: "webBrowser",
            environmentId,
            generation,
            cursor,
            capabilities: ["page.snapshot", "page.events"],
            completeness: "hostObservedBrowserEvents",
          };
        throw new VerificationError(
          problem(
            "unsupportedCapability",
            "This page runtime has no Electron main or daemon diagnostic source",
          ),
        );
      },
    };
  } catch (error) {
    await browser.close();
    throw error;
  }
}

export async function installReadOnlyRoutes(
  context: BrowserContext,
  webOrigin: string,
  bindings: Bindings,
  sourceRef: string,
) {
  const binding = bindings.get(sourceRef),
    localOrigin = new URL(webOrigin).origin;
  await context.routeWebSocket("**/*", (socket) => socket.close());
  await context.route("**/*", async (route) => {
    const request = route.request(),
      url = new URL(request.url());
    if (request.method() !== "GET" || url.origin !== localOrigin) {
      await route.abort("blockedbyclient");
      return;
    }
    if (url.pathname.startsWith("/api/")) {
      try {
        if (
          request.headers()["x-workspace-id"] &&
          request.headers()["x-workspace-id"] !== binding.scope.workspaceId
        )
          throw new Error("Workspace changed");
        const workspaceRoute = /^\/api\/workspaces\/([^/]+)/.exec(url.pathname);
        if (
          workspaceRoute &&
          decodeURIComponent(workspaceRoute[1]!) !== binding.scope.workspaceId
        )
          throw new Error("Workspace changed");
        if (
          /^\/api\/attachments\/[^/]+\/(download|content)$/.test(url.pathname)
        )
          await bindings.request(
            sourceRef,
            url.pathname.replace(/\/(download|content)$/, ""),
          );
        const data = await bindings.bytes(
          sourceRef,
          `${url.pathname}${url.search}`,
        );
        if (
          url.pathname === "/api/me" &&
          JSON.parse(data.toString()).id !== binding.scope.userId
        )
          throw new Error("Account changed");
        let body = data;
        if (url.pathname === "/api/workspaces") {
          const value: unknown = JSON.parse(data.toString());
          if (!Array.isArray(value))
            throw new Error("Unsupported workspace list");
          body = Buffer.from(
            JSON.stringify(
              value.filter((item) => item?.id === binding.scope.workspaceId),
            ),
          );
        }
        await route.fulfill({
          status: 200,
          contentType: "application/json",
          body,
        });
      } catch {
        await route.fulfill({
          status: 403,
          contentType: "application/json",
          body: JSON.stringify({ error: "read_only_source_unavailable" }),
        });
      }
      return;
    }
    // Only the local app and its static resources reach the dev server. API,
    // uploads, auth, websocket and optimized remote-image proxies cannot bypass the guard.
    if (
      /^\/(auth|uploads|v1|ws|docs)(\/|$)/.test(url.pathname) ||
      url.pathname === "/_next/image"
    ) {
      await route.abort("blockedbyclient");
      return;
    }
    await route.continue();
  });
}
