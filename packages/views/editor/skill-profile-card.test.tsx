// @vitest-environment jsdom

import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithI18n } from "../test/i18n";
import { workspaceKeys, skillDetailOptions } from "@multica/core/workspace/queries";
import type { Skill, Agent } from "@multica/core/types";
import { SkillProfileCard } from "./skill-profile-card";

// Pin the workspace context so skill-detail queries resolve against the
// same fixture scope. SkillProfileCard derives a workspace slug via
// useRequiredWorkspaceSlug, which throws when no slug is in scope; provide
// a stub wsId "ws-1" + "acme" slug here.
vi.mock("@multica/core/hooks", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/hooks")>();
  return { ...actual, useWorkspaceId: () => "ws-1" };
});
vi.mock("@multica/core/paths", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/paths")>();
  return {
    ...actual,
    useWorkspaceSlug: () => "acme",
    useRequiredWorkspaceSlug: () => "acme",
    useWorkspacePaths: () => ({
      ...actual.paths.workspace("acme"),
      agentDetail: (id: string) => `/acme/agents/${id}`,
    }),
  };
});

// AppLink depends on the navigation provider context; in the test we
// render SkillProfileCard in isolation, so swap AppLink for an anchor
// that doesn't read any provider.
vi.mock("../navigation/app-link", () => ({
  AppLink: ({
    href,
    children,
    ...props
  }: React.AnchorHTMLAttributes<HTMLAnchorElement> & { href: string }) => (
    <a href={href} {...props}>
      {children}
    </a>
  ),
}));

// Pin the workspace context so skill-detail queries resolve against the
// same fixture scope. SkillProfileCard derives a workspace slug via
// useRequiredWorkspaceSlug, which throws when no slug is in scope; provide
// a stub wsId "ws-1" + "acme" slug here.
vi.mock("@multica/core/hooks", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/hooks")>();
  return { ...actual, useWorkspaceId: () => "ws-1" };
});
vi.mock("@multica/core/paths", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/paths")>();
  return {
    ...actual,
    useWorkspaceSlug: () => "acme",
    useRequiredWorkspaceSlug: () => "acme",
    useWorkspacePaths: () => ({
      ...actual.paths.workspace("acme"),
      agentDetail: (id: string) => `/acme/agents/${id}`,
    }),
  };
});

// AppLink depends on the navigation provider context; in the test we
// render SkillProfileCard in isolation, so swap AppLink for an anchor
// that doesn't read any provider.
vi.mock("../navigation/app-link", () => ({
  AppLink: ({
    href,
    children,
    ...props
  }: React.AnchorHTMLAttributes<HTMLAnchorElement> & { href: string }) => (
    <a href={href} {...props}>
      {children}
    </a>
  ),
}));

const WS = "ws-1";

function TestHarness({
  children,
  skill,
  agents,
}: {
  children: React.ReactElement;
  skill?: Skill;
  agents?: Agent[];
}) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: Infinity } },
  });
  if (skill) {
    qc.setQueryData(skillDetailOptions(WS, skill.id).queryKey, {
      ...skill,
      files: [],
    });
  }
  if (agents) {
    qc.setQueryData(workspaceKeys.agents(WS), agents);
  }
  return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
}

const skillWithoutFrontmatter: Skill = {
  id: "skill-1",
  workspace_id: WS,
  name: "code-review",
  description: "Reviews PRs for style and correctness.",
  config: {},
  created_by: null,
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
  content: "This skill reviews pull requests.\n\nNo frontmatter here.",
  files: [],
};

const skillWithFrontmatter: Skill = {
  ...skillWithoutFrontmatter,
  id: "skill-2",
  name: "team-standup",
  content:
    "---\ndescription: Daily team standup facilitation\ngithub: team-handbook/repos\n---\n\n# Standup\n\nRun a 15-minute standup.",
};

const agentWithSkill: Agent = {
  id: "agent-1",
  workspace_id: WS,
  runtime_id: "rt-1",
  name: "Reviewer Bot",
  description: "",
  instructions: "",
  avatar_url: null,
  runtime_mode: "cloud",
  runtime_config: {},
  custom_args: [],
  visibility: "workspace",
  permission_mode: "public_to",
  invocation_targets: [],
  owner_id: null,
  skills: [{ id: "skill-1", name: "code-review", description: "Reviews PRs" }],
  status: "idle",
  max_concurrent_tasks: 1,
  model: "claude-sonnet",
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
  archived_at: null,
  archived_by: null,
};

