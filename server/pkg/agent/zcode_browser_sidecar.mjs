#!/usr/bin/env node
// Multica ZCode browser sidecar.
//
// Runs as PID 1 of a dedicated container and speaks NDJSON over stdio with the
// Go host (zcode_browser.go). It mirrors the semantics of the managed headless
// CDP runtime of the official ZCode CLI: one Chromium per sidecar process, one
// browser context per ZCode session, and one generation counter that only
// advances when the browser is lost unexpectedly.
//
// Security-relevant choices, keep them intact:
// - The Playwright runtime is loaded only from the read-only /node_modules
//   mount that the host provides; nothing is downloaded or installed.
// - Only http, https and about:blank URLs are navigated.
// - stdout carries protocol frames only. Diagnostics go to stderr and never
//   include page content, URL query strings, base64 payloads or full ids.

import { createRequire } from "node:module";
import { randomUUID } from "node:crypto";
import { createInterface } from "node:readline";

const require = createRequire(import.meta.url);

const NAVIGATION_TIMEOUT_MS = 30_000;
const CLOSE_TIMEOUT_MS = 1500;
const HARD_EXIT_MS = 3000;
const DEFAULT_VIEWPORT = { width: 1280, height: 720 };
const PLAYWRIGHT_WAIT_FOR_TIMEOUT_MAX_MS = 30_000;

const SIDE_EFFECT_PAGE_METHODS = new Set([
  "navigate",
  "back",
  "forward",
  "reload",
  "click",
  "fill",
  "type",
  "press",
  "cuaKeypress",
  "scroll",
  "cuaScroll",
  "domCuaScroll",
  "hover",
  "select",
  "check",
  "drag",
  "cuaDrag",
  "handleDialog",
  "close",
  "newTab",
  "evaluate",
]);
const SIDE_EFFECT_LOCATOR_OPERATIONS = new Set([
  "click",
  "dblclick",
  "fill",
  "press",
  "selectOption",
  "setChecked",
]);

function parseArgs(argv) {
  const out = { browserId: "", generation: 1 };
  for (let index = 0; index < argv.length; index += 1) {
    if (argv[index] === "--browser-id") out.browserId = String(argv[index + 1] ?? "");
    else if (argv[index] === "--generation") {
      const value = Number(argv[index + 1]);
      if (Number.isFinite(value)) out.generation = Math.max(1, Math.trunc(value));
    }
  }
  return out;
}

const args = parseArgs(process.argv.slice(2));

function abortError() {
  return new DOMException("Browser command cancelled", "AbortError");
}

function raceWithAbort(promise, signal) {
  if (signal.aborted) {
    promise.catch(() => {});
    return Promise.reject(abortError());
  }
  return new Promise((resolve, reject) => {
    const onAbort = () => reject(abortError());
    signal.addEventListener("abort", onAbort, { once: true });
    promise.then(resolve, reject).finally(() => signal.removeEventListener("abort", onAbort));
  });
}

function waitForPromise(promise, timeoutMs) {
  return new Promise((resolve) => {
    let settled = false;
    let timer;
    const finish = (value) => {
      if (settled) return;
      settled = true;
      if (timer) clearTimeout(timer);
      resolve(value);
    };
    timer = setTimeout(() => finish(false), timeoutMs);
    promise.then(
      () => finish(true),
      () => finish(true),
    );
  });
}

function timeoutMs(value) {
  return value === undefined ? 3000 : Math.min(3000, Math.max(1, Math.floor(value)));
}

function classifyError(error) {
  if (error instanceof Error && error.name === "AbortError") return "cancelled";
  if (error instanceof Error && error.name === "TimeoutError") return "timeout";
  if (error instanceof Error && /unavailable|closed|disconnected/iu.test(error.message)) return "backend_unavailable";
  return "execution_error";
}

function hasSideEffects(command) {
  if (command.method === "playwright" && command.action?.name === "locator") {
    return SIDE_EFFECT_LOCATOR_OPERATIONS.has(command.action.operation);
  }
  if (command.method === "playwright" && command.action?.name === "evaluate") return true;
  return SIDE_EFFECT_PAGE_METHODS.has(command.method);
}

function isAllowedUrl(url) {
  try {
    const parsed = new URL(url);
    return parsed.protocol === "http:" || parsed.protocol === "https:" || parsed.href === "about:blank";
  } catch {
    return false;
  }
}

function errorResult(code, message, startedAt, sideEffect) {
  return {
    ok: false,
    error: { code, message, ...(sideEffect ? { sideEffect } : {}) },
    elapsedMs: Date.now() - startedAt,
  };
}

