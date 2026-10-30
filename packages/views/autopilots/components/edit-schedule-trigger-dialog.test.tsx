import { describe, it, expect, vi, beforeEach } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { AutopilotTrigger } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

// The editor a trigger row opens (MUL-7478). Before it, an existing schedule
// could only be deleted and recreated: the autopilot dialog's panel speaks for
// one schedule, and the detail page listed triggers read-only.

const mockUpdateTrigger = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-test" }));

// The submit path validates over the network before it writes. Parking that
// round trip holds the dialog mid-flight, in the window a label typed after
// Save used to fall into.
const preview = vi.hoisted(() => ({ release: null as null | (() => void), hold: false }));

vi.mock("@multica/core/autopilots/queries", () => ({
  cronPreviewOptions: (wsId: string, expr: string, tz: string) => ({
    queryKey: ["cron-preview", wsId, expr, tz],
    queryFn: async () => {
      if (preview.hold) {
        await new Promise<void>((resolve) => {
          preview.release = resolve;
        });
      }
      return { next_runs: ["2126-07-14T01:00:00Z"] };
    },
    retry: false,
  }),
}));

vi.mock("@multica/core/autopilots/mutations", () => ({
  useUpdateAutopilotTrigger: () => ({ mutateAsync: mockUpdateTrigger }),
}));

vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

vi.mock("./pickers/timezone-picker", () => ({
  TimezonePicker: ({ value }: { value: string }) => <div data-testid="timezone-picker">{value}</div>,
}));

import { EditScheduleTriggerDialog } from "./edit-schedule-trigger-dialog";

const AUTOPILOT_ID = "ap-1";

