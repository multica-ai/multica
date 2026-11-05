// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { RuntimeModel, RuntimeModelListRequest } from "@multica/core/types";

const mockInitiateListModels = vi.hoisted(() => vi.fn());
const mockGetListModelsResult = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/api", () => ({
  api: {
    initiateListModels: (...args: unknown[]) => mockInitiateListModels(...args),
    getListModelsResult: (...args: unknown[]) => mockGetListModelsResult(...args),
  },
}));

import { SquadThinkingSelect } from "./squad-thinking-select";

const THINKING_MODEL: RuntimeModel = {
  id: "claude-sonnet-4-6",
  label: "Claude Sonnet 4.6",
  default: true,
  thinking: {
    supported_levels: [
      { value: "low", label: "Low" },
      { value: "high", label: "High" },
    ],
    default_level: "high",
  },
};

const PLAIN_MODEL: RuntimeModel = {
  id: "gemini-2.5-pro",
  label: "Gemini 2.5 Pro",
  default: true,
};

function listResult(models: RuntimeModel[]): RuntimeModelListRequest {
  return {
    id: "req-1",
    runtime_id: "runtime-1",
    status: "completed",
    models,
    supported: true,
    created_at: "2026-05-20T00:00:00Z",
    updated_at: "2026-05-20T00:00:00Z",
  };
}

function renderSelect(props: Partial<React.ComponentProps<typeof SquadThinkingSelect>> = {}) {
  const onSelect = vi.fn();
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={queryClient}>
      <SquadThinkingSelect
        runtimeId="runtime-1"
        runtimeOnline
        provider="claude"
        model="claude-sonnet-4-6"
        value=""
        ariaLabel="Thinking for Amelia"
        defaultLabel="Follow CLI config"
        onSelect={onSelect}
        {...props}
      />
    </QueryClientProvider>,
  );
  return { onSelect };
}

describe("SquadThinkingSelect", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockInitiateListModels.mockResolvedValue(listResult([THINKING_MODEL, PLAIN_MODEL]));
    mockGetListModelsResult.mockResolvedValue(listResult([THINKING_MODEL, PLAIN_MODEL]));
  });

  afterEach(() => {
    cleanup();
  });

  it("offers the selected model's levels and saves the chosen one", async () => {
    const { onSelect } = renderSelect();

    const trigger = await screen.findByRole("button", { name: "Thinking for Amelia" });
    expect(trigger.textContent).toContain("Follow CLI config");
    fireEvent.click(trigger);
    fireEvent.click(await screen.findByText("High"));

    expect(onSelect).toHaveBeenCalledWith("high");
  });

  it("saves an empty level when the CLI default is chosen over a stored level", async () => {
    const { onSelect } = renderSelect({ value: "low" });

    const trigger = await screen.findByRole("button", { name: "Thinking for Amelia" });
    // The raw token shows until the model catalog resolves its label.
    await waitFor(() => expect(trigger.textContent).toContain("Low"));
    fireEvent.click(trigger);
    fireEvent.click(await screen.findByText("Follow CLI config"));

    expect(onSelect).toHaveBeenCalledWith("");
  });

  it("shows a dash and no control when the model advertises no levels", async () => {
    renderSelect({ model: "gemini-2.5-pro" });

    expect(await screen.findByText("—")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Thinking for Amelia" })).toBeNull();
  });
});
