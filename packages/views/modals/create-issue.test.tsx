import { forwardRef, useImperativeHandle, useRef, useState, type ReactNode } from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../locales/en/common.json";
import enModals from "../locales/en/modals.json";

const TEST_RESOURCES = {
  en: { common: enCommon, modals: enModals },
};

function I18nWrapper({ children }: { children: ReactNode }) {
  return (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      {children}
    </I18nProvider>
  );
}

const mockPush = vi.hoisted(() => vi.fn());
const mockCreateIssue = vi.hoisted(() => vi.fn());
const mockUpdateIssue = vi.hoisted(() => vi.fn());
const mockSetDraft = vi.hoisted(() => vi.fn());
const mockClearDraft = vi.hoisted(() => vi.fn());
const mockSetLastAssignee = vi.hoisted(() => vi.fn());
const mockSetKeepOpen = vi.hoisted(() => vi.fn());
const mockCreateLabel = vi.hoisted(() => vi.fn());
const mockToastCustom = vi.hoisted(() => vi.fn());
const mockToastDismiss = vi.hoisted(() => vi.fn());
const mockToastError = vi.hoisted(() => vi.fn());
const mockUploadWithToast = vi.hoisted(() => vi.fn());
const mockUploadState = vi.hoisted(() => ({ uploading: false }));
const mockProjects = vi.hoisted(() => ({
  list: [{ id: "proj-1", title: "Default Project" }],
}));

const mockDraftStore = {
  draft: {
    title: "",
    description: "",
    status: "todo" as const,
    priority: "none" as const,
    assigneeType: undefined as "agent" | "squad" | "member" | undefined,
    assigneeId: undefined as string | undefined,
    startDate: null,
    dueDate: null,
    attachments: [] as Array<{
      id: string;
      workspace_id: string;
      issue_id: string | null;
      comment_id: string | null;
      chat_session_id: string | null;
      chat_message_id: string | null;
      uploader_type: string;
      uploader_id: string;
      filename: string;
      url: string;
      download_url: string;
      markdown_url: string;
      content_type: string;
      size_bytes: number;
      created_at: string;
    }>,
  },
  lastAssigneeType: undefined as "agent" | "squad" | "member" | undefined,
  lastAssigneeId: undefined as string | undefined,
  setDraft: mockSetDraft,
  clearDraft: mockClearDraft,
  setLastAssignee: mockSetLastAssignee,
};

const mockQuickCreateStore = {
  keepOpen: false,
  setKeepOpen: mockSetKeepOpen,
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

vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector?: (state: { user: { id: string } }) => unknown) =>
    (selector ? selector({ user: { id: "user-1" } }) : { user: { id: "user-1" } }),
}));

vi.mock("@multica/core/issues/queries", () => ({
  issueDetailOptions: (wsId: string, id: string) => ({
    queryKey: ["issues", wsId, "detail", id],
    queryFn: () => Promise.resolve(null),
  }),
}));

vi.mock("@multica/core/projects/queries", () => ({
  projectListOptions: () => ({
    queryKey: ["projects"],
    queryFn: () => Promise.resolve(mockProjects.list),
    initialData: mockProjects.list,
  }),
}));

vi.mock("@multica/core/labels", () => ({
  labelListOptions: (wsId: string, scope?: { projectId?: string | null }) => ({
    queryKey: ["labels", wsId, "list", scope?.projectId ? "project" : "workspace", scope?.projectId ?? null],
    queryFn: () =>
      Promise.resolve([
        { id: "label-1", workspace_id: wsId, project_id: null, name: "bug", color: "#ef4444" },
        { id: "label-2", workspace_id: wsId, project_id: null, name: "feature", color: "#22c55e" },
      ]),
  }),
  useCreateLabel: () => ({ mutate: mockCreateLabel, isPending: false }),
}));

vi.mock("@multica/core/issues/stores/draft-store", () => ({
  useIssueDraftStore: Object.assign(
    (selector?: (state: typeof mockDraftStore) => unknown) =>
      (selector ? selector(mockDraftStore) : mockDraftStore),
    { getState: () => mockDraftStore },
  ),
}));

vi.mock("@multica/core/issues/stores/quick-create-store", () => ({
  useQuickCreateStore: (selector?: (state: typeof mockQuickCreateStore) => unknown) =>
    (selector ? selector(mockQuickCreateStore) : mockQuickCreateStore),
}));

