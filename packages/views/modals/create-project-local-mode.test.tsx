// @vitest-environment jsdom

import React from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderWithI18n } from "../test/i18n";

// Runtime list drives the worktree version gate. Each test sets the reported
// CLI version before rendering.
let runtimeCliVersion = "9.9.9";
// What the fake runtime row says about worktree support. This — not the version
// string — is what the gate reads now (MUL-5707), and the three values are
// genuinely different states: "yes", "a daemon that cannot", and a row written
// by a server too old to record capabilities at all (#7113).
let runtimeWorktreeMetadata: "advertised" | "daemon_cannot" | "server_recorded_nothing" =
  "advertised";
// What the desktop validator reports for the picked folder.
let pickedIsGitRepo: boolean | undefined = true;

const createProjectMock = vi.fn().mockResolvedValue({ id: "p1", slug: "p1" });

// jsdom has no IntersectionObserver; the modal's scroll affordances construct
// one, and the resulting rejection aborts the submit handler mid-flight.
class NoopIntersectionObserver {
  observe() {}
  unobserve() {}
  disconnect() {}
  takeRecords() {
    return [];
  }
}
vi.stubGlobal("IntersectionObserver", NoopIntersectionObserver);

vi.mock("@tanstack/react-query", () => ({
  // Query-aware: the modal issues several. Only the runtime list matters here;
  // returning runtimes for all of them would feed them to the member/agent
  // pickers, which expect a different shape.
  useQuery: (options: { queryKey?: unknown[] }) => {
    const key = options?.queryKey?.[0];
    if (key === "members" || key === "agents") return { data: [] };
    return {
      data: [
        {
          daemon_id: "daemon-1",
          metadata: {
            cli_version: runtimeCliVersion,
            // A capability-aware server always writes the key — null when the
            // daemon sent no header — so its absence means the SERVER is old.
            ...(runtimeWorktreeMetadata === "advertised"
              ? { capabilities: ["local-worktree-v1"] }
              : runtimeWorktreeMetadata === "daemon_cannot"
                ? { capabilities: ["skill-bundles-v1"] }
                : {}),
          },
        },
      ],
    };
  },
  // runtimeListOptions builds its descriptor with this; the mocked useQuery
  // reads queryKey off it, so an identity passthrough is enough.
  queryOptions: (options: unknown) => options,
}));

vi.mock("@multica/core/projects/mutations", () => ({
  useCreateProject: () => ({ mutateAsync: createProjectMock }),
}));

vi.mock("@multica/core/projects", () => ({
  useProjectDraftStore: (selector: (state: unknown) => unknown) =>
    selector({
      draft: {
        title: "Game Client",
        description: "",
        status: "planned",
        priority: "none",
        icon: "📁",
        leadId: null,
        leadType: null,
        startDate: null,
        dueDate: null,
        repos: [],
      },
      setDraft: vi.fn(),
      clearDraft: vi.fn(),
      resetDraft: vi.fn(),
    }),
}));

// Whether the connected server validates execution_mode at all. Absent on every
// release before the worktree save gate.
let serverValidatesWorktree = true;
vi.mock("@multica/core/config", () => ({
  useConfigStore: (selector: (state: { localWorktreeSupported: boolean }) => unknown) =>
    selector({ localWorktreeSupported: serverValidatesWorktree }),
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "workspace-1" }));
vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => ({ id: "workspace-1", slug: "ws", repos: [] }),
  useWorkspacePaths: () => ({ projectDetail: (id: string) => `/ws/projects/${id}` }),
}));
vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: () => ({ queryKey: ["members"], queryFn: vi.fn() }),
  agentListOptions: () => ({ queryKey: ["agents"], queryFn: vi.fn() }),
}));
vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({ getActorName: vi.fn() }),
}));
vi.mock("../navigation", () => ({ useNavigation: () => ({ push: vi.fn() }) }));
vi.mock("../editor", () => {
  const ContentEditor = React.forwardRef<HTMLTextAreaElement, { placeholder?: string }>(
    ({ placeholder }, ref) => <textarea ref={ref} placeholder={placeholder} />,
  );
  ContentEditor.displayName = "ContentEditor";
  return {
    ContentEditor,
    TitleEditor: ({
      placeholder,
      onChange,
    }: {
      placeholder?: string;
      onChange?: (value: string) => void;
    }) => <input placeholder={placeholder} onChange={(e) => onChange?.(e.target.value)} />,
  };
});
vi.mock("../issues/components/priority-icon", () => ({ PriorityIcon: () => <span /> }));
vi.mock("../common/actor-avatar", () => ({ ActorAvatar: () => <span /> }));
vi.mock("../projects/components/project-start-date-picker", () => ({
  ProjectStartDatePicker: () => <button type="button">Start date</button>,
}));
vi.mock("../projects/components/project-due-date-picker", () => ({
  ProjectDueDatePicker: () => <button type="button">Due date</button>,
}));

