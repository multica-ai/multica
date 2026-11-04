/**
 * @vitest-environment jsdom
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import userEvent from "@testing-library/user-event";
import { renderWithI18n } from "../test/i18n";
import { GovernanceGoalCreateDialog } from "./governance-goal-create-dialog";

const api = vi.hoisted(() => ({
  createGoal: vi.fn(),
  listMetrics: vi.fn(),
  listOfficeFloors: vi.fn(),
  listInitiatives: vi.fn(),
  listSquads: vi.fn(),
  listAgents: vi.fn(),
  listMembers: vi.fn(),
}));
const toasts = vi.hoisted(() => ({ success: vi.fn(), error: vi.fn() }));

vi.mock("sonner", () => ({ toast: toasts }));
vi.mock("@multica/core/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/api")>()),
  api,
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));

function renderDialog(onOpenChange = vi.fn()) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  renderWithI18n(
    <QueryClientProvider client={queryClient}>
      <GovernanceGoalCreateDialog open onOpenChange={onOpenChange} />
    </QueryClientProvider>,
  );
  return { onOpenChange };
}

/** Opens a select by its accessible name and picks one option. Base UI reacts to pointer events, so this uses user-event. */
async function pick(user: ReturnType<typeof userEvent.setup>, name: string, option: string) {
  await user.click(screen.getByRole("combobox", { name }));
  await user.click(await screen.findByRole("option", { name: option }, { timeout: 3000 }));
  // The list closes before the next select can take pointer input.
  await waitFor(() => expect(screen.queryByRole("listbox")).toBeNull());
}

function fill(label: string, value: string) {
  fireEvent.change(screen.getByLabelText(label), { target: { value } });
}

describe("GovernanceGoalCreateDialog", { timeout: 30_000 }, () => {
  beforeEach(() => {
    api.createGoal.mockReset();
    toasts.success.mockReset();
    toasts.error.mockReset();
    api.listMetrics.mockResolvedValue([
      { key: "mrr", name: "Monthly recurring revenue", unit: "USD", source: "manual", direction: "increase", description: "", created_at: "" },
    ]);
    api.listOfficeFloors.mockResolvedValue([]);
    api.listInitiatives.mockResolvedValue([]);
    api.listSquads.mockResolvedValue([]);
    api.listAgents.mockResolvedValue([]);
    api.listMembers.mockResolvedValue([
      { id: "m-1", workspace_id: "ws-1", user_id: "u-1", role: "owner", created_at: "", name: "Ada", email: "ada@example.com", avatar_url: null },
    ]);
  });
  afterEach(cleanup);

  it("creates a company goal with its key result and closes once the server keeps it", async () => {
    const user = userEvent.setup({ delay: null });
    api.createGoal.mockResolvedValue({ id: "g-1" });
    const { onOpenChange } = renderDialog();

    fill("Title", "Grow revenue");
    fill("Starts", "2026-10-01");
    fill("Ends", "2026-12-31");
    await pick(user, "Owner type", "Member");
    await pick(user, "Owner", "Ada");
    await user.click(screen.getByRole("button", { name: "Add key result" }));
    fill("Key result 1", "Reach 1000 MRR");
    await pick(user, "Metric 1", "Monthly recurring revenue");
    fill("Start value", "0");
    fill("Target value", "1000");
    await user.click(screen.getByRole("button", { name: "Create goal" }));

    await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false));
    expect(api.createGoal).toHaveBeenCalledWith(
      expect.objectContaining({
        scope_type: "company",
        title: "Grow revenue",
        why: "",
        period_start: "2026-10-01",
        period_end: "2026-12-31",
        owner_type: "member",
        owner_id: "u-1",
        key_results: [{ title: "Reach 1000 MRR", metric_key: "mrr", start_value: 0, target_value: 1000 }],
      }),
    );
    expect(toasts.success).toHaveBeenCalledWith("Goal created");
  });

  it("creates a goal with no key results yet; progress is not set until one is added", async () => {
    const user = userEvent.setup({ delay: null });
    api.createGoal.mockResolvedValue({ id: "g-2" });
    const { onOpenChange } = renderDialog();

    fill("Title", "Grow revenue");
    fill("Starts", "2026-10-01");
    fill("Ends", "2026-12-31");
    await pick(user, "Owner type", "Member");
    await pick(user, "Owner", "Ada");
    await user.click(screen.getByRole("button", { name: "Create goal" }));

    await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false));
    expect(api.createGoal).toHaveBeenCalledWith(expect.objectContaining({ title: "Grow revenue", key_results: [] }));
  });

  it("names what is missing and sends nothing", async () => {
    const user = userEvent.setup({ delay: null });
    api.createGoal.mockResolvedValue({ id: "g-1" });
    const { onOpenChange } = renderDialog();

    await user.click(screen.getByRole("button", { name: "Create goal" }));

    expect(await screen.findByText("Give the goal a title.")).toBeTruthy();
    expect(api.createGoal).not.toHaveBeenCalled();
    expect(onOpenChange).not.toHaveBeenCalled();
  });

  it("refuses a target that equals its start, before the server does", async () => {
    const user = userEvent.setup({ delay: null });
    renderDialog();

    fill("Title", "Grow revenue");
    fill("Starts", "2026-10-01");
    fill("Ends", "2026-12-31");
    await pick(user, "Owner type", "Member");
    await pick(user, "Owner", "Ada");
    await user.click(screen.getByRole("button", { name: "Add key result" }));
    fill("Key result 1", "Hold");
    await pick(user, "Metric 1", "Monthly recurring revenue");
    fill("Start value", "5");
    fill("Target value", "5");
    await user.click(screen.getByRole("button", { name: "Create goal" }));

    expect(await screen.findByText("The target must differ from the start.")).toBeTruthy();
    expect(api.createGoal).not.toHaveBeenCalled();
  });

  it("keeps what was typed and says so when the server refuses it", async () => {
    const user = userEvent.setup({ delay: null });
    api.createGoal.mockRejectedValue(new Error("bad request"));
    const { onOpenChange } = renderDialog();

    fill("Title", "Grow revenue");
    fill("Starts", "2026-10-01");
    fill("Ends", "2026-12-31");
    await pick(user, "Owner type", "Member");
    await pick(user, "Owner", "Ada");
    await user.click(screen.getByRole("button", { name: "Add key result" }));
    fill("Key result 1", "Reach 1000 MRR");
    await pick(user, "Metric 1", "Monthly recurring revenue");
    fill("Start value", "0");
    fill("Target value", "1000");
    await user.click(screen.getByRole("button", { name: "Create goal" }));

    expect(await screen.findByText("Couldn't create the goal. Check the fields and try again.")).toBeTruthy();
    expect(onOpenChange).not.toHaveBeenCalled();
    expect((screen.getByLabelText("Title") as HTMLInputElement).value).toBe("Grow revenue");
  });
});