vi.mock("@multica/core/issues/mutations", () => ({
  useCreateIssue: () => ({ mutateAsync: mockCreateIssue }),
  useUpdateIssue: () => ({ mutate: vi.fn(), mutateAsync: mockUpdateIssue }),
}));

vi.mock("@multica/core/hooks/use-file-upload", () => ({
  useFileUpload: (_api: unknown, onError?: (error: Error) => void) => ({
    uploadWithToast: async (...args: unknown[]) => {
      try {
        return await mockUploadWithToast(...args);
      } catch (err) {
        onError?.(err instanceof Error ? err : new Error("Upload failed"));
        return null;
      }
    },
    uploading: mockUploadState.uploading,
  }),
}));

// Hoisted ApiError class so both the vi.mock factory and the tests below
// can construct/instanceof-check the same identity. vi.mock is hoisted, so
// a normal `class` declaration above it would still be in the TDZ at mock
// evaluation time.
const { ApiError } = vi.hoisted(() => {
  class ApiErrorImpl extends Error {
    readonly status: number;
    readonly statusText: string;
    readonly body?: unknown;
    constructor(message: string, status: number, statusText: string, body?: unknown) {
      super(message);
      this.name = "ApiError";
      this.status = status;
      this.statusText = statusText;
      this.body = body;
    }
  }
  return { ApiError: ApiErrorImpl };
});

vi.mock("@multica/core/api", async () => {
  // Pull real `parseWithFallback` + `DuplicateIssueErrorBodySchema` from the
  // schema modules so the drift-fallback branch in create-issue.tsx runs the
  // actual validation logic (not a stub). Only `ApiError` is local — the
  // component imports it from this module and the cross-realm `instanceof`
  // check requires a single class identity.
  const { parseWithFallback } = await vi.importActual<typeof import("@multica/core/api/schema")>(
    "@multica/core/api/schema",
  );
  const { DuplicateIssueErrorBodySchema } = await vi.importActual<
    typeof import("@multica/core/api/schemas")
  >("@multica/core/api/schemas");
  return {
    api: {},
    ApiError,
    parseWithFallback,
    DuplicateIssueErrorBodySchema,
  };
});

