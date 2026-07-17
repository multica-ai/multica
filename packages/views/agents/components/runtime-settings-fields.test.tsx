// @vitest-environment jsdom

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { runtimeModelsKeys } from "@multica/core/runtimes";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enAgents from "../../locales/en/agents.json";
import enIssues from "../../locales/en/issues.json";
import { RuntimeSettingsFields } from "./runtime-settings-fields";

const resources = { en: { common: enCommon, agents: enAgents, issues: enIssues } };

describe("RuntimeSettingsFields", () => {
  it("shows and returns the selected model's runtime-native effort and speed", () => {
    const client = new QueryClient();
    client.setQueryData(runtimeModelsKeys.forRuntime("rt-codex"), {
      supported: true,
      models: [{
        id: "gpt-5.6-sol",
        label: "GPT-5.6 Sol",
        default: true,
        thinking: { supported_levels: [{ value: "medium", label: "Medium" }, { value: "high", label: "High" }] },
        speed: { supported_levels: [{ value: "standard", label: "Standard" }, { value: "fast", label: "Fast" }] },
      }],
    });
    const onThinkingChange = vi.fn();
    const onSpeedChange = vi.fn();

    render(
      <I18nProvider locale="en" resources={resources}>
        <QueryClientProvider client={client}>
          <RuntimeSettingsFields
            runtimeId="rt-codex"
            runtimeOnline
            model="gpt-5.6-sol"
            thinkingLevel=""
            speedMode="standard"
            onThinkingChange={onThinkingChange}
            onSpeedChange={onSpeedChange}
          />
        </QueryClientProvider>
      </I18nProvider>,
    );

    fireEvent.click(screen.getByRole("button", { name: /Effort: Follow runtime default/i }));
    fireEvent.change(screen.getByRole("textbox", { name: /Search effort/i }), { target: { value: "high" } });
    fireEvent.click(screen.getByRole("button", { name: "High" }));

    fireEvent.click(screen.getByRole("button", { name: /Speed: Standard/i }));
    fireEvent.change(screen.getByRole("textbox", { name: /Search speed/i }), { target: { value: "fast" } });
    fireEvent.click(screen.getByRole("button", { name: "Fast" }));
    expect(onThinkingChange).toHaveBeenCalledWith("high");
    expect(onSpeedChange).toHaveBeenCalledWith("fast");
  });

  it("hides settings that the selected runtime model does not advertise", () => {
    const client = new QueryClient();
    client.setQueryData(runtimeModelsKeys.forRuntime("rt-gateway"), {
      supported: true,
      models: [{ id: "gateway-model", label: "Gateway model", default: true }],
    });

    render(
      <I18nProvider locale="en" resources={resources}>
        <QueryClientProvider client={client}>
          <RuntimeSettingsFields
            runtimeId="rt-gateway"
            runtimeOnline
            model="gateway-model"
            thinkingLevel=""
            speedMode=""
            onThinkingChange={vi.fn()}
            onSpeedChange={vi.fn()}
          />
        </QueryClientProvider>
      </I18nProvider>,
    );

    expect(screen.queryByRole("button", { name: /Effort|Speed/i })).toBeNull();
  });
});
