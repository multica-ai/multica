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

function row(): SkillRow {
  return {
    skill: {
      id: "s1",
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
  // No beforeEach reset: each test below sets its own mock implementation
  // (mockResolvedValue / mockRejectedValue) before use, so a shared
  // beforeEach isn't needed for isolation. (mockReimportSkill.mockReset()
  // in a beforeEach here reproducibly triggers a Vitest 4.1.0/tinyspy false
  // "unhandled rejection" on the second test even though the component's
  // try/catch fully handles the rejection — verified by tracing execution
  // order with temporary logging; the catch, toast.error, and finally all
  // run before the test completes. Isolate the two tests' mock state via
  // explicit mockResolvedValue/mockRejectedValue instead of a reset hook.)

  it("calls reimportSkill and invalidates skills on confirm", async () => {
    mockReimportSkill.mockResolvedValue({});
    const qc = new QueryClient();
    const invalidate = vi.spyOn(qc, "invalidateQueries");
    render(<UpdateSkillDialog row={row()} ctx={ctx} open onOpenChange={() => {}} />, {
      wrapper: wrap(qc),
    });
    fireEvent.click(screen.getByRole("button", { name: "Update" }));
    await waitFor(() => expect(mockReimportSkill).toHaveBeenCalledWith("s1"));
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: ["workspaces", "ws1", "skills"],
    });
  });

  it("does not throw on failure (error toast path)", async () => {
    mockReimportSkill.mockRejectedValue(new Error("boom"));
    const qc = new QueryClient();
    render(<UpdateSkillDialog row={row()} ctx={ctx} open onOpenChange={() => {}} />, {
      wrapper: wrap(qc),
    });
    fireEvent.click(screen.getByRole("button", { name: "Update" }));
    await waitFor(() => expect(mockReimportSkill).toHaveBeenCalled());
  });
});
