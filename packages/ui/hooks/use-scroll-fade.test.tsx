// @vitest-environment jsdom
import { render, act } from "@testing-library/react";
import { type CSSProperties, useRef, useState } from "react";
import { describe, expect, it, vi } from "vitest";
import { useScrollFade } from "./use-scroll-fade";

/** jsdom reports every box as 0×0; these getters stand in for real layout. */
function fakeScrollBox(
  el: HTMLElement,
  box: { client: number; scroll: number; position?: number },
): void {
  Object.defineProperty(el, "clientHeight", { configurable: true, value: box.client });
  Object.defineProperty(el, "scrollHeight", { configurable: true, value: box.scroll });
  Object.defineProperty(el, "scrollTop", {
    configurable: true,
    value: box.position ?? 0,
  });
}

/**
 * The hook measures through three layout getters per run; wrap the fakes so
 * each measurement bumps a counter the lifecycle tests can assert against.
 * `fakeScrollBox` defines these configurable, so the wrappers stack.
 */
function trackMeasures(el: HTMLElement): () => number {
  let count = 0;
  for (const prop of ["scrollTop", "scrollHeight", "clientHeight"]) {
    const desc = Object.getOwnPropertyDescriptor(el, prop)!;
    const value = desc.value;
    Object.defineProperty(el, prop, {
      configurable: desc.configurable,
      enumerable: desc.enumerable,
      get() {
        count += 1;
        return typeof value === "function" ? value.call(el) : value;
      },
    });
  }
  return () => count;
}

