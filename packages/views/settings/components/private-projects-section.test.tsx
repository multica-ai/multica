import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { setApiInstance } from "@multica/core/api";
import type { ApiClient } from "@multica/core/api/client";
import type { PrivateProjectOverview } from "@multica/core/types";
import { EMPTY_PROJECT } from "@multica/core/api/schemas";
import { renderWithI18n } from "../../test/i18n";
import { PrivateProjectsSection } from "./private-projects-section";

const toast = vi.hoisted(() => ({ success: vi.fn(), error: vi.fn() }));
vi.mock("sonner", () => ({ toast }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws" }));
vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({ projectDetail: (id: string) => `/acme/projects/${id}` }),
}));
vi.mock("../../navigation", () => ({
  AppLink: ({ href, children, ...props }: { href: string; children?: React.ReactNode }) => (
    <a href={href} {...props}>{children}</a>
  ),
}));

const alice = { user_id: "alice", name: "Alice", email: "alice@test", avatar_url: null };
const bob = { user_id: "bob", name: "Bob", email: "bob@test", avatar_url: null };
const overview = (patch: Partial<PrivateProjectOverview>): PrivateProjectOverview => ({
  id: "p1", title: "Secret Plan", icon: null, lead_type: "member", lead_id: "alice",
  created_at: "", archived_at: null, is_member: false, members: [alice, bob], ...patch,
});

let qc: QueryClient;
let projects: PrivateProjectOverview[];
let api: { listPrivateProjectsForOwner: ReturnType<typeof vi.fn>; joinPrivateProject: ReturnType<typeof vi.fn> };

function renderSection() {
  return renderWithI18n(<QueryClientProvider client={qc}><PrivateProjectsSection /></QueryClientProvider>);
}

beforeEach(() => {
  toast.success.mockClear();
  toast.error.mockClear();
  qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  projects = [
    overview({}),
    // The lead is listed first wherever the server put them.
    overview({ id: "p2", title: "Board Notes", is_member: true, members: [{ ...bob, user_id: "me", name: "Me" }, alice] }),
  ];
  api = {
    listPrivateProjectsForOwner: vi.fn().mockImplementation(async () => ({ projects, total: projects.length })),
    joinPrivateProject: vi.fn().mockImplementation(async (id: string) => {
      projects = projects.map((project) => (project.id === id ? { ...project, is_member: true } : project));
      return { ...EMPTY_PROJECT, id };
    }),
  };
  setApiInstance(api as unknown as ApiClient);
});
afterEach(() => { qc.clear(); });

it("lists every private project with its members and marks the lead", async () => {
  renderSection();
  expect(await screen.findByText("Secret Plan")).toBeVisible();
  expect(screen.getByText("Alice (lead), Bob")).toBeVisible();
  expect(screen.getByText("Alice (lead), Me")).toBeVisible();
  // Already a member: a way in, and nothing to join.
  expect(screen.getByRole("button", { name: "Open" })).toHaveAttribute("href", "/acme/projects/p2");
  expect(screen.getAllByRole("button", { name: "Join" })).toHaveLength(1);
});

it("joins only after confirmation, and says existing members are told", async () => {
  const user = userEvent.setup();
  renderSection();
  await user.click(await screen.findByRole("button", { name: "Join" }));
  const dialog = await screen.findByRole("alertdialog");
  expect(within(dialog).getByText(/current members will be notified/)).toBeVisible();
  expect(within(dialog).queryByText(/become its lead/)).not.toBeInTheDocument();
  expect(api.joinPrivateProject).not.toHaveBeenCalled();

  await user.click(within(dialog).getByRole("button", { name: "Cancel" }));
  await waitFor(() => expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument());
  expect(api.joinPrivateProject).not.toHaveBeenCalled();

  await user.click(screen.getByRole("button", { name: "Join" }));
  await user.click(within(await screen.findByRole("alertdialog")).getByRole("button", { name: "Join project" }));
  await waitFor(() => expect(api.joinPrivateProject).toHaveBeenCalledWith("p1"));
  await waitFor(() => expect(toast.success).toHaveBeenCalledWith("You joined Secret Plan"));
  await waitFor(() => expect(screen.queryByRole("button", { name: "Join" })).not.toBeInTheDocument());
  expect(screen.getAllByRole("button", { name: "Open" })).toHaveLength(2);
});

