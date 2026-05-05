import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";

vi.mock("@multica/views/common/markdown", () => ({
  Markdown: ({ children }: { children: string }) => (
    <div data-testid="md">{children}</div>
  ),
}));

vi.mock("./mermaid-diagram", () => ({
  MermaidDiagram: ({ code }: { code: string }) => (
    <div data-testid="mermaid">{code}</div>
  ),
}));

import { ArtifactBody } from "./artifact-body";

describe("ArtifactBody", () => {
  it("renders plain markdown when no mermaid block", () => {
    render(<ArtifactBody body={"# Hello\n\nWorld"} />);
    const md = screen.getAllByTestId("md");
    expect(md).toHaveLength(1);
    expect(md[0]).toHaveTextContent("# Hello");
    expect(screen.queryByTestId("mermaid")).toBeNull();
  });

  it("splits markdown around a mermaid fenced block", () => {
    const body = "Before\n\n```mermaid\nflowchart LR\n  A --> B\n```\n\nAfter";
    render(<ArtifactBody body={body} />);
    const md = screen.getAllByTestId("md");
    const mermaid = screen.getByTestId("mermaid");
    expect(md).toHaveLength(2);
    expect(md[0]).toHaveTextContent("Before");
    expect(md[1]).toHaveTextContent("After");
    expect(mermaid).toHaveTextContent(/flowchart LR/);
  });

  it("handles only-mermaid body", () => {
    render(<ArtifactBody body={"```mermaid\ngraph TD\n  X --> Y\n```"} />);
    expect(screen.getByTestId("mermaid")).toHaveTextContent("graph TD");
  });
});
