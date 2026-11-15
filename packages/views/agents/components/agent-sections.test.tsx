// @vitest-environment jsdom

import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import type { Agent, AgentRuntime } from "@multica/core/types";

// This module decides which sections exist and which editor each one opens;
// what each editor does is covered by its own tests, so they're stubs.
vi.mock("./agent-detail-inspector", () => ({
  AgentProfileSettings: () => <div>profile-form</div>,
  AgentExecutionSettings: () => <div>execution-form</div>,
}));
vi.mock("./agent-access-settings", () => ({
  AgentAccessSettings: () => <div>agent-access-settings</div>,
}));
vi.mock("./agent-channels-section", () => ({
  AgentChannelsSection: () => <div>channels-section</div>,
}));
vi.mock("./tabs/instructions-tab", () => ({
  InstructionsTab: () => <div>instructions-tab</div>,
}));
vi.mock("./tabs/skills-tab", () => ({
  SkillsTab: () => <div>skills-tab</div>,
}));
vi.mock("./tabs/env-tab", () => ({
  EnvTab: () => <div>env-tab</div>,
}));
vi.mock("./tabs/custom-args-tab", () => ({
  CustomArgsTab: () => <div>custom-args-tab</div>,
}));
vi.mock("./tabs/mcp-config-tab", () => ({
  McpConfigTab: () => <div>mcp-config-tab</div>,
}));
vi.mock("./tabs/agent-apps-tab", () => ({
  AgentAppsTab: () => <div>agent-apps-tab</div>,
}));
vi.mock("./tabs/qoder-agent-tab", () => ({
  QoderAgentTab: () => <div>qoder-agent-tab</div>,
}));
vi.mock("./tabs/runtime-config-tab", () => ({
  RuntimeConfigTab: () => <div>runtime-config-tab</div>,
}));

import {
  AgentSectionContent,
  resolveAgentSection,
  useAgentSectionGroups,
  type AgentSection,
} from "./agent-sections";

const agent = { id: "agent-1", name: "Agent" } as Agent;

function makeRuntime(provider: string): AgentRuntime {
  return {
    id: "runtime-1",
    workspace_id: "ws-1",
    daemon_id: null,
    name: "Runtime",
    runtime_mode: "local",
    provider,
    launch_header: "",
    status: "online",
    device_info: "",
    metadata: {},
    owner_id: null,
    visibility: "private",
    last_seen_at: null,
    created_at: "2026-05-28T00:00:00Z",
    updated_at: "2026-05-28T00:00:00Z",
  };
}

function Sections({
  runtime,
  canEdit,
  view,
}: {
  runtime: AgentRuntime | null;
  canEdit: boolean;
  view: string | null;
}) {
  const groups = useAgentSectionGroups({ runtime, canEdit });
  const active = resolveAgentSection(view, groups);
  return (
    <>
      {groups.map((group) => (
        <ul key={group.key} aria-label={group.key}>
          {group.items.map((item) => (
            <li key={item.id}>{`${item.id}:${item.labelKey}`}</li>
          ))}
        </ul>
      ))}
      <output>{active.id}</output>
    </>
  );
}

function renderSections(
  runtime: AgentRuntime | null,
  { canEdit = true, view = null }: { canEdit?: boolean; view?: string | null } = {},
) {
  return render(<Sections runtime={runtime} canEdit={canEdit} view={view} />);
}

const ids = () =>
  screen.getAllByRole("listitem").map((item) => item.textContent!.split(":")[0]);
const group = (key: string) =>
  Array.from(
    screen.getByRole("list", { name: key }).querySelectorAll("li"),
    (item) => item.textContent,
  );

describe("agent page sections", () => {
  it("groups every section an ordinary runtime offers", () => {
    renderSections(makeRuntime("claude"));
    expect(group("main")).toEqual(["overview:overview", "work:work"]);
    expect(group("define")).toEqual([
      "general:profile",
      "instructions:instructions",
      "skills:skills",
      "mcp_config:mcp_config",
    ]);
    expect(group("connect")).toEqual(["channels:channels", "access:access"]);
    expect(group("run")).toEqual([
      "runtime:runtime",
      "env:environment",
      "custom_args:custom_args",
    ]);
  });

  it("offers Routing only on OpenClaw", () => {
    renderSections(makeRuntime("openclaw"));
    expect(group("run")).toContain("runtime_config:runtime_config");
  });

  it("drops what the Qoder bridge rejects and names the Qoder Cloud Agents section", () => {
    renderSections(makeRuntime("qoder_cloud"));
    expect(ids()).toEqual([
      "overview",
      "work",
      "general",
      "instructions",
      "channels",
      "access",
      "runtime",
      "runtime_config",
    ]);
    expect(group("run")).toContain("runtime_config:qoder_agent");
  });
});