function trigger(overrides: Partial<AutopilotTrigger> = {}): AutopilotTrigger {
  return {
    id: "trg-evening",
    autopilot_id: AUTOPILOT_ID,
    kind: "schedule",
    enabled: true,
    cron_expression: "TZ=Asia/Bangkok 0 */3 * * *",
    timezone: "Asia/Bangkok",
    next_run_at: null,
    webhook_token: null,
    label: null,
    last_fired_at: null,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

function renderDialog(trig: AutopilotTrigger = trigger()) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const onOpenChange = vi.fn();
  const tree = (next: AutopilotTrigger) => (
    <QueryClientProvider client={qc}>
      <EditScheduleTriggerDialog
        open
        onOpenChange={onOpenChange}
        autopilotId={AUTOPILOT_ID}
        trigger={next}
      />
    </QueryClientProvider>
  );
  const result = renderWithI18n(tree(trig));
  // The detail query refreshing under an open dialog: new props, same mount.
  return { ...result, onOpenChange, refreshProps: (next: AutopilotTrigger) => result.rerender(tree(next)) };
}

const saveButton = () => screen.getByRole("button", { name: "Save" });
const labelInput = () => screen.getByPlaceholderText("e.g. Weekday morning");
const enabledSwitch = () => screen.getByRole("switch", { name: "Enabled" });

beforeEach(() => {
  mockUpdateTrigger.mockReset().mockResolvedValue({ id: "trg-evening" });
  preview.hold = false;
  preview.release = null;
});

describe("EditScheduleTriggerDialog", () => {
  it("opens on the schedule the row already runs, not on a default", () => {
    renderDialog();

    // The stored zone and interval, read back from the row — seeding the editor
    // with its own 09:00 default would be a proposal dressed as the trigger's
    // state, which is how MUL-5649 lost a save under a success toast.
    expect(screen.getByTestId("timezone-picker")).toHaveTextContent("Asia/Bangkok");
    expect(screen.getByRole("button", { name: "At an interval", pressed: true })).toBeInTheDocument();
    expect(screen.getByDisplayValue("3")).toBeInTheDocument();
  });

  it("patches this trigger alone, carrying the zone with the expression", async () => {
    const user = userEvent.setup();
    const { onOpenChange } = renderDialog();

    await user.click(screen.getByRole("button", { name: "At a time" }));
    await user.click(saveButton());

    await waitFor(() => expect(mockUpdateTrigger).toHaveBeenCalledTimes(1));
    const patch = mockUpdateTrigger.mock.calls[0]?.[0];
    expect(patch).toMatchObject({
      autopilotId: AUTOPILOT_ID,
      triggerId: "trg-evening",
      timezone: "Asia/Bangkok",
    });
    expect(patch.cron_expression).toContain("Asia/Bangkok");
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });

  it("keeps the dialog open when the write fails, with the server's reason", async () => {
    const user = userEvent.setup();
    const { onOpenChange } = renderDialog();
    mockUpdateTrigger.mockRejectedValueOnce(new Error("cron_expression is invalid"));

    await user.click(screen.getByRole("button", { name: "At a time" }));
    await user.click(saveButton());

    await waitFor(() => expect(mockUpdateTrigger).toHaveBeenCalledTimes(1));
    const { toast } = await import("sonner");
    expect(toast.error).toHaveBeenCalledWith("cron_expression is invalid");
    expect(onOpenChange).not.toHaveBeenCalledWith(false);
  });
});

// The PATCH is partial on purpose. A changed cron / timezone / enabled reads
// server-side as a substantive edit: it republishes the rule version and moves
// this trigger's accountability to whoever saved (`UpdateAutopilotTrigger` in
// server/internal/handler/autopilot.go, MUL-4302). Since `parseCron` → `toCron`
// hands an untouched schedule back normalized — `TZ=` prefix and all, textually
// different from the stored row — resending it would make a rename look like a
// schedule change and carry that responsibility along with it.
describe("EditScheduleTriggerDialog sends only what the user changed", () => {
  it("sends the label alone when only the label was touched", async () => {
    const user = userEvent.setup();
    // Stored without the prefix the editor adds back, so a resend would be
    // visibly a different string to the server.
    renderDialog(trigger({ cron_expression: "0 */3 * * *", label: "Old name" }));

    await user.clear(labelInput());
    await user.type(labelInput(), "Evening sweep");
    await user.click(saveButton());

    await waitFor(() => expect(mockUpdateTrigger).toHaveBeenCalledTimes(1));
    expect(mockUpdateTrigger.mock.calls[0]?.[0]).toEqual({
      autopilotId: AUTOPILOT_ID,
      triggerId: "trg-evening",
      label: "Evening sweep",
    });
  });

  it("pauses a schedule without resending the schedule", async () => {
    const user = userEvent.setup();
    renderDialog(trigger({ cron_expression: "0 */3 * * *" }));

    await user.click(enabledSwitch());
    await user.click(saveButton());

    await waitFor(() => expect(mockUpdateTrigger).toHaveBeenCalledTimes(1));
    // The cron stays where it is — pausing is not an edit to it, and the row
    // keeps running the same schedule if it is switched back on.
    expect(mockUpdateTrigger.mock.calls[0]?.[0]).toEqual({
      autopilotId: AUTOPILOT_ID,
      triggerId: "trg-evening",
      enabled: false,
    });
  });

  it("has nothing to save until something changes", async () => {
    const user = userEvent.setup();
    renderDialog();

    // A no-op PATCH is not free: the server recomputes and rewrites the row's
    // next_run_at from whatever it is sent, so opening and saving a dialog the
    // user never edited would still move a reading they never touched.
    expect(saveButton()).toBeDisabled();

    await user.click(enabledSwitch());
    expect(saveButton()).toBeEnabled();

    await user.click(enabledSwitch());
    expect(saveButton()).toBeDisabled();
    expect(mockUpdateTrigger).not.toHaveBeenCalled();
  });

  it("takes no label the in-flight write could not carry", async () => {
    const user = userEvent.setup();
    renderDialog();
    preview.hold = true;

    await user.click(screen.getByRole("button", { name: "At a time" }));
    await user.click(saveButton());

    // Parked mid-validation: submit has already read the label it will send, so
    // the input locks rather than accepting one this write cannot carry and the
    // closing dialog would swallow.
    await waitFor(() => expect(labelInput()).toBeDisabled());
    expect(mockUpdateTrigger).not.toHaveBeenCalled();

    preview.release?.();
    await waitFor(() => expect(mockUpdateTrigger).toHaveBeenCalledTimes(1));
    expect(mockUpdateTrigger.mock.calls[0]?.[0].label).toBeUndefined();
  });

  it("does not call a stored label with stray whitespace an edit", () => {
    renderDialog(trigger({ label: "  Morning sweep  " }));

    // The baseline is trimmed the way submit trims, so opening the dialog on a
    // historical label does not by itself arm Save.
    expect(saveButton()).toBeDisabled();
  });

  it("leaves alone a field a teammate changed under the open dialog", async () => {
    const user = userEvent.setup();
    const { refreshProps } = renderDialog(trigger({ label: "Old name" }));

    // A teammate renames this row and pauses it; the detail query refreshes and
    // the dialog takes the new props without remounting, so its untouched
    // controls still hold what it opened on.
    refreshProps(trigger({ label: "Renamed by teammate", enabled: false }));

    // This user has edited nothing, so there is nothing of theirs to save.
    expect(saveButton()).toBeDisabled();

    // And when they do edit one field, only that field travels: the rename and
    // the pause stay as the teammate left them instead of being reverted to
    // what this dialog happened to be showing.
    await user.click(screen.getByRole("button", { name: "At a time" }));
    await user.click(saveButton());

    await waitFor(() => expect(mockUpdateTrigger).toHaveBeenCalledTimes(1));
    const patch = mockUpdateTrigger.mock.calls[0]?.[0];
    expect(patch.label).toBeUndefined();
    expect(patch.enabled).toBeUndefined();
    expect(patch.cron_expression).toBeDefined();
  });
});
