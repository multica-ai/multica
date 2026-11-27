import { act, fireEvent, render, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { continuous } from "@multica/ui/components/ui/number-flow";
import { FooterWordmark } from "./footer-wordmark";

type IntersectionCb = IntersectionObserverCallback;

const intersectionObservers: Array<{ cb: IntersectionCb; elements: Element[] }> = [];

function stubBrowserAPIs(reducedMotion = false) {
  intersectionObservers.length = 0;

  vi.stubGlobal(
    "matchMedia",
    vi.fn().mockImplementation((query: string) => ({
      matches: query === "(prefers-reduced-motion: reduce)" ? reducedMotion : false,
      media: query,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      addListener: vi.fn(),
      removeListener: vi.fn(),
      dispatchEvent: vi.fn(),
    })),
  );

  vi.stubGlobal(
    "IntersectionObserver",
    class MockIntersectionObserver {
      private record: { cb: IntersectionCb; elements: Element[] };
      constructor(cb: IntersectionCb) {
        this.record = { cb, elements: [] };
        intersectionObservers.push(this.record);
      }
      observe(el: Element) {
        this.record.elements.push(el);
      }
      unobserve() {}
      disconnect() {}
      takeRecords() {
        return [];
      }
    },
  );

  vi.stubGlobal(
    "ResizeObserver",
    class MockResizeObserver {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  );

  vi.spyOn(HTMLCanvasElement.prototype, "getContext").mockReturnValue({
    setTransform: vi.fn(),
    clearRect: vi.fn(),
    fillRect: vi.fn(),
    translate: vi.fn(),
    rotate: vi.fn(),
    scale: vi.fn(),
    fill: vi.fn(),
    stroke: vi.fn(),
    getImageData: vi.fn(() => ({ data: new Uint8ClampedArray(4) })),
    fillStyle: "",
    globalAlpha: 1,
    lineWidth: 1,
  } as unknown as CanvasRenderingContext2D);
}

function triggerAllIntersections() {
  for (const obs of intersectionObservers) {
    for (const target of obs.elements) {
      obs.cb(
        [{ isIntersecting: true, target } as IntersectionObserverEntry],
        {} as IntersectionObserver,
      );
    }
  }
}

describe("FooterWordmark", () => {
  beforeEach(() => {
    stubBrowserAPIs(false);
  });

  afterEach(() => {
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
  });

  it("keeps the counter unmounted until the server total arrives, then animates with NumberFlow", async () => {
    let resolveFetch!: (value: Response) => void;
    const fetchMock = vi.fn().mockImplementation(
      () =>
        new Promise<Response>((resolve) => {
          resolveFetch = resolve;
        }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const { container } = render(
      <FooterWordmark
        label="大家已经把这颗星吹转了 {count} 圈"
        locale="zh-Hans"
      />,
    );

    const counterParagraph = container.querySelector("p");
    expect(counterParagraph).toHaveClass("opacity-0");
    expect(container.querySelector("number-flow-react")).toBeNull();

    act(() => {
      triggerAllIntersections();
    });

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith("/api/site/star", { cache: "no-store" });
    });

    await act(async () => {
      resolveFetch(
        new Response(JSON.stringify({ turns: 113835 }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
      );
    });

    const flow = container.querySelector("number-flow-react") as
      | (HTMLElement & {
          respectMotionPreference?: boolean;
          plugins?: unknown[];
        })
      | null;

    expect(counterParagraph).not.toHaveClass("opacity-0");
    expect(counterParagraph).toHaveTextContent("大家已经把这颗星吹转了");
    expect(counterParagraph).toHaveTextContent("圈");
    expect(flow).not.toBeNull();
    expect(flow).toHaveAttribute("aria-label", "113,835");
    expect(flow?.respectMotionPreference).toBe(true);
    expect(flow?.plugins).toEqual([continuous]);
  });

  it("updates the animated NumberFlow counter as turns accumulate", async () => {
    stubBrowserAPIs(true);
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ turns: 113835 }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const { container } = render(
      <FooterWordmark
        label="Everyone has blown the star round {count} times"
        locale="en"
      />,
    );

    act(() => {
      triggerAllIntersections();
    });

    await waitFor(() => {
      expect(container.querySelector("number-flow-react")).toHaveAttribute(
        "aria-label",
        "113,835",
      );
    });

    // Under reduced motion each click turns the star one arm (1/8 turn),
    // so 8 clicks credit 1 full turn.
    const canvas = container.querySelector("canvas")!;
    act(() => {
      for (let i = 0; i < 8; i++) {
        fireEvent.pointerDown(canvas, { button: 0, clientX: 50, clientY: 50 });
      }
    });

    await waitFor(() => {
      expect(container.querySelector("number-flow-react")).toHaveAttribute(
        "aria-label",
        "113,836",
      );
    });
  });
});
