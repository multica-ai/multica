// @vitest-environment jsdom

import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enDocs from "../../locales/en/docs.json";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";
import { DocHistoryPage } from "./doc-history-page";

const api = vi.hoisted(() => ({
  getDoc: vi.fn(),
  listDocVersions: vi.fn(),
  getDocVersion: vi.fn(),
  updateDoc: vi.fn(),
  listDocTree: vi.fn(),
}));
const toast = vi.hoisted(() => Object.assign(vi.fn(), { success: vi.fn(), error: vi.fn() }));

vi.mock("@multica/core/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/api")>()),
  api,
}));
vi.mock("sonner", () => ({ toast }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/paths", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/paths")>()),
  useWorkspacePaths: () => ({
    docs: () => "/acme/docs",
    docDetail: (id: string) => `/acme/docs/${id}`,
    docFolder: (id: string) => `/acme/docs/folders/${id}`,
    docHistory: (id: string) => `/acme/docs/${id}/history`,
  }),
}));
vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({ getActorName: () => "Dev" }),
}));
vi.mock("../../common/actor-avatar", () => ({ ActorAvatar: () => null }));
vi.mock("../../editor", () => ({
  ReadonlyContent: ({ content }: { content: string }) => <div>{content}</div>,
}));

type Version = { number: number; title: string; content: string };

function setup(versions: Version[], current: { title: string; content: string }) {
  const latest = versions.length;
  const doc = {
    id: "d1",
    workspace_id: "ws-1",
    folder_id: null,
    format: "markdown",
    revision: 7,
    version_count: latest,
    unread: false,
    created_by_type: "member",
    created_by_id: "u1",
    updated_by_type: "member",
    updated_by_id: "u1",
    created_at: "2026-10-01T00:00:00Z",
    updated_at: "2026-10-01T00:00:00Z",
    project_ids: [],
    last_seen_at: null,
    ...current,
  };
  const meta = (v: Version) => ({
    doc_id: "d1",
    number: v.number,
    title: v.title,
    author_type: "member",
    author_id: "u1",
    message: "",
    action: v.number === 1 ? "create" : "edit",
    action_ref: null,
    task_id: null,
    issue_id: null,
    lines_added: 1,
    lines_removed: 0,
    created_at: "2026-10-01T00:00:00Z",
    updated_at: "2026-10-01T00:00:00Z",
  });
  api.getDoc.mockResolvedValue(doc);
  api.listDocTree.mockResolvedValue({ folders: [], docs: [] });
  api.listDocVersions.mockResolvedValue([...versions].reverse().map(meta));
  api.getDocVersion.mockImplementation(async (_id: string, number: number) => {
    const v = versions.find((x) => x.number === number);
    return v ? { ...meta(v), content: v.content } : null;
  });
  return { doc };
}

function renderPage() {
  const navigation: NavigationAdapter = {
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    pathname: "/acme/docs/d1/history",
    searchParams: new URLSearchParams(),
    hash: "",
    getShareableUrl: (path) => path,
  };
  render(
    <I18nProvider locale="en" resources={{ en: { common: enCommon, docs: enDocs } }}>
      <NavigationProvider value={navigation}>
        <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
          <DocHistoryPage docId="d1" />
        </QueryClientProvider>
      </NavigationProvider>
    </I18nProvider>,
  );
}

beforeEach(() => {
  vi.clearAllMocks();
});

