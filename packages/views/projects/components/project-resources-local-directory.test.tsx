// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach } from "vitest";
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { renderWithI18n } from "../../test/i18n";
import en from "../../locales/en/projects.json";

const state = vi.hoisted(() => ({
  resources: [] as unknown[],
  runtimes: [] as unknown[],
  desktop: true,
  daemon: { daemonId: "daemon-1" as string | null, deviceName: "MacBook" as string | null, running: true },
  createMock: vi.fn(),
  updateMock: vi.fn(),
  pickDirectory: vi.fn(),
  validateLocalDirectory: vi.fn(),
  copyText: vi.fn(),
}));

const NOW = Date.now();

function folder(id: string, daemonId: string, overrides: Record<string, unknown> = {}) {
  return {
    id,
    project_id: "p1",
    workspace_id: "workspace-1",
    resource_type: "local_directory",
    resource_ref: {
      local_path: `/Users/dev/work/${id}`,
      daemon_id: daemonId,
      label: id,
      execution_mode: "worktree",
    },
    label: null,
    position: 0,
    created_at: "2026-08-18T00:00:00Z",
    created_by: "u1",
    ...overrides,
  };
}

function runtime(daemonId: string, host: string, status: "online" | "offline") {
  return {
    id: `rt-${daemonId}`,
    workspace_id: "workspace-1",
    daemon_id: daemonId,
    name: `Claude (${host})`,
    runtime_mode: "local",
    provider: "claude",
    launch_header: "",
    status,
    device_info: `${host} · claude 1.0.0`,
    metadata: {},
    owner_id: "u1",
    visibility: "private",
    last_seen_at: new Date(NOW - (status === "online" ? 10_000 : 3_600_000)).toISOString(),
    created_at: "2026-05-17T11:00:00Z",
    updated_at: "2026-05-17T11:00:00Z",
  };
}

vi.mock("@tanstack/react-query", () => ({
  useQuery: (options: { queryKey?: unknown[] }) => {
    const key = options?.queryKey?.[0];
    if (key === "project-resources") return { data: state.resources };
    if (key === "runtimes") return { data: state.runtimes };
    return { data: [] };
  },
  queryOptions: (options: unknown) => options,
}));

vi.mock("@multica/core/projects", () => ({
  projectResourcesOptions: () => ({ queryKey: ["project-resources"], queryFn: vi.fn() }),
  useCreateProjectResource: () => ({ mutateAsync: state.createMock, isPending: false }),
  useUpdateProjectResource: () => ({ mutateAsync: state.updateMock }),
  useDeleteProjectResource: () => ({ mutateAsync: vi.fn() }),
}));

vi.mock("@multica/core/config", () => ({
  useConfigStore: (selector: (s: { localWorktreeSupported: boolean }) => unknown) =>
    selector({ localWorktreeSupported: true }),
}));

vi.mock("@multica/core/runtimes", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/runtimes")>()),
  runtimeListOptions: () => ({ queryKey: ["runtimes"], queryFn: vi.fn() }),
  runtimeAdvertisesLocalWorktree: () => true,
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "workspace-1" }));
vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => ({ id: "workspace-1", slug: "ws", repos: [] }),
}));
vi.mock("../../platform/local-directory", () => ({
  isDesktopShell: () => state.desktop,
  pickDirectory: state.pickDirectory,
  validateLocalDirectory: state.validateLocalDirectory,
}));
vi.mock("../../platform/use-local-daemon-status", () => ({
  useLocalDaemonStatus: () => state.daemon,
}));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("@multica/ui/lib/clipboard", () => ({ copyText: state.copyText }));

import { ProjectResourcesSection } from "./project-resources-section";

const strings = en.resources;

async function openAddMenu() {
  fireEvent.click(screen.getByRole("button", { name: strings.add }));
  return screen.findByRole("menuitem", {
    name: (name) => name.startsWith(strings.add_local),
  });
}

