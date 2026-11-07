import {
  chromium,
  type Browser,
  type Frame,
  type Locator,
  type Page,
} from "playwright";
import { z } from "zod";
import type { Environments } from "../setup/environments.js";
import { problem, VerificationError } from "../host/problem.js";

export const locatorSchema = z.discriminatedUnion("kind", [
  z
    .object({ kind: z.literal("testId"), value: z.string().min(1).max(200) })
    .strict(),
  z
    .object({
      kind: z.literal("text"),
      value: z.string().min(1).max(200),
      exact: z.boolean().default(true),
    })
    .strict(),
  z
    .object({
      kind: z.literal("label"),
      value: z.string().min(1).max(200),
      exact: z.boolean().default(true),
    })
    .strict(),
  z
    .object({
      kind: z.literal("role"),
      role: z.string().min(1).max(60),
      name: z.string().max(200).optional(),
      exact: z.boolean().default(true),
    })
    .strict(),
  z
    .object({ kind: z.literal("css"), value: z.string().min(1).max(500) })
    .strict(),
]);
export const targetSchema = z
  .object({
    windowId: z.string().min(1).max(200).optional(),
    frameUrl: z.string().max(2000).optional(),
    locator: locatorSchema,
    index: z.number().int().nonnegative().max(1000).optional(),
  })
  .strict();
export const pointSchema = z
  .object({
    windowId: z.string().min(1).max(200).optional(),
    x: z.number().finite().nonnegative(),
    y: z.number().finite().nonnegative(),
  })
  .strict();
export const pageActionSchema = z.discriminatedUnion("kind", [
  z.object({ kind: z.literal("click") }).strict(),
  z.object({ kind: z.literal("fill"), text: z.string().max(100000) }).strict(),
  z.object({ kind: z.literal("select"), value: z.string().max(1000) }).strict(),
  z
    .object({ kind: z.literal("pressKey"), key: z.string().min(1).max(100) })
    .strict(),
  z
    .object({
      kind: z.literal("scroll"),
      deltaX: z.number().finite().min(-10000).max(10000),
      deltaY: z.number().finite().min(-10000).max(10000),
    })
    .strict(),
  z.object({ kind: z.literal("scrollIntoView") }).strict(),
]);
/**
 * Desktop windows drop background OS wheel events, so Computer Use cannot
 * scroll them; CDP scrolling reaches the page without activating the app.
 */
const desktopActions = new Set(["scroll", "scrollIntoView"]);
type Target = z.infer<typeof targetSchema>;
type Session = { browser: Browser; identity: string };
const mainSnapshotSchema = z
  .object({
    payload: z
      .object({
        computerUseAppId: z.string().nullable(),
        windows: z.array(
          z
            .object({
              title: z.string(),
              url: z.string(),
              computerUseWindowId: z.string().nullable(),
            })
            .passthrough(),
        ),
      })
      .passthrough(),
  })
  .passthrough();

export class Pages {
  private readonly sessions = new Map<string, Session>();
  private readonly actions = new Map<string, Promise<unknown>>();
  constructor(private readonly environments: Environments) {}

  private async session(environmentId: string): Promise<Session> {
    const environment = await this.environments.get(environmentId);
    const descriptor = await this.environments.descriptor(environmentId),
      identity = `${descriptor.ownerId}:${descriptor.pid}`;
    const existing = this.sessions.get(environmentId);
    if (existing?.identity === identity && existing.browser.isConnected())
      return existing;
    if (existing) {
      await existing.browser.close().catch(() => undefined);
      this.sessions.delete(environmentId);
    }
    if (!descriptor.debugPort)
      throw new VerificationError(
        problem(
          "unsupportedCapability",
          "The dev application did not publish its page debugging port",
        ),
      );
    if (environment.dataMode !== "liveReadOnly")
      await this.environments.desktopRequest(environmentId, "/capabilities");
    const browser = await chromium.connectOverCDP(
      `http://127.0.0.1:${descriptor.debugPort}`,
      { timeout: 10000 },
    );
    const current = await this.environments.descriptor(environmentId);
    if (
      current.ownerId !== descriptor.ownerId ||
      current.pid !== descriptor.pid
    ) {
      await browser.close();
      throw new VerificationError(
        problem(
          "identityMismatch",
          "The application restarted while connecting to its pages",
        ),
      );
    }
    const session = { browser, identity };
    this.sessions.set(environmentId, session);
    return session;
  }

