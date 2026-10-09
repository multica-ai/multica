import { forwardRef, useEffect, useRef, useState, useImperativeHandle } from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Issue } from "@multica/core/types";
import { I18nProvider } from "@multica/core/i18n/react";
import enAgents from "../../locales/en/agents.json";
import enCommon from "../../locales/en/common.json";
import enIssues from "../../locales/en/issues.json";

const TEST_RESOURCES = { en: { agents: enAgents, common: enCommon, issues: enIssues } };

// ---------------------------------------------------------------------------
// Mocks (minimal set required to mount IssueDetail)
// ---------------------------------------------------------------------------

vi.mock("@multica/ui/hooks/use-mobile", () => ({
  useIsMobile: () => false,
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

const mockAuthUser = { id: "user-1", email: "test@test.com", name: "Test User" };
vi.mock("@multica/core/auth", () => ({
  useAuthStore: Object.assign(
    (selector?: any) => {
      const state = { user: mockAuthUser, isAuthenticated: true };
      return selector ? selector(state) : state;
    },
    { getState: () => ({ user: mockAuthUser, isAuthenticated: true }) },
  ),
  registerAuthStore: vi.fn(),
  createAuthStore: vi.fn(),
}));

vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({
    getMemberName: () => "Test User",
    getAgentName: () => "Unknown Agent",
    getActorName: () => "Test User",
    getActorInitials: () => "TU",
    getActorAvatarUrl: () => null,
  }),
}));

vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: () => ({
    queryKey: ["workspaces", "ws-1", "members"],
    queryFn: () => Promise.resolve([{ user_id: "user-1", name: "Test User", email: "test@test.com", role: "admin" }]),
  }),
  agentListOptions: () => ({
    queryKey: ["workspaces", "ws-1", "agents"],
    queryFn: () => Promise.resolve([]),
  }),
  squadListOptions: () => ({
    queryKey: ["workspaces", "ws-1", "squads"],
    queryFn: () => Promise.resolve([]),
  }),
  assigneeFrequencyOptions: () => ({
    queryKey: ["workspaces", "ws-1", "assignee-frequency"],
    queryFn: () => Promise.resolve([]),
  }),
  workspaceListOptions: () => ({
    queryKey: ["workspaces"],
    queryFn: () => Promise.resolve([{ id: "ws-1", name: "Test WS", slug: "test" }]),
  }),
}));

vi.mock("@multica/core/paths", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/paths")>("@multica/core/paths");
  return {
    ...actual,
    useCurrentWorkspace: () => ({ id: "ws-1", name: "Test WS", slug: "test" }),
    useWorkspacePaths: () => actual.paths.workspace("test"),
  };
});

vi.mock("../../navigation", () => ({
  AppLink: ({ children, href, ...props }: any) => <a href={href} {...props}>{children}</a>,
  useNavigation: () => ({ push: vi.fn(), pathname: "/issues/issue-1", getShareableUrl: (p: string) => `https://app.multica.com${p}` }),
  useBackOrReplace: () => vi.fn(),
  NavigationProvider: ({ children }: { children: React.ReactNode }) => children,
}));

