// @vitest-environment jsdom
import { observable, runInAction } from "mobx";
import { afterEach, expect, it, vi } from "vitest";
import type { AgentGuiHostEvent, AgentGuiInitSnapshot } from "./protocol";
import { installConversationUiProfile } from "./ui-performance";

type Args = Parameters<typeof installConversationUiProfile>;
const cleanups: (() => void)[] = [];
afterEach(() => {
  for (const cleanup of cleanups.splice(0)) cleanup();
  window.multicaUiPerformance?.stop();
  delete window.multicaUiPerformance;
  document.body.replaceChildren();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

function setup(browserEntries = false) {
  vi.useFakeTimers({ toFake: ["Date", "performance", "setTimeout", "clearTimeout"] });
  const logs = vi.spyOn(console, "info").mockImplementation(() => {});
  const frames = new Map<number, FrameRequestCallback>();
  let sequence = 0;
  vi.stubGlobal("requestAnimationFrame", (callback: FrameRequestCallback) => { frames.set(++sequence, callback); return sequence; });
  vi.stubGlobal("cancelAnimationFrame", (id: number) => frames.delete(id));
  const observerCallbacks = new Map<string, (list: { getEntries(): PerformanceEntry[] }) => void>();
  const disconnect = vi.fn();
  vi.stubGlobal("PerformanceObserver", browserEntries ? class {
    static supportedEntryTypes = ["longtask", "layout-shift", "event", "long-animation-frame"];
    constructor(private callback: (list: { getEntries(): PerformanceEntry[] }) => void) {}
    observe({ type }: { type: string }) { observerCallbacks.set(type, this.callback); }
    disconnect = disconnect;
  } : undefined);
  Object.defineProperty(document, "visibilityState", { configurable: true, value: "visible" });
  const root = document.body.appendChild(document.createElement("div"));
  root.innerHTML = '<main data-agent-session-id="new"><div contenteditable="true"></div></main>';
  const pane = root.querySelector("main")!;
  const activations = observable.box<{ mode: string; requestId: string; content: unknown[]; agentSessionId: string; requestedAtUnixMs: number }[]>([]);
  const listeners = new Set<(event: AgentGuiHostEvent) => void>();
  const client = { subscribe: (listener: (event: AgentGuiHostEvent) => void) => {
    listeners.add(listener); return () => { listeners.delete(listener); };
  } } as Args[1];
  const handle = { runtime: { activations: { pending: () => activations.get() } } } as unknown as Args[2];
  const initial = { workspaceId: "workspace", userId: "user", hostVisible: true } as AgentGuiInitSnapshot;
  const profile = installConversationUiProfile(root, client, handle, initial);
  cleanups.push(() => profile.dispose());
  window.multicaUiPerformance!.start();
  const activate = () => runInAction(() => activations.set([{
    mode: "new", requestId: "request", agentSessionId: "new", content: [{ text: "SECRET" }], requestedAtUnixMs: Date.now(),
  }]));
  const frame = async (ms = 16) => {
    await vi.advanceTimersByTimeAsync(ms);
    const pending = [...frames.values()]; frames.clear();
    for (const callback of pending) callback(performance.now());
  };
  return { root, pane, profile, activate, frame, frames, logs, listeners, initial, observerCallbacks, disconnect };
}

it("captures first-send disappearance and welcome reentry without retaining message content or sampling at idle", async () => {
  const app = setup();
  expect(app.frames.size).toBe(0);
  app.activate();
  app.profile.rendered(42);
  app.pane.insertAdjacentHTML("beforeend", '<div data-agent-message-speaker="user">SECRET</div>');
  await app.frame();
  app.pane.querySelector('[data-agent-message-speaker="user"]')!.remove();
  app.pane.insertAdjacentHTML("beforeend", '<div class="multica-empty-conversation-welcome"></div>');
  await app.frame(70);
  app.pane.querySelector(".multica-empty-conversation-welcome")!.remove();
  app.pane.querySelector('[contenteditable="true"]')!.replaceWith(document.createElement("div"));
  app.pane.insertAdjacentHTML("beforeend", '<div contenteditable="true"></div><div data-agent-message-speaker="user">SECRET</div>');
  await app.frame();
  await vi.advanceTimersByTimeAsync(2_000);
  const samples = window.multicaUiPerformance!.snapshot();
  expect(samples).toHaveLength(1);
  expect(samples[0]).toMatchObject({
    action: "first_send", outcome: "window_complete", sampledFrames: 3,
    firstMessageFrameMs: 16, selectedSessionFrameMs: 16, maxFrameIntervalMs: 70,
    frameIntervalsOver50Ms: 1, messageDisappearances: 1, welcomeReentries: 1,
    composerReplacements: 1, reactCommits: 1, reactDurationMs: 42, maxReactDurationMs: 42,
  });
  expect(JSON.stringify(app.logs.mock.calls)).not.toContain("SECRET");
  expect(app.frames.size).toBe(0);
  expect(vi.getTimerCount()).toBe(0);
});

it("measures selection from the list click, ignores modified clicks and stops cleanly on request", async () => {
  const app = setup();
  document.body.insertAdjacentHTML("afterbegin", '<nav id="local-sessions"><a href="http://localhost/conversations?session=other">Private title</a></nav>');
  const link = document.querySelector("a")!;
  const click = (metaKey = false) => {
    const event = new MouseEvent("click", { bubbles: true, cancelable: true, metaKey });
    Object.defineProperty(event, "timeStamp", { value: performance.now() });
    link.addEventListener("click", (event) => event.preventDefault(), { once: true });
    link.dispatchEvent(event);
  };
  click(true);
  expect(app.frames.size).toBe(0);
  click();
  await app.frame();
  app.pane.setAttribute("data-agent-session-id", "other");
  await app.frame(32);
  window.multicaUiPerformance!.stop();
  expect(window.multicaUiPerformance!.snapshot()[0]).toMatchObject({
    action: "switch_conversation", outcome: "stopped", selectedSessionFrameMs: 48,
    firstMessageFrameMs: null, messageDisappearances: 0,
  });
  expect(JSON.stringify(app.logs.mock.calls)).not.toMatch(/Private title|session=other/);
  click();
  expect(app.frames.size).toBe(0);
  expect(vi.getTimerCount()).toBe(0);
});

it("ends a capture when the host becomes hidden and disposes its listeners", () => {
  const app = setup();
  app.activate();
  for (const listener of app.listeners)
    listener({ type: "init", snapshot: { ...app.initial, hostVisible: false } });
  expect(window.multicaUiPerformance!.snapshot()[0]).toMatchObject({ action: "first_send", outcome: "hidden" });
  app.profile.dispose();
  expect(app.listeners.size).toBe(0);
  expect(app.frames.size).toBe(0);
  expect(vi.getTimerCount()).toBe(0);
});

it("separates renderer long tasks from root-scoped layout and event timings", async () => {
  const app = setup(true);
  app.activate();
  const deliver = (entryType: string, fields: object) => {
    app.observerCallbacks.get(entryType)!({ getEntries: () => [{ entryType, startTime: 0, duration: 75, ...fields } as PerformanceEntry] });
  };
  deliver("longtask", {});
  deliver("layout-shift", { value: 0.001234, sources: [{ node: app.pane }] });
  deliver("layout-shift", { value: 0.5, sources: [{ node: document.body }] });
  deliver("event", { target: app.pane });
  deliver("event", { target: document.body, duration: 900 });
  deliver("long-animation-frame", { duration: 150, renderStart: 20, blockingDuration: 100,
    scripts: [{ sourceURL: "http://localhost/@fs/private/user/repo/packages/ui/render.ts?token=SECRET", sourceFunctionName: "render", sourceCharPosition: 40, duration: 60, forcedStyleAndLayoutDuration: 12 }] });
  await vi.advanceTimersByTimeAsync(2_000);
  expect(window.multicaUiPerformance!.snapshot()[0]).toMatchObject({
    browserEntryTypes: ["longtask", "layout-shift", "event", "long-animation-frame"],
    pageLongTasks: 1, pageLongTaskMs: 75, layoutShiftScore: 0.001234, maxEventResponseMs: 75,
    firstMessageFrameMs: null,
    pageLongAnimationFrames: 1, pageAnimationBlockingMs: 100, pageRenderPhaseMs: 130,
    slowScripts: [{ file: "packages/ui/render.ts", functionName: "render", sourceCharPosition: 40, durationMs: 60, forcedStyleAndLayoutMs: 12 }],
  });
  expect(JSON.stringify(app.logs.mock.calls)).not.toMatch(/SECRET|private\/user/);
  app.profile.dispose();
  expect(app.disconnect).toHaveBeenCalledTimes(4);
});