  private async pages(environmentId: string) {
    const { browser } = await this.session(environmentId);
    const pages = browser
      .contexts()
      .flatMap((context) => context.pages())
      .filter(
        (page) => !page.isClosed() && !page.url().startsWith("devtools://"),
      );
    return Promise.all(
      pages.map(async (page) => {
        const cdp = await page.context().newCDPSession(page);
        try {
          const { targetInfo } = (await cdp.send("Target.getTargetInfo")) as {
            targetInfo: { targetId: string };
          };
          return { windowId: targetInfo.targetId, page };
        } finally {
          await cdp.detach().catch(() => undefined);
        }
      }),
    );
  }

  private async page(environmentId: string, windowId?: string): Promise<Page> {
    const pages = await this.pages(environmentId);
    const match = windowId
      ? pages.find((item) => item.windowId === windowId)
      : pages[0];
    if (!match)
      throw new VerificationError(
        problem(
          "notFound",
          windowId
            ? "The window does not exist"
            : "The application has no open window",
        ),
      );
    return match.page;
  }

  private frame(page: Page, frameUrl?: string): Frame {
    if (frameUrl === undefined) return page.mainFrame();
    const frame = page.frames().find((item) => item.url() === frameUrl);
    if (!frame)
      throw new VerificationError(
        problem("notFound", "The frame does not exist"),
      );
    return frame;
  }

  private locate(frame: Frame, target: Target): Locator {
    const value = target.locator;
    const locator =
      value.kind === "testId"
        ? frame.getByTestId(value.value)
        : value.kind === "text"
          ? frame.getByText(value.value, { exact: value.exact })
          : value.kind === "label"
            ? frame.getByLabel(value.value, { exact: value.exact })
            : value.kind === "role"
              ? frame.getByRole(
                  value.role as Parameters<Frame["getByRole"]>[0],
                  {
                    ...(value.name === undefined ? {} : { name: value.name }),
                    exact: value.exact,
                  },
                )
              : frame.locator(value.value);
    return locator;
  }

  /**
   * Desktop windows also carry their Multica Computer Use target, matched by
   * page URL (and title when several windows share one), because input to the
   * dev application goes through Computer Use in the background.
   */
  async windows(environmentId: string) {
    const environment = await this.environments.get(environmentId);
    const windows = await Promise.all(
      (await this.pages(environmentId)).map(async ({ windowId, page }) => ({
        windowId,
        title: await page.title(),
        url: page.url(),
      })),
    );
    if (environment.mode !== "desktop") return windows;
    const { payload: main } = mainSnapshotSchema.parse(
      await this.environments.desktopRequest(
        environmentId,
        "/snapshot?source=main",
      ),
    );
    return windows.map((window) => {
      const byUrl = main.windows.filter((item) => item.url === window.url),
        matches =
          byUrl.length > 1
            ? byUrl.filter((item) => item.title === window.title)
            : byUrl,
        windowId = matches.length === 1 ? matches[0]!.computerUseWindowId : null;
      return {
        ...window,
        computerUse:
          main.computerUseAppId && windowId
            ? { appId: main.computerUseAppId, windowId }
            : null,
      };
    });
  }

  async observe(
    environmentId: string,
    windowId?: string,
    frameUrl?: string,
    offset = 0,
    limit = 50,
  ) {
    const page = await this.page(environmentId, windowId),
      frame = this.frame(page, frameUrl);
    const candidates = frame.locator(
        "button,a,input:not([type=hidden]),textarea,select,[role],[contenteditable=true],[data-testid]",
      ),
      total = await candidates.count(),
      elements = [];
    for (let index = offset; index < Math.min(total, offset + limit); index++) {
      const element = candidates.nth(index);
      if (!(await element.isVisible())) continue;
      elements.push({
        ...(await element.evaluate((node) => {
          const element = node as HTMLElement,
            input = element as HTMLInputElement;
          return {
            tag: element.tagName.toLowerCase(),
            role: element.getAttribute("role"),
            name: (
              element.getAttribute("aria-label") ||
              input.labels?.[0]?.textContent ||
              element.textContent ||
              element.getAttribute("placeholder") ||
              ""
            )
              .trim()
              .slice(0, 160),
            testId: element.getAttribute("data-testid"),
            value:
              input.type === "password"
                ? "[REDACTED]"
                : typeof input.value === "string"
                  ? input.value.slice(0, 256)
                  : undefined,
          };
        })),
        enabled: await element.isEnabled(),
        bounds: await element.boundingBox(),
      });
    }
    return {
      url: frame.url(),
      frames: page
        .frames()
        .map((item) => ({ name: item.name(), url: item.url() })),
      elements,
      nextOffset: Math.min(total, offset + limit),
      hasMore: offset + limit < total,
      candidateCount: total,
    };
  }

