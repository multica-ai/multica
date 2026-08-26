// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { SkillSummary } from "@multica/core/types";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enSkills from "../../locales/en/skills.json";
import type { SkillRow } from "./skills-page";
import type { SkillActionsContext } from "./skill-list-actions";

const TEST_RESOURCES = { en: { common: enCommon, skills: enSkills } };

const downloadSkillArchive = vi.hoisted(() => vi.fn());
vi.mock("../lib/export-skill", () => ({ downloadSkillArchive }));

const intentNavigate = vi.hoisted(() => vi.fn());
vi.mock("../../navigation", () => ({
  useNavigation: () => ({
    push: vi.fn(),
    openInNewTab: vi.fn(),
    getShareableUrl: (p: string) => p,
  }),
  useIntentNavigate: () => intentNavigate,
}));

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({ skillDetail: (id: string) => `/acme/skills/${id}` }),
}));

vi.mock("@multica/core/api", () => ({ api: {} }));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

import { SkillRowActions } from "./skill-list-actions";

function makeRow(id: string, over: Partial<SkillSummary> = {}): SkillRow {
  const skill: SkillSummary = {
    id,
    workspace_id: "ws-1",
    name: `skill-${id}`,
    description: "",
    config: {},
    created_by: "user-1",
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    ...over,
  };
  return {
    skill,
    agents: [],
    creator: null,
    runtime: null,
    originType: "manual",
    canEdit: true,
  };
}

const ctx: SkillActionsContext = {
  wsId: "ws-1",
  agents: [],
  currentUserId: "user-1",
  isAdmin: true,
};

function renderRow(row: SkillRow) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <QueryClientProvider client={queryClient}>
        <SkillRowActions row={row} ctx={ctx} />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

describe("SkillRowActions export", () => {
  beforeEach(() => {
    downloadSkillArchive.mockReset();
    intentNavigate.mockReset();
  });

  it("exports the skill archive from the row kebab menu", async () => {
    const row = makeRow("skill-1");
    renderRow(row);

    await userEvent.click(screen.getByRole("button", { name: "Actions" }));
    await userEvent.click(await screen.findByRole("menuitem", { name: "Export" }));

    expect(downloadSkillArchive).toHaveBeenCalledTimes(1);
    expect(downloadSkillArchive).toHaveBeenCalledWith("skill-1");
  });
});
