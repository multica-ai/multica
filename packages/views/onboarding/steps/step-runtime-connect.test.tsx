import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { act, render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { AgentRuntime } from "@multica/core/types";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enOnboarding from "../../locales/en/onboarding.json";

const TEST_RESOURCES = { en: { common: enCommon, onboarding: enOnboarding } };

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

function renderStep(props: { runtimesPending?: boolean } = {}) {
  const onNext = vi.fn();
  const onBack = vi.fn();
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  render(
    <QueryClientProvider client={qc}>
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <StepRuntimeConnect
          wsId="ws_test"
          onNext={onNext}
          onBack={onBack}
          runtimesPending={props.runtimesPending}
        />
      </I18nProvider>
    </QueryClientProvider>,
  );
  return { onNext, onBack };
}

describe("StepRuntimeConnect", () => {
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

  it("mounts and shows the scanning UI without touching framework-level globals", () => {
    renderStep();
    expect(
      screen.getByText(/connecting this computer/i),
    ).toBeInTheDocument();
  });

  it("does not render a permanently-disabled Start exploring while scanning", () => {
    renderStep();
    expect(
      screen.queryByRole("button", { name: /start exploring/i }),
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

    expect(
      screen.getByText(/this computer is connected/i),
    ).toBeInTheDocument();
    expect(
      screen.queryByText(/no agent runtime found/i),
    ).not.toBeInTheDocument();
    // Continue is actionable in the found phase.
    expect(
      screen.getByRole("button", { name: /start exploring/i }),
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
