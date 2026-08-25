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

// Discovery outcome for the next render. resolveRuntimeModels rejects with the
// daemon's reported error text, so a failure is modelled as a throwing queryFn.
let discovery: () => Promise<RuntimeModelsResult> = async () => CODEX_MODELS;

vi.mock("@multica/core/runtimes", () => ({
  runtimeModelsOptions: (runtimeId: string | null) => ({
    enabled: Boolean(runtimeId),
    queryKey: ["runtime-models", runtimeId, discoveryKey],
    queryFn: () => discovery(),
  }),
}));

// Bumped per test so React Query cannot serve a previous case's cached result.
let discoveryKey = 0;

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
  afterEach(() => {
    cleanup();
    discovery = async () => CODEX_MODELS;
    discoveryKey += 1;
  });

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

  // MUL-6606: a runtime that could not enumerate its models used to report an
  // empty catalog with no error, which rendered as an authoritative empty
  // dropdown. The reason has to reach the user, because for hermes it names the
  // exact command that fixes the problem.
  it("shows the runtime's own reason when discovery fails", async () => {
    const reason =
      "ACP model discovery session/new failed: No LLM provider configured. " +
      "Run `hermes model` to select a provider.";
    discovery = async () => {
      throw new Error(reason);
    };

    const { container } = renderDropdown();
    openDropdown(container);

    expect(await screen.findByText(reason)).toBeTruthy();
    // And the picker says so up front, rather than looking like an empty catalog.
    expect(screen.getByText(enAgents.model_dropdown.discovery_failed)).toBeTruthy();
    expect(
      screen.queryByText(enAgents.pickers.model_empty_with_dot),
    ).toBeNull();
  });

  // A reason with no way forward is a dead end: manual entry is the documented
  // fallback for a failed discovery, so it must survive one.
  it("still accepts a manually typed model ID after a failed discovery", async () => {
    discovery = async () => {
      throw new Error("discovery blew up");
    };

    const { container, onChange } = renderDropdown();
    openDropdown(container);

    await screen.findByText("discovery blew up");
    // The popover renders through a portal, so reach it via screen, not container.
    const input = screen.getByPlaceholderText(
      enAgents.pickers.model_search_placeholder,
    );
    fireEvent.change(input, { target: { value: "vertex/gemini-3.1-pro" } });

    fireEvent.click(await screen.findByText(/vertex\/gemini-3\.1-pro/));
    expect(onChange).toHaveBeenCalledWith("vertex/gemini-3.1-pro");
  });
});
