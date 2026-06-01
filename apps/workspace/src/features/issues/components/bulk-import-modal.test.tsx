import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { parseTextInput, parseCsvInput, BulkImportModal } from "./bulk-import-modal";

// ---------------------------------------------------------------------------
// Parser tests (tasks 5.1)
// ---------------------------------------------------------------------------

describe("parseTextInput", () => {
  it("splits non-empty lines into issue items", () => {
    const result = parseTextInput("Fix login bug\nAdd dark mode\nUpdate docs");
    expect(result).toEqual([
      { title: "Fix login bug" },
      { title: "Add dark mode" },
      { title: "Update docs" },
    ]);
  });

  it("ignores blank lines", () => {
    const result = parseTextInput("Issue 1\n\n\nIssue 2\n");
    expect(result).toHaveLength(2);
    expect(result[0]?.title).toBe("Issue 1");
    expect(result[1]?.title).toBe("Issue 2");
  });

  it("returns empty array for all-blank input", () => {
    expect(parseTextInput("   \n\n   ")).toEqual([]);
    expect(parseTextInput("")).toEqual([]);
  });

  it("trims whitespace from titles", () => {
    const result = parseTextInput("  Trimmed  ");
    expect(result[0]?.title).toBe("Trimmed");
  });
});

describe("parseCsvInput", () => {
  it("parses header + data rows into issue items", () => {
    const csv = "title,description,priority,status\nFix bug,desc,high,todo";
    const result = parseCsvInput(csv);
    expect(result).toHaveLength(1);
    expect(result[0]).toMatchObject({
      title: "Fix bug",
      description: "desc",
      priority: "high",
      status: "todo",
    });
  });

  it("handles title-only CSV", () => {
    const csv = "title\nIssue One\nIssue Two";
    const result = parseCsvInput(csv);
    expect(result).toHaveLength(2);
    expect(result[0]?.title).toBe("Issue One");
    expect(result[1]?.description).toBeUndefined();
  });

  it("keeps rows with empty title so validation can surface them", () => {
    const csv = "title,priority\n,high\nValid Issue,low";
    const result = parseCsvInput(csv);
    expect(result).toHaveLength(2);
    expect(result[0]?.title).toBe("");
    expect(result[1]?.title).toBe("Valid Issue");
  });

  it("returns empty array when no title column", () => {
    const csv = "name,description\nFoo,Bar";
    expect(parseCsvInput(csv)).toEqual([]);
  });

  it("returns empty array for fewer than 2 lines", () => {
    expect(parseCsvInput("title")).toEqual([]);
    expect(parseCsvInput("")).toEqual([]);
  });

  it("handles case-insensitive headers", () => {
    const csv = "Title,Description\nMy Issue,Some desc";
    const result = parseCsvInput(csv);
    expect(result).toHaveLength(1);
    expect(result[0]?.title).toBe("My Issue");
  });
});

// ---------------------------------------------------------------------------
// BulkImportModal component tests (task 5.2)
// ---------------------------------------------------------------------------

vi.mock("@/shared/api", () => ({
  api: {
    dryRunWorkspaceImport: vi.fn(),
    applyWorkspaceImport: vi.fn(),
  },
}));

const issueStoreMocks = vi.hoisted(() => ({
  fetch: vi.fn().mockResolvedValue(undefined),
}));

vi.mock("@/features/issues", () => ({
  useIssueStore: (sel: (s: { fetch: () => Promise<void> }) => unknown) =>
    sel({ fetch: issueStoreMocks.fetch }),
}));

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