vi.mock("../editor", () => {
  const ContentEditor = forwardRef(({ defaultValue, onUpdate, onUploadFile, placeholder, attachments }: any, ref: any) => {
    const valueRef = useRef(defaultValue || "");
    const [value, setValue] = useState(defaultValue || "");
    useImperativeHandle(ref, () => ({
      getMarkdown: () => valueRef.current,
      clearContent: () => {
        valueRef.current = "";
        setValue("");
      },
      uploadFile: (file: File) => onUploadFile?.(file),
      uploadFiles: vi.fn(),
      hasActiveUploads: () => false,
    }));
    return (
      <>
        <textarea
          value={value}
          placeholder={placeholder}
          data-attachments-count={attachments?.length ?? 0}
          onChange={(e) => {
            valueRef.current = e.target.value;
            setValue(e.target.value);
            onUpdate?.(e.target.value);
          }}
          onPaste={(e) => {
            const file = Array.from(e.clipboardData?.files ?? [])[0];
            if (file) void onUploadFile?.(file);
          }}
        />
        {onUploadFile && (
          <button type="button" onClick={() => onUploadFile(new File(["test"], "editor-test.txt"))}>
            Editor upload file
          </button>
        )}
      </>
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
  // Surface open/onOpenChange so tests can assert progressive-disclosure
  // behavior (mounted only when the user has opted in or has a value).
  StartDatePicker: ({ open, onOpenChange }: { open?: boolean; onOpenChange?: (v: boolean) => void }) => (
    <div
      data-testid="start-date-picker"
      data-open={open ? "true" : "false"}
      onClick={() => onOpenChange?.(false)}
    />
  ),
  DueDatePicker: () => <div data-testid="due-date-picker" />,
}));

vi.mock("../projects/components/project-picker", () => ({
  ProjectPicker: ({ projectId }: { projectId: string | null }) => (
    <div data-testid="project-picker" data-project-id={projectId ?? ""} />
  ),
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

vi.mock("../issues/components/label-scope-segment", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../issues/components/label-scope-segment")>();
  return {
    ...actual,
    LabelScopeSegment: ({
      value,
      onValueChange,
      projectLabel,
      workspaceLabel,
    }: {
      value: "project" | "workspace";
      onValueChange: (value: "project" | "workspace") => void;
      projectLabel: string;
      workspaceLabel: string;
    }) => (
      <div>
        <button
          type="button"
          aria-pressed={value === "project"}
          onClick={() => onValueChange("project")}
        >
          {projectLabel}
        </button>
        <button
          type="button"
          aria-pressed={value === "workspace"}
          onClick={() => onValueChange("workspace")}
        >
          {workspaceLabel}
        </button>
      </div>
    ),
  };
});

vi.mock("./issue-picker-modal", () => ({
  IssuePickerModal: () => null,
}));

vi.mock("@multica/ui/components/ui/tooltip", () => ({
  Tooltip: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  TooltipTrigger: ({ render }: { render: React.ReactNode }) => <>{render}</>,
  TooltipContent: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  TooltipProvider: ({ children }: { children: React.ReactNode }) => <>{children}</>,
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

vi.mock("@multica/ui/components/ui/switch", () => ({
  Switch: ({
    checked,
    onCheckedChange,
  }: {
    checked: boolean;
    onCheckedChange: (v: boolean) => void;
  }) => (
    <input
      aria-label="Create another"
      type="checkbox"
      checked={checked}
      onChange={(e) => onCheckedChange(e.target.checked)}
    />
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

import { CreateIssueModal, ManualCreatePanel } from "./create-issue";

function renderModal(element: React.ReactElement) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <I18nWrapper>
      <QueryClientProvider client={qc}>{element}</QueryClientProvider>
    </I18nWrapper>,
  );
}

describe("CreateIssueModal", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockQuickCreateStore.keepOpen = false;
    mockProjects.list = [{ id: "proj-1", title: "Default Project" }];
    mockDraftStore.draft = {
      title: "",
      description: "",
      status: "todo",
      priority: "none",
      assigneeType: undefined,
      assigneeId: undefined,
      startDate: null,
      dueDate: null,
      attachments: [],
    };
    mockSetKeepOpen.mockImplementation((v: boolean) => {
      mockQuickCreateStore.keepOpen = v;
    });
    mockCreateLabel.mockImplementation(
      (
        data: { name: string; color: string; project_id?: string | null },
        opts?: { onSuccess?: (label: { id: string; workspace_id: string; project_id: string | null; name: string; color: string }) => void },
      ) => {
        opts?.onSuccess?.({
          id: "label-new",
          workspace_id: "ws-test",
          project_id: data.project_id ?? null,
          name: data.name,
          color: data.color,
        });
      },
    );
    // Reset the shared draft mock so per-test assignee seeding (squad / agent)
    // doesn't leak into the next test in the suite.
    mockDraftStore.draft.assigneeType = undefined;
    mockDraftStore.draft.assigneeId = undefined;
    mockDraftStore.draft.attachments = [];
    mockDraftStore.lastAssigneeType = undefined;
    mockDraftStore.lastAssigneeId = undefined;
    mockSetDraft.mockImplementation((patch: Partial<typeof mockDraftStore.draft>) => {
      mockDraftStore.draft = { ...mockDraftStore.draft, ...patch };
    });
    mockSetLastAssignee.mockImplementation((type, id) => {
      mockDraftStore.lastAssigneeType = type;
      mockDraftStore.lastAssigneeId = id;
    });
    mockClearDraft.mockImplementation(() => {
      mockDraftStore.draft = {
        title: "",
        description: "",
        status: "todo",
        priority: "none",
        assigneeType: mockDraftStore.lastAssigneeType,
        assigneeId: mockDraftStore.lastAssigneeId,
        startDate: null,
        dueDate: null,
        attachments: [],
      };
    });
    mockCreateIssue.mockResolvedValue({
      id: "issue-123",
      identifier: "TES-123",
      title: "Ship create issue regression coverage",
      status: "todo",
    });
    mockUpdateIssue.mockResolvedValue({});
    mockUploadWithToast.mockResolvedValue(null);
    mockUploadState.uploading = false;
  });

  it("shows success feedback with a direct path to the new issue", async () => {
    const user = userEvent.setup();
    const onClose = vi.fn();

    renderModal(<CreateIssueModal onClose={onClose} />);

    fireEvent.change(screen.getByPlaceholderText("Issue title"), {
      target: { value: "  Ship create issue regression coverage  " },
    });
    await user.click(screen.getByRole("button", { name: "Create Issue" }));

    await waitFor(() => {
      expect(mockCreateIssue).toHaveBeenCalledWith({
        title: "Ship create issue regression coverage",
        description: undefined,
        status: "todo",
        priority: "none",
        assignee_type: "member",
        assignee_id: "user-1",
        start_date: undefined,
        due_date: undefined,
        attachment_ids: undefined,
        parent_issue_id: undefined,
        project_id: "proj-1",
        label_ids: undefined,
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

    expect(mockPush).toHaveBeenCalledWith("/ws-test/issues/TES-123");
    expect(mockToastDismiss).toHaveBeenCalledWith("toast-1");
  });

  it("prefills title and description from modal data without writing them to the draft", () => {
    renderModal(
      <CreateIssueModal
        onClose={vi.fn()}
        data={{
          title: "Original issue title",
          description: "Original issue body",
          project_id: "proj-1",
          parent_issue_id: "issue-parent",
        }}
      />,
    );

    expect(screen.getByPlaceholderText("Issue title")).toHaveValue("Original issue title");
    expect(screen.getByPlaceholderText("Add description...")).toHaveValue("Original issue body");
    expect(screen.getByTestId("project-picker")).toHaveAttribute("data-project-id", "proj-1");
    expect(mockSetDraft).not.toHaveBeenCalledWith(
      expect.objectContaining({
        title: "Original issue title",
      }),
    );
    expect(mockSetDraft).not.toHaveBeenCalledWith(
      expect.objectContaining({
        description: "Original issue body",
      }),
    );
  });

  it("creates from modal data as a child issue and blocks the source issue after create", async () => {
    const user = userEvent.setup();

    renderModal(
      <CreateIssueModal
        onClose={vi.fn()}
        data={{
          title: "Original issue title",
          description: "Original issue body",
          project_id: "proj-1",
          parent_issue_id: "issue-parent",
          block_issue_id_on_create: "issue-parent",
        }}
      />,
    );

    await user.click(screen.getByRole("button", { name: "Create Issue" }));

    await waitFor(() => {
      expect(mockCreateIssue).toHaveBeenCalledWith(
        expect.objectContaining({
          title: "Original issue title",
          description: "Original issue body",
          project_id: "proj-1",
          parent_issue_id: "issue-parent",
        }),
      );
    });
    expect(mockUpdateIssue).toHaveBeenCalledWith({
      id: "issue-parent",
      status: "blocked",
    });
  });

  it("does not block the source issue when create fails", async () => {
    const user = userEvent.setup();
    mockCreateIssue.mockRejectedValue(new Error("create failed"));

    renderModal(
      <CreateIssueModal
        onClose={vi.fn()}
        data={{
          title: "Original issue title",
          project_id: "proj-1",
          parent_issue_id: "issue-parent",
          block_issue_id_on_create: "issue-parent",
        }}
      />,
    );

    await user.click(screen.getByRole("button", { name: "Create Issue" }));

    await waitFor(() => expect(mockToastError).toHaveBeenCalledWith("create failed"));
    expect(mockUpdateIssue).not.toHaveBeenCalled();
  });

  it("shows a warning when blocking the source issue fails after create succeeds", async () => {
    const user = userEvent.setup();
    mockUpdateIssue.mockRejectedValue(new Error("block failed"));

    renderModal(
      <CreateIssueModal
        onClose={vi.fn()}
        data={{
          title: "Original issue title",
          project_id: "proj-1",
          parent_issue_id: "issue-parent",
          block_issue_id_on_create: "issue-parent",
        }}
      />,
    );

    await user.click(screen.getByRole("button", { name: "Create Issue" }));

    await waitFor(() => expect(mockCreateIssue).toHaveBeenCalled());
    expect(mockUpdateIssue).toHaveBeenCalledWith({
      id: "issue-parent",
      status: "blocked",
    });
    expect(mockToastError).toHaveBeenCalledWith("block failed");
  });

  it("keeps manual mode open and clears content when create another is enabled", async () => {
    const user = userEvent.setup();
    const onClose = vi.fn();
    mockQuickCreateStore.keepOpen = true;
    mockDraftStore.draft.assigneeType = "member";
    mockDraftStore.draft.assigneeId = "alice";

    renderModal(<CreateIssueModal onClose={onClose} />);

    await user.type(screen.getByPlaceholderText("Issue title"), "First follow-up issue");
    await user.type(screen.getByPlaceholderText("Add description..."), "Description to clear");
    await user.click(screen.getByRole("button", { name: "Create Issue" }));

    await waitFor(() => {
      expect(mockCreateIssue).toHaveBeenCalledWith({
        title: "First follow-up issue",
        description: "Description to clear",
        status: "todo",
        priority: "none",
        assignee_type: "member",
        assignee_id: "alice",
        start_date: undefined,
        due_date: undefined,
        attachment_ids: undefined,
        parent_issue_id: undefined,
        project_id: "proj-1",
        label_ids: undefined,
      });
    });

    expect(onClose).not.toHaveBeenCalled();
    expect(screen.getByPlaceholderText("Issue title")).toHaveValue("");
    expect(screen.getByPlaceholderText("Add description...")).toHaveValue("");
    expect(mockSetDraft).toHaveBeenCalledWith({
      title: "",
      description: "",
      status: "todo",
      priority: "none",
      assigneeType: "member",
      assigneeId: "alice",
      startDate: null,
      dueDate: null,
    });
  });

  // Manual → agent must also forward the picked squad. Without this branch
  // the agent panel silently falls back to the persisted actor / first
  // visible agent and the user loses the squad they just chose in manual.
  it("forwards the picked squad when switching to agent mode", async () => {
    mockDraftStore.draft.assigneeType = "squad";
    mockDraftStore.draft.assigneeId = "squad-1";
    const user = userEvent.setup();
    const onSwitchMode = vi.fn();

    renderModal(
      <ManualCreatePanel
        onClose={vi.fn()}
        onSwitchMode={onSwitchMode}
        isExpanded={false}
        setIsExpanded={vi.fn()}
        backlogHintIssueId={null}
        setBacklogHintIssueId={vi.fn()}
      />,
    );

    await user.type(screen.getByPlaceholderText("Issue title"), "Refactor auth");
    await user.click(screen.getByRole("button", { name: /Switch to Agent/i }));

    expect(onSwitchMode).toHaveBeenCalledTimes(1);
    const carry = onSwitchMode.mock.calls[0]?.[0];
    expect(carry).toEqual(
      expect.objectContaining({ prompt: "Refactor auth", squad_id: "squad-1" }),
    );
    expect(carry).not.toHaveProperty("agent_id");
  });

  it("blocks manual create when multiple projects exist and none is selected", async () => {
    const user = userEvent.setup();
    mockProjects.list = [
      { id: "proj-1", title: "Project One" },
      { id: "proj-2", title: "Project Two" },
    ];

    renderModal(<CreateIssueModal onClose={vi.fn()} />);

    await user.type(screen.getByPlaceholderText("Issue title"), "Needs a project");
    await user.click(screen.getByRole("button", { name: "Create Issue" }));

    expect(mockCreateIssue).not.toHaveBeenCalled();
    expect(mockToastError).toHaveBeenCalledWith("Select a project before creating an issue.");
  });

  it("rejects an upload result with an empty attachment id before submit", async () => {
    const user = userEvent.setup();
    mockUploadWithToast.mockResolvedValue({
      id: "",
      url: "https://cdn.example.test/orphan.txt",
      download_url: "https://cdn.example.test/orphan.txt",
      filename: "orphan.txt",
      link: "https://cdn.example.test/orphan.txt",
    });

    renderModal(<CreateIssueModal onClose={vi.fn()} />);

    await user.click(screen.getByRole("button", { name: "Editor upload file" }));
    await user.type(screen.getByPlaceholderText("Issue title"), "Do not submit invalid attachment");
    await user.click(screen.getByRole("button", { name: "Create Issue" }));

    expect(mockUploadWithToast).toHaveBeenCalledTimes(1);
    expect(mockToastError).toHaveBeenCalledWith("Attachment upload failed. Try again.");
    expect(mockCreateIssue).toHaveBeenCalledWith(expect.objectContaining({
      attachment_ids: undefined,
    }));
  });

  it("submits uploaded UUIDv7 attachment ids when creating an issue", async () => {
    const user = userEvent.setup();
    const attachmentId = "019e5d0e-6f02-7335-8e7f-8276f3f410df";
    mockUploadWithToast.mockResolvedValue({
      id: attachmentId,
      url: "https://cdn.example.test/uploaded.txt",
      download_url: "https://cdn.example.test/uploaded.txt",
      filename: "uploaded.txt",
      link: "https://cdn.example.test/uploaded.txt",
    });

    renderModal(<CreateIssueModal onClose={vi.fn()} />);

    await user.click(screen.getByRole("button", { name: "Editor upload file" }));
    await user.type(screen.getByPlaceholderText("Issue title"), "Submit UUIDv7 attachment");
    await user.click(screen.getByRole("button", { name: "Create Issue" }));

    await waitFor(() => {
      expect(mockCreateIssue).toHaveBeenCalledWith(expect.objectContaining({
        attachment_ids: [attachmentId],
      }));
    });
    expect(mockToastError).not.toHaveBeenCalledWith("Attachment upload failed. Try again.");
  });

  it("shows an upload error when the manual create attachment upload fails", async () => {
    const user = userEvent.setup();
    mockUploadWithToast.mockRejectedValue(new Error("upload failed"));

    renderModal(<CreateIssueModal onClose={vi.fn()} />);

    await user.click(screen.getByRole("button", { name: "Editor upload file" }));

    await waitFor(() => {
      expect(mockToastError).toHaveBeenCalledWith("Attachment upload failed. Try again.");
    });
    expect(mockCreateIssue).not.toHaveBeenCalled();
  });

  it("shows an upload error when a pasted image upload fails", async () => {
    mockUploadWithToast.mockRejectedValue(new Error("upload failed"));

    renderModal(<CreateIssueModal onClose={vi.fn()} />);

    fireEvent.paste(screen.getByPlaceholderText("Add description..."), {
      clipboardData: {
        files: [new File(["image"], "pasted-image.png", { type: "image/png" })],
      },
    });

    await waitFor(() => {
      expect(mockUploadWithToast).toHaveBeenCalledWith(
        expect.objectContaining({
          name: "pasted-image.png",
          type: "image/png",
        }),
      );
      expect(mockToastError).toHaveBeenCalledWith("Attachment upload failed. Try again.");
    });
    expect(mockCreateIssue).not.toHaveBeenCalled();
  });

  it("blocks manual create while an attachment upload is still running", async () => {
    const user = userEvent.setup();
    mockUploadState.uploading = true;

    renderModal(<CreateIssueModal onClose={vi.fn()} />);

    await user.type(screen.getByPlaceholderText("Issue title"), "Wait for upload");
    await user.click(screen.getByRole("button", { name: "Create Issue" }));

    expect(mockCreateIssue).not.toHaveBeenCalled();
    expect(mockToastError).toHaveBeenCalledWith("Please wait for uploads to finish…");
  });

  it("sends selected existing labels in create request", async () => {
    const user = userEvent.setup();
    renderModal(<CreateIssueModal onClose={vi.fn()} />);

    await user.type(screen.getByPlaceholderText("Issue title"), "Issue with existing label");
    await user.click(screen.getByLabelText("Select labels"));
    await user.click(screen.getByRole("button", { name: /bug/i }));
    await user.click(screen.getByRole("button", { name: "Create Issue" }));

    await waitFor(() => {
      expect(mockCreateIssue).toHaveBeenCalledWith(
        expect.objectContaining({
          title: "Issue with existing label",
          label_ids: ["label-1"],
        }),
      );
    });
  });

  it("supports creating a new label inline and attaches it on create", async () => {
    const user = userEvent.setup();
    renderModal(<CreateIssueModal onClose={vi.fn()} />);

    await user.type(screen.getByPlaceholderText("Issue title"), "Issue with new label");
    await user.click(screen.getByLabelText("Select labels"));
    await user.type(screen.getByPlaceholderText("Search or type a new label"), "critical");
    await user.click(screen.getByRole("button", { name: /Create label "critical"/i }));
    await user.click(screen.getByRole("button", { name: "Create Issue" }));

    await waitFor(() => {
      expect(mockCreateLabel).toHaveBeenCalledWith(
        expect.objectContaining({ name: "critical" }),
        expect.any(Object),
      );
      expect(mockCreateIssue).toHaveBeenCalledWith(
        expect.objectContaining({
          title: "Issue with new label",
          label_ids: ["label-new"],
        }),
      );
    });
  });

  it("supports creating a workspace label from the project create flow", async () => {
    const user = userEvent.setup();
    renderModal(<CreateIssueModal onClose={vi.fn()} />);

    await user.type(screen.getByPlaceholderText("Issue title"), "Issue with workspace label");
    await user.click(screen.getByLabelText("Select labels"));
    await user.click(screen.getByRole("button", { name: "Workspace" }));
    await user.type(screen.getByPlaceholderText("Search or type a new label"), "global-risk");
    await user.click(screen.getByRole("button", { name: /Create label "global-risk"/i }));
    await user.click(screen.getByRole("button", { name: "Create Issue" }));

    await waitFor(() => {
      expect(mockCreateLabel).toHaveBeenCalledWith(
        expect.objectContaining({ name: "global-risk", project_id: null }),
        expect.any(Object),
      );
      expect(mockCreateIssue).toHaveBeenCalledWith(
        expect.objectContaining({ label_ids: ["label-new"] }),
      );
    });
  });

  // Manual → agent must forward the picked project so the new modal pins to
  // the same target. Without this the agent panel re-seeds from its own
  // persisted `lastProjectId` and silently routes the issue to a stale one.
  // Reporter scenario: backend rejects same-titled create with a 409 +
  // structured duplicate body. The user should land on a duplicate toast
  // pointing at the existing issue, not a generic "create failed" message.
  it("shows duplicate-issue toast with a working view-existing link", async () => {
    const user = userEvent.setup();
    const onClose = vi.fn();
    mockCreateIssue.mockRejectedValue(
      new ApiError("An active issue with this title already exists: MUL-7 – Login bug", 409, "Conflict", {
        code: "active_duplicate_issue",
        error: "An active issue with this title already exists: MUL-7 – Login bug",
        issue: {
          id: "issue-dup",
          identifier: "MUL-7",
          title: "Login bug",
        },
      }),
    );

    renderModal(<CreateIssueModal onClose={onClose} />);
    await user.type(screen.getByPlaceholderText("Issue title"), "Login bug");
    await user.click(screen.getByRole("button", { name: "Create Issue" }));

    await waitFor(() => expect(mockToastCustom).toHaveBeenCalledTimes(1));
    expect(mockToastError).not.toHaveBeenCalled();
    expect(onClose).not.toHaveBeenCalled();

    const renderToast = mockToastCustom.mock.calls[0]?.[0];
    expect(typeof renderToast).toBe("function");
    render(renderToast("toast-dup"));

    expect(screen.getByText("Duplicate issue")).toBeInTheDocument();
    expect(screen.getByText(/MUL-7/)).toBeInTheDocument();
    expect(screen.getByText(/Login bug/)).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "View existing issue" }));
    expect(mockPush).toHaveBeenCalledWith("/ws-test/issues/issue-dup");
    expect(mockToastDismiss).toHaveBeenCalledWith("toast-dup");
  });

  // Schema drift safety: server returns a 409 with a body that doesn't match
  // the duplicate schema (renamed code, missing issue object, etc.). UI must
  // not throw — it must fall back to a normal error toast carrying the
  // backend message so the user still sees a useful reason.
  it("falls back to a normal error toast when a 409 body does not match the duplicate schema", async () => {
    const user = userEvent.setup();
    mockCreateIssue.mockRejectedValue(
      new ApiError("Backend says title is taken", 409, "Conflict", {
        code: "renamed_duplicate_marker",
      }),
    );

    renderModal(<CreateIssueModal onClose={vi.fn()} />);
    await user.type(screen.getByPlaceholderText("Issue title"), "Login bug");
    await user.click(screen.getByRole("button", { name: "Create Issue" }));

    await waitFor(() => expect(mockToastError).toHaveBeenCalledTimes(1));
    expect(mockToastError).toHaveBeenCalledWith("Backend says title is taken");
    expect(mockToastCustom).not.toHaveBeenCalled();
  });

  // Non-409 errors with a real message: surface the backend reason rather
  // than the generic i18n fallback. This is the whole point of the issue.
  it("surfaces err.message verbatim for non-duplicate errors", async () => {
    const user = userEvent.setup();
    mockCreateIssue.mockRejectedValue(new Error("Server is overloaded, try again"));

    renderModal(<CreateIssueModal onClose={vi.fn()} />);
    await user.type(screen.getByPlaceholderText("Issue title"), "Anything");
    await user.click(screen.getByRole("button", { name: "Create Issue" }));

    await waitFor(() => expect(mockToastError).toHaveBeenCalledTimes(1));
    expect(mockToastError).toHaveBeenCalledWith("Server is overloaded, try again");
  });

  // Non-Error throws (string, plain object) have no `.message`. Fall back to
  // the i18n key so the user always sees something readable.
  it("falls back to the generic toast when the thrown value is not an Error", async () => {
    const user = userEvent.setup();
    mockCreateIssue.mockRejectedValue("network exploded");

    renderModal(<CreateIssueModal onClose={vi.fn()} />);
    await user.type(screen.getByPlaceholderText("Issue title"), "Anything");
    await user.click(screen.getByRole("button", { name: "Create Issue" }));

    await waitFor(() => expect(mockToastError).toHaveBeenCalledTimes(1));
    expect(mockToastError).toHaveBeenCalledWith("Failed to create issue");
  });

  it("forwards the picked project when switching to agent mode", async () => {
    const user = userEvent.setup();
    const onSwitchMode = vi.fn();

    renderModal(
      <ManualCreatePanel
        onClose={vi.fn()}
        onSwitchMode={onSwitchMode}
        data={{ project_id: "proj-1" }}
        isExpanded={false}
        setIsExpanded={vi.fn()}
        backlogHintIssueId={null}
        setBacklogHintIssueId={vi.fn()}
      />,
    );

    await user.type(screen.getByPlaceholderText("Issue title"), "Refactor auth");

    await user.click(screen.getByRole("button", { name: /Switch to Agent/i }));

    expect(onSwitchMode).toHaveBeenCalledTimes(1);
    expect(onSwitchMode.mock.calls[0]?.[0]).toEqual(
      expect.objectContaining({
        prompt: "Refactor auth",
        project_id: "proj-1",
      }),
    );
  });

  // Manual → agent must forward parent_issue_id when the modal was opened
  // from "Add sub issue". Before this, the agent panel received no parent
  // context and the new issue was filed as a standalone — silently dropping
  // the sub-issue intent set by openCreateSubIssue. The parent_issue_identifier
  // tags along so the agent panel can render a "Sub-issue of MUL-XX" chip
  // without an extra round-trip.
  //
  // The identifier fallback matters here: the mocked issueDetailOptions
  // resolves to null (parent query not hydrated), so without the
  // `data.parent_issue_identifier` fallback the agent chip would render as
  // "Sub-issue of " with an empty tail. The UUID alone still wires the
  // sub-issue relationship correctly, but the visible affordance breaks.
  it("forwards parent_issue_id and falls back to seeded identifier when switching to agent mode", async () => {
    const user = userEvent.setup();
    const onSwitchMode = vi.fn();

    renderModal(
      <ManualCreatePanel
        onClose={vi.fn()}
        onSwitchMode={onSwitchMode}
        data={{
          parent_issue_id: "parent-uuid-1",
          parent_issue_identifier: "MUL-2534",
        }}
        isExpanded={false}
        setIsExpanded={vi.fn()}
        backlogHintIssueId={null}
        setBacklogHintIssueId={vi.fn()}
      />,
    );

    await user.type(screen.getByPlaceholderText("Issue title"), "Refactor auth");
    await user.click(screen.getByRole("button", { name: /Switch to Agent/i }));

    expect(onSwitchMode).toHaveBeenCalledTimes(1);
    expect(onSwitchMode.mock.calls[0]?.[0]).toEqual(
      expect.objectContaining({
        prompt: "Refactor auth",
        parent_issue_id: "parent-uuid-1",
        parent_issue_identifier: "MUL-2534",
      }),
    );
  });

  // Start date is a low-frequency field — by default it lives behind the
  // ⋯ overflow menu and is not rendered inline. Clicking the overflow
  // entry opens it (and mounts the inline pill so the popover has an
  // anchor); closing without picking returns it to the menu-only state.
  it("hides start date behind the overflow menu and reveals it on demand", async () => {
    const user = userEvent.setup();

    renderModal(<CreateIssueModal onClose={vi.fn()} />);

    expect(screen.queryByTestId("start-date-picker")).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: /Set start date/i }));

    const picker = await screen.findByTestId("start-date-picker");
    expect(picker).toHaveAttribute("data-open", "true");

    await user.click(picker);

    expect(screen.queryByTestId("start-date-picker")).not.toBeInTheDocument();
  });

  // Title + description are packed into the agent prompt on switch; if we
  // leave them in the shared draft store, the next agent→manual switch
  // surfaces the stale manual draft on top of the prompt-as-description,
  // duplicating the user's text on every round-trip.
  it("clears the manual draft when packing title and description into the agent prompt", async () => {
    const user = userEvent.setup();

    renderModal(
      <ManualCreatePanel
        onClose={vi.fn()}
        onSwitchMode={vi.fn()}
        isExpanded={false}
        setIsExpanded={vi.fn()}
        backlogHintIssueId={null}
        setBacklogHintIssueId={vi.fn()}
      />,
    );

    await user.type(screen.getByPlaceholderText("Issue title"), "Update");
    await user.type(screen.getByPlaceholderText("Add description..."), "Some body");

    mockSetDraft.mockClear();
    await user.click(screen.getByRole("button", { name: /Switch to Agent/i }));

    expect(mockSetDraft).toHaveBeenCalledWith({ title: "", description: "" });
  });
});
