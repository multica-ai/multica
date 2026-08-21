// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { SkillSummary } from "@multica/core/types";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enSkills from "../../locales/en/skills.json";
import type { SkillRow } from "./skills-page";
import type { SkillActionsContext } from "./skill-list-actions";

const TEST_RESOURCES = { en: { common: enCommon, skills: enSkills } };

vi.mock("@multica/core/api", () => ({
  api: { refreshSkill: vi.fn() },
}));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

import { api } from "@multica/core/api";
import { toast } from "sonner";
import { UpdateSkillsDialog } from "./skill-list-actions";

const refreshSkill = vi.mocked(api.refreshSkill);

function makeRow(id: string): SkillRow {
  const skill: SkillSummary = {
    id,
    workspace_id: "ws-1",
    name: `skill-${id}`,
    description: "",
    config: {
      origin: { type: "github", source_url: `https://github.com/acme/${id}` },
    },
    created_by: "user-1",
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
  };
  return {
    skill,
    agents: [],
    creator: null,
    runtime: null,
    originType: "github",
    canEdit: true,
  };
}

const ctx: SkillActionsContext = {
  wsId: "ws-1",
  agents: [],
  currentUserId: "user-1",
  isAdmin: true,
};

function renderDialog(
  rows: SkillRow[],
  skippedCount = 0,
  onUpdated?: () => void,
) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <QueryClientProvider client={queryClient}>
        <UpdateSkillsDialog
          rows={rows}
          skippedCount={skippedCount}
          ctx={ctx}
          open
          onOpenChange={() => {}}
          onUpdated={onUpdated}
        />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

beforeEach(() => {
  vi.clearAllMocks();
});

describe("UpdateSkillsDialog", () => {
  it("summarizes the updatable selection and the skipped entries", async () => {
    renderDialog([makeRow("a"), makeRow("b")], 1);

    expect(
      await screen.findByText("2 skills will be updated from their sources"),
    ).toBeTruthy();
    // The never-updatable selection entries passed via skippedCount.
    expect(
      screen.getByText("1 can't be updated from a source — skipped"),
    ).toBeTruthy();
    expect(screen.getByRole("button", { name: /Update 2/ })).toBeTruthy();
  });

  it("omits the skipped line when the whole selection is updatable", async () => {
    renderDialog([makeRow("a")]);

    expect(
      await screen.findByText("1 skill will be updated from its source"),
    ).toBeTruthy();
    expect(screen.queryByText(/skipped/)).toBeNull();
  });

  it("refreshes every updatable skill and clears the selection on success", async () => {
    refreshSkill.mockResolvedValue({} as never);
    const onUpdated = vi.fn();
    renderDialog([makeRow("a"), makeRow("b")], 0, onUpdated);

    await userEvent.click(
      await screen.findByRole("button", { name: /Update 2/ }),
    );

    await waitFor(() => expect(onUpdated).toHaveBeenCalled());
    expect(refreshSkill.mock.calls.map(([id]) => id)).toEqual(["a", "b"]);
    expect(toast.success).toHaveBeenCalled();
  });

  it("continues past a failing item and reports a partial toast", async () => {
    refreshSkill.mockImplementation((id: string) =>
      id === "a"
        ? Promise.reject(new Error("name conflict"))
        : Promise.resolve({} as never),
    );
    const onUpdated = vi.fn();
    renderDialog([makeRow("a"), makeRow("b")], 0, onUpdated);

    await userEvent.click(
      await screen.findByRole("button", { name: /Update 2/ }),
    );

    await waitFor(() => expect(toast.error).toHaveBeenCalled());
    // The failure on "a" must not strand "b".
    expect(refreshSkill.mock.calls.map(([id]) => id)).toEqual(["a", "b"]);
    // Partial success keeps the selection: onUpdated only fires on a clean run.
    expect(onUpdated).not.toHaveBeenCalled();
  });
});
