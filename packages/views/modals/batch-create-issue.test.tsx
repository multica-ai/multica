import { describe, expect, it, vi, beforeEach } from "vitest";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";

const mockMutateAsync = vi.hoisted(() => vi.fn());
const mockSetLastMode = vi.hoisted(() => vi.fn());
const mockToastSuccess = vi.hoisted(() => vi.fn());
const mockToastError = vi.hoisted(() => vi.fn());
const mockToastWarning = vi.hoisted(() => vi.fn());
const modalMessages = vi.hoisted(() => ({
  common: {
    close: "Close",
    cancel: "Cancel",
  },
  create_issue: {
    sr_batch: "Batch create issues",
    batch_breadcrumb: "Batch creation",
    project: "Project",
    batch: {
      upload_json: "Upload JSON",
      upload_json_hint: "Drop a .json file here",
      upload_file_aria: "Upload JSON file",
      download_template: "Download JSON template",
      preview: "Preview",
      row_count: "{{count}} rows",
      no_rows: "No rows",
      agent_run_count: "{{count}} agent runs",
      table_row: "Row",
      table_title: "Title",
      table_status: "Status",
      table_assignee: "Assignee",
      table_project: "Project",
      table_agent_run: "Agent run",
      table_agent_run_tooltip: "Whether this row will enqueue an agent run immediately after creation",
      empty_state: "Paste or upload JSON",
      create: "Create issues",
      checking: "Checking...",
      creating: "Creating...",
      cancel: "Cancel",
      confirm_title: "Create {{count}} issues?",
      confirm_description: "{{count}} non-backlog agent-assigned issues may enqueue agent runs and consume a large amount of tokens. Review the batch before continuing.",
      invalid_json_title: "Fix the JSON before creating issues",
      validation_errors_title: "Validation errors",
      invalid_json_description: "Expected a JSON object with an issues array. Check missing braces, brackets, commas, and quotes.",
      starts_now: "Starts now",
      toast_upload_json: "Upload a JSON file",
      toast_validate_failed: "Failed to validate batch",
      toast_create_failed: "Failed to create issues",
      toast_created: "Created {{count}} issues",
      toast_created_with_warnings: "Created {{count}} issues; {{warningCount}} agent tasks need attention",
    },
  },
}));
const MockApiError = vi.hoisted(() =>
  class ApiError extends Error {
    body?: unknown;
    constructor(body: unknown) {
      super("api error");
      this.body = body;
    }
  },
);

vi.mock("../i18n", () => ({
  useT: () => ({
    t: (selector: (messages: typeof modalMessages) => string, values?: Record<string, string | number>) => {
      const template = selector(modalMessages);
      return Object.entries(values ?? {}).reduce(
        (text, [key, value]) => text.replaceAll(`{{${key}}}`, String(value)),
        template,
      );
    },
  }),
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: ({ queryKey }: { queryKey: string[] }) => {
    const key = queryKey.join(":");
    if (key.includes("agents")) {
      return {
        data: [{ id: "agent-1", name: "Codex Agent", archived_at: null }],
      };
    }
    if (key.includes("members")) {
      return {
        data: [{ user_id: "user-1", name: "Ding", email: "ding@example.com", role: "admin" }],
      };
    }
    if (key.includes("projects")) {
      return {
        data: [{ id: "project-1", title: "Batch Project" }],
      };
    }
    return { data: [] };
  },
}));

vi.mock("@multica/core/api", () => ({
  ApiError: MockApiError,
}));

vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector?: (state: { user: { id: string } }) => unknown) =>
    (selector ? selector({ user: { id: "user-1" } }) : { user: { id: "user-1" } }),
}));

vi.mock("@multica/core/issues/mutations", () => ({
  useBatchCreateIssues: () => ({ mutateAsync: mockMutateAsync, isPending: false }),
}));

vi.mock("@multica/core/issues/stores/create-mode-store", () => ({
  useCreateModeStore: (selector?: (state: { setLastMode: typeof mockSetLastMode }) => unknown) =>
    (selector ? selector({ setLastMode: mockSetLastMode }) : { setLastMode: mockSetLastMode }),
}));

vi.mock("@multica/core/issues/stores/draft-store", () => ({
  useIssueDraftStore: (
    selector?: (state: {
      lastAssigneeType: "agent";
      lastAssigneeId: string;
      lastProjectId: string;
    }) => unknown,
  ) => {
    const state = {
      lastAssigneeType: "agent" as const,
      lastAssigneeId: "agent-1",
      lastProjectId: "project-1",
    };
    return selector ? selector(state) : state;
  },
}));

