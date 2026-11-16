/**
 * @vitest-environment jsdom
 */
import { render } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { InfiniteScrollSentinel } from "./infinite-scroll-sentinel";

describe("InfiniteScrollSentinel", () => {
  afterEach(() => vi.unstubAllGlobals());

  it("keeps one observer while loading and callback props change", () => {
    let notify!: IntersectionObserverCallback;
    const observe = vi.fn();
    const disconnect = vi.fn();
    const Observer = vi.fn(function Observer(
      callback: IntersectionObserverCallback,
    ) {
      notify = callback;
      return { observe, disconnect, unobserve: vi.fn(), takeRecords: vi.fn() };
    });
    vi.stubGlobal("IntersectionObserver", Observer);

    const first = vi.fn();
    const second = vi.fn();
    const view = render(
      <InfiniteScrollSentinel onVisible={first} loading={false} />,
    );

    notify(
      [{ isIntersecting: true } as IntersectionObserverEntry],
      {} as IntersectionObserver,
    );
    expect(first).toHaveBeenCalledTimes(1);

    view.rerender(
      <InfiniteScrollSentinel onVisible={second} loading />,
    );

    expect(Observer).toHaveBeenCalledTimes(1);
    expect(disconnect).not.toHaveBeenCalled();

    notify(
      [{ isIntersecting: true } as IntersectionObserverEntry],
      {} as IntersectionObserver,
    );
    expect(second).toHaveBeenCalledTimes(1);
    view.unmount();
    expect(disconnect).toHaveBeenCalledTimes(1);
  });
});
