import { forwardRef, useImperativeHandle, useRef, useState } from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

const mockPush = vi.hoisted(() => vi.fn());
const mockCreateIssue = vi.hoisted(() => vi.fn());
const mockSetDraft = vi.hoisted(() => vi.fn());
const mockClearDraft = vi.hoisted(() => vi.fn());
const mockToastCustom = vi.hoisted(() => vi.fn());
const mockToastDismiss = vi.hoisted(() => vi.fn());
const mockToastError = vi.hoisted(() => vi.fn());

const mockDraftStore = {
  draft: {
    title: "",
    description: "",
    status: "todo" as const,
    priority: "none" as const,
    assigneeType: undefined,
    assigneeId: undefined,
    dueDate: null,
  },
  setDraft: mockSetDraft,
  clearDraft: mockClearDraft,
};

vi.mock("../navigation", () => ({
  useNavigation: () => ({ push: mockPush }),
}));

vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => ({ name: "Test Workspace" }),
  useWorkspacePaths: () => ({
    issueDetail: (id: string) => `/ws-test/issues/${id}`,
  }),
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-test",
}));

vi.mock("@multica/core/issues/queries", () => ({
  issueDetailOptions: (wsId: string, id: string) => ({
    queryKey: ["issues", wsId, "detail", id],
    queryFn: () => Promise.resolve(null),
  }),
}));

vi.mock("@multica/core/issues/stores/draft-store", () => ({
  useIssueDraftStore: Object.assign(
    (selector?: (state: typeof mockDraftStore) => unknown) =>
      (selector ? selector(mockDraftStore) : mockDraftStore),
    { getState: () => mockDraftStore },
  ),
}));

vi.mock("@multica/core/issues/mutations", () => ({
  useCreateIssue: () => ({ mutateAsync: mockCreateIssue }),
  useUpdateIssue: () => ({ mutate: vi.fn() }),
}));

vi.mock("@multica/core/hooks/use-file-upload", () => ({
  useFileUpload: () => ({ uploadWithToast: vi.fn() }),
}));

vi.mock("@multica/core/api", () => ({
  api: {},
}));

vi.mock("../editor", () => {
  const ContentEditor = forwardRef(({ defaultValue, onUpdate, placeholder }: any, ref: any) => {
    const valueRef = useRef(defaultValue || "");
    const [value, setValue] = useState(defaultValue || "");
    useImperativeHandle(ref, () => ({
      getMarkdown: () => valueRef.current,
      uploadFile: vi.fn(),
    }));
    return (
      <textarea
        value={value}
        placeholder={placeholder}
        onChange={(e) => {
          valueRef.current = e.target.value;
          setValue(e.target.value);
          onUpdate?.(e.target.value);
        }}
      />
    );
  });
  ContentEditor.displayName = "ContentEditor";

  return {
    useFileDropZone: () => ({ isDragOver: false, dropZoneProps: {} }),
    FileDropOverlay: () => null,
    ContentEditor,
    TitleEditor: ({ defaultValue, placeholder, onChange, onSubmit }: any) => {
      const [value, setValue] = useState(defaultValue || "");
      return (
        <input
          value={value}
          placeholder={placeholder}
          onChange={(e) => {
            setValue(e.target.value);
            onChange?.(e.target.value);
          }}
          onKeyDown={(e) => {
            if (e.key === "Enter") onSubmit?.();
          }}
        />
      );
    },
  };
});

vi.mock("../issues/components", () => ({
  StatusIcon: ({ status }: { status: string }) => <span data-testid="status-icon">{status}</span>,
  StatusPicker: () => <div data-testid="status-picker" />,
  PriorityPicker: () => <div data-testid="priority-picker" />,
  AssigneePicker: () => <div data-testid="assignee-picker" />,
  DueDatePicker: () => <div data-testid="due-date-picker" />,
}));

vi.mock("../projects/components/project-picker", () => ({
  ProjectPicker: () => <div data-testid="project-picker" />,
}));

