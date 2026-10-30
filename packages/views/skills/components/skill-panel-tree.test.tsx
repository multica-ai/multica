// @vitest-environment jsdom

import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Skill, SkillSummary } from "@multica/core/types";
import { skillDetailOptions } from "@multica/core/workspace/queries";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enDocs from "../../locales/en/docs.json";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";
import { SkillPanelTree } from "./skill-panel-tree";

vi.mock("@multica/core/paths", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/paths")>()),
  useWorkspacePaths: () => ({
    skillDetail: (id: string) => `/acme/skills/${id}`,
    skillFile: (id: string, path: string) => `/acme/skills/${id}?file=${encodeURIComponent(path)}`,
  }),
}));

const skills = [
  { id: "s1", name: "review" },
  { id: "s2", name: "deploy" },
] as SkillSummary[];

function renderTree(pathname: string, search = "") {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  queryClient.setQueryData(skillDetailOptions("ws-1", "s1").queryKey, {
    id: "s1",
    name: "review",
    content: "",
    files: [{ path: "templates/pr.md" }, { path: "checklist.md" }],
  } as unknown as Skill);
  const navigation: NavigationAdapter = {
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    pathname,
    searchParams: new URLSearchParams(search),
    hash: "",
    getShareableUrl: (path) => path,
  };
  render(
    <I18nProvider locale="en" resources={{ en: { common: enCommon, docs: enDocs } }}>
      <NavigationProvider value={navigation}>
        <QueryClientProvider client={queryClient}>
          <SkillPanelTree wsId="ws-1" skills={skills} />
        </QueryClientProvider>
      </NavigationProvider>
    </I18nProvider>,
  );
}

describe("SkillPanelTree", () => {
  it("expands the open skill and marks SKILL.md by default", () => {
    renderTree("/acme/skills/s1");
    const main = screen.getByRole("link", { name: "SKILL.md" });
    expect(main.getAttribute("href")).toBe("/acme/skills/s1");
    expect(main.getAttribute("aria-current")).toBe("page");
    expect(screen.getByRole("link", { name: "checklist.md" }).getAttribute("href")).toBe(
      "/acme/skills/s1?file=checklist.md",
    );
    // Other skills stay collapsed until asked.
    expect(screen.getByRole("link", { name: "deploy" }).getAttribute("aria-current")).toBeNull();
  });

  it("opens the folders holding the open file and marks it", () => {
    renderTree("/acme/skills/s1", "file=templates%2Fpr.md");
    const file = screen.getByRole("link", { name: "pr.md" });
    expect(file.getAttribute("aria-current")).toBe("page");
    expect(screen.getByRole("link", { name: "SKILL.md" }).getAttribute("aria-current")).toBeNull();
  });

  it("collapses and expands folders inside a skill", async () => {
    renderTree("/acme/skills/s1");
    expect(screen.queryByRole("link", { name: "pr.md" })).toBeNull();
    await userEvent.click(screen.getByRole("button", { name: "Show contents of templates" }));
    expect(screen.getByRole("link", { name: "pr.md" })).toBeTruthy();
  });
});
