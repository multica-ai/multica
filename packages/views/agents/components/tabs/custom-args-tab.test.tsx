// @vitest-environment jsdom

import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
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
  custom_args: ["--profile", "research"],
  visibility: "workspace",
  permission_mode: "public_to",
  invocation_targets: [{ target_type: "workspace", target_id: null }],
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

const runtimeDevice = {
  launch_header: "codex app-server",
} as RuntimeDevice;

function renderTab(
  overrides: Partial<Agent> = {},
  onSave = vi.fn().mockResolvedValue(undefined),
) {
  const result = render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <CustomArgsTab
        agent={{ ...baseAgent, ...overrides }}
        runtimeDevice={runtimeDevice}
        onSave={onSave}
      />
    </I18nProvider>,
  );

  return { ...result, onSave };
}

describe("CustomArgsTab", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("renders configured arguments as a list, not persistent inputs", () => {
    renderTab();

    expect(screen.getByText("--profile")).toBeInTheDocument();
    expect(screen.getByText("research")).toBeInTheDocument();
    expect(screen.queryByRole("textbox")).not.toBeInTheDocument();
    expect(screen.getByText("codex app-server --profile research")).toBeInTheDocument();
  });

  it("uses one editor to add one argument token", async () => {
    const user = userEvent.setup();
    renderTab({ custom_args: [] });

    await user.click(screen.getByRole("button", { name: /add argument/i }));
    const input = screen.getByRole("textbox", { name: /new argument/i });
    await user.type(input, "value with spaces");
    await user.click(screen.getByRole("button", { name: /^add$/i }));

    expect(screen.queryByRole("textbox")).not.toBeInTheDocument();
    expect(screen.getByText("value with spaces")).toBeInTheDocument();
  });

  it("edits a list item in place with the same single editor", async () => {
    const user = userEvent.setup();
    renderTab();

    await user.click(screen.getByRole("button", { name: /edit argument 1/i }));
    const input = screen.getByRole("textbox", { name: /argument 1/i });
    await user.clear(input);
    await user.type(input, "--model");
    await user.click(screen.getByRole("button", { name: /update/i }));

    expect(screen.queryByRole("textbox")).not.toBeInTheDocument();
    expect(screen.getByText("--model")).toBeInTheDocument();
    expect(screen.queryByText("--profile")).not.toBeInTheDocument();
  });

  it("preserves spaces inside one token when saving", async () => {
    const user = userEvent.setup();
    const { onSave } = renderTab({ custom_args: [] });

    await user.click(screen.getByRole("button", { name: /add argument/i }));
    await user.type(
      screen.getByRole("textbox", { name: /new argument/i }),
      "value with spaces",
    );
    await user.click(screen.getByRole("button", { name: /^add$/i }));
    await user.click(screen.getByRole("button", { name: /^save$/i }));

    expect(onSave).toHaveBeenCalledWith({ custom_args: ["value with spaces"] });
  });
});
