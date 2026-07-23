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
vi.mock("@multica/core/api", () => ({
  api: { listPersonaSandboxes: vi.fn().mockResolvedValue([]) },
}));
vi.mock("@multica/core/runtimes", () => ({
  runtimeListOptions: () => ({ queryKey: ["runtimes"], queryFn: async () => [] }),
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
  persona_sandbox: "",
} satisfies Agent;

function renderFields(overrides: {
  workspaceBriefMode?: string;
  toolsBriefMode?: string;
  onWorkspaceBriefMode?: (v: string) => void;
  onToolsBriefMode?: (v: string) => void;
} = {}) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <AgentContextConfigFields
        agent={agent}
        model=""
        thinkingLevel=""
        personaSandbox=""
        workspaceBriefMode={overrides.workspaceBriefMode ?? ""}
        toolsBriefMode={overrides.toolsBriefMode ?? ""}
        onModel={() => {}}
        onThinkingLevel={() => {}}
        onPersonaSandbox={() => {}}
        onWorkspaceBriefMode={overrides.onWorkspaceBriefMode ?? (() => {})}
        onToolsBriefMode={overrides.onToolsBriefMode ?? (() => {})}
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
});
