import { act, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { useAppearanceStore } from "@multica/core/appearance";
import { renderWithI18n } from "../../test/i18n";
import { CollectionBatchBar } from "./collection-batch-bar";

function bar(count: number) {
  return (
    <CollectionBatchBar count={count} onClear={() => {}}>
      <button type="button">Archive</button>
    </CollectionBatchBar>
  );
}

/** One animation frame: motion applies instant values on its next frame. */
const nextFrame = () => act(() => new Promise<void>((resolve) => requestAnimationFrame(() => resolve())));

const toolbar = () => screen.queryByRole("toolbar");

afterEach(() => {
  useAppearanceStore.getState().reset();
  vi.unstubAllGlobals();
});

describe("CollectionBatchBar", () => {
  it("fades and rises in when rows are selected", async () => {
    const view = renderWithI18n(bar(0));
    view.rerender(bar(2));
    await nextFrame();
    expect(toolbar()).toBeInTheDocument();
    expect(Number(toolbar()!.style.opacity)).toBeLessThan(1);
  });

  describe.each([
    ["the Appearance Reduce motion switch", () => useAppearanceStore.getState().set({ reduceMotion: true })],
    [
      "the system reduced-motion setting",
      () =>
        vi.stubGlobal("matchMedia", vi.fn((query: string) => ({
          matches: query === "(prefers-reduced-motion: reduce)",
          addEventListener: vi.fn(),
          removeEventListener: vi.fn(),
        }))),
    ],
  ])("with %s", (_name, reduce) => {
    it("shows the bar fully in the first frame and removes it in the first frame", async () => {
      reduce();
      const view = renderWithI18n(bar(0));
      view.rerender(bar(2));
      await nextFrame();
      expect(toolbar()).toHaveStyle({ opacity: "1", transform: "translateY(0)" });

      view.rerender(bar(0));
      await nextFrame();
      expect(toolbar()).toBeNull();
    });
  });
});
