import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { DataTab } from "./data-tab";
import { toast } from "sonner";

const apiMocks = vi.hoisted(() => ({
  exportWorkspaceData: vi.fn(),
  dryRunWorkspaceImport: vi.fn(),
  applyWorkspaceImport: vi.fn(),
}));

const issueStoreMocks = vi.hoisted(() => ({
  fetch: vi.fn(),
}));

vi.mock("@/shared/api", () => ({
  api: apiMocks,
}));

vi.mock("@/features/issues", () => ({
  useIssueStore: {
    getState: () => issueStoreMocks,
  },
}));

vi.mock("sonner", () => ({
  toast: {
    success: vi.fn(),
    error: vi.fn(),
  },
}));

describe("DataTab", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("calls exportWorkspaceData when export button is clicked", async () => {
    const user = userEvent.setup();
    apiMocks.exportWorkspaceData.mockResolvedValue({
      schema_version: "2026-05-31",
      workspace: {
        id: "ws-1",
        slug: "demo",
        exported_at: "2026-05-31T00:00:00Z",
        source_app_version: "dev",
      },
      data: { issues: [] },
    });

    const createObjectURL = vi.spyOn(URL, "createObjectURL").mockReturnValue("blob:test");
    const revokeObjectURL = vi.spyOn(URL, "revokeObjectURL").mockImplementation(() => {});
    const clickSpy = vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(() => {});

    render(<DataTab />);
    await user.click(screen.getByRole("button", { name: /export json/i }));

    await waitFor(() => {
      expect(apiMocks.exportWorkspaceData).toHaveBeenCalledTimes(1);
    });

    expect(createObjectURL).toHaveBeenCalledTimes(1);
    expect(clickSpy).toHaveBeenCalledTimes(1);
    expect(revokeObjectURL).toHaveBeenCalledTimes(1);

    createObjectURL.mockRestore();
    revokeObjectURL.mockRestore();
    clickSpy.mockRestore();
  });

  it("calls dryRunWorkspaceImport with normalized canonical payload", async () => {
    const user = userEvent.setup();
    apiMocks.dryRunWorkspaceImport.mockResolvedValue({
      summary: "dry-run ok",
      warnings: [],
      errors: [],
      created: 0,
      skipped: 0,
      failed: 0,
    });

    render(<DataTab />);
    fireEvent.change(screen.getByLabelText(/manifest json/i), {
      target: {
        value: JSON.stringify({
          schema_version: "2026-05-31",
          workspace: { id: "ws-1" },
          data: { issues: [{ title: "Issue A" }] },
        }),
      },
    });
    await user.click(screen.getByRole("button", { name: /dry run/i }));

    await waitFor(() => {
      expect(apiMocks.dryRunWorkspaceImport).toHaveBeenCalledWith(
        expect.objectContaining({
          source_type: "canonical-json",
          workspace_id: "ws-1",
        }),
      );
    });
  });

  it("calls applyWorkspaceImport when apply button is clicked", async () => {
    const user = userEvent.setup();
    apiMocks.applyWorkspaceImport.mockResolvedValue({
      summary: "apply ok",
      warnings: [],
      errors: [],
      created: 1,
      skipped: 0,
      failed: 0,
    });

    render(<DataTab />);
    fireEvent.change(screen.getByLabelText(/manifest json/i), {
      target: {
        value: JSON.stringify({
          schema_version: "2026-05-31",
          source_type: "issue-csv",
          issues: [{ title: "Issue B" }],
        }),
      },
    });
    await user.click(screen.getByRole("button", { name: /apply import/i }));

    await waitFor(() => {
      expect(apiMocks.applyWorkspaceImport).toHaveBeenCalledWith(
        expect.objectContaining({
          source_type: "issue-csv",
        }),
      );
    });
    expect(issueStoreMocks.fetch).toHaveBeenCalledTimes(1);
  });

  it("does not report success when apply returns errors", async () => {
    const user = userEvent.setup();
    apiMocks.applyWorkspaceImport.mockResolvedValue({
      summary: "apply partial",
      warnings: [],
      errors: [{ code: "create_issue_failed", message: "create issue failed" }],
      created: 0,
      skipped: 0,
      failed: 1,
    });

    render(<DataTab />);
    fireEvent.change(screen.getByLabelText(/manifest json/i), {
      target: {
        value: JSON.stringify({
          schema_version: "2026-05-31",
          source_type: "issue-csv",
          issues: [{ title: "Issue B" }],
        }),
      },
    });
    await user.click(screen.getByRole("button", { name: /apply import/i }));

    await waitFor(() => {
      expect(apiMocks.applyWorkspaceImport).toHaveBeenCalledTimes(1);
    });
    expect(toast.success).not.toHaveBeenCalledWith("Import apply completed");
    expect(screen.getByText(/apply partial/i)).toBeInTheDocument();
  });
});
