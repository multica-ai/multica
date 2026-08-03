import { useImperativeHandle, useRef, useState } from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderWithI18n } from "../../test/i18n";

// Regression cover for GitHub #6231: "Create Autopilot" was disabled whenever
// a required field was empty, so a user who had not picked an assignee saw a
// dead control and no reason for it. The button is now live whenever a save
// isn't already in flight, and the click it accepts is what surfaces an inline
// error on the field at fault.

const mockCreateAutopilot = vi.hoisted(() => vi.fn());
const mockCreateTrigger = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-test" }));
vi.mock("@multica/core/paths", () => ({ useCurrentWorkspace: () => ({ name: "Acme" }) }));

vi.mock("@multica/core/workspace/queries", () => ({
  agentListOptions: (wsId: string) => ({
    queryKey: ["agents", wsId],
    queryFn: async () => [
      {
        id: "agent-1",
        name: "Scout",
        description: "Researches things",
        archived_at: null,
        runtime_id: "runtime-1",
      },
    ],
  }),
  squadListOptions: (wsId: string) => ({
    queryKey: ["squads", wsId],
    queryFn: async () => [],
  }),
}));

vi.mock("@multica/core/projects/queries", () => ({
  projectListOptions: (wsId: string) => ({
    queryKey: ["projects", wsId],
    queryFn: async () => [],
  }),
}));

vi.mock("@multica/core/autopilots/queries", () => ({
  cronPreviewOptions: (wsId: string, expr: string, tz: string) => ({
    queryKey: ["cron-preview", wsId, expr, tz],
    queryFn: async () => ({ next_runs: ["2126-07-14T01:00:00Z"] }),
    retry: false,
  }),
}));

vi.mock("@multica/core/autopilots/mutations", () => ({
  useCreateAutopilot: () => ({ mutateAsync: mockCreateAutopilot }),
  useCreateAutopilotTrigger: () => ({ mutateAsync: mockCreateTrigger }),
  useUpdateAutopilot: () => ({ mutateAsync: vi.fn() }),
  useUpdateAutopilotTrigger: () => ({ mutateAsync: vi.fn() }),
}));

vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

// Tiptap in jsdom is neither cheap nor the subject here: the title is a plain
// input whose ref honours focus(), which is all the dialog asks of it.
vi.mock("../../editor", () => ({
  TitleEditor: ({ ref, defaultValue, placeholder, onChange, onSubmit }: any) => {
    const [value, setValue] = useState(defaultValue ?? "");
    const inputRef = useRef<HTMLInputElement>(null);
    useImperativeHandle(ref, () => ({
      getText: () => value,
      focus: () => inputRef.current?.focus(),
      focusAtCoords: () => inputRef.current?.focus(),
    }));
    return (
      <input
        ref={inputRef}
        aria-label="title"
        value={value}
        placeholder={placeholder}
        onChange={(e) => {
          setValue(e.target.value);
          onChange?.(e.target.value);
        }}
        onKeyDown={(e) => {
          if (e.key === "Enter") onSubmit?.();
        }}
      />
    );
  },
  ContentEditor: ({ placeholder }: any) => <textarea aria-label="runbook" placeholder={placeholder} />,
}));

vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: ({ actorId }: { actorId: string }) => <span data-testid="actor-avatar">{actorId}</span>,
}));

vi.mock("./subscriber-multi-select", () => ({
  SubscriberMultiSelect: () => <div data-testid="subscriber-multi-select" />,
}));

vi.mock("../../projects/components/project-picker", () => ({
  ProjectPicker: ({ triggerRender }: { triggerRender: React.ReactElement }) => triggerRender,
}));

vi.mock("../pickers/timezone-picker", () => ({
  TimezonePicker: ({ value }: { value: string }) => <div data-testid="timezone-picker">{value}</div>,
}));

import { AutopilotDialog } from "./autopilot-dialog";

function renderCreateDialog() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <AutopilotDialog mode="create" open onOpenChange={vi.fn()} />
    </QueryClientProvider>,
  );
}

const createButton = () => screen.getByRole("button", { name: "Create autopilot" });
const assigneeTrigger = () => screen.getByRole("button", { name: /Select agent or squad/ });

describe("AutopilotDialog required-field feedback", () => {
  beforeEach(() => {
    mockCreateAutopilot.mockReset();
    mockCreateTrigger.mockReset();
  });

  it("leaves the create button fully live while required fields are empty", () => {
    renderCreateDialog();
    // A dimmed button is a dead end with no room for a reason. Neither the
    // native attribute nor the ARIA one may be set: the click is the whole
    // feedback channel.
    expect(createButton()).not.toBeDisabled();
    expect(createButton()).not.toHaveAttribute("aria-disabled");
  });

  it("names the missing title on a blocked submit instead of doing nothing", async () => {
    const user = userEvent.setup();
    renderCreateDialog();

    expect(screen.queryByText("Enter a name for this autopilot.")).not.toBeInTheDocument();
    await user.click(createButton());

    expect(await screen.findByText("Enter a name for this autopilot.")).toBeInTheDocument();
    expect(mockCreateAutopilot).not.toHaveBeenCalled();
  });

  it("names the missing assignee once the title is filled, and marks the picker invalid", async () => {
    const user = userEvent.setup();
    renderCreateDialog();

    await user.type(screen.getByLabelText("title"), "Daily digest");
    await user.click(createButton());

    expect(
      await screen.findByText("Choose the agent or squad that will run this autopilot."),
    ).toBeInTheDocument();
    // The title error clears itself the moment the field is filled — no second
    // submit needed to retire an error the user has already fixed.
    expect(screen.queryByText("Enter a name for this autopilot.")).not.toBeInTheDocument();
    expect(assigneeTrigger()).toHaveAttribute("aria-invalid", "true");
    expect(mockCreateAutopilot).not.toHaveBeenCalled();
  });

  it("clears the assignee error and submits once an agent is picked", async () => {
    const user = userEvent.setup();
    mockCreateAutopilot.mockResolvedValue({ id: "ap-1" });
    mockCreateTrigger.mockResolvedValue({ id: "tr-1" });
    renderCreateDialog();

    await user.type(screen.getByLabelText("title"), "Daily digest");
    await user.click(createButton());
    await screen.findByText("Choose the agent or squad that will run this autopilot.");

    await user.click(assigneeTrigger());
    await user.click(await screen.findByRole("button", { name: /Scout/ }));

    await waitFor(() => {
      expect(
        screen.queryByText("Choose the agent or squad that will run this autopilot."),
      ).not.toBeInTheDocument();
    });

    await user.click(createButton());
    await waitFor(() => expect(mockCreateAutopilot).toHaveBeenCalledTimes(1));
    expect(mockCreateAutopilot.mock.calls[0]?.[0]).toMatchObject({
      title: "Daily digest",
      assignee_type: "agent",
      assignee_id: "agent-1",
    });
  });
});
