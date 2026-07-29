// @vitest-environment jsdom

// FIR-3212 — the Setup screen must expose the two brief-layer modes
// (workspace_brief_mode, tools_brief_mode) as settable controls that show the
// agent's current value and emit a change, so an admin can free a triage/CFO
// agent from the shared brief without editing code.

import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Agent } from "@multica/core/types";

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/runtimes", () => ({
  runtimeListOptions: () => {
    const runtimes = [
      {
        id: "runtime-1",
        name: "Claude workstation",
        provider: "claude",
        status: "online",
      },
    ];
    return {
      queryKey: ["runtimes"],
      queryFn: async () => runtimes,
      initialData: runtimes,
    };
  },
  runtimeModelsOptions: () => ({
    queryKey: ["runtime-models"],
    queryFn: async () => ({ supported: true, models: [] }),
  }),
}));

import { AgentContextConfigFields } from "./agent-context-config-fields";

const agent = {
  id: "agent-1",
  workspace_id: "ws-1",
  runtime_id: "runtime-1",
  name: "Kathrine",
  description: "",
  instructions: "",
  avatar_url: null,
  runtime_mode: "cloud",
  runtime_config: {},
  custom_args: [],
  custom_env_redacted: false,
  visibility: "workspace",
  status: "idle",
  max_concurrent_tasks: 1,
  model: "",
  owner_id: "user-1",
  skills: [],
  created_at: "2026-04-16T00:00:00Z",
  updated_at: "2026-04-16T00:00:00Z",
  archived_at: null,
  archived_by: null,
} satisfies Agent;

function renderFields(overrides: {
  workspaceBriefMode?: string;
  toolsBriefMode?: string;
  systemPromptMode?: string;
  speedMode?: string;
  maxTurns?: string;
  timeoutMinutes?: string;
  runtimeId?: string;
  instructions?: string;
  controlFirst?: boolean;
  onWorkspaceBriefMode?: (v: string) => void;
  onToolsBriefMode?: (v: string) => void;
  onSystemPromptMode?: (v: string) => void;
  onSpeedMode?: (v: string) => void;
  onMaxTurns?: (v: string) => void;
  onTimeoutMinutes?: (v: string) => void;
  onRuntimeId?: (v: string) => void;
} = {}) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <AgentContextConfigFields
        agent={agent}
        runtimeId={overrides.runtimeId ?? agent.runtime_id}
        model=""
        thinkingLevel=""
        workspaceBriefMode={overrides.workspaceBriefMode ?? ""}
        toolsBriefMode={overrides.toolsBriefMode ?? ""}
        systemPromptMode={overrides.systemPromptMode ?? ""}
        speedMode={overrides.speedMode ?? ""}
        maxTurns={overrides.maxTurns ?? ""}
        timeoutMinutes={overrides.timeoutMinutes ?? ""}
        instructions={overrides.instructions}
        controlFirst={overrides.controlFirst}
        onModel={() => {}}
        onRuntimeId={overrides.onRuntimeId ?? (() => {})}
        onThinkingLevel={() => {}}
        onWorkspaceBriefMode={overrides.onWorkspaceBriefMode ?? (() => {})}
        onToolsBriefMode={overrides.onToolsBriefMode ?? (() => {})}
        onSystemPromptMode={overrides.onSystemPromptMode ?? (() => {})}
        onSpeedMode={overrides.onSpeedMode ?? (() => {})}
        onMaxTurns={overrides.onMaxTurns ?? (() => {})}
        onTimeoutMinutes={overrides.onTimeoutMinutes ?? (() => {})}
      />
    </QueryClientProvider>,
  );
}

