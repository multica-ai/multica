import { describe, it, expect, vi, beforeEach } from "vitest";
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { AutopilotTrigger } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

// The detail page's trigger row: what a schedule row says about itself, and
// the edit entry it grew in MUL-7478.

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-test" }));
vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({}),
  useCurrentWorkspace: () => ({ name: "Acme" }),
}));

vi.mock("@multica/core/autopilots/queries", () => ({
  autopilotDetailOptions: () => ({ queryKey: ["autopilot"], queryFn: async () => null }),
  autopilotRunsOptions: () => ({ queryKey: ["runs"], queryFn: async () => [] }),
  autopilotRunOptions: () => ({ queryKey: ["run"], queryFn: async () => null }),
  cronPreviewOptions: (wsId: string, expr: string, tz: string) => ({
    queryKey: ["cron-preview", wsId, expr, tz],
    queryFn: async () => ({ next_runs: ["2126-07-14T01:00:00Z"] }),
    retry: false,
  }),
}));

vi.mock("@multica/core/autopilots/mutations", () => ({
  useUpdateAutopilot: () => ({ mutateAsync: vi.fn() }),
  useDeleteAutopilot: () => ({ mutateAsync: vi.fn() }),
  useTriggerAutopilot: () => ({ mutateAsync: vi.fn() }),
  useCreateAutopilotTrigger: () => ({ mutateAsync: vi.fn() }),
  useDeleteAutopilotTrigger: () => ({ mutateAsync: vi.fn() }),
  useUpdateAutopilotTrigger: () => ({ mutateAsync: vi.fn() }),
  useRotateAutopilotTriggerWebhookToken: () => ({ mutateAsync: vi.fn(), isPending: false }),
}));

// A webhook row composes its URL from the API base; everything else in this
// module (ApiError, which the schedule gate type-checks against) stays real.
vi.mock("@multica/core/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/api")>();
  return { ...actual, api: { ...actual.api, getBaseUrl: () => "https://api.test" } };
});

vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

vi.mock("./pickers/timezone-picker", () => ({
  TimezonePicker: ({ value }: { value: string }) => <div data-testid="timezone-picker">{value}</div>,
}));

import { TriggerRow } from "./autopilot-detail-page";

const AUTOPILOT_ID = "ap-1";

function trigger(overrides: Partial<AutopilotTrigger> = {}): AutopilotTrigger {
  return {
    id: "trg-morning",
    autopilot_id: AUTOPILOT_ID,
    kind: "schedule",
    enabled: true,
    cron_expression: "TZ=Asia/Bangkok 0 9 * * *",
    timezone: "Asia/Bangkok",
    next_run_at: "2126-07-14T02:00:00Z",
    webhook_token: null,
    label: null,
    last_fired_at: null,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

function renderRow(trig: AutopilotTrigger, canWrite = true) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <TriggerRow trigger={trig} autopilotId={AUTOPILOT_ID} canWrite={canWrite} />
    </QueryClientProvider>,
  );
}

describe("TriggerRow", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("reads out the next run of a live schedule", () => {
    renderRow(trigger());

    expect(screen.getByText(/Next:/)).toBeInTheDocument();
    expect(screen.queryByText("Disabled")).not.toBeInTheDocument();
  });

  it("stops promising a next run once the schedule is paused", () => {
    // The server keeps next_run_at on a disabled trigger — the dispatcher
    // filters on `enabled` rather than clearing the column — so the row used to
    // carry the Disabled badge and "Next: ..." at the same time, one of them a
    // run that will never happen.
    renderRow(trigger({ enabled: false }));

    expect(screen.getByText("Disabled")).toBeInTheDocument();
    expect(screen.queryByText(/Next:/)).not.toBeInTheDocument();
  });

  it("opens the editor on the schedule this row runs", async () => {
    const user = userEvent.setup();
    renderRow(trigger());

    await user.click(screen.getByRole("button", { name: "Edit schedule" }));

    expect(await screen.findByText("Edit schedule", { selector: "h2" })).toBeInTheDocument();
    expect(screen.getByTestId("timezone-picker")).toHaveTextContent("Asia/Bangkok");
  });

  it("offers no schedule editor on a trigger that has no cron to edit", () => {
    // cron_expression / timezone are rejected on any other kind, so a webhook
    // row must not offer an entry that could only fail.
    renderRow(trigger({ kind: "webhook", cron_expression: null, timezone: null, next_run_at: null }));

    expect(screen.queryByRole("button", { name: "Edit schedule" })).not.toBeInTheDocument();
  });

  it("offers no edit entry to a reader who cannot write", () => {
    renderRow(trigger(), false);

    expect(screen.queryByRole("button", { name: "Edit schedule" })).not.toBeInTheDocument();
  });
});
