/**
 * @vitest-environment jsdom
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { proposalKeys, type SquadAttention } from "@multica/core/office";
import { renderWithI18n } from "../../test/i18n";
import { OfficeSquadAttention } from "./office-squad-attention";

const mocks = vi.hoisted(() => ({
  get: vi.fn(),
  pin: vi.fn(),
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));

vi.mock("@multica/core/api", () => ({
  api: {
    getSquadAttention: mocks.get,
    setSquadAttentionPin: mocks.pin,
  },
}));

const BASE: SquadAttention = {
  squad_id: "s1",
  pending_cap: 4,
  weekly_cap: 5,
  computed_pending_cap: 4,
  computed_weekly_cap: 5,
  pinned: false,
  pinned_pending_cap: null,
  pinned_weekly_cap: null,
  acceptance_rate: 0.72,
  sample_size: 9,
  reason: "above_threshold",
  computed_at: "2026-11-02T03:00:00Z",
};

function renderAttention(canPin: boolean, client = new QueryClient({ defaultOptions: { queries: { retry: false } } })) {
  renderWithI18n(
    <QueryClientProvider client={client}>
      <OfficeSquadAttention squadId="s1" canPin={canPin} />
    </QueryClientProvider>,
  );
  return client;
}

beforeEach(() => {
  mocks.get.mockResolvedValue(BASE);
  mocks.pin.mockImplementation(async (_squadId: string, data: object) => ({ ...BASE, ...data }));
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe("OfficeSquadAttention", () => {
  it("shows the effective caps, the approval rate and its sample", async () => {
    renderAttention(false);

    expect(await screen.findByText("72% of 9 decided")).toBeTruthy();
    const list = screen.getByRole("region", { name: "Attention limits" });
    expect(list.textContent).toContain("Pending limit4");
    expect(list.textContent).toContain("Weekly limit5");
  });

  it("says when nothing has been decided yet", async () => {
    mocks.get.mockResolvedValue({ ...BASE, acceptance_rate: null, sample_size: 0, reason: "sample_too_small" });
    renderAttention(false);

    expect(await screen.findByText("No decided proposals yet")).toBeTruthy();
  });

  it.each([
    ["pinned", "An admin set these limits."],
    ["sample_too_small", "Fewer than 5 decided proposals in the last 60 days, so the default limits apply."],
    ["below_threshold", "Under 20% of proposals are approved, so the limits are lower."],
    ["above_threshold", "Over 60% of proposals are approved, so the limits are higher."],
    ["default", "Approvals are in the normal range, so the default limits apply."],
  ] as const)("explains the %s reason in plain words", async (reason, copy) => {
    mocks.get.mockResolvedValue({ ...BASE, reason });
    renderAttention(false);

    expect(await screen.findByText(copy)).toBeTruthy();
  });

  it("shows no reason line for a reason a newer server adds", async () => {
    mocks.get.mockResolvedValue({ ...BASE, reason: "unknown" });
    renderAttention(false);

    await screen.findByText("72% of 9 decided");
    expect(screen.queryByText(/default limits apply|are lower|are higher|set these limits/)).toBeNull();
  });

  it("gives members no pin control", async () => {
    renderAttention(false);

    await screen.findByText("72% of 9 decided");
    expect(screen.queryByRole("button", { name: "Pin limits" })).toBeNull();
  });

  it("pins the limits an admin enters", async () => {
    renderAttention(true);

    fireEvent.change(await screen.findByLabelText("Pending"), { target: { value: "1" } });
    fireEvent.change(screen.getByLabelText("Weekly"), { target: { value: "2" } });
    fireEvent.click(screen.getByRole("button", { name: "Pin limits" }));

    await waitFor(() => expect(mocks.pin).toHaveBeenCalledTimes(1));
    expect(mocks.pin).toHaveBeenCalledWith("s1", { pinned: true, pending_cap: 1, weekly_cap: 2 });
  });

  it("blocks a pin outside the server's range and says so", async () => {
    renderAttention(true);

    fireEvent.change(await screen.findByLabelText("Pending"), { target: { value: "11" } });

    expect((screen.getByRole("button", { name: "Pin limits" }) as HTMLButtonElement).disabled).toBe(true);
    expect(screen.getByRole("alert").textContent).toBe("Whole numbers from 1 to 10.");
    expect(screen.getByLabelText("Pending").getAttribute("max")).toBe("10");
  });

  it("refuses a fractional cap", async () => {
    renderAttention(true);

    fireEvent.change(await screen.findByLabelText("Weekly"), { target: { value: "2.5" } });

    expect((screen.getByRole("button", { name: "Pin limits" }) as HTMLButtonElement).disabled).toBe(true);
  });

  it("clears a pin without sending caps", async () => {
    mocks.get.mockResolvedValue({
      ...BASE,
      pinned: true,
      pinned_pending_cap: 1,
      pinned_weekly_cap: 2,
      pending_cap: 1,
      weekly_cap: 2,
      reason: "pinned",
    });
    renderAttention(true);

    fireEvent.click(await screen.findByRole("button", { name: "Clear pin" }));

    await waitFor(() => expect(mocks.pin).toHaveBeenCalledTimes(1));
    expect(mocks.pin).toHaveBeenCalledWith("s1", { pinned: false });
  });

  it("refreshes the proposal lists when a pin changes", async () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const invalidate = vi.spyOn(client, "invalidateQueries");
    renderAttention(true, client);

    fireEvent.change(await screen.findByLabelText("Pending"), { target: { value: "2" } });
    fireEvent.click(screen.getByRole("button", { name: "Pin limits" }));

    await waitFor(() =>
      expect(invalidate).toHaveBeenCalledWith({ queryKey: proposalKeys.all("ws-1") }),
    );
  });
});
