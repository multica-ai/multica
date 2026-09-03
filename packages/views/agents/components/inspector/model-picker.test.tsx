// @vitest-environment jsdom

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import type { RuntimeModelsResult } from "@multica/core/types";
import { afterEach, describe, expect, it, vi } from "vitest";
import enAgents from "../../../locales/en/agents.json";
import enCommon from "../../../locales/en/common.json";
import enIssues from "../../../locales/en/issues.json";
import { ModelPicker } from "./model-picker";

const TEST_RESOURCES = {
  en: { common: enCommon, agents: enAgents, issues: enIssues },
};

// MUL-6961. The create-agent dropdown and this inspector picker are two
// separate renderers over one catalog, and the first attempt at unavailable
// models only taught the dropdown about them — leaving the inspector, which is
// where an ALREADY configured agent gets edited, still offering a model whose
// every run 400s. That asymmetry is what this file exists to prevent.
const CLAUDE_CATALOG: RuntimeModelsResult = {
  models: [{ id: "claude-fable-5", label: "Fable", provider: "anthropic" }],
  unavailableModels: [
    {
      id: "cc-update-required-1",
      label: "Fable 5.1 (disabled)",
      reason: "Update to 2.1.255+ to use Fable 5.1",
    },
  ],
  supported: true,
};

let discovery: () => Promise<RuntimeModelsResult> = async () => CLAUDE_CATALOG;
let discoveryKey = 0;
const mockRefreshRuntimeModels = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/runtimes", () => ({
  runtimeModelsOptions: (runtimeId: string | null) => ({
    enabled: Boolean(runtimeId),
    queryKey: ["runtime-models", runtimeId, discoveryKey],
    queryFn: () => discovery(),
  }),
  refreshRuntimeModels: (...args: unknown[]) =>
    mockRefreshRuntimeModels(...args),
}));

function renderPicker() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const onChange = vi.fn(async () => {});
  const view = render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <QueryClientProvider client={queryClient}>
        <ModelPicker
          runtimeId="rt-claude"
          runtimeOnline
          value=""
          onChange={onChange}
        />
      </QueryClientProvider>
    </I18nProvider>,
  );
  return { ...view, onChange, queryClient };
}

function openPicker(container: HTMLElement) {
  const trigger = container.querySelector<HTMLButtonElement>("button");
  if (!trigger) throw new Error("model picker trigger not rendered");
  fireEvent.click(trigger);
}

describe("ModelPicker (inspector)", () => {
  afterEach(() => {
    cleanup();
    discovery = async () => CLAUDE_CATALOG;
    discoveryKey += 1;
    mockRefreshRuntimeModels.mockReset();
  });

  it("offers the runnable model", async () => {
    const { container, onChange } = renderPicker();
    openPicker(container);

    fireEvent.click(await screen.findByText("Fable"));
    expect(onChange).toHaveBeenCalledWith("claude-fable-5");
  });

  it("shows an unavailable model with its reason and no way to select it", async () => {
    const { container, onChange } = renderPicker();
    openPicker(container);

    const row = await screen.findByText("Fable 5.1 (disabled)");
    expect(
      screen.getByText("Update to 2.1.255+ to use Fable 5.1"),
    ).toBeTruthy();

    fireEvent.click(row);
    expect(onChange).not.toHaveBeenCalled();
    expect(row.closest("button")).toBeNull();
  });

  // A backend older than the field sends nothing, and the picker must simply
  // not render the section rather than break or show an empty heading.
  it("renders no unavailable section when the backend omits the field", async () => {
    discovery = async () => ({
      models: [{ id: "claude-fable-5", label: "Fable", provider: "anthropic" }],
      supported: true,
    });

    const { container } = renderPicker();
    openPicker(container);

    await screen.findByText("Fable");
    expect(
      screen.queryByText(enAgents.pickers.model_unavailable_heading),
    ).toBeNull();
  });

  it("exposes the live-catalog refresh from the inspector picker", async () => {
    mockRefreshRuntimeModels.mockResolvedValue(CLAUDE_CATALOG);
    const { container, queryClient } = renderPicker();
    openPicker(container);
    await screen.findByText("Fable");
    fireEvent.click(
      screen.getByRole("button", { name: enAgents.pickers.model_refresh }),
    );

    expect(mockRefreshRuntimeModels).toHaveBeenCalledWith(
      queryClient,
      "rt-claude",
    );
    queryClient.clear();
  });
});
