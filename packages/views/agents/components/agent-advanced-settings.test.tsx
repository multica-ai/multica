// @vitest-environment jsdom

import { useEffect } from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Agent, AgentRuntime } from "@multica/core/types";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enAgents from "../../locales/en/agents.json";
import {
  NavigationProvider,
  type NavigationAdapter,
} from "../../navigation";

const TEST_RESOURCES = { en: { common: enCommon, agents: enAgents } };

// The dialog decides which sections exist; what each section does is
// covered by its own tests, so the editors are stubs.
vi.mock("./agent-detail-inspector", () => ({
  AgentDetailInspector: () => <div>general-form</div>,
}));
vi.mock("./agent-access-settings", () => ({
  AgentAccessSettings: () => <div>agent-access-settings</div>,
}));
vi.mock("./tabs/skills-tab", () => ({
  SkillsTab: () => <div>skills-tab</div>,
}));
vi.mock("./tabs/env-tab", () => ({
  EnvTab: () => <div>env-tab</div>,
}));
vi.mock("./tabs/custom-args-tab", () => ({
  CustomArgsTab: ({ onDirtyChange }: { onDirtyChange?: (dirty: boolean) => void }) => {
    useEffect(() => onDirtyChange?.(true), [onDirtyChange]);
    return <div>custom-args-tab</div>;
  },
}));
vi.mock("./tabs/mcp-config-tab", () => ({
  McpConfigTab: () => <div>mcp-config-tab</div>,
}));
vi.mock("./tabs/agent-mcp-tab", () => ({
  AgentMcpTab: () => <div>agent-mcp-tab</div>,
}));
vi.mock("./tabs/integrations-tab", () => ({
  IntegrationsTab: () => <div>integrations-tab</div>,
}));
vi.mock("./tabs/qoder-agent-tab", () => ({
  QoderAgentTab: () => <div>qoder-agent-tab</div>,
}));
vi.mock("./tabs/runtime-config-tab", () => ({
  RuntimeConfigTab: () => <div>runtime-config-tab</div>,
}));

// Whether the Integrations section is worth showing depends on the
// deployment having a channel integration configured. Each listing is
// backed by a ref so a test can flip `configured`.
const larkListingRef = vi.hoisted(() => ({
  current: { installations: [] as unknown[], configured: false },
}));
const slackListingRef = vi.hoisted(() => ({
  current: { installations: [] as unknown[], configured: false },
}));
const telegramListingRef = vi.hoisted(() => ({
  current: { installations: [] as unknown[], configured: false },
}));
vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));
vi.mock("@multica/core/lark", () => ({
  larkInstallationsOptions: () => ({
    queryKey: ["lark", "installations"],
    queryFn: () => Promise.resolve(larkListingRef.current),
  }),
}));
vi.mock("@multica/core/slack", () => ({
  slackInstallationsOptions: () => ({
    queryKey: ["slack", "installations"],
    queryFn: () => Promise.resolve(slackListingRef.current),
  }),
}));
vi.mock("@multica/core/telegram", () => ({
  telegramInstallationsOptions: () => ({
    queryKey: ["telegram", "installations"],
    queryFn: () => Promise.resolve(telegramListingRef.current),
  }),
}));

import { AgentAdvancedSettings, type AdvancedSection } from "./agent-advanced-settings";

const baseAgent: Agent = {
  id: "agent-1",
  workspace_id: "ws-1",
  runtime_id: "runtime-1",
  name: "Agent",
  description: "",
  instructions: "",
  avatar_url: null,
  runtime_mode: "local",
  runtime_config: {},
  custom_args: [],
  visibility: "workspace",
  permission_mode: "public_to",
  invocation_targets: [{ target_type: "workspace", target_id: null }],
  status: "idle",
  max_concurrent_tasks: 1,
  model: "",
  owner_id: "user-1",
  skills: [],
  created_at: "2026-05-28T00:00:00Z",
  updated_at: "2026-05-28T00:00:00Z",
  archived_at: null,
  archived_by: null,
};

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

function renderDialog(
  runtimes: AgentRuntime[],
  {
    canEdit = true,
    section = "general",
    onSectionChange = vi.fn(),
  }: {
    canEdit?: boolean;
    section?: AdvancedSection | null;
    onSectionChange?: (section: AdvancedSection | null) => void;
  } = {},
) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const navigation: NavigationAdapter = {
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    pathname: "/acme/agents/agent-1",
    searchParams: new URLSearchParams(),
    hash: "",
    getShareableUrl: (path) => path,
  };
  return render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <NavigationProvider value={navigation}>
        <QueryClientProvider client={queryClient}>
          <AgentAdvancedSettings
            agent={baseAgent}
            runtime={runtimes[0] ?? null}
            runtimes={runtimes}
            members={[]}
            onUpdate={vi.fn().mockResolvedValue(undefined)}
            currentUserId="user-1"
            canEdit={canEdit}
            section={section}
            onSectionChange={onSectionChange}
          />
        </QueryClientProvider>
      </NavigationProvider>
    </I18nProvider>,
  );
}

const sectionNames = () =>
  screen.getAllByRole("tab").map((tab) => tab.textContent);

beforeEach(() => {
  larkListingRef.current = { installations: [], configured: false };
  slackListingRef.current = { installations: [], configured: false };
  telegramListingRef.current = { installations: [], configured: false };
});

