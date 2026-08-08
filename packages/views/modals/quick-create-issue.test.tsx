import { forwardRef, useImperativeHandle, useRef, useState, type ReactNode } from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

const mockQuickCreateIssue = vi.hoisted(() => vi.fn());
const mockSetLastActor = vi.hoisted(() => vi.fn());
const mockSetQuickCreateFieldVisible = vi.hoisted(() => vi.fn());
const mockSetKeepOpen = vi.hoisted(() => vi.fn());
const mockSetLastMode = vi.hoisted(() => vi.fn());
const mockToastSuccess = vi.hoisted(() => vi.fn());
// Uploads flow through the module-level coordinator, which calls
// `api.uploadFile(file, ctx, signal)` (MUL-5181 L2).
const mockApiUploadFile = vi.hoisted(() => vi.fn());
const mockNavigationPush = vi.hoisted(() => vi.fn());
const mockSetShared = vi.hoisted(() => vi.fn());
const mockSetManual = vi.hoisted(() => vi.fn());
const mockSetAgent = vi.hoisted(() => vi.fn());
const mockSetActiveMode = vi.hoisted(() => vi.fn());
const mockClearDraft = vi.hoisted(() => vi.fn());

const emptyIssueDraft = () => ({
  shared: {
    projectId: undefined as string | undefined,
    priority: "none" as "none" | "low" | "medium" | "high" | "urgent",
    dueDate: null as string | null,
    attachments: [] as Array<{ id: string }>,
  },
  manual: {
    title: "",
    description: "",
    status: "todo" as const,
    startDate: null as string | null,
    assigneeType: undefined as "agent" | "squad" | "member" | undefined,
    assigneeId: undefined as string | undefined,
    labelIds: [] as string[],
    propertyValues: {} as Record<string, string | number | boolean | string[]>,
  },
  agent: {
    prompt: "",
    actorType: undefined as "agent" | "squad" | undefined,
    actorId: undefined as string | undefined,
  },
  activeMode: "agent" as "agent" | "manual",
});

const mockIssueDraftStore = {
  draft: emptyIssueDraft(),
  setShared: mockSetShared,
  setManual: mockSetManual,
  setAgent: mockSetAgent,
  setActiveMode: mockSetActiveMode,
  clearDraft: mockClearDraft,
};

const mockQuickCreateStore = {
  lastActorType: null as "agent" | "squad" | null,
  lastActorId: null as string | null,
  setLastActor: mockSetLastActor,
  keepOpen: false,
  setKeepOpen: mockSetKeepOpen,
  // Not part of the store's interface any more (MUL-5862), but an older
  // build persisted it and localStorage still hands it back on rehydrate.
  // Kept here so the tests can prove the panel ignores it.
  lastProjectId: null as string | null,
};

const mockCreateSettingsStore = {
  quickCreateFields: ["project"] as Array<"project" | "priority" | "due_date">,
  setQuickCreateFieldVisible: mockSetQuickCreateFieldVisible,
};

// Per-test override for the projects query, so tests can swap between
// "loaded as empty" (the deleted-project case) and "still loading" without
// re-mocking the whole module.
const mockProjectsQuery = vi.hoisted(() => ({
  data: [] as Array<{ id: string; title: string; icon: string | null }>,
  isSuccess: true,
}));

// Per-test override for the squads list so we can flip between "squads
// exist and one's leader is reachable" and "no squads" cases without
// re-mocking the whole module.
const mockSquadsData = vi.hoisted(
  () => ({ list: [] as Array<{ id: string; name: string; leader_id: string; archived_at: string | null }> }),
);

// The real handle mints an id when it inserts the placeholder and hands it to
// the uploader, which adopts it as the draft `clientUploadId`. Mocks must do
// the same or the two records drift apart only in tests.
let mockUploadIdSeq = 0;

vi.mock("@tanstack/react-query", () => ({
  useQuery: ({ queryKey }: { queryKey: string[] }) => {
    // Workspace-scoped query keys carry the wsId as `queryKey[1]`; the
    // discriminator is at `queryKey[2]` (e.g. ["workspaces", wsId, "squads"]).
    if (queryKey[0] === "workspaces" && queryKey[2] === "squads") {
      return { data: mockSquadsData.list };
    }
    switch (queryKey[0]) {
      case "members":
        return { data: [{ user_id: "user-1", role: "admin" }] };
      case "agents":
        return {
          data: [{ id: "agent-1", name: "Bohan", archived_at: null, runtime_id: "runtime-1" }],
        };
      case "runtimes":
        return { data: [{ id: "runtime-1", metadata: { cli_version: "1.2.3" } }] };
      case "projects":
        return mockProjectsQuery;
      default:
        return { data: [] };
    }
  },
}));

vi.mock("@multica/core/api", () => ({
  api: {
    quickCreateIssue: mockQuickCreateIssue,
    uploadFile: mockApiUploadFile,
  },
  ApiError: class ApiError extends Error {
    body?: unknown;
  },
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-test",
}));

vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => ({ name: "Test Workspace" }),
  useWorkspacePaths: () => ({
    settings: () => "/ws-test/settings",
  }),
}));

