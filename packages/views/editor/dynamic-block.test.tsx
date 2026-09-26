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

  it("has no fullscreen button unless the kind offers one", () => {
    renderBlock();
    expect(screen.queryByRole("button", { name: "Fullscreen" })).toBeNull();
  });
});
