import { afterEach } from "vitest";
import "@testing-library/jest-dom/vitest";
import "@multica/vitest-setup/web-storage";

// jsdom ships neither class. use-scroll-fade observes both.
const roInstances: ResizeObserverStub[] = [];

class ResizeObserverStub implements ResizeObserver {
  static triggerAll(): void {
    for (const instance of [...roInstances]) instance.callback([], instance);
  }

  constructor(private readonly callback: ResizeObserverCallback) {
    roInstances.push(this);
  }

  observe(): void {}

  unobserve(): void {}

  disconnect(): void {}
}

if (typeof globalThis.ResizeObserver === "undefined") {
  globalThis.ResizeObserver = ResizeObserverStub;
}

// jsdom never runs its own frame loop. Queue callbacks so tests can inspect
// exactly how many frames are pending and flush one batch at a time.
const rafQueue: FrameRequestCallback[] = [];
globalThis.requestAnimationFrame = (cb: FrameRequestCallback): number => {
  rafQueue.push(cb);
  return rafQueue.length;
};
globalThis.cancelAnimationFrame = (handle: number): void => {
  const index = handle - 1;
  if (index >= 0 && index < rafQueue.length) rafQueue[index] = null as unknown as FrameRequestCallback;
};
globalThis.flushRaf = (): void => {
  const pending = rafQueue.splice(0, rafQueue.length);
  for (const cb of pending) cb?.(performance.now());
};
globalThis.pendingRaf = (): number =>
  rafQueue.filter((cb) => typeof cb === "function").length;

declare global {
  var flushRaf: () => void;
  var pendingRaf: () => number;
  var triggerResizeObservers: () => void;
}

globalThis.triggerResizeObservers = ResizeObserverStub.triggerAll;

// Run leftover frames instead of dropping them: Base UI's module-level frame
// scheduler only requests a new frame after its pending one has run, so a
// dropped frame stalls every later Base UI animation in the same file.
const MAX_TEARDOWN_FRAMES = 100;

function drainRaf(): void {
  for (let i = 0; i < MAX_TEARDOWN_FRAMES && rafQueue.length > 0; i++) {
    for (const cb of rafQueue.splice(0, rafQueue.length)) {
      try {
        cb?.(performance.now());
      } catch {
        // The component is already unmounted; its late frame has nothing to update.
      }
    }
  }
  rafQueue.length = 0;
}

afterEach(() => {
  drainRaf();
  roInstances.length = 0;
});
