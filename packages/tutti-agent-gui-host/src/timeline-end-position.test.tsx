// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { useAgentGUIDetailScroll } from "../../../vendor/tutti/packages/agent/gui/agent-gui/agentGuiNode/view/useAgentGUIDetailScroll";
import type { AgentGUIDetailScrollInput } from "../../../vendor/tutti/packages/agent/gui/agent-gui/agentGuiNode/view/agentGUIDetailScrollTypes";

vi.mock("../../../vendor/tutti/packages/agent/gui/agent-gui/agentGuiNode/view/AgentGUIConversationTimelinePane", () => ({
  setTimelineScrollTopInstantly: (element: HTMLElement, top: number) => { element.scrollTop = top; },
  setTimelineScrollTopWithUserTransition: (element: HTMLElement, top: number) => { element.scrollTop = top; },
}));

let root: Root;
let container: HTMLDivElement;
let timeline: HTMLDivElement;
let content: HTMLDivElement;
let input: AgentGUIDetailScrollInput;
let result: ReturnType<typeof useAgentGUIDetailScroll>;
let height: number;
let viewport: number;
let frames: Map<number, FrameRequestCallback>;
let observers: { callback: ResizeObserverCallback; elements: Set<Element> }[];

function Probe() {
  result = useAgentGUIDetailScroll(input);
  return result.isTimelineScrolledToBottom ? null : <button onClick={result.scrollTimelineToBottom}>Scroll to bottom</button>;
}
async function render() { await act(async () => root.render(<Probe />)); await flush(); }
async function flush() {
  await act(async () => {
    const batch = [...frames.values()]; frames.clear();
    batch.forEach((callback) => callback(0));
  });
}
async function resize() {
  await act(async () => {
    for (const observer of observers) {
      const entries = [...observer.elements].map((target) => ({ target, contentRect: { height: target === timeline ? viewport : height } }));
      if (entries.length) observer.callback(entries as ResizeObserverEntry[], {} as ResizeObserver);
    }
  });
  await flush();
}
async function scroll(top: number, key?: string) {
  await act(async () => {
    if (key) timeline.dispatchEvent(new KeyboardEvent("keydown", { key }));
    timeline.scrollTop = top;
    timeline.dispatchEvent(new Event("scroll"));
  });
  await flush();
}

beforeEach(() => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  frames = new Map(); let nextFrame = 0; observers = [];
  vi.spyOn(window, "requestAnimationFrame").mockImplementation((callback) => { frames.set(++nextFrame, callback); return nextFrame; });
  vi.spyOn(window, "cancelAnimationFrame").mockImplementation((id) => { frames.delete(id); });
  vi.stubGlobal("ResizeObserver", class {
    elements = new Set<Element>();
    constructor(readonly callback: ResizeObserverCallback) { observers.push(this); }
    observe(element: Element) { this.elements.add(element); }
    disconnect() { this.elements.clear(); }
  });
  timeline = document.createElement("div"); content = document.createElement("div");
  height = 1000; viewport = 300;
  Object.defineProperties(timeline, {
    scrollHeight: { get: () => height }, clientHeight: { get: () => viewport },
  });
  input = {
    actions: { loadOlderConversationMessages: vi.fn() },
    timelineRef: { current: timeline }, timelineContentRef: { current: content },
    bottomDockRef: { current: document.createElement("div") }, bottomDockStoreRevision: "initial",
    timelineScrollAnchorRef: { current: null }, pendingPrependScrollAnchorRef: { current: null },
    submittedPromptScrollConversationRef: { current: null }, virtualScrollControllerRef: { current: null },
    isVisible: true, showTimelineSkeleton: false, timelineConversationId: "session",
    conversation: null,
    viewModel: { rail: { activeConversationId: "session" }, detail: { hasOlderMessages: false, isLoadingOlderMessages: false } },
  } as unknown as AgentGUIDetailScrollInput;
  container = document.createElement("div"); document.body.append(container); root = createRoot(container);
});
afterEach(async () => {
  await act(async () => root.unmount()); container.remove();
  vi.restoreAllMocks(); vi.unstubAllGlobals();
});

