// @vitest-environment jsdom
// Store-backed lifecycle tests cover mode switches and parent-before-child
// layout cleanup without mocking the draft store.
import { forwardRef, useImperativeHandle, useRef, useState, type ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../locales/en/common.json";
import enModals from "../locales/en/modals.json";
import enEditor from "../locales/en/editor.json";
import enIssues from "../locales/en/issues.json";

const TEST_RESOURCES = {
  en: { common: enCommon, modals: enModals, editor: enEditor, issues: enIssues },
};

const { SOURCE_CONTEXT_PREVIEW } = vi.hoisted(() => ({ SOURCE_CONTEXT_PREVIEW: {
  source_issue: {
    id: "issue-source",
    identifier: "MUL-9",
    number: 9,
    title: "Source",
    description: "Historical body",
    created_at: "2026-08-20T00:00:00Z",
    updated_at: "2026-08-21T00:00:00Z",
    revision: 1,
    attachments: [],
  },
  comment_thread: [{
    id: "comment-source",
    parent_id: null,
    type: "comment",
    content: "Historical comment",
    author: { type: "member", id: "user-1", name: "Alice" },
    created_at: "2026-08-21T00:00:00Z",
    updated_at: "2026-08-21T00:00:00Z",
    revision: 1,
    attachments: [],
  }],
  anchor_comment_id: "comment-source",
  capture_token: "sha256:preview-token",
  limits: {
    comment_count: 1,
    text_bytes: 100,
    attachment_count: 0,
    attachment_bytes: 0,
  },
} }));

const mockCreateIssue = vi.hoisted(() => vi.fn());
const mockCreateCommentSubIssue = vi.hoisted(() => vi.fn());
const mockListProperties = vi.hoisted(() => vi.fn());

// A single `ApiError` identity for the component's cross-realm `instanceof`
// check; the schema helpers stay real so the drift-fallback branch runs the
// actual validation logic.
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

vi.mock("../navigation/context", () => ({
  useNavigation: () => ({ push: vi.fn() }),
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

vi.mock("./use-issue-limit-upgrade-prompt", () => ({
  useIssueLimitUpgradePrompt: () => vi.fn(),
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
  sourceContextPreviewOptions: (wsId: string, anchorCommentId: string) => ({
    queryKey: ["issues", wsId, "source-context", anchorCommentId],
    queryFn: () => Promise.resolve(SOURCE_CONTEXT_PREVIEW),
  }),
}));

vi.mock("../issues/hooks/use-issue-trigger-preview", () => ({
  useIssueTriggerPreview: () => ({ triggers: [], totalCount: 0, isLoading: false }),
}));

vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({ getActorName: () => "Agent" }),
}));

vi.mock("../common/actor-avatar", () => ({
  ActorAvatar: () => null,
}));

vi.mock("@multica/core/issues/mutations", () => ({
  useCreateIssue: () => ({ mutateAsync: mockCreateIssue }),
  useCreateCommentSubIssue: () => ({
    mutateAsync: ({ anchorCommentId, data }: { anchorCommentId: string; data: unknown }) =>
      mockCreateCommentSubIssue(anchorCommentId, data),
  }),
  useUpdateIssue: () => ({ mutate: vi.fn() }),
}));

vi.mock("@multica/core/labels", () => ({
  useAttachLabelToIssue: () => ({ mutateAsync: vi.fn() }),
}));

vi.mock("@multica/core/properties", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/properties")>();
  return {
    ...actual,
    useSetIssueProperty: () => ({ mutateAsync: vi.fn() }),
  };
});

vi.mock("@multica/core/api", async () => {
  const { parseWithFallback } = await vi.importActual<typeof import("@multica/core/api/schema")>(
    "@multica/core/api/schema",
  );
  const { DuplicateIssueErrorBodySchema } = await vi.importActual<
    typeof import("@multica/core/api/schemas")
  >("@multica/core/api/schemas");
  return {
    api: {
      listProperties: mockListProperties,
      uploadFile: vi.fn(),
    },
    ApiError,
    parseWithFallback,
    DuplicateIssueErrorBodySchema,
  };
});

