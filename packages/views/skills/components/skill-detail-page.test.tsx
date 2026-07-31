// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Skill } from "@multica/core/types";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enSkills from "../../locales/en/skills.json";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";

const TEST_RESOURCES = { en: { common: enCommon, skills: enSkills } };

const skillRef = vi.hoisted(() => ({ current: null as unknown }));
const agentsRef = vi.hoisted(() => ({ current: [] as unknown[] }));
const membersRef = vi.hoisted(() => ({ current: [] as unknown[] }));
const canEditRef = vi.hoisted(() => ({ current: true }));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/workspace/queries", () => ({
  skillDetailOptions: (wsId: string, id: string) => ({
    queryKey: ["skill", wsId, id],
    queryFn: () => Promise.resolve(skillRef.current),
  }),
  agentListOptions: (wsId: string) => ({
    queryKey: ["agents", wsId],
    queryFn: () => Promise.resolve(agentsRef.current),
  }),
  memberListOptions: (wsId: string) => ({
    queryKey: ["members", wsId],
    queryFn: () => Promise.resolve(membersRef.current),
  }),
  selectSkillAssignments: () => new Map(),
  workspaceKeys: {
    skills: (wsId: string) => ["skills", wsId],
    agents: (wsId: string) => ["agents", wsId],
  },
}));
vi.mock("@multica/core/runtimes", () => ({
  runtimeListOptions: (wsId: string) => ({
    queryKey: ["runtimes", wsId],
    queryFn: () => Promise.resolve([]),
  }),
  runtimeDisplayLabel: (r: { name?: string }) => r.name ?? "runtime",
}));
vi.mock("@multica/core/auth", () => {
  const state = () => ({ user: { id: "user-1" } });
  return {
    useAuthStore: Object.assign(
      (selector?: (s: ReturnType<typeof state>) => unknown) =>
        selector ? selector(state()) : state(),
      { getState: state },
    ),
  };
});
// Partial: `WORKSPACE_PAGES` also comes from here and backs the shared
// SkillIcon, so the real module has to stay reachable.
vi.mock("@multica/core/paths", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/paths")>()),
  useWorkspacePaths: () => ({ skills: () => "/acme/skills" }),
}));
vi.mock("@multica/core/permissions", () => ({
  useSkillPermissions: () => ({ canEdit: { allowed: true, reason: null } }),
}));
vi.mock("@multica/core/workspace/avatar-url", () => ({
  resolvePublicFileUrl: (v: string | null) => v,
}));
vi.mock("@multica/core/api", () => ({
  api: { updateSkill: vi.fn(), deleteSkill: vi.fn() },
}));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("../hooks/use-can-edit-skill", () => ({
  useCanEditSkill: () => canEditRef.current,
}));

// Heavy leaves that carry no behaviour under test.
vi.mock("../../rich-content", () => ({
  RichContent: ({ content }: { content: string }) => (
    <div data-testid="preview">{content}</div>
  ),
}));
vi.mock("../../labels/resource-label-picker", () => ({
  ResourceLabelPicker: () => <div data-testid="labels" />,
  // The row is only laid out when the release flag is on; with it off the
  // picker renders nothing and the label would sit above an empty field.
  useResourceLabelsEnabled: () => true,
}));
vi.mock("./skill-list-actions", () => ({ AddToAgentDialog: () => null }));
vi.mock("@multica/ui/components/common/capability-banner", () => ({
  CapabilityBanner: () => <div data-testid="capability-banner" />,
}));
vi.mock("@multica/ui/components/common/actor-avatar", () => ({
  ActorAvatar: () => <div />,
}));

import { SkillDetailPage } from "./skill-detail-page";

const LONG_DESCRIPTION =
  "Animation for product interfaces — when to animate and when not to. " +
  "Triggers on: animation, easing, cubic-bezier, spring, keyframes.";

const baseSkill: Skill = {
  id: "skill-1",
  workspace_id: "ws-1",
  name: "aiforui-animations",
  description: LONG_DESCRIPTION,
  config: {},
  created_by: "user-1",
  created_at: "2026-07-28T18:11:37Z",
  updated_at: "2026-07-28T18:14:40Z",
  content: "---\nname: aiforui-animations\n---\n\n# Interface Animations\n",
  files: [
    { id: "f-1", path: "patterns.md", content: "# Patterns\n" } as Skill["files"][number],
  ],
};

function renderPage(searchParams = new URLSearchParams()) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const replace = vi.fn();
  const navigation: NavigationAdapter = {
    push: vi.fn(),
    replace,
    back: vi.fn(),
    pathname: "/acme/skills/skill-1",
    searchParams,
    getShareableUrl: (path) => path,
  };
  render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <NavigationProvider value={navigation}>
        <QueryClientProvider client={queryClient}>
          <SkillDetailPage skillId="skill-1" />
        </QueryClientProvider>
      </NavigationProvider>
    </I18nProvider>,
  );
  return { replace };
}

