import { forwardRef, useImperativeHandle, useRef, useState, type ReactNode } from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../locales/en/common.json";
import enModals from "../locales/en/modals.json";
import enEditor from "../locales/en/editor.json";

const TEST_RESOURCES = {
  // `editor` carries the shared upload-gate copy ("Uploading…").
  en: { common: enCommon, modals: enModals, editor: enEditor },
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
const mockAttachLabel = vi.hoisted(() => vi.fn());
const mockListProperties = vi.hoisted(() => vi.fn());
const mockSetIssueProperty = vi.hoisted(() => vi.fn());
const mockSetDraft = vi.hoisted(() => vi.fn());
const mockClearDraft = vi.hoisted(() => vi.fn());
const mockSetLastAssignee = vi.hoisted(() => vi.fn());
const mockSetKeepOpen = vi.hoisted(() => vi.fn());
const mockToastCustom = vi.hoisted(() => vi.fn());
const mockToastDismiss = vi.hoisted(() => vi.fn());
const mockToastError = vi.hoisted(() => vi.fn());
const mockUploadWithToast = vi.hoisted(() => vi.fn());

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
    labelIds: [] as string[],
    propertyValues: {} as Record<string, string | number | boolean | string[]>,
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
  lastAssigneeType: undefined,
  lastAssigneeId: undefined,
  setDraft: mockSetDraft,
  clearDraft: mockClearDraft,
  setLastAssignee: mockSetLastAssignee,
};

const mockQuickCreateStore = {
  keepOpen: false,
  setKeepOpen: mockSetKeepOpen,
};

type ManualCreateField =
  | "status"
  | "priority"
  | "assignee"
  | "labels"
  | "project"
  | "due_date"
  | "start_date";

const DEFAULT_MANUAL_FIELDS: ManualCreateField[] = [
  "status",
  "priority",
  "assignee",
  "labels",
  "project",
];

const mockCreateSettingsStore = {
  manualCreateFields: DEFAULT_MANUAL_FIELDS as ManualCreateField[],
};

vi.mock("../navigation", () => ({
  useNavigation: () => ({ push: mockPush }),
}));

vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => ({ name: "Test Workspace" }),
  useWorkspacePaths: () => ({
    issueDetail: (id: string) => `/ws-test/issues/${id}`,
    settings: () => "/ws-test/settings",
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
  childIssuesOptions: (wsId: string, id: string) => ({
    queryKey: ["issues", wsId, "children", id],
    queryFn: () => Promise.resolve([]),
  }),
}));

// CreateRunHint's pre-trigger preview + actor-name lookup are exercised in
// their own suites; here we only need the create form to render without query
// infra, so stub them to the inert "no run will start" state.
vi.mock("../issues/hooks/use-issue-trigger-preview", () => ({
  useIssueTriggerPreview: () => ({
    triggers: [],
    totalCount: 0,
    isLoading: false,
    handoffSupported: false,
  }),
}));

vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({ getActorName: () => "Agent" }),
}));

