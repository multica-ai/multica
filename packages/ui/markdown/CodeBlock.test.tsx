import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { CodeBlock } from "./CodeBlock";

vi.mock("react-i18next", () => ({
  useTranslation: () => ({
    t: () => "Plain text",
  }),
}));

describe("CodeBlock", () => {
  afterEach(() => {
    cleanup();
  });

  it("keeps full-mode code content manually selectable", () => {
    const { container } = render(
      <CodeBlock code="const value = 1;" language="tsx" mode="full" />,
    );

    const wrapper = container.firstElementChild;
    const body = screen.getByText("const value = 1;").closest("div");

    expect(wrapper?.className).toContain("select-text");
    expect(body?.className).toContain("select-text");
  });

  it("prevents the copy button mousedown from clearing a manual selection", () => {
    render(<CodeBlock code="const value = 1;" language="tsx" mode="full" />);

    const copyButton = screen.getByRole("button", { name: "Plain text" });
    const mouseDown = fireEvent.mouseDown(copyButton);

    expect(mouseDown).toBe(false);
  });

  it("keeps terminal and minimal modes selectable before highlighting finishes", () => {
    const { rerender, container } = render(
      <CodeBlock code="pnpm test" language="bash" mode="terminal" />,
    );

    expect(container.querySelector("pre")?.className).toContain("select-text");

    rerender(<CodeBlock code="pnpm test" language="bash" mode="minimal" />);
    expect(container.querySelector("pre")?.className).toContain("select-text");
  });
});
