// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
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
  return { replace, queryClient };
}

/** Publishes a new server version of the skill, as a `skill:updated` event would. */
async function remoteUpdate(queryClient: QueryClient, next: Partial<Skill>) {
  skillRef.current = {
    ...(skillRef.current as Skill),
    updated_at: "2026-07-29T10:00:00Z",
    ...next,
  };
  await queryClient.invalidateQueries({ queryKey: ["skill", "ws-1", "skill-1"] });
  await new Promise((resolve) => setTimeout(resolve, 0));
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

  it("shows resource labels in Overview without a release flag", async () => {
    renderPage();
    expect(await screen.findByTestId("labels")).toBeTruthy();
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

    fireEvent.click(await screen.findByRole("button", { name: "Edit" }));
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

describe("SkillDetailPage edit action (MUL-5654)", () => {
  /** Opens a file row's action menu the way the rail exposes it. */
  async function openRowMenu(path: string | RegExp) {
    // The file-name button carries role="tab", so a "button" match on the row
    // is the trailing "..." trigger.
    await userEvent.click(await screen.findByRole("button", { name: path }));
  }

  it("opens a supporting file in a focused editor", async () => {
    renderPage(new URLSearchParams("view=files"));

    await openRowMenu(/patterns\.md/);
    await userEvent.click(
      await screen.findByRole("menuitem", { name: "Edit" }),
    );

    // One gesture owes all three: the file is open, the pane is the editor
    // rather than the preview, and the caret is already in it.
    const editor = screen.getByRole("textbox", { name: /patterns\.md/ });
    expect(screen.queryByTestId("preview")).toBeNull();
    expect(document.activeElement).toBe(editor);
  });

  it("focuses the editor for the file that is already open", async () => {
    renderPage(new URLSearchParams("view=files"));

    // SKILL.md opens selected, so this path mounts nothing new. A mount-only
    // autoFocus would silently do nothing here.
    await openRowMenu(/SKILL\.md/);
    await userEvent.click(
      await screen.findByRole("menuitem", { name: "Edit" }),
    );

    expect(document.activeElement).toBe(
      screen.getByRole("textbox", { name: /SKILL\.md/ }),
    );
  });

  it("leaves the caret at the top rather than the end of the file", async () => {
    renderPage(new URLSearchParams("view=files"));

    await openRowMenu(/patterns\.md/);
    await userEvent.click(
      await screen.findByRole("menuitem", { name: "Edit" }),
    );

    const editor = screen.getByRole("textbox", {
      name: /patterns\.md/,
    }) as HTMLTextAreaElement;
    expect([editor.selectionStart, editor.selectionEnd]).toEqual([0, 0]);
  });

  it("offers read-only viewers a plain-text view, not an edit they cannot make", async () => {
    canEditRef.current = false;
    renderPage(new URLSearchParams("view=files"));

    expect(await screen.findByRole("button", { name: "Plain text" })).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Edit" })).toBeNull();
    // No row menus either — the tree offers no action the page would refuse.
    expect(screen.queryByRole("button", { name: /Actions for/ })).toBeNull();
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
    fireEvent.click(screen.getByRole("button", { name: "Edit" }));
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

/**
 * MUL-5645. Dirty state is measured against the seeded baseline, not against
 * the latest server skill. The two failures that rule prevents:
 *
 * 1. A description carrying trailing whitespace — what `description: |`
 *    frontmatter yields, so every imported skill — used to compare unequal to
 *    itself because only one side of the check was trimmed. The page opened
 *    permanently dirty and Discard reseeded the same value, so it never cleared.
 * 2. A remote update read as a local edit, because the check compared the draft
 *    against the NEW server skill. Any agent edit froze the editor on stale
 *    text behind a conflict banner, whatever the description looked like.
 */
describe("SkillDetailPage draft baseline (MUL-5645)", () => {
  const CONFLICT_BANNER = "Someone else updated this skill";

  it("opens clean when the description carries a trailing newline", async () => {
    skillRef.current = { ...baseSkill, description: `${LONG_DESCRIPTION}\n` };
    renderPage();
    await screen.findAllByRole("tab", { name: /Overview|Files/ });
    expect(screen.queryByText(/^Changed:/)).toBeNull();
  });

  it("stays clean after Discard on a trailing-newline description", async () => {
    skillRef.current = { ...baseSkill, description: `${LONG_DESCRIPTION}\n` };
    renderPage();
    const field = (await screen.findByLabelText(
      "Description",
    )) as HTMLTextAreaElement;
    fireEvent.change(field, { target: { value: "edited" } });
    fireEvent.click(await screen.findByRole("button", { name: "Discard" }));
    expect(screen.queryByText(/^Changed:/)).toBeNull();
  });

  it("pulls a remote edit in silently while the draft is untouched", async () => {
    const { queryClient } = renderPage();
    await screen.findAllByRole("tab", { name: /Overview|Files/ });

    await remoteUpdate(queryClient, { description: "Rewritten by the agent" });

    // The new text reaching the field IS the fix: the old code left the editor
    // frozen on the pre-update value behind a conflict banner.
    expect(await screen.findByDisplayValue("Rewritten by the agent")).toBeTruthy();
    expect(screen.queryByText(CONFLICT_BANNER)).toBeNull();
    expect(screen.queryByText(/^Changed:/)).toBeNull();
  });

  it("pulls a remote SKILL.md edit in silently too", async () => {
    const { queryClient } = renderPage(new URLSearchParams("view=files"));
    await screen.findAllByRole("tab", { name: /Overview|Files/ });

    await remoteUpdate(queryClient, {
      content: `${baseSkill.content}\n## Added remotely\n`,
    });

    const preview = await screen.findByTestId("preview");
    expect(preview.textContent).toContain("Added remotely");
    expect(screen.queryByText(CONFLICT_BANNER)).toBeNull();
    expect(screen.queryByText(/^Changed:/)).toBeNull();
  });

  it("releases the conflict once the user reverts their own edits", async () => {
    const { queryClient } = renderPage();
    const field = (await screen.findByLabelText(
      "Description",
    )) as HTMLTextAreaElement;
    fireEvent.change(field, { target: { value: "my unsaved edit" } });

    await remoteUpdate(queryClient, { description: "Rewritten by the agent" });
    expect(await screen.findByText(CONFLICT_BANNER)).toBeTruthy();

    // Reverting by hand leaves nothing to protect. The save bar is dirty-gated,
    // so if the page held the conflict here the banner would sit above stale
    // text with no Discard left to press — a dead end short of a reload.
    fireEvent.change(field, { target: { value: LONG_DESCRIPTION } });

    expect(await screen.findByDisplayValue("Rewritten by the agent")).toBeTruthy();
    expect(screen.queryByText(CONFLICT_BANNER)).toBeNull();
  });

  it("keeps the draft and warns when a remote edit lands on real local edits", async () => {
    const { queryClient } = renderPage();
    const field = (await screen.findByLabelText(
      "Description",
    )) as HTMLTextAreaElement;
    fireEvent.change(field, { target: { value: "my unsaved edit" } });

    await remoteUpdate(queryClient, { description: "Rewritten by the agent" });

    expect(await screen.findByText(CONFLICT_BANNER)).toBeTruthy();
    expect(
      (screen.getByLabelText("Description") as HTMLTextAreaElement).value,
    ).toBe("my unsaved edit");
  });
});
