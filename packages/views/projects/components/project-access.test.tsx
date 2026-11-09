import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { configStore } from "@multica/core/config";
import { setApiInstance } from "@multica/core/api";
import type { ApiClient } from "@multica/core/api/client";
import type { Project, MemberWithUser } from "@multica/core/types";
import { EMPTY_PROJECT } from "@multica/core/api/schemas";
import { renderWithI18n } from "../../test/i18n";
import { ProjectAccessControl, ProjectAccessDraftPicker } from "./project-access";

const state = vi.hoisted(() => ({ viewer: "owner", push: vi.fn() }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws" }));
vi.mock("@multica/core/auth", () => ({ useAuthStore: Object.assign((selector: (s: { user: { id: string } }) => unknown) => selector({ user: { id: state.viewer } }), { getState: () => ({ user: { id: state.viewer } }) }) }));
vi.mock("@multica/core/paths", () => ({ useWorkspacePaths: () => ({ projects: () => "/dev/projects" }) }));
vi.mock("../../navigation", () => ({ useNavigation: () => ({ push: state.push }) }));
vi.mock("../../common/actor-avatar", () => ({ ActorAvatar: () => <span aria-hidden="true" /> }));

const members: MemberWithUser[] = [
  { id: "1", workspace_id: "ws", user_id: "owner", name: "Owner", email: "owner@test", role: "owner", avatar_url: null, created_at: "" },
  { id: "2", workspace_id: "ws", user_id: "lead", name: "Lead", email: "lead@test", role: "member", avatar_url: null, created_at: "" },
  { id: "3", workspace_id: "ws", user_id: "member", name: "Member", email: "member@test", role: "member", avatar_url: null, created_at: "" },
  { id: "4", workspace_id: "ws", user_id: "outsider", name: "Outside Person", email: "outside@test", role: "member", avatar_url: null, created_at: "" },
];
const project = { ...EMPTY_PROJECT, id: "p1", workspace_id: "ws", title: "Secret Plan", visibility: "private" as const, lead_type: "member" as const, lead_id: "lead" };
let qc: QueryClient;
let ids: string[];
let api: { listMembers: ReturnType<typeof vi.fn>; getProjectMembers: ReturnType<typeof vi.fn>; addProjectMembers: ReturnType<typeof vi.fn>; removeProjectMember: ReturnType<typeof vi.fn>; updateProject: ReturnType<typeof vi.fn> };

function renderControl(value: Project = project) {
  return renderWithI18n(<QueryClientProvider client={qc}><ProjectAccessControl project={value} /></QueryClientProvider>);
}

beforeEach(() => {
  state.viewer = "owner";
  state.push.mockReset();
  configStore.getState().setFeatureFlags({ private_projects_v1: true });
  qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  ids = ["owner", "lead", "member"];
  const response = () => ({ members: members.filter((m) => ids.includes(m.user_id)).map((m) => ({ ...m, project_id: "p1" })), total: ids.length });
  api = {
    listMembers: vi.fn().mockResolvedValue(members),
    getProjectMembers: vi.fn().mockImplementation(async () => response()),
    addProjectMembers: vi.fn().mockImplementation(async (_id, added: string[]) => { ids = [...ids, ...added]; return response(); }),
    removeProjectMember: vi.fn().mockImplementation(async (_id, removed: string) => { ids = ids.filter((id) => id !== removed); }),
    updateProject: vi.fn().mockResolvedValue(project),
  };
  setApiInstance(api as unknown as ApiClient);
});
afterEach(() => { qc.clear(); configStore.getState().setFeatureFlags({}); });

it("loads the member list, finds people by email, and adds and removes only after success", async () => {
  const user = userEvent.setup();
  renderControl();
  await user.click(screen.getByRole("button", { name: "Access" }));
  await screen.findByText("member@test");
  expect(screen.getByText(/owners and admins always have access/)).toBeVisible();
  await user.click(screen.getByRole("button", { name: "Add workspace member" }));
  await user.type(screen.getByRole("combobox"), "outside@test");
  await user.click(screen.getByRole("option", { name: /Outside Person/ }));
  await waitFor(() => expect(api.addProjectMembers).toHaveBeenCalledWith("p1", ["outsider"]));
  await screen.findByRole("button", { name: "Remove Outside Person" });
  api.removeProjectMember.mockRejectedValueOnce(new Error("Remove denied"));
  await user.click(screen.getByRole("button", { name: "Remove Outside Person" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("Remove denied");
  expect(screen.getByText("outside@test")).toBeVisible();
  expect(screen.getByRole("dialog")).toBeVisible();
});

it("restricts scope changes and removal of other people for a regular project member", async () => {
  state.viewer = "member";
  const user = userEvent.setup();
  renderControl();
  await user.click(screen.getByRole("button", { name: "Access" }));
  await screen.findByText("member@test");
  expect(screen.getByRole("radio", { name: "Entire workspace" })).toBeDisabled();
  expect(screen.getByRole("button", { name: "Add workspace member" })).toBeEnabled();
  expect(screen.queryByRole("button", { name: "Remove Owner" })).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Remove Lead" })).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Remove Member" }));
  await waitFor(() => expect(state.push).toHaveBeenCalledWith("/dev/projects"));
  expect(api.removeProjectMember).toHaveBeenCalledWith("p1", "member");
});

it("requires confirmation before making private content visible to the workspace", async () => {
  const user = userEvent.setup();
  renderControl();
  await user.click(screen.getByRole("button", { name: "Access" }));
  await screen.findByText("member@test");
  await user.click(screen.getByRole("radio", { name: "Entire workspace" }));
  await user.click(screen.getByRole("button", { name: "Save" }));
  const confirmation = await screen.findByRole("alertdialog");
  expect(api.updateProject).not.toHaveBeenCalled();
  await user.click(within(confirmation).getByRole("button", { name: "Confirm" }));
  await waitFor(() => expect(api.updateProject).toHaveBeenCalledWith("p1", { visibility: "workspace", member_ids: ["owner", "lead", "member"] }));
});

it("keeps existing private memberships manageable with the creation flag off", async () => {
  configStore.getState().setFeatureFlags({ private_projects_v1: false });
  const user = userEvent.setup();
  renderControl();
  await user.click(screen.getByRole("button", { name: "Access" }));
  await screen.findByText("member@test");
  expect(screen.getByRole("button", { name: "Add workspace member" })).toBeEnabled();
});

it("does not show private creation controls when the backend has not enabled them", () => {
  configStore.getState().setFeatureFlags({});
  renderControl({ ...project, visibility: "workspace" });
  expect(screen.queryByRole("button", { name: "Access" })).not.toBeInTheDocument();
});

it("keeps new-project access in the draft until the project is created", async () => {
  const changed = vi.fn();
  const user = userEvent.setup();
  renderWithI18n(<ProjectAccessDraftPicker visibility="workspace" memberIds={[]} leadId="lead" members={members} onChange={changed} />);
  await user.click(screen.getByRole("button", { name: "Access" }));
  await user.click(screen.getByRole("radio", { name: "Project members only" }));
  expect(changed).toHaveBeenCalledWith("private", ["owner", "lead"]);
  expect(api.updateProject).not.toHaveBeenCalled();
  expect(api.addProjectMembers).not.toHaveBeenCalled();
});
