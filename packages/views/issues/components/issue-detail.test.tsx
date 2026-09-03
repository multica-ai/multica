import { forwardRef, useEffect, useRef, useState, useImperativeHandle } from "react";
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Issue, IssueStatusEntry, Label, TimelineEntry } from "@multica/core/types";
import { issueStatusKeys } from "@multica/core/issue-statuses";
import { I18nProvider } from "@multica/core/i18n/react";
import { toast } from "sonner";
import { useResolvedExpandStore } from "@multica/core/issues/stores/resolved-expand-store";
import {
  DEFAULT_SUB_ISSUE_ROW_PROPERTIES,
  useSubIssueDisplayStore,
} from "@multica/core/issues/stores/sub-issue-display-store";
import enCommon from "../../locales/en/common.json";
import enIssues from "../../locales/en/issues.json";

const TEST_RESOURCES = { en: { common: enCommon, issues: enIssues } };

const mockViewport = vi.hoisted(() => ({ isMobile: false }));

// Counts MockContentEditor mounts. This pins the description to exactly one
// eager editor per issue and catches stale editor reuse across issue switches.
const contentEditorMounts = vi.hoisted(() => ({ count: 0 }));
// Stable empty-attachments reference: the real store returns a shared constant
// so the `useCommentDraftStore(s => s.getAttachments(key))` selector keeps a
// stable identity. A fresh `[]` per call would loop useSyncExternalStore.
const emptyDraftAttachments = vi.hoisted(() => [] as unknown[]);

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
    push: vi.fn(),
    pathname: "/issues/issue-1",
    getShareableUrl: (p: string) => `https://app.multica.com${p}`,
  }),
  useBackOrReplace: () => vi.fn(),
  NavigationProvider: ({ children }: { children: React.ReactNode }) => children,
}));