vi.mock("../../editor", async () => ({
  ...(await vi.importActual<typeof import("../../editor/use-lazy-editor")>("../../editor/use-lazy-editor")),
  ...(await vi.importActual<typeof import("../../editor/use-upload-gate")>("../../editor/use-upload-gate")),
  ...(await vi.importActual<typeof import("../../editor/use-composer-submit")>("../../editor/use-composer-submit")),
  useEditorUpload: () => ({ uploadWithToast: vi.fn(), upload: vi.fn(), uploading: false }),
  useFileDropZone: () => ({ isDragOver: false, dropZoneProps: {} }),
  FileDropOverlay: () => null,
  useDownloadAttachment: () => vi.fn(),
  useAttachmentPreview: () => ({ open: vi.fn(), tryOpen: () => false, modal: null }),
  PreviewSequenceProvider: ({ children }: { children: React.ReactNode }) => children,
  usePreviewSequence: () => ({ openAt: () => false }),
  collectPreviewSequence: () => [],
  AttachmentDownloadProvider: ({ children }: { children: React.ReactNode }) => children,
  Attachment: ({ attachment }: { attachment: { filename?: string } }) => <span>{attachment.filename}</span>,
  isPreviewable: () => false,
  ReadonlyContent: ({ content }: { content: string }) => <div data-testid="readonly-content">{content}</div>,
  ContentEditor: forwardRef(function MockContentEditor({ defaultValue, placeholder, onReady }: any, ref: any) {
    const [value, setValue] = useState(defaultValue ?? "");
    useEffect(() => { onReady?.(); }, []); // eslint-disable-line react-hooks/exhaustive-deps
    useImperativeHandle(ref, () => ({
      getMarkdown: () => value,
      clearContent: () => setValue(""),
      adoptContent: (md: string) => setValue(md),
      focus: () => {},
      focusAtCoords: () => {},
      blur: () => {},
      hasActiveUploads: () => false,
      insertUploadPlaceholder: () => true,
      settleUploadPlaceholder: () => false,
      uploadFile: () => {},
    }));
    return <textarea value={value} onChange={(e) => setValue(e.target.value)} placeholder={placeholder} data-testid="rich-text-editor" />;
  }),
  TitleEditor: forwardRef(function MockTitleEditor({ defaultValue, placeholder, onBlur, onChange, onReady }: any, ref: any) {
    const valueRef = useRef(defaultValue || "");
    const [value, setValue] = useState(defaultValue || "");
    useEffect(() => { onReady?.(); }, []); // eslint-disable-line react-hooks/exhaustive-deps
    useImperativeHandle(ref, () => ({ getText: () => valueRef.current, focus: () => {}, focusAtCoords: () => {} }));
    return (
      <input
        value={value}
        onChange={(e) => { valueRef.current = e.target.value; setValue(e.target.value); onChange?.(e.target.value); }}
        onBlur={() => onBlur?.(valueRef.current)}
        placeholder={placeholder}
        data-testid="title-editor"
      />
    );
  }),
}));

vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: ({ actorType, actorId }: any) => <span>{actorType}:{actorId}</span>,
}));
vi.mock("../../projects/components/project-picker", () => ({
  ProjectPicker: () => <span data-testid="project-picker">Project</span>,
}));

const mockApiObj = vi.hoisted(() => ({
  getIssue: vi.fn(),
  listTimeline: vi.fn().mockResolvedValue([]),
  listComments: vi.fn().mockResolvedValue([]),
  createComment: vi.fn(),
  updateComment: vi.fn(),
  deleteComment: vi.fn(),
  deleteIssue: vi.fn(),
  updateIssue: vi.fn(),
  listIssueSubscribers: vi.fn().mockResolvedValue([]),
  subscribeToIssue: vi.fn().mockResolvedValue(undefined),
  unsubscribeFromIssue: vi.fn().mockResolvedValue(undefined),
  unsubscribeFromIssueSubtree: vi.fn().mockResolvedValue(undefined),
  getActiveTasksForIssue: vi.fn().mockResolvedValue({ tasks: [] }),
  listTasksByIssue: vi.fn().mockResolvedValue([]),
  rerunIssue: vi.fn(),
  listTaskMessages: vi.fn().mockResolvedValue([]),
  listChildIssues: vi.fn().mockResolvedValue({ issues: [] }),
  getChildIssueProgress: vi.fn().mockResolvedValue({ progress: [] }),
  getAgentTaskSnapshot: vi.fn().mockResolvedValue([]),
  getWorkspaceWorkingAgents: vi.fn().mockResolvedValue([]),
  listProperties: vi.fn().mockResolvedValue({ properties: [], total: 0 }),
  listIssues: vi.fn().mockResolvedValue({ issues: [], total: 0 }),
  uploadFile: vi.fn(),
  listIssueReactions: vi.fn().mockResolvedValue([]),
  addIssueReaction: vi.fn(),
  removeIssueReaction: vi.fn(),
  listAttachments: vi.fn().mockResolvedValue([]),
  addCommentReaction: vi.fn(),
  removeCommentReaction: vi.fn(),
  listMembers: vi.fn().mockResolvedValue([{ user_id: "user-1", name: "Test User", email: "test@test.com", role: "admin" }]),
  listAgents: vi.fn().mockResolvedValue([]),
  getAgent: vi.fn().mockResolvedValue(null),
  listRuntimes: vi.fn().mockResolvedValue([]),
  getProject: vi.fn(),
  listProjects: vi.fn().mockResolvedValue({ projects: [] }),
}));