// Desktop-only surface: without these the Local directory tab never renders.
vi.mock("../platform/local-directory", () => ({
  isDesktopShell: () => true,
  pickDirectory: () =>
    Promise.resolve({ ok: true, path: "/Users/dev/work/game-client", basename: "game-client" }),
  validateLocalDirectory: () => Promise.resolve({ ok: true, is_git_repo: pickedIsGitRepo }),
}));
vi.mock("../platform/use-local-daemon-status", () => ({
  useLocalDaemonStatus: () => ({ daemonId: "daemon-1", deviceName: "MacBook", running: true }),
}));

// Render overlays inline so their contents are assertable.
vi.mock("@multica/ui/components/ui/dialog", () => ({
  Dialog: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DialogContent: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DialogTitle: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
}));
vi.mock("@multica/ui/components/ui/popover", () => ({
  Popover: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  PopoverTrigger: ({ render }: { render: React.ReactNode }) => <>{render}</>,
  PopoverContent: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
}));
vi.mock("@multica/ui/components/ui/dropdown-menu", () => ({
  DropdownMenu: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  DropdownMenuTrigger: ({ render }: { render: React.ReactNode }) => <>{render}</>,
  DropdownMenuContent: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  DropdownMenuItem: ({ children, onClick }: { children: React.ReactNode; onClick?: () => void }) => (
    <button type="button" onClick={onClick}>
      {children}
    </button>
  ),
}));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

import { CreateProjectModal, buildLocalDirectoryResourceRef } from "./create-project";

async function pickLocalDirectory(user: ReturnType<typeof userEvent.setup>) {
  await user.click(screen.getByRole("button", { name: /Local directory/i }));
  await user.click(screen.getByRole("button", { name: /Choose directory/i }));
  await waitFor(() => expect(screen.getByText("/Users/dev/work/game-client")).toBeInTheDocument());
}

