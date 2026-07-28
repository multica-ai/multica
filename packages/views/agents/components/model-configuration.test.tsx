// @vitest-environment jsdom

import { afterEach, describe, expect, it, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import enAgents from "../../locales/en/agents.json";
import enCommon from "../../locales/en/common.json";
import {
  CLOUD_PREVIEW_RUNTIME_ID,
  CLOUD_PREVIEW_RUNTIME_IDS,
} from "../../runtimes/components/cloud-preview";
import { ModelConfiguration } from "./model-configuration";

const TEST_RESOURCES = {
  en: { common: enCommon, agents: enAgents },
};

function renderConfiguration({
  runtimeId = CLOUD_PREVIEW_RUNTIME_ID,
  model = "gpt-5.6-sol",
}: {
  runtimeId?: string;
  model?: string;
} = {}) {
  const onThinkingLevelChange = vi.fn();
  const onServiceTierChange = vi.fn();
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <QueryClientProvider client={queryClient}>
        <ModelConfiguration
          runtimeId={runtimeId}
          runtimeOnline
          model={model}
          thinkingLevel=""
          serviceTier=""
          onModelChange={vi.fn()}
          onThinkingLevelChange={onThinkingLevelChange}
          onServiceTierChange={onServiceTierChange}
        />
      </QueryClientProvider>
    </I18nProvider>,
  );
  return { onThinkingLevelChange, onServiceTierChange };
}

describe("ModelConfiguration", () => {
  afterEach(() => {
    cleanup();
    document.body.innerHTML = "";
  });

  it("shows model-specific thinking and Fast Mode options", () => {
    const { onThinkingLevelChange, onServiceTierChange } =
      renderConfiguration();

    fireEvent.click(screen.getByRole("radio", { name: "High" }));
    expect(onThinkingLevelChange).toHaveBeenCalledWith("high");

    fireEvent.click(screen.getByRole("button", { name: /Fast Mode/ }));
    expect(onServiceTierChange).toHaveBeenCalledWith("priority");
  });

  it("hides unsupported options for the Gemini mock catalog", () => {
    renderConfiguration({
      runtimeId: CLOUD_PREVIEW_RUNTIME_IDS.gemini,
      model: "gemini-3.1-pro",
    });

    expect(screen.queryByText("Model options")).toBeNull();
    expect(screen.queryByRole("radio", { name: "High" })).toBeNull();
    expect(screen.queryByRole("button", { name: /Fast Mode/ })).toBeNull();
  });
});
