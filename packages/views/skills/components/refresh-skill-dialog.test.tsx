// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { SkillSummary } from "@multica/core/types";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enSkills from "../../locales/en/skills.json";
import type { OriginInfo } from "../lib/origin";

const TEST_RESOURCES = { en: { common: enCommon, skills: enSkills } };

vi.mock("@multica/core/api", () => ({
  api: { refreshSkill: vi.fn() },
}));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

import { RefreshSkillDialog } from "./refresh-skill-dialog";

const skill: SkillSummary = {
  id: "skill-1",
  workspace_id: "ws-1",
  name: "animations",
  description: "",
  config: {},
  created_by: "user-1",
  created_at: "2026-07-28T18:11:37Z",
  updated_at: "2026-07-28T18:14:40Z",
};

function renderDialog(origin: OriginInfo | null) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <QueryClientProvider client={queryClient}>
        <RefreshSkillDialog
          skill={skill}
          origin={origin}
          wsId="ws-1"
          open
          onOpenChange={() => {}}
        />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

beforeEach(() => {
  vi.clearAllMocks();
});

describe("RefreshSkillDialog source URL", () => {
  const SOURCE_URL = "https://github.com/anthropics/skills/tree/main/animations";

  it("shows the source URL as an external link", async () => {
    renderDialog({ type: "github", source_url: SOURCE_URL });
    const link = (await screen.findByRole("link")) as HTMLAnchorElement;
    expect(link.getAttribute("href")).toBe(SOURCE_URL);
    expect(link.getAttribute("target")).toBe("_blank");
    expect(link.getAttribute("rel")).toContain("noopener");
  });

  it("middle-truncates a long source URL but links the full one", async () => {
    const longUrl =
      "https://github.com/anthropics/a-very-long-repository-name/tree/main/deeply/nested/skill-directory";
    renderDialog({ type: "github", source_url: longUrl });
    const link = (await screen.findByRole("link")) as HTMLAnchorElement;
    expect(link.getAttribute("href")).toBe(longUrl);
    expect(link.textContent).toContain("…");
    expect((link.textContent ?? "").length).toBeLessThanOrEqual(60);
  });

  // Which source_urls are linkable is originSourceUrl's contract, and its full
  // matrix — host mismatch, lookalike hosts, non-http schemes, malformed and
  // missing values, non-import origins — lives in ../lib/origin.test.ts. This
  // case pins the wiring instead: when the helper refuses, the dialog renders
  // no anchor at all. config.origin is persisted verbatim through the skill
  // update API, so an injected source_url is the rejection that matters most.
  it("renders no link when the helper refuses the source_url", () => {
    renderDialog({ type: "manual", source_url: SOURCE_URL });
    expect(screen.queryByRole("link")).toBeNull();
  });
});
