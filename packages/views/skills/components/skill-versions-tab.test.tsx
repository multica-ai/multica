// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { MemberWithUser, Skill, SkillVersion, SkillVersionSummary } from "@multica/core/types";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enSkills from "../../locales/en/skills.json";

const TEST_RESOURCES = { en: { common: enCommon, skills: enSkills } };

const apiMock = vi.hoisted(() => ({
  listSkillVersions: vi.fn(),
  getSkillVersion: vi.fn(),
  restoreSkillVersion: vi.fn(),
}));
vi.mock("@multica/core/api", () => ({ api: apiMock }));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

import { SkillVersionsTab } from "./skill-versions-tab";

const skill: Skill = {
  id: "skill-1",
  workspace_id: "ws-1",
  name: "pr-review",
  description: "",
  config: {},
  created_by: "user-1",
  created_at: "2026-10-01T00:00:00Z",
  updated_at: "2026-10-02T00:00:00Z",
  content: "current body",
  files: [],
};

const members = [{ user_id: "user-1", name: "Bohan" }] as MemberWithUser[];

function summary(version: number, extra: Partial<SkillVersionSummary> = {}): SkillVersionSummary {
  return {
    id: `v-${version}`,
    skill_id: skill.id,
    version,
    name: skill.name,
    source: "update",
    restored_from_version: null,
    created_by: "user-1",
    created_at: "2026-10-02T00:00:00Z",
    ...extra,
  };
}

function snapshot(version: number, content: string): SkillVersion {
  return {
    ...summary(version),
    description: "",
    content,
    files: [{ path: "notes.md", content: `notes ${version}` }],
  };
}

function renderTab(props: { canEdit?: boolean; hasUnsavedChanges?: boolean; onRestored?: (s: Skill) => void } = {}) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <QueryClientProvider client={queryClient}>
        <SkillVersionsTab
          skill={skill}
          wsId="ws-1"
          canEdit={props.canEdit ?? true}
          hasUnsavedChanges={props.hasUnsavedChanges ?? false}
          members={members}
          onRestored={props.onRestored ?? (() => {})}
        />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  apiMock.listSkillVersions.mockResolvedValue([
    summary(2),
    summary(1, { source: "create" }),
  ]);
  apiMock.getSkillVersion.mockImplementation(async (_id: string, v: number) =>
    snapshot(v, `body ${v}`),
  );
});

describe("SkillVersionsTab", () => {
  it("lists versions newest first and opens the current one", async () => {
    renderTab();
    const tabs = await screen.findAllByRole("tab", { name: /^v\d/ });
    expect(tabs.map((t) => t.textContent)).toEqual([
      expect.stringContaining("v2"),
      expect.stringContaining("v1"),
    ]);
    expect(tabs[0]?.textContent).toContain("Current");
    expect(tabs[1]?.textContent).toContain("Created · Bohan");
    await waitFor(() => expect(apiMock.getSkillVersion).toHaveBeenCalledWith("skill-1", 2));
    // The current version cannot be restored onto itself.
    expect(screen.queryByRole("button", { name: "Restore" })).toBeNull();
  });

  it("restores an older version after confirmation", async () => {
    const user = userEvent.setup();
    const restored = { ...skill, content: "body 1" };
    apiMock.restoreSkillVersion.mockResolvedValue(restored);
    const onRestored = vi.fn();
    renderTab({ hasUnsavedChanges: true, onRestored });

    await user.click(await screen.findByRole("tab", { name: /^v1/ }));
    await waitFor(() => expect(apiMock.getSkillVersion).toHaveBeenCalledWith("skill-1", 1));
    await user.click(await screen.findByRole("button", { name: "Restore" }));

    expect(await screen.findByText("Restore v1?")).toBeTruthy();
    expect(screen.getByText("Your unsaved changes will be discarded.")).toBeTruthy();
    const dialogRestore = screen
      .getAllByRole("button", { name: "Restore" })
      .find((b) => b.closest("[role='dialog']"));
    await user.click(dialogRestore!);

    await waitFor(() => expect(apiMock.restoreSkillVersion).toHaveBeenCalledWith("skill-1", 1));
    await waitFor(() => expect(onRestored).toHaveBeenCalledWith(restored));
  });

  it("offers no restore to read-only viewers", async () => {
    const user = userEvent.setup();
    renderTab({ canEdit: false });
    await user.click(await screen.findByRole("tab", { name: /^v1/ }));
    await waitFor(() => expect(apiMock.getSkillVersion).toHaveBeenCalledWith("skill-1", 1));
    expect(screen.queryByRole("button", { name: "Restore" })).toBeNull();
  });

  it("explains an empty history", async () => {
    apiMock.listSkillVersions.mockResolvedValue([]);
    renderTab();
    expect(await screen.findByText(/No versions yet/)).toBeTruthy();
  });
});
