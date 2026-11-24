import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import { configStore } from "@multica/core/config";
import { docKeys, useDocDraftStore } from "@multica/core/docs";
import { DOCS_V1_FLAG } from "@multica/core/feature-flags";
import type { Doc } from "@multica/core/types";
import { DocPage } from "./doc-page";
import enDocs from "../../locales/en/docs.json";
import enEditor from "../../locales/en/editor.json";
import enIssues from "../../locales/en/issues.json";

// A doc opened from a hover has its live answer on hand, and its editor takes
// that answer as it starts. The page must not read that as "not known yet"
// on its next render and drop the editor for the skeleton.
const { apiMock, live } = vi.hoisted(() => ({
  apiMock: {
    getDoc: vi.fn(), updateDoc: vi.fn(), listDocTree: vi.fn(),
    listDocComments: vi.fn(), listMembers: vi.fn(), listAgents: vi.fn(),
  },
  live: { answer: undefined as unknown },
}));
vi.mock("@multica/core/api", () => ({ api: apiMock, ApiError: class extends Error {} }));
vi.mock("@multica/core/hooks", async (original) => ({
  ...await original<object>(), useWorkspaceId: () => "review-ws",
}));
vi.mock("@multica/core/platform", async (original) => ({
  ...await original<object>(), getCurrentWsId: () => "review-ws",
}));
vi.mock("@multica/core/auth", () => ({
  useAuthStore: Object.assign(vi.fn(), { getState: () => ({ user: { id: "reviewer" } }) }),
}));
vi.mock("@multica/core/paths", async (original) => ({
  ...await original<object>(), useWorkspacePaths: () => ({
    docs: () => "/review/docs", docDetail: (id: string) => `/review/docs/${id}`,
    docFolder: (id: string) => `/review/docs/folders/${id}`,
    docHistory: (id: string) => `/review/docs/${id}/history`,
  }),
}));
vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({ getActorName: () => "Reviewer" }),
}));
vi.mock("../../navigation", () => ({
  useNavigation: () => ({ pathname: "/review/docs/runbook", push: vi.fn(), getShareableUrl: (path: string) => path, searchParams: new URLSearchParams() }),
  AppLink: ({ children, href, className }: { children: React.ReactNode; href: string; className?: string }) => (
    <a href={href} className={className}>{children}</a>
  ),
}));
vi.mock("../../editor", () => ({
  ContentEditor: ({ value, onUpdate }: { value: string; onUpdate: (value: string) => void }) => (
    <textarea aria-label="Body" value={value} onChange={(event) => onUpdate(event.target.value)} />
  ),
  ReadonlyContent: ({ content }: { content: string }) => <div>{content}</div>,
  useEditorUpload: () => ({ uploadWithToast: vi.fn() }),
}));
// The editor's session takes the prefetched answer; the page then edits solo
// (as after a connect timeout), which renders the plain body.
vi.mock("./use-doc-collab", () => ({
  useDocCollab: () => {
    live.answer = undefined;
    return { mode: "solo" };
  },
}));
vi.mock("@multica/core/realtime", () => ({ useWS: () => ({ collabUrl: "ws://collab.test/collab" }) }));
vi.mock("./doc-live-session", () => ({
  liveEditingOff: new Set(),
  peekDocLive: () => live.answer,
  findDocSession: () => undefined,
  prefetchDocLive: () => new Promise(() => {}),
}));
vi.mock("../../common/actor-avatar", () => ({ ActorAvatar: () => null }));
vi.mock("../../layout/breadcrumb-header", () => ({
  BreadcrumbHeader: ({ actions }: { actions?: React.ReactNode }) => <header>{actions}</header>,
}));

const doc: Doc = {
  id: "runbook",
  workspaceId: "review-ws",
  folderId: null,
  title: "Release runbook",
  content: "Steps",
  revision: 1,
  versionCount: 1,
  createdByType: "member",
  createdById: "reviewer",
  updatedByType: "member",
  updatedById: "reviewer",
  createdAt: "2026-11-18T00:00:00Z",
  updatedAt: "2026-11-18T00:00:00Z",
};
let qc: QueryClient;
beforeEach(() => {
  vi.clearAllMocks();
  configStore.getState().setFeatureFlags({ [DOCS_V1_FLAG]: true });
  useDocDraftStore.getState().clearDraft();
  qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  live.answer = { enabled: true, token: "t", document: "doc:runbook", expiresAt: "" };
  apiMock.getDoc.mockResolvedValue(doc);
  apiMock.listDocTree.mockResolvedValue({ folders: [], docs: [doc] });
  apiMock.listDocComments.mockResolvedValue([]);
  apiMock.listMembers.mockResolvedValue([]);
  apiMock.listAgents.mockResolvedValue([]);
});
afterEach(() => { cleanup(); qc.clear(); });

function mount() {
  return render(<QueryClientProvider client={qc}><I18nProvider locale="en" resources={{ en: { docs: enDocs, editor: enEditor, issues: enIssues } }}><DocPage docId="runbook" /></I18nProvider></QueryClientProvider>);
}

it("keeps the editor through a refetch after its session took the prefetched answer", async () => {
  mount();
  const body = await screen.findByLabelText("Body");
  // Someone else's edit: the realtime event refetches the doc.
  apiMock.getDoc.mockResolvedValue({ ...doc, revision: 2, updatedAt: "2026-11-18T01:00:00Z" });
  await act(async () => { await qc.invalidateQueries({ queryKey: docKeys.all("review-ws") }); });
  expect(screen.getByLabelText("Body")).toBe(body);
});

it("keeps the editor when a save updates the doc", async () => {
  apiMock.updateDoc.mockImplementation(async (_id: string, { content }: { content?: string }) => {
    const saved = { ...doc, content: content ?? doc.content, revision: 2 };
    apiMock.getDoc.mockResolvedValue(saved);
    return saved;
  });
  mount();
  const body = await screen.findByLabelText("Body");
  fireEvent.change(body, { target: { value: "Steps, edited" } });
  await waitFor(() => expect(apiMock.updateDoc).toHaveBeenCalled(), { timeout: 4000 });
  await waitFor(() => expect(qc.getQueryData<Doc>(docKeys.detail("review-ws", "runbook"))?.revision).toBe(2));
  expect(screen.getByLabelText("Body")).toBe(body);
});
