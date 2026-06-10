import { forwardRef, useEffect, useRef, useState, useImperativeHandle } from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useActiveIssueContextStore } from "@multica/core/issues/stores/active-issue-context-store";
import type { Issue, TimelineEntry } from "@multica/core/types";
import type { ComponentProps } from "react";

const mockNavigationPush = vi.hoisted(() => vi.fn());
const mockNavigationReplace = vi.hoisted(() => vi.fn());
const mockNavigationSearchParams = vi.hoisted(() => ({
  current: new URLSearchParams(),
}));
const mockEventSources = vi.hoisted(() => [] as EventSourceMock[]);
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enIssues from "../../locales/en/issues.json";
import zhHansCommon from "../../locales/zh-Hans/common.json";
import zhHansIssues from "../../locales/zh-Hans/issues.json";
import type { SupportedLocale } from "@multica/core/i18n";

const TEST_RESOURCES = {
  en: { common: enCommon, issues: enIssues },
  "zh-Hans": { common: zhHansCommon, issues: zhHansIssues },
};

const mockViewport = vi.hoisted(() => ({ isMobile: false }));
const mockUploadResult = vi.hoisted(() => ({
  current: {
    id: "upload-1",
    workspace_id: "ws-1",
    issue_id: null,
    comment_id: null,
    chat_session_id: null,
    chat_message_id: null,
    uploader_type: "member",
    uploader_id: "user-1",
    filename: "upload.pdf",
    url: "https://cdn.example.test/upload.pdf",
    download_url: "https://cdn.example.test/upload.pdf",
    content_type: "application/pdf",
    size_bytes: 100,
    created_at: "2026-01-20T00:00:00Z",
    link: "https://cdn.example.test/upload.pdf",
  },
}));

vi.mock("@multica/ui/hooks/use-mobile", () => ({
  useIsMobile: () => mockViewport.isMobile,
}));

// useWorkspaceId() derives from useCurrentWorkspace (relative import inside
// @multica/core/hooks.tsx). vi.mock("@multica/core/paths") only intercepts
// the bare-specifier, not the internal relative import. Mock the hooks module
// directly so the bridge hook returns the test UUID.
vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

// Mock @multica/core/auth
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

// Mock @multica/core/workspace/hooks
vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({
    getMemberName: (id: string) => (id === "user-1" ? "Test User" : "Unknown"),
    getAgentName: (id: string) => (id === "agent-1" ? "Claude Agent" : "Unknown Agent"),
    getActorName: (type: string, id: string) => {
      if (type === "member" && id === "user-1") return "Test User";
      if (type === "agent" && id === "agent-1") return "Claude Agent";
      return "Unknown";
    },
    getActorInitials: (type: string) => (type === "member" ? "TU" : "CA"),
    getActorAvatarUrl: () => null,
  }),
}));

// Mock workspace queries
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
  mentionFrequencyOptions: () => ({
    queryKey: ["workspaces", "ws-1", "mention-frequency"],
    queryFn: () => Promise.resolve([]),
  }),
  workspaceListOptions: () => ({
    queryKey: ["workspaces"],
    queryFn: () => Promise.resolve([{ id: "ws-1", name: "Test WS", slug: "test" }]),
  }),
}));

// Mock @multica/core/paths — after the URL-driven workspace refactor,
// useCurrentWorkspace / useWorkspacePaths derive from the workspace slug in
// URL Context. Tests don't mount a real route, so we short-circuit to fixtures.
vi.mock("@multica/core/paths", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/paths")>(
    "@multica/core/paths",
  );
  return {
    ...actual,
    useCurrentWorkspace: () => ({ id: "ws-1", name: "Test WS", slug: "test" }),
    useWorkspacePaths: () => actual.paths.workspace("test"),
  };
});

// Mock navigation
vi.mock("../../navigation", () => ({
  AppLink: ({ children, href, ...props }: any) => (
    <a href={href} {...props}>
      {children}
    </a>
  ),
  useNavigation: () => ({
    push: mockNavigationPush,
    replace: mockNavigationReplace,
    pathname: "/issues/issue-1",
    searchParams: mockNavigationSearchParams.current,
    getShareableUrl: (p: string) => `https://app.multica.com${p}`,
  }),
  NavigationProvider: ({ children }: { children: React.ReactNode }) => children,
}));

// Mock editor components (Tiptap requires real DOM)
vi.mock("../../editor", () => ({
  useFileDropZone: () => ({ isDragOver: false, dropZoneProps: {} }),
  FileDropOverlay: () => null,
  AttachmentDownloadProvider: ({ children }: { children: React.ReactNode }) => (
    <>{children}</>
  ),
  AttachmentCard: ({
    filename,
    onPreview,
    onDownload,
  }: {
    filename: string;
    onPreview?: () => void;
    onDownload?: () => void;
  }) => (
    <div data-testid="issue-attachment-card">
      {filename}
      <button type="button" aria-label={`Preview ${filename}`} onClick={onPreview} />
      <button type="button" aria-label={`Download ${filename}`} onClick={onDownload} />
    </div>
  ),
  Attachment: ({ attachment }: { attachment: { kind: string; attachment?: { filename: string } } }) => (
    <div data-testid="issue-attachment-renderer">
      {attachment.kind === "record" ? attachment.attachment?.filename : ""}
    </div>
  ),
  // No-op so comment-card's AttachmentList can render without hitting the
  // real API singleton; tests that care about download wiring should write
  // dedicated specs against `use-download-attachment.test.tsx`.
  useDownloadAttachment: () => vi.fn(),
  // Inert preview hook — comment-card's AttachmentList uses it to gate the
  // Eye button. Dedicated coverage lives in attachment-preview-modal.test.tsx.
  useAttachmentPreview: () => ({
    open: vi.fn(),
    tryOpen: () => false,
    modal: null,
  }),
  isPreviewable: () => false,
  ReadonlyContent: ({ content }: { content: string }) => (
    <div data-testid="readonly-content">{content}</div>
  ),
  ContentEditor: forwardRef(function MockContentEditor(
    { defaultValue, onUpdate, onExternalSyncAccepted, onUploadFile, placeholder, hideAttachments }: any,
    ref: any,
  ) {
    const valueRef = useRef(defaultValue || "");
    const emittedRef = useRef(defaultValue || "");
    const [value, setValue] = useState(defaultValue || "");
    useEffect(() => {
      const incoming = defaultValue || "";
      if (valueRef.current !== emittedRef.current) return;
      if (valueRef.current !== incoming) {
        valueRef.current = incoming;
        emittedRef.current = incoming;
        setValue(incoming);
      }
      onExternalSyncAccepted?.(incoming);
    }, [defaultValue, onExternalSyncAccepted]);
    useImperativeHandle(ref, () => ({
      getMarkdown: () => valueRef.current,
      setMarkdown: (markdown: string) => { valueRef.current = markdown; emittedRef.current = markdown; setValue(markdown); },
      clearContent: () => { valueRef.current = ""; emittedRef.current = ""; setValue(""); },
      focus: () => {},
      uploadFile: (file: File) => { void onUploadFile?.(file); },
      uploadFiles: (files: File[]) => { files.forEach((file) => void onUploadFile?.(file)); },
      hasActiveUploads: () => false,
    }));
    return (
      <textarea
        value={value}
        onChange={(e) => {
          valueRef.current = e.target.value;
          setValue(e.target.value);
          onUpdate?.(e.target.value);
        }}
        placeholder={placeholder}
        data-hide-attachments={hideAttachments ? "true" : "false"}
        data-testid="rich-text-editor"
      />
    );
  }),
  TitleEditor: forwardRef(function MockTitleEditor(
    { defaultValue, placeholder, onBlur, onChange }: any,
    ref: any,
  ) {
    const valueRef = useRef(defaultValue || "");
    const [value, setValue] = useState(defaultValue || "");
    useImperativeHandle(ref, () => ({
      getText: () => valueRef.current,
      focus: () => {},
    }));
    return (
      <input
        value={value}
        onChange={(e) => {
          valueRef.current = e.target.value;
          setValue(e.target.value);
          onChange?.(e.target.value);
        }}
        onBlur={() => onBlur?.(valueRef.current)}
        placeholder={placeholder}
        data-testid="title-editor"
      />
    );
  }),
}));

