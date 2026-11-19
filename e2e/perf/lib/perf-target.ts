/**
 * Where a core-path sample runs: a fresh Chromium context against the web
 * build (`web`), or the real Electron renderer over CDP (`desktop`, MUL-9612).
 *
 * Both give the spec the same thing: a page with the frame metrics installed,
 * a CDP session, collected page errors, and an `open(path)` that boots the app
 * straight onto a route. On the web that is a navigation. Desktop routes live
 * in a memory router, so the sample writes a tab layout holding only that
 * route and reloads the renderer: the app restores the tab exactly as it does
 * on launch. Each desktop sample also clears storage and the HTTP cache, so it
 * starts as cold as a fresh browser context does.
 */
import { chromium, type Browser, type CDPSession, type Page } from "@playwright/test";
import { installFrameMetrics } from "./frame-metrics";

export type Platform = "web" | "desktop";
export const PLATFORM: Platform = process.env.PERF_PLATFORM === "desktop" ? "desktop" : "web";

export interface SampleOptions {
  token: string;
  viewport: { width: number; height: number };
  cpuThrottle: number;
  /** localStorage entries in place before the app boots. */
  storage?: Record<string, string>;
}

export interface Sample {
  page: Page;
  cdp: CDPSession;
  errors: string[];
  /** Boots the app onto `path` (`/<slug>/...`). Resolves once the navigation has committed. */
  open(path: string): Promise<void>;
  close(): Promise<void>;
}

export interface Target {
  platform: Platform;
  /** Chromium version, plus Electron's on desktop. */
  version(): Promise<string>;
  newSample(options: SampleOptions): Promise<Sample>;
}

async function prepareSession(page: Page, cdp: CDPSession, options: SampleOptions) {
  await cdp.send("Performance.enable");
  if (options.cpuThrottle > 1) await cdp.send("Emulation.setCPUThrottlingRate", { rate: options.cpuThrottle });
  const errors: string[] = [];
  const onError = (error: Error) => errors.push(error.message);
  page.on("pageerror", onError);
  return { errors, onError };
}

export function webTarget(browser: Browser): Target {
  return {
    platform: "web",
    version: async () => browser.version(),
    async newSample(options) {
      const context = await browser.newContext({ viewport: options.viewport, locale: "en-US", colorScheme: "light" });
      const page = await context.newPage();
      await installFrameMetrics(page, options.token);
      if (options.storage) {
        await page.addInitScript((entries) => {
          for (const [key, value] of Object.entries(entries)) localStorage.setItem(key, value);
        }, options.storage);
      }
      const cdp = await context.newCDPSession(page);
      const { errors } = await prepareSession(page, cdp, options);
      return {
        page, cdp, errors,
        open: async (path) => { await page.goto(path, { waitUntil: "commit" }); },
        close: () => context.close(),
      };
    },
  };
}

/** The persisted tab layout (tab store v5) holding one tab on `path`. */
function tabLayout(path: string): string {
  const slug = path.split("/")[1]!;
  const tab = { id: "perf-tab", url: path, title: "Benchmark", pinned: false, history: { stack: [path], index: 0 }, memento: { scroll: {}, view: {} } };
  return JSON.stringify({
    state: {
      activeWorkspaceSlug: slug,
      byWorkspace: {
        [slug]: { activeTabId: tab.id, recentTabIds: [], browsingHistory: [], browsingHistoryTitles: {}, issueAliases: {}, tabs: [tab] },
      },
    },
    version: 5,
  });
}

/** The main window's renderer: issue windows load the same page, only the preload tells them apart. */
async function mainWindow(browser: Browser, deadline: number): Promise<Page> {
  for (;;) {
    for (const page of browser.contexts().flatMap((context) => context.pages())) {
      const kind = await page.evaluate(() => (window as unknown as { desktopAPI?: { windowContext?: { kind?: string } } })
        .desktopAPI?.windowContext?.kind ?? (("desktopAPI" in window) ? "main" : "")).catch(() => "");
      if (kind === "main") return page;
    }
    if (Date.now() > deadline) throw new Error("no main Desktop window appeared over CDP");
    await new Promise((done) => setTimeout(done, 500));
  }
}

/** sessionStorage key carrying the next boot's localStorage across the reload. */
const BOOT_KEY = "__perf_boot_storage";