vi.mock("@multica/core/api", () => ({
  api: mockApiObj,
  getApi: () => mockApiObj,
  setApiInstance: vi.fn(),
  errorCode: (error: unknown) =>
    typeof error === "object" && error !== null && "body" in error
      ? (error as { body?: { code?: string } }).body?.code
      : undefined,
}));

vi.mock("@multica/core/modals", () => ({
  useModalStore: Object.assign(
    (selector?: any) => {
      const state = { open: vi.fn(), modal: null };
      return selector ? selector(state) : state;
    },
    { getState: () => ({ open: vi.fn(), modal: null }) },
  ),
}));

vi.mock("@multica/core/hooks/use-file-upload", () => ({
  useFileUpload: () => ({ uploadWithToast: vi.fn().mockResolvedValue("https://example.com/file.png") }),
}));

vi.mock("@multica/core/realtime", () => ({
  useWSEvent: vi.fn(),
  useWSReconnect: vi.fn(),
  useWS: () => ({ subscribe: vi.fn(() => () => {}), onReconnect: vi.fn(() => () => {}) }),
  WSProvider: ({ children }: { children: React.ReactNode }) => children,
  useRealtimeSync: () => {},
}));

vi.mock("sonner", () => ({
  toast: { error: vi.fn(), success: vi.fn() },
}));

vi.mock("react-resizable-panels", () => ({
  Group: ({ children, ...props }: any) => <div {...props}>{children}</div>,
  Panel: ({ children, ...props }: any) => <div {...props}>{children}</div>,
  Separator: ({ children, ...props }: any) => <div {...props}>{children}</div>,
  useDefaultLayout: () => ({ defaultLayout: undefined, onLayoutChanged: vi.fn() }),
  usePanelRef: () => ({ current: { isCollapsed: () => false, expand: vi.fn(), collapse: vi.fn() } }),
}));

vi.mock("react-virtuoso", () => ({
  Virtuoso: forwardRef(function MockVirtuoso(
    { data, itemContent, computeItemKey }: { data: unknown[]; itemContent: (i: number, item: unknown) => unknown; computeItemKey: (i: number, item: unknown) => React.Key },
    ref: any,
  ) {
    useImperativeHandle(ref, () => ({ scrollIntoView: vi.fn(), scrollToIndex: vi.fn() }));
    return (
      <div data-testid="virtuoso-mock">
        {data.map((item, i) => <div key={computeItemKey(i, item)}>{itemContent(i, item) as React.ReactElement}</div>)}
      </div>
    );
  }),
}));

