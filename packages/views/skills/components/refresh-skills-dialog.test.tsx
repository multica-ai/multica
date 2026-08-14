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

const TEST_RESOURCES = { en: { common: enCommon, skills: enSkills } };

const refreshSkill = vi.hoisted(() => vi.fn());
vi.mock("@multica/core/api", () => ({ api: { refreshSkill } }));
vi.mock("@multica/core/workspace/queries", () => ({
  workspaceKeys: {
    skills: (wsId: string) => ["skills", wsId],
    agents: (wsId: string) => ["agents", wsId],
  },
}));
vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({ skillDetail: (id: string) => `/acme/skills/${id}` }),
}));
vi.mock("@multica/core/workspace/avatar-url", () => ({
  resolvePublicFileUrl: (v: string | null) => v,
}));
vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn(), warning: vi.fn() },
}));

import { toast } from "sonner";
import { RefreshSkillsDialog } from "./skill-list-actions";

function makeRow(id: string): SkillRow {
  return {
    skill: {
      id,
      workspace_id: "ws-1",
      name: `skill-${id}`,
      description: "",
      config: {
        origin: { type: "clawhub", source_url: `https://clawhub.ai/acme/${id}` },
      },
      created_by: "user-1",
      created_at: "2026-08-01T00:00:00Z",
      updated_at: "2026-08-01T00:00:00Z",
    } as unknown as SkillSummary,
    agents: [],
    creator: null,
    runtime: null,
    originType: "clawhub",
    canEdit: true,
  };
}

function renderDialog(rows: SkillRow[], onRefreshed = vi.fn()) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const invalidate = vi.spyOn(qc, "invalidateQueries");
  render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <QueryClientProvider client={qc}>
        <RefreshSkillsDialog
          rows={rows}
          ctx={{ wsId: "ws-1", agents: [], currentUserId: "user-1", isAdmin: true }}
          open
          onOpenChange={vi.fn()}
          onRefreshed={onRefreshed}
        />
      </QueryClientProvider>
    </I18nProvider>,
  );
  return { invalidate, onRefreshed };
}

const confirm = async () => {
  await userEvent.click(screen.getByRole("button", { name: "Update" }));
};

describe("RefreshSkillsDialog", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    refreshSkill.mockReset();
  });

  it("refreshes every selected skill and invalidates the caches", async () => {
    refreshSkill.mockResolvedValue({ id: "ok" });
    const rows = [makeRow("a"), makeRow("b"), makeRow("c")];
    const { invalidate, onRefreshed } = renderDialog(rows);

    await confirm();

    expect(refreshSkill).toHaveBeenCalledTimes(3);
    expect(invalidate).toHaveBeenCalledWith({ queryKey: ["skills", "ws-1"] });
    expect(invalidate).toHaveBeenCalledWith({ queryKey: ["agents", "ws-1"] });
    expect(toast.success).toHaveBeenCalled();
    // A clean run clears the selection.
    expect(onRefreshed).toHaveBeenCalled();
  });

  // The core of the batch contract: the first skill is already overwritten
  // server-side, so reporting only the failure would read as "nothing
  // happened" while its local edits are gone.
  it("reports a partial result and still invalidates when one row fails", async () => {
    refreshSkill
      .mockResolvedValueOnce({ id: "a" })
      .mockRejectedValueOnce(new Error("upstream gone"));
    const { invalidate, onRefreshed } = renderDialog([makeRow("a"), makeRow("b")]);

    await confirm();

    expect(invalidate).toHaveBeenCalledWith({ queryKey: ["skills", "ws-1"] });
    expect(toast.warning).toHaveBeenCalledWith(
      "Updated 1 of 2 skills; 1 failed",
    );
    expect(toast.success).not.toHaveBeenCalled();
    // The selection survives a partial failure so the user can retry.
    expect(onRefreshed).not.toHaveBeenCalled();
  });

  // An early rejection must not abort the rows queued behind it, including
  // rows in later concurrency chunks.
  it("processes every row across chunk boundaries despite an early failure", async () => {
    refreshSkill.mockImplementation((id: string) =>
      id === "s0" ? Promise.reject(new Error("nope")) : Promise.resolve({ id }),
    );
    const rows = Array.from({ length: 6 }, (_, i) => makeRow(`s${i}`));
    renderDialog(rows);

    await confirm();

    expect(refreshSkill).toHaveBeenCalledTimes(6);
    expect(toast.warning).toHaveBeenCalledWith(
      "Updated 5 of 6 skills; 1 failed",
    );
  });

  it("does not invalidate when every row fails", async () => {
    refreshSkill.mockRejectedValue(new Error("upstream gone"));
    const { invalidate, onRefreshed } = renderDialog([makeRow("a"), makeRow("b")]);

    await confirm();

    expect(invalidate).not.toHaveBeenCalled();
    expect(toast.error).toHaveBeenCalledWith("upstream gone");
    expect(onRefreshed).not.toHaveBeenCalled();
  });

  it("keeps the single-row copy when only one skill is selected", async () => {
    refreshSkill.mockResolvedValue({ id: "a" });
    renderDialog([makeRow("a")]);

    expect(screen.getByText("Update from source?")).toBeTruthy();

    await confirm();

    expect(toast.success).toHaveBeenCalledWith("Skill updated from ClawHub");
  });
});