vi.mock("../navigation", () => ({
  useNavigation: () => ({ push: mockNavigationPush }),
}));

vi.mock("@multica/core/workspace/queries", () => ({
  agentListOptions: () => ({ queryKey: ["agents"] }),
  memberListOptions: () => ({ queryKey: ["members"] }),
  squadListOptions: (wsId: string) => ({
    queryKey: ["workspaces", wsId, "squads"],
  }),
}));

vi.mock("@multica/core/projects/queries", () => ({
  projectListOptions: () => ({ queryKey: ["projects"] }),
}));

vi.mock("@multica/core/issues/stores/quick-create-store", () => ({
  useQuickCreateStore: (selector?: (state: typeof mockQuickCreateStore) => unknown) =>
    (selector ? selector(mockQuickCreateStore) : mockQuickCreateStore),
}));

vi.mock("@multica/core/issues/stores/draft-store", () => ({
  useIssueDraftStore: Object.assign(
    (selector?: (state: typeof mockIssueDraftStore) => unknown) =>
      (selector ? selector(mockIssueDraftStore) : mockIssueDraftStore),
    { getState: () => mockIssueDraftStore },
  ),
}));

vi.mock("@multica/core/issues/stores/issue-create-settings-store", () => ({
  useIssueCreateSettingsStore: (
    selector?: (state: typeof mockCreateSettingsStore) => unknown,
  ) => (selector ? selector(mockCreateSettingsStore) : mockCreateSettingsStore),
}));

vi.mock("@multica/core/issues/stores/create-mode-store", () => ({
  useCreateModeStore: (selector?: (state: { setLastMode: typeof mockSetLastMode }) => unknown) =>
    (selector ? selector({ setLastMode: mockSetLastMode }) : { setLastMode: mockSetLastMode }),
}));

vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector?: (state: { user: { id: string } }) => unknown) =>
    (selector ? selector({ user: { id: "user-1" } }) : { user: { id: "user-1" } }),
}));

vi.mock("@multica/core/runtimes", () => ({
  runtimeListOptions: () => ({ queryKey: ["runtimes"] }),
  checkQuickCreateCliVersion: () => ({ state: "ok", min: "1.0.0" }),
  checkQuickCreateFieldsCliVersion: () => ({ state: "ok", min: "1.0.0" }),
  readRuntimeCliVersion: () => "1.2.3",
  MIN_QUICK_CREATE_CLI_VERSION: "1.0.0",
}));


vi.mock("../issues/components/pickers/assignee-picker", () => ({
  canAssignAgent: () => true,
}));

vi.mock("../common/actor-avatar", () => ({
  ActorAvatar: () => <span data-testid="actor-avatar" />,
}));

vi.mock("../issues/components", () => ({
  PriorityIcon: ({ priority }: { priority: string }) => <span>{priority}</span>,
  PriorityPicker: ({ priority, onUpdate }: any) => (
    <button type="button" data-testid="priority-picker" onClick={() => onUpdate({ priority: "high" })}>
      Priority {priority}
    </button>
  ),
  DueDatePicker: ({ dueDate, onUpdate }: any) => (
    <button type="button" data-testid="due-date-picker" onClick={() => onUpdate({ due_date: "2026-08-01" })}>
      Due date {dueDate ?? "none"}
    </button>
  ),
}));

vi.mock("../projects/components/project-picker", () => ({
  ProjectPicker: ({ projectId, onUpdate, triggerRender }: any) => (
    <>
      <button type="button" data-testid="project-picker" onClick={() => onUpdate({ project_id: "proj-1" })}>
        Project {projectId ?? "none"}
      </button>
      {/* The caller's own trigger renders too — the pill carries the
          quick-clear ×, which belongs to this panel, not to the picker. */}
      {triggerRender}
    </>
  ),
}));

vi.mock("@multica/ui/components/ui/dropdown-menu", () => ({
  DropdownMenu: ({ children }: { children: ReactNode }) => <>{children}</>,
  DropdownMenuTrigger: ({ render }: { render: ReactNode }) => <>{render}</>,
  DropdownMenuContent: ({ children }: { children: ReactNode }) => <>{children}</>,
  DropdownMenuItem: ({ children, onClick }: any) => (
    <button type="button" onClick={onClick}>{children}</button>
  ),
  DropdownMenuSeparator: () => null,
}));

vi.mock("@multica/ui/lib/utils", () => ({
  cn: (...values: Array<string | false | null | undefined>) => values.filter(Boolean).join(" "),
}));

