import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Artifact } from "@multica/core/types";

vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({
    getActorName: (_t: string, id: string) => `Actor ${id.slice(0, 4)}`,
    getActorInitials: () => "AA",
    getActorAvatarUrl: () => null,
  }),
}));

import { ArtifactCard } from "./artifact-card";

const baseArtifact: Artifact = {
  id: "11111111-1111-1111-1111-111111111111",
  workspace_id: "ws-1",
  project_id: null,
  issue_id: "issue-1",
  folder_id: null,
  origin_issue_id: null,
  kind: "report",
  format: "md",
  title: "Investigation results",
  body:
    "# Findings\n\nLong body content here that should be excerpted in the card preview.",
  file_url: null,
  file_size_bytes: null,
  metadata: {},
  author_type: "member",
  author_id: "user-1",
  created_at: "2026-04-28T10:00:00Z",
  updated_at: "2026-04-28T10:00:00Z",
};

describe("ArtifactCard", () => {
  it("renders title, kind label, and excerpt", () => {
    render(<ArtifactCard artifact={baseArtifact} />);
    expect(screen.getByText("Investigation results")).toBeInTheDocument();
    expect(screen.getByText("Report")).toBeInTheDocument();
    expect(
      screen.getByText(/Findings.*Long body content/, { exact: false }),
    ).toBeInTheDocument();
  });

  it("invokes onClick with the artifact when clicked", async () => {
    const onClick = vi.fn();
    const user = userEvent.setup();
    render(<ArtifactCard artifact={baseArtifact} onClick={onClick} />);
    await user.click(screen.getByText("Investigation results"));
    expect(onClick).toHaveBeenCalledWith(baseArtifact);
  });

  it("shows agent badge when authored by an agent", () => {
    render(
      <ArtifactCard artifact={{ ...baseArtifact, author_type: "agent" }} />,
    );
    expect(screen.getByText("agent")).toBeInTheDocument();
  });
});
