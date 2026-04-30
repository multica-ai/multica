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

  it("ignores rows with empty title", () => {
    const csv = "title,priority\n,high\nValid Issue,low";
    const result = parseCsvInput(csv);
    expect(result).toHaveLength(1);
    expect(result[0]?.title).toBe("Valid Issue");
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
    bulkCreateIssues: vi.fn(),
  },
  BulkCreateApiError: class BulkCreateApiError extends Error {
    errors: { index: number; reason: string }[];
    constructor(errors: { index: number; reason: string }[]) {
      super("Validation errors");
      this.errors = errors;
    }
  },
}));

vi.mock("@/features/issues", () => ({
  useIssueStore: (sel: (s: { addIssue: () => void }) => unknown) =>
    sel({ addIssue: vi.fn() }),
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

  it("calls bulkCreateIssues and closes on success", async () => {
    const { api } = await import("@/shared/api");
    const mockFn = vi.mocked(api.bulkCreateIssues);
    mockFn.mockResolvedValueOnce({
      issues: [
        {
          id: "1", workspace_id: "ws", number: 1, identifier: "MUL-1",
          title: "Issue One", description: null, status: "backlog", priority: "none",
          assignee_type: null, assignee_id: null, creator_type: "member",
          creator_id: "u1", parent_issue_id: null, position: 0,
          due_date: null, start_date: null, end_date: null,
          created_at: "", updated_at: "", project_id: null,
        },
      ],
    });

    const onOpenChange = vi.fn();
    render(<BulkImportModal open onOpenChange={onOpenChange} />);

    const ta = screen.getByRole("textbox");
    fireEvent.change(ta, { target: { value: "Issue One" } });
    fireEvent.click(screen.getByRole("button", { name: /Import 1/i }));

    await waitFor(() => {
      expect(mockFn).toHaveBeenCalledWith([{ title: "Issue One" }]);
      expect(onOpenChange).toHaveBeenCalledWith(false);
    });
  });

  it("shows server errors and keeps modal open on failure", async () => {
    const { api, BulkCreateApiError } = await import("@/shared/api");
    const mockFn = vi.mocked(api.bulkCreateIssues);
    mockFn.mockRejectedValueOnce(
      new BulkCreateApiError([{ index: 0, reason: "title is required" }]),
    );

    const onOpenChange = vi.fn();
    render(<BulkImportModal open onOpenChange={onOpenChange} />);

    const ta = screen.getByRole("textbox");
    fireEvent.change(ta, { target: { value: "Issue One" } });
    fireEvent.click(screen.getByRole("button", { name: /Import 1/i }));

    await waitFor(() => {
      expect(screen.getByText(/title is required/i)).toBeInTheDocument();
      expect(onOpenChange).not.toHaveBeenCalled();
    });
  });
});
