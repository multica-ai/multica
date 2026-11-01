import { afterEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";

vi.mock("../i18n", async () => {
  const editor = (await import("../locales/en/editor.json")).default;
  return {
    useT: () => ({ t: (select: (bundle: typeof editor) => string) => select(editor) }),
  };
});

vi.mock("./code-block-static", () => ({
  CodeBlockStatic: ({ body }: { body: string }) => <pre>{body}</pre>,
}));

const { copyTextMock } = vi.hoisted(() => ({ copyTextMock: vi.fn() }));
vi.mock("@multica/ui/lib/clipboard", () => ({ copyText: copyTextMock }));

import { DynamicBlock } from "./dynamic-block";

afterEach(() => vi.restoreAllMocks());

function renderBlock() {
  return render(
    <DynamicBlock
      kind="html"
      title="Latency"
      source="<p>chart</p>"
      preview={() => <div data-testid="content">chart</div>}
    />,
  );
}

describe("DynamicBlock", () => {
  it("collapses content taller than 480px behind Show all", () => {
    vi.spyOn(HTMLElement.prototype, "offsetHeight", "get").mockReturnValue(900);
    const { container } = renderBlock();

    const body = container.querySelector<HTMLElement>("[data-collapsed]");
    expect(body).not.toBeNull();
    expect(body!.style.maxHeight).toBe("480px");

    fireEvent.click(screen.getByRole("button", { name: "Show all" }));
    expect(container.querySelector("[data-collapsed]")).toBeNull();
    expect(screen.queryByRole("button", { name: "Show all" })).toBeNull();
  });

  it("leaves content that fits alone", () => {
    vi.spyOn(HTMLElement.prototype, "offsetHeight", "get").mockReturnValue(300);
    const { container } = renderBlock();
    expect(container.querySelector("[data-collapsed]")).toBeNull();
    expect(screen.queryByRole("button", { name: "Show all" })).toBeNull();
  });

  it("copies the fence source", async () => {
    copyTextMock.mockResolvedValue(true);
    renderBlock();
    fireEvent.click(screen.getByRole("button", { name: "Copy source" }));
    await waitFor(() => expect(copyTextMock).toHaveBeenCalledWith("<p>chart</p>"));
  });

  it("reveals the actions on hover, but keeps them up in the source view and after a copy", async () => {
    copyTextMock.mockResolvedValue(true);
    const { container } = renderBlock();
    const actions = container.querySelector<HTMLElement>("[data-dynamic-block-actions]")!;
    const hiddenUntilHover = "[@media(hover:hover)]:opacity-0";

    // The title always shows; only the actions wait for hover or focus.
    expect(actions.textContent).not.toContain("Latency");
    expect(actions.className).toContain(hiddenUntilHover);
    expect(actions.className).toContain("group-hover/dynamic-block:opacity-100");
    expect(actions.className).toContain("group-focus-within/dynamic-block:opacity-100");

    // Preview is the way back from the source view, so it cannot hide.
    fireEvent.click(screen.getByRole("tab", { name: "Source" }));
    await waitFor(() => expect(actions.className).not.toContain(hiddenUntilHover));
    fireEvent.click(screen.getByRole("tab", { name: "Preview" }));
    await waitFor(() => expect(actions.className).toContain(hiddenUntilHover));

    // The copy confirmation stays readable after the pointer leaves.
    fireEvent.click(screen.getByRole("button", { name: "Copy source" }));
    await waitFor(() => expect(actions.className).not.toContain(hiddenUntilHover));
  });

  it("has no fullscreen button unless the kind offers one", () => {
    renderBlock();
    expect(screen.queryByRole("button", { name: "Fullscreen" })).toBeNull();
  });

  // Regression: a reported bodyHeight must decide the collapse in the same
  // render it arrives. The clamp used to come from a measure-after-paint
  // observer, so a tall document painted unclamped for a frame and then
  // folded back — the height shock virtualized timelines render as flicker.
  it("decides the collapse from a reported bodyHeight in the same render", () => {
    const { container } = render(
      <DynamicBlock
        kind="html"
        title="Latency"
        source="<p>chart</p>"
        bodyHeight={3000}
        preview={({ collapsed }) => (
          <div data-testid="content" style={{ height: collapsed ? 480 : 3000 }}>
            chart
          </div>
        )}
      />,
    );

    const body = container.querySelector<HTMLElement>("[data-collapsed]");
    expect(body).not.toBeNull();
    expect(body!.style.maxHeight).toBe("480px");
    // The preview laid out at the clamp, not at the full height.
    expect(screen.getByTestId("content").style.height).toBe("480px");
  });

  // Regression: enlarging must not lift the inline clamp. The enlarged dialog
  // is fixed-position and fills itself, but the in-place footprint has to keep
  // holding the clamp — dropping the clip for the enlargement re-expanded the
  // timeline for as long as the dialog was open, the same height shock this
  // frame exists to prevent.
  it("keeps the inline clamp while fullscreen", () => {
    const { container } = render(
      <DynamicBlock
        kind="html"
        title="Latency"
        source="<p>chart</p>"
        fullscreen
        bodyHeight={3000}
        preview={({ collapsed }) => (
          <div data-testid="content" style={{ height: collapsed ? 480 : 3000 }}>
            chart
          </div>
        )}
      />,
    );

    const body = container.querySelector<HTMLElement>("[data-collapsed]");
    expect(body).not.toBeNull();
    expect(body!.style.maxHeight).toBe("480px");
    // The in-place footprint stays at the clamp, never at the full height.
    expect(screen.getByTestId("content").style.height).toBe("480px");
    expect(screen.getByRole("button", { name: "Show all" })).toBeTruthy();
  });
});
