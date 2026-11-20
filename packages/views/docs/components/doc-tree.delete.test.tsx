import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import { docKeys } from "@multica/core/docs";
import { workspaceKeys } from "@multica/core/workspace/queries";
import { DocTree } from "./doc-tree";
import enDocs from "../../locales/en/docs.json";

// Delete is only offered where the server would allow it: the creator, the
// creating agent's owner, and workspace owners or admins.
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
  useNavigation: () => ({ pathname: "/docs", push: vi.fn() }),
  AppLink: ({ children, href }: { children: React.ReactNode; href: string }) => <a href={href}>{children}</a>,
}));

const tree = {
  folders: [],
  docs: [
    { id: "mine", title: "Mine", folderId: null, createdByType: "member", createdById: "me", updatedAt: "2026-11-18T00:00:00Z" },
    { id: "agent", title: "By my agent", folderId: null, createdByType: "agent", createdById: "my-agent", updatedAt: "2026-11-18T00:00:00Z" },
    { id: "theirs", title: "Theirs", folderId: null, createdByType: "member", createdById: "someone", updatedAt: "2026-11-18T00:00:00Z" },
  ],
};
let qc: QueryClient;
beforeEach(() => {
  qc = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  qc.setQueryData(docKeys.tree("ws"), tree);
  qc.setQueryData(workspaceKeys.agents("ws"), [{ id: "my-agent", owner_id: "me" }]);
});
afterEach(() => { cleanup(); qc.clear(); });

function hasDelete(title: string) {
  fireEvent.pointerDown(screen.getByRole("button", { name: `More actions for ${title}` }), { button: 0 });
  fireEvent.click(screen.getByRole("button", { name: `More actions for ${title}` }));
  const shown = screen.queryByRole("menuitem", { name: "Delete" }) !== null;
  fireEvent.keyDown(document.activeElement ?? document.body, { key: "Escape" });
  cleanup();
  return shown;
}
function mount(role: string) {
  qc.setQueryData(workspaceKeys.members("ws"), [{ user_id: "me", role }]);
  render(<QueryClientProvider client={qc}><I18nProvider locale="en" resources={{ en: { docs: enDocs } }}><DocTree /></I18nProvider></QueryClientProvider>);
}

it.each([
  ["member", "Mine", true],
  ["member", "By my agent", true],
  ["member", "Theirs", false],
  ["admin", "Theirs", true],
  ["owner", "Theirs", true],
])("role=%s on %s offers delete=%s", async (role, title, expected) => {
  mount(role);
  expect(hasDelete(title)).toBe(expected);
});
