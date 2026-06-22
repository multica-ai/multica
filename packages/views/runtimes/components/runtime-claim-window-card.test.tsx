// @vitest-environment jsdom

import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import type { AgentRuntime } from "@multica/core/types";
import { I18nProvider } from "@multica/core/i18n/react";
import enRuntimes from "../../locales/en/runtimes.json";

const TEST_RESOURCES = { en: { runtimes: enRuntimes } };
const mockMutate = vi.hoisted(() => vi.fn());
const toastSuccess = vi.hoisted(() => vi.fn());
const toastError = vi.hoisted(() => vi.fn());
const mutationState = vi.hoisted(() => ({ fail: false }));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/runtimes/mutations", () => ({
  useUpdateRuntime: () => ({
    mutate: (
      args: unknown,
      opts?: { onSuccess?: () => void; onError?: (error: Error) => void },
    ) => {
      mockMutate(args);
      if (mutationState.fail) {
        opts?.onError?.(new Error("server rejected schedule"));
      } else {
        opts?.onSuccess?.();
      }
    },
    isPending: false,
  }),
}));

vi.mock("../../common/use-viewing-timezone", () => ({
  useViewingTimezone: () => "Europe/Warsaw",
}));

vi.mock("@multica/ui/components/ui/time-input", () => ({
  TimeInput: ({
    value,
    onChange,
    disabled,
  }: {
    value: string;
    onChange: (value: string) => void;
    disabled?: boolean;
  }) => (
    <input
      aria-label="Starts at"
      value={value}
      disabled={disabled}
      onChange={(event) => onChange(event.target.value)}
    />
  ),
}));

vi.mock("../../common/timezone-select", () => ({
  TimezoneSelect: ({
    value,
    onValueChange,
    disabled,
  }: {
    value: string;
    onValueChange: (value: string) => void;
    disabled?: boolean;
  }) => (
    <select
      aria-label="Timezone"
      value={value}
      disabled={disabled}
      onChange={(event) => onValueChange(event.target.value)}
    >
      <option value="Europe/Warsaw">Europe/Warsaw</option>
      <option value="UTC">UTC</option>
    </select>
  ),
}));

vi.mock("@multica/ui/components/ui/switch", () => ({
  Switch: ({
    checked,
    onCheckedChange,
    disabled,
    ...props
  }: {
    checked: boolean;
    onCheckedChange: (checked: boolean) => void;
    disabled?: boolean;
    "aria-label"?: string;
  }) => (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      disabled={disabled}
      onClick={() => onCheckedChange(!checked)}
      {...props}
    />
  ),
}));

vi.mock("sonner", () => ({
  toast: { success: toastSuccess, error: toastError },
}));

import { RuntimeClaimWindowCard } from "./runtime-claim-window-card";

function makeRuntime(overrides: Partial<AgentRuntime> = {}): AgentRuntime {
  return {
    id: "rt-1",
    workspace_id: "ws-1",
    daemon_id: null,
    name: "Claude",
    runtime_mode: "local",
    provider: "claude",
    launch_header: "",
    status: "online",
    device_info: "host.local",
    metadata: {},
    owner_id: "user-me",
    visibility: "private",
    last_seen_at: "2026-06-22T00:00:00Z",
    created_at: "2026-06-01T00:00:00Z",
    updated_at: "2026-06-01T00:00:00Z",
    claim_window_start: null,
    claim_window_timezone: null,
    claim_window_duration_minutes: 300,
    claim_window_open: null,
    claim_window_transition_at: null,
    ...overrides,
  };
}

function renderCard(runtime: AgentRuntime, canEdit = true) {
  return render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <RuntimeClaimWindowCard runtime={runtime} canEdit={canEdit} />
    </I18nProvider>,
  );
}

describe("RuntimeClaimWindowCard", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mutationState.fail = false;
  });

  it("shows an unscheduled runtime as always available", () => {
    renderCard(makeRuntime());
    expect(screen.getByText("Always available")).toBeInTheDocument();
  });

  it("defaults a newly enabled window to 02:00 in the viewing timezone", () => {
    renderCard(makeRuntime());
    fireEvent.click(screen.getByRole("switch", { name: "Use daily window" }));
    expect(screen.getByLabelText("Starts at")).toHaveValue("02:00");
    expect(screen.getByLabelText("Timezone")).toHaveValue("Europe/Warsaw");
    expect(screen.getByText("02:00-07:00 every day")).toBeInTheDocument();
  });

  it("previews a window that crosses midnight", () => {
    renderCard(makeRuntime());
    fireEvent.click(screen.getByRole("switch", { name: "Use daily window" }));
    fireEvent.change(screen.getByLabelText("Starts at"), {
      target: { value: "23:00" },
    });
    expect(screen.getByText("23:00-04:00 every day")).toBeInTheDocument();
  });

  it("saves an enabled schedule", () => {
    renderCard(makeRuntime());
    fireEvent.click(screen.getByRole("switch", { name: "Use daily window" }));
    fireEvent.click(screen.getByRole("button", { name: "Save schedule" }));
    expect(mockMutate).toHaveBeenCalledWith({
      runtimeId: "rt-1",
      patch: {
        claim_window: { start_time: "02:00", timezone: "Europe/Warsaw" },
      },
    });
  });

  it("saves null when a schedule is disabled", () => {
    renderCard(makeRuntime({
      claim_window_start: "02:00",
      claim_window_timezone: "Europe/Warsaw",
      claim_window_open: false,
    }));
    fireEvent.click(screen.getByRole("switch", { name: "Use daily window" }));
    fireEvent.click(screen.getByRole("button", { name: "Save schedule" }));
    expect(mockMutate).toHaveBeenCalledWith({
      runtimeId: "rt-1",
      patch: { claim_window: null },
    });
  });

  it("keeps the draft and reports a failed save", () => {
    mutationState.fail = true;
    renderCard(makeRuntime({
      claim_window_start: "02:00",
      claim_window_timezone: "Europe/Warsaw",
      claim_window_open: false,
    }));
    fireEvent.change(screen.getByLabelText("Starts at"), {
      target: { value: "23:00" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save schedule" }));
    expect(toastError).toHaveBeenCalledWith("Failed to update task claiming schedule");
    expect(screen.getByLabelText("Starts at")).toHaveValue("23:00");
  });

  it("renders schedule state without controls for a read-only viewer", () => {
    renderCard(makeRuntime({
      claim_window_start: "02:00",
      claim_window_timezone: "Europe/Warsaw",
      claim_window_open: false,
    }), false);
    expect(screen.getByText("Scheduled - opens at 02:00")).toBeInTheDocument();
    expect(screen.queryByRole("switch")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Save schedule" })).not.toBeInTheDocument();
  });

  it("uses the server snapshot for open state", () => {
    renderCard(makeRuntime({
      claim_window_start: "02:00",
      claim_window_timezone: "Europe/Warsaw",
      claim_window_open: true,
    }), false);
    expect(screen.getByText("Open until 07:00")).toBeInTheDocument();
  });
});