describe("agent page MCP section", () => {
  it.each([
    "claude",
    "codex",
    "cursor",
    "hermes",
    "kimi",
    "kiro",
    "opencode",
    "openclaw",
    "omp",
  ])("is offered on the %s runtime", (provider) => {
    renderSections(makeRuntime(provider));
    expect(ids()).toContain("mcp_config");
  });

  it("is hidden for providers whose backend does not read mcp_config", () => {
    // Saving an MCP config on e.g. Gemini would be a silent no-op at run
    // time — that's the bug this hiding logic is meant to prevent.
    renderSections(makeRuntime("gemini"));
    expect(ids()).not.toContain("mcp_config");
  });

  it("stays while the runtime row hasn't loaded yet", () => {
    // No runtime mimics the window before the runtimes query resolves.
    // Hiding it would flicker it off and back on.
    renderSections(null);
    expect(ids()).toContain("mcp_config");
  });
});

describe("agent page Environment section", () => {
  it("is offered to someone who can manage the agent", () => {
    renderSections(makeRuntime("claude"));
    expect(ids()).toContain("env");
  });

  it("is hidden from users who cannot manage the agent", () => {
    // The env endpoints admit the agent owner or a workspace owner/admin
    // (MUL-5438) — the rule `canEdit` already encodes. Anyone else who opens
    // it hits a guaranteed 403 on "Reveal & edit".
    renderSections(makeRuntime("claude"), { canEdit: false });
    expect(ids()).not.toContain("env");
  });
});

describe("resolveAgentSection", () => {
  it("opens the section a link names", () => {
    renderSections(makeRuntime("claude"), { view: "general" });
    expect(screen.getByRole("status")).toHaveTextContent("general");
  });

  it.each([null, "overview", "bogus"])("lands %s on Overview", (view) => {
    renderSections(makeRuntime("claude"), { view });
    expect(screen.getByRole("status")).toHaveTextContent("overview");
  });

  it("lands a link to a section the user can't open on Overview", () => {
    renderSections(makeRuntime("claude"), { canEdit: false, view: "env" });
    expect(screen.getByRole("status")).toHaveTextContent("overview");
  });
});

describe("AgentSectionContent", () => {
  function renderContent(
    section: Exclude<AgentSection, "overview" | "work">,
    runtime: AgentRuntime | null = makeRuntime("claude"),
  ) {
    return render(
      <AgentSectionContent
        section={section}
        agent={agent}
        runtime={runtime}
        runtimes={runtime ? [runtime] : []}
        members={[]}
        currentUserId="user-1"
        canEdit
        onUpdate={vi.fn().mockResolvedValue(undefined)}
        onDirtyChange={vi.fn()}
      />,
    );
  }

  it.each([
    ["general", "profile-form"],
    ["runtime", "execution-form"],
    ["instructions", "instructions-tab"],
    ["skills", "skills-tab"],
    ["mcp_config", "mcp-config-tab"],
    ["composio_mcp", "agent-apps-tab"],
    ["channels", "channels-section"],
    ["access", "agent-access-settings"],
    ["env", "env-tab"],
    ["custom_args", "custom-args-tab"],
  ] as const)("renders %s with its editor", (section, text) => {
    renderContent(section);
    expect(screen.getByText(text)).toBeInTheDocument();
  });

  it("routes the runtime config section by runtime", () => {
    const { unmount } = renderContent("runtime_config", makeRuntime("openclaw"));
    expect(screen.getByText("runtime-config-tab")).toBeInTheDocument();
    unmount();
    renderContent("runtime_config", makeRuntime("qoder_cloud"));
    expect(screen.getByText("qoder-agent-tab")).toBeInTheDocument();
  });
});
