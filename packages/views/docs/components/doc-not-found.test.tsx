import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import { configStore } from "@multica/core/config";
import { docKeys, useDocDraftStore } from "@multica/core/docs";
import { DOCS_V1_FLAG } from "@multica/core/feature-flags";
import type { Doc } from "@multica/core/types";
import { DocPage } from "./doc-page";
import { DocHistoryPage } from "./doc-history-page";
import enDocs from "../../locales/en/docs.json";
import enEditor from "../../locales/en/editor.json";
import enIssues from "../../locales/en/issues.json";

// A doc that no longer exists must read as gone. Retrying a 404 never helps,
// and an editor left open on a deleted doc can only fail to save.
const { apiMock, ApiError } = vi.hoisted(() => {
  class ApiError extends Error {
    constructor(
      message: string,
      public status: number,
      public statusText = "",
      public body?: unknown,
    ) {
      super(message);
    }
  }
  return {
    ApiError,
    apiMock: {
      getDoc: vi.fn(), updateDoc: vi.fn(), listDocTree: vi.fn(),
      listDocVersions: vi.fn(), getDocVersion: vi.fn(),
      listMembers: vi.fn(), listAgents: vi.fn(),
    },
  };
});
vi.mock("@multica/core/api", () => ({ api: apiMock, ApiError }));
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
  useNavigation: () => ({ pathname: "/review/docs/runbook", push: vi.fn(), getShareableUrl: (path: string) => path }),
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
const gone = () => new ApiError("doc not found", 404);
let qc: QueryClient;
beforeEach(() => {
  vi.clearAllMocks();
  configStore.getState().setFeatureFlags({ [DOCS_V1_FLAG]: true });
  useDocDraftStore.getState().clearDraft();
  qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  apiMock.listDocTree.mockResolvedValue({ folders: [], docs: [] });
  apiMock.listMembers.mockResolvedValue([]);
  apiMock.listAgents.mockResolvedValue([]);
});
afterEach(() => { cleanup(); qc.clear(); });

function mount(children: React.ReactNode) {
  return render(<QueryClientProvider client={qc}><I18nProvider locale="en" resources={{ en: { docs: enDocs, editor: enEditor, issues: enIssues } }}>{children}</I18nProvider></QueryClientProvider>);
}

it("shows a doc that does not exist as gone, with the way back, not a Retry", async () => {
  apiMock.getDoc.mockRejectedValue(gone());
  mount(<DocPage docId="runbook" />);
  expect(await screen.findByText("Doc not found")).toBeTruthy();
  expect(screen.queryByRole("button", { name: "Retry" })).toBeNull();
  expect(screen.getByRole("link", { name: "Docs" }).getAttribute("href")).toBe("/review/docs");
});

it("control: a load that failed for another reason still offers Retry", async () => {
  apiMock.getDoc.mockRejectedValue(new ApiError("boom", 500));
  mount(<DocPage docId="runbook" />);
  expect(await screen.findByRole("button", { name: "Retry" })).toBeTruthy();
  expect(screen.queryByText("Doc not found")).toBeNull();
});

it("replaces the open editor when the doc is deleted elsewhere and keeps the unsaved text readable", async () => {
  apiMock.getDoc.mockResolvedValue(doc);
  mount(<DocPage docId="runbook" />);
  fireEvent.change(await screen.findByLabelText("Body"), { target: { value: "Steps and unsaved words" } });
  // Someone else deletes it: the realtime event refetches every doc query.
  apiMock.getDoc.mockRejectedValue(gone());
  apiMock.updateDoc.mockRejectedValue(gone());
  await act(async () => { await qc.invalidateQueries({ queryKey: docKeys.all("review-ws") }); });
  expect(await screen.findByText("Doc not found")).toBeTruthy();
  expect(screen.queryByLabelText("Body")).toBeNull();
  expect((screen.getByLabelText("Your saved draft") as HTMLTextAreaElement).value).toBe("Release runbook\n\nSteps and unsaved words");
});

it("a save answered with not found turns the editor into the gone state", async () => {
  apiMock.getDoc.mockResolvedValueOnce(doc).mockRejectedValue(gone());
  apiMock.updateDoc.mockRejectedValue(gone());
  mount(<DocPage docId="runbook" />);
  fireEvent.change(await screen.findByLabelText("Body"), { target: { value: "More" } });
  await waitFor(() => expect(apiMock.updateDoc).toHaveBeenCalled(), { timeout: 4000 });
  expect(await screen.findByText("Doc not found")).toBeTruthy();
  expect(screen.queryByRole("button", { name: "Not saved · Retry" })).toBeNull();
});

it("keeps the conflict backup and newer unsaved edits readable after deletion", async () => {
  const latest = { ...doc, content: "Steps from another editor", revision: 2 };
  apiMock.getDoc.mockResolvedValueOnce(doc).mockResolvedValue(latest);
  apiMock.updateDoc.mockRejectedValue(
    new ApiError("Changed", 409, "Conflict", { code: "revision_conflict" }),
  );
  mount(<DocPage docId="runbook" />);

  const body = await screen.findByLabelText<HTMLTextAreaElement>("Body");
  fireEvent.change(body, { target: { value: "Edit before the conflict" } });
  fireEvent.click(
    await screen.findByRole("button", { name: "Reload latest" }, { timeout: 4000 }),
  );
  await waitFor(() => expect(body.value).toBe(latest.content));
  fireEvent.change(body, { target: { value: "Edit after reloading" } });

  apiMock.getDoc.mockRejectedValue(gone());
  apiMock.updateDoc.mockRejectedValue(gone());
  await act(async () => {
    await qc.invalidateQueries({ queryKey: docKeys.all("review-ws") });
  });

  await screen.findByText("Doc not found");
  const backup = screen.getByLabelText<HTMLTextAreaElement>("Your saved draft");
  expect(backup.value).toBe("Release runbook\n\nEdit before the conflict");
  expect(backup.readOnly).toBe(true);
  const current = screen.getByLabelText<HTMLTextAreaElement>("Unsaved changes");
  expect(current.value).toBe("Release runbook\n\nEdit after reloading");
  expect(current.readOnly).toBe(true);
});

it("a gone doc without unsaved text shows no draft", async () => {
  apiMock.getDoc.mockRejectedValue(gone());
  mount(<DocPage docId="runbook" />);
  await screen.findByText("Doc not found");
  expect(screen.queryByLabelText("Your saved draft")).toBeNull();
});

it("the history of a doc that does not exist shows it as gone", async () => {
  apiMock.getDoc.mockRejectedValue(gone());
  apiMock.listDocVersions.mockRejectedValue(gone());
  mount(<DocHistoryPage docId="runbook" />);
  expect(await screen.findByText("Doc not found")).toBeTruthy();
  expect(screen.queryByRole("button", { name: "Retry" })).toBeNull();
});
