// @vitest-environment jsdom
// FIR-1673 — the image document viewer must recover from a stalled load:
// auto-retry a couple of times, then offer a manual "Reload image" button.
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { ImagePreview } from "./image-preview";

const SRC = "/api/attachments/abc/download?workspace_id=ws1";

function currentImg(): HTMLImageElement {
  return screen.getByAltText("diagram.png") as HTMLImageElement;
}

describe("ImagePreview", () => {
  beforeEach(() => vi.useFakeTimers());
  afterEach(() => {
    cleanup();
    vi.useRealTimers();
  });

  it("shows a loading indicator and the unmodified src on first paint", () => {
    render(<ImagePreview src={SRC} alt="diagram.png" />);
    expect(screen.getByText("Loading image…")).toBeTruthy();
    expect(currentImg().getAttribute("src")).toBe(SRC);
  });

  it("auto-retries with a cache-busted src, then surfaces a manual reload", () => {
    render(<ImagePreview src={SRC} alt="diagram.png" downloadUrl="/dl" />);

    // First failure → silent retry #1 with a cache-buster.
    act(() => {
      fireEvent.error(currentImg());
      vi.advanceTimersByTime(700);
    });
    expect(currentImg().getAttribute("src")).toContain("_reload=1");

    // Second failure → silent retry #2.
    act(() => {
      fireEvent.error(currentImg());
      vi.advanceTimersByTime(700);
    });
    expect(currentImg().getAttribute("src")).toContain("_reload=2");

    // Third failure → automatic retries spent, error state with the button.
    act(() => {
      fireEvent.error(currentImg());
      vi.advanceTimersByTime(700);
    });
    expect(screen.getByText("The image couldn’t finish loading.")).toBeTruthy();
    expect(screen.getByRole("button", { name: /reload image/i })).toBeTruthy();
    expect(screen.queryByAltText("diagram.png")).toBeNull();
  });

  it("reloads the image when the manual button is clicked", () => {
    render(<ImagePreview src={SRC} alt="diagram.png" />);

    // Burn through the automatic retries to reach the error state.
    for (let i = 0; i < 3; i++) {
      act(() => {
        fireEvent.error(currentImg());
        vi.advanceTimersByTime(700);
      });
    }

    act(() => {
      fireEvent.click(screen.getByRole("button", { name: /reload image/i }));
    });

    // Image is back, loading again, with a fresh cache-buster.
    expect(screen.getByText("Loading image…")).toBeTruthy();
    expect(currentImg().getAttribute("src")).toContain("_reload=");
  });

  it("clears the loading indicator once the image loads", () => {
    render(<ImagePreview src={SRC} alt="diagram.png" />);
    act(() => {
      fireEvent.load(currentImg());
    });
    expect(screen.queryByText("Loading image…")).toBeNull();
  });
});
