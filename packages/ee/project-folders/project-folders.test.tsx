import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { act, fireEvent, renderHook, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { setApiInstance } from "@multica/core/api";
import type { ApiClient } from "@multica/core/api/client";
import { EMPTY_PROJECT } from "@multica/core/api/schemas";
import { I18nProvider } from "@multica/core/i18n/react";
import { DropdownMenu, DropdownMenuContent, DropdownMenuTrigger } from "@multica/ui/components/ui/dropdown-menu";
import { getProjectFoldersExtension, registerProjectFoldersExtension } from "@multica/views/extensions";
import { renderWithI18n } from "../test/i18n";
import { registerEE } from "../index";
import { projectFoldersExtension } from "./components";
import type { ProjectFolderList } from "./data";
import { useFolderDialogStore, useProjectFoldersState } from "./state";

const ws = vi.hoisted(() => ({ handlers: new Map<string, () => void>() }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws" }));
vi.mock("@multica/core/realtime", () => ({
  useWSEvent: (event: string, handler: () => void) => ws.handlers.set(event, handler),
  useWSReconnect: (handler: () => void) => ws.handlers.set("reconnect", handler),
}));
vi.mock("@multica/core/paths", async (importOriginal) => ({
  ...(await importOriginal<object>()),
  useWorkspacePaths: () => ({ settings: () => "/acme/settings" }),
}));
vi.mock("@multica/views/navigation", async (importOriginal) => ({
  ...(await importOriginal<object>()),
  AppLink: ({ href, children, ...props }: React.ComponentProps<"a"> & { href: string }) => (
    <a href={href} {...props}>
      {children}
    </a>
  ),
}));
// The sidebar opens a folder's menu at the pointer; here a button stands in.
vi.mock("@multica/views/layout/collection-list", () => ({
  CollectionRowMenu: ({ label, children }: { label: string; children: ReactNode }) => (
    <DropdownMenu>
      <DropdownMenuTrigger aria-label={label}>{label}</DropdownMenuTrigger>
      <DropdownMenuContent>{children}</DropdownMenuContent>
    </DropdownMenu>
  ),
}));

const { AddMenuItems, FolderMenu, ProjectMenuItems, Host } = projectFoldersExtension;
const project = { ...EMPTY_PROJECT, id: "p1", workspace_id: "ws", title: "Pricing" };

let qc: QueryClient;
let list: ProjectFolderList;
let request: ReturnType<typeof vi.fn>;

beforeEach(() => {
  qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  list = {
    folders: [
      { id: "f2", name: "Sales" },
      { id: "f1", name: "Growth" },
    ],
    filed: [
      { project_id: "p1", folder_id: "f1" },
      { project_id: "p9", folder_id: "gone" },
    ],
  };
  request = vi.fn().mockImplementation(async (path: string, init?: RequestInit) => {
    if (path === "/api/project-folders" && !init) return list;
    if (path === "/api/project-folders") return { id: "f3", name: JSON.parse(String(init?.body)).name };
    return undefined;
  });
  setApiInstance({ request } as unknown as ApiClient);
  useFolderDialogStore.getState().close();
});
afterEach(() => qc.clear());

function wrapper({ children }: { children: ReactNode }) {
  return (
    <QueryClientProvider client={qc}>
      <I18nProvider locale="en" resources={{ en: {} }}>
        {children}
      </I18nProvider>
    </QueryClientProvider>
  );
}

function renderInMenu(items: ReactNode) {
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <DropdownMenu>
        <DropdownMenuTrigger>Open</DropdownMenuTrigger>
        <DropdownMenuContent>{items}</DropdownMenuContent>
      </DropdownMenu>
      <Host />
    </QueryClientProvider>,
  );
}

it("lists folders by name and files only projects in folders that still exist", async () => {
  const { result } = renderHook(() => useProjectFoldersState("ws"), { wrapper });
  await waitFor(() => expect(result.current.canFile).toBe(true));
  expect(result.current.folders.map((f) => f.name)).toEqual(["Growth", "Sales"]);
  expect([...result.current.folderOf]).toEqual([["p1", "f1"]]);
  expect(result.current.labels.newProjectIn("Growth")).toBe("New project in Growth");

  act(() => result.current.moveProject("p1", null));
  await waitFor(() =>
    expect(request).toHaveBeenCalledWith("/api/project-folders/file", {
      method: "POST",
      body: JSON.stringify({ project_id: "p1", folder_id: null }),
    }),
  );
});

it("moves a project between folders from its menu", async () => {
  const user = userEvent.setup();
  renderInMenu(<ProjectMenuItems project={project} />);
  await user.click(screen.getByRole("button", { name: "Open" }));
  await user.click(await screen.findByRole("menuitem", { name: "Move to folder" }));
  const sales = await screen.findByRole("menuitem", { name: "Sales" });
  await waitFor(() => expect(sales).not.toHaveAttribute("aria-disabled"));
  fireEvent.click(sales);
  await waitFor(() =>
    expect(request).toHaveBeenCalledWith("/api/project-folders/file", {
      method: "POST",
      body: JSON.stringify({ project_id: "p1", folder_id: "f2" }),
    }),
  );
});

it("creates a folder from the add menu", async () => {
  const user = userEvent.setup();
  renderInMenu(<AddMenuItems />);
  await user.click(screen.getByRole("button", { name: "Open" }));
  await user.click(await screen.findByRole("menuitem", { name: "New folder" }));
  await user.type(await screen.findByRole("textbox", { name: "Folder name" }), "Partners");
  await user.click(screen.getByRole("button", { name: "Create folder" }));
  await waitFor(() =>
    expect(request).toHaveBeenCalledWith("/api/project-folders", {
      method: "POST",
      body: JSON.stringify({ name: "Partners" }),
    }),
  );
});

it("moves a project out of its folder", async () => {
  const user = userEvent.setup();
  renderInMenu(<ProjectMenuItems project={project} />);
  await user.click(screen.getByRole("button", { name: "Open" }));
  await user.click(await screen.findByRole("menuitem", { name: "Move to folder" }));
  fireEvent.click(await screen.findByRole("menuitem", { name: "Remove from folder" }));
  await waitFor(() =>
    expect(request).toHaveBeenCalledWith("/api/project-folders/file", {
      method: "POST",
      body: JSON.stringify({ project_id: "p1", folder_id: null }),
    }),
  );
});

it("offers rename, delete and a new project from a folder's menu", async () => {
  const user = userEvent.setup();
  renderWithI18n(
    <QueryClientProvider client={qc}>
      <FolderMenu folder={{ id: "f1", name: "Growth" }} />
    </QueryClientProvider>,
  );
  await user.click(screen.getByRole("button", { name: "Actions for Growth" }));
  expect(await screen.findByRole("menuitem", { name: "New project" })).toBeVisible();
  expect(screen.getByRole("menuitem", { name: "Rename folder" })).toBeVisible();
  expect(screen.getByRole("menuitem", { name: "Delete folder" })).toBeVisible();
});

function FolderNames() {
  const { folders, folderOf } = useProjectFoldersState("ws");
  return <output aria-label="folders">{folders.map((f) => f.name).join(",") + "|" + [...folderOf.values()].join(",")}</output>;
}

async function deleteGrowth(answer: () => Promise<undefined>) {
  request.mockImplementation(async (path: string, init?: RequestInit) => {
    if (init?.method === "DELETE") return answer();
    if (path === "/api/project-folders") return list;
    return undefined;
  });
  const user = userEvent.setup();
  renderWithI18n(
    <QueryClientProvider client={qc}>
      <FolderNames />
      <Host />
    </QueryClientProvider>,
  );
  const names = screen.getByRole("status", { name: "folders" });
  await waitFor(() => expect(names).toHaveTextContent("Growth,Sales|f1"));
  act(() => useFolderDialogStore.getState().open({ kind: "delete", folder: { id: "f1", name: "Growth" } }));
  await user.click(await screen.findByRole("button", { name: "Delete folder" }));
  await waitFor(() => expect(request).toHaveBeenCalledWith("/api/project-folders/f1", { method: "DELETE" }));
  return names;
}

it("keeps a folder and its projects until the server deletes it", async () => {
  let finish!: () => void;
  const names = await deleteGrowth(() => new Promise((resolve) => (finish = () => resolve(undefined))));
  expect(names).toHaveTextContent("Growth,Sales|f1");
  expect(screen.getByRole("dialog")).toBeVisible();

  list = { folders: [{ id: "f2", name: "Sales" }], filed: [] };
  act(() => finish());
  await waitFor(() => expect(names).toHaveTextContent("Sales|"));
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
});

it("keeps a folder and its projects when deleting it fails", async () => {
  const names = await deleteGrowth(() => Promise.reject(new Error("offline")));
  expect(await screen.findByRole("dialog")).toBeVisible();
  expect(names).toHaveTextContent("Growth,Sales|f1");
});

it("refetches folders when another member changes them", async () => {
  function Sidebar() {
    useProjectFoldersState("ws");
    return null;
  }
  renderWithI18n(
    <QueryClientProvider client={qc}>
      <Sidebar />
      <Host />
    </QueryClientProvider>,
  );
  await waitFor(() => expect(request).toHaveBeenCalledWith("/api/project-folders"));
  const calls = request.mock.calls.length;
  act(() => ws.handlers.get("project_folder:filed")!());
  await waitFor(() => expect(request.mock.calls.length).toBeGreaterThan(calls));
});

it("registers the folder extension in the app", () => {
  registerProjectFoldersExtension(null);
  registerEE();
  expect(getProjectFoldersExtension()).toBe(projectFoldersExtension);
  registerProjectFoldersExtension(null);
});
