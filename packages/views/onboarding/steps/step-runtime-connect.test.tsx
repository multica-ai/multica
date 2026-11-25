import { api } from "@multica/core/api";
import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { act, render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { AgentRuntime } from "@multica/core/types";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enOnboarding from "../../locales/en/onboarding.json";

import enAgents from "../../locales/en/agents.json";
vi.mock("@multica/core/api", async (importOriginal) => ({
  ...await importOriginal<typeof import("@multica/core/api")>(),
  api: { initiateListModels: vi.fn() },
}));

const TEST_RESOURCES = { en: { common: enCommon, onboarding: enOnboarding, agents: enAgents } };

// Drive the runtime picker via a hoisted mock so the step renders without a
// live daemon. (The onboarding_runtime_detected PostHog event this file used
// to cover was removed in MUL-4127.)
const mocks = vi.hoisted(() => ({
  pickerState: {
    runtimes: [] as AgentRuntime[],
    selected: null as AgentRuntime | null,
    selectedId: null as string | null,
    setSelectedId: vi.fn<(id: string) => void>(),
    hasRuntimes: false,
  },
}));

vi.mock("../components/use-runtime-picker", () => ({
  useRuntimePicker: () => mocks.pickerState,
}));

import { StepRuntimeConnect } from "./step-runtime-connect";

function makeRuntime(overrides: Partial<AgentRuntime> = {}): AgentRuntime {
  return {
    id: "rt_1",
    name: "Claude (dev-box)",
    provider: "claude",
    status: "online",
    ...overrides,
  } as AgentRuntime;
}

function renderStep(props: { runtimesPending?: boolean; wsSlug?: string } = {}) {
  const onNext = vi.fn();
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  render(
    <QueryClientProvider client={qc}>
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <StepRuntimeConnect
          wsId="ws_test"
          wsSlug={props.wsSlug}
          onNext={onNext}
          runtimesPending={props.runtimesPending}
        />
      </I18nProvider>
    </QueryClientProvider>,
  );
  return { onNext };
}

describe("StepRuntimeConnect", () => {
  it("passes the onboarding workspace through the model chooser", async () => {
    const initiate = vi.spyOn(api, "initiateListModels").mockResolvedValue({
      id: "request-1", runtime_id: "rt_1", status: "completed", models: [],
      supported: true, created_at: "2026-10-30T00:00:00Z", updated_at: "2026-10-30T00:00:00Z",
    });
    const rt = makeRuntime();
    mocks.pickerState.runtimes = [rt];
    mocks.pickerState.selected = rt;
    mocks.pickerState.selectedId = rt.id;
    mocks.pickerState.hasRuntimes = true;
    renderStep({ wsSlug: "new-workspace" });
    await act(async () => { await vi.advanceTimersByTimeAsync(0); });
    expect(initiate).toHaveBeenCalledWith(rt.id, undefined, "new-workspace");
    initiate.mockRestore();
  });

  beforeEach(() => {
    mocks.pickerState.runtimes = [];
    mocks.pickerState.selected = null;
    mocks.pickerState.selectedId = null;
    mocks.pickerState.hasRuntimes = false;
    vi.useFakeTimers({ shouldAdvanceTime: true });
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("does not render a permanently-disabled Mika action while scanning", () => {
    renderStep();
    expect(
      screen.queryByRole("button", { name: /start with mika/i }),
    ).not.toBeInTheDocument();
  });

  it("flips to the empty state after the idle timeout when no pending signal is given", () => {
    renderStep();
    act(() => vi.advanceTimersByTime(5000));
    expect(
      screen.getByText(/no agent runtime found on this computer yet/i),
    ).toBeInTheDocument();
  });

  it("keeps scanning past the idle timeout while runtimes are pending, then falls back at the hard ceiling", () => {
    renderStep({ runtimesPending: true });

    // Past the soft budget the daemon still reports work in flight, so the
    // false-negative empty state must not appear (MUL-5119).
    act(() => vi.advanceTimersByTime(5000));
    expect(
      screen.getByText(/connecting this computer/i),
    ).toBeInTheDocument();
    expect(
      screen.queryByText(/no agent runtime found/i),
    ).not.toBeInTheDocument();

    // The absolute ceiling still guarantees a fallback so a wedged probe
    // cannot hang the step on the skeleton forever.
    act(() => vi.advanceTimersByTime(15000));
    expect(
      screen.getByText(/no agent runtime found/i),
    ).toBeInTheDocument();
  });

  it("shows the found list — not the empty state — once runtimes register", () => {
    mocks.pickerState.runtimes = [makeRuntime()];
    mocks.pickerState.selected = makeRuntime();
    mocks.pickerState.selectedId = "rt_1";
    mocks.pickerState.hasRuntimes = true;

    renderStep();
    act(() => vi.advanceTimersByTime(25000));

    // Assert on the runtime count row rather than headline copy: the phase is
    // what this test is about, and keying on the headline made a copy edit
    // look like a behaviour regression.
    expect(screen.getByText(/1 agent runtime/i)).toBeInTheDocument();
    expect(
      screen.queryByText(/no agent runtime found/i),
    ).not.toBeInTheDocument();
    // Starting with Mika is actionable in the found phase.
    expect(
      screen.getByRole("button", { name: /start with mika/i }),
    ).toBeInTheDocument();
  });

  it("shows a single Skip affordance in the empty state (no duplicate footer button)", () => {
    renderStep();
    act(() => vi.advanceTimersByTime(5000));
    // The empty view owns one prominent "Skip for now" card; the footer no
    // longer duplicates it.
    expect(screen.getAllByText("Skip for now")).toHaveLength(1);
  });
});