// Mock common components
vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: ({ actorType, actorId }: any) => (
    <span data-testid="actor-avatar">
      {actorType}:{actorId}
    </span>
  ),
}));

vi.mock("../../projects/components/project-picker", () => ({
  ProjectPicker: () => <span data-testid="project-picker">Project</span>,
}));

// Mock api
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
  getActiveTasksForIssue: vi.fn().mockResolvedValue({ tasks: [] }),
  listTasksByIssue: vi.fn().mockResolvedValue([]),
  listTaskMessages: vi.fn().mockResolvedValue([]),
  listChildIssues: vi.fn().mockResolvedValue({ issues: [] }),
  listIssues: vi.fn().mockResolvedValue({ issues: [], total: 0 }),
  uploadFile: vi.fn(),
  listIssueReactions: vi.fn().mockResolvedValue([]),
  addIssueReaction: vi.fn(),
  removeIssueReaction: vi.fn(),
  listAttachments: vi.fn().mockResolvedValue([]),
  listRuntimes: vi.fn().mockResolvedValue([]),
  listLocalPreviews: vi.fn().mockResolvedValue({ previews: [] }),
  getLocalPreviewStreamUrl: vi.fn((healthPort: number) => `http://127.0.0.1:${healthPort}/preview/stream`),
  stopLocalPreview: vi.fn(),
  getLocalPreviewLogs: vi.fn(),
  addCommentReaction: vi.fn(),
  removeCommentReaction: vi.fn(),
  listMembers: vi.fn().mockResolvedValue([{ user_id: "user-1", name: "Test User", email: "test@test.com", role: "admin" }]),
  listAgents: vi.fn().mockResolvedValue([]),
  getProject: vi.fn(),
  listProjects: vi.fn().mockResolvedValue({ projects: [] }),
}));

vi.mock("@multica/core/api", () => ({
  api: mockApiObj,
  getApi: () => mockApiObj,
  setApiInstance: vi.fn(),
}));

// Mock issue config
vi.mock("@multica/core/issues/config", () => ({
  ALL_STATUSES: ["backlog", "todo", "in_progress", "in_review", "done", "blocked", "cancelled"],
  BOARD_STATUSES: ["backlog", "todo", "in_progress", "in_review", "done", "blocked"],
  STATUS_ORDER: ["backlog", "todo", "in_progress", "in_review", "done", "blocked", "cancelled"],
  UNKNOWN_STATUS_CONFIG: { label: "Unknown", iconColor: "text-muted-foreground", hoverBg: "hover:bg-accent" },
  STATUS_CONFIG: {
    backlog: { label: "Backlog", iconColor: "text-muted-foreground", hoverBg: "hover:bg-accent" },
    todo: { label: "Todo", iconColor: "text-muted-foreground", hoverBg: "hover:bg-accent" },
    in_progress: { label: "In Progress", iconColor: "text-warning", hoverBg: "hover:bg-warning/10" },
    in_review: { label: "In Review", iconColor: "text-success", hoverBg: "hover:bg-success/10" },
    done: { label: "Done", iconColor: "text-info", hoverBg: "hover:bg-info/10" },
    blocked: { label: "Blocked", iconColor: "text-destructive", hoverBg: "hover:bg-destructive/10" },
    cancelled: { label: "Cancelled", iconColor: "text-muted-foreground", hoverBg: "hover:bg-accent" },
  },
  isIssueStatus: (status: string) =>
    ["backlog", "todo", "in_progress", "in_review", "done", "blocked", "cancelled"].includes(status),
  getStatusConfig: (status: string) => {
    const config = {
      backlog: { label: "Backlog", iconColor: "text-muted-foreground", hoverBg: "hover:bg-accent" },
      todo: { label: "Todo", iconColor: "text-muted-foreground", hoverBg: "hover:bg-accent" },
      in_progress: { label: "In Progress", iconColor: "text-warning", hoverBg: "hover:bg-warning/10" },
      in_review: { label: "In Review", iconColor: "text-success", hoverBg: "hover:bg-success/10" },
      done: { label: "Done", iconColor: "text-info", hoverBg: "hover:bg-info/10" },
      blocked: { label: "Blocked", iconColor: "text-destructive", hoverBg: "hover:bg-destructive/10" },
      cancelled: { label: "Cancelled", iconColor: "text-muted-foreground", hoverBg: "hover:bg-accent" },
    } as Record<string, { label: string; iconColor: string; hoverBg: string }>;
    return config[status] ?? { label: "Unknown", iconColor: "text-muted-foreground", hoverBg: "hover:bg-accent" };
  },
  PRIORITY_ORDER: ["urgent", "high", "medium", "low", "none"],
  PRIORITY_CONFIG: {
    urgent: { label: "Urgent", bars: 4, color: "text-destructive", badgeBg: "bg-destructive/10", badgeText: "text-destructive" },
    high: { label: "High", bars: 3, color: "text-warning", badgeBg: "bg-warning/10", badgeText: "text-warning" },
    medium: { label: "Medium", bars: 2, color: "text-warning", badgeBg: "bg-warning/10", badgeText: "text-warning" },
    low: { label: "Low", bars: 1, color: "text-info", badgeBg: "bg-info/10", badgeText: "text-info" },
    none: { label: "No priority", bars: 0, color: "text-muted-foreground", badgeBg: "bg-muted", badgeText: "text-muted-foreground" },
  },
}));

// Mock recent issues store
const mockRecordVisit = vi.fn();
vi.mock("@multica/core/issues/stores", () => ({
  useRecentIssuesStore: Object.assign(
    (selector?: any) => {
      const state = { byWorkspace: {}, recordVisit: mockRecordVisit, pruneWorkspaces: vi.fn() };
      return selector ? selector(state) : state;
    },
    {
      getState: () => ({
        byWorkspace: {},
        recordVisit: mockRecordVisit,
        pruneWorkspaces: vi.fn(),
      }),
    },
  ),
  selectRecentIssues: () => () => [],
  useCommentCollapseStore: (selector?: any) => {
    const state = {
      collapsedByIssue: {},
      isCollapsed: () => false,
      toggle: () => {},
    };
    return selector ? selector(state) : state;
  },
  useCommentDraftStore: Object.assign(
    (selector?: any) => {
      const state = {
        drafts: {} as Record<string, { content: string; updatedAt: number }>,
        getDraft: () => undefined,
        setDraft: () => {},
        clearDraft: () => {},
      };
      return selector ? selector(state) : state;
    },
    {
      getState: () => ({
        drafts: {} as Record<string, { content: string; updatedAt: number }>,
        getDraft: () => undefined,
        setDraft: () => {},
        clearDraft: () => {},
      }),
    },
  ),
}));

// Mock react-virtuoso: jsdom has no real layout, so the real Virtuoso would
// compute a 0-height viewport and render nothing. The mock renders every item
// inline so id="comment-..." nodes are always present in the DOM — this
// matches the production cold-path where `initialItemCount` force-mounts
// items[0..targetIdx], giving the native scrollIntoView a real target.
//
// scrollIntoViewSpy: we spy on Element.prototype.scrollIntoView (jsdom no-ops
// it by default) so tests can assert the deep-link effect dispatched a
// native scroll on the target node.
const scrollIntoViewSpy = vi.hoisted(() => vi.fn());

vi.mock("react-virtuoso", () => ({
  Virtuoso: forwardRef(function MockVirtuoso(
    { data, itemContent }: { data: unknown[]; itemContent: (i: number, item: unknown) => unknown },
    ref: any,
  ) {
    useImperativeHandle(ref, () => ({
      // Real Virtuoso ref methods are not exercised by tests in this file
      // since the cold-path uses native scrollIntoView on the DOM node.
      scrollIntoView: vi.fn(),
      scrollToIndex: vi.fn(),
    }));
    return (
      <div data-testid="virtuoso-mock">
        {data.map((item, i) => (
          <div key={i}>{itemContent(i, item) as React.ReactElement}</div>
        ))}
      </div>
    );
  }),
}));

