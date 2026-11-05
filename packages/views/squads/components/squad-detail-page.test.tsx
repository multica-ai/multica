import type React from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Issue, Squad, SquadMember, SquadMemberStatus } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";
import { SquadDetailPage } from "./squad-detail-page";

type QueryResult = { data?: unknown; isError?: boolean; isPending?: boolean };

const mocks = vi.hoisted(() => ({
  queries: {} as Record<string, QueryResult>,
  userId: "creator",
  openModal: vi.fn(),
  copyText: vi.fn(),
  api: {
    updateSquad: vi.fn(),
    deleteSquad: vi.fn(),
    removeSquadMember: vi.fn(),
    updateAgent: vi.fn(),
    setSquadRuntime: vi.fn(),
    setSquadSkills: vi.fn(),
    setSquadPlugins: vi.fn(),
  },
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: (options: { queryKey?: readonly unknown[] }) => ({
    isError: false,
    isPending: false,
    refetch: vi.fn(),
    ...mocks.queries[(options.queryKey ?? []).join("/")],
  }),
  useQueryClient: () => ({ invalidateQueries: vi.fn() }),
  useQueries: () => [],
  useMutation: ({ mutationFn }: { mutationFn: (input?: unknown) => Promise<unknown> }) => ({
    mutate: (input?: unknown) => void mutationFn(input),
    mutateAsync: (input?: unknown) => mutationFn(input),
    isPending: false,
  }),
}));
vi.mock("@multica/core/company", () => ({
  companyFloorsOptions: () => ({ queryKey: ["floors"] }),
}));
vi.mock("../../company/lib/use-add-squad-to-company", () => ({
  useAddSquadToCompany: () => ({ addSquad: vi.fn(), pending: false }),
}));
vi.mock("@multica/core/workspace/queries", () => ({
  agentListOptions: () => ({ queryKey: ["agents"] }),
  squadListOptions: () => ({ queryKey: ["squads-list"] }),
  memberListOptions: () => ({ queryKey: ["members"] }),
  squadMemberStatusOptions: () => ({ queryKey: ["member-status"] }),
  skillListOptions: () => ({ queryKey: ["skills"] }),
  squadSkillsOptions: () => ({ queryKey: ["squad-skills"] }),
  squadPluginsOptions: () => ({ queryKey: ["squad-plugins"] }),
  workspaceKeys: { squads: () => ["squads"], agents: () => ["agents"] },
}));
vi.mock("@multica/core/runtimes", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/runtimes")>()),
  runtimeListOptions: () => ({ queryKey: ["runtimes"] }),
  runtimeCapabilitiesOptions: () => ({ queryKey: ["capabilities"] }),
  runtimeModelsOptions: () => ({ queryKey: ["models"] }),
}));
vi.mock("@multica/core/api", () => ({ api: mocks.api }));
vi.mock("@multica/core/auth", () => ({
  useAuthStore: (select: (s: { user: { id: string } }) => unknown) => select({ user: { id: mocks.userId } }),
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => ({ id: "ws-1", slug: "acme" }),
  useWorkspacePaths: () => ({
    squads: () => "/acme/squads",
    newAgent: () => "/acme/agents/new",
    agentDetail: (id: string) => `/acme/agents/${id}`,
    issueDetail: (id: string) => `/acme/issues/${id}`,
  }),
}));
// The skill picker pulls in workspace route icons this suite does not mock.
vi.mock("../../agents/components/skill-picker-list", () => ({ SkillPickerList: () => null }));
vi.mock("@multica/core/modals", () => ({
  useModalStore: { getState: () => ({ open: mocks.openModal }) },
}));
vi.mock("@multica/core/issue-statuses/hooks", () => ({
  useIssueStatuses: () => ({ categoryOf: () => "started", colorOf: () => undefined, iconOf: () => undefined }),
}));
vi.mock("../../issues/utils/status-label", () => ({
  useStatusLabel: () => (status: string) => (status === "in_progress" ? "In Progress" : status),
}));
vi.mock("../../issues/components/status-icon", () => ({ StatusIcon: () => null }));
vi.mock("../../common/actor-avatar", () => ({ ActorAvatar: () => null }));
vi.mock("../../common/avatar-upload-control", () => ({
  AvatarUploadControl: () => <span data-testid="avatar-upload" />,
}));
vi.mock("../../editor/readonly-content", () => ({
  ReadonlyContent: ({ content }: { content: string }) => <div>{content}</div>,
}));
vi.mock("../../editor/content-editor", () => ({
  ContentEditor: ({ value, onUpdate }: { value: string; onUpdate: (v: string) => void }) => (
    <textarea
      aria-label="Instructions editor"
      value={value}
      onChange={(event: React.ChangeEvent<HTMLTextAreaElement>) => onUpdate(event.target.value)}
    />
  ),
}));
vi.mock("@multica/ui/lib/clipboard", () => ({ copyText: mocks.copyText }));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

