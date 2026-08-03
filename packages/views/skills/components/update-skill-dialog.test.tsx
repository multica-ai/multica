// @vitest-environment jsdom
import { describe, it, expect, vi } from "vitest";
import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enSkills from "../../locales/en/skills.json";
import type { SkillRow } from "./skills-page";

const mockReimportSkill = vi.hoisted(() => vi.fn());
vi.mock("@multica/core/api", () => ({
  api: { reimportSkill: (...a: unknown[]) => mockReimportSkill(...a) },
}));

const mockToast = vi.hoisted(() => ({
  success: vi.fn(),
  error: vi.fn(),
  warning: vi.fn(),
}));
vi.mock("sonner", () => ({ toast: mockToast }));

import { UpdateSkillDialog } from "./skill-list-actions";

const TEST_RESOURCES = { en: { common: enCommon, skills: enSkills } };

function wrap(qc: QueryClient) {
  function Wrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={qc}>
        <I18nProvider locale="en" resources={TEST_RESOURCES}>
          {children}
        </I18nProvider>
      </QueryClientProvider>
    );
  }
  return Wrapper;
}

function row(id = "s1"): SkillRow {
  return {
    skill: {
      id,
      workspace_id: "ws1",
      name: "My Skill",
      description: "",
      config: { origin: { type: "github", source_url: "https://github.com/a/b" } },
      created_by: "u1",
      created_at: "",
      updated_at: "",
    },
    agents: [],
    creator: null,
    runtime: null,
    originType: "github",
    canEdit: true,
  } as SkillRow;
}

const ctx = { wsId: "ws1", agents: [], currentUserId: "u1", isAdmin: false };

describe("UpdateSkillDialog", () => {
  // Each test sets its own mock outcome (mockResolvedValue / mockRejectedValue),
  // so no shared reset hook is needed.

  it("calls reimportSkill and invalidates skills on confirm", async () => {
    mockReimportSkill.mockResolvedValue({});
    const qc = new QueryClient();
    const invalidate = vi.spyOn(qc, "invalidateQueries");
    render(<UpdateSkillDialog rows={[row()]} ctx={ctx} open onOpenChange={() => {}} />, {
      wrapper: wrap(qc),
    });
    fireEvent.click(screen.getByRole("button", { name: "Update" }));
    await waitFor(() => expect(mockReimportSkill).toHaveBeenCalledWith("s1"));
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: ["workspaces", "ws1", "skills"],
    });
  });

  it("re-imports every selected skill on batch confirm", async () => {
    mockReimportSkill.mockClear();
    mockReimportSkill.mockResolvedValue({});
    const qc = new QueryClient();
    render(
      <UpdateSkillDialog
        rows={[row("s1"), row("s2")]}
        ctx={ctx}
        open
        onOpenChange={() => {}}
      />,
      { wrapper: wrap(qc) },
    );
    fireEvent.click(screen.getByRole("button", { name: "Update" }));
    await waitFor(() => expect(mockReimportSkill).toHaveBeenCalledTimes(2));
    expect(mockReimportSkill).toHaveBeenCalledWith("s1");
    expect(mockReimportSkill).toHaveBeenCalledWith("s2");
  });

  it("does not throw on failure (error toast path)", async () => {
    mockReimportSkill.mockRejectedValue(new Error("boom"));
    const qc = new QueryClient();
    render(<UpdateSkillDialog rows={[row()]} ctx={ctx} open onOpenChange={() => {}} />, {
      wrapper: wrap(qc),
    });
    fireEvent.click(screen.getByRole("button", { name: "Update" }));
    await waitFor(() => expect(mockReimportSkill).toHaveBeenCalled());
  });

  // When the second skill fails, the server has already overwritten the first.
  // The batch must keep going, refresh the caches, and report what landed.
  it("reports a partial result when one skill in a batch fails", async () => {
    mockReimportSkill.mockClear();
    mockToast.warning.mockClear();
    mockReimportSkill.mockImplementation((id: string) =>
      id === "s2" ? Promise.reject(new Error("boom")) : Promise.resolve({}),
    );
    const qc = new QueryClient();
    const invalidate = vi.spyOn(qc, "invalidateQueries");
    const onOpenChange = vi.fn();
    const onUpdated = vi.fn();
    render(
      <UpdateSkillDialog
        rows={[row("s1"), row("s2")]}
        ctx={ctx}
        open
        onOpenChange={onOpenChange}
        onUpdated={onUpdated}
      />,
      { wrapper: wrap(qc) },
    );
    fireEvent.click(screen.getByRole("button", { name: "Update" }));

    await waitFor(() => expect(mockToast.warning).toHaveBeenCalled());
    expect(mockReimportSkill).toHaveBeenCalledTimes(2);
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: ["workspaces", "ws1", "skills"],
    });
    expect(mockToast.warning).toHaveBeenCalledWith(
      "Updated 1 of 2 skills from source; 1 failed",
    );
    expect(onOpenChange).toHaveBeenCalledWith(false);
    // Selection survives a partial failure so the user can retry.
    expect(onUpdated).not.toHaveBeenCalled();
  });

  it("does not invalidate when every skill in a batch fails", async () => {
    mockReimportSkill.mockClear();
    mockToast.error.mockClear();
    mockReimportSkill.mockRejectedValue(new Error("boom"));
    const qc = new QueryClient();
    const invalidate = vi.spyOn(qc, "invalidateQueries");
    render(
      <UpdateSkillDialog
        rows={[row("s1"), row("s2")]}
        ctx={ctx}
        open
        onOpenChange={() => {}}
      />,
      { wrapper: wrap(qc) },
    );
    fireEvent.click(screen.getByRole("button", { name: "Update" }));

    await waitFor(() => expect(mockToast.error).toHaveBeenCalledWith("boom"));
    expect(invalidate).not.toHaveBeenCalled();
  });
});
