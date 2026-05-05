import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { act, render, screen } from "@testing-library/react";
import type { AgentRuntime } from "@multica/core/types";

// Hoisted mocks — replace analytics and the runtime picker before the SUT
// imports them. Tests drive picker state via `mocks.pickerState`; every
// captureEvent / setPersonProperties call lands on `mocks.captureEvent` /
// `mocks.setPersonProperties` so we can assert the payload shape.
const mocks = vi.hoisted(() => ({
  captureEvent: vi.fn<(name: string, props?: Record<string, unknown>) => void>(),
  setPersonProperties: vi.fn<(props: Record<string, unknown>) => void>(),
  pickerState: {
    runtimes: [] as AgentRuntime[],
    selected: null as AgentRuntime | null,
    selectedId: null as string | null,
    setSelectedId: vi.fn<(id: string) => void>(),
    hasRuntimes: false,
  },
}));

vi.mock("@multica/core/analytics", () => ({
  captureEvent: mocks.captureEvent,
  setPersonProperties: mocks.setPersonProperties,
}));

vi.mock("../components/use-runtime-picker", () => ({
  useRuntimePicker: () => mocks.pickerState,
}));

vi.mock("@multica/i18n/react", async () => {
  const { en } = await import("@multica/i18n/dict/en");
  return {
    useT: (ns?: string) => (key: string, params?: Record<string, string | number>) => {
      const template = ns ? en[ns]?.[key] ?? key : key;
      if (!params) return template;
      return template.replace(/\{(\w+)\}/g, (_, k: string) => String(params[k] ?? `{${k}}`));
    },
  };
});

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

function setPicker(patch: Partial<typeof mocks.pickerState> = {}) {
  mocks.pickerState.runtimes = patch.runtimes ?? [];
  mocks.pickerState.selected = patch.selected ?? null;
  mocks.pickerState.selectedId = patch.selectedId ?? null;
  mocks.pickerState.hasRuntimes = patch.hasRuntimes ?? false;
  mocks.pickerState.setSelectedId = vi.fn();
}

function renderStep() {
  const onNext = vi.fn();
  const onBack = vi.fn();
  render(
    <StepRuntimeConnect wsId="ws_test" onNext={onNext} onBack={onBack} />,
  );
  return { onNext, onBack };
}

describe("StepRuntimeConnect — onboarding_runtime_detected", () => {
  beforeEach(() => {
    mocks.captureEvent.mockReset();
    mocks.setPersonProperties.mockReset();
    setPicker();
    vi.useFakeTimers({ shouldAdvanceTime: true });
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("fires `outcome: found` when runtimes arrive synchronously on mount", () => {
    const rt = makeRuntime({
      id: "rt_claude",
      provider: "claude",
      status: "online",
    });
    setPicker({ runtimes: [rt], selected: rt, selectedId: rt.id, hasRuntimes: true });

    renderStep();

    expect(mocks.captureEvent).toHaveBeenCalledTimes(1);
    const [name, props] = mocks.captureEvent.mock.calls[0]!;
    expect(name).toBe("onboarding_runtime_detected");
    expect(props).toMatchObject({
      source: "step3_desktop",
      outcome: "found",
      runtime_count: 1,
      online_count: 1,
      providers: ["claude"],
      has_claude: true,
      has_codex: false,
      has_cursor: false,
    });
    expect(typeof (props as Record<string, unknown>).detect_ms).toBe("number");

    expect(mocks.setPersonProperties).toHaveBeenCalledWith({
      has_any_cli: true,
      detected_cli_count: 1,
    });
  });

  it("derives has_claude / has_codex / has_cursor from distinct providers", () => {
    setPicker({
      runtimes: [
        makeRuntime({ id: "rt1", provider: "claude" }),
        makeRuntime({ id: "rt2", provider: "codex", status: "offline" }),
        makeRuntime({ id: "rt3", provider: "cursor" }),
      ],
      hasRuntimes: true,
    });

    renderStep();

    expect(mocks.captureEvent).toHaveBeenCalledTimes(1);
    const props = mocks.captureEvent.mock.calls[0]![1] as Record<string, unknown>;
    expect(props.runtime_count).toBe(3);
    expect(props.online_count).toBe(2);
    expect(props.providers).toEqual(["claude", "codex", "cursor"]);
    expect(props.has_claude).toBe(true);
    expect(props.has_codex).toBe(true);
    expect(props.has_cursor).toBe(true);
  });

  it("fires `outcome: empty` after the 5s scanning timeout when no runtimes arrive", () => {
    setPicker({ runtimes: [] });

    renderStep();

    // Scanning phase: no event yet.
    expect(mocks.captureEvent).not.toHaveBeenCalled();

    // Advance past the 5s empty-timeout inside act so the state flip
    // flushes React updates before we assert.
    act(() => {
      vi.advanceTimersByTime(5_001);
    });

    expect(mocks.captureEvent).toHaveBeenCalledTimes(1);
    const props = mocks.captureEvent.mock.calls[0]![1] as Record<string, unknown>;
    expect(props).toMatchObject({
      source: "step3_desktop",
      outcome: "empty",
      runtime_count: 0,
      online_count: 0,
      providers: [],
      has_claude: false,
      has_codex: false,
      has_cursor: false,
    });

    expect(mocks.setPersonProperties).toHaveBeenCalledWith({
      has_any_cli: false,
      detected_cli_count: 0,
    });
  });

  it("does not re-emit if the component re-renders after resolution", () => {
    const rt = makeRuntime({ id: "rt_claude", provider: "claude" });
    setPicker({ runtimes: [rt], selected: rt, selectedId: rt.id, hasRuntimes: true });

    const { onNext } = renderStep();
    expect(mocks.captureEvent).toHaveBeenCalledTimes(1);

    // Simulate a runtime coming online / a second runtime registering:
    // the event has already resolved once; it must not re-emit.
    setPicker({
      runtimes: [rt, makeRuntime({ id: "rt_codex", provider: "codex" })],
      selected: rt,
      selectedId: rt.id,
      hasRuntimes: true,
    });
    // Force a re-render by firing a timer tick — React will re-read the
    // mocked picker state but the ref latch keeps the event unique.
    act(() => {
      vi.advanceTimersByTime(1_000);
    });

    expect(mocks.captureEvent).toHaveBeenCalledTimes(1);
    expect(onNext).not.toHaveBeenCalled();
  });

  it("only counts distinct providers (multiple runtimes of the same provider)", () => {
    setPicker({
      runtimes: [
        makeRuntime({ id: "rt1", provider: "claude" }),
        makeRuntime({ id: "rt2", provider: "claude", status: "offline" }),
      ],
      hasRuntimes: true,
    });

    renderStep();

    const props = mocks.captureEvent.mock.calls[0]![1] as Record<string, unknown>;
    expect(props.runtime_count).toBe(2);
    expect(props.online_count).toBe(1);
    expect(props.providers).toEqual(["claude"]);
  });

  it("mounts without touching framework-level globals", () => {
    // Sanity: the StepHeader renders and the DragStrip doesn't explode
    // under jsdom. Keeps the test file honest if someone refactors the
    // shell around the effect.
    setPicker({ runtimes: [] });
    renderStep();
    expect(screen.getByText(/Looking for your tools/i)).toBeInTheDocument();
  });
});
