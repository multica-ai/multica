// @vitest-environment jsdom

import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, describe, expect, it, vi } from "vitest";
import { buildIssueStatusCatalog } from "@multica/core/issue-statuses";
import { agentListOptions, memberListOptions } from "@multica/core/workspace/queries";
import { projectListOptions } from "@multica/core/projects/queries";
import { renderWithI18n } from "../test/i18n";
import { WorkflowEditorPage } from "./workflow-editor-page";

const { createWorkflow } = vi.hoisted(() => ({ createWorkflow: vi.fn() }));

vi.mock("@multica/core/api", () => ({ api: {}, ApiError: class extends Error {}, clientErrorMessage: () => null }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/paths", () => ({ useCurrentWorkspace: () => ({ id: "ws-1", name: "Acme" }) }));
vi.mock("@multica/core/issue-statuses/hooks", () => ({ useIssueStatuses: () => buildIssueStatusCatalog([]) }));
vi.mock("@multica/core/workspace/hooks", () => ({ useActorName: () => ({ getActorName: () => "Builder" }) }));
vi.mock("@multica/core/issue-workflows", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/issue-workflows")>()),
  useIssueWorkflows: () => ({ workflows: [], isLoaded: true }),
  useCreateIssueWorkflow: () => ({ mutateAsync: createWorkflow, isPending: false }),
  useUpdateIssueWorkflow: () => ({ mutateAsync: vi.fn(), isPending: false }),
  useDeleteIssueWorkflow: () => ({ mutateAsync: vi.fn(), isPending: false }),
}));
vi.mock("../common/actor-avatar", () => ({
  ActorAvatar: ({ actorId }: { actorId: string }) => <span data-testid={`avatar-${actorId}`} />,
}));

function renderEditor(props: { template?: "content"; canCreateStatus?: boolean } = {}) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  qc.setQueryData(projectListOptions("ws-1").queryKey, [] as never);
  qc.setQueryData(agentListOptions("ws-1").queryKey, [{ id: "agent-1", name: "Builder", archived_at: null }] as never);
  qc.setQueryData(memberListOptions("ws-1").queryKey, [] as never);
  renderWithI18n(
    <QueryClientProvider client={qc}>
      <WorkflowEditorPage workflowId={null} seed={null} canEdit onBack={vi.fn()} onOpen={vi.fn()} {...props} />
    </QueryClientProvider>,
  );
}

afterEach(() => {
  cleanup();
  createWorkflow.mockReset();
});

describe("WorkflowEditorPage", () => {
  it("edits a new status step's handoff before the save (MUL-8118)", async () => {
    createWorkflow.mockResolvedValue({ id: "wf-1" });
    renderEditor();

    fireEvent.click(screen.getByRole("button", { name: /Add step/ }));
    fireEvent.click(await screen.findByRole("menuitem", { name: /New status/ }));
    const input = screen.getByPlaceholderText("Status name");
    fireEvent.change(input, { target: { value: "Deploy" } });
    fireEvent.keyDown(input, { key: "Enter" });

    // The new step is selected, and its handoff can be edited right away.
    expect(screen.getByRole("heading", { name: "Deploy" })).toBeTruthy();
    const agent = screen.getByRole("radio", { name: /Hand to an agent/ });
    expect((agent as HTMLButtonElement).disabled).toBe(false);
    fireEvent.click(agent);
    fireEvent.click(screen.getByRole("button", { name: "Choose an agent" }));
    fireEvent.click(await screen.findByText("Builder"));

    fireEvent.change(screen.getByRole("textbox", { name: "Name" }), { target: { value: "Release" } });
    fireEvent.click(screen.getByRole("button", { name: "Create" }));

    await waitFor(() => expect(createWorkflow).toHaveBeenCalled());
    const steps = createWorkflow.mock.calls[0]![0].steps as unknown[];
    expect(steps).toContainEqual({
      new_status: { name: "Deploy" },
      handler: { type: "agent", id: "agent-1" },
      instructions: "",
    });
  });

  it("resolves a default next over the unsaved steps, as the save will", async () => {
    renderEditor();
    await addNewStatus("QA");
    await addNewStatus("Deploy");

    // QA is followed by Deploy on the board, so that is where its agent goes
    // next — not Done, the next status that exists before the save.
    fireEvent.click(stepCard("QA"));
    fireEvent.click(screen.getByRole("radio", { name: /Hand to an agent/ }));
    fireEvent.click(screen.getByRole("button", { name: "Choose an agent" }));
    fireEvent.click(await screen.findByText("Builder"));

    expect(screen.getByRole("combobox", { name: "When done, move to" }).textContent).toContain("Next column (Deploy)");
    expect(stepCard("QA").textContent).toContain("then → Deploy");
    // The explicit choices still list only statuses that exist before the save.
    fireEvent.click(screen.getByRole("combobox", { name: "When done, move to" }));
    expect(screen.queryByRole("option", { name: "Deploy" })).toBeNull();
  });
});

// Statuses are shared by every board, so only admins add them (MUL-8120).
describe("WorkflowEditorPage status creation", () => {
  it("blocks saving a template that needs new statuses for a member", () => {
    renderEditor({ template: "content", canCreateStatus: false });
    expect(screen.getByRole("alert").textContent).toMatch(/Only workspace admins can add statuses/);
    expect(screen.getByRole("button", { name: "Create" }).hasAttribute("disabled")).toBe(true);
  });

  it("lets an admin save the same template", () => {
    renderEditor({ template: "content", canCreateStatus: true });
    expect(screen.queryByRole("alert")).toBeNull();
    expect(screen.getByRole("button", { name: "Create" }).hasAttribute("disabled")).toBe(false);
  });
});

/** A step in the list (the detail panel's status picker shares its name). */
function stepCard(name: string) {
  const card = screen
    .getAllByRole("button", { name: new RegExp(`^${name}`) })
    .find((b) => b.hasAttribute("aria-pressed"));
  if (!card) throw new Error(`no step card ${name}`);
  return card;
}

async function addNewStatus(name: string) {
  fireEvent.click(screen.getByRole("button", { name: /Add step/ }));
  fireEvent.click(await screen.findByRole("menuitem", { name: /New status/ }));
  const input = screen.getByPlaceholderText("Status name");
  fireEvent.change(input, { target: { value: name } });
  fireEvent.keyDown(input, { key: "Enter" });
}
