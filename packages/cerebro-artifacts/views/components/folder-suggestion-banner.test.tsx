// FIR-2697 part 2 — the pending folder-suggestion banner. An agent proposed a
// folder; a person accepts (moves) or dismisses (leaves in place). The banner
// renders nothing unless the flag is on AND a pending proposal exists.
import { describe, expect, it, vi, beforeEach } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import type { ArtifactFolderSuggestion } from "@multica/core/types";

const flagOn = vi.hoisted(() => ({ value: true }));
const suggestionRef = vi.hoisted(() => ({
  value: null as ArtifactFolderSuggestion | null,
}));
const acceptMutate = vi.hoisted(() => vi.fn());
const rejectMutate = vi.hoisted(() => vi.fn());

vi.mock("@multica/cerebro-feature-flags", () => ({
  useFeatureFlag: (flag: string) =>
    flag === "cerebro_folder_suggestions" ? flagOn.value : false,
}));

vi.mock("@multica/cerebro-artifacts/core", () => ({
  useFolderSuggestionForArtifact: () => ({ data: suggestionRef.value }),
  useAcceptFolderSuggestion: () => ({
    mutate: acceptMutate,
    isPending: false,
  }),
  useRejectFolderSuggestion: () => ({
    mutate: rejectMutate,
    isPending: false,
  }),
}));

import { FolderSuggestionBanner } from "./folder-suggestion-banner";

function suggestion(
  overrides: Partial<ArtifactFolderSuggestion> = {},
): ArtifactFolderSuggestion {
  return {
    id: "sug-1",
    workspace_id: "ws-1",
    artifact_id: "art-1",
    folder_id: "folder-1",
    folder_name: "Marketing plans",
    surface: "document",
    status: "pending",
    reason: "Fits the campaign docs",
    suggested_by_type: "agent",
    suggested_by_id: "agent-1",
    resolved_by_id: null,
    resolved_at: null,
    created_at: "",
    updated_at: "",
    ...overrides,
  };
}

describe("FolderSuggestionBanner", () => {
  beforeEach(() => {
    flagOn.value = true;
    suggestionRef.value = null;
    acceptMutate.mockReset();
    rejectMutate.mockReset();
  });

  it("shows the proposed folder and reason with accept/dismiss when the caller can resolve", () => {
    suggestionRef.value = suggestion();
    render(<FolderSuggestionBanner artifactId="art-1" canResolve />);

    expect(screen.getByText("Marketing plans")).toBeTruthy();
    expect(screen.getByText(/Fits the campaign docs/)).toBeTruthy();
    expect(screen.getByRole("button", { name: /Accept & move/ })).toBeTruthy();
    expect(screen.getByRole("button", { name: /Dismiss/ })).toBeTruthy();
  });

  it("accepts the proposal by its id", () => {
    suggestionRef.value = suggestion();
    render(<FolderSuggestionBanner artifactId="art-1" canResolve />);

    fireEvent.click(screen.getByRole("button", { name: /Accept & move/ }));
    expect(acceptMutate).toHaveBeenCalledWith({
      id: "sug-1",
      artifact_id: "art-1",
    });
  });

  it("renders nothing when there is no pending proposal", () => {
    suggestionRef.value = null;
    const { container } = render(
      <FolderSuggestionBanner artifactId="art-1" canResolve />,
    );
    expect(container.firstChild).toBeNull();
  });

  it("renders nothing when the feature flag is off", () => {
    flagOn.value = false;
    suggestionRef.value = suggestion();
    const { container } = render(
      <FolderSuggestionBanner artifactId="art-1" canResolve />,
    );
    expect(container.firstChild).toBeNull();
  });

  it("is read-only for a caller who cannot resolve", () => {
    suggestionRef.value = suggestion();
    render(<FolderSuggestionBanner artifactId="art-1" canResolve={false} />);

    expect(screen.queryByRole("button", { name: /Accept & move/ })).toBeNull();
    expect(screen.getByText(/Waiting for a teammate/)).toBeTruthy();
  });
});
