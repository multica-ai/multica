import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import { configStore } from "@multica/core/config";
import { docKeys, useDocDraftStore } from "@multica/core/docs";
import { DOCS_V1_FLAG } from "@multica/core/feature-flags";
import type { Doc } from "@multica/core/types";
import { DocPage } from "./doc-page";
import { DocHistoryPage } from "./doc-history-page";
import { DocsHomePage } from "./docs-home-page";
import { MoveDialog } from "./move-dialog";
import enDocs from "../../locales/en/docs.json";
import enEditor from "../../locales/en/editor.json";
import enIssues from "../../locales/en/issues.json";

const apiMock = vi.hoisted(() => ({
  getDoc: vi.fn(),
  updateDoc: vi.fn(),
  listDocTree: vi.fn(),
  listDocVersions: vi.fn(),
  getDocVersion: vi.fn(),
  listDocComments: vi.fn(),
  listMembers: vi.fn(),
  listAgents: vi.fn(),
}));
vi.mock("@multica/core/api", () => ({
  api: apiMock,
  ApiError: class extends Error {},
}));
vi.mock("@multica/core/hooks", async (original) => ({
  ...(await original<object>()),
  useWorkspaceId: () => "ws",
}));
vi.mock("@multica/core/platform", async (original) => ({
  ...(await original<object>()),
  getCurrentWsId: () => "ws",
}));
vi.mock("@multica/core/auth", () => ({
  useAuthStore: Object.assign(vi.fn(), {
    getState: () => ({ user: { id: "me" } }),
  }),
}));
vi.mock("@multica/core/paths", async (original) => ({
  ...(await original<object>()),
  useWorkspacePaths: () => ({
    docs: () => "/ws/docs",
    docDetail: (id: string) => `/ws/docs/${id}`,
    docFolder: (id: string) => `/ws/docs/folders/${id}`,
    docHistory: (id: string) => `/ws/docs/${id}/history`,
  }),
}));
vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({ getActorName: () => "Me" }),
}));
vi.mock("../../navigation", () => ({
  useNavigation: () => ({
    pathname: "/ws/docs/long-doc",
    push: vi.fn(),
    getShareableUrl: (path: string) => path,
    searchParams: new URLSearchParams(),
  }),
  AppLink: ({
    children,
    href,
    className,
  }: {
    children: React.ReactNode;
    href: string;
    className?: string;
  }) => (
    <a href={href} className={className}>
      {children}
    </a>
  ),
}));
vi.mock("../../editor", () => ({
  ContentEditor: ({
    value,
    onUpdate,
  }: {
    value: string;
    onUpdate: (value: string) => void;
  }) => (
    <textarea
      aria-label="Body"
      value={value}
      onChange={(event) => onUpdate(event.target.value)}
    />
  ),
  ReadonlyContent: ({ content }: { content: string }) => <div>{content}</div>,
  useEditorUpload: () => ({ uploadWithToast: vi.fn() }),
}));
vi.mock("./use-doc-collab", () => ({
  useDocCollab: () => ({ mode: "solo" }),
}));
vi.mock("@multica/core/realtime", () => ({
  useWS: () => ({ collabUrl: "ws://collab.test/collab" }),
}));
vi.mock("./doc-live-session", () => ({
  liveEditingOff: new Set(["ws://collab.test/collab"]),
  peekDocLive: () => undefined,
  findDocSession: () => undefined,
  prefetchDocLive: () => Promise.resolve({ enabled: false }),
}));
vi.mock("./use-prefetch-doc", () => ({
  usePrefetchDoc: () => () => ({}),
  useWarmDocPage: () => undefined,
}));
vi.mock("../../common/actor-avatar", () => ({ ActorAvatar: () => null }));

const LONG_TITLE =
  "Multica 2026 Q4 跨团队知识库架构设计方案与实时协作编辑系统迁移路线图（含多端离线同步与权限模型评审记录）";
const LONG_FOLDER_TITLE =
  "产品研发与基础设施架构设计文档归档目录（2026 年度跨团队协作规范）";

const doc: Doc = {
  id: "long-doc",
  workspaceId: "ws",
  folderId: "long-folder",
  title: LONG_TITLE,
  content: "Body text",
  revision: 1,
  versionCount: 1,
  createdByType: "member",
  createdById: "me",
  updatedByType: "member",
  updatedById: "me",
  createdAt: "2026-11-21T00:00:00Z",
  updatedAt: "2026-11-21T00:00:00Z",
};

const tree = {
  folders: [
    {
      id: "long-folder",
      workspaceId: "ws",
      parentId: null,
      title: LONG_FOLDER_TITLE,
      createdByType: "member" as const,
      createdById: "me",
      createdAt: "2026-11-21T00:00:00Z",
      updatedAt: "2026-11-21T00:00:00Z",
    },
  ],
  docs: [doc],
};

let qc: QueryClient;

