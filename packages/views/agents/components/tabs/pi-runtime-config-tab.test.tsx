// @vitest-environment jsdom

import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Agent, AgentRuntime } from "@multica/core/types";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../../locales/en/common.json";
import enAgents from "../../../locales/en/agents.json";

const TEST_RESOURCES = { en: { common: enCommon, agents: enAgents } };
const getAgentEnv = vi.hoisted(() => vi.fn());
const updateAgentEnv = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/api", () => ({
  api: { getAgentEnv, updateAgentEnv },
}));

vi.mock("sonner", () => ({
  toast: { error: vi.fn(), success: vi.fn() },
}));

import { PiRuntimeConfigTab } from "./pi-runtime-config-tab";

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

const piRuntime: AgentRuntime = {
  id: "runtime-1",
  workspace_id: "ws-1",
  daemon_id: "daemon-1",
  name: "Pi",
  runtime_mode: "local",
  provider: "pi",
  launch_header: "",
  status: "online",
  device_info: "host",
  metadata: {},
  default_model_config: {
    provider: "deepseek",
    api: "openai-completions",
    base_url: "https://api.deepseek.com",
    model: "deepseek-v4-flash",
  },
  has_default_model_api_key: true,
  owner_id: "user-1",
  visibility: "private",
  last_seen_at: "2026-08-04T00:00:00Z",
  created_at: "2026-08-04T00:00:00Z",
  updated_at: "2026-08-04T00:00:00Z",
};

function renderTab(
  onSave = vi.fn().mockResolvedValue(undefined),
  runtime?: AgentRuntime,
  agent: Agent = baseAgent,
) {
  render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <PiRuntimeConfigTab
        agent={agent}
        runtime={runtime}
        onSave={onSave}
      />
    </I18nProvider>,
  );
  return onSave;
}

describe("PiRuntimeConfigTab", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    getAgentEnv.mockResolvedValue({
      agent_id: "agent-1",
      custom_env: { OTHER_SECRET: "preserve", PI_MULTICA_API_KEY: "old-key" },
    });
    updateAgentEnv.mockImplementation(async (_id, body) => ({
      agent_id: "agent-1",
      custom_env: body.custom_env,
    }));
  });

  it("does not reveal secret environment values on mount", () => {
    renderTab();
    expect(getAgentEnv).not.toHaveBeenCalled();
    expect(screen.queryByLabelText("LLM API key")).not.toBeInTheDocument();
  });

  it("shows the inherited runtime connection as the default path", () => {
    renderTab(undefined, piRuntime);

    expect(screen.getByText("Using the runtime default")).toBeInTheDocument();
    expect(
      screen.getByText(/deepseek \/ deepseek-v4-flash is inherited/),
    ).toBeInTheDocument();
    expect(screen.getByLabelText("Model ID")).toHaveValue(
      "deepseek-v4-flash",
    );
    expect(getAgentEnv).not.toHaveBeenCalled();
  });

  it("allows a model-only override to keep using the runtime key", async () => {
    const user = userEvent.setup();
    const onSave = renderTab(undefined, piRuntime);

    const model = screen.getByLabelText("Model ID");
    await user.clear(model);
    await user.type(model, "deepseek-v4-pro");
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(getAgentEnv).not.toHaveBeenCalled();
    expect(onSave).toHaveBeenCalledWith({
      runtime_config: {
        provider: "deepseek",
        api: "openai-completions",
        base_url: "https://api.deepseek.com",
        model: "deepseek-v4-pro",
      },
    });
  });

  it("requires an agent key when overriding the provider endpoint", async () => {
    const user = userEvent.setup();
    renderTab(undefined, piRuntime);

    const provider = screen.getByLabelText("Provider ID");
    await user.clear(provider);
    await user.type(provider, "openai");

    expect(
      screen.getByText(/endpoint differs from the runtime default/),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Save" })).toBeDisabled();
  });

  it("can remove an agent override and return to the runtime default", async () => {
    const user = userEvent.setup();
    const onSave = vi.fn().mockResolvedValue(undefined);
    renderTab(onSave, piRuntime, {
      ...baseAgent,
      runtime_config: {
        provider: "openai",
        api: "openai-responses",
        base_url: "https://api.openai.com/v1",
        model: "gpt-5",
      },
    });

    expect(screen.getByText("Agent override")).toBeInTheDocument();
    await user.click(
      screen.getByRole("button", { name: "Use runtime default" }),
    );
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(onSave).toHaveBeenCalledWith({ runtime_config: {} });
  });

  it("saves the API key through the env endpoint and preserves unrelated keys", async () => {
    const user = userEvent.setup();
    const onSave = renderTab();

    await user.type(screen.getByLabelText("Provider ID"), "openai");
    await user.type(screen.getByLabelText("Base URL"), "https://api.example.com/v1");
    await user.type(screen.getByLabelText("Model ID"), "gpt-5");
    await user.click(screen.getByRole("button", { name: "Reveal & edit" }));
    const keyInput = await screen.findByLabelText("LLM API key");
    await user.clear(keyInput);
    await user.type(keyInput, "new-key");
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(updateAgentEnv).toHaveBeenCalledWith("agent-1", {
      custom_env: {
        OTHER_SECRET: "preserve",
        PI_MULTICA_API_KEY: "new-key",
      },
    });
    expect(onSave).toHaveBeenCalledWith({
      runtime_config: {
        provider: "openai",
        api: "openai-responses",
        base_url: "https://api.example.com/v1",
        model: "gpt-5",
      },
    });
  });
});
