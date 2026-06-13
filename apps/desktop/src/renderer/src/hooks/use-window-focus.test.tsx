import { afterEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { readWindowFocus, useWindowFocus } from "./use-window-focus";

function FocusProbe() {
  const isFocused = useWindowFocus();
  return <div data-testid="focus-state">{String(isFocused)}</div>;
}

afterEach(() => {
  vi.restoreAllMocks();
});

describe("readWindowFocus", () => {
  it("reads document focus state", () => {
    vi.spyOn(document, "hasFocus").mockReturnValue(false);
    expect(readWindowFocus()).toBe(false);

    vi.mocked(document.hasFocus).mockReturnValue(true);
    expect(readWindowFocus()).toBe(true);
  });
});

describe("useWindowFocus", () => {
  it("updates when the window gains and loses focus", () => {
    const hasFocus = vi.spyOn(document, "hasFocus").mockReturnValue(true);
    render(<FocusProbe />);

    expect(screen.getByTestId("focus-state")).toHaveTextContent("true");

    hasFocus.mockReturnValue(false);
    fireEvent.blur(window);
    expect(screen.getByTestId("focus-state")).toHaveTextContent("false");

    hasFocus.mockReturnValue(true);
    fireEvent.focus(window);
    expect(screen.getByTestId("focus-state")).toHaveTextContent("true");
  });

  it("syncs focus on visibility changes", () => {
    const hasFocus = vi.spyOn(document, "hasFocus").mockReturnValue(true);
    render(<FocusProbe />);

    hasFocus.mockReturnValue(false);
    fireEvent(document, new Event("visibilitychange"));

    expect(screen.getByTestId("focus-state")).toHaveTextContent("false");
  });
});