describe("DocHistoryPage restore", () => {
  it("says nothing changed when the restored version matches the doc, and keeps the diff", async () => {
    const { doc } = setup(
      [
        { number: 1, title: "Plan", content: "same" },
        { number: 2, title: "Plan", content: "different" },
        { number: 3, title: "Plan", content: "same" },
      ],
      { title: "Plan", content: "same" },
    );
    api.updateDoc.mockResolvedValue(doc);
    renderPage();

    await userEvent.click(await screen.findByRole("button", { name: /v1/ }));
    await userEvent.click(await screen.findByRole("button", { name: "Restore v1" }));

    await waitFor(() => expect(toast).toHaveBeenCalledWith("Nothing to change — the doc already matches"));
    expect(toast.success).not.toHaveBeenCalled();
    expect(screen.getByText("Changes in v1")).toBeTruthy();
    expect(api.getDocVersion).not.toHaveBeenCalledWith("d1", 4);
  });

  it("jumps to the version the server actually wrote", async () => {
    const { doc } = setup(
      [
        { number: 1, title: "Plan", content: "first" },
        { number: 2, title: "Plan", content: "second" },
      ],
      { title: "Plan", content: "second" },
    );
    api.updateDoc.mockImplementation(async () => {
      api.getDocVersion.mockImplementation(async (_id: string, number: number) => ({
        doc_id: "d1",
        number,
        title: "Plan",
        content: number === 3 ? "first" : "second",
      }));
      return { ...doc, revision: 8, version_count: 3, content: "first" };
    });
    renderPage();

    await userEvent.click(await screen.findByRole("button", { name: /v1/ }));
    await userEvent.click(await screen.findByRole("button", { name: "Restore v1" }));

    expect(await screen.findByText("Changes in v3")).toBeTruthy();
    expect(await screen.findByText("first")).toBeTruthy();
  });
});

describe("DocHistoryPage undo", () => {
  it("undoes a rename-only version by restoring the old title", async () => {
    const { doc } = setup(
      [
        { number: 1, title: "Old name", content: "body" },
        { number: 2, title: "New name", content: "body" },
      ],
      { title: "New name", content: "body" },
    );
    api.updateDoc.mockResolvedValue({ ...doc, version_count: 3, title: "Old name" });
    renderPage();

    expect(await screen.findByText("Title")).toBeTruthy();
    await userEvent.click(screen.getByRole("button", { name: "Undo this change" }));

    await waitFor(() => expect(api.updateDoc).toHaveBeenCalled());
    const [, body] = api.updateDoc.mock.calls[0]!;
    expect(body).toMatchObject({ title: "Old name", action: "revert", action_ref: 2 });
    expect(body).not.toHaveProperty("content");
  });

  it("undoes the title and body of a version that changed both", async () => {
    const { doc } = setup(
      [
        { number: 1, title: "Old", content: "a\nb" },
        { number: 2, title: "New", content: "a\nB" },
        { number: 3, title: "New", content: "a\nB\nc" },
      ],
      { title: "New", content: "a\nB\nc" },
    );
    api.updateDoc.mockResolvedValue({ ...doc, version_count: 4 });
    renderPage();

    await userEvent.click(await screen.findByRole("button", { name: /v2/ }));
    await userEvent.click(await screen.findByRole("button", { name: "Undo this change" }));

    await waitFor(() => expect(api.updateDoc).toHaveBeenCalled());
    expect(api.updateDoc.mock.calls[0]![1]).toMatchObject({ title: "Old", content: "a\nb\nc" });
  });

  it("refuses to undo a rename that was renamed again later", async () => {
    setup(
      [
        { number: 1, title: "Old", content: "body" },
        { number: 2, title: "New", content: "body" },
        { number: 3, title: "Newer", content: "body" },
      ],
      { title: "Newer", content: "body" },
    );
    renderPage();

    await userEvent.click(await screen.findByRole("button", { name: /v2/ }));
    await userEvent.click(await screen.findByRole("button", { name: "Undo this change" }));

    await waitFor(() => expect(toast.error).toHaveBeenCalled());
    expect(api.updateDoc).not.toHaveBeenCalled();
  });
});

describe("DocHistoryPage loading", () => {
  it("shows a retry instead of a skeleton when a version fails to load", async () => {
    setup(
      [
        { number: 1, title: "Plan", content: "first" },
        { number: 2, title: "Plan", content: "second" },
      ],
      { title: "Plan", content: "second" },
    );
    api.getDocVersion.mockRejectedValueOnce(new Error("offline")).mockRejectedValueOnce(new Error("offline"));
    renderPage();

    expect(await screen.findByText("Couldn't load this version")).toBeTruthy();
    await userEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(await screen.findByText("second")).toBeTruthy();
  });
});
