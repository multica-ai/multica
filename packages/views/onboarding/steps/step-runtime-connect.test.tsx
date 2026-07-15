import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { render, screen } from "@testing-library/react";
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

function renderStep() {
  const onNext = vi.fn();
  const onBack = vi.fn();
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
});