vi.mock("@multica/core/issues/stores/quick-create-store", () => ({
  useQuickCreateStore: (selector?: (state: { lastAgentId: string }) => unknown) =>
    (selector ? selector({ lastAgentId: "agent-1" }) : { lastAgentId: "agent-1" }),
}));

vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => ({ name: "Test Workspace" }),
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/workspace/queries", () => ({
  agentListOptions: () => ({ queryKey: ["workspaces", "ws-1", "agents"] }),
  memberListOptions: () => ({ queryKey: ["workspaces", "ws-1", "members"] }),
}));

vi.mock("@multica/core/projects/queries", () => ({
  projectListOptions: () => ({ queryKey: ["projects", "ws-1", "list"] }),
}));

vi.mock("@multica/ui/components/ui/dialog", () => ({
  DialogTitle: ({ children, className }: { children: ReactNode; className?: string }) => (
    <div className={className}>{children}</div>
  ),
}));

vi.mock("@multica/ui/components/ui/button", () => ({
  Button: ({
    children,
    disabled,
    onClick,
    type = "button",
    ...props
  }: {
    children: ReactNode;
    disabled?: boolean;
    onClick?: () => void;
    type?: "button" | "submit" | "reset";
  }) => (
    <button type={type} disabled={disabled} onClick={onClick} {...props}>
      {children}
    </button>
  ),
}));

vi.mock("@multica/ui/components/ui/textarea", () => ({
  Textarea: (props: React.ComponentProps<"textarea">) => <textarea {...props} />,
}));

vi.mock("@multica/ui/components/ui/alert-dialog", () => ({
  AlertDialog: ({ open, children }: { open: boolean; children: ReactNode }) => (open ? <div>{children}</div> : null),
  AlertDialogContent: ({ children }: { children: ReactNode }) => <div role="alertdialog">{children}</div>,
  AlertDialogHeader: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  AlertDialogTitle: ({ children }: { children: ReactNode }) => <h2>{children}</h2>,
  AlertDialogDescription: ({ children }: { children: ReactNode }) => <p>{children}</p>,
  AlertDialogFooter: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  AlertDialogCancel: ({ children, disabled }: { children: ReactNode; disabled?: boolean }) => (
    <button type="button" disabled={disabled}>{children}</button>
  ),
  AlertDialogAction: ({
    children,
    disabled,
    onClick,
  }: {
    children: ReactNode;
    disabled?: boolean;
    onClick?: () => void;
  }) => (
    <button type="button" disabled={disabled} onClick={onClick}>
      {children}
    </button>
  ),
}));

vi.mock("@multica/ui/components/ui/table", () => ({
  Table: ({ children }: { children: ReactNode }) => <table>{children}</table>,
  TableHeader: ({ children }: { children: ReactNode }) => <thead>{children}</thead>,
  TableBody: ({ children }: { children: ReactNode }) => <tbody>{children}</tbody>,
  TableRow: ({ children }: { children: ReactNode }) => <tr>{children}</tr>,
  TableHead: ({ children }: { children: ReactNode }) => <th>{children}</th>,
  TableCell: ({ children }: { children: ReactNode }) => <td>{children}</td>,
}));

vi.mock("../issues/components/status-icon", () => ({
  StatusIcon: ({ status }: { status: string }) => <span data-testid="status-icon">{status}</span>,
}));

vi.mock("@multica/ui/lib/utils", () => ({
  cn: (...values: Array<string | false | null | undefined>) => values.filter(Boolean).join(" "),
}));

vi.mock("sonner", () => ({
  toast: {
    success: mockToastSuccess,
    error: mockToastError,
    warning: mockToastWarning,
  },
}));

import { BatchCreateIssuePanel } from "./batch-create-issue";
import { CreateModeSelector } from "./create-mode-selector";

const validJSON = JSON.stringify({
  issues: [
    {
      title: "Imported issue",
      status: "todo",
    },
  ],
});

function renderBatchPanel(onClose = vi.fn()) {
  return render(<BatchCreateIssuePanel onClose={onClose} />);
}