describe("AgentContextConfigFields brief-layer controls (FIR-3212)", () => {
  it("renders both controls at their current value", () => {
    renderFields({ workspaceBriefMode: "off", toolsBriefMode: "summary" });

    const workspace = screen.getByLabelText("Workspace brief") as HTMLSelectElement;
    const tools = screen.getByLabelText("Tools list") as HTMLSelectElement;
    expect(workspace.value).toBe("off");
    expect(tools.value).toBe("summary");
  });

  it("defaults both controls to the full brief when unset", () => {
    renderFields();

    expect((screen.getByLabelText("Workspace brief") as HTMLSelectElement).value).toBe("");
    expect((screen.getByLabelText("Tools list") as HTMLSelectElement).value).toBe("");
  });

  it("emits the chosen workspace-brief mode", () => {
    const onWorkspaceBriefMode = vi.fn();
    renderFields({ onWorkspaceBriefMode });

    fireEvent.change(screen.getByLabelText("Workspace brief"), {
      target: { value: "off" },
    });
    expect(onWorkspaceBriefMode).toHaveBeenCalledWith("off");
  });

  it("emits the chosen tools-brief mode", () => {
    const onToolsBriefMode = vi.fn();
    renderFields({ onToolsBriefMode });

    fireEvent.change(screen.getByLabelText("Tools list"), {
      target: { value: "summary" },
    });
    expect(onToolsBriefMode).toHaveBeenCalledWith("summary");
  });

  // FIR-3212 was raised to get an agent off the engine's own coding-agent
  // instruction. Until this control existed the mode could only be set by
  // hand-writing the API payload, so the answer to the issue was unreachable
  // from the product.
  it("renders the engine system-prompt control at its current value", () => {
    renderFields({ systemPromptMode: "replace" });

    expect((screen.getByLabelText("Engine system prompt") as HTMLSelectElement).value).toBe(
      "replace",
    );
  });

  it("defaults the engine system-prompt control to the engine default", () => {
    renderFields();

    expect((screen.getByLabelText("Engine system prompt") as HTMLSelectElement).value).toBe("");
  });

  it("emits the chosen system-prompt mode", () => {
    const onSystemPromptMode = vi.fn();
    renderFields({ onSystemPromptMode });

    fireEvent.change(screen.getByLabelText("Engine system prompt"), {
      target: { value: "replace" },
    });
    expect(onSystemPromptMode).toHaveBeenCalledWith("replace");
  });

  // Clearing the control must be expressible: "" is what hands the engine's own
  // system prompt back, so it cannot be a dead option.
  it("can be cleared back to the engine default", () => {
    const onSystemPromptMode = vi.fn();
    renderFields({ systemPromptMode: "replace", onSystemPromptMode });

    fireEvent.change(screen.getByLabelText("Engine system prompt"), {
      target: { value: "" },
    });
    expect(onSystemPromptMode).toHaveBeenCalledWith("");
  });
});

describe("AgentContextConfigFields control-first layout (FIR-4000)", () => {
  it("groups the job, reading context and run controls in one human-readable home", () => {
    renderFields({
      controlFirst: true,
      instructions: "Own customer outcomes.",
      speedMode: "fast",
      maxTurns: "18",
      timeoutMinutes: "45",
    });

    expect(screen.getByText("At a glance")).toBeTruthy();
    expect(screen.getByText("What this agent does")).toBeTruthy();
    expect(screen.getByText("What this agent reads before work")).toBeTruthy();
    expect(screen.getByText("How this agent runs")).toBeTruthy();
    expect((screen.getByLabelText("Instructions") as HTMLTextAreaElement).value).toBe(
      "Own customer outcomes.",
    );
    expect(
      (screen.getByLabelText("Response speed") as HTMLSelectElement).value,
    ).toBe("fast");
    expect((screen.getByLabelText("Where it runs") as HTMLSelectElement).value).toBe(
      agent.runtime_id,
    );
    expect((screen.getByLabelText("Maximum steps") as HTMLInputElement).value).toBe(
      "18",
    );
    expect((screen.getByLabelText("Time limit") as HTMLInputElement).value).toBe(
      "45",
    );
    expect(screen.getByText("Advanced")).toBeTruthy();
  });

  it("emits versioned run-control changes", () => {
    const onRuntimeId = vi.fn();
    const onSpeedMode = vi.fn();
    const onMaxTurns = vi.fn();
    const onTimeoutMinutes = vi.fn();
    renderFields({
      controlFirst: true,
      onRuntimeId,
      onSpeedMode,
      onMaxTurns,
      onTimeoutMinutes,
    });

    fireEvent.change(screen.getByLabelText("Response speed"), {
      target: { value: "fast" },
    });
    fireEvent.change(screen.getByLabelText("Where it runs"), {
      target: { value: agent.runtime_id },
    });
    fireEvent.change(screen.getByLabelText("Maximum steps"), {
      target: { value: "21" },
    });
    fireEvent.change(screen.getByLabelText("Time limit"), {
      target: { value: "50" },
    });

    expect(onSpeedMode).toHaveBeenCalledWith("fast");
    expect(onRuntimeId).toHaveBeenCalledWith(agent.runtime_id);
    expect(onMaxTurns).toHaveBeenCalledWith("21");
    expect(onTimeoutMinutes).toHaveBeenCalledWith("50");
  });
});
