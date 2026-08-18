import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import { WorkspaceSlugProvider } from "@multica/core/paths";
import { NavigationProvider } from "../../navigation";
import type { NavigationAdapter } from "../../navigation";
import type { ProjectFile } from "@multica/core/types";
import enCommon from "../../locales/en/common.json";
import enProjects from "../../locales/en/projects.json";

const TEST_RESOURCES = { en: { common: enCommon, projects: enProjects } };

const mockListFiles = vi.hoisted(() => vi.fn());
const mockHideFile = vi.hoisted(() => vi.fn());
const mockUnhideFile = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/api", () => ({
  api: {
    listProjectFiles: (...args: unknown[]) => mockListFiles(...args),
    hideProjectFile: (...args: unknown[]) => mockHideFile(...args),
    unhideProjectFile: (...args: unknown[]) => mockUnhideFile(...args),
  },
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({
    getActorName: () => "Agent A",
    getActorInitials: () => "AA",
    getActorAvatarUrl: () => null,
  }),
}));

import { ProjectFilesSection } from "./project-files-section";

function makeFile(id: string, filename: string, hidden = false): ProjectFile {
  return {
    id,
    workspace_id: "ws-1",
    issue_id: null,
    comment_id: null,
    chat_session_id: null,
    chat_message_id: null,
    uploader_type: "agent",
    uploader_id: "agent-1",
    filename,
    url: `https://cdn.example/${id}`,
    download_url: `/api/attachments/${id}/download`,
    markdown_url: "",
    content_type: "application/pdf",
    size_bytes: 1024,
    created_at: "2025-01-01T00:00:00Z",
    hidden,
  };
}

const adapter: NavigationAdapter = {
  push: vi.fn(),
  replace: vi.fn(),
  back: vi.fn(),
  pathname: "/acme/projects/proj-1",
  searchParams: new URLSearchParams(),
  getShareableUrl: (path: string) => `https://app.example${path}`,
};

function renderSection() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={qc}>
      <WorkspaceSlugProvider slug="acme">
        <NavigationProvider value={adapter}>
          <I18nProvider locale="en" resources={TEST_RESOURCES}>
            <ProjectFilesSection projectId="proj-1" />
          </I18nProvider>
        </NavigationProvider>
      </WorkspaceSlugProvider>
    </QueryClientProvider>,
  );
}

describe("ProjectFilesSection", () => {
  beforeEach(() => {
    mockListFiles.mockReset();
    mockHideFile.mockReset();
    mockUnhideFile.mockReset();
    mockHideFile.mockResolvedValue(undefined);
    mockUnhideFile.mockResolvedValue(undefined);
  });

  it("renders visible files and keeps hidden files behind the toggle", async () => {
    mockListFiles.mockResolvedValue({
      files: [makeFile("att-1", "visible.pdf"), makeFile("att-2", "hidden.pdf", true)],
      total: 2,
    });
    renderSection();

    await waitFor(() => {
      expect(screen.getByText("visible.pdf")).toBeInTheDocument();
    });
    expect(screen.queryByText("hidden.pdf")).not.toBeInTheDocument();

    // Reveal hidden files via the toggle.
    await userEvent.click(screen.getByText(/Show 1 hidden file/));
    expect(screen.getByText("hidden.pdf")).toBeInTheDocument();
  });

  it("hides a visible file through the hide mutation", async () => {
    mockListFiles.mockResolvedValue({
      files: [makeFile("att-1", "visible.pdf")],
      total: 1,
    });
    renderSection();

    await waitFor(() => {
      expect(screen.getByText("visible.pdf")).toBeInTheDocument();
    });

    await userEvent.click(screen.getByRole("button", { name: "Hide from list" }));
    await waitFor(() => {
      expect(mockHideFile).toHaveBeenCalledWith("proj-1", "att-1");
    });
  });
});
