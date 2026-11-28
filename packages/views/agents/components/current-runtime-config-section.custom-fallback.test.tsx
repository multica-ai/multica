// @vitest-environment jsdom

// Entering custom mode (or editing any other field) saves the
// recommended template config as-is. When the template model has left the
// runtime's live catalog, the server refuses that save — and refusing it
// deadlocks the screen, because picking a new model requires entering custom
// mode first. The section must retry once with the runtime-native template
// fields cleared (model/thinking/speed, matching what switching runtimes
// already does), but a hand-picked model id keeps its named-model refusal.

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, waitFor } from "@testing-library/react";
import type { Agent, AgentRuntime } from "@multica/core/types";

const mockToast = vi.hoisted(() => ({
  success: vi.fn(),
  error: vi.fn(),
  warning: vi.fn(),
}));

vi.mock("sonner", () => ({ toast: mockToast }));

const mockGetAgentRuntimeCurrentConfig = vi.hoisted(() => vi.fn());
const mockUpdateAgentRuntimeCurrentConfig = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/api", () => ({
  api: {
    getAgentRuntimeCurrentConfig: (...args: unknown[]) =>
      mockGetAgentRuntimeCurrentConfig(...args),
    updateAgentRuntimeCurrentConfig: (...args: unknown[]) =>
      mockUpdateAgentRuntimeCurrentConfig(...args),
  },
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("./inspector/runtime-picker", () => ({
  RuntimePicker: () => <div data-testid="runtime-picker" />,
}));
vi.mock("./inspector/model-picker", () => ({
  ModelPicker: ({
    value,
    onChange,
  }: {
    value: string;
    onChange: (next: string) => Promise<void> | void;
  }) => (
    <button
      type="button"
      data-testid="pick-outside-model"
      onClick={() => void onChange("custom-outside-model")}
    >
      {value}
    </button>
  ),
}));
vi.mock("./inspector/thinking-prop-row", () => ({
  ThinkingSettingField: () => <div data-testid="thinking-field" />,
}));
vi.mock("./inspector/service-tier-setting-field", () => ({
  ServiceTierSettingField: () => <div data-testid="service-tier-field" />,
}));
vi.mock("./inspector/concurrency-setting-field", () => ({
  ConcurrencySettingField: ({
    onChange,
  }: {
    onChange: (next: number) => Promise<void> | void;
  }) => (
    <button
      type="button"
      data-testid="set-concurrency"
      onClick={() => void onChange(3)}
    >
      concurrency
    </button>
  ),
}));

import { renderWithI18n } from "../../test/i18n";

import { CurrentRuntimeConfigSection } from "./current-runtime-config-section";

const codexRuntime = {
  id: "codex-runtime-1",
  workspace_id: "ws-1",
  daemon_id: "daemon-1",
  name: "Codex Runtime",
  runtime_mode: "local",
  provider: "codex",
  launch_header: "",
  status: "online",
  device_info: "",
  metadata: {},
  owner_id: "user-1",
  visibility: "private",
  last_seen_at: null,
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
} satisfies AgentRuntime;

const agent = {
  id: "agent-1",
  workspace_id: "ws-1",
  name: "Smart agent",
  share_runtime: false,
} as Agent;

function summaryPayload(current: Record<string, unknown>) {
  return {
    recommended: {
      cli: "codex",
      runtime_id: codexRuntime.id,
      model: "gpt-6.2-sol",
      thinking_level: "",
      service_tier: "",
      max_concurrent_tasks: 1,
      runtime_config: {},
      editable: true,
    },
    current: {
      runtime_id: codexRuntime.id,
      max_concurrent_tasks: 1,
      runtime_config: {},
      notice_required: false,
      resolution_status: "ready",
      ...current,
    },
  };
}

function recommendedWithTemplateModel() {
  return summaryPayload({
    model: "gpt-6.2-sol",
    thinking_level: "",
    service_tier: "",
    strategy: "recommended",
    source: "recommended_match",
    persisted: false,
    editable: true,
  });
}

function customSummary(model: string) {
  return summaryPayload({
    model,
    thinking_level: "",
    service_tier: "",
    strategy: "custom",
    source: "user_override",
    persisted: true,
    editable: true,
  });
}

