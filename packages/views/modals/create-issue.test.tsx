import type { InputHTMLAttributes, ReactNode, TextareaHTMLAttributes } from "react";
import { createContext, forwardRef, useContext, useImperativeHandle, useRef, useState } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

const mockPush = vi.hoisted(() => vi.fn());
const mockCreateIssue = vi.hoisted(() => vi.fn());
const mockSetDraft = vi.hoisted(() => vi.fn());
const mockClearDraft = vi.hoisted(() => vi.fn());
const mockToastCustom = vi.hoisted(() => vi.fn());
const mockToastDismiss = vi.hoisted(() => vi.fn());
const mockToastError = vi.hoisted(() => vi.fn());
const mockClarifyStructuredTask = vi.hoisted(() => vi.fn());
const mockCheckStructuredTaskClarity = vi.hoisted(() => vi.fn());
const mockCreateStructuredTaskTemplate = vi.hoisted(() => vi.fn());
const mockCreateStructuredTaskHistory = vi.hoisted(() => vi.fn());
const mockListStructuredTaskTemplates = vi.hoisted(() => vi.fn());
const mockListStructuredTaskHistory = vi.hoisted(() => vi.fn());

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

vi.mock("@multica/core/issues/stores/draft-store", () => ({
  useIssueDraftStore: Object.assign(
    (selector?: (state: typeof mockDraftStore) => unknown) =>
      selector ? selector(mockDraftStore) : mockDraftStore,
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
  api: {
    clarifyStructuredTask: mockClarifyStructuredTask,
    checkStructuredTaskClarity: mockCheckStructuredTaskClarity,
    createStructuredTaskTemplate: mockCreateStructuredTaskTemplate,
    createStructuredTaskHistory: mockCreateStructuredTaskHistory,
    listStructuredTaskTemplates: mockListStructuredTaskTemplates,
    listStructuredTaskHistory: mockListStructuredTaskHistory,
  },
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
        onChange={(event) => {
          valueRef.current = event.target.value;
          setValue(event.target.value);
          onUpdate?.(event.target.value);
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
          onChange={(event) => {
            setValue(event.target.value);
            onChange?.(event.target.value);
          }}
          onKeyDown={(event) => {
            if (event.key === "Enter") {
              onSubmit?.();
            }
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
  Dialog: ({ children }: { children: ReactNode }) => <div data-testid="dialog-root">{children}</div>,
  DialogContent: ({ children, className }: { children: ReactNode; className?: string }) => (
    <div className={className}>{children}</div>
  ),
  DialogTitle: ({ children, className }: { children: ReactNode; className?: string }) => (
    <div className={className}>{children}</div>
  ),
}));

vi.mock("@multica/ui/components/ui/tooltip", () => ({
  Tooltip: ({ children }: { children: ReactNode }) => <>{children}</>,
  TooltipTrigger: ({ render }: { render: ReactNode }) => <>{render}</>,
  TooltipContent: ({ children }: { children: ReactNode }) => <>{children}</>,
}));

vi.mock("@multica/ui/components/ui/button", () => ({
  Button: ({
    children,
    disabled,
    onClick,
    type = "button",
  }: {
    children: ReactNode;
    disabled?: boolean;
    onClick?: () => void;
    type?: "button" | "submit" | "reset";
  }) => (
    <button type={type} disabled={disabled} onClick={onClick}>
      {children}
    </button>
  ),
}));

vi.mock("@multica/ui/components/ui/input", () => ({
  Input: (props: InputHTMLAttributes<HTMLInputElement>) => <input {...props} />,
}));

vi.mock("@multica/ui/components/ui/textarea", () => ({
  Textarea: (props: TextareaHTMLAttributes<HTMLTextAreaElement>) => <textarea {...props} />,
}));

vi.mock("@multica/ui/components/ui/badge", () => ({
  Badge: ({ children }: { children: ReactNode }) => <span>{children}</span>,
}));

vi.mock("@multica/ui/components/ui/tabs", () => {
  const TabsContext = createContext<{ value: string; setValue: (value: string) => void } | null>(null);

  const Tabs = ({
    value,
    onValueChange,
    children,
  }: {
    value: string;
    onValueChange: (value: string) => void;
    children: ReactNode;
  }) => (
    <TabsContext.Provider value={{ value, setValue: onValueChange }}>
      <div>{children}</div>
    </TabsContext.Provider>
  );

  const TabsList = ({ children }: { children: ReactNode }) => <div>{children}</div>;

  const TabsTrigger = ({
    value,
    children,
  }: {
    value: string;
    children: ReactNode;
  }) => {
    const context = useContext(TabsContext);
    return (
      <button type="button" onClick={() => context?.setValue(value)}>
        {children}
      </button>
    );
  };

  const TabsContent = ({ value, children }: { value: string; children: ReactNode }) => {
    const context = useContext(TabsContext);
    return context?.value === value ? <div>{children}</div> : null;
  };

  return { Tabs, TabsList, TabsTrigger, TabsContent };
});

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

describe("CreateIssueModal", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockClarifyStructuredTask.mockResolvedValue({
      goal: "Build a launch checklist for design team",
      audience: ["design team"],
      output: "Deliverable: checklist doc.",
      constraints: [],
      style: [],
      open_questions: [],
    });
    mockCheckStructuredTaskClarity.mockResolvedValue({
      clarity_status: "clear",
      reason: ["Goal and Output are explicit enough to proceed."],
      suggestions: [],
    });
    mockCreateStructuredTaskTemplate.mockResolvedValue({ id: "template-1" });
    mockCreateStructuredTaskHistory.mockResolvedValue({ id: "history-1" });
    mockListStructuredTaskTemplates.mockResolvedValue([]);
    mockListStructuredTaskHistory.mockResolvedValue([]);
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

    render(<CreateIssueModal onClose={onClose} />);

    await user.type(screen.getByPlaceholderText("Issue title"), " Ship create issue regression coverage ");
    await user.click(screen.getByRole("button", { name: "Create Issue" }));

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
  }, 15000);

  it("creates a structured issue from the Structured Task tab", async () => {
    const user = userEvent.setup();

    mockCreateIssue.mockResolvedValueOnce({
      id: "issue-456",
      identifier: "TES-456",
      title: "Build a launch checklist for design team",
      status: "todo",
    });

    render(<CreateIssueModal onClose={vi.fn()} />);

    await user.click(screen.getByRole("button", { name: "Structured Task" }));
    await user.type(
      screen.getByPlaceholderText("Paste the user's raw request here..."),
      "Build a launch checklist for design team.{enter}Deliverable: checklist doc.{enter}for design team",
    );
    await user.click(screen.getByRole("button", { name: "Generate Structure" }));
    await waitFor(() => {
      expect(mockClarifyStructuredTask).toHaveBeenCalledWith({
        original_input: "Build a launch checklist for design team.\nDeliverable: checklist doc.\nfor design team",
      });
    });
    await waitFor(() => {
      expect(screen.getByRole("button", { name: "Create Structured Issue" })).not.toBeDisabled();
    });
    await user.click(screen.getByRole("button", { name: "Create Structured Issue" }));

    await waitFor(() => {
      expect(mockCreateIssue).toHaveBeenCalledWith(
        expect.objectContaining({
          title: "Build a launch checklist for design team",
          description: expect.stringContaining("## Task Brief"),
        }),
      );
    });
    await waitFor(() => {
      expect(mockCreateStructuredTaskHistory).toHaveBeenCalledWith(
        expect.objectContaining({
          issue_id: "issue-456",
          goal: "Build a launch checklist for design team",
          clarity_status: "clear",
        }),
      );
    });
  }, 15000);

  it("loads templates and history in structured mode and applies them", async () => {
    const user = userEvent.setup();

    mockListStructuredTaskTemplates.mockResolvedValueOnce([
      {
        id: "template-1",
        template_name: "Launch checklist template",
        description: "Checklist workflow for launch prep",
        goal: "Prepare a launch checklist",
        audience: ["design team", "pm"],
        output: "Checklist document",
        constraints: ["Keep it under 10 items"],
        style: ["concise"],
        parameters: [],
        scope: "personal",
        created_at: "2026-04-20T10:00:00.000Z",
        updated_at: "2026-04-20T10:00:00.000Z",
      },
    ]);
    mockListStructuredTaskHistory.mockResolvedValueOnce([
      {
        id: "history-1",
        issue_id: "issue-999",
        goal: "Draft team rollout note",
        clarity_status: "risky",
        used_template_id: null,
        executed_at: "2026-04-20T11:00:00.000Z",
        spec: {
          goal: "Draft team rollout note",
          audience: ["ops"],
          output: "One rollout note",
          constraints: ["No more than 300 words"],
          style: ["formal"],
          open_questions: ["Audience is not explicit."],
        },
      },
    ]);

    render(<CreateIssueModal onClose={vi.fn()} />);

    await user.click(screen.getByRole("button", { name: "Structured Task" }));

    await waitFor(() => {
      expect(mockListStructuredTaskTemplates).toHaveBeenCalledTimes(1);
      expect(mockListStructuredTaskHistory).toHaveBeenCalledTimes(1);
    });

    await user.click(screen.getByRole("button", { name: /Launch checklist template/i }));
    await waitFor(() => {
      expect(screen.getByDisplayValue("Prepare a launch checklist")).toBeInTheDocument();
      expect(screen.getByDisplayValue("design team, pm")).toBeInTheDocument();
      expect(screen.getByDisplayValue("Checklist document")).toBeInTheDocument();
    });

    await user.click(screen.getByRole("button", { name: /Draft team rollout note/i }));
    await waitFor(() => {
      expect(screen.getByDisplayValue("Draft team rollout note")).toBeInTheDocument();
      expect(screen.getByDisplayValue("ops")).toBeInTheDocument();
      expect(screen.getByDisplayValue("One rollout note")).toBeInTheDocument();
    });
  }, 15000);
});
