// @vitest-environment jsdom

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, fireEvent, cleanup } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import type { RuntimeModelsResult } from "@multica/core/types";
import { afterEach, describe, expect, it, vi } from "vitest";
import enAgents from "../../locales/en/agents.json";
import enCommon from "../../locales/en/common.json";
import enIssues from "../../locales/en/issues.json";
import { ModelDropdown } from "./model-dropdown";

const TEST_RESOURCES = {
  en: { common: enCommon, agents: enAgents, issues: enIssues },
};

const CODEX_MODELS: RuntimeModelsResult = {
  models: [
    {
      id: "gpt-5.6-sol",
      label: "GPT-5.6 Sol",
      provider: "openai",
      default: true,
    },
    { id: "gpt-5.6-terra", label: "GPT-5.6 Terra", provider: "openai" },
    { id: "gpt-5.6-luna", label: "GPT-5.6 Luna", provider: "openai" },
  ],
  supported: true,
};

vi.mock("@multica/core/runtimes", () => ({
  runtimeModelsOptions: (runtimeId: string | null) => ({
    enabled: Boolean(runtimeId),
    queryKey: ["runtime-models", runtimeId],
    queryFn: async () => CODEX_MODELS,
  }),
}));

function renderDropdown() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const onChange = vi.fn();
  const view = render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <QueryClientProvider client={queryClient}>
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

describe("ModelDropdown", () => {
  afterEach(() => cleanup());

  it("offers the gpt-5.6 Codex models and submits their canonical IDs", async () => {
    const { container, onChange } = renderDropdown();
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
