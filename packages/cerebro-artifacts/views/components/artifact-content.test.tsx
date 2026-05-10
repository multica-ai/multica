import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import type { Artifact } from "@multica/core/types";

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

import { ArtifactContent } from "./artifact-content";

function artifact(overrides: Partial<Artifact>): Artifact {
  return {
    id: "art-1",
    workspace_id: "ws-1",
    project_id: null,
    issue_id: null,
    folder_id: null,
    origin_issue_id: null,
    kind: "report",
    format: "md",
    title: "Quarter Plan",
    body: "",
    file_url: null,
    file_size_bytes: null,
    metadata: {},
    author_type: "agent",
    author_id: "agent-1",
    requester_user_id: null,
    created_at: "2026-05-10T00:00:00Z",
    updated_at: "2026-05-10T00:00:00Z",
    ...overrides,
  };
}

describe("ArtifactContent", () => {
  it("strips a repeated markdown title before rendering", () => {
    render(
      <ArtifactContent
        artifact={artifact({
          body: "# Quarter Plan\n\nBody content",
        })}
      />,
    );

    expect(screen.getByTestId("md")).toHaveTextContent("Body content");
    expect(screen.getByTestId("md")).not.toHaveTextContent("# Quarter Plan");
  });

  it("isolates full HTML documents in a sandboxed iframe", () => {
    render(
      <ArtifactContent
        artifact={artifact({
          format: "html",
          body: "<!doctype html><html><body><h1>Report</h1></body></html>",
        })}
      />,
    );

    const frame = screen.getByTitle("Quarter Plan");
    expect(frame).toHaveAttribute("sandbox", "");
    expect(frame).toHaveAttribute(
      "srcdoc",
      expect.stringContaining("<!doctype html>"),
    );
  });

  it("renders pdf artifacts in an iframe with file metadata", () => {
    render(
      <ArtifactContent
        artifact={artifact({
          format: "pdf",
          file_url: "https://example.com/report.pdf",
          file_size_bytes: 2048,
        })}
      />,
    );

    expect(screen.getByText("PDF - 2.0 KB")).toBeInTheDocument();
    expect(screen.getByTitle("Quarter Plan")).toHaveAttribute(
      "src",
      "https://example.com/report.pdf",
    );
  });
});
