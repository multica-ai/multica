import { describe, it, expect, vi, beforeEach } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { AutopilotTrigger } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

// The editor a webhook row opens. The PATCH stays partial for the reason the
// schedule dialog's does: a changed filter list or enabled flag republishes
// the rule version and moves this trigger's accountability to the saver
// (`UpdateAutopilotTrigger` in server/internal/handler/autopilot.go, MUL-4302),
// so a rename must not carry them along.

const mockUpdateTrigger = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/autopilots/mutations", () => ({
  useUpdateAutopilotTrigger: () => ({ mutateAsync: mockUpdateTrigger }),
}));

vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

import { EditWebhookTriggerDialog } from "./edit-webhook-trigger-dialog";

const AUTOPILOT_ID = "ap-1";

function trigger(overrides: Partial<AutopilotTrigger> = {}): AutopilotTrigger {
  return {
    id: "trg-hook",
    autopilot_id: AUTOPILOT_ID,
    kind: "webhook",
    enabled: true,
    cron_expression: null,
    timezone: null,
    next_run_at: null,
    webhook_token: "tok-1",
    label: null,
    event_filters: [{ event: "pull_request", actions: ["opened"] }],
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
      <EditWebhookTriggerDialog
        open
        onOpenChange={onOpenChange}
        autopilotId={AUTOPILOT_ID}
        trigger={next}
      />
    </QueryClientProvider>
  );
  const result = renderWithI18n(tree(trig));
  return { ...result, onOpenChange, refreshProps: (next: AutopilotTrigger) => result.rerender(tree(next)) };
}

const saveButton = () => screen.getByRole("button", { name: "Save" });
const labelInput = () => screen.getByPlaceholderText("e.g. GitHub pull requests");
const enabledSwitch = () => screen.getByRole("switch", { name: "Enabled" });

beforeEach(() => {
  mockUpdateTrigger.mockReset().mockResolvedValue({ id: "trg-hook" });
});

