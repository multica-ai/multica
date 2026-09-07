// @vitest-environment jsdom

import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { AgentRuntime } from "@multica/core/types";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enRuntimes from "../../locales/en/runtimes.json";

const TEST_RESOURCES = {
  en: { common: enCommon, runtimes: enRuntimes },
};

const updateMutate = vi.hoisted(() => vi.fn());
const deleteMutate = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/runtimes/mutations", () => ({
  useUpdateRuntimeModelConnection: () => ({
    mutate: updateMutate,
    isPending: false,
  }),
  useDeleteRuntimeModelConnection: () => ({
    mutate: deleteMutate,
    isPending: false,
  }),
}));

vi.mock("sonner", () => ({
  toast: { error: vi.fn(), success: vi.fn() },
}));

import { PiModelConnectionCard } from "./pi-model-connection-card";

const runtime: AgentRuntime = {
  id: "runtime-1",
  workspace_id: "workspace-1",
  daemon_id: "daemon-1",
  name: "Pi",
  runtime_mode: "local",
  provider: "pi",
  launch_header: "",
  status: "online",
  device_info: "host",
  metadata: {},
  default_model_config: {},
  has_default_model_api_key: false,
  owner_id: "user-1",
  visibility: "private",
  last_seen_at: "2026-08-04T00:00:00Z",
  created_at: "2026-08-04T00:00:00Z",
  updated_at: "2026-08-04T00:00:00Z",
};

const configuredRuntime: AgentRuntime = {
  ...runtime,
  default_model_config: {
    provider: "deepseek",
    api: "openai-completions",
    base_url: "https://api.deepseek.com",
    model: "deepseek-v4-pro",
  },
  has_default_model_api_key: true,
};

function renderCard(value: AgentRuntime = runtime) {
  render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <PiModelConnectionCard
        runtime={value}
        wsId="workspace-1"
        canEdit
      />
    </I18nProvider>,
  );
}

describe("PiModelConnectionCard", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("turns an unconfigured runtime into a DeepSeek connection", async () => {
    const user = userEvent.setup();
    renderCard();

    expect(screen.getByText("Needs model setup")).toBeInTheDocument();
    await user.click(
      screen.getByRole("button", { name: "Configure model" }),
    );

    const key = screen.getByLabelText("API key");
    expect(screen.getByLabelText("Model provider")).toHaveValue("deepseek");
    expect(
      screen.getByRole("listbox", { name: "Model" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("option", { name: /deepseek-v4-flash/i }),
    ).toHaveAttribute("aria-selected", "true");
    expect(screen.getByText("Default")).toBeInTheDocument();
    expect(
      screen.getByRole("option", { name: /deepseek-v4-pro/i }),
    ).toHaveAttribute("aria-selected", "false");
    await user.click(
      screen.getByRole("option", { name: /deepseek-v4-pro/i }),
    );
    expect(
      screen.getByRole("button", { name: "Save connection" }),
    ).toBeDisabled();

    await user.type(key, "sk-deepseek");
    await user.click(
      screen.getByRole("button", { name: "Save connection" }),
    );

    expect(updateMutate).toHaveBeenCalledWith(
      {
        runtimeId: "runtime-1",
        connection: {
          provider: "deepseek",
          api: "openai-completions",
          base_url: "https://api.deepseek.com",
          model: "deepseek-v4-pro",
          api_key: "sk-deepseek",
        },
      },
      expect.any(Object),
    );
  });

  it("saves another provider preset with its official endpoint", async () => {
    const user = userEvent.setup();
    renderCard();

    await user.click(
      screen.getByRole("button", { name: "Configure model" }),
    );
    await user.selectOptions(screen.getByLabelText("Model provider"), "xai");

    expect(
      screen.getByRole("option", { name: /grok-4\.5/i }),
    ).toHaveAttribute("aria-selected", "true");
    expect(screen.queryByText("grok-4.3")).not.toBeInTheDocument();

    await user.type(screen.getByLabelText("API key"), "xai-key");
    await user.click(
      screen.getByRole("button", { name: "Save connection" }),
    );

    expect(updateMutate).toHaveBeenCalledWith(
      {
        runtimeId: "runtime-1",
        connection: {
          provider: "xai",
          api: "openai-completions",
          base_url: "https://api.x.ai/v1",
          model: "grok-4.5",
          api_key: "xai-key",
        },
      },
      expect.any(Object),
    );
  });

  it("shows the configured model without exposing a key", () => {
    renderCard(configuredRuntime);

    expect(screen.getByText("Ready")).toBeInTheDocument();
    expect(screen.getByText("deepseek-v4-pro")).toBeInTheDocument();
    expect(screen.queryByText(/sk-/)).not.toBeInTheDocument();
  });

  it("removes a configured model connection after confirmation", async () => {
    const user = userEvent.setup();
    renderCard(configuredRuntime);

    await user.click(
      screen.getByRole("button", { name: "Edit connection" }),
    );
    await user.click(
      screen.getByRole("button", { name: "Remove connection" }),
    );

    expect(
      screen.getByRole("heading", {
        name: "Remove Pi model connection?",
      }),
    ).toBeInTheDocument();

    await user.click(
      screen.getByRole("button", { name: "Remove model connection" }),
    );

    expect(deleteMutate).toHaveBeenCalledWith(
      "runtime-1",
      expect.objectContaining({
        onSuccess: expect.any(Function),
        onError: expect.any(Function),
      }),
    );
  });
});