describe("BatchCreateIssuePanel", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockMutateAsync.mockImplementation(async (payload) => {
      if (payload.validate_only) {
        return {
          valid: true,
          limit: 1000,
          row_count: payload.issues.length,
          agent_task_count: 0,
          rows: payload.issues.map((issue: any, index: number) => ({
            row: index + 1,
            title: issue.title,
            status: issue.status ?? "todo",
            assignee_type: null,
            assignee_id: null,
            project_id: null,
            will_enqueue_agent_task: false,
          })),
        };
      }
      return {
        valid: true,
        created: payload.issues.length,
        row_count: payload.issues.length,
        agent_task_count: 0,
        issues: [],
        warnings: [],
      };
    });
    Object.defineProperty(URL, "createObjectURL", {
      configurable: true,
      value: vi.fn(() => "blob:template"),
    });
    Object.defineProperty(URL, "revokeObjectURL", {
      configurable: true,
      value: vi.fn(),
    });
  });

  it("downloads a valid JSON template", async () => {
    const user = userEvent.setup();
    const originalCreateElement = document.createElement.bind(document);
    const click = vi.fn();
    vi.spyOn(document, "createElement").mockImplementation((tagName) => {
      const element = originalCreateElement(tagName);
      if (tagName === "a") {
        Object.defineProperty(element, "click", { value: click });
      }
      return element;
    });

    renderBatchPanel();
    await user.click(screen.getByLabelText("Download JSON template"));

    expect(URL.createObjectURL).toHaveBeenCalledTimes(1);
    const blob = vi.mocked(URL.createObjectURL).mock.calls[0]![0] as Blob;
    const parsed = JSON.parse(await blob.text());
    expect(parsed.issues[0]).toMatchObject({
      title: "Fix login empty state copy",
      assignee_type: "member",
      assignee_id: "user-1",
      project_id: "project-1",
    });
    expect(parsed.issues[1]).toMatchObject({
      assignee_type: "agent",
      assignee_id: "agent-1",
    });
    expect(click).toHaveBeenCalled();
  });

  it("uploaded JSON populates the preview", async () => {
    const user = userEvent.setup();
    renderBatchPanel();

    await user.upload(
      screen.getByLabelText("Upload JSON file"),
      new File([validJSON], "batch.json", { type: "application/json" }),
    );

    await waitFor(() => {
      expect(screen.getByText("Imported issue")).toBeInTheDocument();
    });
  });

  it("pasted JSON populates the preview", () => {
    renderBatchPanel();
    fireEvent.change(screen.getByLabelText("Batch issues JSON"), {
      target: { value: validJSON },
    });

    expect(screen.getByText("Imported issue")).toBeInTheDocument();
  });

  it("renders line numbers for the JSON input", () => {
    renderBatchPanel();
    fireEvent.change(screen.getByLabelText("Batch issues JSON"), {
      target: { value: "{\n  \"issues\": []\n}" },
    });

    expect(screen.getByTestId("json-line-numbers").textContent?.trim().split(/\s+/)).toEqual([
      "1",
      "2",
      "3",
    ]);
  });

  it("resolves assignee and project IDs to names in the preview", () => {
    renderBatchPanel();
    fireEvent.change(screen.getByLabelText("Batch issues JSON"), {
      target: {
        value: JSON.stringify({
          issues: [
            {
              title: "Assigned import",
              status: "todo",
              assignee_type: "agent",
              assignee_id: "agent-1",
              project_id: "project-1",
            },
          ],
        }),
      },
    });

    expect(screen.getByText("Codex Agent")).toBeInTheDocument();
    expect(screen.getByText("Batch Project")).toBeInTheDocument();
    expect(screen.queryByText("agent-1")).not.toBeInTheDocument();
    expect(screen.queryByText("project-1")).not.toBeInTheDocument();
    expect(screen.getByText("Starts now")).toBeInTheDocument();
  });

  it("renders unknown assignee and project IDs without exposing IDs", () => {
    renderBatchPanel();
    fireEvent.change(screen.getByLabelText("Batch issues JSON"), {
      target: {
        value: JSON.stringify({
          issues: [
            {
              title: "Unknown references",
              status: "todo",
              assignee_type: "agent",
              assignee_id: "missing-agent",
              project_id: "missing-project",
            },
          ],
        }),
      },
    });

    const table = screen.getByRole("table");
    expect(within(table).getAllByText("Unknown")).toHaveLength(2);
    expect(within(table).queryByText(/missing-agent/)).not.toBeInTheDocument();
    expect(within(table).queryByText(/missing-project/)).not.toBeInTheDocument();
  });

  it("invalid JSON shows a parse error and does not call the API", () => {
    renderBatchPanel();
    fireEvent.change(screen.getByLabelText("Batch issues JSON"), {
      target: { value: "{ nope" },
    });

    expect(screen.getByText("Fix the JSON before creating issues")).toBeInTheDocument();
    expect(screen.getByText("JSON syntax")).toBeInTheDocument();
    expect(screen.getByText(/JSON could not be parsed:/)).toBeInTheDocument();
    expect(screen.getByText(/Check missing braces/)).toBeInTheDocument();
    expect(screen.queryByText(/Row - \/ json/)).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Create issues" })).toBeDisabled();
    expect(mockMutateAsync).not.toHaveBeenCalled();
  });

  it("unknown row fields show row-level validation errors", () => {
    renderBatchPanel();
    fireEvent.change(screen.getByLabelText("Batch issues JSON"), {
      target: { value: JSON.stringify({ issues: [{ title: "No priority", priority: "high" }] }) },
    });

    expect(screen.getByText(/Row 1 \/ priority/)).toBeInTheDocument();
  });

  it("Create issues validates and immediately creates when no agent tasks will run", async () => {
    const user = userEvent.setup();
    const onClose = vi.fn();
    renderBatchPanel(onClose);
    fireEvent.change(screen.getByLabelText("Batch issues JSON"), {
      target: { value: validJSON },
    });

    await user.click(screen.getByRole("button", { name: "Create issues" }));

    await waitFor(() => {
      expect(mockMutateAsync).toHaveBeenCalledWith({
        issues: [{ title: "Imported issue", status: "todo" }],
        validate_only: true,
      });
    });
    await waitFor(() => {
      expect(mockMutateAsync).toHaveBeenLastCalledWith({
        issues: [{ title: "Imported issue", status: "todo" }],
        confirm_batch_create: true,
      });
    });
    expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument();
    expect(onClose).toHaveBeenCalled();
  });

  it("confirmation sends confirm_batch_create when agent tasks may run", async () => {
    const user = userEvent.setup();
    const onClose = vi.fn();
    mockMutateAsync.mockImplementation(async (payload) => {
      if (payload.validate_only) {
        return {
          valid: true,
          limit: 1000,
          row_count: payload.issues.length,
          agent_task_count: 1,
          rows: payload.issues.map((issue: any, index: number) => ({
            row: index + 1,
            title: issue.title,
            status: issue.status ?? "todo",
            assignee_type: "agent",
            assignee_id: "agent-id",
            project_id: null,
            will_enqueue_agent_task: true,
          })),
        };
      }
      return {
        valid: true,
        created: payload.issues.length,
        row_count: payload.issues.length,
        agent_task_count: 1,
        issues: [],
        warnings: [],
      };
    });
    renderBatchPanel(onClose);
    fireEvent.change(screen.getByLabelText("Batch issues JSON"), {
      target: { value: validJSON },
    });

    await user.click(screen.getByRole("button", { name: "Create issues" }));
    await screen.findByRole("alertdialog");
    await user.click(screen.getAllByRole("button", { name: "Create issues" }).at(-1)!);

    await waitFor(() => {
      expect(mockMutateAsync).toHaveBeenLastCalledWith({
        issues: [{ title: "Imported issue", status: "todo" }],
        confirm_batch_create: true,
      });
    });
    expect(mockToastSuccess).toHaveBeenCalledWith("Created 1 issues");
    expect(onClose).toHaveBeenCalled();
  });

  it("renders row errors returned by the server", async () => {
    const user = userEvent.setup();
    mockMutateAsync.mockRejectedValueOnce(
      new MockApiError({
        valid: false,
        limit: 1000,
        row_count: 1,
        agent_task_count: 0,
        errors: [
          {
            row: 1,
            field: "assignee_id",
            code: "assignee_not_found",
            message: "assignee_id does not refer to a member of this workspace",
          },
        ],
      }),
    );

    renderBatchPanel();
    fireEvent.change(screen.getByLabelText("Batch issues JSON"), {
      target: { value: validJSON },
    });
    await user.click(screen.getByRole("button", { name: "Create issues" }));

    await waitFor(() => {
      expect(screen.getByText(/assignee_id does not refer/)).toBeInTheDocument();
    });
  });
});

