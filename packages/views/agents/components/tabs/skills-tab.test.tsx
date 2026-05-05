// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import type { Agent } from "@multica/core/types";

const mockListSkills = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/api", () => ({
  api: {
    listSkills: (...args: unknown[]) => mockListSkills(...args),
    setAgentSkills: vi.fn(),
  },
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
  custom_env: {},
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
};

function renderSkillsTab() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: {
        retry: false,
      },
    },
  });

  return render(
    <QueryClientProvider client={queryClient}>
      <SkillsTab agent={agent} />
    </QueryClientProvider>,
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
});