const emptyDraftAttachments = vi.hoisted(() => [] as unknown[]);
vi.mock("@multica/core/issues/stores", async () => ({
  ...(await vi.importActual<typeof import("@multica/core/issues/stores/resolved-expand-store")>(
    "@multica/core/issues/stores/resolved-expand-store",
  )),
  ...(await vi.importActual<typeof import("@multica/core/issues/stores/sub-issue-display-store")>(
    "@multica/core/issues/stores/sub-issue-display-store",
  )),
  ...(await vi.importActual<typeof import("@multica/core/issues/stores/sub-issues-collapse-store")>(
    "@multica/core/issues/stores/sub-issues-collapse-store",
  )),
  useRecentIssuesStore: Object.assign(
    (selector?: any) => {
      const state = { byWorkspace: {}, recordVisit: vi.fn(), pruneWorkspaces: vi.fn() };
      return selector ? selector(state) : state;
    },
    { getState: () => ({ byWorkspace: {}, recordVisit: vi.fn(), pruneWorkspaces: vi.fn() }) },
  ),
  selectRecentIssues: () => () => [],
  useCommentCollapseStore: (await vi.importActual<typeof import("zustand")>("zustand")).create<{
    collapsedByIssue: Record<string, string[]>;
    isCollapsed: (issueId: string, commentId: string) => boolean;
    toggle: (issueId: string, commentId: string) => void;
  }>()((set, get) => ({
    collapsedByIssue: {},
    isCollapsed: (issueId, commentId) => get().collapsedByIssue[issueId]?.includes(commentId) ?? false,
    toggle: (issueId, commentId) =>
      set((s) => {
        const current = s.collapsedByIssue[issueId] ?? [];
        return {
          collapsedByIssue: {
            ...s.collapsedByIssue,
            [issueId]: current.includes(commentId)
              ? current.filter((c) => c !== commentId)
              : [...current, commentId],
          },
        };
      }),
  })),
  useCommentDraftStore: Object.assign(
    (selector?: any) => {
      const state = {
        drafts: {} as Record<string, { content: string; attachments: unknown[]; updatedAt: number }>,
        getDraft: () => undefined,
        getAnnotations: () => emptyDraftAttachments,
        getAttachments: () => emptyDraftAttachments,
        getUploads: () => emptyDraftAttachments,
        setDraft: () => {},
        setAttachments: () => {},
        addUpload: () => {},
        settleUpload: () => {},
        failUpload: () => {},
        removeUpload: () => {},
        clearDraft: () => {},
      };
      return selector ? selector(state) : state;
    },
    {
      getState: () => ({
        drafts: {} as Record<string, { content: string; attachments: unknown[]; updatedAt: number }>,
        getDraft: () => undefined,
        getAnnotations: () => emptyDraftAttachments,
        getAttachments: () => emptyDraftAttachments,
        getUploads: () => emptyDraftAttachments,
        setDraft: () => {},
        setAttachments: () => {},
        addUpload: () => {},
        settleUpload: () => {},
        failUpload: () => {},
        removeUpload: () => {},
        clearDraft: () => {},
      }),
    },
  ),
  useCommentComposerStore: Object.assign(
    (selector?: any) => {
      const state = { sticky: true, runningAgentReply: "steer", toggleSticky: () => {} };
      return selector ? selector(state) : state;
    },
    { getState: () => ({ sticky: true, runningAgentReply: "steer", toggleSticky: () => {} }) },
  ),
}));

// ---------------------------------------------------------------------------
// Test data
// ---------------------------------------------------------------------------

const mockIssue: Issue = {
  id: "issue-1",
  workspace_id: "ws-1",
  number: 1,
  identifier: "TES-1",
  title: "Implement authentication",
  description: "Add JWT auth to the backend",
  status: "in_progress",
  priority: "high",
  assignee_type: "member",
  assignee_id: "user-1",
  creator_type: "member",
  creator_id: "user-1",
  parent_issue_id: null,
  project_id: null,
  position: 0,
  stage: null,
  start_date: null,
  due_date: null,
  metadata: {},
  properties: {},
  created_at: "2026-01-15T00:00:00Z",
  updated_at: "2026-01-20T00:00:00Z",
  revision: 1,
};

// ---------------------------------------------------------------------------
// Import under test
// ---------------------------------------------------------------------------

import { IssueDetail } from "./issue-detail";

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function renderDetail() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 }, mutations: { retry: false } } });
  return render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <QueryClientProvider client={qc}>
        <IssueDetail issueId="issue-1" />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

