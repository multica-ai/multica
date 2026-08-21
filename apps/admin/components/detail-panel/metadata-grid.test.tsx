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
    repoCount: 0,
    ...overrides,
  };
}

describe("MetadataGrid", () => {
  it("renders 'No repos connected' when the workspace has no repos", () => {
    render(<MetadataGrid metadata={makeMetadata({ repoCount: 0 })} />);
    expect(screen.getByText("No repos connected")).toBeInTheDocument();
  });

  it("renders singular phrasing for exactly one repo", () => {
    render(<MetadataGrid metadata={makeMetadata({ repoCount: 1 })} />);
    expect(screen.getByText("1 repo connected")).toBeInTheDocument();
  });

  it("renders plural phrasing for multiple repos", () => {
    render(<MetadataGrid metadata={makeMetadata({ repoCount: 3 })} />);
    expect(screen.getByText("3 repos connected")).toBeInTheDocument();
  });
});
