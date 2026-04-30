import { describe, expect, it, vi, beforeEach } from "vitest";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { AgentRuntime } from "@multica/core/types";

// Swap out the runtime picker so tests can drive runtimes / selection
// without a real TanStack Query + WS stack.
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

import { StepPlatformFork } from "./step-platform-fork";

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

function renderFork(
  overrides: Partial<React.ComponentProps<typeof StepPlatformFork>> = {},
) {
  const onNext = vi.fn();
  render(
    <StepPlatformFork
      wsId="ws_test"
      onNext={onNext}
      cliInstructions={<div data-testid="cli-instructions">install me</div>}
      {...overrides}
    />,
  );
  return { onNext };
}

function resetPicker(patch: Partial<typeof mocks.pickerState> = {}) {
  mocks.pickerState.runtimes = patch.runtimes ?? [];
  mocks.pickerState.selected = patch.selected ?? null;
  mocks.pickerState.selectedId = patch.selectedId ?? null;
  mocks.pickerState.hasRuntimes = patch.hasRuntimes ?? false;
  mocks.pickerState.setSelectedId = vi.fn();
}

describe("StepPlatformFork", () => {
  beforeEach(() => {
    resetPicker();
    vi.restoreAllMocks();
  });

  it("renders the two fork options at rest", () => {
    renderFork();
    expect(screen.getByText(/download the desktop app/i)).toBeInTheDocument();
    expect(screen.getByText(/^install the cli$/i)).toBeInTheDocument();
    // Cloud waitlist option was removed in the self-host fork.
    expect(screen.queryByText(/^cloud runtime$/i)).not.toBeInTheDocument();
    // Dialog closed at rest → no CLI instructions surfaced.
    expect(screen.queryByTestId("cli-instructions")).not.toBeInTheDocument();
  });

  it("footer: Skip only + explanatory hint (no Continue)", () => {
    renderFork();
    expect(
      screen.getByRole("button", { name: /skip for now/i }),
    ).toBeEnabled();
    // Continue is gone — it lived in the footer before; now advancement
    // for the CLI path is owned by the CLI dialog's own button.
    expect(
      screen.queryByRole("button", { name: /^continue$/i }),
    ).not.toBeInTheDocument();
    expect(
      screen.getByText(/pick a path above — or skip and configure/i),
    ).toBeInTheDocument();
  });

  it("Skip is always enabled and calls onNext(null)", async () => {
    const user = userEvent.setup();
    const { onNext } = renderFork();
    await user.click(screen.getByRole("button", { name: /skip for now/i }));
    expect(onNext).toHaveBeenCalledTimes(1);
    expect(onNext).toHaveBeenCalledWith(null);
  });

  it("opens the download page and flips the card to a post-click state", async () => {
    const openSpy = vi.spyOn(window, "open").mockReturnValue(null);
    const user = userEvent.setup();
    renderFork();

    await user.click(screen.getByText(/download the desktop app/i));

    // Routes to the new /download page (not GitHub releases) so the
    // user lands on the OS auto-detect surface.
    expect(openSpy).toHaveBeenCalledWith(
      "/download",
      "_blank",
      "noopener,noreferrer",
    );
    expect(
      screen.getByText(/continuing on the download page/i),
    ).toBeInTheDocument();
  });

  it("CLI dialog: opens with instructions + 'waiting' and a disabled Connect button", async () => {
    const user = userEvent.setup();
    renderFork();

    await user.click(screen.getByRole("button", { name: /show steps/i }));

    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByTestId("cli-instructions")).toBeInTheDocument();
    expect(
      within(dialog).getByText(/listening for your daemon/i),
    ).toBeInTheDocument();
    // Connect & continue stays disabled while no runtime is selected.
    expect(
      within(dialog).getByRole("button", { name: /connect & continue/i }),
    ).toBeDisabled();
  });

  it("CLI dialog with a selected runtime: Connect enables and fires onNext(runtime)", async () => {
    const rt = makeRuntime({ id: "rt_claude", name: "Claude Code" });
    resetPicker({
      runtimes: [rt],
      selected: rt,
      selectedId: rt.id,
      hasRuntimes: true,
    });
    const user = userEvent.setup();
    const { onNext } = renderFork();

    await user.click(screen.getByRole("button", { name: /show steps/i }));

    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByText(/1 runtime connected/i)).toBeInTheDocument();
    expect(
      within(dialog).getByText(/selected: claude code/i),
    ).toBeInTheDocument();

    const connect = within(dialog).getByRole("button", {
      name: /connect & continue/i,
    });
    expect(connect).toBeEnabled();
    await user.click(connect);
    expect(onNext).toHaveBeenCalledTimes(1);
    expect(onNext).toHaveBeenCalledWith(rt);
  });

});