/**
 * Desktop picks its language from localStorage, then from the OS (the web
 * target pins it through the context's `locale`). The specs find controls by
 * their English names, so every desktop boot writes the Settings choice for
 * English: on a Chinese or Japanese system the app would otherwise come up in
 * that language and the scenarios would time out looking for
 * "Toggle right sidebar".
 */
const DESKTOP_LOCALE = { "multica-locale": "en" };

/** Writes `storage` (plus the English locale) for the next boot and reloads into it. */
async function bootWith(page: Page, storage: Record<string, string>) {
  const entries = { ...DESKTOP_LOCALE, ...storage };
  await page.evaluate(({ key, value }) => sessionStorage.setItem(key, value), { key: BOOT_KEY, value: JSON.stringify(entries) });
  await page.reload({ waitUntil: "commit" });
}

/**
 * Boots once, outside any measurement, and fails with a clear message when
 * the app still comes up in another language, instead of letting a scenario
 * time out on a label it cannot find. index.html ships `lang="en"`; App
 * replaces it with the resolved locale in a layout effect of its first
 * render, so the value is final once #root has content.
 */
async function assertEnglish(page: Page) {
  await bootWith(page, {});
  await page.waitForFunction(() => !!document.getElementById("root")?.hasChildNodes(), undefined, { timeout: 60_000 });
  const lang = await page.evaluate(() => document.documentElement.lang);
  if (lang !== "en") throw new Error(`Desktop booted in "${lang}" despite multica-locale=en; the specs match English labels`);
}

export async function desktopTarget(cdpUrl: string, token: string): Promise<Target> {
  const deadline = Date.now() + 60_000;
  let browser: Browser | undefined;
  while (!browser) {
    try {
      browser = await chromium.connectOverCDP(cdpUrl);
    } catch (error) {
      if (Date.now() > deadline) throw new Error(`could not attach to Desktop at ${cdpUrl}: ${(error as Error).message}`);
      await new Promise((done) => setTimeout(done, 1000));
    }
  }
  // The connection is never closed: it drops when the worker exits, leaving the
  // app running for whoever started it. `browser.close()` could quit the app.
  const page = await mainWindow(browser, deadline);
  // Installed once: init scripts persist across reloads and would stack per
  // sample. The storage reset runs first, in the new document before any app
  // script, so whatever the outgoing page persists while unloading is gone;
  // the frame metrics then sign the session in.
  await page.addInitScript((key) => {
    const raw = sessionStorage.getItem(key);
    if (!raw) return;
    sessionStorage.clear();
    localStorage.clear();
    for (const [name, value] of Object.entries(JSON.parse(raw) as Record<string, string>)) localStorage.setItem(name, value);
  }, BOOT_KEY);
  await installFrameMetrics(page, token);
  await assertEnglish(page);
  const connected = browser;
  return {
    platform: "desktop",
    async version() {
      const ua = await page.evaluate(() => navigator.userAgent);
      return `${connected.version()} (Electron ${ua.match(/Electron\/([\d.]+)/)?.[1] ?? "?"})`;
    },
    async newSample(options) {
      const cdp = await page.context().newCDPSession(page);
      await cdp.send("Network.clearBrowserCache");
      await page.setViewportSize(options.viewport);
      const { errors, onError } = await prepareSession(page, cdp, options);
      return {
        page, cdp, errors,
        async open(path) {
          await bootWith(page, { ...options.storage, multica_tabs: tabLayout(path) });
        },
        async close() {
          page.off("pageerror", onError);
          if (options.cpuThrottle > 1) await cdp.send("Emulation.setCPUThrottlingRate", { rate: 1 }).catch(() => {});
          await cdp.detach().catch(() => {});
        },
      };
    },
  };
}

export async function perfTarget(browser: Browser, token: string): Promise<Target> {
  if (PLATFORM === "web") return webTarget(browser);
  const cdpUrl = process.env.PERF_DESKTOP_CDP_URL || process.env.MULTICA_DEV_DESKTOP_CDP_URL;
  if (!cdpUrl) throw new Error("PERF_PLATFORM=desktop needs PERF_DESKTOP_CDP_URL (scripts/perf-desktop.mjs sets it)");
  return desktopTarget(cdpUrl, token);
}