// jsdom gives every element a zero-size rect, so a live resize handle claims
// every pointerdown on the page and no menu could open.
vi.mock("react-resizable-panels", () => ({
  useDefaultLayout: () => ({ defaultLayout: undefined, onLayoutChanged: vi.fn() }),
  usePanelRef: () => ({
    current: { isCollapsed: () => false, expand: vi.fn(), collapse: vi.fn() },
  }),
}));
vi.mock("@multica/ui/components/ui/resizable", () => ({
  ResizablePanelGroup: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  ResizablePanel: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  ResizableHandle: () => null,
}));

const SQUAD: Squad = {
  id: "sq-1",
  workspace_id: "ws-1",
  name: "Strike Team",
  description: "Ships the risky stuff",
  instructions: "Start with a failing test.",
  avatar_url: null,
  leader_id: "agent-lead",
  creator_id: "creator",
  created_at: "2026-10-01T00:00:00Z",
  updated_at: "2026-10-02T00:00:00Z",
  archived_at: null,
  archived_by: null,
};

function member(id: string, type: "agent" | "member", role = ""): SquadMember {
  return { id: `m-${id}`, squad_id: "sq-1", member_type: type, member_id: id, role, created_at: SQUAD.created_at };
}

const MEMBERS = [member("agent-lead", "agent", "Coordinator"), member("agent-2", "agent"), member("creator", "member")];

const STATUSES: SquadMemberStatus[] = [
  {
    member_type: "agent",
    member_id: "agent-2",
    status: "working",
    active_issues: [
      { issue_id: "i-1", identifier: "ARIE-2", title: "Fix login", issue_status: "in_progress" },
    ],
    last_active_at: null,
  } as SquadMemberStatus,
];

const RUNTIMES = [
  { id: "rt-1", name: "Laptop", owner_id: "creator", status: "online", visibility: "private", shared_with: [] },
  { id: "rt-2", name: "Workstation", owner_id: "creator", status: "offline", visibility: "private", shared_with: [] },
  { id: "rt-other", name: "Not mine", owner_id: "someone-else", status: "online", visibility: "private", shared_with: [] },
] as unknown as import("@multica/core/types").RuntimeDevice[];

const ISSUE = {
  id: "i-1",
  identifier: "ARIE-2",
  title: "Fix login",
  status: "in_progress",
  updated_at: "2026-10-02T00:00:00Z",
} as Issue;

function seed(squad: Squad | undefined = SQUAD, extra: Record<string, QueryResult> = {}) {
  mocks.queries = {
    "squads/sq-1": { data: squad },
    "squads/sq-1/members": { data: MEMBERS },
    "member-status": { data: { members: STATUSES } },
    "squads/sq-1/recent-issues": { data: { issues: [ISSUE], total: 1 } },
    agents: {
      data: [
        { id: "agent-lead", name: "Lambda", archived_at: null, runtime_id: "rt-1" },
        { id: "agent-2", name: "Kappa", archived_at: null, runtime_id: "rt-1" },
      ],
    },
    runtimes: { data: RUNTIMES },
    members: {
      data: [
        { user_id: "creator", name: "Jiayuan", role: "member" },
        { user_id: "viewer", name: "Viewer", role: "member" },
      ],
    },
    ...extra,
  };
}

