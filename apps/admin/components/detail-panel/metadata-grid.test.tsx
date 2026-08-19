// @vitest-environment jsdom
import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { MetadataGrid } from "./metadata-grid";
import type { WorkspaceMetadata } from "@/lib/types";

function makeMetadata(overrides: Partial<WorkspaceMetadata> = {}): WorkspaceMetadata {
  return {
    id: "ws-1",
    slug: "acme",
    createdAt: "2026-01-01T00:00:00.000Z",
    owner: "Jane",
    model: "claude-opus-5",
    root: null,
    gitRemote: null,
    ...overrides,
  };
}

describe("MetadataGrid", () => {
  it("renders an http(s) git remote as a clickable link", () => {
    render(<MetadataGrid metadata={makeMetadata({ gitRemote: "https://github.com/g2crowd/agentfarm" })} />);
    const link = screen.getByRole("link", { name: "https://github.com/g2crowd/agentfarm" });
    expect(link).toHaveAttribute("href", "https://github.com/g2crowd/agentfarm");
  });

  it("renders a non-http(s) git remote as plain text, not a link", () => {
    render(<MetadataGrid metadata={makeMetadata({ gitRemote: "javascript:alert(1)" })} />);
    expect(screen.getByText("javascript:alert(1)")).toBeInTheDocument();
    expect(screen.queryByRole("link")).not.toBeInTheDocument();
  });

  it("renders an SSH-style remote as plain text, not a link", () => {
    render(<MetadataGrid metadata={makeMetadata({ gitRemote: "git@github.com:g2crowd/agentfarm.git" })} />);
    expect(screen.getByText("git@github.com:g2crowd/agentfarm.git")).toBeInTheDocument();
    expect(screen.queryByRole("link")).not.toBeInTheDocument();
  });
});
