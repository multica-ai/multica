import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";

// ArtifactBody delegates to the shared ReadonlyContent renderer (the same
// pipeline notes / descriptions / comments use). Stub it so the test asserts
// the contract — the raw body is handed through verbatim — without spinning up
// the full react-markdown + mermaid + lightbox machinery.
vi.mock("@multica/views/editor", () => ({
  ReadonlyContent: ({ content }: { content: string }) => (
    <div data-testid="readonly-content">{content}</div>
  ),
}));

import { ArtifactBody } from "./artifact-body";

describe("ArtifactBody", () => {
  it("passes the markdown body through to ReadonlyContent", () => {
    render(<ArtifactBody body={"# Hello\n\nWorld"} />);
    const content = screen.getByTestId("readonly-content");
    expect(content).toHaveTextContent("# Hello");
    expect(content).toHaveTextContent("World");
  });

  it("hands mermaid fenced blocks through untouched (ReadonlyContent owns rendering)", () => {
    const body = "Before\n\n```mermaid\nflowchart LR\n  A --> B\n```\n\nAfter";
    render(<ArtifactBody body={body} />);
    const content = screen.getByTestId("readonly-content");
    expect(content).toHaveTextContent(/flowchart LR/);
    expect(content).toHaveTextContent("Before");
    expect(content).toHaveTextContent("After");
  });
});