// jsdom's HTMLElement.prototype.scrollIntoView is a no-op stub; replace it
// with a spy so the deep-link effect's call can be observed.
beforeEach(() => {
  scrollIntoViewSpy.mockClear();
  Object.defineProperty(HTMLElement.prototype, "scrollIntoView", {
    configurable: true,
    writable: true,
    value: scrollIntoViewSpy,
  });
});

// Mock modals
vi.mock("@multica/core/modals", () => ({
  useModalStore: Object.assign(
    () => ({ open: vi.fn() }),
    { getState: () => ({ open: vi.fn() }) },
  ),
}));

// Mock core/hooks/use-file-upload
vi.mock("@multica/core/hooks/use-file-upload", () => ({
  useFileUpload: () => ({
    uploadWithToast: vi.fn().mockResolvedValue(mockUploadResult.current),
  }),
}));

// Mock realtime
vi.mock("@multica/core/realtime", () => ({
  useWSEvent: vi.fn(),
  useWSReconnect: vi.fn(),
  useWS: () => ({ subscribe: vi.fn(() => () => {}), onReconnect: vi.fn(() => () => {}) }),
  WSProvider: ({ children }: { children: React.ReactNode }) => children,
  useRealtimeSync: () => {},
}));

// Mock sonner
vi.mock("sonner", () => ({
  toast: { error: vi.fn(), message: vi.fn(), success: vi.fn() },
}));

// Mock react-resizable-panels (used by @multica/ui/components/ui/resizable)
vi.mock("react-resizable-panels", () => ({
  Group: ({ children, ...props }: any) => <div data-testid="panel-group" {...props}>{children}</div>,
  Panel: ({ children, ...props }: any) => <div data-testid="panel" {...props}>{children}</div>,
  Separator: ({ children, ...props }: any) => <div data-testid="panel-handle" {...props}>{children}</div>,
  useDefaultLayout: () => ({ defaultLayout: undefined, onLayoutChanged: vi.fn() }),
  usePanelRef: () => ({ current: { isCollapsed: () => false, expand: vi.fn(), collapse: vi.fn() } }),
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
  start_date: null,
  due_date: "2026-06-01T00:00:00Z",
  metadata: {},
  created_at: "2026-01-15T00:00:00Z",
  updated_at: "2026-01-20T00:00:00Z",
  archived_at: null,
  archived_by: null,
};

const mockTimeline: TimelineEntry[] = [
  {
    type: "comment",
    id: "comment-1",
    actor_type: "member",
    actor_id: "user-1",
    content: "Started working on this",
    parent_id: null,
    created_at: "2026-01-16T00:00:00Z",
    updated_at: "2026-01-16T00:00:00Z",
    comment_type: "comment",
  },
  {
    type: "comment",
    id: "comment-2",
    actor_type: "agent",
    actor_id: "agent-1",
    content: "I can help with this",
    parent_id: null,
    created_at: "2026-01-17T00:00:00Z",
    updated_at: "2026-01-17T00:00:00Z",
    comment_type: "comment",
  },
];

// ---------------------------------------------------------------------------
// Import component under test (after mocks)
// ---------------------------------------------------------------------------

import { IssueDetail } from "./issue-detail";

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

if (typeof window !== "undefined" && !window.matchMedia) {
  Object.defineProperty(window, "matchMedia", {
    writable: true,
    value: (query: string) => ({
      matches: false,
      media: query,
      onchange: null,
      addListener: vi.fn(),
      removeListener: vi.fn(),
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      dispatchEvent: vi.fn(),
    }),
  });
}

if (typeof window !== "undefined" && !window.ResizeObserver) {
  class ResizeObserverMock {
    observe = vi.fn();
    unobserve = vi.fn();
    disconnect = vi.fn();
  }

  Object.defineProperty(window, "ResizeObserver", {
    writable: true,
    value: ResizeObserverMock,
  });
  Object.defineProperty(globalThis, "ResizeObserver", {
    writable: true,
    value: ResizeObserverMock,
  });
}

class EventSourceMock {
  static CONNECTING = 0;
  static OPEN = 1;
  static CLOSED = 2;

  readonly CONNECTING = 0;
  readonly OPEN = 1;
  readonly CLOSED = 2;
  readonly url: string | URL;
  readonly withCredentials = false;
  readyState = EventSourceMock.CONNECTING;
  onerror: ((this: EventSource, ev: Event) => any) | null = null;
  onmessage: ((this: EventSource, ev: MessageEvent) => any) | null = null;
  onopen: ((this: EventSource, ev: Event) => any) | null = null;
  private listeners = new Map<string, Set<EventListenerOrEventListenerObject>>();

  constructor(url: string | URL) {
    this.url = url;
    mockEventSources.push(this);
  }

  addEventListener(type: string, listener: EventListenerOrEventListenerObject) {
    const listeners = this.listeners.get(type) ?? new Set<EventListenerOrEventListenerObject>();
    listeners.add(listener);
    this.listeners.set(type, listeners);
  }

  removeEventListener(type: string, listener: EventListenerOrEventListenerObject) {
    this.listeners.get(type)?.delete(listener);
  }

  close() {
    this.readyState = EventSourceMock.CLOSED;
  }

  dispatch(type: string, data: unknown) {
    const event = new MessageEvent(type, { data: JSON.stringify(data) });
    for (const listener of this.listeners.get(type) ?? []) {
      if (typeof listener === "function") {
        listener.call(this as unknown as EventSource, event);
      } else {
        listener.handleEvent(event);
      }
    }
  }

  dispatchEvent(event: Event) {
    for (const listener of this.listeners.get(event.type) ?? []) {
      if (typeof listener === "function") {
        listener.call(this as unknown as EventSource, event);
      } else {
        listener.handleEvent(event);
      }
    }
    return true;
  }
}

Object.defineProperty(globalThis, "EventSource", {
  configurable: true,
  writable: true,
  value: EventSourceMock,
});

if (typeof window !== "undefined") {
  Object.defineProperty(window, "EventSource", {
    configurable: true,
    writable: true,
    value: EventSourceMock,
  });
}

function createTestQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: 0 },
      mutations: { retry: false },
    },
  });
}

function renderIssueDetail(
  issueId = "issue-1",
  props: Partial<ComponentProps<typeof IssueDetail>> = {},
  options: { locale?: SupportedLocale } = {},
) {
  const queryClient = createTestQueryClient();
  const result = render(
    <I18nProvider locale={options.locale ?? "en"} resources={TEST_RESOURCES}>
      <QueryClientProvider client={queryClient}>
        <IssueDetail issueId={issueId} {...props} />
      </QueryClientProvider>
    </I18nProvider>,
  );
  return { ...result, queryClient };
}

function renderIssueDetailWithHighlight(
  highlightCommentId: string,
  issueId = "issue-1",
  options: { seedTimeline?: boolean } = {},
) {
  const queryClient = createTestQueryClient();
  if (options.seedTimeline) {
    // Pre-populate the timeline cache so the first render sees timeline.length>0.
    // This reproduces the inbox-click race: timeline data is available before
    // the issue itself has finished loading, so the effect that scrolls to
    // the comment fires once with `loading=true` (skeleton still rendered,
    // no comment DOM) and must re-fire when `loading` flips to false.
    queryClient.setQueryData(["issues", "timeline", issueId], mockTimeline);
  }
  const result = render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <QueryClientProvider client={queryClient}>
        <IssueDetail issueId={issueId} highlightCommentId={highlightCommentId} />
      </QueryClientProvider>
    </I18nProvider>,
  );
  return { ...result, queryClient };
}