describe("CreateModeSelector", () => {
  it("switches between manual, agent, and batch", async () => {
    const user = userEvent.setup();
    const onSelect = vi.fn();
    render(<CreateModeSelector mode="manual" onSelect={onSelect} />);

    await user.click(screen.getByRole("tab", { name: "Agent" }));
    await user.click(screen.getByRole("tab", { name: "Batch" }));

    expect(onSelect).toHaveBeenNthCalledWith(1, "agent");
    expect(onSelect).toHaveBeenNthCalledWith(2, "batch");
  });

  it("keeps only the active mode tab in the tab order", () => {
    render(<CreateModeSelector mode="batch" onSelect={vi.fn()} />);

    expect(screen.getByRole("tab", { name: "Manual" })).toHaveAttribute("tabindex", "-1");
    expect(screen.getByRole("tab", { name: "Agent" })).toHaveAttribute("tabindex", "-1");
    expect(screen.getByRole("tab", { name: "Batch" })).toHaveAttribute("tabindex", "0");
  });

  it("supports arrow-key mode switching from the active tab", () => {
    const onSelect = vi.fn();
    render(<CreateModeSelector mode="batch" onSelect={onSelect} />);

    fireEvent.keyDown(screen.getByRole("tab", { name: "Batch" }), { key: "ArrowLeft" });

    expect(onSelect).toHaveBeenCalledWith("agent");
  });
});