/** Fire a bare keydown on `document` as if focus is on the body. */
function pressKey(key: string, target: EventTarget = document.body) {
  fireEvent.keyDown(target, { key, bubbles: true, cancelable: true });
}

// ---------------------------------------------------------------------------
// Tests — issue property keyboard shortcuts (#8799)
// ---------------------------------------------------------------------------

describe("IssueDetail property keyboard shortcuts (#8799)", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockApiObj.getIssue.mockResolvedValue(mockIssue);
    mockApiObj.listTimeline.mockResolvedValue([]);
    mockApiObj.listIssueReactions.mockResolvedValue([]);
    mockApiObj.listIssueSubscribers.mockResolvedValue([]);
    mockApiObj.listChildIssues.mockResolvedValue({ issues: [] });
    mockApiObj.getChildIssueProgress.mockResolvedValue({ progress: [] });
    mockApiObj.getAgentTaskSnapshot.mockResolvedValue([]);
    mockApiObj.getWorkspaceWorkingAgents.mockResolvedValue([]);
    mockApiObj.listProperties.mockResolvedValue({ properties: [], total: 0 });
    mockApiObj.listIssues.mockResolvedValue({ issues: [], total: 0 });
    mockApiObj.getActiveTasksForIssue.mockResolvedValue({ tasks: [] });
    mockApiObj.listTasksByIssue.mockResolvedValue([]);
    mockApiObj.listTaskMessages.mockResolvedValue([]);
    mockApiObj.listAttachments.mockResolvedValue([]);
    mockApiObj.listMembers.mockResolvedValue([{ user_id: "user-1", name: "Test User", email: "test@test.com", role: "admin" }]);
    mockApiObj.listAgents.mockResolvedValue([]);
    mockApiObj.getProject.mockReset();
  });

  it("pressing S opens the status picker popover", async () => {
    renderDetail();

    // Wait for the issue to load and the properties sidebar to appear.
    await screen.findByText("Status");

    // No popover content visible yet.
    expect(document.querySelector('[data-slot="popover-content"]')).toBeNull();

    act(() => pressKey("S"));

    await waitFor(() => {
      expect(document.querySelector('[data-slot="popover-content"]')).not.toBeNull();
    });
  });

  it("pressing lowercase s also opens the status picker", async () => {
    renderDetail();
    await screen.findByText("Status");

    act(() => pressKey("s"));

    await waitFor(() => {
      expect(document.querySelector('[data-slot="popover-content"]')).not.toBeNull();
    });
  });

  it("S inside an input does not open the status picker", async () => {
    renderDetail();
    await screen.findByText("Status");

    const input = document.createElement("input");
    document.body.appendChild(input);
    input.focus();

    act(() => pressKey("S", input));

    // Give any async open a moment to register.
    await new Promise((r) => setTimeout(r, 50));
    expect(document.querySelector('[data-slot="popover-content"]')).toBeNull();

    input.remove();
  });

  it("pressing P reveals and opens the priority picker", async () => {
    renderDetail();
    await screen.findByText("Status");

    // Priority row starts hidden (issue.priority exists but the row is toggled by the shortcut).
    act(() => pressKey("P"));

    await waitFor(() => {
      // The priority row label should now be visible.
      expect(screen.queryByText("Priority")).not.toBeNull();
      // And the popover should be open.
      expect(document.querySelector('[data-slot="popover-content"]')).not.toBeNull();
    });
  });

  it("P inside a textarea does not open the priority picker", async () => {
    renderDetail();
    await screen.findByText("Status");

    const textarea = document.createElement("textarea");
    document.body.appendChild(textarea);
    textarea.focus();

    act(() => pressKey("P", textarea));

    await new Promise((r) => setTimeout(r, 50));
    expect(document.querySelector('[data-slot="popover-content"]')).toBeNull();

    textarea.remove();
  });
});
