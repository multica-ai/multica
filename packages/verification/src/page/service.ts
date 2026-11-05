import { randomUUID } from "node:crypto";
import {
  chromium,
  type Browser,
  type Page,
  type ElementHandle,
  type Frame,
} from "playwright";
import { z } from "zod";
import { pageTargetSchema } from "../contracts/resources.js";
import type { Environments } from "../setup/environments.js";
import type { EvidenceStore } from "../evidence/store.js";
import type { Sources } from "../sources/service.js";
import { problem, VerificationError } from "../host/problem.js";

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
]);
type Session = {
  browser: Browser;
  identity: string;
  windows: Map<string, Page>;
  frames: Map<string, Frame>;
};
type Observation = {
  environmentId: string;
  windowRef: string;
  page: Page;
  identity: string;
  elements: Map<string, ElementHandle>;
};
export class Pages {
  private readonly sessions = new Map<string, Session>();
  private readonly observations = new Map<string, Observation>();
  private readonly latest = new Map<string, string>();
  private readonly actions = new Map<string, Promise<unknown>>();
  constructor(
    private readonly environments: Environments,
    private readonly evidence: EvidenceStore,
    private readonly sources: Sources,
  ) {}
  private async session(environmentId: string): Promise<Session> {
    if (
      (await this.environments.get(environmentId)).components.referenceOnly ===
      true
    )
      throw new VerificationError(
        problem(
          "unsupportedCapability",
          "Read-only reference environments have no application pages; restore a test copy for page verification",
        ),
      );
    const descriptor = await this.environments.descriptor(environmentId),
      identity = `${descriptor.ownerId}:${descriptor.pid}`;
    const existing = this.sessions.get(environmentId);
    if (
      existing &&
      existing.identity === identity &&
      existing.browser.isConnected()
    )
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
    const session = {
      browser,
      identity,
      windows: new Map<string, Page>(),
      frames: new Map<string, Frame>(),
    };
    this.sessions.set(environmentId, session);
    return session;
  }
  async windows(environmentId: string) {
    const session = await this.session(environmentId),
      pages = session.browser
        .contexts()
        .flatMap((context) => context.pages())
        .filter((page) => !page.isClosed());
    for (const page of pages)
      if (![...session.windows.values()].includes(page))
        session.windows.set(randomUUID(), page);
    for (const [ref, page] of session.windows)
      if (!pages.includes(page)) {
        session.windows.delete(ref);
        this.latest.delete(ref);
      }
    return Promise.all(
      [...session.windows].map(async ([windowRef, page]) => ({
        windowRef,
        title: await page.title(),
        url: page.url(),
        instance: session.identity,
      })),
    );
  }
  private async window(
    environmentId: string,
    windowRef: string,
  ): Promise<{ session: Session; page: Page }> {
    await this.windows(environmentId);
    const session = await this.session(environmentId),
      page = session.windows.get(windowRef);
    if (!page || page.isClosed())
      throw new VerificationError(
        problem(
          "staleReference",
          "The window reference is no longer valid",
          "reobserve",
        ),
      );
    return { session, page };
  }
  async observe(
    environmentId: string,
    windowRef: string,
    frameRef?: string,
    offset = 0,
    limit = 50,
  ) {
    const { session, page } = await this.window(environmentId, windowRef),
      frames = page.frames();
    for (const frame of frames)
      if (![...session.frames.values()].includes(frame))
        session.frames.set(randomUUID(), frame);
    for (const [id, frame] of session.frames)
      if (!frames.includes(frame)) session.frames.delete(id);
    const frame = frameRef ? session.frames.get(frameRef) : page.mainFrame();
    if (!frame || frame.isDetached())
      throw new VerificationError(
        problem(
          "staleReference",
          "The frame reference is no longer valid",
          "reobserve",
        ),
      );
    const prior = this.latest.get(windowRef);
    if (prior) await this.dispose(prior);
    const observationId = randomUUID(),
      handles = new Map<string, ElementHandle>(),
      elements = [];
    const candidates = frame.locator(
        "button,a,input:not([type=hidden]),textarea,select,[role],[contenteditable=true],[data-testid]",
      ),
      total = await candidates.count();
    for (let index = offset; index < Math.min(total, offset + limit); index++) {
      const element = await candidates.nth(index).elementHandle();
      if (!element) continue;
      if (!(await element.isVisible())) {
        await element.dispose();
        continue;
      }
      const ref = randomUUID(),
        value = await element.evaluate((node) => {
          const element = node as HTMLElement,
            input = element as HTMLInputElement;
          const password = input.type === "password";
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
            value: password
              ? "[REDACTED]"
              : typeof input.value === "string"
                ? input.value.slice(0, 256)
                : undefined,
          };
        });
      handles.set(ref, element);
      elements.push({
        ref,
        ...value,
        enabled: await element.isEnabled(),
        bounds: await element.boundingBox(),
      });
    }
    this.observations.set(observationId, {
      environmentId,
      windowRef,
      page,
      identity: session.identity,
      elements: handles,
    });
    this.latest.set(windowRef, observationId);
    return {
      observationId,
      windowRef,
      frameRef: [...session.frames].find(([, value]) => value === frame)?.[0],
      frames: [...session.frames].map(([ref, item]) => ({
        ref,
        name: item.name(),
        url: item.url(),
      })),
      url: frame.url(),
      elements,
      nextOffset: Math.min(total, offset + limit),
      hasMore: offset + limit < total,
      candidateCount: total,
    };
  }
  perform(input: {
    verificationId: string;
    environmentId: string;
    stepId: string;
    windowRef: string;
    observationId: string;
    targetRef: string;
    action: z.infer<typeof pageActionSchema>;
  }): Promise<unknown> {
    const next = (this.actions.get(input.environmentId) ?? Promise.resolve())
      .catch(() => undefined)
      .then(async () => {
        const environment = await this.environments.get(input.environmentId);
        if (environment.dataMode === "liveReadOnly")
          throw new VerificationError(
            problem(
              "forbidden",
              "Interact with a test copy; live-source page tools only observe and capture",
            ),
          );
        const step = await this.sources.get(input.stepId);
        if (
          step.verificationId !== input.verificationId ||
          step.environmentId !== input.environmentId ||
          step.state !== "observing" ||
          step.coverage !== "complete"
        )
          throw new VerificationError(
            problem(
              "coverageIncomplete",
              "A matching, ready observation step is required before page interaction",
            ),
          );
        const observation = this.observations.get(input.observationId),
          session = await this.session(input.environmentId);
        if (
          !observation ||
          observation.environmentId !== input.environmentId ||
          observation.windowRef !== input.windowRef ||
          this.latest.get(input.windowRef) !== input.observationId ||
          session.identity !== observation.identity
        )
          throw new VerificationError(
            problem(
              "staleReference",
              "Observe the current page before acting",
              "reobserve",
            ),
          );
        const element = observation.elements.get(input.targetRef);
        if (!element || !(await element.evaluate((node) => node.isConnected)))
          throw new VerificationError(
            problem(
              "staleReference",
              "The observed element was removed",
              "reobserve",
            ),
          );
        try {
          const action = pageActionSchema.parse(input.action);
          if (action.kind === "click") await element.click({ timeout: 5000 });
          else if (action.kind === "fill")
            await element.fill(action.text, { timeout: 5000 });
          else if (action.kind === "select")
            await element.selectOption(action.value, { timeout: 5000 });
          else if (action.kind === "pressKey")
            await element.press(action.key, { timeout: 5000 });
          else {
            await element.scrollIntoViewIfNeeded({ timeout: 5000 });
            const bounds = await element.boundingBox();
            if (!bounds) throw new Error("Invisible scroll target");
            await observation.page.mouse.move(
              bounds.x + bounds.width / 2,
              bounds.y + bounds.height / 2,
            );
            await observation.page.mouse.wheel(action.deltaX, action.deltaY);
          }
          const screenshot = await this.capture(
            input.verificationId,
            input.environmentId,
            input.stepId,
            input.windowRef,
          ).catch(() => null);
          return {
            actionCompleted: true,
            businessOutcome: "notChecked",
            evidenceId: screenshot?.evidenceId ?? null,
          };
        } catch {
          throw new VerificationError(
            problem(
              "outcomeUnknown",
              "The page action may have occurred; inspect the page and original receipt before another action",
              "inspect",
            ),
          );
        } finally {
          this.latest.delete(input.windowRef);
          await this.dispose(input.observationId);
        }
      });
    this.actions.set(input.environmentId, next);
    return next;
  }
  async capture(
    verificationId: string,
    environmentId: string,
    stepId: string,
    windowRef: string,
    fullPage = false,
  ) {
    const { page } = await this.window(environmentId, windowRef);
    return this.evidence.attach({
      verificationId,
      environmentId,
      stepId,
      kind: "screenshot",
      source: "playwright",
      mediaType: "image/png",
      coverage: "complete",
      bytes: await page.screenshot({ type: "png", fullPage, timeout: 10000 }),
    });
  }
  async readCondition(
    environmentId: string,
    target: string | z.infer<typeof pageTargetSchema>,
    kind: string,
  ): Promise<unknown> {
    if (typeof target !== "string") {
      const selector = pageTargetSchema.parse(target),
        { session, page } = await this.window(
          environmentId,
          selector.windowRef,
        );
      const frame = selector.frameRef
        ? session.frames.get(selector.frameRef)
        : page.mainFrame();
      if (!frame || frame.isDetached())
        throw new VerificationError(
          problem(
            "staleReference",
            "The selected frame no longer exists",
            "reobserve",
          ),
        );
      const value = selector.locator;
      const locator =
        value.kind === "testId"
          ? frame.getByTestId(value.value)
          : value.kind === "label"
            ? frame.getByLabel(value.value, { exact: true })
            : frame.getByText(value.value, { exact: true });
      const count = await locator.count();
      if (count > 1)
        throw new VerificationError(
          problem("invalidInput", "The page condition locator is ambiguous"),
        );
      if (kind === "page.visible")
        return count === 1 && (await locator.isVisible());
      if (!count)
        throw new VerificationError(
          problem("sourceUnavailable", "The page condition target is absent"),
        );
      return kind === "page.text"
        ? (await locator.innerText({ timeout: 1000 })).trim()
        : locator.isEnabled();
    }
    const session = await this.session(environmentId),
      observation = [...this.observations.values()].find(
        (item) =>
          item.environmentId === environmentId &&
          item.identity === session.identity &&
          item.elements.has(target),
      );
    const element = observation?.elements.get(target);
    if (!element || !(await element.evaluate((node) => node.isConnected)))
      throw new VerificationError(
        problem(
          "staleReference",
          "The page condition target needs a fresh observation",
          "reobserve",
        ),
      );
    if (kind === "page.visible") return element.isVisible();
    if (kind === "page.text") return (await element.innerText()).trim();
    return element.isEnabled();
  }
  private async dispose(id: string) {
    const observation = this.observations.get(id);
    this.observations.delete(id);
    if (observation)
      await Promise.allSettled(
        [...observation.elements.values()].map((element) => element.dispose()),
      );
  }
  async close() {
    for (const id of this.observations.keys()) await this.dispose(id);
    await Promise.allSettled(
      [...this.sessions.values()].map((session) => session.browser.close()),
    );
    this.sessions.clear();
  }
}
