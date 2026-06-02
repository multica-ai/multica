import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { Markdown } from "./markdown";

vi.mock("@multica/core/config", () => ({
  useConfigStore: (selector: (state: { cdnDomain: string }) => unknown) =>
    selector({ cdnDomain: "" }),
}));

vi.mock("../issues/components/issue-mention-card", () => ({
  IssueMentionCard: ({ issueId }: { issueId: string }) => (
    <span data-testid="issue-mention-card">{issueId}</span>
  ),
}));

const ligatureClasses = [
  "[font-variant-ligatures:none]",
  "[font-feature-settings:'liga'_0]",
];

describe("Markdown", () => {
  it("disables ligatures inside raw code tags", () => {
    render(<Markdown>{"<code>uv run --extra dev pytest -q</code>"}</Markdown>);

    expect(screen.getByText("uv run --extra dev pytest -q")).toHaveClass(...ligatureClasses);
  });

  it("disables ligatures inside fenced code blocks", () => {
    render(<Markdown>{"```sh\nuv run --extra dev pytest -q\n```"}</Markdown>);

    expect(screen.getByText("uv run --extra dev pytest -q")).toHaveClass(...ligatureClasses);
  });

  it("disables ligatures in terminal-mode code", () => {
    render(<Markdown mode="terminal">{"<code>uv run --extra dev pytest -q</code>"}</Markdown>);

    expect(screen.getByText("uv run --extra dev pytest -q")).toHaveClass(...ligatureClasses);
  });

  it("renders slash skill links as slash command pills", () => {
    const { container } = render(
      <Markdown>[/deploy](slash://skill/abc-123)</Markdown>,
    );

    const pill = container.querySelector(".slash-command");
    expect(pill).not.toBeNull();
    expect(pill?.textContent).toBe("/deploy");
  });
});
