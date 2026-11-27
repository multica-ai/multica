import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { ComposerDock, JumpToLatestButton } from "./composer-dock";

describe("JumpToLatestButton", () => {
  it("is a round button named by its label, with no count when nothing is new", () => {
    const onClick = vi.fn();
    render(<JumpToLatestButton onClick={onClick} label="Jump to latest" />);

    const button = screen.getByRole("button", { name: "Jump to latest" });
    expect(button).toHaveClass("rounded-full");
    expect(button).not.toHaveTextContent("Jump to latest");
    expect(button.querySelector("[data-jump-count]")).toBeNull();

    fireEvent.click(button);
    expect(onClick).toHaveBeenCalledOnce();
  });

  it("counts the comments that arrived while away", () => {
    render(<JumpToLatestButton onClick={() => {}} label="3 new comments" count={3} />);

    const button = screen.getByRole("button", { name: "3 new comments" });
    expect(button.querySelector("[data-jump-count]")).toHaveTextContent(/^3$/);
  });

  it("caps the count at 9+", () => {
    render(<JumpToLatestButton onClick={() => {}} label="12 new comments" count={12} />);

    expect(
      screen.getByRole("button", { name: "12 new comments" }).querySelector("[data-jump-count]"),
    ).toHaveTextContent(/^9\+$/);
  });
});

describe("ComposerDock", () => {
  it("holds Jump to latest above the composer", () => {
    const { container } = render(
      <ComposerDock className="bottom-full" jump={<JumpToLatestButton key="jump" onClick={() => {}} label="Jump to latest" />} />,
    );

    const dock = container.querySelector("[data-composer-dock]");
    expect(dock).toHaveClass("bottom-full");
    expect(dock).toContainElement(screen.getByRole("button", { name: "Jump to latest" }));
  });
});
