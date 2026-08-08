// @vitest-environment jsdom

import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider } from "@multica/core/i18n/react";
import type { AgentRuntime } from "@multica/core/types";
import enCommon from "../../locales/en/common.json";
import enRuntimes from "../../locales/en/runtimes.json";
import { PiModelConnectionDialog } from "./pi-model-connection-dialog";

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

function renderDialog(value: AgentRuntime = runtime) {
  const onOpenChange = vi.fn();
  render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <PiModelConnectionDialog
        runtime={value}
        wsId="workspace-1"
        open
        onOpenChange={onOpenChange}
      />
    </I18nProvider>,
  );
  return { onOpenChange };
}

describe("PiModelConnectionDialog", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("saves a provider preset with the entered API key", async () => {
    const user = userEvent.setup();
    renderDialog();

    await user.selectOptions(screen.getByLabelText("Model provider"), "xai");
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

  it("keeps a saved API key when the field is left blank", async () => {
    const user = userEvent.setup();
    renderDialog(configuredRuntime);

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
        },
      },
      expect.any(Object),
    );
  });

  it("blocks saving an invalid custom base URL", async () => {
    const user = userEvent.setup();
    renderDialog(configuredRuntime);

    await user.selectOptions(screen.getByLabelText("Model provider"), "custom");
    const baseUrl = screen.getByLabelText("Base URL");
    await user.clear(baseUrl);
    await user.type(baseUrl, "http://api.example.com/v1");

    expect(
      screen.getByText(
        "Enter an HTTPS URL. HTTP is allowed only for a loopback address.",
      ),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Save connection" }),
    ).toBeDisabled();
  });

  it("deletes a saved connection after confirmation", async () => {
    const user = userEvent.setup();
    renderDialog(configuredRuntime);

    await user.click(
      screen.getByRole("button", { name: "Remove connection" }),
    );
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
