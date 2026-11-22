import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import type {
  CodingToolProbe,
  CodingToolRunEvent,
  CodingToolsBridge,
} from "@multica/core/onboarding";
import type { AgentRuntime } from "@multica/core/types";
import enCommon from "../../locales/en/common.json";
import enOnboarding from "../../locales/en/onboarding.json";

const TEST_RESOURCES = { en: { common: enCommon, onboarding: enOnboarding } };

const mocks = vi.hoisted(() => ({
  runtimes: [] as AgentRuntime[],
  agents: [] as Array<{ name: string }>,
  toast: Object.assign(vi.fn(), { success: vi.fn(), error: vi.fn() }),
}));

vi.mock("sonner", () => ({ toast: mocks.toast }));
vi.mock("../components/use-runtime-picker", () => ({
  useRuntimePicker: () => ({ runtimes: mocks.runtimes }),
}));
vi.mock("@multica/core/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/api")>()),
  api: { listAgents: vi.fn(async () => mocks.agents) },
}));

import { StepAgentSetup } from "./step-agent-setup";

function runtime(provider: string, status: "online" | "offline" = "online"): AgentRuntime {
  return {
    id: `rt-${provider}`,
    name: `${provider} (laptop)`,
    provider,
    status,
    owner_id: "u-1",
  } as unknown as AgentRuntime;
}

function fakeBridge(probes: CodingToolProbe[]) {
  let listener: ((event: CodingToolRunEvent) => void) | null = null;
  const bridge = {
    probe: vi.fn(async () => probes),
    start: vi.fn(async () => ({ runId: "run-1" })),
    submitCode: vi.fn(async () => true),
    cancel: vi.fn(async () => undefined),
    onEvent: vi.fn((callback: (event: CodingToolRunEvent) => void) => {
      listener = callback;
      return () => {
        listener = null;
      };
    }),
  } satisfies CodingToolsBridge;
  const emit = (event: Partial<CodingToolRunEvent>) =>
    act(() => {
      listener?.({
        runId: "run-1",
        toolId: "claude",
        kind: "install",
        phase: "download",
        status: "running",
        ...event,
      });
    });
  return { bridge, emit, probes };
}

const MISSING: CodingToolProbe[] = [
  { id: "claude", installed: false, version: null, loggedIn: null },
  { id: "codex", installed: false, version: null, loggedIn: null },
  { id: "cursor", installed: false, version: null, loggedIn: null },
];

function renderStep(props: Partial<React.ComponentProps<typeof StepAgentSetup>> = {}) {
  const onCreate = vi.fn(async () => undefined);
  const onSkip = vi.fn(async () => undefined);
  const onRuntimeRefresh = vi.fn(async () => undefined);
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={qc}>
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <StepAgentSetup
          wsId="ws-1"
          wsSlug="acme"
          userName="Ada"
          onCreate={onCreate}
          onSkip={onSkip}
          onRuntimeRefresh={onRuntimeRefresh}
          {...props}
        />
      </I18nProvider>
    </QueryClientProvider>,
  );
  return { onCreate, onSkip, onRuntimeRefresh };
}

beforeEach(() => {
  mocks.runtimes = [];
  mocks.agents = [];
  mocks.toast.mockReset();
  mocks.toast.success.mockReset();
  mocks.toast.error.mockReset();
});

afterEach(() => cleanup());