it("says what joining an empty project means, and promises no lead either way", async () => {
  projects = [
    overview({ lead_type: "member", lead_id: "gone" }),
    overview({ id: "p3", title: "Empty Room", lead_type: null, lead_id: null, members: [] }),
  ];
  const user = userEvent.setup();
  renderSection();
  const [leaderless, empty] = await screen.findAllByRole("button", { name: "Join" });
  expect(screen.getByText("No members")).toBeVisible();

  await user.click(leaderless!);
  let dialog = await screen.findByRole("alertdialog");
  expect(within(dialog).getByText(/Its current members will be notified/)).toBeVisible();
  expect(within(dialog).queryByText(/lead/)).not.toBeInTheDocument();
  await user.click(within(dialog).getByRole("button", { name: "Cancel" }));
  await waitFor(() => expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument());

  await user.click(empty!);
  dialog = await screen.findByRole("alertdialog");
  expect(within(dialog).getByText(/You will be its only member/)).toBeVisible();
  expect(within(dialog).queryByText(/lead/)).not.toBeInTheDocument();
});

it("keeps the confirmation open and locked while the join is in flight", async () => {
  let finish!: () => void;
  const join = api.joinPrivateProject.getMockImplementation() as (id: string) => Promise<unknown>;
  api.joinPrivateProject.mockImplementationOnce((id: string) =>
    new Promise((resolve) => { finish = () => resolve(join(id)); }));
  const user = userEvent.setup();
  renderSection();
  await user.click(await screen.findByRole("button", { name: "Join" }));
  const dialog = await screen.findByRole("alertdialog");
  const confirm = within(dialog).getByRole("button", { name: "Join project" });
  await user.click(confirm);
  await waitFor(() => expect(confirm).toBeDisabled());
  expect(within(dialog).getByRole("button", { name: "Cancel" })).toBeDisabled();
  await user.click(confirm);
  await user.keyboard("{Escape}");
  expect(screen.getByRole("alertdialog")).toBeVisible();
  expect(api.joinPrivateProject).toHaveBeenCalledTimes(1);

  finish();
  await waitFor(() => expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument());
  expect(toast.success).toHaveBeenCalledWith("You joined Secret Plan");
});

it("reports a failed join and keeps the confirmation open to retry in place", async () => {
  api.joinPrivateProject.mockRejectedValueOnce(new Error("Join denied"));
  const user = userEvent.setup();
  renderSection();
  await user.click(await screen.findByRole("button", { name: "Join" }));
  const dialog = await screen.findByRole("alertdialog");
  await user.click(within(dialog).getByRole("button", { name: "Join project" }));
  await waitFor(() => expect(toast.error).toHaveBeenCalledWith("Join denied"));
  expect(screen.getByRole("alertdialog")).toBeVisible();

  await user.click(within(dialog).getByRole("button", { name: "Join project" }));
  await waitFor(() => expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument());
  expect(api.joinPrivateProject).toHaveBeenCalledTimes(2);
  expect(toast.success).toHaveBeenCalledWith("You joined Secret Plan");
});

it("says so when there are no private projects, and offers a retry when loading fails", async () => {
  projects = [];
  const { unmount } = renderSection();
  expect(await screen.findByText("This workspace has no private projects.")).toBeVisible();
  unmount();
  qc.clear();

  api.listPrivateProjectsForOwner.mockRejectedValueOnce(new Error("boom"));
  renderSection();
  await userEvent.click(await screen.findByRole("button", { name: "Retry" }));
  expect(await screen.findByText("This workspace has no private projects.")).toBeVisible();
});
