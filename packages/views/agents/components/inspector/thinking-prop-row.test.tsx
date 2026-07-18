// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
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
import enCommon from "../../../locales/en/common.json";
import enAgents from "../../../locales/en/agents.json";
import enIssues from "../../../locales/en/issues.json";

const TEST_RESOURCES = {
  en: { common: enCommon, agents: enAgents, issues: enIssues },
};

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

import { ThinkingPropRow } from "./thinking-prop-row";

const CLAUDE_MODEL: RuntimeModel = {
  id: "claude-sonnet-4-6",
  label: "Claude Sonnet 4.6",
  default: true,
  thinking: {
    supported_levels: [
      { value: "none", label: "None" },
      { value: "low", label: "Low" },
      { value: "medium", label: "Medium" },
      { value: "high", label: "High" },
    ],
    default_level: "medium",
  },
};

// Model without thinking metadata — what the row sees when the agent's
// model swap landed on a non-thinking runtime, or when the daemon catalog
// shrank and stopped emitting `thinking` for this id.
const NO_THINKING_MODEL: RuntimeModel = {
  id: "gemini-2.5-pro",
  label: "Gemini 2.5 Pro",
  default: true,
};

// Codex flagship: flagged Default and advertises the widest catalog
// (up to `ultra`). For an empty (follow-config) codex model the row must NOT
// borrow this entry's levels — that's the MUL-4347 fix.
const CODEX_DEFAULT_MODEL: RuntimeModel = {
  id: "gpt-5.6-sol",
  label: "GPT-5.6 Sol",
  default: true,
  thinking: {
    supported_levels: [
      { value: "low", label: "Low" },
      { value: "medium", label: "Medium" },
      { value: "high", label: "High" },
      { value: "max", label: "Max" },
      { value: "ultra", label: "Ultra" },
    ],
    default_level: "high",
  },
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

function renderRow(
  props: Partial<React.ComponentProps<typeof ThinkingPropRow>> = {},
) {
  const onChange = vi.fn();
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const utils = render(
    // PropRow uses CSS subgrid, so wrap with the same column tracks the
    // inspector parent declares — otherwise the row mounts without a
    // grid context and the column layout warns. Behaviour we care about
    // (visibility + clear flow) is independent of layout.
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <QueryClientProvider client={queryClient}>
        <div className="grid grid-cols-[auto_1fr] gap-x-2 gap-y-0.5">
          <ThinkingPropRow
            runtimeId="runtime-1"
            runtimeOnline
            provider="claude"
            model="claude-sonnet-4-6"
            value=""
            canEdit
            onChange={onChange}
            {...props}
          />
        </div>
      </QueryClientProvider>
    </I18nProvider>,
  );
  return { ...utils, onChange, queryClient };
}

describe("ThinkingPropRow", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockInitiateListModels.mockResolvedValue(listResult([CLAUDE_MODEL]));
    mockGetListModelsResult.mockResolvedValue(listResult([CLAUDE_MODEL]));
  });

  afterEach(() => {
    cleanup();
  });

  it("hides the row when the active model has no thinking levels and nothing is persisted", async () => {
    mockInitiateListModels.mockResolvedValue(listResult([NO_THINKING_MODEL]));
    renderRow({ model: "gemini-2.5-pro", value: "" });

    // ThinkingPropRow returns null when levels are empty and value is
    // empty — both initially (data undefined) and after discovery
    // (NO_THINKING_MODEL has no `thinking` block). The `useQuery` hook
    // runs before the early null return on first render, so the
    // subscription is established and discovery still fires. In
    // production this is also covered by the sibling ModelPicker
    // mounted next to the row in agent-detail-inspector.
    await waitFor(() => {
      expect(mockInitiateListModels).toHaveBeenCalled();
    });
    await waitFor(() => {
      expect(screen.queryByText("Thinking")).toBeNull();
    });
  });

  it("hides the row while the runtime is offline (no query fires)", () => {
    renderRow({ runtimeOnline: false, value: "" });

    // Query disabled when runtimeOnline=false, so no models, levels stay
    // empty, value is empty → row stays hidden.
    expect(screen.queryByText("Thinking")).toBeNull();
    expect(mockInitiateListModels).not.toHaveBeenCalled();
  });

  it("renders the row with the persisted raw token when levels are empty but value is set (stale orphan)", async () => {
    // The agent persisted `thinking_level=xhigh` while it was on a
    // thinking-capable model, then was swapped to gemini (or the CLI
    // catalog shrank). PR1's behavior is daemon-side warn/drop, not a
    // synchronous DB clear, so the frontend must surface the orphan
    // token and let the user clear it explicitly.
    mockInitiateListModels.mockResolvedValue(listResult([NO_THINKING_MODEL]));
    renderRow({ model: "gemini-2.5-pro", value: "xhigh" });

    await screen.findByText("Thinking");
    // The picker chip carries the raw value when it's not in the catalog.
    expect(await screen.findByText("xhigh")).toBeInTheDocument();
  });

  it("clears the orphan value via the picker footer, emitting onChange(\"\")", async () => {
    mockInitiateListModels.mockResolvedValue(listResult([NO_THINKING_MODEL]));
    const { onChange } = renderRow({
      model: "gemini-2.5-pro",
      value: "xhigh",
    });

    // Wait until the row mounts with the orphan value, then open the
    // popover and fire the clear footer. The footer is the only target
    // matching the i18n `thinking_clear_title` copy.
    await screen.findByText("xhigh");
    fireEvent.click(screen.getByRole("button"));
    const clearButton = await screen.findByTitle(/Clear the override/i);
    fireEvent.click(clearButton);

    expect(onChange).toHaveBeenCalledWith("");
  });

  it("renders the row with the matched label when the model still advertises the value", async () => {
    renderRow({ value: "high" });

    await screen.findByText("Thinking");
    // Both the chip and the tooltip carry "High".
    expect((await screen.findAllByText("High")).length).toBeGreaterThan(0);
  });

  it("renders the row with \"Follow CLI config\" when value is empty and the model exposes levels", async () => {
    renderRow({ value: "" });

    await screen.findByText("Thinking");
    // Empty value means Multica omits --effort, so the local CLI's
    // config decides — chip + tooltip both read "Follow CLI config".
    expect((await screen.findAllByText("Follow CLI config")).length).toBeGreaterThan(0);
  });

  it("hides the picker for an empty codex model — it must not borrow the Default's catalog (MUL-4347)", async () => {
    // Empty model on codex follows config.toml, which can resolve to any
    // installed model. Previewing gpt-5.6-sol's levels (the flagged Default,
    // the only one with `ultra`) would offer a level the real model may not
    // support. With no persisted value the row hides entirely.
    mockInitiateListModels.mockResolvedValue(
      listResult([CODEX_DEFAULT_MODEL]),
    );
    mockGetListModelsResult.mockResolvedValue(
      listResult([CODEX_DEFAULT_MODEL]),
    );
    renderRow({ provider: "codex", model: "", value: "" });

    await waitFor(() => {
      expect(mockInitiateListModels).toHaveBeenCalled();
    });
    await waitFor(() => {
      expect(screen.queryByText("Thinking")).toBeNull();
    });
    // Crucially, `Ultra` from the Default entry is never offered.
    expect(screen.queryByText("Ultra")).toBeNull();
  });

  it("still surfaces a persisted level on an empty codex model so it can be cleared", async () => {
    // The clear-stale path must survive: an already-persisted `ultra` (set
    // before the model was cleared) renders the row so the user can drop it,
    // even though the picker offers no per-model levels.
    mockInitiateListModels.mockResolvedValue(
      listResult([CODEX_DEFAULT_MODEL]),
    );
    mockGetListModelsResult.mockResolvedValue(
      listResult([CODEX_DEFAULT_MODEL]),
    );
    const { onChange } = renderRow({
      provider: "codex",
      model: "",
      value: "ultra",
    });

    await screen.findByText("Thinking");
    expect(await screen.findByText("ultra")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button"));
    const clearButton = await screen.findByTitle(/Clear the override/i);
    fireEvent.click(clearButton);
    expect(onChange).toHaveBeenCalledWith("");
  });

  it("still previews the Default model's levels for an empty non-codex model", async () => {
    // Non-codex providers keep the existing behavior: an empty model previews
    // the flagged Default entry's catalog. Only codex is fenced off, because
    // only its empty-model resolution is config-driven and unknowable here.
    renderRow({ provider: "claude", model: "", value: "" });

    await screen.findByText("Thinking");
    // CLAUDE_MODEL (Default) advertises Low/Medium/High — the picker shows them.
    expect((await screen.findAllByText("Follow CLI config")).length).toBeGreaterThan(0);
  });
});