vi.mock("../editor", async () => {
  const uploadGate = await vi.importActual<typeof import("../editor/use-upload-gate")>(
    "../editor/use-upload-gate",
  );
  const composer = await vi.importActual<typeof import("../editor/use-composer-submit")>(
    "../editor/use-composer-submit",
  );

  const ContentEditor = forwardRef(({ defaultValue, onUpdate, placeholder }: any, ref: any) => {
    const valueRef = useRef(defaultValue || "");
    const [value, setValue] = useState(defaultValue || "");
    // Mirrors the real editor's debounce: `flushPendingUpdate` reports a value
    // only when an update is still unflushed, and `null`/`undefined` otherwise
    // (the create flow treats "nothing pending" as "no edit to re-snapshot").
    const pendingRef = useRef(false);
    useImperativeHandle(ref, () => ({
      getMarkdown: () => valueRef.current,
      flushPendingUpdate: () => {
        if (!pendingRef.current) return undefined;
        pendingRef.current = false;
        return valueRef.current;
      },
      clearContent: () => {
        pendingRef.current = false;
        valueRef.current = "";
        setValue("");
      },
      hasActiveUploads: () => false,
      insertUploadPlaceholder: () => true,
      settleUploadPlaceholder: () => false,
    }));
    return (
      <textarea
        value={value}
        placeholder={placeholder}
        onChange={(e) => {
          pendingRef.current = true;
          valueRef.current = e.target.value;
          setValue(e.target.value);
          onUpdate?.(e.target.value);
        }}
      />
    );
  });
  ContentEditor.displayName = "ContentEditor";

  const TitleEditor = forwardRef(
    ({ defaultValue, placeholder, onChange, onSubmit }: any, ref: any) => {
      const valueRef = useRef(defaultValue || "");
      const [value, setValue] = useState(defaultValue || "");
      const inputRef = useRef<HTMLInputElement>(null);
      useImperativeHandle(ref, () => ({
        getText: () => valueRef.current,
        focus: () => inputRef.current?.focus(),
        focusAtCoords: () => inputRef.current?.focus(),
      }));
      return (
        <input
          ref={inputRef}
          value={value}
          placeholder={placeholder}
          onChange={(e) => {
            valueRef.current = e.target.value;
            setValue(e.target.value);
            onChange?.(e.target.value);
          }}
          onKeyDown={(e) => {
            if (e.key === "Enter") onSubmit?.();
          }}
        />
      );
    },
  );
  TitleEditor.displayName = "TitleEditor";

  return {
    ...uploadGate,
    ...composer,
    useFileDropZone: () => ({ isDragOver: false, dropZoneProps: {} }),
    FileDropOverlay: () => null,
    ContentEditor,
    TitleEditor,
  };
});

vi.mock("../issues/components", () => ({
  StatusIcon: ({ status }: { status: string }) => <span data-testid="status-icon">{status}</span>,
  // Drives the two field-level edits a batch continuation must tell apart from
  // the inherited values it started with.
  StatusPicker: ({ onUpdate }: { onUpdate: (u: { status: string }) => void }) => (
    <button type="button" data-testid="status-picker" onClick={() => onUpdate({ status: "done" })} />
  ),
  PriorityPicker: ({ onUpdate }: { onUpdate: (u: { priority: string }) => void }) => (
    <button type="button" data-testid="priority-picker" onClick={() => onUpdate({ priority: "high" })} />
  ),
  StagePicker: () => <div data-testid="stage-picker" />,
  AssigneePicker: () => <div data-testid="assignee-picker" />,
  StartDatePicker: () => <div data-testid="start-date-picker" />,
  DueDatePicker: () => <div data-testid="due-date-picker" />,
  LabelPicker: () => <div data-testid="label-picker" />,
}));

vi.mock("../projects/components/project-picker", () => ({
  ProjectPicker: () => <div data-testid="project-picker" />,
}));

vi.mock("@multica/ui/components/ui/dialog", () => ({
  Dialog: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  DialogContent: ({ children, className }: { children: ReactNode; className?: string }) => (
    <div className={className}>{children}</div>
  ),
  DialogTitle: ({ children, className }: { children: ReactNode; className?: string }) => (
    <div className={className}>{children}</div>
  ),
}));

vi.mock("@multica/ui/components/ui/dropdown-menu", () => {
  const Pass = ({ children }: { children: ReactNode }) => <>{children}</>;
  return {
    DropdownMenu: Pass,
    DropdownMenuTrigger: ({ render }: { render: ReactNode }) => <>{render}</>,
    DropdownMenuContent: Pass,
    DropdownMenuItem: ({ children, onClick }: { children: ReactNode; onClick?: () => void }) => (
      <button type="button" onClick={onClick}>{children}</button>
    ),
    DropdownMenuSeparator: () => null,
    DropdownMenuSub: Pass,
    DropdownMenuSubTrigger: Pass,
    DropdownMenuSubContent: Pass,
  };
});

vi.mock("@multica/ui/components/ui/tooltip", () => {
  const Pass = ({ children }: { children: ReactNode }) => <>{children}</>;
  return {
    Tooltip: Pass,
    TooltipTrigger: ({ render }: { render: ReactNode }) => <>{render}</>,
    TooltipContent: Pass,
    TooltipProvider: Pass,
  };
});

