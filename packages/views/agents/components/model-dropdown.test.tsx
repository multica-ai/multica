// @vitest-environment jsdom

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "@multica/core/i18n/react";
import { runtimeModelsKeys } from "@multica/core/runtimes";
import type { RuntimeDevice, RuntimeModel } from "@multica/core/types";
import enAgents from "../../locales/en/agents.json";
import enCommon from "../../locales/en/common.json";
import enIssues from "../../locales/en/issues.json";
import { ModelDropdown } from "./model-dropdown";

const TEST_RESOURCES = {
  en: { common: enCommon, agents: enAgents, issues: enIssues },
};

// Both suites seed the real query cache rather than mocking
// @multica/core/runtimes: the runtime-default path reads several helpers from
// that module, and replacing the whole module would make one suite's mock
// silently disable the other's behaviour.
const CODEX_MODELS: RuntimeModel[] = [
  { id: "gpt-5.6-sol", label: "GPT-5.6 Sol", provider: "openai", default: true },
  { id: "gpt-5.6-terra", label: "GPT-5.6 Terra", provider: "openai" },
  { id: "gpt-5.6-luna", label: "GPT-5.6 Luna", provider: "openai" },
];

function makePiRuntime(): RuntimeDevice {
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

function makeQueryClient(runtimeId: string, models: RuntimeModel[]) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  queryClient.setQueryData(runtimeModelsKeys.forRuntime(runtimeId), {
    models,
    supported: true,
    cached: false,
  });
  return queryClient;
}

function renderDropdown(models: RuntimeModel[] = []) {
  const runtime = makePiRuntime();
  const onChange = vi.fn();
  render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <QueryClientProvider client={makeQueryClient(runtime.id, models)}>
        <ModelDropdown
          runtime={runtime}
          runtimeId={runtime.id}
          runtimeOnline
          value=""
          onChange={onChange}
        />
      </QueryClientProvider>
    </I18nProvider>,
  );
  return { onChange };
}

function renderPlainDropdown() {
  const onChange = vi.fn();
  const view = render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <QueryClientProvider client={makeQueryClient("rt-codex", CODEX_MODELS)}>
        <ModelDropdown
          runtimeId="rt-codex"
          runtimeOnline
          value=""
          onChange={onChange}
        />
      </QueryClientProvider>
    </I18nProvider>,
  );
  return { ...view, onChange };
}

function openDropdown(container: HTMLElement) {
  const trigger = container.querySelector<HTMLButtonElement>(
    '[data-slot="popover-trigger"]',
  );
  if (!trigger) throw new Error("model dropdown trigger not rendered");
  fireEvent.click(trigger);
}

afterEach(() => cleanup());

describe("ModelDropdown", () => {
  it("offers the gpt-5.6 Codex models and submits their canonical IDs", async () => {
    const { container, onChange } = renderPlainDropdown();
    openDropdown(container);

    expect(await screen.findByText("GPT-5.6 Sol")).toBeTruthy();
    expect(screen.getByText("GPT-5.6 Terra")).toBeTruthy();
    expect(screen.getByText("GPT-5.6 Luna")).toBeTruthy();
    expect(screen.getByText("gpt-5.6-sol")).toBeTruthy();
    expect(screen.getByText("gpt-5.6-terra")).toBeTruthy();
    expect(screen.getByText("gpt-5.6-luna")).toBeTruthy();

    fireEvent.click(screen.getByText("GPT-5.6 Terra"));
    expect(onChange).toHaveBeenCalledWith("gpt-5.6-terra");
  });
});

describe("ModelDropdown runtime default display", () => {
  it("keeps the runtime default visible even when live discovery has no models", () => {
    const { onChange } = renderDropdown();

    expect(
      screen.getByText("Runtime default: deepseek-v4-pro"),
    ).toBeInTheDocument();
    expect(screen.getByText("deepseek · inherited from ys-pi")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: /Runtime default/i }));

    expect(screen.getByText("Use runtime default")).toBeInTheDocument();
    expect(screen.getByText("deepseek-v4-pro · deepseek")).toBeInTheDocument();
    expect(screen.getByText("deepseek-v4-flash")).toBeInTheDocument();
    expect(screen.queryByText("No models available.")).not.toBeInTheDocument();

    fireEvent.click(
      screen.getByRole("button", { name: /deepseek-v4-flash/i }),
    );

    expect(onChange).toHaveBeenCalledWith("deepseek-v4-flash");
  });

  it("hides Pi no-model discovery noise from the model list", () => {
    renderDropdown([{ id: "No/models", label: "No/models", provider: "No" }]);

    fireEvent.click(screen.getByRole("button", { name: /Runtime default/i }));

    expect(screen.getByText("Use runtime default")).toBeInTheDocument();
    expect(screen.getByText("deepseek-v4-flash")).toBeInTheDocument();
    expect(screen.queryByText("No/models")).not.toBeInTheDocument();

    fireEvent.change(screen.getByPlaceholderText("Search or type a model ID"), {
      target: { value: "No/models" },
    });

    expect(screen.queryByText('Use "No/models"')).not.toBeInTheDocument();
  });
});