describe("StepAgentSetup on Desktop", () => {
  it("lists ready, signed-out and missing tools, and creates the agent on the ready one", async () => {
    mocks.runtimes = [runtime("claude"), runtime("codex")];
    mocks.agents = [{ name: "Ada's assistant" }];
    const { bridge } = fakeBridge([
      { id: "claude", installed: true, version: "2.1.3", loggedIn: true },
      { id: "codex", installed: true, version: "0.170.0", loggedIn: false },
      { id: "cursor", installed: false, version: null, loggedIn: null },
    ]);
    const { onCreate } = renderStep({ codingTools: bridge });

    expect(await screen.findByRole("heading", { name: "Pick an AI coding tool" })).toBeInTheDocument();
    const claude = screen.getByTestId("coding-tool-claude");
    expect(claude).toHaveAttribute("data-state", "ready");
    expect(claude).toHaveAttribute("aria-checked", "true");
    expect(within(screen.getByTestId("coding-tool-codex")).getByRole("button", { name: /sign in/i })).toBeInTheDocument();
    expect(within(screen.getByTestId("coding-tool-cursor")).getByRole("button", { name: /install/i })).toBeInTheDocument();

    // The default name is taken in this workspace, so it gets a number.
    const nameInput = screen.getByRole("textbox", { name: "Agent name" });
    await waitFor(() => expect(nameInput).toHaveValue("Ada's assistant 2"));
    expect(screen.getByText("Uses Claude Code")).toBeInTheDocument();
    expect(screen.getByText("Only you can use it")).toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: "Create agent and start chatting" }));
    expect(onCreate).toHaveBeenCalledWith({
      runtime: mocks.runtimes[0],
      name: "Ada's assistant 2",
      description: "Ada's personal agent. Only Ada can use it.",
    });
  });

  it("installs a tool in place, hands over the sign-in code, and restarts the daemon when it is done", async () => {
    const { bridge, emit, probes } = fakeBridge([...MISSING]);
    const { onRuntimeRefresh, onSkip } = renderStep({ codingTools: bridge });

    expect(await screen.findByRole("heading", { name: "No AI coding tool on this computer yet" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Create agent and start chatting" })).toBeDisabled();
    expect(screen.getByText(/we'll add a setup guide/i)).toBeInTheDocument();

    await userEvent.click(within(screen.getByTestId("coding-tool-claude")).getByRole("button", { name: /install/i }));
    expect(bridge.start).toHaveBeenCalledWith("claude", "install");
    expect(screen.getByRole("heading", { name: "Installing Claude Code" })).toBeInTheDocument();

    emit({ phase: "download", progress: 0.5 });
    emit({ phase: "login", loginUrl: "https://claude.com/login", needsCode: true });
    const row = screen.getByTestId("coding-tool-claude");
    expect(within(row).getByText("Downloaded the official installer")).toBeInTheDocument();
    expect(within(row).getByText(/opened the Anthropic sign-in page/i)).toBeInTheDocument();
    await userEvent.type(within(row).getByPlaceholderText("Paste the code"), "abc#123");
    await userEvent.click(within(row).getByRole("button", { name: "Continue" }));
    expect(bridge.submitCode).toHaveBeenCalledWith("run-1", "abc#123");

    probes[0] = { id: "claude", installed: true, version: "2.1.3", loggedIn: true };
    mocks.runtimes = [runtime("claude")];
    emit({ phase: "verify", status: "done", finished: true });

    await waitFor(() => expect(onRuntimeRefresh).toHaveBeenCalled());
    expect(mocks.toast.success).toHaveBeenCalledWith("Claude Code is ready");
    expect(onSkip).not.toHaveBeenCalled();
  });

  it("offers retry, another tool and the terminal when an install fails", async () => {
    const { bridge, emit } = fakeBridge([...MISSING]);
    renderStep({ codingTools: bridge });
    await screen.findByRole("heading", { name: "No AI coding tool on this computer yet" });

    await userEvent.click(within(screen.getByTestId("coding-tool-codex")).getByRole("button", { name: /install/i }));
    emit({
      toolId: "codex",
      phase: "download",
      status: "failed",
      finished: true,
      error: { code: "network", message: "HTTP 503" },
      logTail: "curl: (22) The requested URL returned error: 503",
    });

    expect(screen.getByRole("heading", { name: "Codex didn't install" })).toBeInTheDocument();
    const row = screen.getByTestId("coding-tool-codex");
    expect(within(row).getByText("Couldn't download — check your connection")).toBeInTheDocument();
    await userEvent.click(within(row).getByRole("button", { name: "View log" }));
    expect(within(row).getByText(/returned error: 503/)).toBeInTheDocument();

    expect(within(row).getByText("Use Claude Code instead")).toBeInTheDocument();
    await userEvent.click(within(row).getByRole("button", { name: "Show command" }));
    expect(within(row).getByText("npm install -g @openai/codex")).toBeInTheDocument();

    await userEvent.click(within(row).getByRole("button", { name: "Try again" }));
    expect(bridge.start).toHaveBeenLastCalledWith("codex", "install");
  });

  it("stops waiting on an installed tool the daemon never registers, and retries by restarting the daemon", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    try {
      const { bridge } = fakeBridge([
        { id: "claude", installed: false, version: null, loggedIn: null },
        { id: "codex", installed: true, version: null, loggedIn: null },
        { id: "cursor", installed: false, version: null, loggedIn: null },
      ]);
      const { onRuntimeRefresh } = renderStep({ codingTools: bridge });
      await waitFor(() => expect(screen.getByTestId("coding-tool-codex")).toHaveAttribute("data-state", "connecting"));

      await act(async () => {
        await vi.advanceTimersByTimeAsync(31_000);
      });
      const row = screen.getByTestId("coding-tool-codex");
      expect(row).toHaveAttribute("data-state", "failed");
      expect(within(row).getByText("Codex is installed, but Multica can't run it")).toBeInTheDocument();
      // Not an install failure: the heading stays on picking a tool.
      expect(screen.queryByRole("heading", { name: "Codex didn't install" })).toBeNull();

      onRuntimeRefresh.mockClear();
      await userEvent.click(within(row).getByRole("button", { name: "Try again" }));
      expect(onRuntimeRefresh).toHaveBeenCalled();
      expect(bridge.start).not.toHaveBeenCalled();
      expect(screen.getByTestId("coding-tool-codex")).toHaveAttribute("data-state", "connecting");
    } finally {
      vi.useRealTimers();
    }
  });

  it("lets an invited member skip, with their workspace in the heading", async () => {
    mocks.runtimes = [runtime("claude")];
    const { bridge } = fakeBridge([{ id: "claude", installed: true, version: "2.1.3", loggedIn: true }, ...MISSING.slice(1)]);
    const { onSkip } = renderStep({ codingTools: bridge, workspaceName: "Acme", skipHint: false });

    expect(await screen.findByRole("heading", { name: "Create your agent in Acme" })).toBeInTheDocument();
    expect(screen.queryByText(/we'll add a setup guide/i)).toBeNull();
    await userEvent.click(screen.getByRole("button", { name: "Skip for now" }));
    expect(onSkip).toHaveBeenCalled();
  });
});

describe("StepAgentSetup on the web", () => {
  it("lists the member's online runtimes and creates the agent on the chosen one", async () => {
    mocks.runtimes = [runtime("codex"), runtime("claude", "offline")];
    const { onCreate } = renderStep();

    expect(screen.getByRole("heading", { name: "Your computer is connected" })).toBeInTheDocument();
    expect(screen.getByRole("radio", { name: /codex \(laptop\)/i })).toHaveAttribute("aria-checked", "true");
    expect(screen.queryByText(/claude \(laptop\)/i)).toBeNull();

    const nameInput = screen.getByRole("textbox", { name: "Agent name" });
    await userEvent.clear(nameInput);
    await userEvent.type(nameInput, "Ada bot");
    await userEvent.click(screen.getByRole("button", { name: "Create agent and start chatting" }));
    expect(onCreate).toHaveBeenCalledWith(expect.objectContaining({ runtime: mocks.runtimes[0], name: "Ada bot" }));
  });

  it("lists the coding tools Multica installs first and preselects the top one", () => {
    mocks.runtimes = [runtime("antigravity"), runtime("cursor"), runtime("claude")];
    renderStep();

    const radios = screen.getAllByRole("radio");
    expect(radios.map((radio) => radio.textContent)).toEqual([
      expect.stringMatching(/^claude/),
      expect.stringMatching(/^cursor/),
      expect.stringMatching(/^antigravity/),
    ]);
    expect(radios[0]).toHaveAttribute("aria-checked", "true");
  });
});