describe("EditWebhookTriggerDialog", () => {
  it("writes an added rule to this webhook as its whole filter list", async () => {
    const user = userEvent.setup();
    const { onOpenChange } = renderDialog();

    await user.click(screen.getByRole("button", { name: "Add rule" }));
    // The new rule's event input takes focus, so typing goes straight into it.
    await user.keyboard("issues");
    await user.click(saveButton());

    await waitFor(() => expect(mockUpdateTrigger).toHaveBeenCalledTimes(1));
    expect(mockUpdateTrigger.mock.calls[0]?.[0]).toEqual({
      autopilotId: AUTOPILOT_ID,
      triggerId: "trg-hook",
      event_filters: [{ event: "pull_request", actions: ["opened"] }, { event: "issues" }],
    });
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });

  it("clears the filters explicitly when the last rule is removed", async () => {
    const user = userEvent.setup();
    renderDialog();

    await user.click(screen.getByRole("button", { name: "Remove rule" }));
    await user.click(saveButton());

    await waitFor(() => expect(mockUpdateTrigger).toHaveBeenCalledTimes(1));
    // An omitted field leaves the stored filters in place server-side; only
    // an explicit [] takes the webhook back to accepting every event.
    expect(mockUpdateTrigger.mock.calls[0]?.[0].event_filters).toEqual([]);
  });

  it("edits a saved rule in place", async () => {
    const user = userEvent.setup();
    renderDialog();

    const actions = within(screen.getByRole("group", { name: "pull_request" })).getByRole("textbox", {
      name: "Actions",
    });
    await user.clear(actions);
    await user.type(actions, "opened, reopened");
    await user.click(saveButton());

    await waitFor(() => expect(mockUpdateTrigger).toHaveBeenCalledTimes(1));
    expect(mockUpdateTrigger.mock.calls[0]?.[0].event_filters).toEqual([
      { event: "pull_request", actions: ["opened", "reopened"] },
    ]);
  });

  // Regression cover for text left in a rule form being dropped on Save: an
  // incomplete rule now stops the save and says where, and nothing is sent.
  it("stops a save on an incomplete rule and points at it", async () => {
    const user = userEvent.setup();
    const { onOpenChange } = renderDialog();

    await user.click(screen.getByRole("button", { name: "Add rule" }));
    await user.click(saveButton());

    const [, newEvent] = screen.getAllByRole("textbox", { name: "Event" });
    expect(newEvent).toHaveAttribute("aria-invalid", "true");
    expect(newEvent).toHaveFocus();
    expect(screen.getByText("Enter an event name.")).toBeInTheDocument();
    expect(mockUpdateTrigger).not.toHaveBeenCalled();
    expect(onOpenChange).not.toHaveBeenCalled();

    await user.type(newEvent!, "issues");
    expect(screen.queryByText("Enter an event name.")).not.toBeInTheDocument();
    await user.click(saveButton());
    await waitFor(() => expect(mockUpdateTrigger).toHaveBeenCalledTimes(1));
  });

  // Regression cover: a list value holding a comma came back from the inputs
  // split in two, so an unrelated save rewrote the rule without saying so.
  it("leaves saved filters alone when only the switch moved", async () => {
    const user = userEvent.setup();
    renderDialog(
      trigger({
        event_filters: [
          {
            event: "pull_request",
            conditions: [
              { path: "pull_request.title", op: "in", values: ["fix, docs", "chore"] },
              { path: "pull_request.body", op: "equals", value: "" },
            ],
          },
        ],
      }),
    );

    expect(saveButton()).toBeDisabled();
    await user.click(enabledSwitch());
    await user.click(saveButton());

    await waitFor(() => expect(mockUpdateTrigger).toHaveBeenCalledTimes(1));
    expect(mockUpdateTrigger.mock.calls[0]?.[0]).toEqual({
      autopilotId: AUTOPILOT_ID,
      triggerId: "trg-hook",
      enabled: false,
    });
  });

  it("sends the label alone when only the label was touched", async () => {
    const user = userEvent.setup();
    renderDialog(trigger({ label: "Old name" }));

    await user.clear(labelInput());
    await user.type(labelInput(), "GitHub");
    await user.click(saveButton());

    await waitFor(() => expect(mockUpdateTrigger).toHaveBeenCalledTimes(1));
    expect(mockUpdateTrigger.mock.calls[0]?.[0]).toEqual({
      autopilotId: AUTOPILOT_ID,
      triggerId: "trg-hook",
      label: "GitHub",
    });
  });

  it("pauses a webhook without resending its filters", async () => {
    const user = userEvent.setup();
    renderDialog();

    await user.click(enabledSwitch());
    await user.click(saveButton());

    await waitFor(() => expect(mockUpdateTrigger).toHaveBeenCalledTimes(1));
    expect(mockUpdateTrigger.mock.calls[0]?.[0]).toEqual({
      autopilotId: AUTOPILOT_ID,
      triggerId: "trg-hook",
      enabled: false,
    });
  });

  it("has nothing to save until something changes", async () => {
    const user = userEvent.setup();
    renderDialog();

    expect(saveButton()).toBeDisabled();

    await user.click(enabledSwitch());
    expect(saveButton()).toBeEnabled();

    await user.click(enabledSwitch());
    expect(saveButton()).toBeDisabled();
  });

  it("leaves alone filters a teammate changed under the open dialog", async () => {
    const user = userEvent.setup();
    const { refreshProps } = renderDialog();

    refreshProps(trigger({ event_filters: [{ event: "push" }] }));
    expect(saveButton()).toBeDisabled();

    await user.click(enabledSwitch());
    await user.click(saveButton());

    await waitFor(() => expect(mockUpdateTrigger).toHaveBeenCalledTimes(1));
    expect(mockUpdateTrigger.mock.calls[0]?.[0].event_filters).toBeUndefined();
  });

  it("keeps the dialog open when the write fails, with the server's reason", async () => {
    const user = userEvent.setup();
    const { onOpenChange } = renderDialog();
    mockUpdateTrigger.mockRejectedValueOnce(new Error("event_filters[0].event is required"));

    await user.click(enabledSwitch());
    await user.click(saveButton());

    await waitFor(() => expect(mockUpdateTrigger).toHaveBeenCalledTimes(1));
    const { toast } = await import("sonner");
    expect(toast.error).toHaveBeenCalledWith("event_filters[0].event is required");
    expect(onOpenChange).not.toHaveBeenCalledWith(false);
  });
});
