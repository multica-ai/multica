import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import { docKeys } from "@multica/core/docs";
import { workspaceKeys } from "@multica/core/workspace/queries";
import { DocTree } from "./doc-tree";
import enDocs from "../../locales/en/docs.json";

// The Docs panel is one of the shell's space panels: it shares their title
// band and nav rows (and the `nav-item` theme anchor) instead of its own.
const apiMock = vi.hoisted(() => ({ listDocTree: vi.fn(), listMembers: vi.fn(), listAgents: vi.fn() }));
vi.mock("@multica/core/api", () => ({ api: apiMock }));
vi.mock("@multica/core/hooks", async (original) => ({
  ...await original<object>(), useWorkspaceId: () => "ws",
}));
vi.mock("@multica/core/auth", () => ({
  useAuthStore: (select: (s: { user: { id: string } }) => unknown) => select({ user: { id: "me" } }),
}));
vi.mock("@multica/core/paths", async (original) => ({
  ...await original<object>(), useWorkspacePaths: () => ({
    docs: () => "/docs", docDetail: (id: string) => `/docs/${id}`, docFolder: (id: string) => `/docs/folders/${id}`,
  }),
}));
vi.mock("../../navigation", () => ({
  useNavigation: () => ({ pathname: "/docs/open", push: vi.fn() }),
  AppLink: ({ children, ...props }: React.ComponentProps<"a">) => <a {...props}>{children}</a>,
}));
vi.mock("./use-prefetch-doc", () => ({ usePrefetchDoc: () => () => ({}), useWarmDocPage: () => undefined }));

const doc = (id: string, title: string, folderId: string | null = null) => ({
  id, title, folderId, createdByType: "member", createdById: "me", updatedAt: "2026-11-18T00:00:00Z",
});

let qc: QueryClient;
beforeEach(() => {
  qc = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  qc.setQueryData(docKeys.tree("ws"), {
    folders: [{ id: "specs", title: "Specs", parentId: null }],
    docs: [doc("inside", "Inside", "specs"), doc("open", "Open"), doc("other", "Other")],
  });
  qc.setQueryData(workspaceKeys.agents("ws"), []);
  qc.setQueryData(workspaceKeys.members("ws"), [{ user_id: "me", role: "member" }]);
});
afterEach(() => { cleanup(); qc.clear(); });

it("renders the shared panel title and lights the open doc's nav row", () => {
  render(<QueryClientProvider client={qc}><I18nProvider locale="en" resources={{ en: { docs: enDocs } }}><DocTree /></I18nProvider></QueryClientProvider>);

  expect(screen.getByRole("heading", { level: 2, name: "Docs" })).toBeTruthy();

  const open = screen.getByRole("link", { name: "Open" });
  expect(open.getAttribute("aria-current")).toBe("page");
  expect(open.closest('[data-mc="nav-item"]')?.getAttribute("data-active")).toBe("true");
  for (const title of ["Other", "Inside"]) {
    const link = screen.getByRole("link", { name: title });
    expect(link.getAttribute("aria-current")).toBeNull();
    expect(link.closest('[data-mc="nav-item"]')?.getAttribute("data-active")).toBe("false");
  }
});

it("folder row actions are siblings of the disclosure and never fold the folder", () => {
  render(<QueryClientProvider client={qc}><I18nProvider locale="en" resources={{ en: { docs: enDocs } }}><DocTree /></I18nProvider></QueryClientProvider>);

  const toggle = screen.getByRole("button", { name: "Specs" });
  const more = screen.getByRole("button", { name: "More actions for Specs" });
  expect(toggle.getAttribute("aria-expanded")).toBe("true");
  expect(toggle.contains(more)).toBe(false);

  fireEvent.click(more);
  expect(toggle.getAttribute("aria-expanded")).toBe("true");
  expect(screen.getByRole("link", { name: "Inside" })).toBeTruthy();

  fireEvent.click(toggle);
  expect(toggle.getAttribute("aria-expanded")).toBe("false");
});

it.each([
  ["Open", "link", ["Rename", "Move to...", "Delete"]],
  ["Specs", "button", ["New doc", "New folder", "Rename", "Move to..."]],
] as const)("right-clicking %s opens its row menu", async (name, role, items) => {
  render(<QueryClientProvider client={qc}><I18nProvider locale="en" resources={{ en: { docs: enDocs } }}><DocTree /></I18nProvider></QueryClientProvider>);

  const event = new MouseEvent("contextmenu", { bubbles: true, cancelable: true, clientX: 40, clientY: 60 });
  fireEvent(screen.getByRole(role, { name }), event);

  expect(event.defaultPrevented).toBe(true);
  for (const item of items) expect(await screen.findByRole("menuitem", { name: item })).toBeTruthy();
  // The row reads as open, which keeps its actions shown.
  const row = screen.getByRole("button", { name: `More actions for ${name}` }).closest(".group\\/row");
  expect(row?.hasAttribute("data-menu-open")).toBe(true);

  fireEvent.keyDown(document.activeElement ?? document.body, { key: "Escape" });
  await waitFor(() => expect(row?.hasAttribute("data-menu-open")).toBe(false));
});
