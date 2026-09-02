// @vitest-environment jsdom

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import type {
  RuntimeDevice,
  RuntimeModel,
  RuntimeModelListRequest,
} from "@multica/core/types";
import { I18nProvider } from "@multica/core/i18n/react";
import enAgents from "../../locales/en/agents.json";
import enCommon from "../../locales/en/common.json";
import enIssues from "../../locales/en/issues.json";

const mockInitiateListModels = vi.hoisted(() => vi.fn());
const mockGetListModelsResult = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/api", () => ({
  api: {
    initiateListModels: (...args: unknown[]) => mockInitiateListModels(...args),
    getListModelsResult: (...args: unknown[]) =>
      mockGetListModelsResult(...args),
  },
  ApiError: class ApiError extends Error {},
}));

import type { AgentDraft } from "@multica/core/agents";
import { AgentExecutionOverrides } from "./agent-configuration-panel";

const FAST_MODEL: RuntimeModel = {
  id: "gpt-5.6-sol",
  label: "GPT-5.6 Sol",
  thinking: {
    supported_levels: [
      { value: "medium", label: "Medium" },
      { value: "high", label: "High" },
    ],
  },
  service_tiers: [{ id: "priority", name: "Fast", description: "1.5x speed" }],
};

const PLAIN_MODEL: RuntimeModel = { id: "gpt-5.4-mini", label: "GPT-5.4 mini" };

const ONLINE_RUNTIME = {
  id: "runtime-1",
  name: "Codex laptop",
  provider: "codex",
  status: "online",
} as RuntimeDevice;

function listResult(models: RuntimeModel[]): RuntimeModelListRequest {
  return {
    id: "request-1",
    runtime_id: "runtime-1",
    status: "completed",
    models,
    supported: true,
    created_at: "2026-07-28T00:00:00Z",
    updated_at: "2026-07-28T00:00:00Z",
  };
}

const baseDraft: AgentDraft = {
  name: "Fast Codex",
  description: "",
  instructions: "",
  conversationStarters: [],
  avatarUrl: null,
  runtimeId: "runtime-1",
  model: "gpt-5.6-sol",
  thinkingLevel: "",
  serviceTier: "",
  skillIds: new Set(),
  permissionScope: "private",
  memberIds: new Set(),
  teamIds: new Set(),
};

function renderOverrides(
  props: Partial<React.ComponentProps<typeof AgentExecutionOverrides>> = {},
) {
  const onChange = vi.fn();
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  render(
    <I18nProvider
      locale="en"
      resources={{
        en: { common: enCommon, agents: enAgents, issues: enIssues },
      }}
    >
      <QueryClientProvider client={queryClient}>
        <AgentExecutionOverrides
          draft={baseDraft}
          runtime={ONLINE_RUNTIME}
          onChange={onChange}
          {...props}
        />
      </QueryClientProvider>
    </I18nProvider>,
  );
  return { onChange };
}

// MUL-5390: the create flow never offered these two, so a Fast Codex agent had
// to be created first and fixed afterwards in settings.
describe("AgentExecutionOverrides", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockInitiateListModels.mockResolvedValue(listResult([FAST_MODEL]));
    mockGetListModelsResult.mockResolvedValue(listResult([FAST_MODEL]));
  });

  afterEach(cleanup);

  it("offers Speed for a model whose catalog advertises a tier", async () => {
    const { onChange } = renderOverrides();

    await screen.findByText("Speed");
    fireEvent.click(screen.getByRole("button", { name: /speed/i }));
    fireEvent.click(await screen.findByText("Fast"));

    expect(onChange).toHaveBeenCalledWith(
      expect.objectContaining({ serviceTier: "priority" }),
    );
  });

  it("offers the exact model's thinking levels", async () => {
    const { onChange } = renderOverrides();

    await screen.findByText("Thinking");
    fireEvent.click(screen.getByRole("button", { name: /thinking/i }));
    fireEvent.click(await screen.findByText("High"));

    expect(onChange).toHaveBeenCalledWith(
      expect.objectContaining({ thinkingLevel: "high" }),
    );
  });

  it("hides both fields when the model advertises neither capability", async () => {
    mockInitiateListModels.mockResolvedValue(listResult([PLAIN_MODEL]));
    mockGetListModelsResult.mockResolvedValue(listResult([PLAIN_MODEL]));
    renderOverrides({ draft: { ...baseDraft, model: "gpt-5.4-mini" } });

    await waitFor(() => expect(mockInitiateListModels).toHaveBeenCalled());
    expect(screen.queryByText("Speed")).toBeNull();
    expect(screen.queryByText("Thinking")).toBeNull();
  });

  it("stays hidden and asks the daemon nothing while the runtime is offline", async () => {
    renderOverrides({
      runtime: { ...ONLINE_RUNTIME, status: "offline" } as RuntimeDevice,
    });

    await waitFor(() => {
      expect(screen.queryByText("Speed")).toBeNull();
    });
    expect(mockInitiateListModels).not.toHaveBeenCalled();
  });

  it("stays hidden when model discovery fails", async () => {
    mockInitiateListModels.mockRejectedValue(new Error("discovery failed"));
    renderOverrides();

    await waitFor(() => expect(mockInitiateListModels).toHaveBeenCalled());
    expect(screen.queryByText("Speed")).toBeNull();
    expect(screen.queryByText("Thinking")).toBeNull();
  });

  it("fails closed for an empty model that follows the runtime default", async () => {
    renderOverrides({ draft: { ...baseDraft, model: "" } });

    await waitFor(() => expect(mockInitiateListModels).toHaveBeenCalled());
    expect(screen.queryByText("Speed")).toBeNull();
    expect(screen.queryByText("Thinking")).toBeNull();
  });
});
