import type { ReactNode } from "react";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
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

  it("opens when Tab reaches a control whose text is cut off", async () => {
    const user = userEvent.setup();
    renderWithTooltips(
      <>
        <button type="button">Before</button>
        <Row label="A very long project name" />
      </>,
    );
    setBox(screen.getByTestId("label"), { scrollWidth: 240, clientWidth: 120 });
    screen.getByRole("button", { name: "Before" }).focus();

    await user.tab();
    expect(screen.getByRole("button", { name: "A very long project name" })).toHaveFocus();
    expect(tooltipPopup()).toHaveTextContent("A very long project name");
  });

  it("leaves focus under a hovering pointer to the hover delay", async () => {
    renderWithTooltips(<Row label="A very long project name" />);
    setBox(screen.getByTestId("label"), { scrollWidth: 240, clientWidth: 120 });
    const button = screen.getByRole("button");

    // A menu moves focus to the item under the pointer.
    vi.useFakeTimers();
    try {
      fireEvent.pointerEnter(button, { pointerType: "mouse" });
      await act(async () => {
        button.focus();
      });
      expect(tooltipPopup()).toBeNull();

      await act(async () => {
        vi.advanceTimersByTime(550);
      });
      expect(tooltipPopup()).toHaveTextContent("A very long project name");
    } finally {
      vi.useRealTimers();
    }
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

describe("TruncatedTooltip focusable", () => {
  function Panel() {
    return (
      <>
        <button type="button">Before</button>
        <TruncatedTooltip content="feature/very-long-branch-name" focusable>
          <span style={{ overflowX: "hidden" }}>feature/very-long-branch-name</span>
        </TruncatedTooltip>
        <TruncatedTooltip content="main" focusable>
          <span style={{ overflowX: "hidden" }}>main</span>
        </TruncatedTooltip>
        <button type="button">After</button>
      </>
    );
  }

  // jsdom never resizes, so the test re-runs the measurement once it has
  // given the text its widths.
  function renderPanel() {
    renderWithTooltips(<Panel />);
    setBox(screen.getByText("feature/very-long-branch-name"), { scrollWidth: 240, clientWidth: 120 });
    setBox(screen.getByText("main"), { scrollWidth: 30, clientWidth: 120 });
    act(() => triggerResizeObservers());
  }

  it("reaches cut-off text with Tab and shows the full text until focus leaves", async () => {
    const user = userEvent.setup();
    renderPanel();
    const text = screen.getByText("feature/very-long-branch-name");
    screen.getByRole("button", { name: "Before" }).focus();

    await user.tab();
    expect(text).toHaveFocus();
    expect(tooltipPopup()).toHaveTextContent("feature/very-long-branch-name");

    await user.tab();
    expect(screen.getByRole("button", { name: "After" })).toHaveFocus();
    await waitFor(() => expect(tooltipPopup()).toBeNull());
  });

  it("still opens for Tab and Shift+Tab while the pointer rests on the text", async () => {
    const user = userEvent.setup();
    renderPanel();
    const text = screen.getByText("feature/very-long-branch-name");
    fireEvent.pointerEnter(text, { pointerType: "mouse" });
    screen.getByRole("button", { name: "Before" }).focus();

    await user.tab();
    expect(text).toHaveFocus();
    expect(tooltipPopup()).toHaveTextContent("feature/very-long-branch-name");

    await user.tab();
    await waitFor(() => expect(tooltipPopup()).toBeNull());

    await user.tab({ shift: true });
    expect(text).toHaveFocus();
    expect(tooltipPopup()).toHaveTextContent("feature/very-long-branch-name");
  });

  it("keeps text that fits out of the tab order", async () => {
    const user = userEvent.setup();
    renderPanel();
    expect(screen.getByText("main")).not.toHaveAttribute("tabindex");

    screen.getByRole("button", { name: "Before" }).focus();
    await user.tab();
    await user.tab();
    expect(screen.getByRole("button", { name: "After" })).toHaveFocus();
  });

  it("joins the tab order once a resize cuts the text off, and leaves it when the text fits again", () => {
    renderPanel();
    const text = screen.getByText("main");
    expect(text).not.toHaveAttribute("tabindex");

    setBox(text, { scrollWidth: 30, clientWidth: 20 });
    act(() => triggerResizeObservers());
    expect(text).toHaveAttribute("tabindex", "0");

    setBox(text, { scrollWidth: 30, clientWidth: 120 });
    act(() => triggerResizeObservers());
    expect(text).not.toHaveAttribute("tabindex");
  });

  it("keeps the trigger's own ref and classes", () => {
    let node: HTMLElement | null = null;
    renderWithTooltips(
      <TruncatedTooltip content="Name" focusable>
        <span
          ref={(el) => {
            node = el;
          }}
          className="truncate rounded-md"
        >
          Name
        </span>
      </TruncatedTooltip>,
    );
    const text = screen.getByText("Name");
    expect(node).toBe(text);
    expect(text).toHaveClass("truncate", "rounded-md", "focus-visible:ring-3");
    expect(text).not.toHaveClass("rounded-sm");
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
