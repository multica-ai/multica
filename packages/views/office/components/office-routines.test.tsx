/**
 * @vitest-environment jsdom
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { charterKeys, type SquadRoutine } from "@multica/core/office";
import { renderWithI18n } from "../../test/i18n";
import { OfficeRoutines } from "./office-routines";

const api = vi.hoisted(() => ({
  listSquadRoutines: vi.fn(),
  createSquadRoutine: vi.fn(),
  deleteSquadRoutine: vi.fn(),
  triggerAutopilot: vi.fn(),
  updateAutopilot: vi.fn(),
}));

vi.mock("@multica/core/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/api")>()),
  api,
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));

function routine(overrides: Partial<SquadRoutine> & Pick<SquadRoutine, "autopilot_id" | "state">): SquadRoutine {
  return {
    squad_id: "sq",
    autopilot_title: "Autopilot",
    autopilot_status: "active",
    purpose: "Weekly scan",
    expected_output: "report",
    required: true,
    grace_hours: 24,
    schedule: { cron_expression: "0 9 * * 1", timezone: "UTC" },
    next_run_at: "2026-11-09T09:00:00Z",
    last_run: { id: "r1", status: "completed", source: "schedule", triggered_at: "2026-11-02T09:00:00Z", completed_at: null },
    created_at: "2026-10-01T00:00:00Z",
    ...overrides,
  };
}

const ROUTINES: SquadRoutine[] = [
  routine({ autopilot_id: "ap1", state: "on_time", purpose: "Weekly scan" }),
  routine({
    autopilot_id: "ap2",
    state: "late",
    purpose: "Daily digest",
    schedule: { cron_expression: "0 9 * * *", timezone: "UTC" },
  }),
  routine({
    autopilot_id: "ap3",
    state: "paused",
    purpose: "Quarterly audit",
    autopilot_status: "paused",
    schedule: { cron_expression: "30 7 * * 3", timezone: "UTC" },
  }),
  routine({
    autopilot_id: "ap4",
    state: "never_ran",
    purpose: "Monthly review",
    schedule: null,
    next_run_at: null,
    last_run: null,
  }),
];

describe("OfficeRoutines", () => {
  let queryClient: QueryClient;

  beforeEach(() => {
    vi.clearAllMocks();
    api.listSquadRoutines.mockResolvedValue(ROUTINES);
  });

  afterEach(() => {
    cleanup();
  });

  function setup({ canManage = true, routines = ROUTINES }: { canManage?: boolean; routines?: SquadRoutine[] } = {}) {
    queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
    queryClient.setQueryData(charterKeys.routines("ws-1", "sq"), routines);
    return renderWithI18n(
      <QueryClientProvider client={queryClient}>
        <OfficeRoutines squadId="sq" canManage={canManage} />
      </QueryClientProvider>,
    );
  }

  function row(purpose: string) {
    const item = screen.getByText(purpose).closest("li");
    if (!item) throw new Error(`no routine row for ${purpose}`);
    return item;
  }

  it("shows each routine's state, purpose, schedule, last run and next run", () => {
    setup();

    const weekly = within(row("Weekly scan"));
    expect(weekly.getByText("On time")).toBeTruthy();
    expect(weekly.getByText(/Weekly · Last run/)).toBeTruthy();
    expect(weekly.getByText(/Next/)).toBeTruthy();
    expect(within(row("Daily digest")).getByText("Late")).toBeTruthy();
    expect(within(row("Quarterly audit")).getByText("Paused")).toBeTruthy();
    expect(within(row("Monthly review")).getByText("Not run yet")).toBeTruthy();
    expect(within(row("Monthly review")).getByText(/No schedule/)).toBeTruthy();
  });

  it("names a custom schedule by its cron expression", () => {
    setup();

    expect(within(row("Quarterly audit")).getByText(/30 7 \* \* 3/)).toBeTruthy();
  });

  it("shows the empty state when the squad has no routines", () => {
    setup({ routines: [] });

    expect(screen.getByText("No routines yet.")).toBeTruthy();
  });

  it("says the routines could not load instead of showing an empty list", async () => {
    api.listSquadRoutines.mockRejectedValue(new Error("network down"));
    queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    renderWithI18n(
      <QueryClientProvider client={queryClient}>
        <OfficeRoutines squadId="sq" canManage />
      </QueryClientProvider>,
    );

    expect(await screen.findByText("Couldn't load the routines")).toBeTruthy();
    expect(screen.queryByText("No routines yet.")).toBeNull();
  });

  it("runs a routine now through its autopilot", async () => {
    api.triggerAutopilot.mockResolvedValue({ id: "run1" });
    setup();

    fireEvent.click(within(row("Weekly scan")).getByRole("button", { name: "Run now" }));

    await waitFor(() => expect(api.triggerAutopilot).toHaveBeenCalledWith("ap1"));
  });

  it("does not run a paused routine", () => {
    setup();

    expect((within(row("Quarterly audit")).getByRole("button", { name: "Run now" }) as HTMLButtonElement).disabled).toBe(
      true,
    );
  });

  it("pauses an active routine's autopilot", async () => {
    api.updateAutopilot.mockResolvedValue({ id: "ap1" });
    setup();

    fireEvent.click(within(row("Weekly scan")).getByRole("button", { name: "Pause" }));

    await waitFor(() => expect(api.updateAutopilot).toHaveBeenCalledWith("ap1", { status: "paused" }));
  });

  it("resumes a paused routine's autopilot", async () => {
    api.updateAutopilot.mockResolvedValue({ id: "ap3" });
    setup();

    fireEvent.click(within(row("Quarterly audit")).getByRole("button", { name: "Resume" }));

    await waitFor(() => expect(api.updateAutopilot).toHaveBeenCalledWith("ap3", { status: "active" }));
  });

  it("removes a routine and pauses its autopilot, so it does not run unowned", async () => {
    api.deleteSquadRoutine.mockResolvedValue(undefined);
    setup();

    fireEvent.click(within(row("Daily digest")).getByRole("button", { name: "Remove" }));

    await waitFor(() => expect(api.deleteSquadRoutine).toHaveBeenCalledWith("sq", "ap2", { pause: true }));
  });

  it("adds a routine on a cadence preset with its purpose and instructions", async () => {
    api.createSquadRoutine.mockResolvedValue(routine({ autopilot_id: "ap9", state: "never_ran", purpose: "Market scan" }));
    setup();

    fireEvent.click(screen.getByRole("button", { name: "Add routine" }));
    fireEvent.change(screen.getByLabelText("Purpose"), { target: { value: "Market scan" } });
    fireEvent.change(screen.getByLabelText("Instructions (optional)"), { target: { value: "Read the market notes" } });
    fireEvent.click(screen.getByRole("button", { name: "Add routine" }));

    await waitFor(() => expect(api.createSquadRoutine).toHaveBeenCalledTimes(1));
    expect(api.createSquadRoutine.mock.calls[0]).toEqual([
      "sq",
      {
        cadence: "weekly",
        purpose: "Market scan",
        expected_output: "none",
        description: "Read the market notes",
      },
    ]);
  });

  it("keeps Add routine disabled until a purpose is written", () => {
    setup();

    fireEvent.click(screen.getByRole("button", { name: "Add routine" }));

    expect((screen.getByRole("button", { name: "Add routine" }) as HTMLButtonElement).disabled).toBe(true);
  });

  it("shows members the states but none of the controls", () => {
    setup({ canManage: false });

    expect(within(row("Daily digest")).getByText("Late")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Run now" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Pause" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Add routine" })).toBeNull();
  });
});
