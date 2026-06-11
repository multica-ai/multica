// @vitest-environment jsdom

import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Agent, RuntimeDevice } from "@multica/core/types";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../../locales/en/common.json";
import enAgents from "../../../locales/en/agents.json";

const TEST_RESOURCES = { en: { common: enCommon, agents: enAgents } };

vi.mock("sonner", () => ({
  toast: {
    error: vi.fn(),
    success: vi.fn(),
  },
}));

import { CustomArgsTab } from "./custom-args-tab";

const baseAgent: Agent = {
  id: "agent-1",
  workspace_id: "ws-1",
  runtime_id: "runtime-1",
  name: "Agent",
  description: "",
  instructions: "",
  avatar_url: null,
  runtime_mode: "local",
  runtime_config: {},
  custom_args: [],
  visibility: "private",
  status: "idle",
  max_concurrent_tasks: 1,
  model: "",
  owner_id: "user-1",
  skills: [],
  created_at: "2026-05-28T00:00:00Z",
  updated_at: "2026-05-28T00:00:00Z",
  archived_at: null,
  archived_by: null,
};

const runtimeDevice: RuntimeDevice = {
  id: "runtime-1",
  workspace_id: "ws-1",
  daemon_id: null,
  name: "Claude Runtime",
  runtime_mode: "local",
  provider: "claude",
  launch_header: "claude (stream-json)",
  status: "online",
  device_info: "host.local",
  metadata: {},
  owner_id: "user-1",
  visibility: "private",
  last_seen_at: "2026-05-28T00:00:00Z",
  created_at: "2026-05-28T00:00:00Z",
  updated_at: "2026-05-28T00:00:00Z",
};

function renderTab(
  overrides: Partial<Agent> = {},
  onSave = vi.fn().mockResolvedValue(undefined),
) {
  const agent = { ...baseAgent, ...overrides };
  const result = render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <CustomArgsTab agent={agent} runtimeDevice={runtimeDevice} onSave={onSave} />
    </I18nProvider>,
  );
  return { ...result, onSave };
}

describe("CustomArgsTab", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("edits, removes, appends, splits, and saves the full custom_args array", async () => {
    const user = userEvent.setup();
    const { onSave } = renderTab({
      custom_args: ["--permission-mode", "acceptEdits", "--remove-me"],
    });

    expect(screen.getByText(/Launch mode:/)).toBeInTheDocument();
    expect(screen.getByText(/claude \(stream-json\)/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Save" })).toBeDisabled();

    const inputs = screen.getAllByPlaceholderText("--flag value");
    await user.clear(inputs[0]!);
    await user.type(inputs[0]!, "--permission-mode bypassPermissions");
    await user.click(screen.getAllByRole("button", { name: "Remove argument" })[2]!);
    await user.click(screen.getByRole("button", { name: "Add" }));
    await user.type(screen.getAllByPlaceholderText("--flag value").at(-1)!, "--max-turns 100");

    await user.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() =>
      expect(onSave).toHaveBeenCalledWith({
        custom_args: [
          "--permission-mode",
          "bypassPermissions",
          "acceptEdits",
          "--max-turns",
          "100",
        ],
      }),
    );
  });
});
