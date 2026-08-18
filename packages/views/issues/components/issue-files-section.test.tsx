import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import { WorkspaceSlugProvider } from "@multica/core/paths";
import { NavigationProvider } from "../../navigation";
import type { NavigationAdapter } from "../../navigation";
import type { Attachment } from "@multica/core/types";
import enCommon from "../../locales/en/common.json";
import enIssues from "../../locales/en/issues.json";

const TEST_RESOURCES = { en: { common: enCommon, issues: enIssues } };

const mockListIssueFiles = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/api", () => ({
  api: {
    listIssueFiles: (...args: unknown[]) => mockListIssueFiles(...args),
  },
}));

vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({
    getActorName: () => "Agent A",
    getActorInitials: () => "AA",
    getActorAvatarUrl: () => null,
  }),
}));

import { IssueFilesSection } from "./issue-files-section";

function makeFile(id: string, filename: string): Attachment {
  return {
    id,
    workspace_id: "ws-1",
    issue_id: "issue-1",
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
  };
}

const adapter: NavigationAdapter = {
  push: vi.fn(),
  replace: vi.fn(),
  back: vi.fn(),
  pathname: "/acme/issues/issue-1",
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
            <IssueFilesSection issueId="issue-1" />
          </I18nProvider>
        </NavigationProvider>
      </WorkspaceSlugProvider>
    </QueryClientProvider>,
  );
}

describe("IssueFilesSection", () => {
  beforeEach(() => {
    mockListIssueFiles.mockReset();
  });

  it("renders the task's files", async () => {
    mockListIssueFiles.mockResolvedValue({
      files: [makeFile("att-1", "report.pdf")],
      total: 1,
    });
    renderSection();
    await waitFor(() => {
      expect(screen.getByText("report.pdf")).toBeInTheDocument();
    });
    expect(screen.getByText(/Agent A/)).toBeInTheDocument();
  });

  it("shows the empty message when the task has no files", async () => {
    mockListIssueFiles.mockResolvedValue({ files: [], total: 0 });
    renderSection();
    await waitFor(() => {
      expect(screen.getByText("No files in this task.")).toBeInTheDocument();
    });
  });
});