vi.mock("@multica/ui/components/ui/dialog", () => ({
  Dialog: ({ children }: { children: React.ReactNode }) => <div data-testid="dialog-root">{children}</div>,
  DialogContent: ({ children, className }: { children: React.ReactNode; className?: string }) => (
    <div className={className}>{children}</div>
  ),
  DialogTitle: ({ children, className }: { children: React.ReactNode; className?: string }) => (
    <div className={className}>{children}</div>
  ),
}));

vi.mock("@multica/ui/components/ui/dropdown-menu", () => ({
  DropdownMenu: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  DropdownMenuTrigger: ({ render }: { render: React.ReactNode }) => <>{render}</>,
  DropdownMenuContent: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  DropdownMenuItem: ({ children, onClick }: { children: React.ReactNode; onClick?: () => void }) => (
    <button type="button" onClick={onClick}>{children}</button>
  ),
  DropdownMenuSeparator: () => null,
}));

vi.mock("./issue-picker-modal", () => ({
  IssuePickerModal: () => null,
}));

vi.mock("@multica/ui/components/ui/tooltip", () => ({
  Tooltip: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  TooltipTrigger: ({ render }: { render: React.ReactNode }) => <>{render}</>,
  TooltipContent: ({ children }: { children: React.ReactNode }) => <>{children}</>,
}));

vi.mock("@multica/ui/components/ui/button", () => ({
  Button: ({
    children,
    disabled,
    onClick,
    type = "button",
  }: {
    children: React.ReactNode;
    disabled?: boolean;
    onClick?: () => void;
    type?: "button" | "submit" | "reset";
  }) => (
    <button type={type} disabled={disabled} onClick={onClick}>
      {children}
    </button>
  ),
}));

vi.mock("@multica/ui/components/common/file-upload-button", () => ({
  FileUploadButton: ({ onSelect }: { onSelect: (file: File) => void }) => (
    <button type="button" onClick={() => onSelect(new File(["test"], "test.txt"))}>
      Upload file
    </button>
  ),
}));

vi.mock("@multica/ui/lib/utils", () => ({
  cn: (...values: Array<string | false | null | undefined>) => values.filter(Boolean).join(" "),
}));

vi.mock("sonner", () => ({
  toast: {
    custom: mockToastCustom,
    dismiss: mockToastDismiss,
    error: mockToastError,
  },
}));

import { CreateIssueModal } from "./create-issue";

function renderModal(element: React.ReactElement) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(<QueryClientProvider client={qc}>{element}</QueryClientProvider>);
}

describe("CreateIssueModal", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockCreateIssue.mockResolvedValue({
      id: "issue-123",
      identifier: "TES-123",
      title: "Ship create issue regression coverage",
      status: "todo",
    });
  });

  it("shows success feedback with a direct path to the new issue", async () => {
    const user = userEvent.setup();
    const onClose = vi.fn();

    renderModal(<CreateIssueModal onClose={onClose} />);

    await user.type(screen.getByPlaceholderText("Issue title"), "  Ship create issue regression coverage  ");
    await user.click(screen.getByRole("button", { name: /Create Issue/ }));

    await waitFor(() => {
      expect(mockCreateIssue).toHaveBeenCalledWith({
        title: "Ship create issue regression coverage",
        description: undefined,
        status: "todo",
        priority: "none",
        assignee_type: undefined,
        assignee_id: undefined,
        due_date: undefined,
        attachment_ids: undefined,
        parent_issue_id: undefined,
        project_id: undefined,
      });
    });

    expect(mockClearDraft).toHaveBeenCalled();
    expect(onClose).toHaveBeenCalled();
    expect(mockToastCustom).toHaveBeenCalledTimes(1);

    const renderToast = mockToastCustom.mock.calls[0]?.[0];
    expect(typeof renderToast).toBe("function");

    render(renderToast("toast-1"));

    expect(screen.getByText("Issue created")).toBeInTheDocument();
    expect(screen.getByText(/TES-123/)).toBeInTheDocument();
    expect(screen.getByText(/Ship create issue regression coverage/)).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "View issue" }));

    expect(mockPush).toHaveBeenCalledWith("/ws-test/issues/issue-123");
    expect(mockToastDismiss).toHaveBeenCalledWith("toast-1");
  });
});
