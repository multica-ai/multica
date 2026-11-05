// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, cleanup } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import type { Agent } from "@multica/core/types";
import enCommon from "../../locales/en/common.json";
import enAgents from "../../locales/en/agents.json";

const TEST_RESOURCES = { en: { common: enCommon, agents: enAgents } };

const AGENT_ID = "agent-1";

const squads = [
  { id: "squad-a", name: "Greenfield", archived_at: null },
  { id: "squad-b", name: "Old Squad", archived_at: "2026-01-01T00:00:00Z" },
  { id: "squad-c", name: "Other", archived_at: null },
];

const defaultMembersBySquad: Record<string, unknown[]> = {
  "squad-a": [
    { member_type: "agent", member_id: AGENT_ID },
    { member_type: "agent", member_id: "agent-2" },
  ],
  "squad-b": [{ member_type: "agent", member_id: AGENT_ID }],
  "squad-c": [{ member_type: "agent", member_id: "agent-9" }],
};

// Tests that need a different roster overwrite this in beforeEach.
let membersBySquad: Record<string, unknown[]> = defaultMembersBySquad;

const skillsBySquad: Record<string, unknown[]> = {
  "squad-a": [
    { id: "skill-sdlc", name: "sdlc-review", description: "" },
    { id: "skill-direct", name: "shared-skill", description: "" },
  ],
  "squad-b": [{ id: "skill-archived", name: "archived-skill", description: "" }],
  "squad-c": [{ id: "skill-other", name: "other-skill", description: "" }],
};

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/api", () => ({
  api: {
    listSquads: async () => squads,
    listSquadMembers: async (squadId: string) => membersBySquad[squadId] ?? [],
    listSquadSkills: async (squadId: string) => skillsBySquad[squadId] ?? [],
    listSkills: async () => [],
  },
}));

vi.mock("./skill-add-dialog", () => ({
  SkillAddDialog: () => null,
}));

import { AgentSkillsSection } from "./agent-skills-section";

function makeAgent(skills: { id: string; name: string }[]): Agent {
  return {
    id: AGENT_ID,
    name: "Dev",
    skills,
  } as unknown as Agent;
}

function renderSection(agent: Agent) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <AgentSkillsSection agent={agent} canEdit={false} onManage={() => {}} />
      </I18nProvider>
    </QueryClientProvider>,
  );
}

describe("AgentSkillsSection inherited squad skills", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    membersBySquad = defaultMembersBySquad;
  });
  afterEach(() => {
    cleanup();
  });

  it("lists skills from active squads the agent belongs to, read-only", async () => {
    renderSection(makeAgent([{ id: "skill-direct", name: "shared-skill" }]));

    expect(await screen.findByText("sdlc-review")).toBeTruthy();
    expect(screen.getByText("Inherited from squads")).toBeTruthy();
  });

  it("skips archived squads, squads the agent is not in, and skills attached directly", async () => {
    renderSection(makeAgent([{ id: "skill-direct", name: "shared-skill" }]));

    await screen.findByText("sdlc-review");
    expect(screen.queryByText("archived-skill")).toBeNull();
    expect(screen.queryByText("other-skill")).toBeNull();
    expect(screen.getAllByText("shared-skill")).toHaveLength(1);
  });

  it("omits the inherited block when no squad applies a skill", async () => {
    membersBySquad = { "squad-a": [{ member_type: "agent", member_id: "agent-2" }] };
    renderSection(makeAgent([]));

    await screen.findByText(enAgents.tab_body.skills.empty_title);
    expect(screen.queryByText("Inherited from squads")).toBeNull();
    expect(screen.getByText(enAgents.tab_body.skills.empty_title)).toBeTruthy();
  });
});