function logError(scope, error) {
  const firstLine = String(error?.message ?? error ?? "unknown error").split("\n")[0].slice(0, 300);
  process.stderr.write(`${scope}: ${firstLine}\n`);
}

let exiting = false;

function send(message) {
  if (exiting) return;
  process.stdout.write(`${JSON.stringify(message)}\n`);
}

function loadPlaywright() {
  return require("/node_modules/playwright-core");
}

class ManagedCdpSession {
  #dialogs = new Map();
  #tabs = new Map();
  #activeTabId;

  constructor(context) {
    this.context = context;
    for (const page of context.pages()) this.#registerPage(page);
    context.on("page", (page) => this.#registerPage(page));
  }

  get tabIds() {
    return [...this.#tabs.keys()];
  }

  get activeTabId() {
    return this.#activeTabId;
  }

  async createTab() {
    const page = await this.context.newPage();
    const tab = this.#tabForPage(page) ?? this.#registerPage(page);
    this.#activeTabId = tab.id;
    await page.bringToFront();
    return tab;
  }

  async ensureTab(tabId) {
    if (tabId) {
      const tab = this.#tabs.get(tabId);
      if (!tab || tab.page.isClosed()) throw new Error(`Browser tab '${tabId}' is unavailable`);
      return tab;
    }
    const active = this.#activeTabId ? this.#tabs.get(this.#activeTabId) : undefined;
    if (active && !active.page.isClosed()) return active;
    const open = [...this.#tabs.values()].find((tab) => !tab.page.isClosed());
    return open ?? (await this.createTab());
  }

  async activateTab(tabId) {
    const tab = await this.ensureTab(tabId);
    this.#activeTabId = tab.id;
    await tab.page.bringToFront();
    return tab;
  }

  async closeTab(tabId) {
    const tab = await this.ensureTab(tabId);
    this.#tabs.delete(tab.id);
    this.#dialogs.delete(tab.id);
    if (!tab.page.isClosed()) await tab.page.close({ runBeforeUnload: false });
    if (this.#activeTabId === tab.id) this.#activeTabId = [...this.#tabs.keys()].at(-1);
  }

  async listTabs() {
    const tabs = [];
    for (const tab of this.#tabs.values()) {
      if (tab.page.isClosed()) continue;
      tabs.push({
        tabId: tab.id,
        url: tab.page.url(),
        title: await tab.page.title().catch(() => ""),
        viewport: tab.page.viewportSize() ?? DEFAULT_VIEWPORT,
        ...(tab.id === this.#activeTabId ? { active: true } : {}),
        lifecycle: "active",
      });
    }
    return tabs;
  }

  dialogFor(tabId) {
    return this.#dialogs.get(tabId);
  }

  clearDialog(tabId) {
    this.#dialogs.delete(tabId);
  }

  async setViewport(tabId, size) {
    const tab = await this.ensureTab(tabId);
    await tab.page.setViewportSize(size ?? DEFAULT_VIEWPORT);
  }

  async close() {
    this.#dialogs.clear();
    this.#tabs.clear();
    if (this.context.browser()?.isConnected()) await this.context.close().catch(() => {});
  }

  #registerPage(page) {
    const existing = this.#tabForPage(page);
    if (existing) return existing;
    const created = { id: `tab:${randomUUID()}`, page };
    this.#tabs.set(created.id, created);
    this.#activeTabId ??= created.id;
    page.on("dialog", (dialog) => this.#dialogs.set(created.id, dialog));
    page.on("close", () => {
      this.#tabs.delete(created.id);
      this.#dialogs.delete(created.id);
      if (this.#activeTabId === created.id) this.#activeTabId = [...this.#tabs.keys()].at(-1);
    });
    return created;
  }

  #tabForPage(page) {
    return [...this.#tabs.values()].find((tab) => tab.page === page);
  }
}

// --- Browser process and session bookkeeping ---------------------------------

const browserId = args.browserId;
let generation = args.generation;
let browser;
let launching;
let runtimeClosed = false;
let expectedDisconnect = false;
const closingSessions = new Set();
const inflight = new Map();
const pendingContexts = new Map();
const sessions = new Map();
// Snapshot ref -> element handle, per page. Needed by ref-based screenshots.
const snapshotRefs = new WeakMap();

async function closeBrowserInstance(instance) {
  if (!instance?.isConnected()) return;
  expectedDisconnect = true;
  try {
    await waitForPromise(instance.close(), CLOSE_TIMEOUT_MS);
  } finally {
    expectedDisconnect = false;
  }
}

async function ensureBrowser(signal) {
  if (runtimeClosed) throw new Error("Managed CDP browser runtime is closed");
  if (signal?.aborted) throw abortError();
  if (browser?.isConnected()) return browser;
  launching ??= (async () => {
    let playwright;
    try {
      playwright = loadPlaywright();
    } catch (cause) {
      logError("playwright load failed", cause);
      throw new Error("Managed headless Chromium is unavailable: failed to load the pinned Playwright runtime.", { cause });
    }
    let instance;
    try {
      instance = await playwright.chromium.launch({
        headless: true,
        args: ["--no-first-run", "--no-default-browser-check"],
      });
    } catch (cause) {
      logError("chromium launch failed", cause);
      throw new Error(
        "Managed headless Chromium is unavailable: launch failed. Verify the browser executable and OS sandbox/runtime dependencies.",
        { cause },
      );
    }
    if (runtimeClosed) {
      await closeBrowserInstance(instance);
      throw new Error("Managed CDP browser runtime is closed");
    }
    instance.on("disconnected", () => {
      if (browser !== instance) return;
      browser = undefined;
      sessions.clear();
      const unexpected = !expectedDisconnect;
      if (unexpected) generation += 1;
      send({ event: "browserClosed", generation, unexpected });
    });
    browser = instance;
    return instance;
  })().finally(() => {
    launching = undefined;
  });
  const instance = signal ? await raceWithAbort(launching, signal) : await launching;
  if (signal?.aborted) {
    await closeBrowserInstance(instance);
    throw abortError();
  }
  return instance;
}

async function ensureSession(sessionId) {
  if (closingSessions.has(sessionId)) throw new Error(`Browser session '${sessionId}' is closing`);
  const existing = sessions.get(sessionId);
  if (existing) return existing;
  const pendingCreation = pendingContexts.get(sessionId);
  if (pendingCreation) return await pendingCreation;
  const created = (async () => {
    const instance = await ensureBrowser();
    const startedGeneration = generation;
    const context = await instance.newContext({ acceptDownloads: false, viewport: { ...DEFAULT_VIEWPORT } });
    if (
      runtimeClosed ||
      closingSessions.has(sessionId) ||
      browser !== instance ||
      generation !== startedGeneration ||
      !instance.isConnected()
    ) {
      await waitForPromise(context.close(), CLOSE_TIMEOUT_MS);
      throw new Error("Managed CDP browser context became unavailable during creation");
    }
    const session = new ManagedCdpSession(context);
    sessions.set(sessionId, session);
    return session;
  })();
  pendingContexts.set(sessionId, created);
  try {
    return await created;
  } finally {
    if (pendingContexts.get(sessionId) === created) pendingContexts.delete(sessionId);
  }
}

async function closeBrowser() {
  const instance = browser;
  browser = undefined;
  if (instance) await closeBrowserInstance(instance);
}

async function withMeta(session, command, result, startedAt) {
  const requestedTabId = "tabId" in command ? command.tabId : session.activeTabId;
  const tab = requestedTabId ? await session.ensureTab(requestedTabId).catch(() => undefined) : undefined;
  return {
    ...result,
    elapsedMs: Date.now() - startedAt,
    meta: {
      browserUse: true,
      backendType: "cdp",
      browserId,
      browserGeneration: generation,
      openTabIds: session.tabIds,
      ...(tab ? { tabId: tab.id, currentUrl: tab.page.url(), lifecycle: "active" } : {}),
    },
  };
}

async function execute(message) {
  const startedAt = Date.now();
  const command = message.command && typeof message.command === "object" ? message.command : {};
  if (message.browserGeneration !== undefined && message.browserGeneration !== generation) {
    return errorResult(
      "backend_unavailable",
      `Browser backend '${browserId}' generation ${message.browserGeneration} is stale`,
      startedAt,
    );
  }
  const controller = new AbortController();
  const entry = { controller, sessionId: message.sessionId, turnId: message.turnId };
  inflight.set(message.requestId, entry);
  let running = false;
  try {
    if (controller.signal.aborted) throw abortError();
    await ensureBrowser(controller.signal);
    const session = await ensureSession(message.sessionId);
    if (controller.signal.aborted) throw abortError();
    running = true;
    const result = await raceWithAbort(executeSessionCommand(session, command), controller.signal);
    return await withMeta(session, command, result, startedAt);
  } catch (error) {
    const code = classifyError(error);
    return errorResult(
      code,
      error instanceof Error ? error.message : String(error),
      startedAt,
      code === "cancelled" && running && hasSideEffects(command) ? "uncertain" : undefined,
    );
  } finally {
    if (inflight.get(message.requestId) === entry) inflight.delete(message.requestId);
  }
}

async function executeSessionCommand(session, command) {
  switch (command.method) {
    case "list":
      return { ok: true, tabs: await session.listTabs() };
    case "newTab": {
      const tab = await session.createTab();
      return { ok: true, tab: (await session.listTabs()).find((item) => item.tabId === tab.id) };
    }
    case "activateTab": {
      const tab = await session.activateTab(command.tabId);
      return { ok: true, tab: (await session.listTabs()).find((item) => item.tabId === tab.id) };
    }
    case "close":
      await session.closeTab(command.tabId);
      return { ok: true };
    case "browserViewportSet":
      await session.setViewport(command.tabId, { width: command.width, height: command.height });
      return { ok: true };
    case "browserViewportReset":
      await session.setViewport(command.tabId, null);
      return { ok: true };
    case "getDialog": {
      const tab = await session.ensureTab(command.tabId);
      const dialog = session.dialogFor(tab.id);
      const type = dialog?.type();
      return {
        ok: true,
        dialog:
          dialog && type && ["alert", "confirm", "prompt", "beforeunload"].includes(type)
            ? { type, message: dialog.message(), ...(dialog.defaultValue() ? { defaultPrompt: dialog.defaultValue() } : {}) }
            : null,
      };
    }
    case "handleDialog": {
      const tab = await session.ensureTab(command.tabId);
      const dialog = session.dialogFor(tab.id);
      if (!dialog) throw new Error("No JavaScript dialog is pending for this tab");
      if (command.accept) await dialog.accept(command.promptText);
      else await dialog.dismiss();
      session.clearDialog(tab.id);
      return { ok: true };
    }
    case "nameSession":
      return { ok: true };
    case "listUserTabs":
      return { ok: true, userTabs: [] };
    case "browserVisibilityGet":
    case "browserVisibilitySet":
    case "capabilities":
    case "claimTab":
    case "finalize":
    case "finalizeTabs":
    case "markDeliverable":
    case "markHandoff":
    case "turnEnded":
    case "closeSession":
    case "cancelRequest":
      return {
        ok: false,
        error: { code: "capability_unsupported", message: `Browser command '${command.method}' is unavailable in managed headless CDP` },
      };
    default: {
      const tab = await session.ensureTab("tabId" in command ? command.tabId : undefined);
      return await executePageCommand(tab.page, command);
    }
  }
}

async function closeSession(sessionId) {
  closingSessions.add(sessionId);
  for (const entry of inflight.values()) {
    if (entry.sessionId === sessionId) entry.controller.abort(abortError());
  }
  const pendingCreation = pendingContexts.get(sessionId);
  if (pendingCreation) {
    await waitForPromise(pendingCreation, CLOSE_TIMEOUT_MS);
    pendingCreation.then(
      async () => {
        if (sessions.size === 0) await closeBrowser();
      },
      async () => {
        if (sessions.size === 0) await closeBrowser();
      },
    );
  }
  const session = sessions.get(sessionId);
  sessions.delete(sessionId);
  if (session) await waitForPromise(session.close(), CLOSE_TIMEOUT_MS);
  if (sessions.size === 0) await closeBrowser();
}

async function shutdown() {
  if (runtimeClosed) return;
  runtimeClosed = true;
  const pendingLaunch = launching;
  for (const entry of inflight.values()) entry.controller.abort(abortError());
  inflight.clear();
  closingSessions.clear();
  await Promise.all([...pendingContexts.values()].map((pending) => waitForPromise(pending, CLOSE_TIMEOUT_MS)));
  await Promise.all([...sessions.values()].map((session) => waitForPromise(session.close(), CLOSE_TIMEOUT_MS)));
  sessions.clear();
  await closeBrowser();
  if (pendingLaunch) {
    await waitForPromise(
      pendingLaunch.then((instance) => closeBrowserInstance(instance)),
      CLOSE_TIMEOUT_MS,
    );
  }
}

// --- Page commands -----------------------------------------------------------

async function readPageState(page) {
  const cdp = await page.context().newCDPSession(page);
  try {
    const history = await cdp.send("Page.getNavigationHistory");
    const index = history.currentIndex ?? 0;
    const count = history.entries?.length ?? 1;
    const viewport = page.viewportSize();
    const scroll = await page
      .evaluate(() => ({ scrollX: Math.round(window.scrollX), scrollY: Math.round(window.scrollY) }))
      .catch(() => ({ scrollX: 0, scrollY: 0 }));
    return {
      url: page.url(),
      title: await page.title().catch(() => ""),
      canGoBack: index > 0,
      canGoForward: index + 1 < count,
      ...scroll,
      ...(viewport ? { viewportWidth: viewport.width, viewportHeight: viewport.height } : {}),
    };
  } finally {
    await cdp.detach().catch(() => {});
  }
}

// Evaluated inside the page. Kept free of closures so Playwright can serialize it.
function collectSnapshot(options) {
  const interactiveSelector =
    "a[href],button,input,textarea,select,[role],[onclick],[tabindex],summary,label,[contenteditable]";
  const domSelector =
    "body,main,nav,header,footer,aside,section,article,h1,h2,h3,h4,h5,h6,p,ul,ol,li,dl,dt,dd,blockquote,pre,code,table,caption,thead,tbody,tfoot,tr,th,td,form,fieldset,legend,figure,figcaption,img,canvas,svg,a[href],button,input,textarea,select,option,summary,label,[role],[aria-label],[contenteditable]";
  const refElements = [];
  const refByElement = new WeakMap();
  const viewportWidth = window.innerWidth || document.documentElement.clientWidth || 0;
  const viewportHeight = window.innerHeight || document.documentElement.clientHeight || 0;
  const isHidden = (element) => {
    const style = window.getComputedStyle(element);
    const rect = element.getBoundingClientRect();
    return style.display === "none" || style.visibility === "hidden" || style.opacity === "0" || (rect.width <= 0 && rect.height <= 0);
  };
  const text = (value, limit) =>
    String(value ?? "")
      .trim()
      .replace(/\s+/g, " ")
      .slice(0, limit);
  const implicitRole = (element, tag) => {
    if (tag === "a" && element.hasAttribute("href")) return "link";
    if (tag === "button") return "button";
    if (tag === "select") return "combobox";
    if (tag === "textarea") return "textbox";
    if (tag !== "input") return "";
    const type = (element.getAttribute("type") || "text").toLowerCase();
    if (type === "checkbox") return "checkbox";
    if (type === "radio") return "radio";
    if (["button", "submit", "reset"].includes(type)) return "button";
    return type === "search" ? "searchbox" : "textbox";
  };
  const accessibleName = (element) =>
    text(
      element.getAttribute("aria-label") ||
        element.getAttribute("alt") ||
        element.getAttribute("title") ||
        element.getAttribute("placeholder") ||
        element.innerText ||
        element.textContent,
      120,
    );
  const attributes = (element) => {
    const out = {};
    for (const name of [
      "id",
      "href",
      "name",
      "type",
      "placeholder",
      "title",
      "alt",
      "role",
      "aria-label",
      "data-testid",
      "data-test",
      "data-qa",
    ]) {
      const value = text(element.getAttribute(name), 240);
      if (value) out[name] = value;
    }
    return out;
  };
  const selector = (element) => {
    if (element.id && /^[A-Za-z][A-Za-z0-9_-]*$/u.test(element.id)) return `#${element.id}`;
    const parts = [];
    let node = element;
    for (let depth = 0; node && depth < 6; depth += 1) {
      const tag = node.tagName.toLowerCase();
      const siblings = node.parentElement ? [...node.parentElement.children].filter((sibling) => sibling.tagName === node?.tagName) : [];
      parts.unshift(`${tag}:nth-of-type(${siblings.indexOf(node) + 1})`);
      node = node.parentElement;
    }
    return parts.join(" > ");
  };
  const xpath = (element) => {
    const parts = [];
    let node = element;
    while (node) {
      const tag = node.tagName.toLowerCase();
      const siblings = node.parentElement ? [...node.parentElement.children].filter((sibling) => sibling.tagName === node?.tagName) : [];
      parts.unshift(`${tag}[${siblings.indexOf(node) + 1}]`);
      node = node.parentElement;
    }
    return `/${parts.join("/")}`;
  };
  const inViewport = (rect) => rect.top < viewportHeight && rect.bottom > 0 && rect.left < viewportWidth && rect.right > 0;
  const interactive = [...document.querySelectorAll(interactiveSelector)].filter((element) => options.includeHidden || !isHidden(element));
  const elements = interactive.slice(0, options.maxElements).map((element, index) => {
    const ref = `e${index + 1}`;
    refElements.push(element);
    refByElement.set(element, ref);
    const rect = element.getBoundingClientRect();
    const tag = element.tagName.toLowerCase();
    const role = element.getAttribute("role") || implicitRole(element, tag);
    const name = accessibleName(element);
    const elementText = text(element.innerText, 100);
    const elementAttributes = attributes(element);
    const value = "value" in element ? text(String(element.value), 240) : "";
    let parent = element.parentElement;
    let parentRef;
    while (parent && !parentRef) {
      parentRef = refByElement.get(parent);
      parent = parent.parentElement;
    }
    return {
      ref,
      tag,
      ...(role ? { role } : {}),
      ...(name ? { name } : {}),
      ...(elementText ? { text: elementText } : {}),
      ...(value ? { value } : {}),
      ...("disabled" in element && element.disabled ? { disabled: true } : {}),
      ...("checked" in element ? { checked: !!element.checked } : {}),
      selector: selector(element),
      xpath: xpath(element),
      rect: {
        x: Math.round(rect.x),
        y: Math.round(rect.y),
        width: Math.round(rect.width),
        height: Math.round(rect.height),
      },
      inViewport: inViewport(rect),
      ...(parentRef ? { parentRef } : {}),
      ...(Object.keys(elementAttributes).length > 0 ? { attributes: elementAttributes } : {}),
    };
  });
  const domNodes = [...document.querySelectorAll(domSelector)].filter((element) => options.includeHidden || !isHidden(element));
  const dom = domNodes.slice(0, options.maxDomNodes).map((element) => {
    const rect = element.getBoundingClientRect();
    const tag = element.tagName.toLowerCase();
    const role = element.getAttribute("role") || implicitRole(element, tag);
    const name = accessibleName(element);
    const textContent = /^(h[1-6]|p|li|dt|dd|blockquote|pre|code|caption|th|td|label|summary|button|a|option|legend|figcaption)$/u.test(tag)
      ? text(element.innerText || element.textContent, 300)
      : "";
    let depth = 0;
    for (let node = element.parentElement; node && node !== document.body; node = node.parentElement) depth += 1;
    const ref = refByElement.get(element);
    const nodeAttributes = attributes(element);
    return {
      tag,
      depth,
      inViewport: inViewport(rect),
      ...(ref ? { ref } : {}),
      ...(role ? { role } : {}),
      ...(name ? { name } : {}),
      ...(textContent ? { text: textContent } : {}),
      ...(Object.keys(nodeAttributes).length > 0 ? { attributes: nodeAttributes } : {}),
    };
  });
  return {
    refElements,
    snapshot: {
      url: location.href,
      title: document.title,
      elements,
      truncated: interactive.length > options.maxElements,
      dom,
      domTruncated: domNodes.length > options.maxDomNodes,
    },
  };
}

async function captureSnapshot(page, maxElements = 200, includeHidden = false) {
  const handle = await page.evaluateHandle(collectSnapshot, {
    includeHidden,
    maxDomNodes: 300,
    maxElements: Math.max(1, Math.floor(maxElements)),
  });
  const snapshotHandle = await handle.getProperty("snapshot");
  const refElementsHandle = await handle.getProperty("refElements");
  const snapshot = await snapshotHandle.jsonValue();
  const refs = new Map();
  for (const [key, property] of await refElementsHandle.getProperties()) {
    const element = property.asElement();
    if (!element || !/^\d+$/u.test(key)) {
      await property.dispose();
      continue;
    }
    refs.set(`e${Number(key) + 1}`, element);
  }
  const previous = snapshotRefs.get(page);
  snapshotRefs.set(page, refs);
  await Promise.all([...(previous?.values() ?? [])].map((element) => element.dispose()));
  await Promise.all([snapshotHandle.dispose(), refElementsHandle.dispose(), handle.dispose()]);
  return snapshot;
}

async function elementForRef(page, ref) {
  const element = snapshotRefs.get(page)?.get(ref);
  if (!element) return undefined;
  return (await element.evaluate((node) => node.isConnected).catch(() => false)) ? element : undefined;
}

async function elementForRefOrThrow(page, ref) {
  const element = await elementForRef(page, ref);
  if (!element) throw new Error(`Browser ref '${ref}' is stale or unavailable. Take a fresh snapshot before retrying.`);
  return element;
}

async function evaluatePage(page, expression, expressionKind, arg, timeout) {
  const cdp = await page.context().newCDPSession(page);
  try {
    const source = expressionKind === "function" ? `(${expression})(${JSON.stringify(arg)})` : expression;
    const result = await cdp.send("Runtime.evaluate", {
      expression: source,
      awaitPromise: true,
      returnByValue: true,
      timeout,
    });
    if (result.exceptionDetails) {
      const description =
        result.exceptionDetails.exception?.description ?? result.exceptionDetails.text ?? "Playwright evaluate failed";
      throw new Error(`playwright.evaluate failed: ${description}`);
    }
    return result.result?.value;
  } finally {
    await cdp.detach().catch(() => {});
  }
}

async function executePlaywrightAction(page, action) {
  switch (action.name) {
    case "domSnapshot":
      return { ok: true, value: await page.locator("html").ariaSnapshot({ timeout: 3000 }) };
    case "elementInfo":
      return {
        ok: true,
        value: await page.evaluate(
          ({ x, y, includeNonInteractable }) =>
            document
              .elementsFromPoint(x, y)
              .filter((element) => includeNonInteractable || element.matches("a,button,input,select,textarea,[role],[tabindex]"))
              .map((element) => {
                const rect = element.getBoundingClientRect();
                const visibleText = (element.innerText || element.textContent || "").trim();
                return {
                  tagName: element.tagName.toLowerCase(),
                  role: element.getAttribute("role"),
                  visibleText: visibleText || null,
                  ariaName: element.getAttribute("aria-label") || visibleText || null,
                  testId: element.getAttribute("data-testid"),
                  boundingBox: { x: rect.x, y: rect.y, width: rect.width, height: rect.height },
                  preview: element.outerHTML.slice(0, 300),
                  selector: { primary: element.id ? `#${element.id}` : element.tagName.toLowerCase(), candidates: [] },
                };
              }),
          action,
        ),
      };
    case "elementScreenshot": {
      const markerAttribute = "data-multica-element-screenshot";
      const marked = await page.evaluate(
        ({ x, y, includeNonInteractable, marker }) => {
          const matches = document
            .elementsFromPoint(x, y)
            .filter((element) => includeNonInteractable || element.matches("a,button,input,select,textarea,[role],[tabindex]"));
          let count = 0;
          for (const element of matches) {
            const rect = element.getBoundingClientRect();
            if (rect.width <= 0 || rect.height <= 0) continue;
            const outline = document.createElement("div");
            outline.setAttribute(marker, "");
            Object.assign(outline.style, {
              border: "2px solid #ff2d55",
              boxSizing: "border-box",
              height: `${rect.height}px`,
              left: `${rect.left}px`,
              pointerEvents: "none",
              position: "fixed",
              top: `${rect.top}px`,
              width: `${rect.width}px`,
              zIndex: "2147483647",
            });
            document.documentElement.append(outline);
            count += 1;
          }
          return count;
        },
        { ...action, marker: markerAttribute },
      );
      if (marked === 0) throw new Error("No matching element was found at the requested point");
      try {
        return { ok: true, image: { base64: (await page.screenshot({ type: "png" })).toString("base64"), mimeType: "image/png" } };
      } finally {
        await page.evaluate((marker) => document.querySelectorAll(`[${marker}]`).forEach((element) => element.remove()), markerAttribute).catch(
          () => {},
        );
      }
    }
    case "evaluate":
      return { ok: true, value: await evaluatePage(page, action.expression, action.expressionKind, action.arg, timeoutMs(action.timeoutMs)) };
    case "waitForLoadState":
      if ((action.state ?? "load") === "networkidle") throw new Error("playwright_wait_for_load_state does not support networkidle");
      await page.waitForLoadState(action.state ?? "load", { timeout: timeoutMs(action.timeoutMs) });
      return { ok: true };
    case "waitForURL":
      if (action.waitUntil === "networkidle") throw new Error("playwright_wait_for_url does not support networkidle");
      await page.waitForURL(action.url, { timeout: timeoutMs(action.timeoutMs), waitUntil: action.waitUntil ?? "load" });
      return { ok: true };
    default:
      // Locator actions are deliberately not offered by the managed headless runtime.
      throw new Error(`Playwright action '${action.name}' is unavailable`);
  }
}

async function executePageCommand(page, command) {
  switch (command.method) {
    case "navigate":
      return isAllowedUrl(command.url)
        ? (await page.goto(command.url, { timeout: NAVIGATION_TIMEOUT_MS, waitUntil: "load" }), { ok: true, state: await readPageState(page) })
        : { ok: false, error: { code: "navigation_blocked", message: `Navigation URL is not allowed: ${command.url}` } };
    case "back":
      await page.goBack({ timeout: NAVIGATION_TIMEOUT_MS, waitUntil: "load" });
      return { ok: true, state: await readPageState(page) };
    case "forward":
      await page.goForward({ timeout: NAVIGATION_TIMEOUT_MS, waitUntil: "load" });
      return { ok: true, state: await readPageState(page) };
    case "reload":
      await page.reload({ timeout: NAVIGATION_TIMEOUT_MS, waitUntil: "load" });
      return { ok: true, state: await readPageState(page) };
    case "getState":
      return { ok: true, state: await readPageState(page) };
    case "snapshot":
      return { ok: true, snapshot: await captureSnapshot(page, command.maxElements, command.includeHidden) };
    case "screenshot": {
      const png = command.ref
        ? await (await elementForRefOrThrow(page, command.ref)).screenshot({ type: "png" })
        : await page.screenshot({ type: "png", fullPage: command.fullPage, clip: command.clip });
      return { ok: true, image: { base64: png.toString("base64"), mimeType: "image/png" } };
    }
    case "evaluate":
      return { ok: true, value: await evaluatePage(page, command.expression, "string", undefined, 3000) };
    case "waitFor": {
      const limit = Math.min(3000, Math.max(1, command.timeoutMs ?? 3000));
      if (command.selector) await page.locator(command.selector).waitFor({ timeout: limit, state: "visible" });
      else if (command.text) await page.getByText(command.text).waitFor({ timeout: limit, state: "visible" });
      else if (command.textGone) await page.getByText(command.textGone).waitFor({ timeout: limit, state: "hidden" });
      else throw new Error("waitFor requires selector, text, or textGone");
      return { ok: true };
    }
    case "playwright":
      return await executePlaywrightAction(page, command.action ?? {});
    case "playwrightWaitForTimeout": {
      // The CLI runtime applies no clamp; without one a long wait would outlive the host deadline.
      const requested = Number(command.timeoutMs);
      const limit = Number.isFinite(requested) ? Math.min(PLAYWRIGHT_WAIT_FOR_TIMEOUT_MAX_MS, Math.max(0, requested)) : 0;
      await new Promise((resolve) => setTimeout(resolve, limit));
      return { ok: true };
    }
    default:
      return {
        ok: false,
        error: { code: "capability_unsupported", message: `Browser command '${command.method}' is unavailable in the managed CDP page runtime` },
      };
  }
}

// --- Protocol loop -----------------------------------------------------------

function reply(id, message) {
  send({ id, ...message });
}

function dispatch(message) {
  switch (message.method) {
    case "ensureBrowser":
      ensureBrowser().then(
        () => reply(message.id, { result: { generation } }),
        (error) => reply(message.id, { error: { code: classifyError(error), message: String(error?.message ?? error) } }),
      );
      return;
    case "exec":
      execute(message).then(
        (result) => reply(message.id, { result }),
        (error) => reply(message.id, { error: { code: classifyError(error), message: String(error?.message ?? error) } }),
      );
      return;
    case "cancel": {
      const entry = inflight.get(message.requestId);
      if (entry) entry.controller.abort(abortError());
      reply(message.id, { result: { ok: true } });
      return;
    }
    case "closeSession":
      closeSession(String(message.sessionId ?? "")).then(
        () => reply(message.id, { result: { ok: true } }),
        (error) => reply(message.id, { error: { code: classifyError(error), message: String(error?.message ?? error) } }),
      );
      return;
    default:
      if (message.id !== undefined) {
        reply(message.id, { error: { code: "capability_unsupported", message: `Sidecar method '${String(message.method)}' is unsupported` } });
      }
  }
}

let shuttingDown = false;
async function shutdownAndExit() {
  if (shuttingDown) return;
  shuttingDown = true;
  const hardExit = setTimeout(() => process.exit(0), HARD_EXIT_MS);
  hardExit.unref();
  try {
    await shutdown();
  } catch (error) {
    logError("shutdown failed", error);
  }
  process.exit(0);
}

process.stdout.on("error", () => shutdownAndExit());
process.on("SIGTERM", () => shutdownAndExit());
process.on("SIGINT", () => shutdownAndExit());
process.on("SIGHUP", () => shutdownAndExit());
process.on("unhandledRejection", (error) => logError("unhandled rejection", error));

const lines = createInterface({ input: process.stdin, crlfDelay: Number.POSITIVE_INFINITY });
lines.on("line", (line) => {
  const text = line.trim();
  if (!text) return;
  let message;
  try {
    message = JSON.parse(text);
  } catch {
    return;
  }
  if (!message || typeof message !== "object") return;
  // Requests run concurrently so that cancel and lifecycle frames stay responsive.
  Promise.resolve()
    .then(() => dispatch(message))
    .catch((error) => reply(message.id, { error: { code: "execution_error", message: String(error?.message ?? error) } }));
});
lines.on("close", () => shutdownAndExit());

send({ event: "ready", node: process.version });