afterEach(() => {
  cleanup();
});

describe("SkillProfileCard", () => {
  it("renders the skill name from the API detail (not a UUID)", async () => {
    const ui = renderWithI18n(
      <TestHarness skill={skillWithoutFrontmatter}>
        <SkillProfileCard skillId={skillWithoutFrontmatter.id} />
      </TestHarness>,
    );
    await waitFor(() => {
      expect(screen.getByText("code-review")).toBeInTheDocument();
    });
    expect(ui.container.textContent ?? "").not.toContain(
      skillWithoutFrontmatter.id,
    );
  });

  it("falls back to the prop skillName when the API detail has not resolved", async () => {
    renderWithI18n(
      <TestHarness>
        <SkillProfileCard skillId="skill-x" skillName="Hint Name" />
      </TestHarness>,
    );
    await waitFor(() => {
      expect(screen.getByText("Hint Name")).toBeInTheDocument();
    });
  });

  it("renders the YAML frontmatter when the skill body opens with ---", async () => {
    renderWithI18n(
      <TestHarness skill={skillWithFrontmatter}>
        <SkillProfileCard
          skillId={skillWithFrontmatter.id}
          skillName={skillWithFrontmatter.name}
        />
      </TestHarness>,
    );
    // The skill's top-level description comes from the API and is rendered
    // in the header above; the frontmatter's `description` key is
    // intentionally skipped to avoid duplicating the same value.
    await waitFor(() => {
      expect(screen.getByText("team-standup")).toBeInTheDocument();
    });
    // No duplicate "description" label in the frontmatter dl.
    expect(
      Array.from(screen.queryAllByText("description")).filter(
        (el) => el.tagName === "DT",
      ),
    ).toHaveLength(0);
    // Promoted keys reach the always-visible summary; github is on the
    // allowlist so it renders without expanding.
    expect(screen.getByText("github")).toBeInTheDocument();
    expect(screen.getByText("team-handbook/repos")).toBeInTheDocument();
  });

  it("collapses non-promoted frontmatter behind an expand control", async () => {
    const skillWithMany: Skill = {
      ...skillWithoutFrontmatter,
      content:
        "---\nversion: 1.2.3\nrepository: repo/org\nfree_field_a: a value\nfree_field_b: b value\n---\n\nBody.",
    };
    renderWithI18n(
      <TestHarness skill={skillWithMany}>
        <SkillProfileCard
          skillId={skillWithMany.id}
          skillName={skillWithMany.name}
        />
      </TestHarness>,
    );
    // Two promoted fields (version, repository) render immediately.
    await waitFor(() => {
      expect(screen.getByText("version")).toBeInTheDocument();
    });
    expect(screen.getByText("repository")).toBeInTheDocument();

    // Non-promoted fields are hidden until the user expands.
    expect(screen.queryByText("free_field_a")).toBeNull();
    expect(screen.queryByText("free_field_b")).toBeNull();

    // Expand.
    fireEvent.click(screen.getByRole("button", { name: /show.*more/i }));

    expect(screen.getByText("free_field_a")).toBeInTheDocument();
    expect(screen.getByText("free_field_b")).toBeInTheDocument();
  });

  it("always renders a deep link to the skill detail page", async () => {
    renderWithI18n(
      <TestHarness skill={skillWithoutFrontmatter}>
        <SkillProfileCard skillId={skillWithoutFrontmatter.id} />
      </TestHarness>,
    );
    await waitFor(() => {
      expect(screen.getByText("code-review")).toBeInTheDocument();
    });
    const link = screen.getByRole("link", { name: /view full skill/i });
    expect(link).toHaveAttribute("href", "/acme/skills/skill-1");
  });

  it("lists bound agents", async () => {
    renderWithI18n(
      <TestHarness skill={skillWithoutFrontmatter} agents={[agentWithSkill]}>
        <SkillProfileCard skillId={skillWithoutFrontmatter.id} />
      </TestHarness>,
    );
    await waitFor(() => {
      expect(screen.getByText("Reviewer Bot")).toBeInTheDocument();
    });
  });
});