describe("AgentAdvancedSettings MCP section visibility", () => {
  it.each([
    ["Claude", "claude"],
    ["Codex", "codex"],
    ["Cursor", "cursor"],
    ["Hermes", "hermes"],
    ["Kimi", "kimi"],
    ["Kiro", "kiro"],
    ["OpenCode", "opencode"],
    ["OpenClaw", "openclaw"],
    ["Oh My Pi", "omp"],
  ])("offers MCP when the agent runs on the %s runtime", (_label, provider) => {
    renderDialog([makeRuntime(provider)]);
    expect(screen.getByRole("tab", { name: /^MCP$/i })).toBeInTheDocument();
  });

  it("hides MCP for providers whose backend does not read mcp_config", () => {
    // Saving an MCP config on e.g. Gemini would be a silent no-op at run
    // time — that's the bug this hiding logic is meant to prevent.
    renderDialog([makeRuntime("gemini")]);
    expect(screen.queryByRole("tab", { name: /^MCP$/i })).not.toBeInTheDocument();
  });

  it("keeps MCP visible when the runtime row hasn't loaded yet", () => {
    // Empty runtimes[] mimics the brief window between the page mounting and
    // the runtimes query resolving. Hiding it would flicker it off and back on.
    renderDialog([]);
    expect(screen.getByRole("tab", { name: /^MCP$/i })).toBeInTheDocument();
  });
});

describe("AgentAdvancedSettings Integrations section visibility", () => {
  it.each([
    ["Lark", larkListingRef],
    // Regression: the gate must consider Slack too, not just Lark — a
    // Slack-only deployment was hiding the section (and its bind entry).
    ["Slack", slackListingRef],
    ["Telegram", telegramListingRef],
  ])("shows Integrations once %s is configured", async (_name, ref) => {
    ref.current = { installations: [], configured: true };
    renderDialog([makeRuntime("claude")]);
    expect(
      await screen.findByRole("tab", { name: /^Integrations$/i }),
    ).toBeInTheDocument();
  });

  it("hides Integrations when no channel integration is configured", () => {
    renderDialog([makeRuntime("claude")]);
    expect(
      screen.queryByRole("tab", { name: /^Integrations$/i }),
    ).not.toBeInTheDocument();
  });
});

describe("AgentAdvancedSettings Environment section visibility", () => {
  it("shows Environment to someone who can manage the agent", () => {
    renderDialog([makeRuntime("claude")]);
    expect(screen.getByRole("tab", { name: /^Environment$/i })).toBeInTheDocument();
  });

  it("hides Environment from users who cannot manage the agent", () => {
    // The env endpoints admit the agent owner or a workspace owner/admin
    // (MUL-5438) — the rule `canEdit` already encodes. Anyone else who opens
    // it hits a guaranteed 403 on "Reveal & edit".
    renderDialog([makeRuntime("claude")], { canEdit: false });
    expect(
      screen.queryByRole("tab", { name: /^Environment$/i }),
    ).not.toBeInTheDocument();
  });

  it("lands a link to a section the user can't open on General", () => {
    renderDialog([makeRuntime("claude")], { canEdit: false, section: "env" });
    expect(screen.getByRole("tab", { name: "General" })).toHaveAttribute(
      "aria-selected",
      "true",
    );
    expect(screen.getByText("general-form")).toBeInTheDocument();
    expect(screen.queryByText("env-tab")).not.toBeInTheDocument();
  });
});

describe("AgentAdvancedSettings section lists", () => {
  it("lists settings, then capabilities, for an ordinary runtime", () => {
    renderDialog([makeRuntime("claude")]);
    expect(sectionNames()).toEqual([
      "General",
      "Access",
      "Environment",
      "Custom Args",
      "Skills",
      "MCP",
    ]);
  });

  it("offers Routing only on OpenClaw", () => {
    renderDialog([makeRuntime("openclaw")]);
    expect(screen.getByRole("tab", { name: "Routing" })).toBeInTheDocument();
  });

  it("drops what the Qoder bridge rejects and names the Qoder Cloud Agents section", () => {
    larkListingRef.current = { installations: [], configured: true };
    renderDialog([makeRuntime("qoder_cloud")]);
    expect(sectionNames()).toEqual(["General", "Access", "Qoder Cloud Agents"]);
  });

  it("renders nothing while closed", () => {
    renderDialog([makeRuntime("claude")], { section: null });
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });
});

describe("AgentAdvancedSettings unsaved edits", () => {
  it("asks before leaving a section with unsaved edits", () => {
    const onSectionChange = vi.fn();
    renderDialog([makeRuntime("claude")], {
      section: "custom_args",
      onSectionChange,
    });
    expect(screen.getByText("custom-args-tab")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("tab", { name: "General" }));
    expect(onSectionChange).not.toHaveBeenCalled();
    expect(screen.getByText("Discard unsaved changes?")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Discard changes" }));
    expect(onSectionChange).toHaveBeenCalledWith("general");
  });

  it("moves straight to a section when nothing is unsaved", () => {
    const onSectionChange = vi.fn();
    renderDialog([makeRuntime("claude")], { onSectionChange });
    fireEvent.click(screen.getByRole("tab", { name: "Skills" }));
    expect(onSectionChange).toHaveBeenCalledWith("skills");
  });
});