// Mock editor components (Tiptap requires real DOM)
vi.mock("../../editor", async () => ({
  // Real lazy-mount controller (pure React, no Tiptap) so readonly-first
  // shell → activate → ready flows behave exactly as in production.
  ...(await vi.importActual<typeof import("../../editor/use-lazy-editor")>(
    "../../editor/use-lazy-editor",
  )),
  // Real submit gate (pure React) — see comment-composers.test.tsx.
  ...(await vi.importActual<typeof import("../../editor/use-upload-gate")>(
    "../../editor/use-upload-gate",
  )),
  // Real await-then-render submit hook (pure React) so comment/reply/edit
  // composers run the production submit path.
  ...(await vi.importActual<typeof import("../../editor/use-composer-submit")>(
    "../../editor/use-composer-submit",
  )),
  useEditorUpload: () => ({
    uploadWithToast: vi.fn(),
    upload: vi.fn(),
    uploading: false,
  }),
  useFileDropZone: () => ({ isDragOver: false, dropZoneProps: {} }),
  FileDropOverlay: () => null,
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
  // Pass-through: the detail page wraps its column in the image-sequence
  // provider, but paging between images is covered in
  // image-sequence-context.test.tsx against the real provider.
  ImageSequenceProvider: ({ children }: { children: React.ReactNode }) =>
    children,
  isPreviewable: () => false,
  ReadonlyContent: ({ content }: { content: string }) => (
    <div data-testid="readonly-content">{content}</div>
  ),
  ContentEditor: forwardRef(function MockContentEditor(
    {
      defaultValue,
      value: syncedValue,
      onUpdate,
      placeholder,
      flushPendingOnUnmount,
      onReady,
    }: any,
    ref: any,
  ) {
    const initialValue = syncedValue ?? defaultValue ?? "";
    const valueRef = useRef(initialValue);
    const baseRef = useRef(initialValue);
    const [editorValue, setEditorValue] = useState(initialValue);
    // Mirrors the real editor's dirty guard: once the user has typed, an
    // external `value` change is refused so it cannot clobber unsaved bytes.
    // Only the imperative adoptContent channel lands after that point.
    const dirtyRef = useRef(false);
    useEffect(() => {
      contentEditorMounts.count += 1;
      onReady?.();
      // eslint-disable-next-line react-hooks/exhaustive-deps
    }, []);
    useEffect(() => {
      if (syncedValue === undefined || dirtyRef.current) return;
      valueRef.current = syncedValue;
      baseRef.current = syncedValue;
      setEditorValue(syncedValue);
    }, [syncedValue]);
    useImperativeHandle(ref, () => ({
      getMarkdown: () => valueRef.current,
      clearContent: () => {
        valueRef.current = "";
        dirtyRef.current = false;
        setEditorValue("");
      },
      // The real handle applies content the `value` prop cannot land (a dirty
      // editor refuses external syncs) and does so without emitting an update.
      adoptContent: (markdown: string) => {
        valueRef.current = markdown;
        baseRef.current = markdown;
        dirtyRef.current = false;
        setEditorValue(markdown);
      },
      focus: () => {},
      focusAtCoords: () => {},
      // The top-level composer blurs after a posted comment (afterAccepted).
      blur: () => {},
      // Read by the submit-time upload gate; no uploads are exercised here.
      hasActiveUploads: () => false,
      // Placeholder rebuild contract: the real handle draws a card for an
      // upload the document is not showing and reports whether it landed.
      // Mocks track ids only — no document to draw into.
      insertUploadPlaceholder: () => true,
      settleUploadPlaceholder: () => false,
      uploadFile: () => {},
    }));
    return (
      <textarea
        value={editorValue}
        onChange={(e) => {
          valueRef.current = e.target.value;
          dirtyRef.current = true;
          setEditorValue(e.target.value);
          onUpdate?.(e.target.value, baseRef.current);
        }}
        placeholder={placeholder}
        data-testid="rich-text-editor"
        data-flush-on-unmount={flushPendingOnUnmount ? "true" : undefined}
      />
    );
  }),
  TitleEditor: forwardRef(function MockTitleEditor(
    { defaultValue, placeholder, onBlur, onChange, onReady }: any,
    ref: any,
  ) {
    const valueRef = useRef(defaultValue || "");
    const [value, setValue] = useState(defaultValue || "");
    useEffect(() => {
      onReady?.();
      // eslint-disable-next-line react-hooks/exhaustive-deps
    }, []);
    useImperativeHandle(ref, () => ({
      getText: () => valueRef.current,
      focus: () => {},
      focusAtCoords: () => {},
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
  unsubscribeFromIssueSubtree: vi.fn().mockResolvedValue(undefined),
  getActiveTasksForIssue: vi.fn().mockResolvedValue({ tasks: [] }),
  listTasksByIssue: vi.fn().mockResolvedValue([]),
  rerunIssue: vi.fn(),
  listTaskMessages: vi.fn().mockResolvedValue([]),
  listChildIssues: vi.fn().mockResolvedValue({ issues: [] }),
  getChildIssueProgress: vi.fn().mockResolvedValue({ progress: [] }),
  getAgentTaskSnapshot: vi.fn().mockResolvedValue([]),
  // The sub-issues header chip reads this narrowed to the parent issue.
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

// Mock issue config
vi.mock("@multica/core/issues/config", () => ({
  ALL_STATUSES: ["backlog", "todo", "in_progress", "in_review", "done", "blocked", "cancelled"],
  STATUS_ORDER: ["backlog", "todo", "in_progress", "in_review", "done", "blocked", "cancelled"],
  STATUS_CONFIG: {
    backlog: { label: "Backlog", iconColor: "text-muted-foreground", hoverBg: "hover:bg-accent" },
    todo: { label: "Todo", iconColor: "text-muted-foreground", hoverBg: "hover:bg-accent" },
    in_progress: { label: "In Progress", iconColor: "text-warning", hoverBg: "hover:bg-warning/10" },
    in_review: { label: "In Review", iconColor: "text-success", hoverBg: "hover:bg-success/10" },
    done: { label: "Done", iconColor: "text-info", hoverBg: "hover:bg-info/10" },
    blocked: { label: "Blocked", iconColor: "text-destructive", hoverBg: "hover:bg-destructive/10" },
    cancelled: { label: "Cancelled", iconColor: "text-muted-foreground", hoverBg: "hover:bg-accent" },
  },
  PRIORITY_ORDER: ["urgent", "high", "medium", "low", "none"],
  PRIORITY_DISPLAY_ORDER: ["none", "urgent", "high", "medium", "low"],
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
vi.mock("@multica/core/issues/stores", async () => ({
  // Real store, not a stub: resolved-thread expand/collapse behavior under
  // test runs through it. Deep import keeps the persisted sibling stores
  // (which need localStorage) out of this mock.
  ...(await vi.importActual<
    typeof import("@multica/core/issues/stores/resolved-expand-store")
  >("@multica/core/issues/stores/resolved-expand-store")),
  // Real store: sub-issue display tests drive it with setState, and the
  // component reads it through the barrel — both must hit the same instance.
  ...(await vi.importActual<
    typeof import("@multica/core/issues/stores/sub-issue-display-store")
  >("@multica/core/issues/stores/sub-issue-display-store")),
  // Real store, in-memory (no localStorage): backs the sub-issues section's
  // collapsed state.
  ...(await vi.importActual<
    typeof import("@multica/core/issues/stores/sub-issues-collapse-store")
  >("@multica/core/issues/stores/sub-issues-collapse-store")),
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
        drafts: {} as Record<string, { content: string; attachments: unknown[]; updatedAt: number }>,
        getDraft: () => undefined,
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
      const state = { sticky: true, toggleSticky: () => {} };
      return selector ? selector(state) : state;
    },
    {
      getState: () => ({ sticky: true, toggleSticky: () => {} }),
    },
  ),
}));

// Mock react-virtuoso: jsdom has no real layout, so the real Virtuoso would
// compute a 0-height viewport and render nothing. The mock renders every item
// inline so id="comment-..." nodes are always present in the DOM — this
// matches the production cold-path where `initialItemCount` force-mounts
// items[0..targetIdx], giving the deep-link effect a real target node.
//
// scrollIntoViewSpy: the deep-link effect no longer calls native
// scrollIntoView (it drives the timeline container's scrollTop directly to
// avoid scrolling ancestor overflow:hidden boxes — see issue-detail.tsx). We
// keep a no-op stub on the prototype so any stray scrollIntoView call from
// other components doesn't throw; deep-link tests assert the highlight
// background instead, which is mechanism-independent and observable without
// layout.
const scrollIntoViewSpy = vi.hoisted(() => vi.fn());
const scrollToIndexSpy = vi.hoisted(() => vi.fn());

vi.mock("react-virtuoso", () => ({
  Virtuoso: forwardRef(function MockVirtuoso(
    { data, itemContent }: { data: unknown[]; itemContent: (i: number, item: unknown) => unknown },
    ref: any,
  ) {
    useImperativeHandle(ref, () => ({
      // Real Virtuoso ref methods are not exercised by tests in this file
      // since the deep-link cold-path drives the container's scrollTop on the
      // real DOM node, not Virtuoso's imperative API.
      scrollIntoView: vi.fn(),
      scrollToIndex: scrollToIndexSpy,
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
  scrollToIndexSpy.mockClear();
  Object.defineProperty(HTMLElement.prototype, "scrollIntoView", {
    configurable: true,
    writable: true,
    value: scrollIntoViewSpy,
  });
  // The resolved-expand store is module-global (not per-mount like the old
  // useState); reset so one test's expansions can't leak into the next.
  useResolvedExpandStore.setState({ expandedByIssue: {} });
});

// Mock modals
const mockOpenModal = vi.hoisted(() => vi.fn());
vi.mock("@multica/core/modals", () => ({
  useModalStore: Object.assign(
    (selector?: (state: { open: typeof mockOpenModal }) => unknown) => {
      const state = { open: mockOpenModal };
      return selector ? selector(state) : state;
    },
    { getState: () => ({ open: mockOpenModal }) },
  ),
}));

// Mock core/hooks/use-file-upload
vi.mock("@multica/core/hooks/use-file-upload", () => ({
  useFileUpload: () => ({ uploadWithToast: vi.fn().mockResolvedValue("https://example.com/file.png") }),
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
  toast: { error: vi.fn(), success: vi.fn() },
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
  stage: null,
  start_date: null,
  due_date: "2026-06-01T00:00:00Z",
  metadata: {},
  properties: {},
  created_at: "2026-01-15T00:00:00Z",
  updated_at: "2026-01-20T00:00:00Z",
  revision: 3,
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

import { IssueDetail, groupSubIssuesByStage } from "./issue-detail";

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function createTestQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: 0 },
      mutations: { retry: false },
    },
  });
}

function renderIssueDetail(issueId = "issue-1") {
  const queryClient = createTestQueryClient();
  return render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <QueryClientProvider client={queryClient}>
        <IssueDetail issueId={issueId} />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

/**
 * Renders with the workspace status catalog already in cache, so custom
 * statuses resolve to their real name, category and color. Seeding the query
 * (rather than stubbing the hook) keeps the shipped resolvers in the path; the
 * generous staleTime on the catalog query means it is never refetched. Every
 * other test runs without it — the cold-catalog case. (MUL-6243)
 */
function renderIssueDetailWithStatusCatalog(
  entries: IssueStatusEntry[],
  issueId = "issue-1",
) {
  const queryClient = createTestQueryClient();
  queryClient.setQueryData(issueStatusKeys.list("ws-1"), { statuses: entries });
  return render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <QueryClientProvider client={queryClient}>
        <IssueDetail issueId={issueId} />
      </QueryClientProvider>
    </I18nProvider>,
  );
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

const highlightedCommentBackgroundClass =
  "bg-[color-mix(in_srgb,var(--card)_95%,var(--brand)_5%)]";

function hasHighlightedCommentBackground(root: ParentNode | null): boolean {
  if (!root) return false;

  const elements = root instanceof Element
    ? [root, ...Array.from(root.querySelectorAll("[class]"))]
    : Array.from(root.querySelectorAll("[class]"));

  return elements.some(
    (el) => typeof el.className === "string" && el.className.includes(highlightedCommentBackgroundClass),
  );
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe("IssueDetail (shared)", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    contentEditorMounts.count = 0;
    mockViewport.isMobile = false;
    // Default: issue loads successfully
    mockApiObj.getIssue.mockResolvedValue(mockIssue);
    // /timeline returns the entries flat in chronological order (oldest first).
    mockApiObj.listTimeline.mockResolvedValue(mockTimeline);
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
    mockApiObj.rerunIssue.mockResolvedValue({ id: "task-rerun" });
    mockApiObj.listMembers.mockResolvedValue([
      { user_id: "user-1", name: "Test User", email: "test@test.com", role: "admin" },
    ]);
    mockApiObj.listAgents.mockResolvedValue([]);
    // Reset project mock — individual tests override per case. Default fixture
    // has project_id: null so getProject is not invoked.
    mockApiObj.getProject.mockReset();
  });

  it("opens source-context creation from both a root comment and a reply", async () => {
    mockApiObj.listTimeline.mockResolvedValue([
      { ...mockTimeline[0], id: "source-root", parent_id: null },
      { ...mockTimeline[1], id: "source-reply", parent_id: "source-root" },
    ]);
    const { container } = renderIssueDetail();
    await screen.findByText("Started working on this");

    const menuButton = (commentId: string) => Array.from(
      container.querySelectorAll<HTMLButtonElement>('button[aria-label="Comment actions"]'),
    )
      .find((button): button is HTMLButtonElement => button?.closest("[id^='comment-']")?.id === `comment-${commentId}`);
    await waitFor(() => expect(menuButton("source-root")).toBeTruthy());
    await waitFor(() => expect(menuButton("source-reply")).toBeTruthy());

    fireEvent.click(menuButton("source-root")!);
    const branchAction = await screen.findByText("Create sub-issue from here");
    expect(branchAction.closest('[role="menuitem"]')?.querySelector(".lucide-message-square-plus")).toBeInTheDocument();
    fireEvent.click(branchAction);
    expect(mockOpenModal).toHaveBeenLastCalledWith("quick-create-issue", expect.objectContaining({
      anchor_comment_id: "source-root",
      parent_issue_id: "issue-1",
    }));

    fireEvent.click(menuButton("source-reply")!);
    fireEvent.click(await screen.findByText("Create sub-issue from here"));
    expect(mockOpenModal).toHaveBeenLastCalledWith("quick-create-issue", expect.objectContaining({
      anchor_comment_id: "source-reply",
      parent_issue_id: "issue-1",
    }));
  });

  it("does not offer source-context creation for system comments", async () => {
    mockApiObj.listTimeline.mockResolvedValue([{
      ...mockTimeline[0],
      id: "system-comment",
      comment_type: "system",
    }]);
    const { container } = renderIssueDetail();
    await screen.findByText("Started working on this");

    const menuButton = Array.from(
      container.querySelectorAll<HTMLButtonElement>('button[aria-label="Comment actions"]'),
    ).find((button) => button.closest("[id^='comment-']")?.id === "comment-system-comment");
    expect(menuButton).toBeTruthy();
    fireEvent.click(menuButton!);

    expect(screen.queryByText("Create sub-issue from here")).not.toBeInTheDocument();
  });

  it("shows loading skeleton while data is loading", () => {
    // Make the API hang to keep loading state
    mockApiObj.getIssue.mockReturnValue(new Promise(() => {}));
    renderIssueDetail();

    expect(
      screen.getAllByRole("generic").some((el) => el.getAttribute("data-slot") === "skeleton"),
    ).toBe(true);
  });

  it("gives the skeleton the same horizontal gutters as the loaded column", async () => {
    // The skeleton is the loaded column's stand-in, so a gutter change has to
    // land on both or the column jumps sideways at the moment the issue
    // arrives. Horizontal only: the loaded column also reserves the chat
    // launcher's corner at its bottom, which the skeleton has no scroll to
    // reach.
    const horizontalGutters = (el: Element | null) =>
      (el?.className ?? "")
        .split(/\s+/)
        .filter((cls) => /(^|:)px-/.test(cls))
        .sort();

    mockApiObj.getIssue.mockReturnValue(new Promise(() => {}));
    const loadingRender = renderIssueDetail();
    const skeletonGutters = horizontalGutters(
      loadingRender.container.querySelector(".max-w-4xl"),
    );
    loadingRender.unmount();

    mockApiObj.getIssue.mockResolvedValue(mockIssue);
    const { container } = renderIssueDetail();
    await waitFor(() => {
      expect(screen.getByText("Implement authentication")).toBeInTheDocument();
    });

    // Non-empty guard: without it a renamed column class passes vacuously.
    expect(skeletonGutters.length).toBeGreaterThan(0);
    expect(skeletonGutters).toEqual(
      horizontalGutters(container.querySelector(".max-w-4xl")),
    );
  });

  it("renders issue title and description after loading", async () => {
    renderIssueDetail();

    // The description is the one eager editor: keeping one renderer avoids the
    // layout jump caused by swapping a long react-markdown tree for ProseMirror.
    // Title and comment/reply composers remain readonly-first.
    expect(await screen.findByDisplayValue("Add JWT auth to the backend")).toBeInTheDocument();
    expect(screen.getByText("Implement authentication")).toBeInTheDocument();
    expect(screen.queryByTestId("title-editor")).not.toBeInTheDocument();
    expect(screen.getAllByTestId("rich-text-editor")).toHaveLength(1);
    expect(contentEditorMounts.count).toBe(1);
  });

  it("reconciles a cached list snapshot so source context appears on first entry", async () => {
    const sourceContext: NonNullable<Issue["source_context"]> = {
      id: "context-1",
      version: 1,
      usage: "read_only_historical_background",
      captured_at: "2026-08-21T12:00:00Z",
      display_state: "unchanged",
      source_issue_state: "unchanged",
      comment_thread_state: "unchanged",
      anchor_comment_state: "available",
      can_open_current_source: true,
      current_source: {
        issue_id: "source-issue",
        anchor_comment_id: "source-comment",
        identifier: "TES-7",
      },
      source_author_state: [],
      snapshot: {
        version: 1,
        captured_by_user_id: "user-1",
        captured_at: "2026-08-21T12:00:00Z",
        source_issue: {
          id: "source-issue",
          identifier: "TES-7",
          number: 7,
          title: "Source issue",
          description: "Source description",
          created_at: "2026-08-20T00:00:00Z",
          updated_at: "2026-08-21T00:00:00Z",
          revision: 2,
          attachments: [],
        },
        anchor_comment_id: "source-comment",
        comment_thread: [
          {
            id: "source-comment",
            parent_id: null,
            type: "comment",
            content: "Source comment",
            author: { type: "member", id: "user-1", name: "Test User" },
            created_at: "2026-08-21T01:00:00Z",
            updated_at: "2026-08-21T01:00:00Z",
            revision: 1,
            attachments: [],
          },
        ],
      },
    };
    const cachedWithoutDetailOnlyContext: Issue = {
      ...mockIssue,
      parent_issue_id: "source-issue",
    };
    const authoritativeDetail: Issue = {
      ...cachedWithoutDetailOnlyContext,
      source_context: sourceContext,
    };
    const parentIssue: Issue = {
      ...mockIssue,
      id: "source-issue",
      identifier: "TES-7",
      number: 7,
      title: "Source issue",
      parent_issue_id: null,
    };
    const queryClient = new QueryClient({
      defaultOptions: {
        queries: { retry: false, gcTime: 0, staleTime: Infinity },
        mutations: { retry: false },
      },
    });
    queryClient.setQueryData(
      ["issues", "ws-1", "detail", "issue-1"],
      cachedWithoutDetailOnlyContext,
    );
    mockApiObj.getIssue.mockImplementation((issueId: string) =>
      Promise.resolve(issueId === "source-issue" ? parentIssue : authoritativeDetail),
    );

    render(
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <QueryClientProvider client={queryClient}>
          <IssueDetail issueId="issue-1" />
        </QueryClientProvider>
      </I18nProvider>,
    );

    const snapshotAction = await screen.findByRole("button", { name: "Context snapshot" });
    const summary = snapshotAction.closest<HTMLElement>('[data-slot="source-context-summary"]');
    expect(summary).toHaveTextContent("Sub-issue of");
    expect(within(summary!).getByRole("link", { name: "TES-7 Source issue" })).toBeInTheDocument();
    // The summary replaces the plain parent row rather than adding a second one.
    expect(screen.getAllByText("Sub-issue of")).toHaveLength(1);
    expect(screen.queryByText(/From TES-7/)).not.toBeInTheDocument();
    expect(mockApiObj.getIssue).toHaveBeenCalledWith("issue-1");
  });

  it("opts the description editor into the unmount flush", async () => {
    // Closing the issue modal must save the description the user last saw —
    // ContentEditor drops pending debounced updates on unmount by default
    // (so cancelled comment drafts aren't resurrected), and only this
    // explicit opt-in keeps a paste-then-close from losing the image
    // markdown and its attachment_ids bind (MUL-3254). The flush behavior
    // itself is covered in content-editor.test.tsx; this pins the wiring.
    renderIssueDetail();

    const description = await screen.findByDisplayValue("Add JWT auth to the backend");
    expect(description).toHaveAttribute("data-flush-on-unmount", "true");
  });

  it("remounts the eager description on issue switch without carrying stale content", async () => {
    // The web route reuses IssueDetail across issues. The keyed description
    // editor must remount atomically for the new issue while the title remains
    // on its cheap stand-in.
    const queryClient = createTestQueryClient();
    const issue2 = {
      ...mockIssue,
      id: "issue-2",
      title: "Second issue",
      description: "Second description",
    };
    // The cache seed marks the data stale, so the query refetches in the
    // background — getIssue must answer per-id or the refetch would clobber
    // issue-2 with issue-1's payload.
    mockApiObj.getIssue.mockImplementation((issueId: string) =>
      Promise.resolve(issueId === "issue-2" ? issue2 : mockIssue),
    );
    // Pre-seed issue-2 so its first render skips the loading skeleton.
    queryClient.setQueryData(["issues", "ws-1", "detail", "issue-2"], issue2);
    const ui = (issueId: string) => (
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <QueryClientProvider client={queryClient}>
          <IssueDetail issueId={issueId} />
        </QueryClientProvider>
      </I18nProvider>
    );
    const { rerender } = render(ui("issue-1"));

    await screen.findByDisplayValue("Add JWT auth to the backend");
    const mountsBeforeSwitch = contentEditorMounts.count;

    rerender(ui("issue-2"));

    expect(await screen.findByDisplayValue("Second description")).toBeInTheDocument();
    expect(screen.queryByDisplayValue("Add JWT auth to the backend")).not.toBeInTheDocument();
    expect(screen.queryByTestId("title-editor")).not.toBeInTheDocument();
    expect(contentEditorMounts.count).toBe(mountsBeforeSwitch + 1);
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

  it("renders properties sidebar with all core rows plus set optional rows", async () => {
    renderIssueDetail();

    await waitFor(() => {
      expect(screen.getByText("Properties")).toBeInTheDocument();
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
      expect(screen.getByText("Properties")).toBeInTheDocument();
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

  it("uses a non-resizable layout with the sidebar sheet closed by default on mobile", async () => {
    mockViewport.isMobile = true;

    renderIssueDetail();

    await waitFor(() => {
      expect(screen.getByText("Implement authentication")).toBeInTheDocument();
    });

    expect(screen.queryByTestId("panel-group")).not.toBeInTheDocument();
    expect(screen.queryByText("Properties")).not.toBeInTheDocument();
  });

  it("pins the comment composer to the scroll viewport on a wide screen", async () => {
    const { container } = renderIssueDetail();

    await waitFor(() => {
      expect(screen.getByText("Implement authentication")).toBeInTheDocument();
    });

    // `bottom-0` is unique to the composer wrapper — the sticky affordances
    // inside the timeline (comment headers, resolve bars) all pin to `top-0`.
    expect(container.querySelector(".sticky.bottom-0")).not.toBeNull();
  });

  it("lets the composer ride the end of the timeline on mobile", async () => {
    // A pinned composer on a phone sits on the chat launcher's corner at every
    // scroll position, and the part it covers is its own send button.
    mockViewport.isMobile = true;

    const { container } = renderIssueDetail();

    await waitFor(() => {
      expect(screen.getByText("Implement authentication")).toBeInTheDocument();
    });

    expect(container.querySelector(".sticky.bottom-0")).toBeNull();
  });

  it("realigns after Virtuoso measures the newly posted row", async () => {
    mockApiObj.createComment.mockResolvedValue({
      id: "comment-new",
      issue_id: "issue-1",
      content: "A new update",
      author_type: "member",
      author_id: "user-1",
      parent_id: null,
      type: "comment",
      created_at: "2026-08-13T00:00:00Z",
      updated_at: "2026-08-13T00:00:00Z",
    });
    renderIssueDetail();

    await screen.findByText("Implement authentication");
    fireEvent.click(screen.getByTestId("comment-composer-shell"));
    const editor = await screen.findByPlaceholderText("Leave a comment...");
    fireEvent.change(editor, { target: { value: "A new update" } });
    const composer = editor.closest<HTMLElement>("[aria-busy], .relative.flex.flex-col.rounded-lg")!;
    fireEvent.click(within(composer).getByRole("button", { name: "Send" }));

    expect(scrollToIndexSpy).not.toHaveBeenCalled();
    await waitFor(() => {
      expect(scrollToIndexSpy).toHaveBeenCalledTimes(2);
    });
    expect(scrollToIndexSpy).toHaveBeenNthCalledWith(1, { index: 2, align: "end", offset: 0 });
    expect(scrollToIndexSpy).toHaveBeenNthCalledWith(2, { index: 2, align: "end", offset: 0 });
    expect(scrollIntoViewSpy).not.toHaveBeenCalled();
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

  // Details is creator + immutable timestamps, so it ranks below the
  // execution log, which is what people actually open the sidebar for.
  it("orders the Details section after the execution log", async () => {
    mockApiObj.listTasksByIssue.mockResolvedValue([
      {
        id: "task-past",
        agent_id: "agent-1",
        runtime_id: "runtime-1",
        issue_id: "issue-1",
        status: "completed",
        priority: 0,
        dispatched_at: null,
        started_at: "2026-06-08T08:00:00Z",
        completed_at: "2026-06-08T08:05:00Z",
        result: null,
        error: null,
        created_at: "2026-06-08T08:00:00Z",
        trigger_summary: "Started from comment",
      },
    ]);

    renderIssueDetail();

    const executionLog = await screen.findByText("Execution log");
    const details = screen.getByText("Details");

    // DOCUMENT_POSITION_FOLLOWING: Details comes after the execution log.
    expect(
      executionLog.compareDocumentPosition(details) & Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
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

  it("shows 'Back' button when issue is not found and no onDelete prop", async () => {
    mockApiObj.getIssue.mockRejectedValue(new Error("Not found"));

    renderIssueDetail("nonexistent-id");

    await waitFor(() => {
      expect(screen.getByText("Back")).toBeInTheDocument();
    });
  });

  it("renders comments from timeline", async () => {
    renderIssueDetail();

    await waitFor(() => {
      expect(screen.getByText("Started working on this")).toBeInTheDocument();
    });

    expect(screen.getByText("I can help with this")).toBeInTheDocument();
  });

  it("prefers timeline identity when the actor is absent from the member directory", async () => {
    mockApiObj.listTimeline.mockResolvedValue([
      {
        type: "comment",
        id: "former-member-comment",
        actor_type: "member",
        actor_id: "former-user-1",
        actor_name: "Former Member",
        actor_avatar_url: "https://profiles.example.com/former.png",
        content: "Authored before leaving",
        parent_id: null,
        created_at: "2026-01-18T00:00:00Z",
        updated_at: "2026-01-18T00:00:00Z",
        comment_type: "comment",
      },
    ]);

    renderIssueDetail();

    await screen.findByText("Authored before leaving");
    expect(screen.getByText("Former Member")).toBeInTheDocument();
  });

  it("reruns the source task from an agent failure comment", async () => {
    mockApiObj.listTimeline.mockResolvedValue([
      ...mockTimeline,
      {
        type: "comment",
        id: "comment-failed-task",
        actor_type: "agent",
        actor_id: "agent-1",
        content: "API Error: 500 Internal server error",
        parent_id: null,
        created_at: "2026-01-18T00:00:00Z",
        updated_at: "2026-01-18T00:00:00Z",
        comment_type: "system",
        source_task_id: "task-failed",
      },
    ]);

    renderIssueDetail();

    await screen.findByText("API Error: 500 Internal server error");
    fireEvent.click(screen.getByRole("button", { name: "Retry run" }));

    await waitFor(() => {
      expect(mockApiObj.rerunIssue).toHaveBeenCalledWith("issue-1", "task-failed");
    });
  });

  it("does not show retry for child-done system comments", async () => {
    mockApiObj.listTimeline.mockResolvedValue([
      ...mockTimeline,
      {
        type: "comment",
        id: "comment-child-done",
        actor_type: "system",
        actor_id: "00000000-0000-0000-0000-000000000000",
        content: "Sub-issue MUL-123 is done.",
        parent_id: null,
        created_at: "2026-01-18T00:00:00Z",
        updated_at: "2026-01-18T00:00:00Z",
        comment_type: "system",
      },
    ]);

    renderIssueDetail();

    await screen.findByText("Sub-issue MUL-123 is done.");
    expect(screen.queryByRole("button", { name: "Retry run" })).not.toBeInTheDocument();
  });

  it("does not show retry for successful agent task comments", async () => {
    mockApiObj.listTimeline.mockResolvedValue([
      ...mockTimeline,
      {
        type: "comment",
        id: "comment-successful-task",
        actor_type: "agent",
        actor_id: "agent-1",
        content: "Finished the requested work.",
        parent_id: null,
        created_at: "2026-01-18T00:00:00Z",
        updated_at: "2026-01-18T00:00:00Z",
        comment_type: "comment",
        source_task_id: "task-success",
      },
    ]);

    renderIssueDetail();

    await screen.findByText("Finished the requested work.");
    expect(screen.queryByRole("button", { name: "Retry run" })).not.toBeInTheDocument();
  });

  it("does not show retry for agent system comments without a source task", async () => {
    mockApiObj.listTimeline.mockResolvedValue([
      ...mockTimeline,
      {
        type: "comment",
        id: "comment-agent-system",
        actor_type: "agent",
        actor_id: "agent-1",
        content: "System coordination update.",
        parent_id: null,
        created_at: "2026-01-18T00:00:00Z",
        updated_at: "2026-01-18T00:00:00Z",
        comment_type: "system",
      },
    ]);

    renderIssueDetail();

    await screen.findByText("System coordination update.");
    expect(screen.queryByRole("button", { name: "Retry run" })).not.toBeInTheDocument();
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

  it("renders activity rows with unknown status values without crashing", async () => {
    mockApiObj.listTimeline.mockResolvedValue([
      {
        type: "activity",
        id: "act-unknown-status",
        actor_type: "member",
        actor_id: "user-1",
        action: "status_changed",
        details: { from: "todo", to: "mystery_status" },
        created_at: "2026-01-18T00:00:00Z",
      },
    ] as TimelineEntry[]);

    renderIssueDetail();

    await waitFor(() => {
      expect(screen.getByText(/from Todo to mystery_status/i)).toBeInTheDocument();
    });
  });

  // -------------------------------------------------------------------------
  // MUL-6413 — the activity glyph is per CATEGORY, so a move into a custom
  // status drew the icon of the built-in it sits beside: "In Review → Awaiting
  // Response" repainted identically and read as though nothing had moved.
  // Colour is what carries a custom status's own identity.
  // -------------------------------------------------------------------------

  const IN_REVIEW_BUILT_IN: IssueStatusEntry = {
    id: "in_review",
    workspace_id: "ws-1",
    key: "in_review",
    name: "In Review",
    description: "",
    category: "in_review",
    color: "#8b5cf6",
    is_system: true,
    position: 0,
    archived_at: null,
    created_at: "",
    updated_at: "",
  };

  const AWAITING_RESPONSE: IssueStatusEntry = {
    ...IN_REVIEW_BUILT_IN,
    id: "awaiting_response",
    key: "awaiting_response",
    name: "Awaiting Response",
    color: "#ff0000",
    is_system: false,
    position: 1,
  };

  function statusChangeIcon(to: string): SVGElement {
    const row = screen.getByText(new RegExp(`to ${to}$`, "i")).closest("div")
      ?.parentElement;
    const icon = row?.querySelector("svg");
    if (!icon) throw new Error(`no status glyph for the "${to}" activity row`);
    return icon;
  }

  it("paints a status-change activity in the custom status's own colour", async () => {
    mockApiObj.listTimeline.mockResolvedValue([
      {
        type: "activity",
        id: "act-custom-status",
        actor_type: "member",
        actor_id: "user-1",
        action: "status_changed",
        details: { from: "in_review", to: "awaiting_response" },
        created_at: "2026-01-18T00:00:00Z",
      },
    ] as TimelineEntry[]);

    renderIssueDetailWithStatusCatalog([IN_REVIEW_BUILT_IN, AWAITING_RESPONSE]);

    await waitFor(() => {
      expect(
        screen.getByText(/from In Review to Awaiting Response/i),
      ).toBeInTheDocument();
    });
    expect(statusChangeIcon("Awaiting Response").style.color).toBe("rgb(255, 0, 0)");
  });

  it("leaves a built-in status-change activity on its semantic token colour", async () => {
    // The catalog seeds a colour for the built-ins too, but those are theme
    // tokens in the UI — painting the seeded hex would hard-code one theme.
    mockApiObj.listTimeline.mockResolvedValue([
      {
        type: "activity",
        id: "act-built-in-status",
        actor_type: "member",
        actor_id: "user-1",
        action: "status_changed",
        details: { from: "in_progress", to: "in_review" },
        created_at: "2026-01-18T00:00:00Z",
      },
    ] as TimelineEntry[]);

    renderIssueDetailWithStatusCatalog([IN_REVIEW_BUILT_IN, AWAITING_RESPONSE]);

    await waitFor(() => {
      expect(screen.getByText(/from In Progress to In Review/i)).toBeInTheDocument();
    });
    const icon = statusChangeIcon("In Review");
    expect(icon.style.color).toBe("");
    expect(icon.getAttribute("class")).toContain("text-success");
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

      // The deep-link effect lands on AND highlights the target comment: it
      // drives the timeline container's scrollTop directly (jsdom has no
      // layout, so the scroll itself isn't observable here) and applies the
      // brand highlight background. Assert the user-facing highlight.
      await waitFor(() => {
        expect(
          hasHighlightedCommentBackground(document.getElementById("comment-comment-2")),
        ).toBe(true);
      });
    });

    it("highlights only the target root comment, not the whole thread", async () => {
      mockApiObj.listTimeline.mockResolvedValue([
        {
          type: "comment",
          id: "comment-root",
          actor_type: "member",
          actor_id: "user-1",
          content: "Root target",
          parent_id: null,
          created_at: "2026-01-18T00:00:00Z",
          updated_at: "2026-01-18T00:00:00Z",
          comment_type: "comment",
        } as TimelineEntry,
        {
          type: "comment",
          id: "reply-under-root",
          actor_type: "member",
          actor_id: "user-1",
          content: "Reply should stay neutral",
          parent_id: "comment-root",
          created_at: "2026-01-18T01:00:00Z",
          updated_at: "2026-01-18T01:00:00Z",
          comment_type: "comment",
        } as TimelineEntry,
      ]);

      renderIssueDetailWithHighlight("comment-root");

      await waitFor(() => {
        expect(document.getElementById("comment-comment-root")).not.toBeNull();
      });
      await waitFor(() => {
        expect(
          hasHighlightedCommentBackground(document.getElementById("comment-comment-root")),
        ).toBe(true);
      });

      const reply = document.getElementById("comment-reply-under-root");
      expect(reply).not.toBeNull();
      expect(hasHighlightedCommentBackground(reply)).toBe(false);
    });

    it("still scrolls when the timeline is ready before the issue (regression for inbox click)", async () => {
      // Reproduces the inbox-click race: timeline data is in the cache
      // before the issue resolves. While loading is true, IssueDetail
      // renders the loading skeleton (the timeline never mounts), so no
      // scroll/highlight can fire. After the issue resolves, the timeline
      // mounts and the deep-link effect lands on + highlights the comment.
      let resolveIssue: (value: Issue) => void = () => {};
      const issuePromise = new Promise<Issue>((resolve) => {
        resolveIssue = resolve;
      });
      mockApiObj.getIssue.mockReturnValue(issuePromise);

      renderIssueDetailWithHighlight("comment-2", "issue-1", { seedTimeline: true });

      expect(
        document.getElementById("comment-comment-2"),
      ).toBeNull();
      // Nothing highlighted while the loading skeleton is up.
      expect(hasHighlightedCommentBackground(document)).toBe(false);

      resolveIssue(mockIssue);

      await waitFor(() => {
        expect(
          document.getElementById("comment-comment-2"),
        ).not.toBeNull();
      });
      await waitFor(() => {
        expect(
          hasHighlightedCommentBackground(document.getElementById("comment-comment-2")),
        ).toBe(true);
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
      // -unfolded CommentCard) and the deep-link effect must land on + highlight
      // it. The reply highlight renders as a computed bg tint on its row (see
      // CommentCard's reply branch), so assert the row carries the brand tint.
      await waitFor(() => {
        expect(
          document.getElementById("comment-reply-1"),
        ).not.toBeNull();
      });
      await waitFor(() => {
        expect(
          document.getElementById("comment-reply-1")?.className,
        ).toContain("bg-[color-mix(in_srgb,var(--card)_95%,var(--brand)_5%)]");
      });
    });
  });

  it("marks a reply-resolved thread as resolved on the quick-jump rail", async () => {
    // A resolution on a REPLY leaves the thread expanded, so it flattens to a
    // plain `comment` item, not a `resolved-bar`. The rail must still read it
    // as resolved — proof the flag comes from deriveThreadResolution and not
    // from the fold state.
    mockApiObj.listTimeline.mockResolvedValue([
      ...mockTimeline,
      {
        type: "comment",
        id: "reply-1",
        actor_type: "member",
        actor_id: "user-1",
        content: "That fixed it",
        parent_id: "comment-1",
        created_at: "2026-01-18T00:00:00Z",
        updated_at: "2026-01-18T00:00:00Z",
        comment_type: "comment",
        resolved_at: "2026-01-19T00:00:00Z",
      } as TimelineEntry,
    ]);

    renderIssueDetail();

    await waitFor(() => {
      expect(
        screen.getByRole("button", { name: "Started working on this (resolved)" }),
      ).toBeInTheDocument();
    });
    expect(screen.getByRole("button", { name: "I can help with this" })).toBeInTheDocument();
  });

  it("sends empty description when editor is cleared", async () => {
    renderIssueDetail();

    const editor = await screen.findByDisplayValue("Add JWT auth to the backend");
    fireEvent.change(editor, { target: { value: "" } });

    await waitFor(() => {
      expect(mockApiObj.updateIssue).toHaveBeenCalledWith(
        "issue-1",
        expect.objectContaining({
          description: "",
          description_base: "Add JWT auth to the backend",
        }),
      );
    });
  });

  // Descriptions are last-write-wins (MUL-6971). The baseline still ships as
  // channel-media merge metadata, but a rejected save no longer opens a compare
  // panel — that panel fired on the editor's own autosave and wedged the
  // session, and the title/comment editors keep the compare flow they can
  // actually satisfy.
  it("keeps editing after a failed description save without a compare panel", async () => {
    mockApiObj.updateIssue.mockRejectedValueOnce({
      body: { code: "revision_conflict" },
    });
    renderIssueDetail();

    const editor = await screen.findByDisplayValue("Add JWT auth to the backend");
    fireEvent.focus(editor);
    fireEvent.change(editor, { target: { value: "My local description" } });

    await waitFor(() =>
      expect(mockApiObj.updateIssue).toHaveBeenCalledWith(
        "issue-1",
        expect.objectContaining({
          description: "My local description",
          description_base: "Add JWT auth to the backend",
        }),
      ),
    );
    // The compare panel's own actions — still rendered for a title conflict,
    // so their absence is about the description, not a missing translation.
    expect(
      screen.queryByRole("button", { name: "Keep my version" }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Use the latest version" }),
    ).not.toBeInTheDocument();
    expect(screen.getByDisplayValue("My local description")).toBeVisible();

    // The save gate reopened: the next edit still reaches the server.
    fireEvent.change(editor, { target: { value: "My next description" } });
    await waitFor(() =>
      expect(mockApiObj.updateIssue).toHaveBeenLastCalledWith(
        "issue-1",
        expect.objectContaining({ description: "My next description" }),
      ),
    );
  });

  it("serializes description saves and rebases the queued draft on submitted content", async () => {
    let resolveFirst!: (issue: Issue) => void;
    const firstSave = new Promise<Issue>((resolve) => {
      resolveFirst = resolve;
    });
    mockApiObj.updateIssue
      .mockReturnValueOnce(firstSave)
      .mockResolvedValueOnce({
        ...mockIssue,
        description: "Second local description",
        revision: 5,
      });
    renderIssueDetail();

    const editor = await screen.findByDisplayValue("Add JWT auth to the backend");
    fireEvent.focus(editor);
    fireEvent.change(editor, { target: { value: "First local description" } });
    await waitFor(() => expect(mockApiObj.updateIssue).toHaveBeenCalledTimes(1));
    fireEvent.change(editor, { target: { value: "Second local description" } });
    expect(mockApiObj.updateIssue).toHaveBeenCalledTimes(1);

    await act(async () => {
      resolveFirst({ ...mockIssue, description: "First local description", revision: 4 });
      await firstSave;
    });

    await waitFor(() => expect(mockApiObj.updateIssue).toHaveBeenCalledTimes(2));
    expect(mockApiObj.updateIssue).toHaveBeenNthCalledWith(
      2,
      "issue-1",
      expect.objectContaining({
        description: "Second local description",
        description_base: "First local description",
      }),
    );
  });

  it("ignores a late description callback after switching issues", async () => {
    const queryClient = createTestQueryClient();
    const issue2: Issue = {
      ...mockIssue,
      id: "issue-2",
      identifier: "TES-2",
      description: "Second issue description",
      revision: 8,
    };
    queryClient.setQueryData(["issues", "ws-1", "detail", "issue-2"], issue2);
    mockApiObj.getIssue.mockImplementation((issueId: string) =>
      Promise.resolve(issueId === "issue-2" ? issue2 : mockIssue),
    );

    let resolveFirst!: (issue: Issue) => void;
    const firstSave = new Promise<Issue>((resolve) => {
      resolveFirst = resolve;
    });
    mockApiObj.updateIssue
      .mockReturnValueOnce(firstSave)
      .mockResolvedValueOnce({ ...issue2, description: "Issue two draft", revision: 9 });

    const ui = (issueId: string) => (
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <QueryClientProvider client={queryClient}>
          <IssueDetail issueId={issueId} />
        </QueryClientProvider>
      </I18nProvider>
    );
    const { rerender } = render(ui("issue-1"));

    const issueOneEditor = await screen.findByDisplayValue("Add JWT auth to the backend");
    fireEvent.focus(issueOneEditor);
    fireEvent.change(issueOneEditor, {
      target: { value: "Issue one draft" },
    });
    await waitFor(() => expect(mockApiObj.updateIssue).toHaveBeenCalledTimes(1));

    rerender(ui("issue-2"));
    const issueTwoEditor = await screen.findByDisplayValue("Second issue description");
    fireEvent.focus(issueTwoEditor);
    fireEvent.change(issueTwoEditor, {
      target: { value: "Issue two draft" },
    });
    await waitFor(() => expect(mockApiObj.updateIssue).toHaveBeenCalledTimes(2));
    expect(mockApiObj.updateIssue).toHaveBeenNthCalledWith(
      2,
      "issue-2",
      expect.objectContaining({ description_base: "Second issue description" }),
    );

    await act(async () => {
      resolveFirst({ ...mockIssue, description: "Issue one draft", revision: 4 });
      await firstSave;
    });

    expect(mockApiObj.updateIssue).toHaveBeenCalledTimes(2);
    expect(screen.getByDisplayValue("Issue two draft")).toBeVisible();
  });

  it("keeps a title draft visible when its captured content conflicts", async () => {
    mockApiObj.updateIssue.mockRejectedValueOnce({
      body: { code: "revision_conflict" },
    });
    renderIssueDetail();

    fireEvent.click(await screen.findByRole("button", { name: "Implement authentication" }));
    const editor = await screen.findByTestId("title-editor");
    fireEvent.change(editor, { target: { value: "My local title" } });
    fireEvent.blur(editor);

    await waitFor(() =>
      expect(mockApiObj.updateIssue).toHaveBeenCalledWith(
        "issue-1",
        expect.objectContaining({
          title: "My local title",
          title_base: "Implement authentication",
        }),
      ),
    );
    expect(
      await screen.findByText("The title was changed concurrently. Compare both versions."),
    ).toBeVisible();
    expect(screen.getAllByText("My local title").length).toBeGreaterThan(0);
    expect(screen.getByDisplayValue("My local title")).toBeVisible();
  });

  it("restores the server title when the user takes the latest version", async () => {
    mockApiObj.updateIssue.mockRejectedValueOnce({
      body: { code: "revision_conflict" },
    });
    renderIssueDetail();

    fireEvent.click(await screen.findByRole("button", { name: "Implement authentication" }));
    const editor = await screen.findByTestId("title-editor");
    fireEvent.change(editor, { target: { value: "My local title" } });
    fireEvent.blur(editor);

    expect(
      await screen.findByText("The title was changed concurrently. Compare both versions."),
    ).toBeVisible();
    const callsBeforeDiscard = mockApiObj.updateIssue.mock.calls.length;

    fireEvent.click(screen.getByRole("button", { name: "Use the latest version" }));

    await waitFor(() =>
      expect(
        screen.queryByText("The title was changed concurrently. Compare both versions."),
      ).not.toBeInTheDocument(),
    );
    // TitleEditor takes its text at mount, so the remount is what puts the
    // server title back — see titleResetToken.
    expect(await screen.findByDisplayValue("Implement authentication")).toBeVisible();
    // Taking the server version is local-only: the server already holds it.
    expect(mockApiObj.updateIssue).toHaveBeenCalledTimes(callsBeforeDiscard);
  });

  describe("sub-issues list", () => {
    beforeEach(() => {
      useSubIssueDisplayStore.setState({
        rowProperties: { ...DEFAULT_SUB_ISSUE_ROW_PROPERTIES },
        rowPropertyIds: [],
      });
    });

    const label = (id: string, name: string): Label => ({
      id,
      workspace_id: "ws-1",
      resource_type: "issue",
      name,
      color: "#3b82f6",
      created_at: "2026-01-01T00:00:00Z",
      updated_at: "2026-01-01T00:00:00Z",
    });

    const subIssue = (overrides: Partial<Issue>): Issue => ({
      ...mockIssue,
      parent_issue_id: "issue-1",
      assignee_type: null,
      assignee_id: null,
      due_date: null,
      priority: "none",
      ...overrides,
    });

    it("renders priority, labels, due date and nested progress on rows", async () => {
      mockApiObj.listChildIssues.mockResolvedValue({
        issues: [
          subIssue({
            id: "child-1",
            number: 11,
            identifier: "TES-11",
            title: "Fix login flow",
            priority: "urgent",
            labels: [label("l1", "backend"), label("l2", "auth")],
            // Past date-only value → overdue styling on an open issue.
            due_date: "2020-01-01",
          }),
          subIssue({
            id: "child-2",
            number: 12,
            identifier: "TES-12",
            title: "Ship dashboards",
          }),
        ],
      });
      mockApiObj.getChildIssueProgress.mockResolvedValue({
        progress: [{ parent_issue_id: "child-1", done: 1, total: 3 }],
      });

      renderIssueDetail();

      await screen.findByText("Fix login flow");
      // Label chips ride inside the row link.
      expect(screen.getByText("backend")).toBeInTheDocument();
      expect(screen.getByText("auth")).toBeInTheDocument();
      // Nested own-children progress for child-1 only (header shows 0/2).
      expect(await screen.findByText("1/3")).toBeInTheDocument();
      // Overdue open issue renders its due date in the destructive tone.
      const due = screen.getByText("Jan 1");
      expect(due.closest("span")?.className).toContain("text-destructive");
      // Bare row shows no due date / no progress chip of its own.
      const bareRow = screen.getByText("Ship dashboards").closest("a");
      expect(bareRow?.textContent).not.toContain("/");
    });

    it("hides fields the user toggled off in the display preference", async () => {
      useSubIssueDisplayStore.setState({
        rowProperties: {
          ...DEFAULT_SUB_ISSUE_ROW_PROPERTIES,
          labels: false,
          dueDate: false,
        },
      });
      mockApiObj.listChildIssues.mockResolvedValue({
        issues: [
          subIssue({
            id: "child-1",
            number: 11,
            identifier: "TES-11",
            title: "Fix login flow",
            labels: [label("l1", "backend")],
            due_date: "2020-01-01",
          }),
        ],
      });

      renderIssueDetail();

      await screen.findByText("Fix login flow");
      expect(screen.queryByText("backend")).not.toBeInTheDocument();
      expect(screen.queryByText("Jan 1")).not.toBeInTheDocument();
    });

    it("renders opted-in custom property chips on rows that carry a value", async () => {
      const estimate = {
        id: "prop-1",
        workspace_id: "ws-1",
        name: "Estimate",
        type: "text",
        config: {},
        position: 0,
        archived: false,
        created_at: "2026-01-01T00:00:00Z",
        updated_at: "2026-01-01T00:00:00Z",
      };
      mockApiObj.listProperties.mockResolvedValue({
        properties: [estimate],
        total: 1,
      });
      useSubIssueDisplayStore.setState({ rowPropertyIds: ["prop-1"] });
      mockApiObj.listChildIssues.mockResolvedValue({
        issues: [
          subIssue({
            id: "child-1",
            number: 11,
            identifier: "TES-11",
            title: "Fix login flow",
            properties: { "prop-1": "Sprint 3" },
          }),
          subIssue({
            id: "child-2",
            number: 12,
            identifier: "TES-12",
            title: "Ship dashboards",
          }),
        ],
      });

      renderIssueDetail();

      await screen.findByText("Fix login flow");
      // Chip renders only on the row that has a value for the property.
      expect(await screen.findByText("Sprint 3")).toBeInTheDocument();
      expect(screen.getAllByText("Sprint 3")).toHaveLength(1);
    });

    it("mutes the due date on done sub-issues even when past", async () => {
      mockApiObj.listChildIssues.mockResolvedValue({
        issues: [
          subIssue({
            id: "child-1",
            number: 11,
            identifier: "TES-11",
            title: "Wrapped up",
            status: "done",
            due_date: "2020-01-01",
          }),
        ],
      });

      renderIssueDetail();

      await screen.findByText("Wrapped up");
      const due = screen.getByText("Jan 1");
      expect(due.closest("span")?.className).not.toContain("text-destructive");
      expect(due.closest("span")?.className).toContain("text-muted-foreground");
    });
  });

  // Deliberately drives the real Base UI DropdownMenu rather than a stub: the
  // bug these tests pin (MUL-5710) was a handler wired to `onSelect`, which
  // typechecks because Menu.Item's props extend the whole div attribute set,
  // then lands on the DOM as the native text-selection event and never fires.
  // Only the real menu reproduces that; any hand-rolled mock hides it.
  describe("unsubscribe menu", () => {
    const subscribedAsMember = [
      {
        issue_id: "issue-1",
        user_type: "member" as const,
        user_id: "user-1",
        reason: "manual" as const,
        created_at: "2026-01-01T00:00:00Z",
      },
    ];

    beforeEach(() => {
      mockApiObj.listIssueSubscribers.mockResolvedValue(subscribedAsMember);
      // The menu only exists when there is a sub-tree for its second item to
      // act on. A childless issue renders a direct button instead — covered
      // by the "no sub-issues" tests below (MUL-5714).
      mockApiObj.listChildIssues.mockResolvedValue({
        issues: [{ ...mockIssue, id: "child-1", parent_issue_id: "issue-1" }],
      });
    });

    // Base UI portals the popup onto document.body; RTL unmounts it, but wipe
    // the body too so a leftover portal can't duplicate menu item names.
    afterEach(() => {
      document.body.innerHTML = "";
    });

    async function openUnsubscribeMenu() {
      renderIssueDetail();
      fireEvent.click(await screen.findByText("Unsubscribe"));
      return screen.findByRole("menu");
    }

    it("unsubscribes the current member when the single-issue item is clicked", async () => {
      await openUnsubscribeMenu();

      fireEvent.click(
        screen.getByRole("menuitem", { name: "Unsubscribe from this issue" }),
      );

      await waitFor(() =>
        expect(mockApiObj.unsubscribeFromIssue).toHaveBeenCalledWith(
          "issue-1",
          "user-1",
          "member",
        ),
      );
      expect(mockApiObj.unsubscribeFromIssueSubtree).not.toHaveBeenCalled();
    });

    it("unsubscribes from the whole subtree when the subtree item is clicked", async () => {
      await openUnsubscribeMenu();

      fireEvent.click(
        screen.getByRole("menuitem", {
          name: "Unsubscribe from this issue and its sub-issues",
        }),
      );

      await waitFor(() =>
        expect(mockApiObj.unsubscribeFromIssueSubtree).toHaveBeenCalledWith(
          "issue-1",
          "user-1",
          "member",
        ),
      );
      expect(mockApiObj.unsubscribeFromIssue).not.toHaveBeenCalled();
    });

    it("still offers the menu while the child count is unknown", async () => {
      // Children never resolve, so the component cannot yet tell a childless
      // issue from one with a sub-tree. The menu is the safe answer: unlike
      // the direct button it never picks an opt-out scope for the user.
      mockApiObj.listChildIssues.mockReturnValue(new Promise(() => {}));
      renderIssueDetail();

      const control = await screen.findByText("Unsubscribe");

      expect(control.getAttribute("aria-haspopup")).toBe("menu");
    });
  });

  // The reported bug: on an issue with no sub-issues the only way to leave was
  // a menu whose second item pointed at a sub-tree that does not exist
  // (MUL-5714).
  describe("unsubscribe without sub-issues", () => {
    const subscribedAsMember = [
      {
        issue_id: "issue-1",
        user_type: "member" as const,
        user_id: "user-1",
        reason: "manual" as const,
        created_at: "2026-01-01T00:00:00Z",
      },
    ];

    beforeEach(() => {
      mockApiObj.listIssueSubscribers.mockResolvedValue(subscribedAsMember);
      mockApiObj.listChildIssues.mockResolvedValue({ issues: [] });
    });

    afterEach(() => {
      document.body.innerHTML = "";
    });

    // The label is the same either way, so aria-haspopup is what separates the
    // menu trigger from the plain button. The subscribers query resolves first,
    // so the control is briefly the trigger before the child count settles —
    // wait for the collapsed form rather than grabbing the first match.
    async function findDirectUnsubscribeButton() {
      return waitFor(() => {
        const el = screen.getByText("Unsubscribe");
        expect(el.getAttribute("aria-haspopup")).toBeNull();
        return el;
      });
    }

    it("unsubscribes in one click, with no menu", async () => {
      renderIssueDetail();

      fireEvent.click(await findDirectUnsubscribeButton());

      await waitFor(() =>
        expect(mockApiObj.unsubscribeFromIssue).toHaveBeenCalledWith(
          "issue-1",
          "user-1",
          "member",
        ),
      );
      expect(screen.queryByRole("menu")).toBeNull();
    });

    // The scope choice, pinned. RemoveIssueSubscriber writes
    // opt_out_scope='issue'; the subtree route writes 'subtree', which also
    // blocks FUTURE children from re-subscribing the user. Collapsing the menu
    // must not quietly upgrade a one-issue opt-out into a whole-tree one
    // (server/pkg/db/queries/subscriber.sql).
    it("uses the root-only route, never the subtree one", async () => {
      renderIssueDetail();

      fireEvent.click(await findDirectUnsubscribeButton());

      await waitFor(() =>
        expect(mockApiObj.unsubscribeFromIssue).toHaveBeenCalled(),
      );
      expect(mockApiObj.unsubscribeFromIssueSubtree).not.toHaveBeenCalled();
    });

    it("sends one request for a rapid double-click", async () => {
      let release: (() => void) | undefined;
      mockApiObj.unsubscribeFromIssue.mockReturnValue(
        new Promise<void>((resolve) => {
          release = () => resolve();
        }),
      );
      renderIssueDetail();
      const button = await findDirectUnsubscribeButton();

      // Same tick, no await between. React Query flushes isPending in a
      // microtask, so `disabled` has not landed yet and the second click still
      // reaches an enabled button — the in-flight guard is what stops it. Two
      // overlapping toggles is the one case the mutation's whole-list snapshot
      // cannot survive: the second snapshots the first one's optimistic patch
      // and rolls back to it (MUL-5714).
      fireEvent.click(button);
      fireEvent.click(button);

      // Both handlers already ran synchronously; only the request dispatch is
      // async. So once any call has landed, the count is final.
      await waitFor(() =>
        expect(mockApiObj.unsubscribeFromIssue).toHaveBeenCalled(),
      );
      expect(mockApiObj.unsubscribeFromIssue).toHaveBeenCalledTimes(1);
      release?.();
    });

    it("disables the button while the toggle is in flight, then reports failure", async () => {
      let reject: ((err: Error) => void) | undefined;
      mockApiObj.unsubscribeFromIssue.mockReturnValue(
        new Promise<void>((_resolve, rej) => {
          reject = (err) => rej(err);
        }),
      );
      renderIssueDetail();
      const button = await findDirectUnsubscribeButton();

      fireEvent.click(button);

      await waitFor(() =>
        expect((button as HTMLButtonElement).disabled).toBe(true),
      );

      reject?.(new Error("boom"));

      // The optimistic patch rolls itself back, restoring the exact row the
      // user started from — without this message that is indistinguishable
      // from a button that never fired.
      await waitFor(() =>
        expect(toast.error).toHaveBeenCalledWith(
          enIssues.detail.subscription_update_failed,
        ),
      );
      await waitFor(() =>
        expect((button as HTMLButtonElement).disabled).toBe(false),
      );
    });
  });

  // Before the subscribers query resolves the hook's list defaults to empty,
  // which reads as "not subscribed" for everyone. Rendering that default
  // showed a Subscribe button to people who were already subscribed, and a
  // click landing in that window sent a subscribe instead of the unsubscribe
  // they meant (MUL-5714).
  describe("subscription state before the query resolves", () => {
    afterEach(() => {
      document.body.innerHTML = "";
    });

    it("renders no subscribe control while subscribers are loading", async () => {
      mockApiObj.listIssueSubscribers.mockReturnValue(new Promise(() => {}));
      renderIssueDetail();

      // Wait for the issue itself, so this asserts on a rendered page rather
      // than on the loading skeleton.
      await screen.findByText("Implement authentication");

      expect(screen.queryByText("Subscribe")).toBeNull();
      expect(screen.queryByText("Unsubscribe")).toBeNull();
    });

    it("renders Subscribe once the query says the user is not subscribed", async () => {
      mockApiObj.listIssueSubscribers.mockResolvedValue([]);
      renderIssueDetail();

      expect(await screen.findByText("Subscribe")).toBeTruthy();
    });
  });

  // Same cold-cache hazard as the Subscribe button, but worse to act on. Every
  // checkbox here is drawn from the subscribers list, so an unresolved query
  // renders everyone — including people who ARE subscribed — as unchecked.
  // Clicking one of those rows sends an explicit subscribe, which rewrites the
  // target's reason to 'manual' and clears any opt-out scope
  // (server/pkg/db/queries/subscriber.sql), discarding a delegated
  // subscription or a deliberate opt-out (MUL-5714).
  describe("subscriber picker before the query resolves", () => {
    // The picker sits next to the subscribe control in the Activity header.
    // Anchor on the heading, not on that control — the whole point of these
    // cases is that it is not rendered yet.
    async function openSubscriberPicker() {
      const heading = await screen.findByText("Activity");
      const header = heading.parentElement?.parentElement;
      const trigger = header?.querySelector('[data-slot="popover-trigger"]');
      if (!trigger) throw new Error("subscriber picker trigger not found");
      fireEvent.click(trigger);
      return waitFor(() => {
        const content = document.querySelector('[data-slot="popover-content"]');
        if (!content) throw new Error("picker did not open");
        return content;
      });
    }

    function memberRow(content: Element) {
      const row = Array.from(
        content.querySelectorAll('[data-slot="command-item"]'),
      ).find((el) => el.textContent?.includes("Test User"));
      if (!row) throw new Error("member row not found");
      return row;
    }

    it("disables the rows while subscribers are loading", async () => {
      mockApiObj.listIssueSubscribers.mockReturnValue(new Promise(() => {}));
      renderIssueDetail();

      const row = memberRow(await openSubscriberPicker());

      expect(row.getAttribute("data-disabled")).toBe("true");

      fireEvent.click(row);

      expect(mockApiObj.subscribeToIssue).not.toHaveBeenCalled();
      expect(mockApiObj.unsubscribeFromIssue).not.toHaveBeenCalled();
    });

    it("disables the rows when the subscribers query failed", async () => {
      mockApiObj.listIssueSubscribers.mockRejectedValue(new Error("boom"));
      renderIssueDetail();

      const row = memberRow(await openSubscriberPicker());

      expect(row.getAttribute("data-disabled")).toBe("true");

      fireEvent.click(row);

      expect(mockApiObj.subscribeToIssue).not.toHaveBeenCalled();
      expect(mockApiObj.unsubscribeFromIssue).not.toHaveBeenCalled();
    });

    it("enables the rows once the query has a real answer", async () => {
      mockApiObj.listIssueSubscribers.mockResolvedValue([]);
      renderIssueDetail();
      // Wait for the resolved state before opening, so this is not just the
      // pending case passing by accident.
      await screen.findByText("Subscribe");

      const row = memberRow(await openSubscriberPicker());

      expect(row.getAttribute("data-disabled")).not.toBe("true");

      fireEvent.click(row);

      await waitFor(() =>
        expect(mockApiObj.subscribeToIssue).toHaveBeenCalledWith(
          "issue-1",
          "user-1",
          "member",
        ),
      );
    });
  });
});

describe("groupSubIssuesByStage", () => {
  const child = (id: string, stage: number | null): Issue => ({
    ...mockIssue,
    id,
    parent_issue_id: "parent-1",
    stage,
  });

  it("returns a single null-stage group when nothing is staged", () => {
    const groups = groupSubIssuesByStage([child("a", null), child("b", null)]);
    expect(groups).toHaveLength(1);
    expect(groups[0]?.stage).toBeNull();
    expect(groups[0]?.items.map((i) => i.id)).toEqual(["a", "b"]);
  });

  it("orders staged groups ascending with the unstaged group last", () => {
    const groups = groupSubIssuesByStage([
      child("s2", 2),
      child("u", null),
      child("s1a", 1),
      child("s1b", 1),
    ]);
    expect(groups.map((g) => g.stage)).toEqual([1, 2, null]);
    expect(groups[0]?.items.map((i) => i.id)).toEqual(["s1a", "s1b"]);
    expect(groups[2]?.items.map((i) => i.id)).toEqual(["u"]);
  });

  it("omits the unstaged group when every child is staged", () => {
    const groups = groupSubIssuesByStage([child("s1", 1), child("s2", 2)]);
    expect(groups.map((g) => g.stage)).toEqual([1, 2]);
  });
});
