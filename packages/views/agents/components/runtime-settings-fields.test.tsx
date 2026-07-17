// @vitest-environment jsdom

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { runtimeModelsKeys } from "@multica/core/runtimes";
import { RuntimeSettingsFields } from "./runtime-settings-fields";

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
      </QueryClientProvider>,
    );

    const [effort, speed] = screen.getAllByRole("combobox");
    fireEvent.change(effort!, { target: { value: "high" } });
    fireEvent.change(speed!, { target: { value: "fast" } });
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
      </QueryClientProvider>,
    );

    expect(screen.queryByRole("combobox")).toBeNull();
  });
});
