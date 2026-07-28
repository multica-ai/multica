// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Agent } from "@multica/core/types";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../../locales/en/common.json";
import enAgents from "../../../locales/en/agents.json";

const TEST_RESOURCES = { en: { common: enCommon, agents: enAgents } };

const mockListSkills = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector?: (s: { user: { id: string } | null }) => unknown) => {
    const state = { user: null };
    return selector ? selector(state) : state;
  },
}));

vi.mock("@multica/core/api", () => ({
  api: {
    listSkills: (...args: unknown[]) => mockListSkills(...args),
    setAgentSkills: vi.fn(),
    listMembers: vi.fn().mockResolvedValue([]),
    listAgentContextVersions: vi.fn().mockResolvedValue([]),
    listAgentContextChangeRequests: vi.fn().mockResolvedValue([]),
  },
}));

vi.mock("sonner", () => ({
  toast: {
    error: vi.fn(),
    success: vi.fn(),
  },
}));

import { SkillsTab } from "./skills-tab";

const agent: Agent = {
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

function renderSkillsTab(opts: { agent?: Agent; canEdit?: boolean } = {}) {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: {
        retry: false,
      },
    },
  });

  return render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <QueryClientProvider client={queryClient}>
        <SkillsTab
          agent={opts.agent ?? agent}
          canEdit={opts.canEdit ?? true}
        />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

describe("SkillsTab", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockListSkills.mockResolvedValue([]);
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

  it("hides add and remove actions when canEdit is false", async () => {
    mockListSkills.mockResolvedValue([
      { id: "skill-2", name: "Other skill", description: "" },
    ]);
    renderSkillsTab({
      canEdit: false,
      agent: {
        ...agent,
        skills: [{ id: "skill-1", name: "Attached skill", description: "" }],
      },
    });

    expect(await screen.findByText("Attached skill")).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /Add skill/i }),
    ).toBeDisabled();
    expect(
      screen.queryByRole("button", { name: /Remove/i }),
    ).not.toBeInTheDocument();
  });

  // FIR-3805 — the "Always on" checkbox lives on the skill's own row, so the
  // row is both the control and the answer to "which skills are always on".
  describe("always-on skills", () => {
    it("renders a checkbox per bound skill, ticked from the binding", async () => {
      renderSkillsTab({
        agent: {
          ...agent,
          skills: [
            { id: "skill-1", name: "Caveman", description: "", always_on: true },
            { id: "skill-2", name: "Deploy", description: "" },
          ],
        },
      });

      expect(await screen.findByText("Caveman")).toBeInTheDocument();
      const boxes = screen.getAllByRole("checkbox", { name: /Always on/i });
      expect(boxes).toHaveLength(2);
      expect(boxes[0]).toBeChecked();
      expect(boxes[1]).not.toBeChecked();
    });

    it("disables the checkbox when the user cannot manage the agent", async () => {
      renderSkillsTab({
        canEdit: false,
        agent: {
          ...agent,
          skills: [{ id: "skill-1", name: "Caveman", description: "" }],
        },
      });

      expect(await screen.findByText("Caveman")).toBeInTheDocument();
      // Base UI renders the checkbox as a span with role=checkbox, so the
      // disabled state is aria-disabled rather than the native attribute.
      expect(screen.getByRole("checkbox", { name: /Always on/i })).toHaveAttribute(
        "aria-disabled",
        "true",
      );
    });

    it("offers Propose change once a box is ticked", async () => {
      const user = userEvent.setup();
      renderSkillsTab({
        agent: {
          ...agent,
          skills: [{ id: "skill-1", name: "Caveman", description: "" }],
        },
      });

      expect(await screen.findByText("Caveman")).toBeInTheDocument();
      // Ticking the box is a proposable change on its own — no skill was
      // added or removed, so this is what proves the flag is versioned.
      expect(
        screen.queryByRole("button", { name: /Propose change/i }),
      ).not.toBeInTheDocument();

      await user.click(screen.getByRole("checkbox", { name: /Always on/i }));

      expect(
        await screen.findByRole("button", { name: /Propose change/i }),
      ).toBeInTheDocument();
    });
  });
});