  async read(environmentId: string, input: Target, limit = 20) {
    const target = targetSchema.parse(input);
    const page = await this.page(environmentId, target.windowId),
      locator = this.locate(this.frame(page, target.frameUrl), target),
      count = await locator.count(),
      indexes =
        target.index === undefined
          ? Array.from({ length: Math.min(count, limit) }, (_, index) => index)
          : target.index < count
            ? [target.index]
            : [];
    const items = [];
    for (const index of indexes) {
      const element = locator.nth(index);
      items.push({
        index,
        visible: await element.isVisible(),
        enabled: await element.isEnabled(),
        text: (await element.innerText({ timeout: 1000 })).slice(0, 20000),
        value: await element.evaluate((node) =>
          node instanceof HTMLInputElement && node.type === "password"
            ? "[REDACTED]"
            : node instanceof HTMLInputElement ||
                node instanceof HTMLTextAreaElement ||
                node instanceof HTMLSelectElement
              ? node.value
              : null,
        ),
        bounds: await element.boundingBox(),
      });
    }
    return { count, items };
  }

  perform(
    environmentId: string,
    target: Target | z.infer<typeof pointSchema>,
    action: z.infer<typeof pageActionSchema>,
  ): Promise<{ performed: true }> {
    const next = (this.actions.get(environmentId) ?? Promise.resolve())
      .catch(() => undefined)
      .then(async () => {
        const parsed = pageActionSchema.parse(action);
        // Clicks and typing go through Multica Computer Use, matching what a
        // person does; only scrolling, which it cannot deliver, stays on CDP.
        if (
          (await this.environments.get(environmentId)).mode === "desktop" &&
          !desktopActions.has(parsed.kind)
        )
          throw new VerificationError(
            problem(
              "unsupportedCapability",
              "Desktop environments accept scroll and scrollIntoView here; click and type through Multica Computer Use with the computerUse target from page_windows",
            ),
          );
        if ("x" in target) {
          const point = pointSchema.parse(target),
            page = await this.page(environmentId, point.windowId);
          if (parsed.kind === "click") await page.mouse.click(point.x, point.y);
          else if (parsed.kind === "scroll") {
            await page.mouse.move(point.x, point.y);
            await page.mouse.wheel(parsed.deltaX, parsed.deltaY);
          } else
            throw new VerificationError(
              problem(
                "invalidInput",
                "Coordinate targets support click and scroll; use a locator for text input",
              ),
            );
          return { performed: true as const };
        }
        const selector = targetSchema.parse(target),
          page = await this.page(environmentId, selector.windowId),
          locator = this.locate(this.frame(page, selector.frameUrl), selector),
          count = await locator.count();
        if (selector.index === undefined && count !== 1)
          throw new VerificationError(
            problem(
              "invalidInput",
              `The locator matched ${count} elements; pass an index to choose one`,
            ),
          );
        const element = locator.nth(selector.index ?? 0);
        if (parsed.kind === "click") await element.click({ timeout: 5000 });
        else if (parsed.kind === "fill")
          await element.fill(parsed.text, { timeout: 5000 });
        else if (parsed.kind === "select")
          await element.selectOption(parsed.value, { timeout: 5000 });
        else if (parsed.kind === "pressKey")
          await element.press(parsed.key, { timeout: 5000 });
        else if (parsed.kind === "scrollIntoView")
          await element.scrollIntoViewIfNeeded({ timeout: 5000 });
        else {
          await element.scrollIntoViewIfNeeded({ timeout: 5000 });
          const bounds = await element.boundingBox();
          if (!bounds)
            throw new VerificationError(
              problem("invalidInput", "The scroll target has no visible box"),
            );
          await page.mouse.move(
            bounds.x + bounds.width / 2,
            bounds.y + bounds.height / 2,
          );
          await page.mouse.wheel(parsed.deltaX, parsed.deltaY);
        }
        return { performed: true as const };
      });
    this.actions.set(environmentId, next);
    return next;
  }

  async screenshot(environmentId: string, windowId?: string, fullPage = false) {
    const page = await this.page(environmentId, windowId);
    const colorScheme = await page.evaluate(() => {
      const root = document.documentElement,
        scheme = getComputedStyle(root).colorScheme;
      if (root.classList.contains("dark") || scheme === "dark") return "dark";
      if (root.classList.contains("light") || scheme === "light")
        return "light";
      return matchMedia("(prefers-color-scheme: dark)").matches
        ? "dark"
        : "light";
    });
    return {
      colorScheme,
      capturedAt: new Date().toISOString(),
      png: await page.screenshot({ type: "png", fullPage, timeout: 10000 }),
    };
  }

  async close() {
    await Promise.allSettled(
      [...this.sessions.values()].map((session) => session.browser.close()),
    );
    this.sessions.clear();
  }
}
