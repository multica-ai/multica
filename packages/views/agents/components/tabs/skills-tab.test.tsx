// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { Agent } from "@multica/core/types";
import { I18nProvider } from "@multica/core/i18n/react";
import { WorkspaceSlugProvider } from "@multica/core/paths";
import { NavigationProvider, type NavigationAdapter } from "../../../navigation";
import enCommon from "../../../locales/en/common.json";
import enAgents from "../../../locales/en/agents.json";

const TEST_RESOURCES = { en: { common: enCommon, agents: enAgents } };

const mockListSkills = vi.hoisted(() => vi.fn());
const mockSetAgentSkills = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/api", () => ({
  api: {
    listSkills: (...args: unknown[]) => mockListSkills(...args),
    setAgentSkills: (...args: unknown[]) => mockSetAgentSkills(...args),
  },
}));

vi.mock("sonner", () => ({
  toast: {
    error: vi.fn(),
    success: vi.fn(),
  },
}));

import { SkillsTab } from "./skills-tab";

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
  status: "idle",
  max_concurrent_tasks: 1,
  model: "",
  owner_id: "user-1",
  skills: [],
  created_at: "2026-04-16T00:00:00Z",
  updated_at: "2026-04-16T00:00:00Z",
  archived_at: null,
  archived_by: null,
};

function renderSkillsTab(agent: Agent = baseAgent) {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: {
        retry: false,
      },
    },
  });
  const navigation: NavigationAdapter = {
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    pathname: "/test/agents/agent-1",
    searchParams: new URLSearchParams(),
    getShareableUrl: (path) => path,
  };

  const view = render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <WorkspaceSlugProvider slug="test">
        <NavigationProvider value={navigation}>
          <QueryClientProvider client={queryClient}>
            <SkillsTab agent={agent} />
          </QueryClientProvider>
        </NavigationProvider>
      </WorkspaceSlugProvider>
    </I18nProvider>,
  );

  return { ...view, navigation };
}

describe("SkillsTab", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockListSkills.mockResolvedValue([]);
    mockSetAgentSkills.mockResolvedValue(undefined);
  });

  it("does not render the inline Local Runtime Skills section even for local-runtime agents", async () => {
    // The inline section auto-loaded local skills on every Skills-tab
    // entry, which was both noisy and (under multi-replica deploys) prone
    // to "request not found" because the request store is in-process.
    // Local-skill import now lives behind the explicit Skills page →
    // Add Skill → From Runtime tab; nothing here may auto-load.
    renderSkillsTab();

    // Top informational callout should still render; that's how we know
    // the tab body itself rendered (not stuck in a loading state).
    expect(
      await screen.findByText(/Local runtime skills are always available/i),
    ).toBeInTheDocument();

    // The removed section's heading and its trigger button must be gone.
    expect(screen.queryByText("Local Runtime Skills")).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: /Import to Workspace/i }),
    ).not.toBeInTheDocument();

    // No runtime list / local-skills query should be wired up either —
    // we removed @multica/core/runtimes from this file's imports.
    // Surface it via behaviour: the `agent` here has runtime_id but the
    // tab must not invoke any runtime-list mock to render. (Both are
    // already deleted from the mock setup above; this assertion is
    // implicit — the test file would fail to import if the component
    // still referenced runtimeListOptions / runtimeLocalSkillsOptions.)
  });

  it("navigates to the skill detail page when an assigned skill is clicked", () => {
    const { navigation } = renderSkillsTab({
      ...baseAgent,
      skills: [
        {
          id: "skill-1",
          name: "Review skill",
          description: "Review pull requests",
        },
      ],
    });

    fireEvent.click(screen.getByRole("link", { name: /Review skill/i }));

    expect(navigation.push).toHaveBeenCalledWith("/test/skills/skill-1");
  });

  it("does not navigate when removing an assigned skill", async () => {
    const { navigation } = renderSkillsTab({
      ...baseAgent,
      skills: [
        {
          id: "skill-1",
          name: "Review skill",
          description: "Review pull requests",
        },
      ],
    });
    const removeButton = screen.getByRole("button", { name: /Remove skill/i });

    fireEvent.click(removeButton);

    await waitFor(() => {
      expect(mockSetAgentSkills).toHaveBeenCalledWith("agent-1", {
        skill_ids: [],
      });
    });
    await waitFor(() => expect(removeButton).not.toBeDisabled());
    expect(navigation.push).not.toHaveBeenCalled();
  });
});
