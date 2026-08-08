import { describe, it, expect, vi, beforeEach } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Artifact, ArtifactFolder } from "@multica/core/types";

// Stub the FIR-1590 folder access-control picker. It's an independent widget
// (auth store + member queries + visibility mutations) that FileManagerPage
// only renders per row; these drag-and-drop tests don't exercise it, and
// rendering the real one would require booting the platform auth store.
vi.mock("./folder-access-control", () => ({
  FolderAccessControl: () => null,
}));

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
const navigationPush = vi.hoisted(() => vi.fn());

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
    memberDetail: (id: string) => `/ws/members/${id}`,
    agentDetail: (id: string) => `/ws/agents/${id}`,
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
    push: navigationPush,
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
    kind: "document",
    created_at: "2026-04-01T00:00:00Z",
    updated_at: "2026-04-01T00:00:00Z",
  },
  {
    id: "folder-b",
    workspace_id: "ws-1",
    parent_id: null,
    name: "Reports",
    kind: "document",
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
  {
    id: "art-2",
    workspace_id: "ws-1",
    project_id: null,
    issue_id: null,
    folder_id: null,
    origin_issue_id: null,
    kind: "report",
    format: "html",
    title: "HTML report",
    body: "<p>Body</p>",
    file_url: null,
    file_size_bytes: null,
    metadata: {},
    author_type: "agent",
    author_id: "agent-1",
    requester_user_id: null,
    created_at: "2026-04-28T00:00:00Z",
    updated_at: "2026-04-28T00:00:00Z",
  },
  // Lives inside "Sales" and only mentions "budget" in its body — the fixture
  // the FIR-4163 search-scope and Location tests below need.
  {
    id: "art-3",
    workspace_id: "ws-1",
    project_id: null,
    issue_id: null,
    folder_id: "folder-a",
    origin_issue_id: null,
    kind: "report",
    format: "md",
    title: "Quarterly numbers",
    body: "The budget is on track.",
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

// FIR-4624: record the search params so a test can assert the folder scope is
// sent to the server rather than applied after the fact.
const searchParams = vi.hoisted(() => [] as Record<string, unknown>[]);

vi.mock("@multica/cerebro-artifacts/core/queries", () => ({
  artifactFoldersOptions: () => ({
    queryKey: ["artifact-folders"],
    queryFn: () => Promise.resolve(folders),
  }),
  artifactSearchOptions: (_wsId: string, params: Record<string, unknown>) => {
    searchParams.push(params);
    return {
      queryKey: ["artifacts", "search"],
      queryFn: () => Promise.resolve(artifacts),
    };
  },
}));

import { FileManagerPage } from "./file-manager-page";

function renderPage(initialFolderId: string | null = null) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  // Pre-seed cache so the lists render synchronously.
  qc.setQueryData(["artifact-folders"], folders);
  qc.setQueryData(["artifacts", "search"], artifacts);
  return render(
    <QueryClientProvider client={qc}>
      <FileManagerPage initialFolderId={initialFolderId} />
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

  it("renders the documents search as a browser search field with autofill disabled", () => {
    renderPage();

    const search = screen.getByPlaceholderText("Search names…");

    expect(search).toHaveAttribute("type", "search");
    expect(search).toHaveAttribute("autocomplete", "off");
    expect(search).toHaveAttribute("autocorrect", "off");
    expect(search).toHaveAttribute("spellcheck", "false");
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
    // Exact name: the Location column also renders per-row buttons, but those
    // are named "Go to All documents".
    const root = screen.getByRole("button", { name: "All documents" });
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

// Regression for JEH-1060: the row kebab on a document row exposed Rename /
// Edit body / Move to / Delete via `onSelect`, which is a Radix prop —
// Base UI's Menu.Item fires on `onClick`, so every action was silently
// dropped. These tests pin the wiring so we can't reintroduce that bug.
describe("FileManagerPage artifact row menu actions", () => {
  beforeEach(() => {
    deleteArtifactMutateAsync.mockReset().mockResolvedValue(undefined);
    updateArtifactMutateAsync.mockReset().mockResolvedValue(undefined);
    moveArtifactMutate.mockReset();
    navigationPush.mockReset();
  });

  function openArtifactKebab(title: string) {
    const kebab = screen.getByLabelText(`Actions for ${title}`);
    fireEvent.click(kebab);
  }

  it("Rename action opens the rename dialog", () => {
    renderPage();
    openArtifactKebab("Daily sales report");

    const rename = screen.getAllByText("Rename")[0]!;
    fireEvent.click(rename);

    expect(screen.getByText("Rename document")).toBeTruthy();
    const input = screen.getByDisplayValue("Daily sales report");
    expect(input).toBeTruthy();
  }, 10_000);

  it("Edit body action navigates non-markdown artifacts to the document edit page", () => {
    renderPage();
    openArtifactKebab("HTML report");

    const editBody = screen.getByText("Edit body");
    fireEvent.click(editBody);

    expect(navigationPush).toHaveBeenCalledWith("/ws/documents/art-2/edit");
  });

  it("does not show Edit body for markdown artifacts", () => {
    renderPage();
    openArtifactKebab("Daily sales report");

    expect(screen.queryByText("Edit body")).not.toBeInTheDocument();
  });

  it("Delete action opens the delete confirmation dialog", () => {
    renderPage();
    openArtifactKebab("Daily sales report");

    const del = screen.getByText("Delete");
    fireEvent.click(del);

    expect(screen.getByText("Delete document?")).toBeTruthy();
  });
});

// FIR-4163: you have to be able to see which folder a document sits in, search
// folder names as well as document names, opt into searching the contents, and
// choose which columns the list shows.
describe("FileManagerPage folder visibility, search scope and columns", () => {
  beforeEach(() => {
    window.localStorage.clear();
    moveArtifactMutate.mockReset();
  });

  function search(term: string) {
    fireEvent.change(screen.getByPlaceholderText("Search names…"), {
      target: { value: term },
    });
  }

  it("shows the containing folder in the Location column", () => {
    renderPage();

    // Both root-level documents say they sit outside any folder.
    expect(
      screen.getAllByRole("button", { name: "Go to All documents" }).length,
    ).toBe(2);

    // A hit from elsewhere in the workspace names its folder.
    search("Quarterly");
    expect(
      screen.getByRole("button", { name: "Go to Sales" }),
    ).toBeInTheDocument();
  });

  it("clicking a Location cell opens that folder and leaves the search", () => {
    renderPage();
    search("Quarterly");

    fireEvent.click(screen.getByRole("button", { name: "Go to Sales" }));

    // Landing in Sales means the search is over and its contents are listed —
    // not the workspace-wide result set that got us here.
    expect(screen.getByPlaceholderText("Search names…")).toHaveValue("");
    expect(screen.getByLabelText("Select Quarterly numbers")).toBeInTheDocument();
    expect(screen.queryByLabelText("Select Daily sales report")).toBeNull();
  });

  it("matches folder names, not just document names", () => {
    renderPage();
    search("Reports");

    // The folder matches; no document title contains "Reports".
    expect(screen.getByLabelText("Select Reports")).toBeInTheDocument();
    expect(screen.queryByLabelText("Select Daily sales report")).toBeNull();
  });

  it("finds a document that lives in another folder", () => {
    renderPage();
    search("Quarterly");

    // Standing at "All documents", so under the old folder-scoped filter this
    // row was invisible.
    expect(screen.getByLabelText("Select Quarterly numbers")).toBeInTheDocument();
  });

  it("only matches contents once the Contents toggle is on", () => {
    renderPage();
    search("budget");

    // "budget" appears only in a body, so names-only finds nothing. Exact name:
    // the empty state offers its own "Search contents too" shortcut.
    const toggle = screen.getByRole("button", { name: "Contents" });
    expect(toggle).toHaveAttribute("aria-pressed", "false");
    expect(screen.queryByLabelText("Select Quarterly numbers")).toBeNull();

    fireEvent.click(toggle);

    expect(toggle).toHaveAttribute("aria-pressed", "true");
    expect(screen.getByLabelText("Select Quarterly numbers")).toBeInTheDocument();
  });

  it("hiding a column removes its header and remembers the choice", () => {
    renderPage();
    expect(screen.getByText("Location")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Columns" }));
    fireEvent.click(screen.getByRole("menuitemcheckbox", { name: "Location" }));

    expect(
      JSON.parse(window.localStorage.getItem("documents:columns") ?? "[]"),
    ).not.toContain("location");

    // Re-mount rather than assert against the still-open menu: this proves the
    // remembered choice survives a page load, which is the point of storing it.
    cleanup();
    renderPage();
    expect(screen.queryByText("Location")).toBeNull();
  });
});

// FIR-4624: folders rendered as "This folder is empty" whenever their documents
// fell outside the newest-N workspace-wide window the list fetched. The fix is
// to ask the server for the folder, so these assert the request, not the render.
describe("FileManagerPage folder scope (FIR-4624)", () => {
  beforeEach(() => {
    searchParams.length = 0;
  });

  it("asks the server for the open folder", () => {
    renderPage("folder-a");
    expect(searchParams.at(-1)?.folder).toBe("folder-a");
  });

  it("asks the server for unfiled documents at the root", () => {
    renderPage(null);
    expect(searchParams.at(-1)?.folder).toBe("root");
  });

  it("drops the folder scope while searching, so results span the workspace", () => {
    renderPage("folder-a");
    fireEvent.change(screen.getByPlaceholderText("Search names…"), {
      target: { value: "sales" },
    });
    expect(searchParams.at(-1)?.folder).toBeUndefined();
  });
});
