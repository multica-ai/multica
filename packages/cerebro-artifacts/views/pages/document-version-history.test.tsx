// FIR-2697 — the Documents editor exposes a "Version history" action (gated by
// the cerebro_document_versions flag) that opens the injected versions slot.
import * as React from "react";
import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Artifact } from "@multica/core/types";

const navigationPush = vi.hoisted(() => vi.fn());

vi.mock("@multica/cerebro-artifacts/core", () => ({
  artifactDetailOptions: (_wsId: string, id: string) => ({
    queryKey: ["artifacts", "ws-1", "detail", id],
    queryFn: () => Promise.reject(new Error("seed the artifact")),
  }),
  useDeleteArtifact: () => ({ mutateAsync: vi.fn(), isPending: false }),
  useUpdateArtifact: () => ({ mutateAsync: vi.fn(), isPending: false }),
  useFolderSuggestionForArtifact: () => ({ data: null }),
  useAcceptFolderSuggestion: () => ({ mutate: vi.fn(), isPending: false }),
  useRejectFolderSuggestion: () => ({ mutate: vi.fn(), isPending: false }),
  parseOutline: () => [],
  countDocument: () => ({ words: 0, characters: 0 }),
  countMatches: () => 0,
  replaceAll: (body: string) => ({ body, count: 0 }),
  replaceFirst: (body: string) => ({ body, replaced: false }),
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/permissions", () => ({
  useCurrentMember: () => ({ userId: "user-1", role: "member" }),
}));
vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    documents: () => "/ws/documents",
    documentDetail: (id: string) => `/ws/documents/${id}`,
    documentEdit: (id: string) => `/ws/documents/${id}/edit`,
    issueDetail: (id: string) => `/ws/issues/${id}`,
    projectDetail: (id: string) => `/ws/projects/${id}`,
  }),
}));
vi.mock("@multica/views/navigation", () => ({
  useNavigation: () => ({ push: navigationPush }),
}));
vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({ getActorName: () => "Alice", getMemberName: () => "Alice" }),
}));
vi.mock("@multica/views/layout/page-header", () => ({
  MobileSidebarTrigger: () => <button type="button" aria-label="sidebar" />,
}));
vi.mock("@multica/views/common/actor-avatar", () => ({ ActorAvatar: () => null }));
vi.mock("../components/artifact-content", () => ({
  ArtifactContent: () => <div data-testid="readonly-artifact-content" />,
}));
vi.mock("@multica/views/editor", () => ({
  ContentEditor: React.forwardRef(() => <textarea aria-label="Markdown editor" />),
}));

// The version-history action is gated on this flag; force it on.
vi.mock("@multica/cerebro-feature-flags", () => ({
  useFeatureFlag: (flag: string) => flag === "cerebro_document_versions",
}));

import { DocumentViewPage } from "./document-view-page";

function artifact(overrides: Partial<Artifact> = {}): Artifact {
  return {
    id: "art-1",
    workspace_id: "ws-1",
    project_id: null,
    issue_id: null,
    folder_id: null,
    origin_issue_id: null,
    kind: "report",
    format: "md",
    title: "Report",
    body: "# Report",
    file_url: null,
    file_size_bytes: null,
    metadata: {},
    author_type: "agent",
    author_id: "agent-1",
    requester_user_id: null,
    created_at: "2026-06-01T00:00:00Z",
    updated_at: "2026-06-01T00:00:00Z",
    ...overrides,
  };
}

function renderPage(renderVersions: (o: { artifactId: string; open: boolean; onOpenChange: (o: boolean) => void }) => React.ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const item = artifact();
  qc.setQueryData(["artifacts", "ws-1", "detail", item.id], item);
  return render(
    <QueryClientProvider client={qc}>
      <DocumentViewPage artifactId={item.id} renderVersions={renderVersions} />
    </QueryClientProvider>,
  );
}

describe("DocumentViewPage version history", () => {
  it("opens the versions slot from the actions menu when the flag is on", () => {
    const slot = vi.fn(({ open }) =>
      open ? <div data-testid="versions-open" /> : null,
    );
    renderPage(slot);

    // Slot is mounted but closed initially.
    expect(screen.queryByTestId("versions-open")).toBeNull();

    // Open the "⋯" actions menu, then click Version history.
    fireEvent.click(screen.getByLabelText("Document actions"));
    fireEvent.click(screen.getByText("Version history"));

    expect(screen.getByTestId("versions-open")).toBeInTheDocument();
    expect(slot).toHaveBeenLastCalledWith(
      expect.objectContaining({ artifactId: "art-1", open: true }),
    );
  });
});