it("hides the button at the physical end while detached and keeps subsequent content from pulling the reader", async () => {
  await render(); expect(result.followEndMode).toBe("following");
  await scroll(400, "ArrowUp"); expect(container.querySelector("button")).not.toBeNull();
  timeline.dispatchEvent(new Event("scrollend"));
  await scroll(700);
  expect(result.followEndMode).toBe("detached");
  expect(container.querySelector("button")).toBeNull();
  height = 1200; await resize();
  expect(timeline.scrollTop).toBe(700);
  expect(result.followEndMode).toBe("detached");
  expect(container.querySelector("button")).not.toBeNull();
  await act(async () => container.querySelector("button")!.click()); await scroll(900);
  expect(result.followEndMode).toBe("following");
  expect(container.querySelector("button")).toBeNull();
});

it("updates after viewport and content resizes with a one CSS pixel rounding tolerance", async () => {
  await render(); await scroll(400, "PageUp");
  viewport = 600; await resize();
  expect(result.isTimelineScrolledToBottom).toBe(true);
  viewport = 300; await resize(); expect(result.isTimelineScrolledToBottom).toBe(false);
  await scroll(699.25); expect(result.isTimelineScrolledToBottom).toBe(true);
  await scroll(698.5); expect(result.isTimelineScrolledToBottom).toBe(false);
  height = 200; timeline.scrollTop = 0; await resize();
  expect(result.isTimelineScrolledToBottom).toBe(true);
});

it("measures virtual spacer growth even while the controller's cached end state lags", async () => {
  const controller = {
    enabled: true, agentSessionId: "session", isAtEnd: vi.fn(() => true),
    scrollToEnd: vi.fn(), scrollToOffset: vi.fn(),
  };
  input.virtualScrollControllerRef.current = controller;
  await render(); await scroll(400, "Home");
  expect(result.isTimelineScrolledToBottom).toBe(false);
  await scroll(700); expect(result.isTimelineScrolledToBottom).toBe(true);
  expect(result.followEndMode).toBe("detached");
  height = 1200; await resize();
  expect(result.isTimelineScrolledToBottom).toBe(false);
  expect(timeline.scrollTop).toBe(700);
  expect(controller.isAtEnd).not.toHaveBeenCalled();
  height = 1000;
  await act(async () => result.setVirtualScrollController({ ...controller, isAtEnd: () => false }));
  await flush(); expect(result.isTimelineScrolledToBottom).toBe(true);
});

it("cleans up measurements while hidden and resets for skeletons and empty conversations", async () => {
  await render(); await scroll(400, "ArrowUp");
  expect(result.isTimelineScrolledToBottom).toBe(false);
  input = { ...input, isVisible: false }; await render();
  expect(observers.every((observer) => observer.elements.size === 0)).toBe(true);
  expect(frames.size).toBe(0);
  height = 700; input = { ...input, isVisible: true }; await render();
  expect(result.isTimelineScrolledToBottom).toBe(true);
  input = { ...input, showTimelineSkeleton: true }; await render();
  expect(result.isTimelineScrolledToBottom).toBe(true);
  input = { ...input, showTimelineSkeleton: false, timelineConversationId: null }; await render();
  expect(result.isTimelineScrolledToBottom).toBe(true);
});

it("does not reuse virtual geometry from the previous session", async () => {
  await render(); await scroll(400, "ArrowUp");
  input.virtualScrollControllerRef.current = {
    enabled: true, agentSessionId: "previous", isAtEnd: vi.fn(() => false), scrollToEnd: vi.fn(), scrollToOffset: vi.fn(),
  };
  input = { ...input, timelineConversationId: "next", viewModel: { ...input.viewModel, rail: { ...input.viewModel.rail, activeConversationId: "next" } } };
  await render(); expect(result.isTimelineScrolledToBottom).toBe(true);
  expect(input.virtualScrollControllerRef.current!.isAtEnd).not.toHaveBeenCalled();
});