function modelUnavailableError(model: string): Error {
  return Object.assign(new Error("conflict"), {
    status: 409,
    body: {
      code: "model_unavailable",
      resolution_status: "model_unavailable",
      error: `模型 ${model} 已不在该运行时的模型目录中，请更新模型配置后重试`,
      resolution_message: `模型 ${model} 已不在该运行时的模型目录中，请更新模型配置后重试`,
    },
  });
}

function runtimeOfflineError(): Error {
  return Object.assign(new Error("conflict"), {
    status: 409,
    body: {
      code: "runtime_offline",
      resolution_status: "runtime_offline",
      resolution_message: "运行时离线",
    },
  });
}

function renderSection(queryClient: QueryClient) {
  return renderWithI18n(
    <QueryClientProvider client={queryClient}>
      <CurrentRuntimeConfigSection
        agent={agent}
        runtimes={[codexRuntime]}
        members={[]}
        currentUserId="user-1"
      />
    </QueryClientProvider>,
  );
}

function newQueryClient() {
  return new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
}

describe("CurrentRuntimeConfigSection custom-save template model fallback", () => {
  beforeEach(() => {
    mockGetAgentRuntimeCurrentConfig.mockReset();
    mockUpdateAgentRuntimeCurrentConfig.mockReset();
  });

  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
  });

  it("retries the first custom save without the template model fields", async () => {
    mockGetAgentRuntimeCurrentConfig.mockResolvedValue(
      recommendedWithTemplateModel(),
    );
    mockUpdateAgentRuntimeCurrentConfig
      .mockRejectedValueOnce(modelUnavailableError("gpt-6.2-sol"))
      .mockResolvedValue(customSummary(""));

    const { container } = renderSection(newQueryClient());
    const radios = await waitFor(() => {
      const group = container.querySelectorAll('[role="radio"]');
      expect(group).toHaveLength(2);
      return group;
    });

    fireEvent.click(radios[1]!);

    await waitFor(() => {
      expect(mockUpdateAgentRuntimeCurrentConfig).toHaveBeenCalledTimes(2);
    });
    const [firstBody, secondBody] =
      mockUpdateAgentRuntimeCurrentConfig.mock.calls.map(
        (call) => call[1] as Record<string, unknown>,
      );
    expect(firstBody).toMatchObject({
      strategy: "custom",
      model: "gpt-6.2-sol",
    });
    expect(secondBody).toMatchObject({
      strategy: "custom",
      model: "",
      thinking_level: "",
      service_tier: "",
    });
    expect(mockToast.warning.mock.calls[0]?.[0]).toContain("gpt-6.2-sol");
    expect(mockToast.error).not.toHaveBeenCalled();
    expect(mockToast.success).not.toHaveBeenCalled();
  });

  it("keeps a hand-picked model refusal without a silent reset", async () => {
    mockGetAgentRuntimeCurrentConfig.mockResolvedValue(customSummary(""));
    mockUpdateAgentRuntimeCurrentConfig.mockRejectedValue(
      modelUnavailableError("custom-outside-model"),
    );

    const { container, getByTestId } = renderSection(newQueryClient());
    await waitFor(() => {
      expect(getByTestId("pick-outside-model")).toBeTruthy();
    });

    fireEvent.click(getByTestId("pick-outside-model"));

    await waitFor(() => {
      expect(mockToast.error).toHaveBeenCalled();
    });
    expect(mockUpdateAgentRuntimeCurrentConfig).toHaveBeenCalledTimes(1);
    expect(mockUpdateAgentRuntimeCurrentConfig.mock.calls[0]?.[1]).toMatchObject(
      { strategy: "custom", model: "custom-outside-model" },
    );
    expect(mockToast.error.mock.calls[0]?.[0]).toContain(
      "custom-outside-model",
    );
    expect(mockToast.warning).not.toHaveBeenCalled();
    // The refused model must not land in the strategy radios' state: the
    // section stays on its previous custom summary.
    expect(container.querySelector('[role="radio"][aria-checked="true"]'))
      .toHaveProperty("textContent", expect.stringContaining("Custom"));
  });

  it("also rescues non-strategy edits that carry the template model", async () => {
    mockGetAgentRuntimeCurrentConfig.mockResolvedValue(
      customSummary("gpt-6.2-sol"),
    );
    mockUpdateAgentRuntimeCurrentConfig
      .mockRejectedValueOnce(modelUnavailableError("gpt-6.2-sol"))
      .mockResolvedValue(customSummary(""));

    const { getByTestId } = renderSection(newQueryClient());
    await waitFor(() => {
      expect(getByTestId("set-concurrency")).toBeTruthy();
    });

    fireEvent.click(getByTestId("set-concurrency"));

    await waitFor(() => {
      expect(mockUpdateAgentRuntimeCurrentConfig).toHaveBeenCalledTimes(2);
    });
    const [firstBody, secondBody] =
      mockUpdateAgentRuntimeCurrentConfig.mock.calls.map(
        (call) => call[1] as Record<string, unknown>,
      );
    expect(firstBody).toMatchObject({
      strategy: "custom",
      model: "gpt-6.2-sol",
      max_concurrent_tasks: 3,
    });
    // The retry keeps the field the user actually changed and clears only
    // the runtime-native template fields.
    expect(secondBody).toMatchObject({
      model: "",
      max_concurrent_tasks: 3,
    });
    expect(mockToast.warning.mock.calls[0]?.[0]).toContain("gpt-6.2-sol");
    expect(mockToast.error).not.toHaveBeenCalled();
  });

  it("does not fall back for a non-model 409 conflict", async () => {
    mockGetAgentRuntimeCurrentConfig.mockResolvedValue(
      customSummary("gpt-6.2-sol"),
    );
    mockUpdateAgentRuntimeCurrentConfig.mockRejectedValue(
      runtimeOfflineError(),
    );

    const { getByTestId } = renderSection(newQueryClient());
    await waitFor(() => {
      expect(getByTestId("set-concurrency")).toBeTruthy();
    });

    fireEvent.click(getByTestId("set-concurrency"));

    await waitFor(() => {
      expect(mockToast.error).toHaveBeenCalled();
    });
    expect(mockUpdateAgentRuntimeCurrentConfig).toHaveBeenCalledTimes(1);
    expect(mockToast.warning).not.toHaveBeenCalled();
  });

  // Once the model has been cleared the first 409's "model left the catalog"
  // copy is stale: a retry refused for another reason (offline runtime, a
  // dropped network, a 500) must surface its own error, or the user is sent
  // chasing a model problem that no longer exists.
  it.each([
    {
      name: "a resolution conflict",
      retryError: runtimeOfflineError(),
      expectedMessage: "运行时离线",
    },
    {
      name: "a non-conflict failure",
      retryError: Object.assign(new Error("Internal Server Error"), {
        status: 500,
      }),
      expectedMessage: "Internal Server Error",
    },
  ])(
    "reports the retry's own error when the retry fails with $name",
    async ({ retryError, expectedMessage }) => {
      mockGetAgentRuntimeCurrentConfig.mockResolvedValue(
        recommendedWithTemplateModel(),
      );
      mockUpdateAgentRuntimeCurrentConfig
        .mockRejectedValueOnce(modelUnavailableError("gpt-6.2-sol"))
        .mockRejectedValue(retryError);

      const { container } = renderSection(newQueryClient());
      const radios = await waitFor(() => {
        const group = container.querySelectorAll('[role="radio"]');
        expect(group).toHaveLength(2);
        return group;
      });

      fireEvent.click(radios[1]!);

      await waitFor(() => {
        expect(mockToast.error).toHaveBeenCalled();
      });
      expect(mockUpdateAgentRuntimeCurrentConfig).toHaveBeenCalledTimes(2);
      expect(mockToast.error).toHaveBeenCalledTimes(1);
      expect(mockToast.error.mock.calls[0]?.[0]).toBe(expectedMessage);
      expect(mockToast.warning).not.toHaveBeenCalled();
      expect(mockToast.success).not.toHaveBeenCalled();
    },
  );
});