async function pickFolder(isGitRepo: boolean) {
  state.pickDirectory.mockResolvedValueOnce({
    ok: true,
    path: "/Users/dev/work/game-client",
    basename: "game-client",
  });
  state.validateLocalDirectory.mockResolvedValueOnce({ ok: true, is_git_repo: isGitRepo });
  fireEvent.click(await openAddMenu());
  return screen.findByRole("dialog");
}

async function openRowMenu(name: string) {
  fireEvent.click(screen.getByRole("button", { name: `Actions for ${name}` }));
  return screen.findByRole("menu");
}

beforeEach(() => {
  state.resources = [];
  state.runtimes = [];
  state.desktop = true;
  state.daemon = { daemonId: "daemon-1", deviceName: "MacBook", running: true };
  state.createMock.mockReset().mockResolvedValue({});
  state.updateMock.mockReset().mockResolvedValue({});
  state.pickDirectory.mockReset();
  state.validateLocalDirectory.mockReset();
  state.copyText.mockReset().mockResolvedValue(true);
});

describe("ProjectResourcesSection — local folders", () => {
  // The read-order matrix lives in local-directory-label.test.ts; this pins
  // the row to that helper so a label set through the API is what people see.
  it("names the row from the top-level label, not the copy inside the ref", () => {
    state.resources = [folder("game-client", "daemon-1", { label: "Game Client" })];
    renderWithI18n(<ProjectResourcesSection projectId="p1" />);
    expect(screen.getByText("Game Client")).toBeInTheDocument();
    expect(screen.queryByText("game-client")).not.toBeInTheDocument();
  });

  // A folder is identified by its path, so a rename only changed the title and
  // read as a broken edit action beside the other controls (MUL-7525). What
  // remains is the one setting that changes how tasks run on the folder.
  it("offers the execution mode and remove actions, and no rename", async () => {
    state.resources = [folder("game-client", "daemon-1")];
    renderWithI18n(<ProjectResourcesSection projectId="p1" />);
    expect(screen.getByRole("button", { name: strings.mode_badge_worktree })).toBeInTheDocument();
    const menu = await openRowMenu("game-client");
    expect(within(menu).getByRole("menuitem", { name: strings.remove_tooltip })).toBeInTheDocument();
    expect(within(menu).queryByRole("menuitem", { name: /rename/i })).not.toBeInTheDocument();
    expect(screen.queryByRole("textbox")).not.toBeInTheDocument();
  });

  it("copies a folder's path from its row menu", async () => {
    state.resources = [folder("game-client", "daemon-1")];
    renderWithI18n(<ProjectResourcesSection projectId="p1" />);
    const menu = await openRowMenu("game-client");
    fireEvent.click(within(menu).getByRole("menuitem", { name: strings.copy_path }));
    await waitFor(() => expect(state.copyText).toHaveBeenCalledWith("/Users/dev/work/game-client"));
  });

  it("asks for a repository or folder when the project has neither", () => {
    renderWithI18n(<ProjectResourcesSection projectId="p1" />);
    expect(screen.getByText(strings.empty)).toBeInTheDocument();
  });

  // The server allows one folder per computer, so a machine that already has
  // one offers no way to add a second, and says why.
  it("does not offer another folder on a computer that already has one", async () => {
    state.resources = [folder("game-client", "daemon-1")];
    renderWithI18n(<ProjectResourcesSection projectId="p1" />);
    const item = await openAddMenu();
    expect(item).toHaveAttribute("aria-disabled", "true");
    expect(item).toHaveTextContent(strings.add_local_exists);
  });

  it("names the computer each folder belongs to, with that computer's state", async () => {
    state.runtimes = [
      runtime("daemon-1", "macbook.local", "online"),
      runtime("daemon-2", "studio-pc.local", "offline"),
    ];
    state.resources = [folder("server-checkout", "daemon-2")];
    renderWithI18n(<ProjectResourcesSection projectId="p1" />);

    const row = screen.getByText("server-checkout").closest("[data-resource-row]") as HTMLElement;
    expect(within(row).getByText("studio-pc.local")).toBeInTheDocument();
    expect(within(row).getByText(strings.local_offline)).toBeInTheDocument();
    // This computer has no folder yet, so the Add menu offers one.
    const item = await openAddMenu();
    expect(item).not.toHaveAttribute("aria-disabled", "true");
    expect(item).toHaveTextContent(strings.add_local_hint);
  });

  it("marks this computer's folder and lists it first", () => {
    state.runtimes = [
      runtime("daemon-1", "macbook.local", "online"),
      runtime("daemon-2", "studio-pc.local", "online"),
    ];
    state.resources = [folder("server-checkout", "daemon-2"), folder("game-client", "daemon-1")];
    renderWithI18n(<ProjectResourcesSection projectId="p1" />);

    const rows = document.querySelectorAll("[data-resource-row]");
    expect(rows[0]).toHaveTextContent("game-client");
    expect(within(rows[0] as HTMLElement).getByText(strings.local_this_computer)).toBeInTheDocument();
    expect(rows[1]).toHaveTextContent("server-checkout");
    expect(within(rows[1] as HTMLElement).queryByText(strings.local_offline)).not.toBeInTheDocument();
  });

  it("falls back to a generic name for a computer with no registered runtime", () => {
    state.resources = [folder("old-checkout", "daemon-gone")];
    renderWithI18n(<ProjectResourcesSection projectId="p1" />);
    expect(screen.getByText(strings.local_unknown_computer)).toBeInTheDocument();
  });

  // Picking needs this machine's daemon, so web cannot add a folder; editing
  // one only rewrites the resource, so that stays available.
  it("lets web edit existing folders but not add one", async () => {
    state.desktop = false;
    state.daemon = { daemonId: null, deviceName: null, running: false };
    state.runtimes = [runtime("daemon-2", "studio-pc.local", "online")];
    state.resources = [folder("server-checkout", "daemon-2")];
    renderWithI18n(<ProjectResourcesSection projectId="p1" />);

    expect(screen.getByText("studio-pc.local")).toBeInTheDocument();
    expect(screen.queryByText(strings.local_offline)).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: strings.mode_badge_worktree })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Actions for server-checkout" })).toBeInTheDocument();
    const item = await openAddMenu();
    expect(item).toHaveAttribute("aria-disabled", "true");
    expect(item).toHaveTextContent(strings.add_local_desktop_only);
  });

  it("lists folders read-only, with nothing to add or change", () => {
    state.resources = [folder("game-client", "daemon-1"), folder("server-checkout", "daemon-2")];
    renderWithI18n(<ProjectResourcesSection projectId="p1" readOnly />);

    expect(screen.getByText("game-client")).toBeInTheDocument();
    expect(screen.getByText("server-checkout")).toBeInTheDocument();
    expect(screen.getAllByText(strings.mode_badge_worktree)).toHaveLength(2);
    expect(screen.queryByRole("button", { name: strings.mode_badge_worktree })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /^Actions for/ })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: strings.add })).not.toBeInTheDocument();
  });

  it("says there is nothing yet, read-only, without offering to add", () => {
    renderWithI18n(<ProjectResourcesSection projectId="p1" readOnly />);
    expect(screen.getByText(strings.empty_read_only)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: strings.add })).not.toBeInTheDocument();
  });

  it("asks to start the daemon before a folder can be chosen", async () => {
    state.daemon = { daemonId: null, deviceName: "MacBook", running: false };
    renderWithI18n(<ProjectResourcesSection projectId="p1" />);
    const item = await openAddMenu();
    expect(item).toHaveAttribute("aria-disabled", "true");
    expect(item).toHaveTextContent(strings.local_daemon_offline_hint);
  });

  // The mode is part of what the user is choosing — whether tasks edit this
  // folder or hand back a branch — so it is asked before anything is created.
  it("asks for the mode in a dialog and creates the folder with it", async () => {
    renderWithI18n(<ProjectResourcesSection projectId="p1" />);
    const dialog = await pickFolder(true);

    expect(within(dialog).getByText("/Users/dev/work/game-client")).toBeInTheDocument();
    const [direct, parallel] = within(dialog).getAllByRole("radio");
    expect(parallel).toHaveAttribute("aria-checked", "true");
    expect(state.createMock).not.toHaveBeenCalled();

    fireEvent.click(direct!);
    fireEvent.click(within(dialog).getByRole("button", { name: strings.mode_add }));

    await waitFor(() =>
      expect(state.createMock).toHaveBeenCalledWith({
        resource_type: "local_directory",
        resource_ref: {
          local_path: "/Users/dev/work/game-client",
          daemon_id: "daemon-1",
          label: "game-client",
          execution_mode: "in_place",
        },
      }),
    );
  });

  it("keeps the picked folder on screen with the server's reason when adding fails", async () => {
    state.createMock.mockRejectedValueOnce(new Error("this computer already has a folder"));
    renderWithI18n(<ProjectResourcesSection projectId="p1" />);
    const dialog = await pickFolder(true);

    fireEvent.click(within(dialog).getByRole("button", { name: strings.mode_add }));

    expect(await within(dialog).findByText("this computer already has a folder")).toBeInTheDocument();
    expect(within(dialog).getByText("/Users/dev/work/game-client")).toBeInTheDocument();
  });

  it("swaps the picked folder for another one from the dialog", async () => {
    renderWithI18n(<ProjectResourcesSection projectId="p1" />);
    const dialog = await pickFolder(true);

    state.pickDirectory.mockResolvedValueOnce({
      ok: true,
      path: "/Users/dev/work/notes",
      basename: "notes",
    });
    state.validateLocalDirectory.mockResolvedValueOnce({ ok: true, is_git_repo: false });
    fireEvent.click(within(dialog).getByRole("button", { name: strings.local_dialog_change }));

    expect(await within(dialog).findByText("/Users/dev/work/notes")).toBeInTheDocument();
    expect(within(dialog).getAllByRole("radio")[0]).toHaveAttribute("aria-checked", "true");
  });

  it("starts a non-git folder on direct editing, with parallel closed", async () => {
    renderWithI18n(<ProjectResourcesSection projectId="p1" />);
    const dialog = await pickFolder(false);

    const [direct, parallel] = within(dialog).getAllByRole("radio");
    expect(direct).toHaveAttribute("aria-checked", "true");
    expect(parallel).toBeDisabled();
  });

  it("creates nothing when the picked folder is cancelled", async () => {
    renderWithI18n(<ProjectResourcesSection projectId="p1" />);
    const dialog = await pickFolder(true);

    fireEvent.click(within(dialog).getByRole("button", { name: strings.mode_cancel }));

    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    expect(state.createMock).not.toHaveBeenCalled();
  });

  it("saves a mode as soon as it is picked, keeping the rest of the ref", async () => {
    state.resources = [folder("game-client", "daemon-1")];
    renderWithI18n(<ProjectResourcesSection projectId="p1" />);

    fireEvent.click(screen.getByRole("button", { name: strings.mode_badge_worktree }));
    fireEvent.click(screen.getAllByRole("radio")[0]!);

    await waitFor(() =>
      expect(state.updateMock).toHaveBeenCalledWith({
        resourceId: "game-client",
        data: {
          resource_ref: {
            local_path: "/Users/dev/work/game-client",
            daemon_id: "daemon-1",
            label: "game-client",
            execution_mode: "in_place",
          },
        },
      }),
    );
  });

  // The likely failure is the server's daemon-version gate; a toast alone
  // would leave nothing to act on.
  it("shows a rejected mode change inside the picker", async () => {
    state.updateMock.mockRejectedValueOnce(new Error("daemon is too old for parallel runs"));
    state.resources = [folder("game-client", "daemon-1", {
      resource_ref: {
        local_path: "/Users/dev/work/game-client",
        daemon_id: "daemon-1",
        label: "game-client",
        execution_mode: "in_place",
      },
    })];
    renderWithI18n(<ProjectResourcesSection projectId="p1" />);

    fireEvent.click(screen.getByRole("button", { name: strings.mode_badge_in_place }));
    fireEvent.click(screen.getAllByRole("radio")[1]!);

    expect(await screen.findByText("daemon is too old for parallel runs")).toBeInTheDocument();
  });
});
