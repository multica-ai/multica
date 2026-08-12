// @vitest-environment jsdom

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { I18nProvider } from "@multica/core/i18n/react";
import { runtimeModelsKeys } from "@multica/core/runtimes";
import type { AgentRuntime, RuntimeModel } from "@multica/core/types";
import enAgents from "../../../locales/en/agents.json";
import enCommon from "../../../locales/en/common.json";
import enIssues from "../../../locales/en/issues.json";
import { ModelPicker } from "./model-picker";

const TEST_RESOURCES = {
  en: { common: enCommon, agents: enAgents, issues: enIssues },
};

function makePiRuntime(): AgentRuntime {
  return {
    id: "rt-pi",
    workspace_id: "ws-1",
    daemon_id: "daemon-1",
    name: "ys-pi",
    runtime_mode: "local",
    provider: "pi",
    launch_header: "",
    status: "online",
    device_info: "macOS",
    metadata: {},
    default_model_config: {
      provider: "deepseek",
      api: "openai-completions",
      base_url: "https://api.deepseek.com",
      model: "deepseek-v4-pro",
    },
    has_default_model_api_key: true,
    owner_id: "user-1",
    visibility: "private",
    last_seen_at: "2026-08-06T00:00:00Z",
    created_at: "2026-08-06T00:00:00Z",
    updated_at: "2026-08-06T00:00:00Z",
  };
}

function renderPicker(models: RuntimeModel[] = []) {
  const runtime = makePiRuntime();
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  queryClient.setQueryData(runtimeModelsKeys.forRuntime(runtime.id), {
    models,
    supported: true,
    cached: false,
  });

  const onChange = vi.fn();
  render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <QueryClientProvider client={queryClient}>
        <ModelPicker
          runtime={runtime}
          runtimeId={runtime.id}
          runtimeOnline
          value=""
          variant="field"
          showLabel={false}
          onChange={onChange}
        />
      </QueryClientProvider>
    </I18nProvider>,
  );
  return { onChange };
}

describe("ModelPicker runtime default display", () => {
  it("shows the Pi runtime default as the selected empty model", () => {
    const { onChange } = renderPicker();

    expect(
      screen.getByText("Runtime default: deepseek-v4-pro"),
    ).toBeInTheDocument();
    expect(screen.getByText("deepseek · inherited from ys-pi")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button"));

    expect(screen.getByText("Use runtime default")).toBeInTheDocument();
    expect(screen.getByText("deepseek-v4-pro · deepseek")).toBeInTheDocument();
    expect(screen.getByText("deepseek-v4-flash")).toBeInTheDocument();
    expect(screen.queryByText("No models available")).not.toBeInTheDocument();

    fireEvent.click(screen.getByText("deepseek-v4-flash"));

    expect(onChange).toHaveBeenCalledWith("deepseek-v4-flash");
  });

  it("hides Pi no-model discovery noise from the model list", () => {
    renderPicker([{ id: "No/models", label: "No/models", provider: "No" }]);

    fireEvent.click(screen.getByRole("button"));

    expect(screen.getByText("Use runtime default")).toBeInTheDocument();
    expect(screen.getByText("deepseek-v4-flash")).toBeInTheDocument();
    expect(screen.queryByText("No/models")).not.toBeInTheDocument();

    fireEvent.change(
      screen.getByRole("textbox", {
        name: "Search or type a model ID",
      }),
      {
        target: { value: "No/models" },
      },
    );

    expect(screen.queryByText('Use "No/models"')).not.toBeInTheDocument();
  });
});