vi.mock("../editor", async () => {
  // Real submit gate (pure React) driven by the mock editor's
  // `hasActiveUploads` / `onUploadingChange`.
  const uploadGate = await vi.importActual<typeof import("../editor/use-upload-gate")>(
    "../editor/use-upload-gate",
  );
  // Real composer submit contract — pure React, no network. Drives the
  // single-flight + upload-gate semantics against the mocked editor/api.
  const composer = await vi.importActual<typeof import("../editor/use-composer-submit")>(
    "../editor/use-composer-submit",
  );
  const ContentEditor = forwardRef(({ defaultValue, onUpdate, onSubmit, onUploadFile, onUploadingChange, placeholder }: any, ref: any) => {
    const valueRef = useRef(defaultValue || "");
    const [value, setValue] = useState(defaultValue || "");
    // Mirrors the real editor's `uploading` node attrs: the placeholder sits
    // in the doc from before the await until the upload settles, which is what
    // `hasActiveUploads` reports and `onUploadingChange` publishes.
    const inFlightRef = useRef(0);
    const runUpload = async (file: File) => {
      inFlightRef.current += 1;
      if (inFlightRef.current === 1) onUploadingChange?.(true);
      try {
        return await onUploadFile?.(file, `mock-upload-${++mockUploadIdSeq}`);
      } finally {
        inFlightRef.current -= 1;
        if (inFlightRef.current === 0) onUploadingChange?.(false);
      }
    };

    useImperativeHandle(ref, () => ({
      getMarkdown: () => valueRef.current,
      clearContent: () => {
        valueRef.current = "";
        setValue("");
      },
      uploadFile: runUpload,
      focus: vi.fn(),
      hasActiveUploads: () => inFlightRef.current > 0,
      // Placeholder rebuild contract: the real handle draws a card for an
      // upload the document is not showing and reports whether it landed.
      // Mocks track ids only — no document to draw into.
      insertUploadPlaceholder: () => true,
      settleUploadPlaceholder: () => false,
    }));

    return (
      <>
        <textarea
          value={value}
          placeholder={placeholder}
          onChange={(e) => {
            valueRef.current = e.target.value;
            setValue(e.target.value);
            onUpdate?.(e.target.value);
          }}
          onKeyDown={(e) => {
            if ((e.metaKey || e.ctrlKey) && e.key === "Enter") {
              onSubmit?.();
            }
          }}
        />
        <button
          type="button"
          onClick={() => runUpload(new File(["image"], "shot.png", { type: "image/png" }))}
        >
          Mock editor upload
        </button>
      </>
    );
  });
  ContentEditor.displayName = "ContentEditor";

  return {
    ...uploadGate,
    ...composer,
    ContentEditor,
    useFileDropZone: () => ({ isDragOver: false, dropZoneProps: {} }),
    FileDropOverlay: () => null,
  };
});

vi.mock("@multica/ui/components/ui/dialog", () => ({
  DialogTitle: ({ children, className }: { children: ReactNode; className?: string }) => (
    <div className={className}>{children}</div>
  ),
}));

vi.mock("../issues/components/pickers/property-picker", () => ({
  PropertyPicker: ({
    trigger,
    children,
    searchPlaceholder,
    onSearchChange,
  }: {
    trigger: ReactNode;
    children: ReactNode;
    searchPlaceholder?: string;
    onSearchChange?: (v: string) => void;
  }) => (
    <>
      {trigger}
      <input
        aria-label="actor-search"
        placeholder={searchPlaceholder}
        onChange={(e) => onSearchChange?.(e.target.value)}
      />
      {children}
    </>
  ),
  PickerItem: ({
    children,
    onClick,
    selected,
  }: {
    children: ReactNode;
    onClick: () => void;
    selected?: boolean;
  }) => (
    <button type="button" onClick={onClick} data-selected={selected ? "true" : "false"}>
      {children}
    </button>
  ),
  PickerSection: ({ label, children }: { label: string; children: ReactNode }) => (
    <div>
      <div data-testid="picker-section-label">{label}</div>
      {children}
    </div>
  ),
  PickerEmpty: () => <div data-testid="picker-empty" />,
}));

vi.mock("@multica/ui/components/ui/button", () => ({
  Button: ({ children, disabled, onClick }: { children: ReactNode; disabled?: boolean; onClick?: () => void }) => (
    <button type="button" disabled={disabled} onClick={onClick}>
      {children}
    </button>
  ),
}));

vi.mock("@multica/ui/components/ui/switch", () => ({
  Switch: ({ checked, onCheckedChange }: { checked: boolean; onCheckedChange: (v: boolean) => void }) => (
    <input
      type="checkbox"
      checked={checked}
      onChange={(e) => onCheckedChange(e.target.checked)}
    />
  ),
}));

vi.mock("@multica/ui/components/common/file-upload-button", () => ({
  // `disabled` is forwarded so the "can still queue another file mid-upload"
  // guarantee is actually assertable here (MUL-4808).
  FileUploadButton: ({ disabled }: { disabled?: boolean }) => (
    <button type="button" disabled={disabled}>Upload file</button>
  ),
}));

vi.mock("sonner", () => ({
  toast: {
    success: mockToastSuccess,
  },
}));

import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../locales/en/common.json";
import enModals from "../locales/en/modals.json";
import enEditor from "../locales/en/editor.json";
import enProjects from "../locales/en/projects.json";
import { AgentCreatePanel } from "./quick-create-issue";

const TEST_RESOURCES = {
  en: { common: enCommon, modals: enModals, editor: enEditor, projects: enProjects },
};

function renderPanel(props: React.ComponentProps<typeof AgentCreatePanel>) {
  return render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <AgentCreatePanel {...props} />
    </I18nProvider>,
  );
}