vi.mock("@multica/ui/components/ui/button", () => ({
  Button: ({
    children,
    disabled,
    onClick,
    type = "button",
    ...rest
  }: {
    children: ReactNode;
    disabled?: boolean;
    onClick?: () => void;
    type?: "button" | "submit" | "reset";
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
  FileUploadButton: () => null,
}));

vi.mock("@multica/ui/lib/utils", () => ({
  cn: (...values: Array<string | false | null | undefined>) => values.filter(Boolean).join(" "),
}));

vi.mock("sonner", () => ({
  toast: { custom: vi.fn(), dismiss: vi.fn(), error: vi.fn() },
}));

vi.mock("./issue-picker-modal", () => ({
  IssuePickerModal: () => null,
}));

import { ManualCreatePanel } from "./create-issue";
import { CreateIssueDialog } from "./create-issue-dialog";
// The REAL stores: this suite exists to exercise the real cleanup predicate and
// the real persistence around it.
import {
  useIssueDraftStore,
  type IssueCreateDraft,
} from "@multica/core/issues/stores/draft-store";
import { useQuickCreateStore } from "@multica/core/issues/stores/quick-create-store";

const emptyDraft = (): IssueCreateDraft => ({
  shared: { projectId: undefined, priority: "none", dueDate: null, attachments: [] },
  manual: {
    title: "",
    description: "",
    status: "todo",
    startDate: null,
    assigneeType: undefined,
    assigneeId: undefined,
    labelIds: [],
    propertyValues: {},
  },
  agent: { prompt: "", actorType: undefined, actorId: undefined },
  activeMode: "manual",
});

function I18nWrapper({ children }: { children: ReactNode }) {
  return (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      {children}
    </I18nProvider>
  );
}

function renderUi(element: React.ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <I18nWrapper>
      <QueryClientProvider client={qc}>{element}</QueryClientProvider>
    </I18nWrapper>,
  );
}

const draftState = () => useIssueDraftStore.getState();

beforeEach(() => {
  vi.clearAllMocks();
  useIssueDraftStore.setState({
    draft: emptyDraft(),
    isolatedDraftBackup: undefined,
    batchContinuationBaseline: undefined,
    lastAssigneeType: undefined,
    lastAssigneeId: undefined,
  });
  useQuickCreateStore.setState({ keepOpen: false });
  mockListProperties.mockResolvedValue({ properties: [] });
  const createdIssue = {
    id: "issue-1",
    identifier: "MUL-1",
    title: "Created",
    status: "todo",
    labels: [],
  };
  mockCreateIssue.mockResolvedValue(createdIssue);
  mockCreateCommentSubIssue.mockResolvedValue(createdIssue);
});

/**
 * Stands in for the create-issue shell's local mode state
 * (create-issue-dialog.tsx owns the real one): flipping to agent unmounts this
 * panel exactly like the shell does, which is the lifecycle under test. The
 * shell's own mode/carry wiring is covered by create-issue-dialog.test.tsx.
 */
function ModeSwitchHarness({ data }: { data?: Record<string, unknown> | null }) {
  const [mode, setMode] = useState<"manual" | "agent">("manual");
  const [isExpanded, setIsExpanded] = useState(false);
  if (mode === "agent") return null;
  return (
    <ManualCreatePanel
      onClose={() => {}}
      onSwitchMode={() => setMode("agent")}
      data={data}
      isExpanded={isExpanded}
      setIsExpanded={setIsExpanded}
    />
  );
}

/** Keep the batch pattern for the next issue: submit once with keep-open on. */
async function submitAndKeepOpen(user: ReturnType<typeof userEvent.setup>) {
  await user.type(screen.getByPlaceholderText("Issue title"), "First batch issue");
  await user.click(screen.getByRole("button", { name: "Create Issue" }));
  await waitFor(() => expect(draftState().batchContinuationBaseline).toBeDefined());
  expect(screen.getByPlaceholderText("Issue title")).toHaveValue("");
}

describe("keep-open continuation cleanup", () => {
  it("closes a continuation that was only switched to agent mode as a fresh todo create", async () => {
    const user = userEvent.setup();
    useQuickCreateStore.setState({ keepOpen: true });

    const modal = renderUi(<ModeSwitchHarness data={{ status: "in_progress" }} />);
    await submitAndKeepOpen(user);
    // The inherited status is what the next issue in the batch starts from.
    expect(draftState().draft.manual.status).toBe("in_progress");

    // Only a mode switch — no manual field was touched.
    await user.click(screen.getByRole("button", { name: "Switch to Agent" }));

    // The continuation is gone, so the batch status must not reach the next
    // fresh manual create.
    expect(draftState().draft.manual.status).toBe("todo");
    expect(draftState().batchContinuationBaseline).toBeUndefined();
    expect(draftState().hasDraft()).toBe(false);

    modal.unmount();

    renderUi(<ModeSwitchHarness />);
    await user.type(screen.getByPlaceholderText("Issue title"), "Fresh manual issue");
    await user.click(screen.getByRole("button", { name: "Create Issue" }));
    await waitFor(() =>
      expect(mockCreateIssue).toHaveBeenLastCalledWith(
        expect.objectContaining({ status: "todo" }),
      ),
    );
  });

  it("keeps a priority-only edit as-is, inherited batch status included", async () => {
    const user = userEvent.setup();
    useQuickCreateStore.setState({ keepOpen: true });

    const modal = renderUi(<ModeSwitchHarness data={{ status: "in_progress" }} />);
    await submitAndKeepOpen(user);

    // Priority is a manual-form field, so it is a real selection the user made
    // for the next issue — unlike the status the batch inherited.
    await user.click(screen.getByTestId("priority-picker"));
    expect(draftState().draft.shared.priority).toBe("high");

    modal.unmount();

    // A genuine edit closes as the user's unfinished draft, status included.
    expect(draftState().draft.shared.priority).toBe("high");
    expect(draftState().draft.manual.status).toBe("in_progress");
  });

  it("keeps a status the user explicitly selected in the continuation", async () => {
    const user = userEvent.setup();
    useQuickCreateStore.setState({ keepOpen: true });

    const modal = renderUi(<ModeSwitchHarness data={{ status: "in_progress" }} />);
    await submitAndKeepOpen(user);

    await user.click(screen.getByTestId("status-picker"));

    modal.unmount();

    expect(draftState().draft.manual.status).toBe("done");
  });

  it("keeps a shared-field edit recoverable when the form switches to agent", async () => {
    const user = userEvent.setup();
    useQuickCreateStore.setState({ keepOpen: true });

    const modal = renderUi(<ModeSwitchHarness data={{ status: "in_progress" }} />);
    await submitAndKeepOpen(user);

    // A real selection for the next issue, made before the switch and
    // re-committed by it.
    await user.click(screen.getByTestId("priority-picker"));
    await user.click(screen.getByRole("button", { name: "Switch to Agent" }));

    expect(draftState().draft.shared.priority).toBe("high");
    // The selection is the user's, so the continuation closes as it is.
    expect(draftState().draft.manual.status).toBe("in_progress");
    expect(draftState().batchContinuationBaseline).toBeUndefined();

    modal.unmount();
    expect(draftState().draft.shared.priority).toBe("high");
  });

  it("preserves the seeded agent prompt of a started continuation across a switch", async () => {
    const user = userEvent.setup();
    useQuickCreateStore.setState({ keepOpen: true });

    const modal = renderUi(<ModeSwitchHarness data={{ status: "in_progress" }} />);
    await submitAndKeepOpen(user);

    // The next issue of the batch was started: a title is real content, and the
    // switch seeds the agent prompt from it.
    await user.type(screen.getByPlaceholderText("Issue title"), "Second batch issue");
    await user.click(screen.getByRole("button", { name: "Switch to Agent" }));

    const state = draftState();
    expect(state.draft.agent.prompt).toBe("Second batch issue");
    expect(state.draft.manual.title).toBe("Second batch issue");
    expect(state.draft.manual.status).toBe("in_progress");

    modal.unmount();
    expect(draftState().draft.manual.title).toBe("Second batch issue");
    expect(draftState().draft.agent.prompt).toBe("Second batch issue");
  });

  it("restores the ordinary draft when a source-context keep-open session closes", async () => {
    const user = userEvent.setup();
    useQuickCreateStore.setState({ keepOpen: true });
    // An ordinary field-only draft: priority + status, no title or body, so
    // nothing counts as draft content and the status is not a batch default.
    useIssueDraftStore.setState({
      draft: {
        ...emptyDraft(),
        shared: { ...emptyDraft().shared, priority: "high" },
        manual: { ...emptyDraft().manual, status: "backlog" },
      },
    });

    // The real shell, so the source-context wrapper's layout-effect cleanup runs
    // before the manual panel's on unmount.
    const modal = renderUi(
      <CreateIssueDialog
        onClose={() => {}}
        initialMode="manual"
        data={{ anchor_comment_id: "comment-source" }}
      />,
    );
    await screen.findByPlaceholderText("Issue title");
    await submitAndKeepOpen(user);

    modal.unmount();

    // The restored ordinary draft is not a batch continuation to discard.
    expect(draftState().isolatedDraftBackup).toBeUndefined();
    expect(draftState().draft.shared.priority).toBe("high");
    expect(draftState().draft.manual.status).toBe("backlog");

    renderUi(<ModeSwitchHarness />);
    await user.type(screen.getByPlaceholderText("Issue title"), "Ordinary follow-up");
    await user.click(screen.getByRole("button", { name: "Create Issue" }));
    await waitFor(() =>
      expect(mockCreateIssue).toHaveBeenLastCalledWith(
        expect.objectContaining({ status: "backlog", priority: "high" }),
      ),
    );
  });
});
