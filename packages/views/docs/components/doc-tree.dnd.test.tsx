import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import { docKeys } from "@multica/core/docs";
import { DocTree } from "./doc-tree";
import enDocs from "../../locales/en/docs.json";
import enEditor from "../../locales/en/editor.json";
import enIssues from "../../locales/en/issues.json";

// Real drag events through the tree: a refused drop must not bubble to the
// top-level drop zone and move the item to the root (PR #10926 review).
const apiMock = vi.hoisted(() => ({
  listDocTree: vi.fn(), moveDoc: vi.fn(), moveDocFolder: vi.fn(),
  searchIssues: vi.fn(), searchProjects: vi.fn(),
}));
vi.mock("@multica/core/api", () => ({ api: apiMock }));
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
  }),
}));
vi.mock("../../navigation", () => ({
  useNavigation: () => ({ pathname: "/review/docs", push: vi.fn() }),
  AppLink: ({ children, href }: { children: React.ReactNode; href: string }) => <a href={href}>{children}</a>,
}));
vi.mock("./use-prefetch-doc", () => ({ usePrefetchDoc: () => () => ({}), useWarmDocPage: () => undefined }));
vi.mock("@multica/core/issue-statuses/hooks", () => ({
  useIssueStatuses: () => ({ iconOf: () => null, colorOf: () => null }),
}));
vi.mock("../../common/actor-avatar", () => ({ ActorAvatar: () => null }));

const tree = {
  folders: [
    { id: "parent", title: "Parent", parentId: null },
    { id: "child", title: "Child", parentId: "parent" },
    { id: "grandchild", title: "Grandchild", parentId: "child" },
    { id: "other", title: "Other", parentId: null },
  ],
  docs: [{ id: "runbook", title: "Release runbook", folderId: "child", updatedAt: "2026-11-18T00:00:00Z" }],
};
let qc: QueryClient;
beforeEach(() => {
  vi.clearAllMocks();
  qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  apiMock.listDocTree.mockResolvedValue(tree);
  apiMock.moveDoc.mockResolvedValue({});
  apiMock.moveDocFolder.mockResolvedValue({});
  apiMock.searchIssues.mockResolvedValue({ issues: [] });
  apiMock.searchProjects.mockResolvedValue({ projects: [] });
});
afterEach(() => { cleanup(); qc.clear(); });

function mount(children: React.ReactNode) {
  return render(<QueryClientProvider client={qc}><I18nProvider locale="en" resources={{ en: { docs: enDocs, editor: enEditor, issues: enIssues } }}>{children}</I18nProvider></QueryClientProvider>);
}
function folder(title: string) { return screen.getByText(title, { exact: true }).closest("[data-doc-folder]")!; }
function dragDrop(source: Element, target: Element) {
  const dataTransfer = { setData: vi.fn(), effectAllowed: "", dropEffect: "" };
  fireEvent.dragStart(source, { dataTransfer });
  fireEvent.dragOver(target, { dataTransfer });
  fireEvent.drop(target, { dataTransfer });
  fireEvent.dragEnd(source, { dataTransfer });
}

it("control: a doc dropped on another folder moves to that folder", async () => {
  qc.setQueryData(docKeys.tree("review-ws"), tree);
  mount(<DocTree />);
  dragDrop(screen.getByRole("link", { name: "Release runbook" }), folder("Other"));
  await waitFor(() => expect(apiMock.moveDoc).toHaveBeenCalledWith("runbook", "other"));
});

it("dropping a doc on its current folder must not move it to the root", async () => {
  qc.setQueryData(docKeys.tree("review-ws"), tree);
  mount(<DocTree />);
  dragDrop(screen.getByRole("link", { name: "Release runbook" }), folder("Child"));
  await act(async () => { await new Promise((resolve) => setTimeout(resolve, 20)); });
  expect(apiMock.moveDoc).not.toHaveBeenCalled();
});

it("dropping a nested folder on its descendant must not move the subtree to the root", async () => {
  qc.setQueryData(docKeys.tree("review-ws"), tree);
  mount(<DocTree />);
  dragDrop(folder("Child"), folder("Grandchild"));
  await act(async () => { await new Promise((resolve) => setTimeout(resolve, 20)); });
  expect(apiMock.moveDocFolder).not.toHaveBeenCalled();
});
