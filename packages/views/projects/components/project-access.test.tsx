import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { setApiInstance } from "@multica/core/api";
import type { ApiClient } from "@multica/core/api/client";
import type { Project, MemberWithUser } from "@multica/core/types";
import { EMPTY_PROJECT } from "@multica/core/api/schemas";
import { renderWithI18n } from "../../test/i18n";
import { ProjectAccessControl, ProjectAccessDraftPicker } from "./project-access";
import { ProjectLeadPicker } from "./project-lead-picker";

const state = vi.hoisted(() => ({ viewer: "owner" }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws" }));
vi.mock("@multica/core/auth", () => ({ useAuthStore: Object.assign((selector: (s: { user: { id: string } }) => unknown) => selector({ user: { id: state.viewer } }), { getState: () => ({ user: { id: state.viewer } }) }) }));
vi.mock("../../common/actor-avatar", () => ({ ActorAvatar: () => <span aria-hidden="true" /> }));
vi.mock("@multica/core/workspace/hooks", () => ({ useActorName: () => ({ getActorName: () => "Lead" }) }));

const members: MemberWithUser[] = [
  { id: "1", workspace_id: "ws", user_id: "owner", name: "Owner", email: "owner@test", role: "owner", avatar_url: null, created_at: "" },
  { id: "2", workspace_id: "ws", user_id: "lead", name: "Lead", email: "lead@test", role: "member", avatar_url: null, created_at: "" },
  { id: "3", workspace_id: "ws", user_id: "member", name: "Member", email: "member@test", role: "member", avatar_url: null, created_at: "" },
  { id: "4", workspace_id: "ws", user_id: "outsider", name: "Outside Person", email: "outside@test", role: "member", avatar_url: null, created_at: "" },
];
const project = { ...EMPTY_PROJECT, id: "p1", workspace_id: "ws", title: "Secret Plan", visibility: "private" as const, lead_type: "member" as const, lead_id: "lead" };
const agentLed = { ...project, lead_type: "agent" as const, lead_id: "a1" };
let qc: QueryClient;
let ids: string[];
// Who the server names as the project's access manager.
let managerId: string | null;
let api: { listMembers: ReturnType<typeof vi.fn>; listAgents: ReturnType<typeof vi.fn>; getProjectMembers: ReturnType<typeof vi.fn>; addProjectMembers: ReturnType<typeof vi.fn>; removeProjectMember: ReturnType<typeof vi.fn>; updateProject: ReturnType<typeof vi.fn> };

function renderControl(value: Project = project) {
  return renderWithI18n(<QueryClientProvider client={qc}><ProjectAccessControl project={value} /></QueryClientProvider>);
}

beforeEach(() => {
  state.viewer = "owner";
  qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  ids = ["owner", "lead", "member"];
  managerId = "lead";
  const response = () => ({ members: members.filter((m) => ids.includes(m.user_id)).map((m) => ({ ...m, project_id: "p1" })), total: ids.length, manager_id: managerId });
  api = {
    listMembers: vi.fn().mockResolvedValue(members),
    listAgents: vi.fn().mockResolvedValue([]),
    getProjectMembers: vi.fn().mockImplementation(async () => response()),
    addProjectMembers: vi.fn().mockImplementation(async (_id, added: string[]) => { ids = [...ids, ...added]; return response(); }),
    removeProjectMember: vi.fn().mockImplementation(async (_id, removed: string) => { ids = ids.filter((id) => id !== removed); }),
    updateProject: vi.fn().mockResolvedValue(project),
  };
  setApiInstance(api as unknown as ApiClient);
});
afterEach(() => { qc.clear(); });

it("shows both access notices only when project-members visibility is selected", async () => {
  const user = userEvent.setup();
  renderControl({ ...project, visibility: "workspace" });
  await user.click(screen.getByRole("button", { name: "Access" }));
  expect(await screen.findByRole("radio", { name: "Entire workspace" })).toBeChecked();
  expect(screen.queryByText(/Only the people listed here have access/)).not.toBeInTheDocument();
  expect(screen.queryByText(/Agents follow their owner/)).not.toBeInTheDocument();
  await user.click(screen.getByRole("radio", { name: "Project members only" }));
  expect(screen.getByText(/Only the people listed here have access/)).toBeVisible();
  expect(screen.getByText(/Agents follow their owner/)).toBeVisible();
  await user.click(screen.getByRole("radio", { name: "Entire workspace" }));
  expect(screen.queryByText(/Only the people listed here have access/)).not.toBeInTheDocument();
  expect(screen.queryByText(/Agents follow their owner/)).not.toBeInTheDocument();
  expect(api.listAgents).not.toHaveBeenCalled();
});

it("loads the member list, finds people by email, and adds and removes only after success", async () => {
  const user = userEvent.setup();
  renderControl();
  await user.click(screen.getByRole("button", { name: "Access" }));
  await screen.findByText("member@test");
  expect(screen.getByText(/Only the people listed here have access/)).toBeVisible();
  expect(screen.getByText("Agents follow their owner's access.")).toBeVisible();
  expect(api.listAgents).not.toHaveBeenCalled();
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

it("treats workspace owners and admins as ordinary project members", async () => {
  state.viewer = "lead";
  const user = userEvent.setup();
  renderControl();
  await user.click(screen.getByRole("button", { name: "Access" }));
  await screen.findByText("owner@test");
  await user.click(screen.getByRole("button", { name: "Remove Owner" }));
  await waitFor(() => expect(api.removeProjectMember).toHaveBeenCalledWith("p1", "owner"));
  await waitFor(() => expect(screen.queryByText("owner@test")).not.toBeInTheDocument());
});

it("keeps project membership read-only for a regular member, including their own membership", async () => {
  state.viewer = "member";
  const user = userEvent.setup();
  renderControl();
  await user.click(screen.getByRole("button", { name: "Access" }));
  await screen.findByText("member@test");
  expect(screen.getByRole("radio", { name: "Entire workspace" })).toBeDisabled();
  expect(screen.queryByRole("button", { name: "Add workspace member" })).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Remove Owner" })).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Remove Lead" })).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Remove Member" })).not.toBeInTheDocument();
  expect(api.removeProjectMember).not.toHaveBeenCalled();
});

it.each(["owner", "lead"])("allows %s to manage project members", async (viewer) => {
  state.viewer = viewer;
  renderControl();
  await userEvent.click(screen.getByRole("button", { name: "Access" }));
  await screen.findByText("member@test");
  expect(screen.getByRole("button", { name: "Add workspace member" })).toBeEnabled();
  expect(screen.getByRole("button", { name: "Remove Member" })).toBeEnabled();
  expect(screen.queryByRole("button", { name: "Remove Lead" })).not.toBeInTheDocument();
});

it.each(["owner", "lead", "member"])("guards the lead picker for %s", async (viewer) => {
  state.viewer = viewer;
  const update = vi.fn();
  renderWithI18n(<QueryClientProvider client={qc}><ProjectLeadPicker project={project} handleUpdate={update} renderTrigger={() => <button type="button">Change lead</button>} /></QueryClientProvider>);
  const trigger = screen.getByRole("button", { name: "Change lead" });
  if (viewer === "member") {
    expect(trigger).toBeDisabled();
    await userEvent.click(trigger);
    expect(screen.queryByPlaceholderText("Assign lead...")).not.toBeInTheDocument();
    expect(update).not.toHaveBeenCalled();
  } else {
    await waitFor(() => expect(trigger).toBeEnabled());
    await userEvent.click(trigger);
    await userEvent.click(screen.getByRole("button", { name: "Outside Person" }));
    expect(update).toHaveBeenCalledWith({ lead_type: "member", lead_id: "outsider" });
  }
});

it("offers agents and no lead on a private project as on any other", async () => {
  api.listAgents.mockResolvedValue([{ id: "a1", name: "Build Agent", archived_at: null }]);
  const update = vi.fn();
  renderWithI18n(<QueryClientProvider client={qc}><ProjectLeadPicker project={project} handleUpdate={update} renderTrigger={() => <button type="button">Change lead</button>} /></QueryClientProvider>);
  await waitFor(() => expect(screen.getByRole("button", { name: "Change lead" })).toBeEnabled());
  await userEvent.click(screen.getByRole("button", { name: "Change lead" }));
  expect(await screen.findByRole("button", { name: "Member" })).toBeVisible();
  await userEvent.click(await screen.findByRole("button", { name: "Build Agent" }));
  expect(update).toHaveBeenCalledWith({ lead_type: "agent", lead_id: "a1" });
  await userEvent.click(screen.getByRole("button", { name: "Change lead" }));
  await userEvent.click(await screen.findByRole("button", { name: "No lead" }));
  expect(update).toHaveBeenCalledWith({ lead_type: null, lead_id: null });
});

it("marks who manages access, and lets them manage a project an agent leads", async () => {
  managerId = "member";
  state.viewer = "member";
  const user = userEvent.setup();
  renderControl(agentLed);
  await user.click(screen.getByRole("button", { name: "Access" }));
  const row = (await screen.findByText("member@test")).parentElement!.parentElement!;
  expect(within(row).getByText("Manages access")).toBeVisible();
  expect(screen.getAllByText(/Manages access/)).toHaveLength(1);
  // "Lead" is also a member's name here; the mark is the caption beside one.
  expect(screen.queryByText("Lead", { selector: "span" })).not.toBeInTheDocument();
  expect(screen.getByRole("radio", { name: "Entire workspace" })).toBeEnabled();
  expect(screen.getByRole("button", { name: "Add workspace member" })).toBeEnabled();
  await user.click(screen.getByRole("button", { name: "Remove Lead" }));
  await waitFor(() => expect(api.removeProjectMember).toHaveBeenCalledWith("p1", "lead"));
});

it("keeps a project an agent leads read-only for members who do not manage it", async () => {
  managerId = "member";
  state.viewer = "lead";
  const user = userEvent.setup();
  renderControl(agentLed);
  await user.click(screen.getByRole("button", { name: "Access" }));
  const row = (await screen.findByText("member@test")).parentElement!.parentElement!;
  expect(within(row).getByText("Manages access")).toBeVisible();
  expect(screen.getByRole("radio", { name: "Entire workspace" })).toBeDisabled();
  expect(screen.queryByRole("button", { name: "Add workspace member" })).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Remove Member" })).not.toBeInTheDocument();
});

it("shows a person who leads the project as its manager too", async () => {
  const user = userEvent.setup();
  renderControl();
  await user.click(screen.getByRole("button", { name: "Access" }));
  const row = (await screen.findByText("lead@test")).parentElement!.parentElement!;
  expect(within(row).getByText("Lead · Manages access")).toBeVisible();
});

it.each(["member", "lead"])("opens the lead picker of an agent-led project to its manager only (%s)", async (viewer) => {
  managerId = "member";
  state.viewer = viewer;
  const update = vi.fn();
  renderWithI18n(<QueryClientProvider client={qc}><ProjectLeadPicker project={agentLed} handleUpdate={update} renderTrigger={() => <button type="button">Change lead</button>} /></QueryClientProvider>);
  const trigger = screen.getByRole("button", { name: "Change lead" });
  await waitFor(() => expect(api.getProjectMembers).toHaveBeenCalledWith("p1"));
  if (viewer === "member") {
    await waitFor(() => expect(trigger).toBeEnabled());
    await userEvent.click(trigger);
    await userEvent.click(screen.getByRole("button", { name: "Outside Person" }));
    expect(update).toHaveBeenCalledWith({ lead_type: "member", lead_id: "outsider" });
  } else {
    await waitFor(() => expect(qc.isFetching()).toBe(0));
    expect(trigger).toBeDisabled();
  }
});

it("asks nobody who manages a project when the viewer's role already settles it", async () => {
  renderWithI18n(<QueryClientProvider client={qc}><ProjectLeadPicker project={agentLed} handleUpdate={vi.fn()} renderTrigger={() => <button type="button">Change lead</button>} /></QueryClientProvider>);
  await waitFor(() => expect(screen.getByRole("button", { name: "Change lead" })).toBeEnabled());
  await waitFor(() => expect(qc.isFetching()).toBe(0));
  expect(api.getProjectMembers).not.toHaveBeenCalled();
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

it("names no lead for a project that turns private without one, and keeps whoever does it on the list", async () => {
  const user = userEvent.setup();
  renderControl({ ...project, visibility: "workspace", lead_type: null, lead_id: null });
  await user.click(screen.getByRole("button", { name: "Access" }));
  await user.click(await screen.findByRole("radio", { name: "Project members only" }));
  await screen.findByText("owner@test");
  expect(screen.queryByText("Lead", { selector: "span" })).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Remove Owner" })).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Save" }));
  await waitFor(() => expect(api.updateProject).toHaveBeenCalledWith("p1", expect.objectContaining({ visibility: "private" })));
  expect(api.updateProject.mock.calls[0]?.[1]).not.toHaveProperty("lead_id");
});

it("shows who will manage a new private project: a person named lead, otherwise its creator", async () => {
  const user = userEvent.setup();
  const { unmount } = renderWithI18n(<ProjectAccessDraftPicker visibility="private" memberIds={["member"]} leadId="lead" members={members} onChange={vi.fn()} />);
  await user.click(screen.getByRole("button", { name: "Access" }));
  let row = (await screen.findByText("lead@test")).parentElement!.parentElement!;
  expect(within(row).getByText("Lead · Manages access")).toBeVisible();
  unmount();

  renderWithI18n(<ProjectAccessDraftPicker visibility="private" memberIds={["member"]} members={members} onChange={vi.fn()} />);
  await user.click(screen.getByRole("button", { name: "Access" }));
  row = (await screen.findByText("owner@test")).parentElement!.parentElement!;
  expect(within(row).getByText("Manages access")).toBeVisible();
  expect(screen.queryByText(/Lead/, { selector: "span" })).not.toBeInTheDocument();
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
