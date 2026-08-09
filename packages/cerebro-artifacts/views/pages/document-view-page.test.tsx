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
const featureFlags = vi.hoisted(() => ({ editorToolbar: false }));

vi.mock("@multica/cerebro-feature-flags", () => ({
  useFeatureFlag: (key: string) =>
    key === "cerebro_editor_toolbar" ? featureFlags.editorToolbar : false,
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
  useFolderSuggestionForArtifact: () => ({ data: null }),
  useAcceptFolderSuggestion: () => ({ mutate: vi.fn(), isPending: false }),
  useRejectFolderSuggestion: () => ({ mutate: vi.fn(), isPending: false }),
  parseOutline: () => [],
  countDocument: () => ({ words: 0, characters: 0 }),
  countMatches: () => 0,
  replaceAll: (body: string) => ({ body, count: 0 }),
  replaceFirst: (body: string) => ({ body, replaced: false }),
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

vi.mock("../components/artifact-content", () => ({
  ArtifactContent: ({ className }: { className?: string }) => (
    <div data-testid="readonly-artifact-content" data-classname={className} />
  ),
}));

vi.mock("@multica/views/editor", () => ({
  ContentEditor: React.forwardRef(
    (
      props: {
        defaultValue?: string;
        onUpdate?: (markdown: string) => void;
        onEditorReady?: (editor: unknown) => void;
        showBubbleMenu?: boolean;
      },
      ref: React.ForwardedRef<{ getMarkdown: () => string }>,
    ) => {
      const [value, setValue] = React.useState(props.defaultValue ?? "");
      React.useEffect(() => {
        props.onEditorReady?.({ state: { selection: { empty: true } } });
        // The real ContentEditor reports its stable editor instance once.
        // eslint-disable-next-line react-hooks/exhaustive-deps
      }, []);
      React.useImperativeHandle(ref, () => ({
        getMarkdown: () => value,
      }));
      return (
        <>
          <span data-testid="bubble-menu-setting">
            {String(props.showBubbleMenu)}
          </span>
          <textarea
            aria-label="Markdown editor"
            value={value}
            onChange={(event) => {
              setValue(event.target.value);
              props.onUpdate?.(event.target.value);
            }}
          />
        </>
      );
    },
  ),
}));

vi.mock("@multica/cerebro-ui", () => ({
  EditorFormattingToolbar: ({ editor }: { editor: unknown }) => (
    <div role="toolbar" aria-label="Formatting toolbar">
      {editor ? "Ready" : "Loading"}
    </div>
  ),
  // FIR-4028 slice 8 — the right-click wrapper is a Base UI context menu; here
  // it only has to render the editor it wraps.
  EditorContextMenu: ({ children }: { children: React.ReactNode }) => (
    <div data-testid="editor-context-menu">{children}</div>
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
    featureFlags.editorToolbar = false;
  });

  it("keeps selection formatting available when the editor toolbar flag is off", async () => {
    renderPage(artifact());

    expect(await screen.findByTestId("bubble-menu-setting")).toHaveTextContent("true");
    expect(screen.queryByRole("toolbar", { name: "Formatting toolbar" })).not.toBeInTheDocument();
  });

  it("renders owned markdown documents as an inline editor and autosaves body changes", async () => {
    featureFlags.editorToolbar = true;
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
    expect(
      screen.getByRole("toolbar", { name: "Formatting toolbar" }),
    ).toHaveTextContent("Ready");
    expect(screen.getByTestId("bubble-menu-setting")).toHaveTextContent("false");

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

  // FIR-3778 — an agent that writes a document for a person stays its author,
  // so the person it was written for used to get a read-only view with no edit
  // affordance at all. requester_user_id names that person.
  it("lets the person an agent wrote the document for edit it", () => {
    currentMember.userId = "user-1";
    currentMember.role = "member";

    renderPage(
      artifact({
        author_type: "agent",
        author_id: "agent-1",
        requester_user_id: "user-1",
      }),
    );

    expect(screen.getByLabelText("Markdown editor")).toBeInTheDocument();
    expect(screen.queryByTestId("readonly-artifact-content")).toBeNull();
  });

  it("keeps an unrelated member out of a document an agent wrote for someone else", () => {
    currentMember.userId = "user-2";
    currentMember.role = "member";

    renderPage(
      artifact({
        author_type: "agent",
        author_id: "agent-1",
        requester_user_id: "user-1",
      }),
    );

    expect(screen.queryByLabelText("Markdown editor")).toBeNull();
  });

  // Guard against the null trap: a signed-out reader (userId === null) must not
  // match a document whose requester_user_id is also null.
  it("does not treat a signed-out reader as the requester of an unrequested document", () => {
    currentMember.userId = null;
    currentMember.role = null;

    renderPage(
      artifact({
        author_type: "agent",
        author_id: "agent-1",
        requester_user_id: null,
      }),
    );

    expect(screen.queryByLabelText("Markdown editor")).toBeNull();
  });

  // FIR-3190 — the readonly path was missing the same 70ch-cap override the
  // editable path applies, squeezing wide tables (e.g. AI CFO reports) into a
  // narrow column and shredding cell text mid-word.
  it("lifts the 70ch readability cap on readonly document bodies so wide tables can use the full width", () => {
    renderPage(artifact({ format: "pdf" }));

    expect(
      screen.getByTestId("readonly-artifact-content"),
    ).toHaveAttribute("data-classname", "!max-w-none");
  });
});