function selectTextNodeContent(element: HTMLElement) {
  const textNode = element.firstChild;
  if (!textNode) throw new Error("Expected element to have a text node");
  const range = document.createRange();
  range.selectNodeContents(textNode);
  Object.defineProperty(range, "getBoundingClientRect", {
    configurable: true,
    value: () => ({
      x: 100,
      y: 100,
      width: 120,
      height: 20,
      top: 100,
      right: 220,
      bottom: 120,
      left: 100,
      toJSON: () => {},
    }),
  });
  const selection = window.getSelection();
  selection?.removeAllRanges();
  selection?.addRange(range);
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe("IssueDetail (shared)", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockViewport.isMobile = false;
    mockNavigationSearchParams.current = new URLSearchParams();
    // Default: issue loads successfully
    mockApiObj.getIssue.mockResolvedValue(mockIssue);
    // /timeline returns the entries flat in chronological order (oldest first).
    mockApiObj.listTimeline.mockResolvedValue(mockTimeline);
    mockApiObj.listAttachments.mockResolvedValue([]);
    mockApiObj.listIssueReactions.mockResolvedValue([]);
    mockApiObj.listIssueSubscribers.mockResolvedValue([]);
    mockApiObj.listChildIssues.mockResolvedValue({ issues: [] });
    mockApiObj.listIssues.mockResolvedValue({ issues: [], total: 0 });
    mockApiObj.getActiveTasksForIssue.mockResolvedValue({ tasks: [] });
    mockApiObj.listTasksByIssue.mockResolvedValue([]);
    mockApiObj.listRuntimes.mockResolvedValue([]);
    mockApiObj.listLocalPreviews.mockResolvedValue({ previews: [] });
    mockApiObj.getLocalPreviewStreamUrl.mockImplementation((healthPort: number) => `http://127.0.0.1:${healthPort}/preview/stream`);
    mockApiObj.stopLocalPreview.mockResolvedValue(undefined);
    mockApiObj.getLocalPreviewLogs.mockResolvedValue({ id: "preview-1", log_path: "preview.log", logs: "" });
    mockApiObj.listMembers.mockResolvedValue([
      { user_id: "user-1", name: "Test User", email: "test@test.com", role: "admin" },
    ]);
    mockApiObj.listAgents.mockResolvedValue([]);
    mockEventSources.length = 0;
    window.getSelection()?.removeAllRanges();
    mockApiObj.getProject.mockReset();
    useActiveIssueContextStore.setState({ current: null });
  });

  it("shows loading skeleton while data is loading", () => {
    // Make the API hang to keep loading state
    mockApiObj.getIssue.mockReturnValue(new Promise(() => {}));
    renderIssueDetail();

    expect(
      screen.getAllByRole("generic").some((el) => el.getAttribute("data-slot") === "skeleton"),
    ).toBe(true);
  });

  it("renders issue title and description after loading", async () => {
    renderIssueDetail();

    await waitFor(() => {
      expect(screen.getByDisplayValue("Implement authentication")).toBeInTheDocument();
    });

    expect(screen.getByDisplayValue("Add JWT auth to the backend")).toBeInTheDocument();
  });

  it("renders referenced attachments inline while keeping the attachment area visible", async () => {
    mockApiObj.getIssue.mockResolvedValue({
      ...mockIssue,
      description: "Spec file: [report.pdf](https://cdn.example.test/report.pdf)",
    });
    mockApiObj.listAttachments.mockResolvedValue([
      {
        id: "att-1",
        workspace_id: "ws-1",
        issue_id: "issue-1",
        comment_id: null,
        chat_session_id: null,
        chat_message_id: null,
        uploader_type: "member",
        uploader_id: "user-1",
        filename: "report.pdf",
        url: "https://cdn.example.test/report.pdf",
        download_url: "https://cdn.example.test/report.pdf",
        content_type: "application/pdf",
        size_bytes: 123,
        created_at: "2026-01-20T00:00:00Z",
      },
    ]);

    renderIssueDetail();

    await waitFor(() => {
      expect(screen.getByTestId("issue-attachment-card")).toHaveTextContent("report.pdf");
    });
    expect(screen.getByTestId("rich-text-editor")).toHaveAttribute("data-hide-attachments", "false");
  });

  it("uploads description files into the attachment area and binds them immediately", async () => {
    const { container } = renderIssueDetail();

    await waitFor(() => {
      expect(screen.getByDisplayValue("Add JWT auth to the backend")).toBeInTheDocument();
    });

    const file = new File(["%PDF-1.4"], "upload.pdf", { type: "application/pdf" });
    const input = container.querySelector('input[type="file"]') as HTMLInputElement;
    expect(input).not.toBeNull();
    fireEvent.change(input, { target: { files: [file] } });

    await waitFor(() => {
      expect(screen.getByTestId("issue-attachment-card")).toHaveTextContent("upload.pdf");
    });
    await waitFor(() => {
      expect(mockApiObj.updateIssue).toHaveBeenCalledWith(
        "issue-1",
        {
          attachment_ids: ["upload-1"],
        },
      );
    });
  });

  it("sends empty description baseline in update payload", async () => {
    mockApiObj.getIssue.mockResolvedValue({
      ...mockIssue,
      description: "",
    });

    renderIssueDetail();

    const editor = await screen.findByTestId("rich-text-editor");
    fireEvent.change(editor, { target: { value: "new description" } });

    await waitFor(() => {
      expect(mockApiObj.updateIssue).toHaveBeenCalledWith(
        "issue-1",
        expect.objectContaining({
          description: "new description",
          description_base_updated_at: mockIssue.updated_at,
          description_base_value: "",
        }),
      );
    });
  });

  it("keeps the previous description baseline when a dirty editor rejects a remote description", async () => {
    const oldIssue = {
      ...mockIssue,
      description: "old description",
      updated_at: "2026-01-20T00:00:00Z",
    };
    const remoteIssue = {
      ...oldIssue,
      description: "remote description",
      updated_at: "2026-01-20T00:01:00Z",
    };
    mockApiObj.getIssue.mockResolvedValue(oldIssue);

    const { queryClient } = renderIssueDetail();

    const editor = await screen.findByTestId("rich-text-editor");
    await waitFor(() => {
      expect(editor).toHaveValue("old description");
    });

    fireEvent.change(editor, { target: { value: "local draft" } });

    await waitFor(() => {
      expect(mockApiObj.updateIssue).toHaveBeenLastCalledWith(
        "issue-1",
        expect.objectContaining({
          description: "local draft",
          description_base_updated_at: oldIssue.updated_at,
          description_base_value: "old description",
        }),
      );
    });

    mockApiObj.updateIssue.mockClear();
    queryClient.setQueryData(["issues", "ws-1", "detail", "issue-1"], remoteIssue);

    await waitFor(() => {
      expect(editor).toHaveValue("local draft");
    });

    fireEvent.change(editor, { target: { value: "local draft after remote" } });

    await waitFor(() => {
      expect(mockApiObj.updateIssue).toHaveBeenLastCalledWith(
        "issue-1",
        expect.objectContaining({
          description: "local draft after remote",
          description_base_updated_at: oldIssue.updated_at,
          description_base_value: "old description",
        }),
      );
    });
  });

  it("renders issue identifier in the breadcrumb", async () => {
    renderIssueDetail();

    await waitFor(() => {
      expect(screen.getByText("TES-1")).toBeInTheDocument();
    });
  });

  it("canonicalizes legacy issue-id routes to identifier routes", async () => {
    renderIssueDetail();

    await waitFor(() => {
      expect(mockNavigationReplace).toHaveBeenCalledWith("/test/issues/TES-1");
    });
  });

  it("renders workspace name as breadcrumb link", async () => {
    renderIssueDetail();

    const workspaceLink = await screen.findByText("Test WS");
    expect(workspaceLink.closest("a")).toHaveAttribute("href", "/test");
  });

  it("renders the issue title leaf as a link to the issue detail page", async () => {
    renderIssueDetail();

    // The breadcrumb leaf is the whole "identifier + title" string wrapped in a
    // single link to the issue's own detail route (used to open the full page
    // from the inline Inbox pane). A bare issue has no ancestor crumbs.
    const leaf = await screen.findByText("TES-1 Implement authentication");
    expect(leaf.closest("a")).toHaveAttribute("href", "/test/issues/issue-1");
  });

  it("omits the project breadcrumb segment when the issue has no project_id", async () => {
    // Default fixture has project_id: null.
    renderIssueDetail();

    // Leaf renders once loaded; a bare issue has no ancestor crumbs at all.
    await screen.findByText("TES-1 Implement authentication");

    // Project is never fetched and no project crumb appears.
    expect(mockApiObj.getProject).not.toHaveBeenCalled();
    expect(screen.queryByText("Marketing site refresh")).not.toBeInTheDocument();
  });

  it("renders the project breadcrumb segment when the issue belongs to a project", async () => {
    mockApiObj.getIssue.mockResolvedValue({ ...mockIssue, project_id: "p-1" });
    mockApiObj.getProject.mockResolvedValue({
      id: "p-1",
      workspace_id: "ws-1",
      title: "Marketing site refresh",
      description: null,
      icon: "🚀",
      status: "in_progress",
      priority: "none",
      lead_type: null,
      lead_id: null,
      created_at: "2026-01-01T00:00:00Z",
      updated_at: "2026-01-01T00:00:00Z",
      issue_count: 0,
      done_count: 0,
      resource_count: 0,
    });

    renderIssueDetail();

    const projectLink = await screen.findByText("Marketing site refresh");
    // The whole project segment is a single AppLink pointing at the project
    // detail route under the active workspace slug.
    expect(projectLink.closest("a")).toHaveAttribute("href", "/test/projects/p-1");
  });

  it("publishes and clears active issue context for embedded creation entrypoints", async () => {
    mockApiObj.getIssue.mockResolvedValue({ ...mockIssue, project_id: "p-1" });

    const view = renderIssueDetail();

    await waitFor(() => {
      expect(useActiveIssueContextStore.getState().current).toEqual({
        issueId: "issue-1",
        identifier: "TES-1",
        projectId: "p-1",
      });
    });

    view.unmount();

    expect(useActiveIssueContextStore.getState().current).toBeNull();
  });

  it("shows an Unknown project placeholder when the project query fails", async () => {
    mockApiObj.getIssue.mockResolvedValue({ ...mockIssue, project_id: "p-missing" });
    mockApiObj.getProject.mockRejectedValue(new Error("not found"));

    renderIssueDetail();

    await waitFor(() => {
      expect(screen.getByText("Unknown project")).toBeInTheDocument();
    });
    // Placeholder is non-interactive — no link wraps the text.
    const placeholder = screen.getByText("Unknown project");
    expect(placeholder.closest("a")).toBeNull();
  });

  it("renders properties sidebar with all core rows plus set optional rows", async () => {
    renderIssueDetail();

    await waitFor(() => {
      expect(screen.getAllByText("Properties").length).toBeGreaterThanOrEqual(1);
    });

    // Core rows — always rendered regardless of whether the issue has a value.
    expect(screen.getByText("Status")).toBeInTheDocument();
    expect(screen.getByText("Assignee")).toBeInTheDocument();
    // "Project" appears twice (row label + picker stub), so disambiguate by id.
    expect(screen.getByTestId("project-picker")).toBeInTheDocument();
    // priority="high" + due_date are set in the fixture, so both optional rows show.
    expect(screen.getByText("Priority")).toBeInTheDocument();
    expect(screen.getByText("Due date")).toBeInTheDocument();
    // No labels are attached in the fixture — the Labels optional row
    // must stay hidden by default.
    expect(screen.queryByText("Labels")).not.toBeInTheDocument();
    // Parent issue lives in its own section and only renders when the
    // issue actually has a parent — the fixture has none.
    expect(screen.queryByText("Parent issue")).not.toBeInTheDocument();
    // The "+ Add property" affordance is always offered while any
    // optional field is still hidden.
    expect(screen.getByText("Add property")).toBeInTheDocument();
  });

  it("renders issue detail when the API returns an unknown status", async () => {
    mockApiObj.getIssue.mockResolvedValue({
      ...mockIssue,
      status: "triage",
    } as unknown as Issue);

    renderIssueDetail();

    await waitFor(() => {
      expect(screen.getByDisplayValue("Implement authentication")).toBeInTheDocument();
    });

    expect(screen.getByText("triage")).toBeInTheDocument();
  });

  it("opens the shared assignee picker from the issue-detail overflow menu", async () => {
    renderIssueDetail();

    await waitFor(() => {
      expect(screen.getAllByText("TES-1").length).toBeGreaterThan(0);
    });

    fireEvent.click(screen.getByLabelText("More"));
    fireEvent.click((await screen.findAllByText("Assignee"))[1]!);

    expect(await screen.findByPlaceholderText("Assign to...")).toBeInTheDocument();
    expect(await screen.findByText("Members")).toBeInTheDocument();
    expect((await screen.findAllByText("Test User")).length).toBeGreaterThan(0);
  });

  it("hides every optional property row when none are set", async () => {
    // Override the default fixture: nothing optional set.
    mockApiObj.getIssue.mockResolvedValue({
      ...mockIssue,
      priority: "none",
      start_date: null,
      due_date: null,
    });

    renderIssueDetail();

    await waitFor(() => {
      expect(screen.getAllByText("Properties").length).toBeGreaterThanOrEqual(1);
    });

    expect(screen.queryByText("Priority")).not.toBeInTheDocument();
    expect(screen.queryByText("Due date")).not.toBeInTheDocument();
    expect(screen.queryByText("Labels")).not.toBeInTheDocument();
    // Project stays as a core row regardless of value.
    expect(screen.getByTestId("project-picker")).toBeInTheDocument();
    // No parent → no standalone Parent issue section either.
    expect(screen.queryByText("Parent issue")).not.toBeInTheDocument();
    expect(screen.getByText("Add property")).toBeInTheDocument();
  });

  it("uses a wider default max sidebar size on the standalone issue page", async () => {
    renderIssueDetail();

    await waitFor(() => {
      expect(screen.getByDisplayValue("Implement authentication")).toBeInTheDocument();
    });

    const panels = screen.getAllByTestId("panel");
    expect(panels[1]).toHaveAttribute("maxsize", "560");
  });

  it("keeps custom sidebar max size overrides for embedded layouts", async () => {
    renderIssueDetail("issue-1", { sidebarMaxSize: 420 });

    await waitFor(() => {
      expect(screen.getByDisplayValue("Implement authentication")).toBeInTheDocument();
    });

    const panels = screen.getAllByTestId("panel");
    expect(panels[1]).toHaveAttribute("maxsize", "420");
  });

  it("uses a non-resizable layout with the sidebar sheet closed by default on mobile", async () => {
    mockViewport.isMobile = true;

    renderIssueDetail();

    await waitFor(() => {
      expect(screen.getByDisplayValue("Implement authentication")).toBeInTheDocument();
    });

    expect(screen.queryByTestId("panel-group")).not.toBeInTheDocument();
    expect(screen.queryByText("Properties")).not.toBeInTheDocument();
  });

  it("hides metadata content from the sidebar and shows a button when the bag has keys", async () => {
    // Metadata is agent-facing; the sidebar only exposes a button that opens
    // the raw JSON on demand. Keys are NOT rendered inline anywhere.
    mockApiObj.getIssue.mockResolvedValue({
      ...mockIssue,
      metadata: {
        pr_url: "https://example.com/pr/1",
        pipeline_status: "running",
      },
    });

    renderIssueDetail();

    fireEvent.click(await screen.findByRole("tab", { name: "Properties" }));

    await waitFor(() => {
      // Trigger label includes a "· N" count so users can see payload size
      // before clicking — accept any count via regex.
      expect(screen.getByRole("button", { name: /^Metadata\b/ })).toBeInTheDocument();
    });

    // Key names are not rendered in the sidebar prior to opening the dialog.
    expect(screen.queryByText("pr_url")).not.toBeInTheDocument();
    expect(screen.queryByText("pipeline_status")).not.toBeInTheDocument();
  });

  it("opens a dialog with formatted JSON when the Metadata button is clicked", async () => {
    mockApiObj.getIssue.mockResolvedValue({
      ...mockIssue,
      metadata: {
        pr_url: "https://example.com/pr/1",
        pipeline_status: "running",
      },
    });

    renderIssueDetail();

    fireEvent.click(await screen.findByRole("tab", { name: "Properties" }));
    const button = await screen.findByRole("button", { name: /^Metadata\b/ });
    fireEvent.click(button);

    // The dialog renders a <pre> containing the formatted JSON; checking the
    // exact serialized payload also verifies the indent / structure.
    const expected = JSON.stringify(
      { pr_url: "https://example.com/pr/1", pipeline_status: "running" },
      null,
      2,
    );
    await waitFor(() => {
      const pre = document.querySelector("pre");
      expect(pre).not.toBeNull();
      expect(pre!.textContent).toBe(expected);
    });
  });

  it("hides the Metadata button entirely when the bag is empty", async () => {
    // Default fixture already has metadata: {}, asserted explicitly here.
    renderIssueDetail();

    await waitFor(() => {
      expect(screen.getByText("Details")).toBeInTheDocument();
    });

    expect(screen.queryByRole("button", { name: /^Metadata\b/ })).not.toBeInTheDocument();
  });

  it("renders Details section with Created by and dates", async () => {
    renderIssueDetail();

    await waitFor(() => {
      expect(screen.getByText("Details")).toBeInTheDocument();
    });

    expect(screen.getByText("Created by")).toBeInTheDocument();
    expect(screen.getByText("Created")).toBeInTheDocument();
    expect(screen.getByText("Updated")).toBeInTheDocument();
  });

  it("renders local previews from the daemon preview stream", async () => {
    mockApiObj.listRuntimes.mockResolvedValue([
      {
        id: "runtime-1",
        workspace_id: "ws-1",
        runtime_mode: "local",
        status: "online",
        metadata: { health_port: 20038 },
      },
    ]);
    mockApiObj.listLocalPreviews.mockResolvedValue({ previews: [] });

    renderIssueDetail();

    await waitFor(() => {
      expect(mockEventSources).toHaveLength(1);
    });

    mockEventSources[0]!.dispatch("ready", {
      previews: [
        {
          id: "preview-1",
          workspace_id: "ws-1",
          issue_id: "issue-1",
          visibility: "private",
          cwd: "/tmp/app",
          command: ["npm", "run", "dev"],
          pid: 123,
          port: 5173,
          url: "http://127.0.0.1:5173/",
          health_url: "http://127.0.0.1:5173/",
          log_path: "/tmp/preview.log",
          status: "running",
          started_at: "2026-05-25T00:00:00Z",
        },
      ],
    });

    expect(await screen.findByText("Local Preview")).toBeInTheDocument();
    expect(await screen.findByText("running · :5173")).toBeInTheDocument();
    expect(mockApiObj.getLocalPreviewStreamUrl).toHaveBeenCalledWith(20038, {
      workspace_id: "ws-1",
      issue_id: "issue-1",
    });
  });

  it("shows 'not found' message when issue does not exist", async () => {
    mockApiObj.getIssue.mockRejectedValue(new Error("Not found"));

    renderIssueDetail("nonexistent-id");

    await waitFor(() => {
      expect(
        screen.getByText("This issue does not exist or has been deleted in this workspace."),
      ).toBeInTheDocument();
    });
  });

  it("shows 'Back to Issues' button when issue is not found and no onDelete prop", async () => {
    mockApiObj.getIssue.mockRejectedValue(new Error("Not found"));

    renderIssueDetail("nonexistent-id");

    await waitFor(() => {
      expect(screen.getByText("Back to Issues")).toBeInTheDocument();
    });
  });

  it("renders Activity section header", async () => {
    renderIssueDetail();

    await waitFor(() => {
      expect(screen.getAllByText("Activity").length).toBeGreaterThanOrEqual(1);
    });
  });

  it("renders comments from timeline", async () => {
    renderIssueDetail();

    await waitFor(() => {
      expect(screen.getByText("Started working on this")).toBeInTheDocument();
    });

    expect(screen.getByText("I can help with this")).toBeInTheDocument();
  });

  it("localizes the quote selection menu from the active locale", async () => {
    renderIssueDetail("issue-1", {}, { locale: "zh-Hans" });

    const comment = await screen.findByText("Started working on this");
    selectTextNodeContent(comment);
    fireEvent.mouseUp(comment);

    await waitFor(() => {
      expect(screen.getByRole("button", { name: /放到新评论/ })).toBeInTheDocument();
      expect(screen.getByRole("button", { name: /放到回复/ })).toBeInTheDocument();
    });

    expect(screen.queryByText("Add to new comment")).not.toBeInTheDocument();
    expect(screen.queryByText("Add to reply")).not.toBeInTheDocument();
  });

  it("collapses non-trailing activity blocks and expands the last one by default", async () => {
    // Timeline shape:
    //   [activities: status_changed, priority_changed] ← block A (older)
    //   [comment-1]
    //   [activities: due_date_changed]                  ← block B (latest)
    // Block A should be collapsed; block B should be expanded.
    mockApiObj.listTimeline.mockResolvedValue([
      {
        type: "activity",
        id: "act-1",
        actor_type: "member",
        actor_id: "user-1",
        action: "status_changed",
        details: { from: "todo", to: "in_progress" },
        created_at: "2026-01-16T00:00:00Z",
      },
      {
        type: "activity",
        id: "act-2",
        actor_type: "member",
        actor_id: "user-1",
        action: "priority_changed",
        details: { from: "low", to: "high" },
        created_at: "2026-01-16T01:00:00Z",
      },
      {
        type: "comment",
        id: "comment-1",
        actor_type: "member",
        actor_id: "user-1",
        content: "Talking it through",
        parent_id: null,
        created_at: "2026-01-17T00:00:00Z",
        updated_at: "2026-01-17T00:00:00Z",
        comment_type: "comment",
      },
      {
        type: "activity",
        id: "act-3",
        actor_type: "member",
        actor_id: "user-1",
        action: "due_date_changed",
        details: { to: "2026-02-01T00:00:00Z" },
        created_at: "2026-01-18T00:00:00Z",
      },
    ] as TimelineEntry[]);

    renderIssueDetail();

    // Latest block (single activity) is expanded — its rendered text is visible.
    await waitFor(() => {
      expect(screen.getByText(/set due date to/i)).toBeInTheDocument();
    });

    // Older block is collapsed: shows the summary, hides the individual entries.
    expect(screen.getByText("2 activities")).toBeInTheDocument();
    expect(screen.queryByText(/changed status/i)).not.toBeInTheDocument();
    expect(screen.queryByText(/changed priority/i)).not.toBeInTheDocument();

    // Clicking the summary expands the older block.
    fireEvent.click(screen.getByText("2 activities"));
    await waitFor(() => {
      expect(screen.getByText(/changed status/i)).toBeInTheDocument();
    });
    expect(screen.getByText(/changed priority/i)).toBeInTheDocument();
  });

  it("truncates the trailing activity block to the most recent 8 entries with a show-more toggle", async () => {
    // 10 activities, all in the trailing block (no comment after them, so it's
    // the trailing block by definition). Alternating action types so the
    // 2-minute coalesce window never merges consecutive entries — we end up
    // with 10 distinct rows.
    const trailingBlock: TimelineEntry[] = [
      { type: "activity", id: "act-1", actor_type: "member", actor_id: "user-1", action: "status_changed", details: { from: "todo", to: "in_progress" }, created_at: "2026-01-18T00:00:00Z" },
      { type: "activity", id: "act-2", actor_type: "member", actor_id: "user-1", action: "priority_changed", details: { from: "low", to: "medium" }, created_at: "2026-01-18T00:01:00Z" },
      { type: "activity", id: "act-3", actor_type: "member", actor_id: "user-1", action: "status_changed", details: { from: "in_progress", to: "in_review" }, created_at: "2026-01-18T00:02:00Z" },
      { type: "activity", id: "act-4", actor_type: "member", actor_id: "user-1", action: "priority_changed", details: { from: "medium", to: "high" }, created_at: "2026-01-18T00:03:00Z" },
      { type: "activity", id: "act-5", actor_type: "member", actor_id: "user-1", action: "status_changed", details: { from: "in_review", to: "done" }, created_at: "2026-01-18T00:04:00Z" },
      { type: "activity", id: "act-6", actor_type: "member", actor_id: "user-1", action: "priority_changed", details: { from: "high", to: "urgent" }, created_at: "2026-01-18T00:05:00Z" },
      { type: "activity", id: "act-7", actor_type: "member", actor_id: "user-1", action: "status_changed", details: { from: "done", to: "blocked" }, created_at: "2026-01-18T00:06:00Z" },
      { type: "activity", id: "act-8", actor_type: "member", actor_id: "user-1", action: "priority_changed", details: { from: "urgent", to: "low" }, created_at: "2026-01-18T00:07:00Z" },
      { type: "activity", id: "act-9", actor_type: "member", actor_id: "user-1", action: "status_changed", details: { from: "blocked", to: "todo" }, created_at: "2026-01-18T00:08:00Z" },
      { type: "activity", id: "act-10", actor_type: "member", actor_id: "user-1", action: "due_date_changed", details: { to: "2026-02-01T00:00:00Z" }, created_at: "2026-01-18T00:09:00Z" },
    ] as TimelineEntry[];
    mockApiObj.listTimeline.mockResolvedValue(trailingBlock);

    renderIssueDetail();

    // In the truncated default state the "N activities" collapse header
    // stays hidden — the "Show N more" link is the only control we want
    // to expose for a glance at recent activity.
    await waitFor(() => {
      expect(screen.getByText("Show 2 more activities")).toBeInTheDocument();
    });
    expect(screen.queryByText("10 activities")).not.toBeInTheDocument();

    // Only the 8 most recent entries (act-3..act-10) are rendered by default.
    // act-1 and act-2 are folded behind the show-more line.
    expect(screen.getByText(/from In Progress to In Review/i)).toBeInTheDocument(); // act-3
    expect(screen.getByText(/set due date to/i)).toBeInTheDocument(); // act-10
    expect(screen.queryByText(/from Todo to In Progress/i)).not.toBeInTheDocument(); // act-1
    expect(screen.queryByText(/from Low to Medium/i)).not.toBeInTheDocument(); // act-2

    // Clicking the toggle reveals the older entries in place and brings the
    // full "N activities" header back (so the user can fold the block).
    fireEvent.click(screen.getByText("Show 2 more activities"));
    await waitFor(() => {
      expect(screen.getByText(/from Todo to In Progress/i)).toBeInTheDocument();
    });
    expect(screen.getByText(/from Low to Medium/i)).toBeInTheDocument();
    expect(screen.getByText(/set due date to/i)).toBeInTheDocument();
    expect(screen.getByText("10 activities")).toBeInTheDocument();
    expect(screen.queryByText(/Show \d+ more activit/i)).not.toBeInTheDocument();
  });

  it("does not show the show-more toggle when the trailing block has 8 or fewer entries", async () => {
    const trailingBlock: TimelineEntry[] = [
      { type: "activity", id: "act-1", actor_type: "member", actor_id: "user-1", action: "status_changed", details: { from: "todo", to: "in_progress" }, created_at: "2026-01-18T00:00:00Z" },
      { type: "activity", id: "act-2", actor_type: "member", actor_id: "user-1", action: "priority_changed", details: { from: "low", to: "high" }, created_at: "2026-01-18T00:01:00Z" },
      { type: "activity", id: "act-3", actor_type: "member", actor_id: "user-1", action: "status_changed", details: { from: "in_progress", to: "in_review" }, created_at: "2026-01-18T00:02:00Z" },
      { type: "activity", id: "act-4", actor_type: "member", actor_id: "user-1", action: "priority_changed", details: { from: "high", to: "urgent" }, created_at: "2026-01-18T00:03:00Z" },
      { type: "activity", id: "act-5", actor_type: "member", actor_id: "user-1", action: "status_changed", details: { from: "in_review", to: "done" }, created_at: "2026-01-18T00:04:00Z" },
      { type: "activity", id: "act-6", actor_type: "member", actor_id: "user-1", action: "priority_changed", details: { from: "urgent", to: "low" }, created_at: "2026-01-18T00:05:00Z" },
      { type: "activity", id: "act-7", actor_type: "member", actor_id: "user-1", action: "status_changed", details: { from: "done", to: "blocked" }, created_at: "2026-01-18T00:06:00Z" },
      { type: "activity", id: "act-8", actor_type: "member", actor_id: "user-1", action: "due_date_changed", details: { to: "2026-02-01T00:00:00Z" }, created_at: "2026-01-18T00:07:00Z" },
    ] as TimelineEntry[];
    mockApiObj.listTimeline.mockResolvedValue(trailingBlock);

    renderIssueDetail();

    await waitFor(() => {
      expect(screen.getByText("8 activities")).toBeInTheDocument();
    });
    // Every one of the 8 entries should be visible — the trailing block fits
    // exactly within the limit, so no "Show N more activities" line appears.
    expect(screen.getByText(/from Todo to In Progress/i)).toBeInTheDocument();
    expect(screen.getByText(/from Low to High/i)).toBeInTheDocument();
    expect(screen.getByText(/from In Progress to In Review/i)).toBeInTheDocument();
    expect(screen.getByText(/from High to Urgent/i)).toBeInTheDocument();
    expect(screen.getByText(/from In Review to Done/i)).toBeInTheDocument();
    expect(screen.getByText(/from Urgent to Low/i)).toBeInTheDocument();
    expect(screen.getByText(/from Done to Blocked/i)).toBeInTheDocument();
    expect(screen.getByText(/set due date to/i)).toBeInTheDocument();
    expect(screen.queryByText(/Show \d+ more activit/i)).not.toBeInTheDocument();
  });

  it("expanding a non-trailing block shows every entry — only the trailing block truncates older ones", async () => {
    // Non-trailing block (10 activities) + comment + trailing block (1 activity).
    // Manually expanding the older block must reveal all 10 entries — the
    // truncate-to-8 rule applies only to the trailing block.
    const timeline: TimelineEntry[] = [
      { type: "activity", id: "old-1", actor_type: "member", actor_id: "user-1", action: "status_changed", details: { from: "backlog", to: "todo" }, created_at: "2026-01-16T00:00:00Z" },
      { type: "activity", id: "old-2", actor_type: "member", actor_id: "user-1", action: "priority_changed", details: { from: "none", to: "low" }, created_at: "2026-01-16T00:01:00Z" },
      { type: "activity", id: "old-3", actor_type: "member", actor_id: "user-1", action: "status_changed", details: { from: "todo", to: "in_progress" }, created_at: "2026-01-16T00:02:00Z" },
      { type: "activity", id: "old-4", actor_type: "member", actor_id: "user-1", action: "priority_changed", details: { from: "low", to: "medium" }, created_at: "2026-01-16T00:03:00Z" },
      { type: "activity", id: "old-5", actor_type: "member", actor_id: "user-1", action: "status_changed", details: { from: "in_progress", to: "in_review" }, created_at: "2026-01-16T00:04:00Z" },
      { type: "activity", id: "old-6", actor_type: "member", actor_id: "user-1", action: "priority_changed", details: { from: "medium", to: "high" }, created_at: "2026-01-16T00:05:00Z" },
      { type: "activity", id: "old-7", actor_type: "member", actor_id: "user-1", action: "status_changed", details: { from: "in_review", to: "done" }, created_at: "2026-01-16T00:06:00Z" },
      { type: "activity", id: "old-8", actor_type: "member", actor_id: "user-1", action: "priority_changed", details: { from: "high", to: "urgent" }, created_at: "2026-01-16T00:07:00Z" },
      { type: "activity", id: "old-9", actor_type: "member", actor_id: "user-1", action: "status_changed", details: { from: "done", to: "blocked" }, created_at: "2026-01-16T00:08:00Z" },
      { type: "activity", id: "old-10", actor_type: "member", actor_id: "user-1", action: "priority_changed", details: { from: "urgent", to: "low" }, created_at: "2026-01-16T00:09:00Z" },
      {
        type: "comment", id: "comment-mid", actor_type: "member", actor_id: "user-1",
        content: "Splitting the blocks", parent_id: null,
        created_at: "2026-01-17T00:00:00Z", updated_at: "2026-01-17T00:00:00Z",
        comment_type: "comment",
      },
      { type: "activity", id: "last-1", actor_type: "member", actor_id: "user-1", action: "due_date_changed", details: { to: "2026-02-01T00:00:00Z" }, created_at: "2026-01-18T00:00:00Z" },
    ] as TimelineEntry[];
    mockApiObj.listTimeline.mockResolvedValue(timeline);

    renderIssueDetail();

    // The older block defaults to collapsed; its summary reports 10.
    await waitFor(() => {
      expect(screen.getByText("10 activities")).toBeInTheDocument();
    });
    // None of the older entries are rendered before expansion.
    expect(screen.queryByText(/from Backlog to Todo/i)).not.toBeInTheDocument();

    // Expand the older block by clicking its summary line.
    fireEvent.click(screen.getByText("10 activities"));

    // Every one of the 10 entries should now be visible — even though the
    // block has more than 8 entries, the truncate-to-8 rule does not apply
    // to non-trailing blocks, so no "Show N more activities" line appears.
    await waitFor(() => {
      expect(screen.getByText(/from Backlog to Todo/i)).toBeInTheDocument();
    });
    expect(screen.getByText(/from No priority to Low/i)).toBeInTheDocument();
    expect(screen.getByText(/from Todo to In Progress/i)).toBeInTheDocument();
    expect(screen.getByText(/from Low to Medium/i)).toBeInTheDocument();
    expect(screen.getByText(/from In Progress to In Review/i)).toBeInTheDocument();
    expect(screen.getByText(/from Medium to High/i)).toBeInTheDocument();
    expect(screen.getByText(/from In Review to Done/i)).toBeInTheDocument();
    expect(screen.getByText(/from High to Urgent/i)).toBeInTheDocument();
    expect(screen.getByText(/from Done to Blocked/i)).toBeInTheDocument();
    expect(screen.getByText(/from Urgent to Low/i)).toBeInTheDocument();
    expect(screen.queryByText(/Show \d+ more activit/i)).not.toBeInTheDocument();
  });

  describe("highlightCommentId scroll-to-comment", () => {
    it("scrolls to the comment from URL search params", async () => {
      mockNavigationSearchParams.current = new URLSearchParams({
        comment: "comment-2",
      });

      renderIssueDetail();

      await waitFor(() => {
        expect(mockApiObj.listTimeline).toHaveBeenCalledWith(
          "issue-1",
          { mode: "around", id: "comment-2" },
        );
      });
      await waitFor(() => {
        expect(
          document.getElementById("comment-comment-2"),
        ).not.toBeNull();
      });
      await waitFor(() => {
        expect(scrollIntoViewSpy).toHaveBeenCalledWith(
          expect.objectContaining({ block: "center" }),
        );
      });
    });

    it("scrolls to the highlighted comment after both issue and timeline finish loading", async () => {
      renderIssueDetailWithHighlight("comment-2");

      // Wait for the comment row to mount. With initialItemCount in
      // production, items[0..targetIdx] are force-mounted on first commit;
      // the mock unconditionally inline-renders every item, so this just
      // waits for the regular render pass.
      await waitFor(() => {
        expect(
          document.getElementById("comment-comment-2"),
        ).not.toBeNull();
      });

      // The deep-link useLayoutEffect calls native scrollIntoView on the
      // target node ({block: 'center'}).
      await waitFor(() => {
        expect(scrollIntoViewSpy).toHaveBeenCalled();
      });
      expect(scrollIntoViewSpy).toHaveBeenCalledWith(
        expect.objectContaining({ block: "center" }),
      );
    });

    it("still scrolls when the timeline is ready before the issue (regression for inbox click)", async () => {
      // Reproduces the inbox-click race: timeline data is in the cache
      // before the issue resolves. While loading is true, IssueDetail
      // renders the loading skeleton (Virtuoso never mounts), so no
      // scroll can fire. After the issue resolves, Virtuoso mounts and
      // the useLayoutEffect dispatches the native scroll.
      let resolveIssue: (value: Issue) => void = () => {};
      const issuePromise = new Promise<Issue>((resolve) => {
        resolveIssue = resolve;
      });
      mockApiObj.getIssue.mockReturnValue(issuePromise);

      renderIssueDetailWithHighlight("comment-2", "issue-1", { seedTimeline: true });

      expect(
        document.getElementById("comment-comment-2"),
      ).toBeNull();
      expect(scrollIntoViewSpy).not.toHaveBeenCalled();

      resolveIssue(mockIssue);

      await waitFor(() => {
        expect(
          document.getElementById("comment-comment-2"),
        ).not.toBeNull();
      });
      await waitFor(() => {
        expect(scrollIntoViewSpy).toHaveBeenCalledWith(
          expect.objectContaining({ block: "center" }),
        );
      });
    });

    it("auto-expands a folded resolved thread when deep-link target is a reply inside it", async () => {
      // Seed a timeline where comment-3 is resolved (so it renders as a
      // resolved-bar by default) and has a reply, reply-1, whose id is the
      // deep-link target. The reply is not in the flat items array — only
      // the resolved-bar root is. The effect must detect this, expand the
      // thread, then on re-run scroll to the reply's id="comment-reply-1" node.
      const timelineWithResolvedThread: TimelineEntry[] = [
        ...mockTimeline,
        {
          type: "comment",
          id: "comment-3",
          actor_type: "member",
          actor_id: "user-1",
          content: "Resolved root",
          parent_id: null,
          created_at: "2026-01-18T00:00:00Z",
          updated_at: "2026-01-18T00:00:00Z",
          comment_type: "comment",
          resolved_at: "2026-01-19T00:00:00Z",
        } as TimelineEntry,
        {
          type: "comment",
          id: "reply-1",
          actor_type: "member",
          actor_id: "user-1",
          content: "Reply inside resolved thread",
          parent_id: "comment-3",
          created_at: "2026-01-18T01:00:00Z",
          updated_at: "2026-01-18T01:00:00Z",
          comment_type: "comment",
        } as TimelineEntry,
      ];
      mockApiObj.listTimeline.mockResolvedValue(timelineWithResolvedThread);

      const queryClient = createTestQueryClient();
      render(
        <I18nProvider locale="en" resources={TEST_RESOURCES}>
          <QueryClientProvider client={queryClient}>
            <IssueDetail issueId="issue-1" highlightCommentId="reply-1" />
          </QueryClientProvider>
        </I18nProvider>,
      );

      // After expansion, the reply must appear in the DOM (inside the now
      // -unfolded CommentCard) and the deep-link effect must scroll to it.
      await waitFor(() => {
        expect(
          document.getElementById("comment-reply-1"),
        ).not.toBeNull();
      });
      await waitFor(() => {
        expect(scrollIntoViewSpy).toHaveBeenCalledWith(
          expect.objectContaining({ block: "center" }),
        );
      });
    });
  });

  it("sends empty description when editor is cleared", async () => {
    renderIssueDetail();

    await waitFor(() => {
      expect(screen.getByDisplayValue("Add JWT auth to the backend")).toBeInTheDocument();
    });

    const editor = screen.getByPlaceholderText("Add description...");
    fireEvent.change(editor, { target: { value: "" } });

    await waitFor(() => {
      expect(mockApiObj.updateIssue).toHaveBeenCalledWith(
        "issue-1",
        expect.objectContaining({ description: "" }),
      );
    });
  });

  describe("browser tab title", () => {
    it("sets document.title to identifier and title when issue loads", async () => {
      document.title = "Multica";
      renderIssueDetail();

      await waitFor(() => {
        expect(document.title).toBe("TES-1 Implement authentication | Multica");
      });
    });

    it("restores document.title to Multica on unmount", async () => {
      document.title = "Some old title";
      const { unmount } = renderIssueDetail();

      await waitFor(() => {
        expect(document.title).toBe("TES-1 Implement authentication | Multica");
      });

      unmount();

      expect(document.title).toBe("Multica");
    });

    it("falls back gracefully when identifier is empty", async () => {
      document.title = "Multica";
      mockApiObj.getIssue.mockResolvedValue({
        ...mockIssue,
        identifier: "",
      });

      renderIssueDetail();

      await waitFor(() => {
        expect(document.title).toBe("Implement authentication | Multica");
      });
    });

    it("falls back gracefully when title is empty", async () => {
      document.title = "Multica";
      mockApiObj.getIssue.mockResolvedValue({
        ...mockIssue,
        title: "",
      });

      renderIssueDetail();

      await waitFor(() => {
        expect(document.title).toBe("TES-1 | Multica");
      });
    });

    it("falls back to Multica when both identifier and title are empty", async () => {
      document.title = "Some old title";
      mockApiObj.getIssue.mockResolvedValue({
        ...mockIssue,
        identifier: "",
        title: "",
      });

      renderIssueDetail();

      await waitFor(() => {
        expect(document.title).toBe("Multica");
      });
    });
  });
});
