/**
 * Shared mock harness for IssueDetail suites (OL-270): editor, virtuoso, API,
 * store and i18n mocks plus render helpers. Mock state and fixtures live in
 * ./issue-detail.test-harness-state (plain module scope — `vi.hoisted` results can't
 * be exported); this module imports it FIRST so the `vi.mock` factories below
 * see initialized bindings when they run lazily.
 *
 * Import this module BEFORE `./issue-detail` in any test file: the `vi.mock`
 * registrations here must be in place when the component under test
 * evaluates. Used by `issue-detail.test.tsx` and the platform deep-link
 * integration suites in apps/web + apps/desktop.
 */
import {
  archiveSectionProps,
  contentEditorMounts,
  descriptionSelectionAction,
  emptyDraftAttachments,
  mockApiObj,
  mockAuthUser,
  mockOpenModal,
  mockRecordVisit,
  mockViewport,
  readonlyContentRenders,
  scrollIntoViewSpy,
  scrollToIndexSpy,
  TEST_RESOURCES,
  mockTimeline,
} from "./issue-detail.test-harness-state";

export * from "./issue-detail.test-harness-state";

import { forwardRef, useEffect, useRef, useState, useImperativeHandle } from "react";
import { beforeEach, vi } from "vitest";
import { render, type RenderResult } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { IssueStatusEntry } from "@multica/core/types";
import { issueStatusKeys } from "@multica/core/issue-statuses";
import { I18nProvider } from "@multica/core/i18n/react";
import { useResolvedExpandStore } from "@multica/core/issues/stores/resolved-expand-store";
import { useCommentCollapseStore } from "@multica/core/issues/stores";

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

vi.mock("../../you-should-know/archive/archive-section", () => ({
  YouShouldKnowArchiveSection: (props: unknown) => {
    archiveSectionProps.current = props;
    return null;
  },
}));

vi.mock("./issue-dependencies-section", () => ({
  IssueDependenciesSection: ({ issue }: { issue: { id: string } }) => (
    <div data-testid="issue-dependencies-section" data-issue-id={issue.id} />
  ),
}));

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
    queryFn: () => mockApiObj.listAgents(),
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

// Injectable navigation: the provider passes the adapter value through into a
// context the tests can feed with a REAL platform adapter (the web/desktop
// deep-link integration suites); without one, views fall back to an inert
// stub like before.
const testNavigation = vi.hoisted(() => ({ Ctx: null as unknown as React.Context<any> }));
vi.mock("../../navigation", async () => {
  const React = await vi.importActual<typeof import("react")>("react");
  const Ctx = React.createContext<any>(null);
  testNavigation.Ctx = Ctx;
  return {
    AppLink: ({ children, href, ...props }: any) => (
      <a href={href} {...props}>
        {children}
      </a>
    ),
    useNavigation: () =>
      React.useContext(Ctx) ?? {
        push: vi.fn(),
        pathname: "/issues/issue-1",
        getShareableUrl: (p: string) => `https://app.multica.com${p}`,
      },
    useBackOrReplace: () => vi.fn(),
    NavigationProvider: ({ value, children }: { value: unknown; children: React.ReactNode }) =>
      React.createElement(Ctx.Provider, { value }, children),
  };
});

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
  // Pass-through: the detail page wraps its column in the preview-sequence
  // provider, but paging between files is covered in
  // preview-sequence-context.test.tsx against the real provider.
  PreviewSequenceProvider: ({ children }: { children: React.ReactNode }) =>
    children,
  usePreviewSequence: () => ({ openAt: () => false }),
  collectPreviewSequence: () => [],
  // Comment attachments render as their filename — the viewer and file cards
  // have their own suites.
  AttachmentDownloadProvider: ({ children }: { children: React.ReactNode }) =>
    children,
  Attachment: ({ attachment }: { attachment: { filename?: string } }) => (
    <span data-testid="comment-attachment">{attachment.filename}</span>
  ),
  isPreviewable: () => false,
  ReadonlyContent: ({ content }: { content: string }) => {
    readonlyContentRenders.push(content);
    return <div data-testid="readonly-content">{content}</div>;
  },
  ContentEditor: forwardRef(function MockContentEditor(
    {
      defaultValue,
      value: syncedValue,
      onUpdate,
      placeholder,
      flushPendingOnUnmount,
      onReady,
      selectionAction,
    }: any,
    ref: any,
  ) {
    const initialValue = syncedValue ?? defaultValue ?? "";
    if (syncedValue !== undefined) descriptionSelectionAction.current = selectionAction;
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

vi.mock("@multica/core/api", () => ({
  api: mockApiObj,
  getApi: () => mockApiObj,
  setApiInstance: vi.fn(),
  errorCode: (error: unknown) =>
    typeof error === "object" && error !== null && "body" in error
      ? (error as { body?: { code?: string } }).body?.code
      : undefined,
}));

// Mock recent issues store + reactive in-memory stand-ins; real stores where
// the behavior under test runs through them.
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
  // Reactive in-memory stand-in: a quick-jump to a reply reopens a thread the
  // reader collapsed, so the card has to re-render when `toggle` runs.
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
    {
      getState: () => ({ sticky: true, runningAgentReply: "steer", toggleSticky: () => {} }),
    },
  ),
}));

// Mock react-virtuoso: jsdom has no real layout, so the real Virtuoso would
// compute a 0-height viewport and render nothing. The mock renders every item
// inline so id="comment-..." nodes are available to the exact-anchor effect.
vi.mock("react-virtuoso", () => ({
  Virtuoso: forwardRef(function MockVirtuoso(
    { data, itemContent, computeItemKey }: {
      data: unknown[];
      itemContent: (i: number, item: unknown) => unknown;
      computeItemKey: (i: number, item: unknown) => React.Key;
    },
    ref: any,
  ) {
    useImperativeHandle(ref, () => ({
      scrollIntoView: vi.fn(),
      scrollToIndex: scrollToIndexSpy,
    }));
    return (
      <div data-testid="virtuoso-mock">
        {data.map((item, i) => (
          <div key={computeItemKey(i, item)}>{itemContent(i, item) as React.ReactElement}</div>
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
  useCommentCollapseStore.setState({ collapsedByIssue: {} });
});

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
// Component under test — imported after every vi.mock above is registered.
// ---------------------------------------------------------------------------

import { IssueDetail } from "./issue-detail";

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

export function createTestQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: 0 },
      mutations: { retry: false },
    },
  });
}

export function renderIssueDetail(issueId = "issue-1"): RenderResult {
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
export function renderIssueDetailWithStatusCatalog(
  entries: IssueStatusEntry[],
  issueId = "issue-1",
): RenderResult {
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

export function renderIssueDetailWithHighlight(
  highlightCommentId: string,
  issueId = "issue-1",
  options: { seedTimeline?: boolean } = {},
): RenderResult & { queryClient: QueryClient } {
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

export const highlightedCommentBackgroundClass =
  "bg-[color-mix(in_srgb,var(--card)_95%,var(--brand)_5%)]";

export function hasHighlightedCommentBackground(root: ParentNode | null): boolean {
  if (!root) return false;

  const elements = root instanceof Element
    ? [root, ...Array.from(root.querySelectorAll("[class]"))]
    : Array.from(root.querySelectorAll("[class]"));

  return elements.some(
    (el) => typeof el.className === "string" && el.className.includes(highlightedCommentBackgroundClass),
  );
}