// jsdom's cssstyle drops `mask-image`, so assert on the hook's return value.
function Host({
  box,
  axis,
  onRender,
  onStyle,
}: {
  box: { client: number; scroll: number; position?: number };
  axis?: "vertical" | "horizontal";
  onRender?: () => void;
  onStyle?: (style: CSSProperties | undefined) => void;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const [, bump] = useState(0);
  const style = useScrollFade(ref, 32, axis ?? "vertical");
  onRender?.();
  onStyle?.(style);
  return (
    <div
      ref={(node) => {
        ref.current = node;
        if (node) fakeScrollBox(node, box);
      }}
    >
      <button type="button" onClick={() => bump((n) => n + 1)}>
        bump
      </button>
    </div>
  );
}

describe("useScrollFade", () => {
  it("returns no mask for a non-scrollable box", () => {
    const styles: (CSSProperties | undefined)[] = [];
    render(<Host box={{ client: 100, scroll: 100 }} onStyle={(s) => styles.push(s)} />);
    act(() => flushRaf());
    expect(styles.at(-1)).toBeUndefined();
  });

  it("returns the end fade at the top of a scrollable box", () => {
    const styles: (CSSProperties | undefined)[] = [];
    render(<Host box={{ client: 100, scroll: 400 }} onStyle={(s) => styles.push(s)} />);
    act(() => flushRaf());
    expect(String(styles.at(-1)?.maskImage)).toContain("transparent 100%");
  });

  it("keeps the fade value when the box overflows and position does not change", () => {
    // Mount and resize storms used to dispatch an identical setState per
    // observer hit — hundreds per second across the shell's fade hosts.
    // Identical values must not reach React at all.
    const renders = vi.fn();
    const styles: (CSSProperties | undefined)[] = [];
    const { container } = render(
      <Host box={{ client: 100, scroll: 400 }} onRender={renders} onStyle={(s) => styles.push(s)} />,
    );
    act(() => flushRaf());
    const afterMount = renders.mock.calls.length;
    const box = container.querySelector("div")!;
    // Simulate the mount/realtime storm: far more mutations and scroll events
    // than frames, with layout unchanged throughout.
    act(() => {
      for (let i = 0; i < 50; i++) {
        box.dispatchEvent(new Event("scroll"));
        box.appendChild(document.createElement("span"));
        box.removeChild(box.lastChild!);
      }
      flushRaf();
    });
    expect(renders.mock.calls.length).toBe(afterMount);
    expect(String(styles.at(-1)?.maskImage)).toContain("transparent 100%");
  });

  it("absorbs the queued resize frame when a synchronous scroll lands first", async () => {
    // Audit scenario: resize queues frame A, a synchronous scroll measures
    // right away and must CANCEL A (not just drop its handle), then a
    // mutation queues frame B — exactly one frame pending, two measurements
    // total.
    const { container } = render(<Host box={{ client: 100, scroll: 400 }} />);
    act(() => flushRaf());
    const el = container.querySelector("div")!;
    const measures = trackMeasures(el);
    act(() => {
      triggerResizeObservers();
      expect(pendingRaf()).toBe(1);
      el.dispatchEvent(new Event("scroll"));
      expect(pendingRaf()).toBe(0);
    });
    // jsdom dispatches MutationObserver callbacks on a microtask.
    await act(async () => {
      el.appendChild(document.createElement("span"));
      await Promise.resolve();
    });
    act(() => {
      expect(pendingRaf()).toBe(1);
      flushRaf();
    });
    expect(measures()).toBe(6); // scroll + frame B, 3 layout reads each
  });

  it("leaves no queued frame behind when unmounting after a synchronous scroll", () => {
    // Audit scenario: resize queued a frame, scroll absorbed it, unmount —
    // nothing may fire afterwards (the old handle drop let the frame
    // survive cleanup).
    const { container, unmount } = render(<Host box={{ client: 100, scroll: 400 }} />);
    act(() => flushRaf());
    const el = container.querySelector("div")!;
    const measures = trackMeasures(el);
    act(() => {
      triggerResizeObservers();
      el.dispatchEvent(new Event("scroll"));
    });
    unmount();
    expect(pendingRaf()).toBe(0);
    act(() => flushRaf());
    expect(measures()).toBe(3); // only the synchronous scroll measured
  });

  it("removes the scroll listener on cleanup", () => {
    // Audit scenario: the cleanup used to remove `schedule` while the
    // listener was registered as `run`, leaving the measurement attached
    // across StrictMode remounts and axis changes.
    const { container, unmount } = render(<Host box={{ client: 100, scroll: 400 }} />);
    act(() => flushRaf());
    const el = container.querySelector("div")!;
    const measures = trackMeasures(el);
    unmount();
    const before = measures();
    el.dispatchEvent(new Event("scroll"));
    expect(measures()).toBe(before);
  });

  it("rebuilds with the new axis and drops the old scroll listener", () => {
    const styles: (CSSProperties | undefined)[] = [];
    const { container, rerender } = render(
      <Host axis="vertical" box={{ client: 100, scroll: 400 }} onStyle={(s) => styles.push(s)} />,
    );
    act(() => flushRaf());
    rerender(<Host axis="horizontal" box={{ client: 100, scroll: 400 }} onStyle={(s) => styles.push(s)} />);
    act(() => flushRaf());
    const el = container.querySelector("div")!;
    const measures = trackMeasures(el);
    const before = measures();
    el.dispatchEvent(new Event("scroll"));
    // The old axis reads scrollTop/scrollHeight/clientHeight; a leftover
    // listener from the vertical closure would bump this counter. The new
    // horizontal run sees no overflow in jsdom and renders no mask.
    expect(measures() - before).toBe(0);
    expect(styles.at(-1)).toBeUndefined();
  });

  it("updates the fade when the scroll position crosses a boundary", () => {
    const styles: (CSSProperties | undefined)[] = [];
    const { container } = render(
      <Host box={{ client: 100, scroll: 400 }} onStyle={(s) => styles.push(s)} />,
    );
    act(() => flushRaf());
    const box = container.querySelector("div")!;
    act(() => {
      fakeScrollBox(box, { client: 100, scroll: 400, position: 150 });
      box.dispatchEvent(new Event("scroll"));
      flushRaf();
    });
    const mask = String(styles.at(-1)?.maskImage ?? "");
    expect(mask).toContain("transparent 0%");
    expect(mask).toContain("transparent 100%");
  });
});
