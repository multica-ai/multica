// @vitest-environment jsdom

import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enDocs from "../../locales/en/docs.json";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";
import { ProjectDocsSection } from "./project-docs-section";

const api = vi.hoisted(() => ({
  listProjectDocs: vi.fn(),
  listDocTree: vi.fn(),
  linkProjectDoc: vi.fn(),
  unlinkProjectDoc: vi.fn(),
  createDoc: vi.fn(),
}));

vi.mock("@multica/core/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/api")>()),
  api,
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/paths", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/paths")>()),
  useWorkspacePaths: () => ({ docDetail: (id: string) => `/acme/docs/${id}` }),
}));

const doc = (id: string, title: string) => ({
  id,
  title,
  format: "markdown",
  folder_id: null,
  revision: 1,
  version_count: 1,
  unread: false,
  created_by: "u1",
  updated_by: "u1",
  created_at: "2026-10-01T00:00:00Z",
  updated_at: "2026-10-01T00:00:00Z",
});

function renderSection() {
  const push = vi.fn();
  const navigation: NavigationAdapter = {
    push,
    replace: vi.fn(),
    back: vi.fn(),
    pathname: "/acme/projects/p1",
    searchParams: new URLSearchParams(),
    hash: "",
    getShareableUrl: (path) => path,
  };
  render(
    <I18nProvider locale="en" resources={{ en: { common: enCommon, docs: enDocs } }}>
      <NavigationProvider value={navigation}>
        <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
          <ProjectDocsSection projectId="p1" />
        </QueryClientProvider>
      </NavigationProvider>
    </I18nProvider>,
  );
  return { push };
}

beforeEach(() => {
  vi.clearAllMocks();
  api.listProjectDocs.mockResolvedValue([doc("d1", "Launch plan")]);
  api.listDocTree.mockResolvedValue({ folders: [], docs: [doc("d1", "Launch plan"), doc("d2", "Pricing notes")] });
  api.linkProjectDoc.mockResolvedValue(undefined);
  api.unlinkProjectDoc.mockResolvedValue(undefined);
});

describe("ProjectDocsSection", () => {
  it("lists linked docs and offers only the unlinked ones", async () => {
    renderSection();
    expect((await screen.findByRole("link", { name: "Launch plan" })).getAttribute("href")).toBe("/acme/docs/d1");

    await userEvent.click(screen.getByRole("button", { name: "Link doc" }));
    expect(await screen.findByRole("menuitem", { name: "Pricing notes" })).toBeTruthy();
    expect(screen.queryByRole("menuitem", { name: "Launch plan" })).toBeNull();

    await userEvent.click(screen.getByRole("menuitem", { name: "Pricing notes" }));
    await waitFor(() => expect(api.linkProjectDoc).toHaveBeenCalledWith("p1", "d2"));
  });

  it("unlinks a doc", async () => {
    renderSection();
    await userEvent.click(await screen.findByRole("button", { name: "Unlink Launch plan" }));
    await waitFor(() => expect(api.unlinkProjectDoc).toHaveBeenCalledWith("p1", "d1"));
  });

  it("creates a doc already linked to the project, then opens it", async () => {
    api.createDoc.mockResolvedValue(doc("d3", "Untitled"));
    const { push } = renderSection();
    await userEvent.click(await screen.findByRole("button", { name: "New doc" }));
    await waitFor(() => expect(push).toHaveBeenCalledWith("/acme/docs/d3"));
    expect(api.linkProjectDoc).toHaveBeenCalledWith("p1", "d3");
  });
});
