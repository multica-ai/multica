import * as React from "react";
import { describe, expect, it, beforeEach, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Artifact } from "@multica/core/types";

const updateArtifactMutateAsync = vi.hoisted(() =>
  vi.fn().mockResolvedValue(undefined),
);
const deleteArtifactMutateAsync = vi.hoisted(() =>
  vi.fn().mockResolvedValue(undefined),
);
const navigationPush = vi.hoisted(() => vi.fn());
const currentMember = vi.hoisted(() => ({
  userId: "user-1" as string | null,
  role: "member" as string | null,
}));

vi.mock("@multica/cerebro-artifacts/core", () => ({
  artifactDetailOptions: (_wsId: string, id: string) => ({
    queryKey: ["artifacts", "ws-1", "detail", id],
    queryFn: () => Promise.reject(new Error("artifact query should be seeded")),
  }),
  useDeleteArtifact: () => ({
    mutateAsync: deleteArtifactMutateAsync,
    isPending: false,
  }),
  useUpdateArtifact: () => ({
    mutateAsync: updateArtifactMutateAsync,
    isPending: false,
  }),
  parseOutline: () => [],
  countDocument: () => ({ words: 0, characters: 0 }),
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/permissions", () => ({
  useCurrentMember: () => currentMember,
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
  useNavigation: () => ({
    push: navigationPush,
  }),
}));

vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({
    getActorName: () => "Alice",
    getMemberName: () => "Alice",
  }),
}));

vi.mock("@multica/views/layout/page-header", () => ({
  MobileSidebarTrigger: () => <button type="button" aria-label="sidebar" />,
}));

vi.mock("@multica/views/common/actor-avatar", () => ({
  ActorAvatar: () => null,
}));

vi.mock("../components/move-scope-menu", () => ({
  MoveScopeMenu: () => <button type="button">Move</button>,
}));

vi.mock("../components/artifact-content", () => ({
  ArtifactContent: () => <div data-testid="readonly-artifact-content" />,
}));

vi.mock("@multica/views/editor", () => ({
  ContentEditor: React.forwardRef(
    (
      props: {
        defaultValue?: string;
        onUpdate?: (markdown: string) => void;
      },
      ref: React.ForwardedRef<{ getMarkdown: () => string }>,
    ) => {
      const [value, setValue] = React.useState(props.defaultValue ?? "");
      React.useImperativeHandle(ref, () => ({
        getMarkdown: () => value,
      }));
      return (
        <textarea
          aria-label="Markdown editor"
          value={value}
          onChange={(event) => {
            setValue(event.target.value);
            props.onUpdate?.(event.target.value);
          }}
        />
      );
    },
  ),
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
    kind: "note",
    format: "md",
    title: "Readme",
    body: "# Readme\n\nInitial body",
    file_url: null,
    file_size_bytes: null,
    metadata: {},
    author_type: "member",
    author_id: "user-1",
    requester_user_id: null,
    created_at: "2026-06-01T00:00:00Z",
    updated_at: "2026-06-01T00:00:00Z",
    ...overrides,
  };
}

function renderPage(item: Artifact) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  qc.setQueryData(["artifacts", "ws-1", "detail", item.id], item);
  return render(
    <QueryClientProvider client={qc}>
      <DocumentViewPage artifactId={item.id} />
    </QueryClientProvider>,
  );
}

describe("DocumentViewPage markdown body", () => {
  beforeEach(() => {
    updateArtifactMutateAsync.mockClear();
    deleteArtifactMutateAsync.mockClear();
    navigationPush.mockClear();
    currentMember.userId = "user-1";
    currentMember.role = "member";
  });

  it("renders owned markdown documents as an inline editor and autosaves body changes", async () => {
    renderPage(artifact());

    expect(screen.getByLabelText("Markdown editor")).toHaveValue(
      "# Readme\n\nInitial body",
    );
    expect(screen.queryByTestId("readonly-artifact-content")).toBeNull();
    expect(
      screen.queryByText("Edit body"),
    ).not.toBeInTheDocument();
    // Google-Docs style: no Save button — editing autosaves.
    expect(screen.queryByText("Save")).not.toBeInTheDocument();

    fireEvent.change(screen.getByLabelText("Markdown editor"), {
      target: { value: "# Readme\n\nChanged body" },
    });

    await waitFor(() =>
      expect(updateArtifactMutateAsync).toHaveBeenCalledWith({
        id: "art-1",
        data: { body: "# Readme\n\nChanged body" },
      }),
    );
  });

  it("lets workspace admins edit agent-authored markdown documents", () => {
    currentMember.userId = "admin-1";
    currentMember.role = "admin";

    renderPage(
      artifact({
        author_type: "agent",
        author_id: "agent-1",
      }),
    );

    expect(screen.getByLabelText("Markdown editor")).toBeInTheDocument();
  });
});