beforeEach(() => {
  vi.clearAllMocks();
  configStore.getState().setFeatureFlags({ [DOCS_V1_FLAG]: true });
  useDocDraftStore.getState().clearDraft();
  qc = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  apiMock.getDoc.mockResolvedValue(doc);
  apiMock.listDocTree.mockResolvedValue(tree);
  apiMock.listDocComments.mockResolvedValue([]);
  apiMock.listMembers.mockResolvedValue([]);
  apiMock.listAgents.mockResolvedValue([]);
  apiMock.listDocVersions.mockResolvedValue([
    {
      number: 1,
      authorType: "member",
      authorId: "me",
      contributors: [],
      message: "",
      updatedAt: "2026-11-21T00:00:00Z",
    },
  ]);
  apiMock.getDocVersion.mockResolvedValue({
    number: 1,
    title: LONG_TITLE,
    content: "Body text",
    updatedAt: "2026-11-21T00:00:00Z",
  });
});

afterEach(() => {
  cleanup();
  qc.clear();
});

function mount(children: React.ReactNode) {
  return render(
    <QueryClientProvider client={qc}>
      <I18nProvider
        locale="en"
        resources={{ en: { docs: enDocs, editor: enEditor, issues: enIssues } }}
      >
        {children}
      </I18nProvider>
    </QueryClientProvider>,
  );
}

it("renders the doc title as an auto-wrapping textarea that strips pasted newlines and blurs on Enter", async () => {
  mount(<DocPage docId="long-doc" />);

  const title = (await screen.findByRole("textbox", {
    name: "Title",
  })) as HTMLTextAreaElement;
  expect(title.tagName).toBe("TEXTAREA");
  expect(title.maxLength).toBe(300);
  expect(title.value).toBe(LONG_TITLE);
  expect(title.parentElement?.getAttribute("data-replicated-value")).toBe(
    LONG_TITLE,
  );

  // Pasted multi-line text is flattened to a single paragraph.
  fireEvent.change(title, { target: { value: "Line 1\n\nLine 2" } });
  expect(title.value).toBe("Line 1 Line 2");
  expect(title.parentElement?.getAttribute("data-replicated-value")).toBe(
    "Line 1 Line 2",
  );

  // Enter while composing (IME) must not blur; plain Enter blurs without inserting a newline.
  title.focus();
  fireEvent.keyDown(title, { key: "Enter", isComposing: true });
  expect(document.activeElement).toBe(title);

  fireEvent.keyDown(title, { key: "Enter" });
  expect(document.activeElement).not.toBe(title);

  // Breadcrumb ancestor folder segment truncates instead of staying shrink-0.
  const folderLink = screen.getByRole("link", { name: LONG_FOLDER_TITLE });
  expect(folderLink.className).toContain("truncate");
  expect(folderLink.className).toContain("min-w-0");
});

it("truncates the doc breadcrumb segment and wraps the heading on DocHistoryPage", async () => {
  mount(<DocHistoryPage docId="long-doc" />);

  const heading = await screen.findByRole("heading", { level: 1 });
  expect(heading.textContent).toBe(LONG_TITLE);
  expect(heading.className).toContain("min-w-0");
  expect(heading.className).toContain("wrap-anywhere");

  const docCrumb = screen.getByRole("link", { name: LONG_TITLE });
  expect(docCrumb.className).toContain("truncate");
  expect(docCrumb.className).toContain("min-w-0");

  const restoreBtn = screen.getByRole("button", { name: "Restore this version" });
  expect(restoreBtn.className).toContain("shrink-0");
});

it("constrains MoveDialog header and listbox width for long titles", () => {
  qc.setQueryData(docKeys.tree("ws"), tree);
  mount(
    <MoveDialog
      target={{ kind: "doc", id: "long-doc", title: LONG_TITLE }}
      onOpenChange={vi.fn()}
    />,
  );

  const title = screen.getByRole("heading", {
    name: `Move "${LONG_TITLE}"`,
  });
  expect(title.className).toContain("truncate");
  expect(title.className).toContain("min-w-0");
  expect(title.parentElement?.className).toContain("min-w-0");
  expect(title.parentElement?.className).toContain("pr-8");

  const listbox = screen.getByRole("listbox");
  expect(listbox.className).toContain("min-w-0");
});

it("truncates long folder headers and keeps list row icons un-squished on DocsHomePage", async () => {
  qc.setQueryData(docKeys.tree("ws"), tree);
  mount(<DocsHomePage folderId="long-folder" />);

  const headerHeading = await screen.findByRole("heading", {
    level: 1,
    name: LONG_FOLDER_TITLE,
  });
  expect(headerHeading.className).toContain("truncate");
  expect(headerHeading.className).toContain("min-w-0");

  const docLink = screen.getByRole("link", { name: LONG_TITLE });
  const icon = docLink.querySelector("svg");
  expect(icon?.getAttribute("class")).toContain("shrink-0");
});