function renderPage(search = "") {
  const adapter: NavigationAdapter = {
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    pathname: "/acme/squads/sq-1",
    searchParams: new URLSearchParams(search),
    hash: "",
    getShareableUrl: (path) => `https://multica.test${path}`,
  };
  renderWithI18n(
    <NavigationProvider value={adapter}>
      <SquadDetailPage />
    </NavigationProvider>,
  );
  return adapter;
}

function section(name: string) {
  return screen.getByRole("heading", { level: 2, name }).closest("section") as HTMLElement;
}

beforeEach(() => {
  seed();
  mocks.userId = "creator";
  mocks.openModal.mockReset();
  mocks.copyText.mockReset().mockResolvedValue(true);
  mocks.api.updateSquad.mockReset().mockResolvedValue(SQUAD);
  mocks.api.deleteSquad.mockReset().mockResolvedValue(undefined);
  mocks.api.removeSquadMember.mockReset().mockResolvedValue(undefined);
  mocks.api.updateAgent.mockReset().mockResolvedValue({});
  mocks.api.setSquadRuntime.mockReset().mockResolvedValue({ runtime_id: "rt-2", updated: [], unchanged: [], skipped: [] });
});

describe("SquadDetailPage", () => {
  it("puts the squads list in the breadcrumb and the squad in the leaf", () => {
    renderPage();
    const header = screen.getByRole("banner");
    expect(within(header).getByRole("link", { name: "Squads" })).toHaveAttribute("href", "/acme/squads");
    expect(within(header).getByText("Strike Team")).toBeInTheDocument();
    expect(within(header).queryByRole("tab")).toBeNull();
  });

  it("assigns work to the squad from the header's one solid action", async () => {
    const user = userEvent.setup();
    renderPage();

    await user.click(screen.getByRole("button", { name: "Assign work" }));
    expect(mocks.openModal).toHaveBeenCalledWith("create-issue", {
      assignee_type: "squad",
      assignee_id: "sq-1",
    });
  });

  it("keeps the other page actions in the more menu", async () => {
    const user = userEvent.setup();
    const { push } = renderPage();

    await user.click(screen.getByRole("button", { name: "More actions" }));
    await user.click(await screen.findByRole("menuitem", { name: "Copy link" }));
    expect(mocks.copyText).toHaveBeenCalledWith("https://multica.test/acme/squads/sq-1");

    await user.click(screen.getByRole("button", { name: "More actions" }));
    await user.click(await screen.findByRole("menuitem", { name: "Create Agent" }));
    expect(push).toHaveBeenCalledWith("/acme/agents/new?squad=sq-1");

    await user.click(screen.getByRole("button", { name: "More actions" }));
    await user.click(await screen.findByRole("menuitem", { name: "Archive" }));
    await user.click(await screen.findByRole("button", { name: "Archive" }));
    expect(mocks.api.deleteSquad).toHaveBeenCalledWith("sq-1");
  });

  it("shows identity, members, instructions and recent work as sections", () => {
    renderPage();

    expect(screen.getByRole("heading", { level: 1, name: "Strike Team" })).toBeInTheDocument();
    expect(screen.getByText("Ships the risky stuff")).toBeInTheDocument();

    const members = section("Members");
    expect(within(members).getByText("3")).toBeInTheDocument();
    expect(within(members).getByRole("button", { name: "Add member" })).toBeInTheDocument();
    expect(within(members).getByText("Lambda")).toBeInTheDocument();
    expect(within(members).getByText("Leader")).toBeInTheDocument();
    expect(within(members).getByText("Working on")).toBeInTheDocument();
    expect(within(members).getByRole("link", { name: "ARIE-2" })).toHaveAttribute("href", "/acme/issues/i-1");

    expect(within(section("Instructions")).getByText("Start with a failing test.")).toBeInTheDocument();
    expect(within(section("Instructions")).getByRole("button", { name: "Edit" })).toBeInTheDocument();

    const recent = section("Recent work");
    expect(within(recent).getByRole("link", { name: /Fix login/ })).toHaveAttribute("href", "/acme/issues/i-1");
  });

  it("keeps per-member actions in the row menu", async () => {
    const user = userEvent.setup();
    const { push } = renderPage();

    await user.click(screen.getByRole("button", { name: "Actions for Kappa" }));
    await user.click(await screen.findByRole("menuitem", { name: "Make squad leader" }));
    expect(mocks.api.updateSquad).toHaveBeenCalledWith("sq-1", { leader_id: "agent-2" });

    await user.click(screen.getByRole("button", { name: "Actions for Kappa" }));
    await user.click(await screen.findByRole("menuitem", { name: "Open agent detail" }));
    expect(push).toHaveBeenCalledWith("/acme/agents/agent-2");

    await user.click(screen.getByRole("button", { name: "Actions for Jiayuan" }));
    await user.click(await screen.findByRole("menuitem", { name: "Remove from squad" }));
    expect(mocks.api.removeSquadMember).toHaveBeenCalledWith("sq-1", {
      member_type: "member",
      member_id: "creator",
    });
  });

  it("edits instructions in place", async () => {
    const user = userEvent.setup();
    renderPage();

    await user.click(within(section("Instructions")).getByRole("button", { name: "Edit" }));
    const editor = screen.getByRole("textbox", { name: "Instructions editor" });
    await user.clear(editor);
    await user.type(editor, "Ship small.");
    await user.click(screen.getByRole("button", { name: "Save" }));
    expect(mocks.api.updateSquad).toHaveBeenCalledWith("sq-1", { instructions: "Ship small." });
  });

  it("lists the squad's properties in the panel", () => {
    renderPage();

    expect(screen.getByRole("button", { name: "Toggle right sidebar" })).toBeInTheDocument();
    expect(screen.getByText("Properties")).toBeInTheDocument();
    expect(screen.getByText("Visibility")).toBeInTheDocument();
    expect(screen.getByText("Workspace")).toBeInTheDocument();
    expect(screen.getByText("3 members")).toBeInTheDocument();
    expect(screen.getByText("Created by")).toBeInTheDocument();
  });

  it("switches one agent's runtime from its row", async () => {
    const user = userEvent.setup();
    renderPage();

    await user.click(screen.getByRole("button", { name: "Runtime for Kappa" }));
    await user.click(await screen.findByRole("menuitemradio", { name: "Workstation" }));

    expect(mocks.api.updateAgent).toHaveBeenCalledTimes(1);
    expect(mocks.api.updateAgent).toHaveBeenCalledWith("agent-2", { runtime_id: "rt-2" });
  });

  it("applies one runtime to every agent from the squad dropdown only after confirmation", async () => {
    const user = userEvent.setup();
    renderPage();

    await user.click(screen.getByRole("button", { name: "Runtime for the whole squad" }));
    await user.click(await screen.findByRole("menuitemradio", { name: "Workstation" }));

    expect(await screen.findByRole("alertdialog")).toHaveTextContent("Apply to all agents?");
    expect(mocks.api.setSquadRuntime).not.toHaveBeenCalled();

    await user.click(screen.getByRole("button", { name: "Apply to all" }));

    expect(mocks.api.setSquadRuntime).toHaveBeenCalledTimes(1);
    expect(mocks.api.setSquadRuntime).toHaveBeenCalledWith("sq-1", "rt-2");
    expect(mocks.api.updateAgent).not.toHaveBeenCalled();
  });

  it("does not apply a squad-wide change when the confirmation is cancelled", async () => {
    const user = userEvent.setup();
    renderPage();

    await user.click(screen.getByRole("button", { name: "Runtime for the whole squad" }));
    await user.click(await screen.findByRole("menuitemradio", { name: "Workstation" }));
    await user.click(await screen.findByRole("button", { name: "Cancel" }));

    expect(mocks.api.setSquadRuntime).not.toHaveBeenCalled();
  });

  it("shows Custom in the squad header when agents run different runtimes", () => {
    seed(SQUAD, {
      agents: {
        data: [
          { id: "agent-lead", name: "Lambda", archived_at: null, runtime_id: "rt-1", model: "" },
          { id: "agent-2", name: "Kappa", archived_at: null, runtime_id: "rt-2", model: "" },
        ],
      },
    });
    renderPage();

    expect(screen.getByRole("button", { name: "Runtime for the whole squad" })).toHaveTextContent("Custom");
  });

  it("keeps the model control empty and disabled until the agent has a runtime", () => {
    seed(SQUAD, {
      agents: {
        data: [
          { id: "agent-lead", name: "Lambda", archived_at: null, runtime_id: "rt-1", model: "" },
          { id: "agent-2", name: "Kappa", archived_at: null, runtime_id: "", model: "" },
        ],
      },
    });
    renderPage();

    const control = screen.getByRole("button", { name: "Model for Kappa" });
    expect(control).toBeDisabled();
    expect(control).toHaveTextContent("");
  });

  it("picks several skills at once, with select-all acting on the filtered rows", async () => {
    const user = userEvent.setup();
    seed(SQUAD, {
      skills: {
        data: [
          { id: "sk-2", name: "lint-rules", description: "Lint" },
          { id: "sk-3", name: "deploy-notes", description: "Ship" },
        ],
      },
    });
    renderPage();

    await user.click(within(section("Skills & plugins")).getByRole("button", { name: "Add skill" }));
    await user.type(screen.getByRole("textbox", { name: "Search" }), "lint");

    // The filter leaves one row, so select-all reads 1 and picks just that row.
    await user.click(screen.getByRole("checkbox", { name: "Select all (1)" }));
    await user.click(screen.getByRole("button", { name: "Add 1 selected" }));

    expect(mocks.api.setSquadSkills).toHaveBeenCalledWith("sq-1", ["sk-2"]);
  });

  it("offers only runtimes the viewer can bind", async () => {
    const user = userEvent.setup();
    renderPage();

    await user.click(screen.getByRole("button", { name: "Runtime for Kappa" }));

    expect(await screen.findByRole("menuitemradio", { name: "Laptop" })).toBeInTheDocument();
    expect(screen.queryByRole("menuitemradio", { name: "Not mine" })).not.toBeInTheDocument();
  });

  it("lists the squad's skills and plugins and removes a skill", async () => {
    const user = userEvent.setup();
    seed(SQUAD, {
      "squad-skills": { data: [{ id: "sk-1", name: "code-style", description: "House style" }] },
      "squad-plugins": { data: [{ name: "bmad-method", install_hint: "npx bmad-method install" }] },
    });
    renderPage();

    const capabilities = section("Skills & plugins");
    expect(within(capabilities).getByText("code-style")).toBeInTheDocument();
    expect(within(capabilities).getByText("bmad-method")).toBeInTheDocument();
    expect(within(capabilities).getByText("npx bmad-method install")).toBeInTheDocument();

    await user.click(within(capabilities).getByRole("button", { name: "Remove code-style" }));

    expect(mocks.api.setSquadSkills).toHaveBeenCalledWith("sq-1", []);
  });

  it("gives viewers who cannot manage the squad a read-only page", async () => {
    const user = userEvent.setup();
    mocks.userId = "viewer";
    renderPage();

    expect(screen.getByRole("button", { name: "Assign work" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Add member" })).toBeNull();
    expect(within(section("Instructions")).queryByRole("button", { name: "Edit" })).toBeNull();
    expect(screen.queryByTestId("avatar-upload")).toBeNull();

    await user.click(screen.getByRole("button", { name: "More actions" }));
    expect(await screen.findByRole("menuitem", { name: "Copy link" })).toBeInTheDocument();
    expect(screen.queryByRole("menuitem", { name: "Archive" })).toBeNull();
  });

  it("drops assigning and editing on an archived squad", () => {
    seed({ ...SQUAD, archived_at: "2026-10-03T00:00:00Z" });
    renderPage();

    expect(screen.queryByRole("button", { name: "Assign work" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Add member" })).toBeNull();
    expect(screen.getAllByText("Archived").length).toBeGreaterThan(0);
  });

  it("ignores a legacy ?tab= link", () => {
    renderPage("tab=instructions");
    expect(screen.getByRole("heading", { level: 1, name: "Strike Team" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { level: 2, name: "Members" })).toBeInTheDocument();
  });

  it("shows not found in the body when the squad fails to load", () => {
    seed(undefined, { "squads/sq-1": { data: undefined, isError: true } });
    renderPage();
    expect(screen.getByRole("link", { name: "Squads" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Squad not found" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "All squads" })).toBeInTheDocument();
  });
});
