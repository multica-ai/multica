// @vitest-environment jsdom

// CEREBRO-PATCH(attachment-folder): FIR-2697 part 4 — render tests for the
// folder segment on the artifact card. These drive the actual component (not a
// screenshot) to prove the function: when the part-4 flag is on and the
// attached agent document has a folder, the card shows and LINKS to that folder;
// when the flag is off the folder segment is absent; a note surface links into
// the notes folder tree. Every external hook is mocked so the render is
// deterministic and offline.

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, cleanup } from "@testing-library/react";
import type { Artifact, ArtifactFolder } from "@multica/core/types";

// --- Mutable test state the mocks read from -------------------------------
let mockArtifact: Artifact | undefined;
let mockFolders: ArtifactFolder[] | undefined;
const flagState: Record<string, boolean> = {
  cerebro_artifact_references: true,
  cerebro_attachment_folder: true,
};

// useQuery is resolved by queryKey[0]: the chip fires two queries, one for the
// artifact ("artifacts") and one for the folder list ("artifact-folders").
vi.mock("@tanstack/react-query", () => ({
  queryOptions: (o: unknown) => o,
  useQuery: (opts: { queryKey: unknown[]; enabled?: boolean }) => {
    // Match react-query: a disabled query yields no data until it runs.
    if (opts.enabled === false) return { data: undefined };
    const kind = opts.queryKey[0];
    if (kind === "artifacts") return { data: mockArtifact };
    if (kind === "artifact-folders") return { data: mockFolders };
    return { data: undefined };
  },
}));

vi.mock("@multica/core/api", () => ({
  api: {
    getArtifact: vi.fn(),
    listArtifactFolders: vi.fn(),
  },
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    documentDetail: (id: string) => `/ws-1/documents/${id}`,
    documentsFolder: (id: string) => `/ws-1/documents?folder=${id}`,
    notesFolder: (id: string) => `/ws-1/notes?folder=${id}`,
  }),
}));

vi.mock("@multica/views/navigation", () => ({
  // No openInNewTab → the component keeps a real href we can assert on.
  useNavigation: () => ({ openInNewTab: undefined }),
}));

vi.mock("@multica/cerebro-feature-flags", () => ({
  useFeatureFlag: (key: string) => flagState[key] ?? false,
}));

import { ArtifactMentionChip } from "./artifact-chip";

function makeArtifact(overrides: Partial<Artifact> = {}): Artifact {
  return {
    id: "art-1",
    workspace_id: "ws-1",
    project_id: null,
    issue_id: null,
    folder_id: "folder-1",
    origin_issue_id: null,
    kind: "report",
    format: "md",
    title: "Launch notes",
    body: "",
    file_url: null,
    file_size_bytes: null,
    metadata: {},
    author_type: "agent",
    author_id: "agent-1",
    requester_user_id: null,
    created_at: "2026-07-05T00:00:00Z",
    updated_at: "2026-07-05T00:00:00Z",
    ...overrides,
  };
}

function makeFolder(overrides: Partial<ArtifactFolder> = {}): ArtifactFolder {
  return {
    id: "folder-1",
    workspace_id: "ws-1",
    parent_id: null,
    name: "Ship the launch plan",
    kind: "document",
    created_at: "2026-07-05T00:00:00Z",
    updated_at: "2026-07-05T00:00:00Z",
    ...overrides,
  };
}

beforeEach(() => {
  mockArtifact = makeArtifact();
  mockFolders = [makeFolder()];
  flagState.cerebro_artifact_references = true;
  flagState.cerebro_attachment_folder = true;
});

afterEach(() => cleanup());

describe("ArtifactMentionChip folder segment (FIR-2697 part 4)", () => {
  it("shows the folder name and links to the document folder view", () => {
    render(<ArtifactMentionChip artifactId="art-1" />);

    // Document link stays.
    const doc = screen.getByText("Launch notes").closest("a");
    expect(doc?.getAttribute("href")).toBe("/ws-1/documents/art-1");

    // Folder segment appears and links to the folder view.
    const folder = screen.getByText("Ship the launch plan").closest("a");
    expect(folder?.getAttribute("href")).toBe("/ws-1/documents?folder=folder-1");
    expect(folder?.getAttribute("title")).toBe("In folder: Ship the launch plan");
  });

  it("hides the folder segment when the part-4 flag is off", () => {
    flagState.cerebro_attachment_folder = false;
    render(<ArtifactMentionChip artifactId="art-1" />);

    expect(screen.getByText("Launch notes")).toBeTruthy();
    expect(screen.queryByText("Ship the launch plan")).toBeNull();
  });

  it("hides the folder segment when the artifact has no folder", () => {
    mockArtifact = makeArtifact({ folder_id: null });
    render(<ArtifactMentionChip artifactId="art-1" />);

    expect(screen.getByText("Launch notes")).toBeTruthy();
    expect(screen.queryByText("Ship the launch plan")).toBeNull();
  });

  it("links a note artifact into the notes folder tree", () => {
    mockArtifact = makeArtifact({ kind: "note", title: "A note" });
    mockFolders = [makeFolder({ kind: "note", name: "Note run" })];
    render(<ArtifactMentionChip artifactId="art-1" />);

    const folder = screen.getByText("Note run").closest("a");
    expect(folder?.getAttribute("href")).toBe("/ws-1/notes?folder=folder-1");
  });
});