describe("BulkImportModal", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("renders in text mode by default with Import button disabled", () => {
    render(<BulkImportModal open onOpenChange={vi.fn()} />);
    expect(screen.getByPlaceholderText(/Fix login bug/i)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /^import$/i })).toBeDisabled();
  });

  it("enables Import button when text input has content", () => {
    render(<BulkImportModal open onOpenChange={vi.fn()} />);
    const ta = screen.getByRole("textbox");
    fireEvent.change(ta, { target: { value: "Issue One\nIssue Two" } });
    expect(screen.getByRole("button", { name: /Import 2/i })).not.toBeDisabled();
  });

  it("shows 'No valid issues found' hint for CSV input missing title column", () => {
    render(<BulkImportModal open onOpenChange={vi.fn()} />);
    // Switch to CSV tab
    fireEvent.click(screen.getByRole("tab", { name: /csv/i }));
    const ta = screen.getByRole("textbox");
    // CSV with no title header → parsedItems will be empty but input is non-empty
    fireEvent.change(ta, { target: { value: "name,description\nFoo,Bar" } });
    expect(screen.getByText(/No valid issues found/i)).toBeInTheDocument();
  });

  it("calls dry-run and apply import pipeline then closes on success", async () => {
    const { api } = await import("@/shared/api");
    const dryRunMock = vi.mocked(api.dryRunWorkspaceImport);
    const applyMock = vi.mocked(api.applyWorkspaceImport);
    dryRunMock.mockResolvedValueOnce({
      summary: "dry-run ok",
      warnings: [],
      errors: [],
      created: 0,
      skipped: 0,
      failed: 0,
    });
    applyMock.mockResolvedValueOnce({
      summary: "apply ok",
      warnings: [],
      errors: [],
      created: 1,
      skipped: 0,
      failed: 0,
    });

    const onOpenChange = vi.fn();
    render(<BulkImportModal open onOpenChange={onOpenChange} />);

    const ta = screen.getByRole("textbox");
    fireEvent.change(ta, { target: { value: "Issue One" } });
    fireEvent.click(screen.getByRole("button", { name: /Import 1/i }));

    await waitFor(() => {
      expect(dryRunMock).toHaveBeenCalledWith(
        expect.objectContaining({ source_type: "issue-csv", issues: [{ title: "Issue One" }] }),
      );
      expect(applyMock).toHaveBeenCalledWith(
        expect.objectContaining({ source_type: "issue-csv", issues: [{ title: "Issue One" }] }),
      );
      expect(onOpenChange).toHaveBeenCalledWith(false);
    });
  });

  it("shows dry-run errors and keeps modal open", async () => {
    const { api } = await import("@/shared/api");
    const dryRunMock = vi.mocked(api.dryRunWorkspaceImport);
    const applyMock = vi.mocked(api.applyWorkspaceImport);
    dryRunMock.mockResolvedValueOnce({
      summary: "dry-run blocked",
      warnings: [],
      errors: [{ code: "title_required", message: "title is required" }],
      created: 0,
      skipped: 0,
      failed: 1,
    });

    const onOpenChange = vi.fn();
    render(<BulkImportModal open onOpenChange={onOpenChange} />);

    const ta = screen.getByRole("textbox");
    fireEvent.change(ta, { target: { value: "Issue One" } });
    fireEvent.click(screen.getByRole("button", { name: /Import 1/i }));

    await waitFor(() => {
      expect(screen.getByText(/title is required/i)).toBeInTheDocument();
      expect(applyMock).not.toHaveBeenCalled();
      expect(onOpenChange).not.toHaveBeenCalled();
    });
  });

  it("shows apply errors and keeps modal open", async () => {
    const { api } = await import("@/shared/api");
    const dryRunMock = vi.mocked(api.dryRunWorkspaceImport);
    const applyMock = vi.mocked(api.applyWorkspaceImport);
    dryRunMock.mockResolvedValueOnce({
      summary: "dry-run ok",
      warnings: [],
      errors: [],
      created: 0,
      skipped: 0,
      failed: 0,
    });
    applyMock.mockResolvedValueOnce({
      summary: "apply partial",
      warnings: [],
      errors: [{ code: "create_issue_failed", message: "create issue failed" }],
      created: 0,
      skipped: 0,
      failed: 1,
    });

    const onOpenChange = vi.fn();
    render(<BulkImportModal open onOpenChange={onOpenChange} />);

    const ta = screen.getByRole("textbox");
    fireEvent.change(ta, { target: { value: "Issue One" } });
    fireEvent.click(screen.getByRole("button", { name: /Import 1/i }));

    await waitFor(() => {
      expect(screen.getByText(/create issue failed/i)).toBeInTheDocument();
      expect(issueStoreMocks.fetch).toHaveBeenCalledTimes(1);
      expect(onOpenChange).not.toHaveBeenCalled();
    });
  });
});
