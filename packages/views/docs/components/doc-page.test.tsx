// @vitest-environment jsdom

import { forwardRef, useImperativeHandle, useRef } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { act, fireEvent, render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enDocs from "../../locales/en/docs.json";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";
import { DocDetailPage } from "./doc-page";

const api = vi.hoisted(() => ({
  getDoc: vi.fn(),
  updateDoc: vi.fn(),
  markDocRead: vi.fn(),
  listDocTree: vi.fn(),
}));

vi.mock("@multica/core/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/api")>()),
  api,
}));
vi.mock("sonner", () => ({ toast: Object.assign(vi.fn(), { success: vi.fn(), error: vi.fn() }) }));
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
vi.mock("./doc-side-panel", () => ({ DocSidePanel: () => null }));
vi.mock("../../editor", () => ({
  ContentEditor: forwardRef(function FakeEditor(
    { defaultValue, onUpdate }: { defaultValue: string; onUpdate: (markdown: string) => void },
    ref,
  ) {
    const el = useRef<HTMLTextAreaElement>(null);
    useImperativeHandle(ref, () => ({
      getMarkdown: () => el.current?.value ?? "",
      flushPendingUpdate: () => {},
      adoptContent: (text: string) => {
        if (el.current) el.current.value = text;
      },
      focus: () => el.current?.focus(),
    }));
    return (
      <textarea aria-label="Body" ref={el} defaultValue={defaultValue} onChange={(e) => onUpdate(e.target.value)} />
    );
  }),
}));

const doc = {
  id: "d1",
  workspace_id: "ws-1",
  folder_id: null,
  format: "markdown",
  title: "Plan",
  content: "server text",
  revision: 1,
  version_count: 1,
  unread: false,
  created_by_type: "member",
  created_by_id: "u1",
  updated_by_type: "member",
  updated_by_id: "u1",
  created_at: "2026-10-01T00:00:00Z",
  updated_at: "2026-10-01T00:00:00Z",
  project_ids: [],
  last_seen_at: null,
};

function renderPage() {
  const navigation: NavigationAdapter = {
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    pathname: "/acme/docs/d1",
    searchParams: new URLSearchParams(),
    hash: "",
    getShareableUrl: (path) => path,
  };
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <I18nProvider locale="en" resources={{ en: { common: enCommon, docs: enDocs } }}>
      <NavigationProvider value={navigation}>
        <QueryClientProvider client={queryClient}>
          <DocDetailPage docId="d1" />
        </QueryClientProvider>
      </NavigationProvider>
    </I18nProvider>,
  );
  return { queryClient };
}

beforeEach(() => {
  vi.clearAllMocks();
  api.getDoc.mockResolvedValue(doc);
  api.markDocRead.mockResolvedValue(undefined);
  api.listDocTree.mockResolvedValue({ folders: [], docs: [] });
});

describe("DocDetailPage refresh failures", () => {
  it("keeps the editor and the unsaved draft when a background refresh fails", async () => {
    const { queryClient } = renderPage();
    const body = (await screen.findByLabelText("Body")) as HTMLTextAreaElement;

    api.updateDoc.mockRejectedValue(new Error("offline"));
    fireEvent.change(body, { target: { value: "my unsaved text" } });
    expect(await screen.findByText("Not saved · Retry")).toBeTruthy();

    api.getDoc.mockRejectedValue(new Error("offline"));
    await act(() => queryClient.refetchQueries({ queryKey: ["docs", "ws-1", "detail", "d1"] }).catch(() => {}));

    expect(await screen.findByText("Couldn't load the latest version. Your edits are kept here.")).toBeTruthy();
    expect((screen.getByLabelText("Body") as HTMLTextAreaElement).value).toBe("my unsaved text");
    expect(screen.queryByText("This doc doesn't exist or was deleted")).toBeNull();
    expect(screen.getByText("Not saved · Retry")).toBeTruthy();
  });
});
