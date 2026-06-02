import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { AgentRuntime } from "@wallts/core/types";
import { I18nProvider } from "@wallts/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enOnboarding from "../../locales/en/onboarding.json";

const TEST_RESOURCES = { en: { common: enCommon, onboarding: enOnboarding } };

// Hoisted mocks — replace the runtime picker before the SUT
// imports it. Tests drive picker state via `mocks.pickerState`.
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
    id: "rt_test",
    workspace_id: "ws_test",
    name: "Claude Code",
    provider: "claude",
    status: "online",
    runtime_mode: "local",
    runtime_config: {},
    device_info: "",
    metadata: {},
    daemon_id: null,
    last_seen_at: new Date().toISOString(),
    created_at: new Date().toISOString(),
    updated_at: new Date().toISOString(),
    ...overrides,
  } as unknown as AgentRuntime;
}

function setPicker(
  overrides: Partial<typeof mocks.pickerState> = {},
) {
  mocks.pickerState = {
    runtimes: [],
    selected: null,
    selectedId: null,
    setSelectedId: mocks.pickerState.setSelectedId,
    hasRuntimes: false,
    ...overrides,
  };
}

const onNext = vi.fn();
const onBack = vi.fn();

function renderStep() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  render(
    <QueryClientProvider client={qc}>
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <StepRuntimeConnect wsId="ws_test" onNext={onNext} onBack={onBack} />
      </I18nProvider>
    </QueryClientProvider>,
  );
  return { onNext, onBack };
}

describe("StepRuntimeConnect", () => {
  beforeEach(() => {
    setPicker();
    vi.useFakeTimers({ shouldAdvanceTime: true });
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("mounts without touching framework-level globals", () => {
    // Sanity: the StepHeader renders and the DragStrip doesn't explode
    // under jsdom. Keeps the test file honest if someone refactors the
    // shell around the effect.
    setPicker({ runtimes: [] });
    renderStep();
    expect(
      screen.getByText(/connecting this computer/i),
    ).toBeInTheDocument();
  });
});
