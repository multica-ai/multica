import { describe, it, expect, vi, beforeEach } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Artifact, ArtifactFolder } from "@multica/core/types";

// ---------- Hoisted mocks ----------

const moveArtifactMutate = vi.hoisted(() => vi.fn());
const updateFolderMutate = vi.hoisted(() => vi.fn());
const updateFolderMutateAsync = vi.hoisted(() =>
  vi.fn().mockResolvedValue(undefined),
);
const deleteFolderMutateAsync = vi.hoisted(() =>
  vi.fn().mockResolvedValue(undefined),
);
const createFolderMutateAsync = vi.hoisted(() =>
  vi.fn().mockResolvedValue(undefined),
);
const deleteArtifactMutateAsync = vi.hoisted(() =>
  vi.fn().mockResolvedValue(undefined),
);
const updateArtifactMutateAsync = vi.hoisted(() =>
  vi.fn().mockResolvedValue(undefined),
);

vi.mock("@multica/cerebro-artifacts/core/mutations", () => ({
  useCreateArtifactFolder: () => ({
    mutateAsync: createFolderMutateAsync,
    isPending: false,
  }),
  useUpdateArtifactFolder: () => ({
    mutate: updateFolderMutate,
    mutateAsync: updateFolderMutateAsync,
    isPending: false,
  }),
  useDeleteArtifactFolder: () => ({
    mutateAsync: deleteFolderMutateAsync,
    isPending: false,
  }),
  useMoveArtifactToFolder: () => ({
    mutate: moveArtifactMutate,
    isPending: false,
  }),
  useDeleteArtifact: () => ({
    mutateAsync: deleteArtifactMutateAsync,
    isPending: false,
  }),
  useUpdateArtifact: () => ({
    mutateAsync: updateArtifactMutateAsync,
    isPending: false,
  }),
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    documentDetail: (id: string) => `/ws/documents/${id}`,
    documentEdit: (id: string) => `/ws/documents/${id}/edit`,
    documentNew: () => "/ws/documents/new",
    issueDetail: (id: string) => `/ws/issues/${id}`,
    projectDetail: (id: string) => `/ws/projects/${id}`,
  }),
}));

vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({
    getActorName: (_t: string, id: string) => `Actor ${id.slice(0, 4)}`,
    getMemberName: (id: string) => `Member ${id.slice(0, 4)}`,
    getActorInitials: () => "AA",
    getActorAvatarUrl: () => null,
  }),
}));

vi.mock("@multica/views/navigation", () => ({
  useNavigation: () => ({
    push: vi.fn(),
    openInNewTab: vi.fn(),
  }),
  AppLink: ({ children }: { children: React.ReactNode }) => children,
}));

vi.mock("@multica/ui/components/common/actor-avatar", () => ({
  ActorAvatar: () => null,
}));

const folders: ArtifactFolder[] = [
  {
    id: "folder-a",
    workspace_id: "ws-1",
    parent_id: null,
    name: "Sales",
    created_at: "2026-04-01T00:00:00Z",
    updated_at: "2026-04-01T00:00:00Z",
  },
  {
    id: "folder-b",
    workspace_id: "ws-1",
    parent_id: null,
    name: "Reports",
    created_at: "2026-04-02T00:00:00Z",
    updated_at: "2026-04-02T00:00:00Z",
  },
];

const artifacts: Artifact[] = [
  {
    id: "art-1",
    workspace_id: "ws-1",
    project_id: null,
    issue_id: null,
    folder_id: null,
    origin_issue_id: null,
    kind: "report",
    format: "md",
    title: "Daily sales report",
    body: "Body",
    file_url: null,
    file_size_bytes: null,
    metadata: {},
    author_type: "agent",
    author_id: "agent-1",
    requester_user_id: null,
    created_at: "2026-04-28T00:00:00Z",
    updated_at: "2026-04-28T00:00:00Z",
  },
];

vi.mock("@multica/cerebro-artifacts/core/queries", () => ({
  artifactFoldersOptions: () => ({
    queryKey: ["artifact-folders"],
    queryFn: () => Promise.resolve(folders),
  }),
  artifactSearchOptions: () => ({
    queryKey: ["artifacts", "search"],
    queryFn: () => Promise.resolve(artifacts),
  }),
}));

import { FileManagerPage } from "./file-manager-page";

function renderPage() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  // Pre-seed cache so the lists render synchronously.
  qc.setQueryData(["artifact-folders"], folders);
  qc.setQueryData(["artifacts", "search"], artifacts);
  return render(
    <QueryClientProvider client={qc}>
      <FileManagerPage initialFolderId={null} />
    </QueryClientProvider>,
  );
}

function makeDataTransfer(initial: Record<string, string> = {}) {
  const store = new Map<string, string>(Object.entries(initial));
  return {
    setData: vi.fn((type: string, value: string) => {
      store.set(type, value);
    }),
    getData: vi.fn((type: string) => store.get(type) ?? ""),
    setDragImage: vi.fn(),
    effectAllowed: "uninitialized",
  };
}