describe("AgentCreatePanel", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockQuickCreateStore.lastActorType = null;
    mockQuickCreateStore.lastActorId = null;
    mockQuickCreateStore.lastProjectId = null;
    mockCreateSettingsStore.quickCreateFields = ["project"];
    mockQuickCreateStore.keepOpen = false;
    mockIssueDraftStore.draft = emptyIssueDraft();
    // The prompt now lives in the unified draft's agent slot.
    mockIssueDraftStore.draft.agent.prompt = "Persisted draft prompt";
    mockSetShared.mockImplementation((patch: Partial<typeof mockIssueDraftStore.draft.shared>) => {
      mockIssueDraftStore.draft.shared = { ...mockIssueDraftStore.draft.shared, ...patch };
    });
    mockSetManual.mockImplementation((patch: Partial<typeof mockIssueDraftStore.draft.manual>) => {
      mockIssueDraftStore.draft.manual = { ...mockIssueDraftStore.draft.manual, ...patch };
    });
    mockSetAgent.mockImplementation((patch: Partial<typeof mockIssueDraftStore.draft.agent>) => {
      mockIssueDraftStore.draft.agent = { ...mockIssueDraftStore.draft.agent, ...patch };
    });
    mockClearDraft.mockImplementation(() => {
      mockIssueDraftStore.draft = emptyIssueDraft();
    });
    mockProjectsQuery.data = [];
    mockProjectsQuery.isSuccess = true;
    mockSquadsData.list = [];
    mockQuickCreateIssue.mockResolvedValue(undefined);
    mockApiUploadFile.mockResolvedValue({
      id: "019ec09d-6222-722b-bdfa-427b105d80be",
      workspace_id: "ws-test",
      issue_id: null,
      comment_id: null,
      chat_session_id: null,
      chat_message_id: null,
      uploader_type: "member",
      uploader_id: "user-1",
      filename: "shot.png",
      url: "/uploads/shot.png",
      download_url: "/api/attachments/019ec09d-6222-722b-bdfa-427b105d80be/download",
      markdown_url: "/api/attachments/019ec09d-6222-722b-bdfa-427b105d80be/download",
      content_type: "image/png",
      size_bytes: 5,
      created_at: "2026-06-12T00:00:00Z",
    });
    mockSetKeepOpen.mockImplementation((value: boolean) => {
      mockQuickCreateStore.keepOpen = value;
    });
  });

  it("loads the persisted prompt draft when no transient prompt is provided", () => {
    renderPanel({ onClose: vi.fn(), isExpanded: false, setIsExpanded: vi.fn() });

    expect(
      screen.getByPlaceholderText(
        'Tell the agent what to do, e.g. "let Bohan fix the inbox loading slowness in the Web project"',
      ),
    ).toHaveValue("Persisted draft prompt");
  });

  it("restores unfinished actor, project, priority, and due-date selections after remount", async () => {
    mockSquadsData.list = [
      { id: "squad-1", name: "Frontend Squad", leader_id: "agent-1", archived_at: null },
    ];
    mockProjectsQuery.data = [{ id: "proj-1", title: "Web", icon: null }];
    mockCreateSettingsStore.quickCreateFields = ["project", "priority", "due_date"];
    const user = userEvent.setup();

    const firstOpen = renderPanel({
      onClose: vi.fn(),
      isExpanded: false,
      setIsExpanded: vi.fn(),
    });

    await user.click(screen.getByRole("button", { name: /Frontend Squad/ }));
    await user.click(screen.getByTestId("project-picker"));
    await user.click(screen.getByTestId("priority-picker"));
    await user.click(screen.getByTestId("due-date-picker"));

    expect(mockIssueDraftStore.draft.agent).toEqual(
      expect.objectContaining({ actorType: "squad", actorId: "squad-1" }),
    );
    expect(mockIssueDraftStore.draft.shared).toEqual(
      expect.objectContaining({
        projectId: "proj-1",
        priority: "high",
        dueDate: "2026-08-01",
      }),
    );

    firstOpen.unmount();
    renderPanel({ onClose: vi.fn(), isExpanded: false, setIsExpanded: vi.fn() });

    expect(screen.getByRole("button", { name: /Frontend Squad/ })).toHaveAttribute(
      "data-selected",
      "true",
    );
    expect(screen.getByTestId("project-picker")).toHaveTextContent("Project proj-1");
    expect(screen.getByTestId("priority-picker")).toHaveTextContent("Priority high");
    expect(screen.getByTestId("due-date-picker")).toHaveTextContent("Due date 2026-08-01");
  });

  it("writes prompt changes back to the draft store and clears them after submit", async () => {
    const user = userEvent.setup();
    const onClose = vi.fn();

    renderPanel({ onClose, isExpanded: false, setIsExpanded: vi.fn() });

    const editor = screen.getByPlaceholderText(
      'Tell the agent what to do, e.g. "let Bohan fix the inbox loading slowness in the Web project"',
    );

    await user.clear(editor);
    await user.type(editor, "New agent prompt");
    expect(mockSetAgent).toHaveBeenLastCalledWith({ prompt: "New agent prompt" });

    await user.click(screen.getByRole("button", { name: /^Create$/i }));

    await waitFor(() => {
      expect(mockQuickCreateIssue).toHaveBeenCalledWith({
        agent_id: "agent-1",
        prompt: "New agent prompt",
        project_id: undefined,
      });
    });

    expect(mockSetLastActor).toHaveBeenCalledWith("agent", "agent-1");
    // A successful create ends the whole unified draft.
    expect(mockClearDraft).toHaveBeenCalled();
    expect(mockSetLastMode).toHaveBeenCalledWith("agent");
    expect(onClose).toHaveBeenCalled();
  });

  it("reveals optional fields from the overflow and submits their values", async () => {
    const user = userEvent.setup();

    renderPanel({ onClose: vi.fn(), isExpanded: false, setIsExpanded: vi.fn() });

    expect(screen.queryByTestId("priority-picker")).not.toBeInTheDocument();
    await user.click(screen.getByText("Set priority..."));
    await user.click(screen.getByTestId("priority-picker"));
    await user.click(screen.getByRole("button", { name: /^Create$/i }));

    await waitFor(() => {
      expect(mockQuickCreateIssue).toHaveBeenCalledWith(
        expect.objectContaining({
          agent_id: "agent-1",
          priority: "high",
        }),
      );
    });
  });

  it("routes Customize fields to Settings → Issue, keeping the typed prompt", async () => {
    const user = userEvent.setup();
    const onClose = vi.fn();

    renderPanel({ onClose, isExpanded: false, setIsExpanded: vi.fn() });

    const editor = screen.getByPlaceholderText(
      'Tell the agent what to do, e.g. "let Bohan fix the inbox loading slowness in the Web project"',
    );
    fireEvent.change(editor, { target: { value: "Half-typed request" } });
    await user.click(screen.getByRole("button", { name: "Customize fields..." }));

    expect(mockSetAgent).toHaveBeenLastCalledWith({ prompt: "Half-typed request" });
    expect(onClose).toHaveBeenCalled();
    expect(mockNavigationPush).toHaveBeenCalledWith("/ws-test/settings?tab=issue");
  });

  it("respects fields enabled in Settings → Issue by rendering them inline", () => {
    mockCreateSettingsStore.quickCreateFields = ["project", "priority", "due_date"];

    renderPanel({ onClose: vi.fn(), isExpanded: false, setIsExpanded: vi.fn() });

    expect(screen.getByTestId("project-picker")).toBeInTheDocument();
    expect(screen.getByTestId("priority-picker")).toBeInTheDocument();
    expect(screen.getByTestId("due-date-picker")).toBeInTheDocument();
  });

  it("submits seeded priority and due date as authoritative quick-create fields", async () => {
    const user = userEvent.setup();

    renderPanel({
      onClose: vi.fn(),
      isExpanded: false,
      setIsExpanded: vi.fn(),
      data: { priority: "urgent", due_date: "2026-08-01" },
    });

    await user.click(screen.getByRole("button", { name: /^Create$/i }));

    await waitFor(() => {
      expect(mockQuickCreateIssue).toHaveBeenCalledWith(
        expect.objectContaining({
          priority: "urgent",
          due_date: "2026-08-01",
        }),
      );
    });
  });

  // MUL-5181 P0: success may only consume the draft it submitted — the editor
  // stays interactive during the request, so mid-flight edits must survive.
  it("typing draft B while draft A's quick-create is pending survives the success (mounted)", async () => {
    let resolveCreate!: (v: unknown) => void;
    mockQuickCreateIssue.mockImplementationOnce(
      () => new Promise((resolve) => { resolveCreate = resolve; }),
    );
    const onClose = vi.fn();
    renderPanel({ onClose, isExpanded: false, setIsExpanded: vi.fn() });
    const editor = screen.getByPlaceholderText(
      'Tell the agent what to do, e.g. "let Bohan fix the inbox loading slowness in the Web project"',
    );
    fireEvent.change(editor, { target: { value: "Draft A prompt" } });
    fireEvent.click(screen.getByRole("button", { name: /^Create$/i }));
    await waitFor(() => expect(mockQuickCreateIssue).toHaveBeenCalled());

    // Mid-flight edit replaces the singleton draft's object identity.
    mockIssueDraftStore.draft = {
      ...emptyIssueDraft(),
      agent: { ...emptyIssueDraft().agent, prompt: "Draft B prompt" },
    };

    await act(async () => {
      resolveCreate(undefined);
      await Promise.resolve();
    });

    expect(mockClearDraft).not.toHaveBeenCalled();
    expect(onClose).not.toHaveBeenCalled();
    expect(mockIssueDraftStore.draft.agent.prompt).toBe("Draft B prompt");
  });

  // MUL-5181 P0: a submit that outlives its dialog may only consume the draft
  // it submitted — never one typed after closing and reopening.
  it("a late quick-create success does NOT clear a draft replaced after close", async () => {
    let resolveCreate!: (v: unknown) => void;
    mockQuickCreateIssue.mockImplementationOnce(
      () => new Promise((resolve) => { resolveCreate = resolve; }),
    );
    const view = renderPanel({ onClose: vi.fn(), isExpanded: false, setIsExpanded: vi.fn() });
    const editor = screen.getByPlaceholderText(
      'Tell the agent what to do, e.g. "let Bohan fix the inbox loading slowness in the Web project"',
    );
    fireEvent.change(editor, { target: { value: "Draft A prompt" } });
    fireEvent.click(screen.getByRole("button", { name: /^Create$/i }));
    await waitFor(() => expect(mockQuickCreateIssue).toHaveBeenCalled());

    view.unmount();
    mockIssueDraftStore.draft = {
      ...emptyIssueDraft(),
      agent: { ...emptyIssueDraft().agent, prompt: "Draft B prompt" },
    };

    await act(async () => {
      resolveCreate(undefined);
      await Promise.resolve();
    });

    expect(mockClearDraft).not.toHaveBeenCalled();
    expect(mockIssueDraftStore.draft.agent.prompt).toBe("Draft B prompt");
  });

  it("a late quick-create success still clears an untouched draft", async () => {
    let resolveCreate!: (v: unknown) => void;
    mockQuickCreateIssue.mockImplementationOnce(
      () => new Promise((resolve) => { resolveCreate = resolve; }),
    );
    const view = renderPanel({ onClose: vi.fn(), isExpanded: false, setIsExpanded: vi.fn() });
    const editor = screen.getByPlaceholderText(
      'Tell the agent what to do, e.g. "let Bohan fix the inbox loading slowness in the Web project"',
    );
    fireEvent.change(editor, { target: { value: "Draft A prompt" } });
    fireEvent.click(screen.getByRole("button", { name: /^Create$/i }));
    await waitFor(() => expect(mockQuickCreateIssue).toHaveBeenCalled());

    view.unmount();
    await act(async () => {
      resolveCreate(undefined);
      await Promise.resolve();
    });

    expect(mockClearDraft).toHaveBeenCalled();
  });

  it("passes referenced upload attachment ids to quick-create", async () => {
    const user = userEvent.setup();
    const onClose = vi.fn();

    renderPanel({ onClose, isExpanded: false, setIsExpanded: vi.fn() });

    await user.click(screen.getByRole("button", { name: "Mock editor upload" }));
    await waitFor(() => expect(mockApiUploadFile).toHaveBeenCalled());

    const editor = screen.getByPlaceholderText(
      'Tell the agent what to do, e.g. "let Bohan fix the inbox loading slowness in the Web project"',
    );
    await user.clear(editor);
    fireEvent.change(editor, {
      target: {
        value: "Create issue with ![image](/api/attachments/019ec09d-6222-722b-bdfa-427b105d80be/download)",
      },
    });

    await user.click(screen.getByRole("button", { name: /^Create$/i }));

    await waitFor(() => {
      expect(mockQuickCreateIssue).toHaveBeenCalledWith({
        agent_id: "agent-1",
        prompt: "Create issue with ![image](/api/attachments/019ec09d-6222-722b-bdfa-427b105d80be/download)",
        project_id: undefined,
        parent_issue_id: undefined,
        attachment_ids: ["019ec09d-6222-722b-bdfa-427b105d80be"],
      });
    });
  });

  // Picking a squad routes the submission through `squad_id` (not
  // `agent_id`) so the backend can resolve the squad's leader agent and
  // inject the squad-leader briefing on dispatch. The persisted preference
  // remembers the actor type so the next open defaults back to the squad.
  it("submits squad_id when the user picks a squad in the actor picker", async () => {
    mockSquadsData.list = [
      { id: "squad-1", name: "Frontend Squad", leader_id: "agent-1", archived_at: null },
    ];
    const user = userEvent.setup();
    const onClose = vi.fn();

    renderPanel({ onClose, isExpanded: false, setIsExpanded: vi.fn() });

    // The picker mock renders both sections inline as buttons; click the
    // squad row directly.
    await user.click(screen.getByRole("button", { name: /Frontend Squad/ }));

    const editor = screen.getByPlaceholderText(
      'Tell the agent what to do, e.g. "let Bohan fix the inbox loading slowness in the Web project"',
    );
    await user.clear(editor);
    await user.type(editor, "Investigate the regression");

    await user.click(screen.getByRole("button", { name: /^Create$/i }));

    await waitFor(() => {
      expect(mockQuickCreateIssue).toHaveBeenCalledWith({
        squad_id: "squad-1",
        prompt: "Investigate the regression",
        project_id: undefined,
      });
    });
    expect(mockSetLastActor).toHaveBeenCalledWith("squad", "squad-1");
  });

  // Squads whose leader agent isn't visible (archived, private, etc.) must
  // not appear in the picker — the backend would reject the pick on
  // validateAssigneePair, and showing them invites a confusing dead path.
  it("hides squads whose leader agent is not in the visible-agents list", () => {
    mockSquadsData.list = [
      { id: "squad-orphan", name: "Orphan Squad", leader_id: "agent-missing", archived_at: null },
    ];

    renderPanel({ onClose: vi.fn(), isExpanded: false, setIsExpanded: vi.fn() });

    expect(screen.queryByRole("button", { name: /Orphan Squad/ })).toBeNull();
  });

  // A successful create used to persist its project, so the NEXT open
  // re-seeded the pill with it and quietly filed the following issue into the
  // same place. The target project belongs to the issue being filed, not to
  // the user as a standing preference — the actor is the only thing that
  // carries over now (MUL-5862).
  describe("project is not remembered across creates", () => {
    it("ignores a lastProjectId left behind by an older build", () => {
      mockQuickCreateStore.lastProjectId = "proj-1";
      mockProjectsQuery.data = [{ id: "proj-1", title: "Web", icon: null }];
      mockProjectsQuery.isSuccess = true;

      renderPanel({ onClose: vi.fn(), isExpanded: false, setIsExpanded: vi.fn() });

      // Seeding from it is exactly the removed behavior — the pill must be
      // empty, and with no value there is nothing to clear.
      expect(screen.getByTestId("project-picker")).toHaveTextContent("Project none");
      expect(screen.queryByRole("button", { name: "Clear project" })).not.toBeInTheDocument();
    });

    it("still submits the project picked in this session", async () => {
      // Guard against over-removal: dropping the memory must not drop the
      // field from the outgoing request.
      const user = userEvent.setup();
      mockProjectsQuery.data = [{ id: "proj-1", title: "Web", icon: null }];
      mockProjectsQuery.isSuccess = true;

      renderPanel({ onClose: vi.fn(), isExpanded: false, setIsExpanded: vi.fn() });

      await user.click(screen.getByRole("button", { name: /Bohan/ }));
      await user.click(screen.getByTestId("project-picker"));
      await user.type(
        screen.getByPlaceholderText(
          'Tell the agent what to do, e.g. "let Bohan fix the inbox loading slowness in the Web project"',
        ),
        "Ship it",
      );
      await user.click(screen.getByRole("button", { name: /^Create$/i }));

      await waitFor(() => {
        expect(mockQuickCreateIssue).toHaveBeenCalledWith(
          expect.objectContaining({ project_id: "proj-1" }),
        );
      });
      // The actor is still remembered — only the project memory is gone.
      expect(mockSetLastActor).toHaveBeenCalledWith("agent", "agent-1");
    });
  });

  // If the unfinished draft points at a project that has been deleted (or
  // moved to another workspace), the modal must not keep submitting that dead
  // UUID. Once the projects query resolves and the id is missing, we clear
  // BOTH local state and the draft; dropping only local state would leave the
  // next open re-seeding the same dead value and trigger the server's
  // `project not found` rejection. The draft is now the only persisted copy —
  // the last-create memory is gone (MUL-5862).
  it("clears a stale drafted project once the projects list resolves without it", async () => {
    mockIssueDraftStore.draft.shared.projectId = "deleted-proj";
    mockProjectsQuery.data = [];
    mockProjectsQuery.isSuccess = true;

    renderPanel({ onClose: vi.fn(), isExpanded: false, setIsExpanded: vi.fn() });

    await waitFor(() => {
      expect(mockSetShared).toHaveBeenCalledWith({ projectId: undefined });
    });
    expect(screen.getByTestId("project-picker")).toHaveTextContent("Project none");
  });

  // Dropping a project used to cost two clicks — open the popover, hit
  // "No project" — because the pill had no clear affordance of its own
  // (MUL-5862). The × is part of the pill, so it only exists once the field
  // has a value to drop.
  describe("project pill quick-clear", () => {
    it("has no × while no project is selected", () => {
      renderPanel({ onClose: vi.fn(), isExpanded: false, setIsExpanded: vi.fn() });

      expect(screen.getByTestId("project-picker")).toHaveTextContent("Project none");
      expect(screen.queryByRole("button", { name: "Clear project" })).not.toBeInTheDocument();
    });

    it("clears local state and the shared draft in one click", async () => {
      const user = userEvent.setup();
      mockIssueDraftStore.draft.shared.projectId = "proj-1";
      mockProjectsQuery.data = [{ id: "proj-1", title: "Web", icon: null }];
      mockProjectsQuery.isSuccess = true;

      renderPanel({ onClose: vi.fn(), isExpanded: false, setIsExpanded: vi.fn() });
      expect(screen.getByTestId("project-picker")).toHaveTextContent("Project proj-1");

      await user.click(screen.getByRole("button", { name: "Clear project" }));

      expect(screen.getByTestId("project-picker")).toHaveTextContent("Project none");
      expect(mockSetShared).toHaveBeenCalledWith({ projectId: undefined });
      // The × retires with the value it cleared.
      expect(screen.queryByRole("button", { name: "Clear project" })).not.toBeInTheDocument();
    });
  });

  // Mirror case: while the query is still loading, we must NOT preemptively
  // clear the drafted project — that would wipe a perfectly valid selection
  // on every open before the list ever renders.
  it("keeps the drafted project while the projects list is still loading", () => {
    mockIssueDraftStore.draft.shared.projectId = "proj-1";
    mockProjectsQuery.data = [];
    mockProjectsQuery.isSuccess = false;

    renderPanel({ onClose: vi.fn(), isExpanded: false, setIsExpanded: vi.fn() });

    expect(mockSetShared).not.toHaveBeenCalled();
    expect(screen.getByTestId("project-picker")).toHaveTextContent("Project proj-1");
  });

  // When the modal was opened from "Add sub issue" on an existing issue,
  // the manual panel transfers parent_issue_id through the `data` payload
  // on switch-to-agent. The agent panel must forward that UUID to the
  // quick-create API silently — without surfacing a parent picker — so the
  // new issue is filed as a sub-issue. Dropping parent_issue_id here was
  // the original bug; this locks the wiring in.
  it("forwards parent_issue_id from the carry payload to the quick-create API", async () => {
    const user = userEvent.setup();

    renderPanel({
      onClose: vi.fn(),
      isExpanded: false,
      setIsExpanded: vi.fn(),
      data: {
        parent_issue_id: "parent-uuid-1",
        parent_issue_identifier: "MUL-2534",
      },
    });

    // Sub-issue context chip is visible so the user knows the new issue
    // will be filed as a sub-issue.
    expect(screen.getByTestId("agent-sub-issue-chip")).toBeInTheDocument();

    const editor = screen.getByPlaceholderText(
      'Tell the agent what to do, e.g. "let Bohan fix the inbox loading slowness in the Web project"',
    );
    await user.clear(editor);
    await user.type(editor, "Investigate the regression");

    await user.click(screen.getByRole("button", { name: /^Create$/i }));

    await waitFor(() => {
      expect(mockQuickCreateIssue).toHaveBeenCalledWith({
        agent_id: "agent-1",
        prompt: "Investigate the regression",
        project_id: undefined,
        parent_issue_id: "parent-uuid-1",
      });
    });
  });

  // The sub-issue chip is purely opt-in context — it only appears when the
  // modal was opened from an "Add sub issue" entry. A plain quick-create
  // (no parent in data) must NOT render the chip; otherwise users would see
  // a stray badge on every quick-create.
  it("does not render the sub-issue chip when no parent is seeded", () => {
    renderPanel({ onClose: vi.fn(), isExpanded: false, setIsExpanded: vi.fn() });
    expect(screen.queryByTestId("agent-sub-issue-chip")).toBeNull();
  });

  // MUL-4808 — Quick Create already gated Create; these pin the two gaps:
  // the mode switch (which re-serializes the prompt into the manual draft)
  // and the file button that used to lock during an upload for no reason.
  describe("upload submit gate", () => {
    function startPendingUpload() {
      let release!: (result: unknown) => void;
      mockApiUploadFile.mockImplementationOnce(
        () => new Promise((resolve) => { release = resolve; }),
      );
      fireEvent.click(screen.getByRole("button", { name: "Mock editor upload" }));
      return { release: (result: unknown) => release(result) };
    }

    it("blocks Switch to Manual while an upload is in flight", async () => {
      const onSwitchMode = vi.fn();
      renderPanel({ onClose: vi.fn(), onSwitchMode, isExpanded: false, setIsExpanded: vi.fn() });

      startPendingUpload();

      // The switch hands the serialized prompt to the manual panel — mid-upload
      // that prompt has already lost the pending image.
      const switchButton = screen.getByRole("button", { name: /Switch to Manual/i });
      await waitFor(() => expect(switchButton).toBeDisabled());
      fireEvent.click(switchButton);
      expect(onSwitchMode).not.toHaveBeenCalled();
    });

    it("keeps the attach-file button usable during an upload so files can queue", async () => {
      renderPanel({ onClose: vi.fn(), isExpanded: false, setIsExpanded: vi.fn() });

      startPendingUpload();

      // Each file is its own queue entry — making users wait for the first to
      // land before picking the second was a restriction with no race behind
      // it, and this issue explicitly removed it.
      const submit = await screen.findByRole("button", { name: "Uploading…" });
      expect(submit).toBeDisabled();
      expect(screen.getByRole("button", { name: "Upload file" })).not.toBeDisabled();
    });
  });

  // MUL-4931 — this path files a real issue, so a double-fire is a duplicate
  // issue, not a cosmetic glitch. `submitting` is state: two chords landing in
  // one tick both read the pre-update value, so only a synchronously-flipped
  // ref can gate it. Mirrors the manual-create regression.
  describe("send shortcut single-flight", () => {
    it("creates once when the send chord fires twice in the same tick", async () => {
      // Hold the request open so both presses land inside the in-flight window.
      let release!: (v: unknown) => void;
      mockQuickCreateIssue.mockImplementationOnce(
        () => new Promise((resolve) => { release = resolve; }),
      );

      renderPanel({ onClose: vi.fn(), isExpanded: false, setIsExpanded: vi.fn() });

      const editor = screen.getByPlaceholderText(
        'Tell the agent what to do, e.g. "let Bohan fix the inbox loading slowness in the Web project"',
      );

      // Both presses inside ONE act: React cannot re-render between them, so
      // the second handler still closes over `submitting === false`. fireEvent
      // would flush in between and hide the race entirely.
      await act(async () => {
        const press = () =>
          editor.dispatchEvent(
            new KeyboardEvent("keydown", { key: "Enter", metaKey: true, bubbles: true }),
          );
        press();
        press();
      });

      await act(async () => { release(undefined); });

      expect(mockQuickCreateIssue).toHaveBeenCalledTimes(1);
    });
  });
});