beforeEach(() => {
  vi.clearAllMocks();
  skillRef.current = baseSkill;
  agentsRef.current = [];
  membersRef.current = [{ user_id: "user-1", role: "member" }];
  canEditRef.current = true;
});

describe("SkillDetailPage tabs", () => {
  it("opens on Overview and exposes exactly two tabs", async () => {
    renderPage();
    const tabs = await screen.findAllByRole("tab", { name: /Overview|Files/ });
    expect(tabs.map((t) => t.textContent)).toEqual(["Overview", "Files 2"]);
    // A Settings tab would only hold a delete button and a read-only
    // sentence — the skill update payload has no settings-shaped fields.
    expect(screen.queryByRole("tab", { name: "Settings" })).toBeNull();
    expect(
      screen.getByRole("tab", { name: "Overview" }).getAttribute("aria-selected"),
    ).toBe("true");
  });

  it("mirrors the active tab into ?view= so the pane survives a reload", async () => {
    const { replace } = renderPage();
    fireEvent.click(await screen.findByRole("tab", { name: "Files 2" }));
    expect(replace).toHaveBeenCalledWith("/acme/skills/skill-1?view=files");
  });

  it("restores the Files tab from ?view=files", async () => {
    renderPage(new URLSearchParams("view=files"));
    expect(
      (await screen.findByRole("tab", { name: "Files 2" })).getAttribute(
        "aria-selected",
      ),
    ).toBe("true");
  });
});

describe("SkillDetailPage file mode", () => {
  it("keeps plain-text mode when switching files", async () => {
    renderPage(new URLSearchParams("view=files"));

    fireEvent.click(await screen.findByRole("button", { name: "Plain text" }));
    expect(screen.getByRole("textbox", { name: /SKILL\.md/ })).toBeTruthy();

    // Mode used to live inside FileViewer, which the per-path `key`
    // remounted — selecting another file silently snapped back to preview.
    fireEvent.click(screen.getByRole("tab", { name: "patterns.md" }));
    expect(screen.getByRole("textbox", { name: /patterns\.md/ })).toBeTruthy();
    expect(screen.queryByTestId("preview")).toBeNull();
  });

  it("strips frontmatter from the preview so name/description are not shown twice", async () => {
    renderPage(new URLSearchParams("view=files"));
    const preview = await screen.findByTestId("preview");
    expect(preview.textContent).toContain("# Interface Animations");
    expect(preview.textContent).not.toContain("name: aiforui-animations");
  });
});

describe("SkillDetailPage save pill", () => {
  it("is absent while clean and floats in with a change summary once dirty", async () => {
    renderPage();
    await screen.findByRole("tab", { name: "Overview" });
    expect(screen.queryByRole("button", { name: /Save changes/ })).toBeNull();

    fireEvent.change(screen.getByLabelText("Description"), {
      target: { value: "changed" },
    });
    expect(screen.getByText("Changed: Description")).toBeTruthy();
    expect(
      screen.getByRole("button", { name: /Save changes/ }).hasAttribute("disabled"),
    ).toBe(false);
  });

  it("lists every dirty part in the summary, reusing the field labels", async () => {
    renderPage();
    fireEvent.change(await screen.findByLabelText("Name"), {
      target: { value: "renamed-skill" },
    });
    fireEvent.change(screen.getByLabelText("Description"), {
      target: { value: "changed" },
    });
    expect(screen.getByText("Changed: Name · Description")).toBeTruthy();
  });

  it("counts a supporting-file edit as one changed file", async () => {
    renderPage(new URLSearchParams("view=files"));
    fireEvent.click(await screen.findByRole("tab", { name: "patterns.md" }));
    fireEvent.click(screen.getByRole("button", { name: "Plain text" }));
    fireEvent.change(screen.getByRole("textbox", { name: /patterns\.md/ }), {
      target: { value: "edited content" },
    });
    expect(screen.getByText("Changed: 1 file")).toBeTruthy();
  });

  it("is absent for read-only viewers, who get the capability banner instead", async () => {
    canEditRef.current = false;
    renderPage();
    expect(await screen.findByTestId("capability-banner")).toBeTruthy();
    expect(screen.queryByRole("button", { name: /Save changes/ })).toBeNull();
  });
});

describe("SkillDetailPage properties", () => {
  it("renders the whole description in an editable field sized for it", async () => {
    renderPage();
    const field = (await screen.findByLabelText("Description")) as HTMLTextAreaElement;
    expect(field.value).toBe(LONG_DESCRIPTION);
    expect(Number(field.rows)).toBeGreaterThanOrEqual(4);
    expect(
      screen.getByText(`${LONG_DESCRIPTION.length} characters.`, { exact: false }),
    ).toBeTruthy();
  });
});