// CreateRunHint now renders an ActorAvatar for agent/squad assignees. This
// suite is about the create form, not the avatar (whose own workspace/presence/
// navigation hook tree is exercised elsewhere), so stub it inert.
vi.mock("../common/actor-avatar", () => ({
  ActorAvatar: () => null,
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

vi.mock("@multica/core/issues/stores/issue-create-settings-store", () => ({
  useIssueCreateSettingsStore: (
    selector?: (state: typeof mockCreateSettingsStore) => unknown,
  ) => (selector ? selector(mockCreateSettingsStore) : mockCreateSettingsStore),
}));

vi.mock("@multica/core/issues/mutations", () => ({
  useCreateIssue: () => ({ mutateAsync: mockCreateIssue }),
  useUpdateIssue: () => ({ mutate: vi.fn() }),
}));

vi.mock("@multica/core/labels", () => ({
  useAttachLabelToIssue: () => ({ mutateAsync: mockAttachLabel }),
}));

vi.mock("@multica/core/properties", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/properties")>();
  return {
    ...actual,
    useSetIssueProperty: () => ({
      mutateAsync: ({ issueId, propertyId, value }: {
        issueId: string;
        propertyId: string;
        value: string | number | boolean | string[];
      }) => mockSetIssueProperty(issueId, propertyId, value),
    }),
  };
});

vi.mock("@multica/core/hooks/use-file-upload", () => ({
  useFileUpload: () => ({ uploadWithToast: mockUploadWithToast }),
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
    api: {
      listProperties: mockListProperties,
      setIssueProperty: mockSetIssueProperty,
    },
    ApiError,
    parseWithFallback,
    DuplicateIssueErrorBodySchema,
  };
});

vi.mock("../editor", async () => {
  // Real submit gate (pure React) driven by the mock editor's
  // `hasActiveUploads` / `onUploadingChange`.
  const uploadGate = await vi.importActual<typeof import("../editor/use-upload-gate")>(
    "../editor/use-upload-gate",
  );
  const ContentEditor = forwardRef(({ defaultValue, onUpdate, onUploadFile, onUploadingChange, placeholder, attachments }: any, ref: any) => {
    const valueRef = useRef(defaultValue || "");
    const [value, setValue] = useState(defaultValue || "");
    // Mirrors the real editor's `uploading` node attrs: the placeholder is in
    // the doc from before the await until the upload settles, and the host
    // hears about it through onUploadingChange.
    const inFlightRef = useRef(0);
    useImperativeHandle(ref, () => ({
      getMarkdown: () => valueRef.current,
      clearContent: () => {
        valueRef.current = "";
        setValue("");
      },
      uploadFile: async (file: File) => {
        inFlightRef.current += 1;
        if (inFlightRef.current === 1) onUploadingChange?.(true);
        try {
          return await onUploadFile?.(file);
        } finally {
          inFlightRef.current -= 1;
          if (inFlightRef.current === 0) onUploadingChange?.(false);
        }
      },
      hasActiveUploads: () => inFlightRef.current > 0,
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
        />
      </>
    );
  });
  ContentEditor.displayName = "ContentEditor";

  return {
    ...uploadGate,
    useEditorUpload: () => ({
      uploadWithToast: mockUploadWithToast,
      upload: vi.fn(),
      uploading: false,
    }),
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
  StagePicker: () => <div data-testid="stage-picker" />,
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
  // Due date now shares the start-date overflow pattern, so surface
  // open/onOpenChange to assert it too.
  DueDatePicker: ({ open, onOpenChange }: { open?: boolean; onOpenChange?: (v: boolean) => void }) => (
    <div
      data-testid="due-date-picker"
      data-open={open ? "true" : "false"}
      onClick={() => onOpenChange?.(false)}
    />
  ),
  // Labels can now be hidden via Settings → Issue and revealed from the
  // overflow, so surface open/onOpenChange like the date pickers.
  LabelPicker: ({ open, onOpenChange }: { open?: boolean; onOpenChange?: (v: boolean) => void }) => (
    <div
      data-testid="label-picker"
      data-open={open ? "true" : "false"}
      onClick={() => onOpenChange?.(false)}
    />
  ),
}));

vi.mock("../issues/components/pickers/custom-property-picker", () => ({
  CustomPropertyValueInput: ({ property, onChange }: any) => (
    <button
      type="button"
      aria-label={`Edit ${property.name}`}
      onClick={() => onChange("option-enterprise")}
    >
      {property.name}
    </button>
  ),
  CustomPropertyValueDisplay: ({ value }: any) => <span>{String(value)}</span>,
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
  DropdownMenuSub: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  DropdownMenuSubTrigger: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  DropdownMenuSubContent: ({ children }: { children: React.ReactNode }) => <>{children}</>,
}));

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
    ...rest
  }: {
    children: React.ReactNode;
    disabled?: boolean;
    onClick?: () => void;
    type?: "button" | "submit" | "reset";
    // The real Button spreads the rest onto the element; forwarding them keeps
    // accessibility props (aria-busy / aria-disabled) assertable here.
    [key: string]: unknown;
  }) => (
    <button type={type} disabled={disabled} onClick={onClick} {...rest}>
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
    mockCreateSettingsStore.manualCreateFields = DEFAULT_MANUAL_FIELDS;
    mockSetKeepOpen.mockImplementation((v: boolean) => {
      mockQuickCreateStore.keepOpen = v;
    });
    mockDraftStore.draft.title = "";
    mockDraftStore.draft.description = "";
    mockDraftStore.draft.status = "todo";
    mockDraftStore.draft.priority = "none";
    // Reset the shared draft mock so per-test assignee seeding (squad / agent)
    // doesn't leak into the next test in the suite.
    mockDraftStore.draft.assigneeType = undefined;
    mockDraftStore.draft.assigneeId = undefined;
    mockDraftStore.draft.startDate = null;
    mockDraftStore.draft.dueDate = null;
    mockDraftStore.draft.labelIds = [];
    mockDraftStore.draft.propertyValues = {};
    mockDraftStore.draft.attachments = [];
    mockSetDraft.mockImplementation((patch: Partial<typeof mockDraftStore.draft>) => {
      mockDraftStore.draft = { ...mockDraftStore.draft, ...patch };
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
        labelIds: [],
        propertyValues: {},
        attachments: [],
      };
    });
    mockUploadWithToast.mockResolvedValue({
      id: "11111111-2222-3333-4444-555555555555",
      workspace_id: "ws-test",
      issue_id: null,
      comment_id: null,
      chat_session_id: null,
      chat_message_id: null,
      uploader_type: "member",
      uploader_id: "user-1",
      filename: "shot.png",
      url: "https://cdn.example.test/shot.png",
      download_url: "https://cdn.example.test/shot.png?Signature=fresh",
      markdown_url: "https://multica-api.copilothub.ai/api/attachments/11111111-2222-3333-4444-555555555555/download",
      content_type: "image/png",
      size_bytes: 123,
      created_at: "2026-06-12T00:00:00Z",
      link: "https://cdn.example.test/shot.png",
      markdownLink: "https://multica-api.copilothub.ai/api/attachments/11111111-2222-3333-4444-555555555555/download",
    });
    mockCreateIssue.mockResolvedValue({
      id: "issue-123",
      identifier: "TES-123",
      title: "Ship create issue regression coverage",
      status: "todo",
      // Current backend echoes the attached labels, so the create flow skips
      // the legacy per-label attach fallback. Empty is enough — what matters
      // is that the field is present (not undefined).
      labels: [],
    });
    mockAttachLabel.mockResolvedValue({ labels: [] });
    mockListProperties.mockResolvedValue({
      properties: [
        {
          id: "property-tier",
          workspace_id: "ws-test",
          name: "Customer tier",
          type: "select",
          config: {
            options: [
              { id: "option-enterprise", name: "Enterprise", color: "#3b82f6" },
            ],
          },
          position: 0,
          archived: false,
          created_at: "2026-01-01T00:00:00Z",
          updated_at: "2026-01-01T00:00:00Z",
        },
      ],
      total: 1,
    });
    mockSetIssueProperty.mockResolvedValue({
      properties: { "property-tier": "option-enterprise" },
    });
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
        assignee_type: undefined,
        assignee_id: undefined,
        start_date: undefined,
        due_date: undefined,
        attachment_ids: undefined,
        parent_issue_id: undefined,
        project_id: undefined,
      });
    });

    expect(mockSetLastAssignee).toHaveBeenCalledWith(undefined, undefined);
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

  it("forwards selected labels in the create payload so they attach in the same transaction", async () => {
    const user = userEvent.setup();
    mockDraftStore.draft.labelIds = [
      "aaaaaaaa-1111-2222-3333-444444444444",
      "bbbbbbbb-1111-2222-3333-444444444444",
    ];

    renderModal(<CreateIssueModal onClose={vi.fn()} />);

    fireEvent.change(screen.getByPlaceholderText("Issue title"), {
      target: { value: "Labeled issue" },
    });
    await user.click(screen.getByRole("button", { name: "Create Issue" }));

    await waitFor(() => {
      expect(mockCreateIssue).toHaveBeenCalledWith(
        expect.objectContaining({
          title: "Labeled issue",
          label_ids: [
            "aaaaaaaa-1111-2222-3333-444444444444",
            "bbbbbbbb-1111-2222-3333-444444444444",
          ],
        }),
      );
    });
    // Backend echoed `labels`, so the atomic path handled it — no legacy
    // per-label attach fallback should run.
    expect(mockAttachLabel).not.toHaveBeenCalled();
  });

  it("falls back to per-label attach when an older backend omits labels from the create response", async () => {
    const user = userEvent.setup();
    // Older backend: ignores label_ids and returns an issue with no `labels`
    // field (the rolling-deploy window where web is ahead of the backend).
    mockCreateIssue.mockResolvedValueOnce({
      id: "issue-123",
      identifier: "TES-123",
      title: "Labeled issue",
      status: "todo",
    });
    mockDraftStore.draft.labelIds = [
      "aaaaaaaa-1111-2222-3333-444444444444",
      "bbbbbbbb-1111-2222-3333-444444444444",
    ];

    renderModal(<CreateIssueModal onClose={vi.fn()} />);

    fireEvent.change(screen.getByPlaceholderText("Issue title"), {
      target: { value: "Labeled issue" },
    });
    await user.click(screen.getByRole("button", { name: "Create Issue" }));

    await waitFor(() => {
      expect(mockAttachLabel).toHaveBeenCalledTimes(2);
    });
    expect(mockAttachLabel).toHaveBeenCalledWith({
      issueId: "issue-123",
      labelId: "aaaaaaaa-1111-2222-3333-444444444444",
    });
    expect(mockAttachLabel).toHaveBeenCalledWith({
      issueId: "issue-123",
      labelId: "bbbbbbbb-1111-2222-3333-444444444444",
    });
  });

  it("keeps manual mode open and clears content when create another is enabled", async () => {
    const user = userEvent.setup();
    const onClose = vi.fn();
    mockQuickCreateStore.keepOpen = true;

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
        assignee_type: undefined,
        assignee_id: undefined,
        start_date: undefined,
        due_date: undefined,
        attachment_ids: undefined,
        parent_issue_id: undefined,
        project_id: undefined,
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
      assigneeType: undefined,
      assigneeId: undefined,
      startDate: null,
      dueDate: null,
      labelIds: [],
      propertyValues: {},
      attachments: [],
    });
  });

  it("sets configured custom property values after the issue is created", async () => {
    const user = userEvent.setup();

    renderModal(<CreateIssueModal onClose={vi.fn()} />);

    await screen.findByText("Customer tier");
    await user.click(screen.getByText("Customer tier"));
    await user.click(screen.getByRole("button", { name: "Edit Customer tier" }));
    await user.type(screen.getByPlaceholderText("Issue title"), "Enterprise follow-up");
    await user.click(screen.getByRole("button", { name: "Create Issue" }));

    await waitFor(() => {
      expect(mockSetIssueProperty).toHaveBeenCalledWith(
        "issue-123",
        "property-tier",
        "option-enterprise",
      );
    });
    expect(mockClearDraft).toHaveBeenCalled();
  });

  it("persists manual-mode uploads in the issue draft", async () => {
    const user = userEvent.setup();

    renderModal(<CreateIssueModal onClose={vi.fn()} />);

    await user.click(screen.getByRole("button", { name: "Upload file" }));

    await waitFor(() => {
      expect(mockSetDraft).toHaveBeenCalledWith({
        attachments: [
          expect.objectContaining({
            id: "11111111-2222-3333-4444-555555555555",
            filename: "shot.png",
            download_url: "",
          }),
        ],
      });
    });
    const draftAttachmentsCall = mockSetDraft.mock.calls.find(
      ([patch]) => Array.isArray(patch.attachments),
    )?.[0] as { attachments?: Array<{ download_url: string }> } | undefined;
    expect(draftAttachmentsCall?.attachments?.[0]?.download_url).not.toContain(
      "Signature=",
    );
  });

  it("reuses draft attachments after reopening manual create so pasted images can render and bind", async () => {
    const user = userEvent.setup();
    const attachment = {
      id: "11111111-2222-3333-4444-555555555555",
      workspace_id: "ws-test",
      issue_id: null,
      comment_id: null,
      chat_session_id: null,
      chat_message_id: null,
      uploader_type: "member",
      uploader_id: "user-1",
      filename: "shot.png",
      url: "https://cdn.example.test/shot.png",
      download_url: "",
      markdown_url: "https://multica-api.copilothub.ai/api/attachments/11111111-2222-3333-4444-555555555555/download",
      content_type: "image/png",
      size_bytes: 123,
      created_at: "2026-06-12T00:00:00Z",
    };
    mockDraftStore.draft.title = "Image draft";
    mockDraftStore.draft.description = `![shot.png](${attachment.markdown_url})`;
    mockDraftStore.draft.attachments = [attachment];

    renderModal(<CreateIssueModal onClose={vi.fn()} />);

    expect(screen.getByPlaceholderText("Add description...")).toHaveAttribute(
      "data-attachments-count",
      "1",
    );

    await user.click(screen.getByRole("button", { name: "Create Issue" }));

    await waitFor(() => {
      expect(mockCreateIssue).toHaveBeenCalledWith(
        expect.objectContaining({
          description: `![shot.png](${attachment.markdown_url})`,
          attachment_ids: ["11111111-2222-3333-4444-555555555555"],
        }),
      );
    });
  });

  it("prunes draft attachments the reopened description no longer references", async () => {
    const referenced = {
      id: "11111111-2222-3333-4444-555555555555",
      workspace_id: "ws-test",
      issue_id: null,
      comment_id: null,
      chat_session_id: null,
      chat_message_id: null,
      uploader_type: "member",
      uploader_id: "user-1",
      filename: "kept.png",
      url: "https://cdn.example.test/kept.png",
      download_url: "",
      markdown_url: "https://multica-api.copilothub.ai/api/attachments/11111111-2222-3333-4444-555555555555/download",
      content_type: "image/png",
      size_bytes: 123,
      created_at: "2026-06-12T00:00:00Z",
    };
    const deleted = {
      ...referenced,
      id: "99999999-8888-7777-6666-555555555555",
      filename: "deleted.png",
      url: "https://cdn.example.test/deleted.png",
      markdown_url: "https://multica-api.copilothub.ai/api/attachments/99999999-8888-7777-6666-555555555555/download",
    };
    mockDraftStore.draft.title = "Image draft";
    mockDraftStore.draft.description = `![kept.png](${referenced.markdown_url})`;
    mockDraftStore.draft.attachments = [referenced, deleted];

    renderModal(<CreateIssueModal onClose={vi.fn()} />);

    await waitFor(() => {
      expect(mockSetDraft).toHaveBeenCalledWith({ attachments: [referenced] });
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

  it("exposes the label picker on the toolbar and keeps due date in the overflow menu", async () => {
    renderModal(<CreateIssueModal onClose={vi.fn()} />);

    // Label entry is now surfaced directly on the dialog...
    expect(screen.getByTestId("label-picker")).toBeInTheDocument();
    // ...while due date is collapsed into the ⋯ menu (no inline pill yet).
    expect(screen.queryByTestId("due-date-picker")).not.toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /Set due date/i }),
    ).toBeInTheDocument();
  });

  it("hides due date behind the overflow menu and reveals it on demand", async () => {
    const user = userEvent.setup();

    renderModal(<CreateIssueModal onClose={vi.fn()} />);

    expect(screen.queryByTestId("due-date-picker")).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: /Set due date/i }));

    const picker = await screen.findByTestId("due-date-picker");
    expect(picker).toHaveAttribute("data-open", "true");

    await user.click(picker);

    expect(screen.queryByTestId("due-date-picker")).not.toBeInTheDocument();
  });

  it("hides toolbar fields turned off in Settings → Issue and re-reveals them from the overflow", async () => {
    const user = userEvent.setup();
    mockCreateSettingsStore.manualCreateFields = ["status", "priority", "assignee", "project"];

    renderModal(<CreateIssueModal onClose={vi.fn()} />);

    expect(screen.queryByTestId("label-picker")).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: /Set labels/i }));

    const picker = await screen.findByTestId("label-picker");
    expect(picker).toHaveAttribute("data-open", "true");

    await user.click(picker);

    expect(screen.queryByTestId("label-picker")).not.toBeInTheDocument();
  });

  it("keeps a hidden field on the toolbar while it holds a value", () => {
    mockCreateSettingsStore.manualCreateFields = ["status", "priority", "assignee", "project"];
    mockDraftStore.draft.labelIds = ["label-1"];

    renderModal(<CreateIssueModal onClose={vi.fn()} />);

    expect(screen.getByTestId("label-picker")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Set labels/i })).not.toBeInTheDocument();
  });

  it("renders due date inline when enabled in Settings → Issue", () => {
    mockCreateSettingsStore.manualCreateFields = [...DEFAULT_MANUAL_FIELDS, "due_date"];

    renderModal(<CreateIssueModal onClose={vi.fn()} />);

    expect(screen.getByTestId("due-date-picker")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Set due date/i })).not.toBeInTheDocument();
  });

  it("routes Customize fields to Settings → Issue and closes the dialog", async () => {
    const user = userEvent.setup();
    const onClose = vi.fn();

    renderModal(<CreateIssueModal onClose={onClose} />);

    await user.click(screen.getByRole("button", { name: /Customize fields/i }));

    expect(onClose).toHaveBeenCalled();
    expect(mockPush).toHaveBeenCalledWith("/ws-test/settings?tab=issue");
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
      />,
    );

    await user.type(screen.getByPlaceholderText("Issue title"), "Update");
    await user.type(screen.getByPlaceholderText("Add description..."), "Some body");

    mockSetDraft.mockClear();
    await user.click(screen.getByRole("button", { name: /Switch to Agent/i }));

    expect(mockSetDraft).toHaveBeenCalledWith({ title: "", description: "" });
  });

  // MUL-4808 — manual create had no upload gate at all: Create, Enter on the
  // title, and Switch to Agent would each fix the draft while an image was
  // still uploading, dropping it from the description with no warning.
  describe("upload submit gate", () => {
    /** Attach a file whose upload stays in flight until the caller releases it. */
    function startPendingUpload() {
      let release!: (result: unknown) => void;
      mockUploadWithToast.mockImplementationOnce(
        () => new Promise((resolve) => { release = resolve; }),
      );
      fireEvent.click(screen.getByRole("button", { name: "Upload file" }));
      return { release: (result: unknown) => release(result) };
    }

    function renderManual(onSwitchMode = vi.fn()) {
      const view = renderModal(
        <ManualCreatePanel
          onClose={vi.fn()}
          onSwitchMode={onSwitchMode}
          isExpanded={false}
          setIsExpanded={vi.fn()}
        />,
      );
      return { ...view, onSwitchMode };
    }

    it("disables Create and shows Uploading… while an upload is in flight", async () => {
      const user = userEvent.setup();
      renderManual();
      await user.type(screen.getByPlaceholderText("Issue title"), "Has a screenshot");

      const pending = startPendingUpload();

      const createButton = await screen.findByRole("button", { name: "Uploading…" });
      await waitFor(() => expect(createButton).toBeDisabled());
      expect(createButton).toHaveAttribute("aria-busy", "true");

      await act(async () => { pending.release({ id: "att-1", url: "https://cdn/x.png" }); });
      await waitFor(() =>
        expect(screen.getByRole("button", { name: "Create Issue" })).not.toBeDisabled(),
      );
    });

    it("never submits manual create from Enter in the title", async () => {
      const user = userEvent.setup();
      renderManual();
      const title = screen.getByPlaceholderText("Issue title");
      await user.type(title, "Has a screenshot");

      fireEvent.keyDown(title, { key: "Enter" });
      await Promise.resolve();
      expect(mockCreateIssue).not.toHaveBeenCalled();
    });

    it("blocks Switch to Agent while an upload is in flight", async () => {
      const user = userEvent.setup();
      const onSwitchMode = vi.fn();
      renderManual(onSwitchMode);
      await user.type(screen.getByPlaceholderText("Issue title"), "Has a screenshot");

      startPendingUpload();

      // The switch packs the description into an agent prompt and clears the
      // manual draft — doing that mid-upload loses the image for good.
      const switchButton = screen.getByRole("button", { name: /Switch to Agent/i });
      await waitFor(() => expect(switchButton).toBeDisabled());
      fireEvent.click(switchButton);
      expect(onSwitchMode).not.toHaveBeenCalled();
    });
  });
});