describe("CreateProjectModal — local directory execution mode", () => {
  beforeEach(() => {
    createProjectMock.mockClear();
    runtimeCliVersion = "9.9.9";
    runtimeWorktreeMetadata = "advertised";
    serverValidatesWorktree = true;
    pickedIsGitRepo = true;
  });

  // Preselection, not a silent default change: a git repo the runtime can
  // actually run worktree mode on starts on parallel, because that is the mode
  // that fits — and the user sees it selected in a control they can flip before
  // creating anything.
  it("preselects parallel for a git repository", async () => {
    const user = userEvent.setup();
    renderWithI18n(<CreateProjectModal onClose={vi.fn()} />);

    await pickLocalDirectory(user);

    expect(screen.getByRole("button", { name: /^Parallel$/i })).toBeInTheDocument();
    expect(screen.getByRole("radio", { name: /Run in parallel, isolated/i })).toHaveAttribute(
      "aria-checked",
      "true",
    );
  });

  it("preselects direct for a plain folder", async () => {
    pickedIsGitRepo = false;
    const user = userEvent.setup();
    renderWithI18n(<CreateProjectModal onClose={vi.fn()} />);

    await pickLocalDirectory(user);

    expect(screen.getByRole("button", { name: /^Direct$/i })).toBeInTheDocument();
  });

  // A machine that has not advertised the capability must not be preselected
  // into parallel: the server gates this create, and a rejection would fail the
  // whole project creation over a mode the user never chose. It stays
  // SELECTABLE — an un-advertised machine may still be able to run it, and only
  // the server can say (#7113).
  it("preselects direct when the machine has not advertised the capability", async () => {
    runtimeWorktreeMetadata = "daemon_cannot";
    const user = userEvent.setup();
    renderWithI18n(<CreateProjectModal onClose={vi.fn()} />);

    await pickLocalDirectory(user);

    expect(screen.getByRole("button", { name: /^Direct$/i })).toBeInTheDocument();
  });

  // Older desktop builds do not report is_git_repo. The option stays available
  // (the daemon has the final say), but we do not guess parallel on the user's
  // behalf without evidence.
  it("preselects direct when the desktop build cannot report git-ness", async () => {
    pickedIsGitRepo = undefined;
    const user = userEvent.setup();
    renderWithI18n(<CreateProjectModal onClose={vi.fn()} />);

    await pickLocalDirectory(user);

    expect(screen.getByRole("button", { name: /^Direct$/i })).toBeInTheDocument();
    // Still selectable — unknown is permissive about what the user MAY choose.
    expect(screen.getByRole("radio", { name: /Run in parallel, isolated/i })).not.toBeDisabled();
  });

  // The preselection is a starting point, not a correction: once the user says
  // what they want, a later folder change must not silently undo it.
  it("keeps an explicit choice when the folder changes to another git repository", async () => {
    const user = userEvent.setup();
    renderWithI18n(<CreateProjectModal onClose={vi.fn()} />);

    await pickLocalDirectory(user);
    await user.click(screen.getByRole("radio", { name: /Edit this folder directly/i }));
    expect(screen.getByRole("button", { name: /^Direct$/i })).toBeInTheDocument();

    // Re-pick (the mocked picker returns the same git repo).
    await user.click(screen.getByRole("button", { name: /Change directory/i }));
    await waitFor(() =>
      expect(screen.getByRole("button", { name: /^Direct$/i })).toBeInTheDocument(),
    );
  });

  // The whole point of this entry point: the mode is part of setting the
  // folder up, not something to go and change afterwards.
  it("offers the mode next to the change-directory action once a folder is picked", async () => {
    const user = userEvent.setup();
    renderWithI18n(<CreateProjectModal onClose={vi.fn()} />);

    await pickLocalDirectory(user);

    // The compact button uses the short label; the picker carries the full
    // title. A git repo preselects parallel, so that is the label here.
    expect(screen.getByRole("button", { name: /^Parallel$/i })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Change directory/i })).toBeInTheDocument();
  });

  it("still offers parallel mode when the machine has not advertised", async () => {
    runtimeWorktreeMetadata = "server_recorded_nothing";
    const user = userEvent.setup();
    renderWithI18n(<CreateProjectModal onClose={vi.fn()} />);

    await pickLocalDirectory(user);

    expect(screen.getByRole("radio", { name: /Run in parallel, isolated/i })).not.toBeDisabled();
  });

  // The version skew this whole issue is about: newest Desktop, newest daemon,
  // a self-hosted backend from before v0.4.25. That server accepts the save and
  // silently strips execution_mode, so trusting it to reject would hand the
  // agent the user's working copy while promising isolation.
  it("blocks parallel mode against a server that cannot honour it", async () => {
    serverValidatesWorktree = false;
    const user = userEvent.setup();
    renderWithI18n(<CreateProjectModal onClose={vi.fn()} />);

    await pickLocalDirectory(user);

    expect(screen.getByRole("radio", { name: /Run in parallel, isolated/i })).toBeDisabled();
    expect(screen.getByText(/Multica server is too old/i)).toBeInTheDocument();
    // And it must not have been preselected either — that would submit a mode
    // the server would silently downgrade.
    expect(screen.getByRole("button", { name: /^Direct$/i })).toBeInTheDocument();
  });

  it("blocks parallel mode for a folder that is not a git repository", async () => {
    pickedIsGitRepo = false;
    const user = userEvent.setup();
    renderWithI18n(<CreateProjectModal onClose={vi.fn()} />);

    await pickLocalDirectory(user);

    expect(screen.getByRole("radio", { name: /Run in parallel, isolated/i })).toBeDisabled();
    expect(screen.getByText(/not a git repository/i)).toBeInTheDocument();
  });
});

// The payload is what the server stores and the daemon later reads; a missing
// or wrong execution_mode is invisible until a task actually runs. Asserted on
// the builder directly rather than through a full modal submit, which drags in
// unrelated modal machinery without testing anything more about this contract.
describe("buildLocalDirectoryResourceRef", () => {
  it("carries the chosen mode", () => {
    expect(
      buildLocalDirectoryResourceRef({
        localPath: "/Users/dev/work/game-client",
        daemonId: "daemon-1",
        label: "game-client",
        mode: "worktree",
      }),
    ).toEqual({
      local_path: "/Users/dev/work/game-client",
      daemon_id: "daemon-1",
      label: "game-client",
      execution_mode: "worktree",
    });
  });

  it("defaults are explicit: in_place is sent, not omitted", () => {
    // The server treats an absent field as in_place, but sending it keeps the
    // stored ref self-describing for anyone reading it later.
    expect(
      buildLocalDirectoryResourceRef({
        localPath: "/tmp/x",
        daemonId: "d",
        label: null,
        mode: "in_place",
      }),
    ).toEqual({ local_path: "/tmp/x", daemon_id: "d", execution_mode: "in_place" });
  });
});
