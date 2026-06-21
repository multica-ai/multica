// @vitest-environment jsdom
// FIR-1673 — the image document viewer must recover from a stalled load:
// auto-retry a couple of times, then offer a manual "Reload image" button.
// FIR-1713 — recovery must also fire when the transfer *stalls* (no `error`
// event): a stall watchdog auto-retries, and an always-visible "Reload image"
// button lets the user re-trigger the fetch at any time.
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { ImagePreview } from "./image-preview";

const SRC = "/api/attachments/abc/download?workspace_id=ws1";
const STALL_TIMEOUT_MS = 10000;

function currentImg(): HTMLImageElement {
  return screen.getByAltText("diagram.png") as HTMLImageElement;
}

function reloadButtons(): HTMLElement[] {
  return screen.getAllByRole("button", { name: /reload image/i });
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

  it("always offers a manual reload button, even while loading", () => {
    render(<ImagePreview src={SRC} alt="diagram.png" />);
    expect(reloadButtons().length).toBeGreaterThanOrEqual(1);
  });

  it("auto-retries with a cache-busted src, then surfaces the error state", () => {
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
    expect(reloadButtons().length).toBeGreaterThanOrEqual(1);
    expect(screen.queryByAltText("diagram.png")).toBeNull();
  });

  it("treats a stall (no error event) as a failure and auto-retries", () => {
    render(<ImagePreview src={SRC} alt="diagram.png" />);
    expect(currentImg().getAttribute("src")).toBe(SRC);

    // No `load`, no `error` — just a hung transfer. The watchdog must trip.
    act(() => {
      vi.advanceTimersByTime(STALL_TIMEOUT_MS);
      vi.advanceTimersByTime(700); // retry delay
    });
    expect(currentImg().getAttribute("src")).toContain("_reload=1");
  });

  it("surfaces the error state after repeated stalls", () => {
    render(<ImagePreview src={SRC} alt="diagram.png" downloadUrl="/dl" />);

    // Two watchdog trips are silent retries; the third spends them.
    for (let i = 0; i < 3; i++) {
      act(() => {
        vi.advanceTimersByTime(STALL_TIMEOUT_MS);
        vi.advanceTimersByTime(700);
      });
    }
    expect(screen.getByText("The image couldn’t finish loading.")).toBeTruthy();
  });

  it("reloads the image when the manual button is clicked", () => {
    render(<ImagePreview src={SRC} alt="diagram.png" />);

    const [firstReload] = reloadButtons();
    act(() => {
      fireEvent.click(firstReload!);
    });

    // Loading again, with a fresh cache-buster.
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

  it("does not trip the stall watchdog after the image loads", () => {
    render(<ImagePreview src={SRC} alt="diagram.png" />);
    act(() => {
      fireEvent.load(currentImg());
    });
    // Well past the stall window — a loaded image must stay put, no retry.
    act(() => {
      vi.advanceTimersByTime(STALL_TIMEOUT_MS * 2);
    });
    expect(currentImg().getAttribute("src")).toBe(SRC);
    expect(screen.queryByText("Loading image…")).toBeNull();
  });
});
