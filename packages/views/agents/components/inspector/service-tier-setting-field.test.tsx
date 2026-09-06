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
  RuntimeModel,
  RuntimeModelListRequest,
} from "@multica/core/types";
import { I18nProvider } from "@multica/core/i18n/react";
import enAgents from "../../../locales/en/agents.json";
import enCommon from "../../../locales/en/common.json";
import enIssues from "../../../locales/en/issues.json";

const mockInitiateListModels = vi.hoisted(() => vi.fn());
const mockGetListModelsResult = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/api", () => ({
  api: {
    initiateListModels: (...args: unknown[]) =>
      mockInitiateListModels(...args),
    getListModelsResult: (...args: unknown[]) =>
      mockGetListModelsResult(...args),
  },
}));

import { ServiceTierSettingField } from "./service-tier-setting-field";

const FAST_MODEL: RuntimeModel = {
  id: "gpt-5.6-sol",
  label: "GPT-5.6 Sol",
  supports_explicit_standard_service_tier: true,
  service_tiers: [
    {
      id: "priority",
      name: "Fast",
      description: "1.5x speed, increased usage",
    },
  ],
};

function listResult(models: RuntimeModel[]): RuntimeModelListRequest {
  return {
    id: "request-1",
    runtime_id: "runtime-1",
    status: "completed",
    models,
    supported: true,
    created_at: "2026-07-23T00:00:00Z",
    updated_at: "2026-07-23T00:00:00Z",
  };
}

function renderField(
  props: Partial<React.ComponentProps<typeof ServiceTierSettingField>> = {},
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
        <ServiceTierSettingField
          label="Speed"
          runtimeId="runtime-1"
          runtimeOnline
          provider="codex"
          model="gpt-5.6-sol"
          value=""
          canEdit
          onChange={onChange}
          {...props}
        />
      </QueryClientProvider>
    </I18nProvider>,
  );
  return { onChange };
}

describe("ServiceTierSettingField", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockInitiateListModels.mockResolvedValue(listResult([FAST_MODEL]));
    mockGetListModelsResult.mockResolvedValue(listResult([FAST_MODEL]));
  });

  afterEach(cleanup);

  it("renders catalog-owned Fast copy and persists its runtime id", async () => {
    const { onChange } = renderField();

    await screen.findByText("Speed");
    fireEvent.click(screen.getByRole("button"));
    expect(await screen.findByText("1.5x speed, increased usage")).toBeInTheDocument();
    fireEvent.click(screen.getByText("Fast"));

    expect(onChange).toHaveBeenCalledWith("priority");
  });

  it("offers explicit standard speed separately from the runtime default", async () => {
    const { onChange } = renderField();

    await screen.findByText("Speed");
    fireEvent.click(screen.getByRole("button"));
    expect(
      await screen.findByText("Standard speed and standard usage"),
    ).toBeInTheDocument();
    fireEvent.click(screen.getByText("Standard"));

    expect(onChange).toHaveBeenCalledWith("default");
  });

  it("does not offer explicit standard when an older daemon omits the capability", async () => {
    const {
      supports_explicit_standard_service_tier: _omitted,
      ...oldDaemonModel
    } = FAST_MODEL;
    mockInitiateListModels.mockResolvedValue(listResult([oldDaemonModel]));
    mockGetListModelsResult.mockResolvedValue(listResult([oldDaemonModel]));
    renderField();

    await screen.findByText("Speed");
    fireEvent.click(screen.getByRole("button"));
    expect(await screen.findByText("Fast")).toBeInTheDocument();
    expect(screen.queryByText("Standard")).toBeNull();
  });

  it("offers explicit standard even when no alternative tier is advertised", async () => {
    const standardOnly: RuntimeModel = {
      id: "gpt-5.4-mini",
      label: "GPT-5.4 mini",
      supports_explicit_standard_service_tier: true,
    };
    mockInitiateListModels.mockResolvedValue(listResult([standardOnly]));
    mockGetListModelsResult.mockResolvedValue(listResult([standardOnly]));
    renderField({ model: standardOnly.id });

    await screen.findByText("Speed");
    fireEvent.click(screen.getByRole("button"));
    expect(await screen.findByText("Standard")).toBeInTheDocument();
  });

  it("offers CLI-level explicit standard without a pinned model", async () => {
    mockInitiateListModels.mockResolvedValue(listResult([FAST_MODEL]));
    mockGetListModelsResult.mockResolvedValue(listResult([FAST_MODEL]));
    renderField({ model: "" });

    await screen.findByText("Speed");
    fireEvent.click(screen.getByRole("button"));
    expect(await screen.findByText("Standard")).toBeInTheDocument();
    expect(screen.queryByText("Fast")).toBeNull();
  });

  it("hides when the model has no tiers and no value is persisted", async () => {
    mockInitiateListModels.mockResolvedValue(
      listResult([{ id: "gpt-5.4-mini", label: "GPT-5.4 mini" }]),
    );
    renderField({ model: "gpt-5.4-mini" });

    await waitFor(() => expect(mockInitiateListModels).toHaveBeenCalled());
    expect(screen.queryByText("Speed")).toBeNull();
  });

  it("fails closed for an unresolved config.toml model", async () => {
    renderField({ model: "" });

    await waitFor(() => expect(mockInitiateListModels).toHaveBeenCalled());
    expect(screen.queryByText("Speed")).toBeNull();
  });

  it("keeps a stale value visible and lets the user clear it", async () => {
    const { onChange } = renderField({
      model: "gpt-5.4-mini",
      value: "priority",
    });

    await screen.findByText("Speed");
    expect(await screen.findByText("priority")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button"));
    fireEvent.click(await screen.findByTitle(/Clear the speed override/i));
    expect(onChange).toHaveBeenCalledWith("");
  });
});
