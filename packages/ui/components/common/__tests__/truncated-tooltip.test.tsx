import type { ReactNode } from "react";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { isContentTruncated, TruncatedTooltip } from "../truncated-tooltip";
import { TooltipProvider } from "../../ui/tooltip";

afterEach(() => {
  cleanup();
  document.body.innerHTML = "";
});

function renderWithTooltips(ui: ReactNode) {
  return render(<TooltipProvider delay={0}>{ui}</TooltipProvider>);
}

// Hover and let the open delay elapse.
async function hover(element: HTMLElement) {
  vi.useFakeTimers();
  try {
    fireEvent.pointerEnter(element, { pointerType: "mouse" });
    await act(async () => {
      vi.advanceTimersByTime(550);
    });
  } finally {
    vi.useRealTimers();
  }
}

// jsdom has no layout, so give an element the box sizes a browser would.
function setBox(
  el: Element,
  box: { scrollWidth: number; clientWidth: number; scrollHeight?: number; clientHeight?: number },
) {
  const { scrollWidth, clientWidth, scrollHeight = 20, clientHeight = 20 } = box;
  Object.defineProperties(el, {
    scrollWidth: { configurable: true, value: scrollWidth },
    clientWidth: { configurable: true, value: clientWidth },
    scrollHeight: { configurable: true, value: scrollHeight },
    clientHeight: { configurable: true, value: clientHeight },
  });
}

const tooltipPopup = () => document.querySelector("[data-slot='tooltip-content']");

function Row({ label }: { label: string }) {
  return (
    <TruncatedTooltip content={label}>
      <button type="button">
        <span data-testid="label" style={{ overflowX: "hidden", overflowY: "hidden" }}>
          {label}
        </span>
      </button>
    </TruncatedTooltip>
  );
}

describe("TruncatedTooltip", () => {
  it("shows the full text when the label is cut off", async () => {
    renderWithTooltips(<Row label="A very long project name" />);
    setBox(screen.getByTestId("label"), { scrollWidth: 240, clientWidth: 120 });

    await hover(screen.getByRole("button"));
    expect(tooltipPopup()).toHaveTextContent("A very long project name");
  });

  it("stays closed when the label fits", async () => {
    renderWithTooltips(<Row label="Short" />);
    setBox(screen.getByTestId("label"), { scrollWidth: 40, clientWidth: 120 });

    await hover(screen.getByRole("button"));
    expect(tooltipPopup()).toBeNull();
  });

  it("opens on keyboard focus when truncated and closes on blur", async () => {
    renderWithTooltips(<Row label="A very long project name" />);
    setBox(screen.getByTestId("label"), { scrollWidth: 240, clientWidth: 120 });
    const button = screen.getByRole("button");

    await act(async () => {
      button.focus();
    });
    expect(tooltipPopup()).toHaveTextContent("A very long project name");

    await act(async () => {
      button.blur();
    });
    await waitFor(() => expect(tooltipPopup()).toBeNull());
  });

  it("stays closed on keyboard focus when the label fits", async () => {
    renderWithTooltips(<Row label="Short" />);
    setBox(screen.getByTestId("label"), { scrollWidth: 40, clientWidth: 120 });

    await act(async () => {
      screen.getByRole("button").focus();
    });
    expect(tooltipPopup()).toBeNull();
  });

  it("does not open from the focus a pointer press causes", async () => {
    renderWithTooltips(<Row label="A very long project name" />);
    setBox(screen.getByTestId("label"), { scrollWidth: 240, clientWidth: 120 });
    const button = screen.getByRole("button");

    await act(async () => {
      fireEvent.pointerDown(button, { pointerType: "mouse" });
      button.focus();
    });
    expect(tooltipPopup()).toBeNull();
  });

  it("lets a long unbroken value wrap inside the bubble", async () => {
    const hash = "a1b2c3d4".repeat(8);
    renderWithTooltips(<Row label={hash} />);
    setBox(screen.getByTestId("label"), { scrollWidth: 480, clientWidth: 120 });

    await hover(screen.getByRole("button"));
    expect(tooltipPopup()).toHaveTextContent(hash);
    expect(tooltipPopup()).toHaveClass("[overflow-wrap:anywhere]");
  });

  it("does not mount tooltip machinery before the first hover", () => {
    renderWithTooltips(<Row label="A very long project name" />);
    expect(screen.getByRole("button")).not.toHaveAttribute("data-popup-open");
    expect(document.querySelector("[data-slot='tooltip-content']")).toBeNull();
  });

  it("renders the child alone without content", () => {
    renderWithTooltips(
      <TruncatedTooltip content="">
        <button type="button">Plain</button>
      </TruncatedTooltip>,
    );
    expect(screen.getByRole("button", { name: "Plain" })).not.toHaveAttribute("data-popup-open");
  });
});

describe("isContentTruncated", () => {
  it("ignores overflow that is not clipped", () => {
    const el = document.createElement("div");
    document.body.appendChild(el);
    const icon = document.createElement("span");
    el.appendChild(icon);
    setBox(icon, { scrollWidth: 18, clientWidth: 14 });
    expect(isContentTruncated(el)).toBe(false);
  });

  it("detects vertical clipping from line-clamp", () => {
    const el = document.createElement("div");
    document.body.appendChild(el);
    // jsdom does not expand the `overflow` shorthand into its longhands.
    el.style.overflowX = "hidden";
    el.style.overflowY = "hidden";
    setBox(el, { scrollWidth: 100, clientWidth: 100, scrollHeight: 60, clientHeight: 40 });
    expect(isContentTruncated(el)).toBe(true);
  });
});