// Find the row in the main file-list (not sidebar tree) for a given folder name.
// The list row has a checkbox labelled "Select <name>" — we use that to
// distinguish it from sidebar entries which don't have one.
function rowFor(folderName: string): HTMLElement {
  const checkbox = screen.getByLabelText(`Select ${folderName}`);
  const row = checkbox.closest("[data-slot=context-menu-trigger]");
  if (!row) throw new Error(`row for ${folderName} not found`);
  return row as HTMLElement;
}

function artifactRow(title: string): HTMLElement {
  const checkbox = screen.getByLabelText(`Select ${title}`);
  const row = checkbox.closest("[data-slot=context-menu-trigger]");
  if (!row) throw new Error(`row for ${title} not found`);
  return row as HTMLElement;
}

describe("FileManagerPage drag-and-drop", () => {
  beforeEach(() => {
    moveArtifactMutate.mockReset();
    updateFolderMutate.mockReset();
    updateFolderMutateAsync.mockReset().mockResolvedValue(undefined);
    deleteFolderMutateAsync.mockReset().mockResolvedValue(undefined);
  });

  it("dragging an artifact onto a folder row moves the artifact", () => {
    renderPage();
    const folderRow = rowFor("Sales");
    const artRow = artifactRow("Daily sales report");

    const dataTransfer = makeDataTransfer();
    fireEvent.dragStart(artRow, { dataTransfer });
    expect(dataTransfer.setData).toHaveBeenCalledWith(
      "text/multica-artifact",
      "art-1",
    );

    fireEvent.dragOver(folderRow, { dataTransfer });
    fireEvent.drop(folderRow, { dataTransfer });

    expect(moveArtifactMutate).toHaveBeenCalledWith({
      id: "art-1",
      data: { folder_id: "folder-a" },
    });
  });

  it("dragging a folder onto another folder moves the folder", () => {
    renderPage();
    const dataTransfer = makeDataTransfer();
    fireEvent.dragStart(rowFor("Reports"), { dataTransfer });
    expect(dataTransfer.setData).toHaveBeenCalledWith(
      "text/multica-folder",
      "folder-b",
    );

    fireEvent.drop(rowFor("Sales"), { dataTransfer });
    expect(updateFolderMutate).toHaveBeenCalledWith({
      id: "folder-b",
      data: { parent_id: "folder-a" },
    });
  });

  it("dragging a folder cannot drop on itself", () => {
    renderPage();
    const dataTransfer = makeDataTransfer({
      "text/multica-folder": "folder-a",
    });
    fireEvent.drop(rowFor("Sales"), { dataTransfer });
    expect(updateFolderMutate).not.toHaveBeenCalled();
  });

  it("dropping an artifact on the sidebar root moves it back to root", () => {
    renderPage();
    const root = screen.getByRole("button", { name: /all documents/i });
    const dataTransfer = makeDataTransfer({
      "text/multica-artifact": "art-1",
    });
    fireEvent.drop(root, { dataTransfer });
    expect(moveArtifactMutate).toHaveBeenCalledWith({
      id: "art-1",
      data: { folder_id: null },
    });
  });

  it("exposes a kebab in the sidebar tree for each folder", () => {
    renderPage();
    // The sidebar tree row + the main-list folder row each render a kebab,
    // so we expect at least 2 (one per surface) for each folder. The label
    // proves the new tree-side kebab — the gap the bug report flagged.
    expect(screen.getAllByLabelText("Actions for Sales").length).toBeGreaterThan(
      1,
    );
  });

  it("dragging an artifact uses a compact drag image, not the full row", () => {
    renderPage();
    const dataTransfer = makeDataTransfer();
    fireEvent.dragStart(artifactRow("Daily sales report"), { dataTransfer });

    expect(dataTransfer.setDragImage).toHaveBeenCalledTimes(1);
    const ghost = dataTransfer.setDragImage.mock.calls[0]![0] as HTMLElement;
    // Custom ghost — narrow pill labelled with the artifact title — replaces
    // the 820px-wide grid row that the browser would otherwise screenshot.
    expect(ghost.getAttribute("data-drag-ghost")).toBe("artifact");
    expect(ghost.textContent).toContain("Daily sales report");
  });

  it("dragging a folder uses a folder-flavoured drag image", () => {
    renderPage();
    const dataTransfer = makeDataTransfer();
    fireEvent.dragStart(rowFor("Reports"), { dataTransfer });

    expect(dataTransfer.setDragImage).toHaveBeenCalledTimes(1);
    const ghost = dataTransfer.setDragImage.mock.calls[0]![0] as HTMLElement;
    expect(ghost.getAttribute("data-drag-ghost")).toBe("folder");
    expect(ghost.textContent).toContain("Reports");
  });
});
