// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach } from "vitest";
import { fireEvent, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Skill } from "@multica/core/types";
import { toast } from "sonner";
import { renderWithI18n } from "../../test/i18n";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";

const skillRef = vi.hoisted(() => ({ current: null as unknown }));
const agentsRef = vi.hoisted(() => ({ current: [] as unknown[] }));
const membersRef = vi.hoisted(() => ({ current: [] as unknown[] }));
const canEditRef = vi.hoisted(() => ({ current: true }));
const copyTextMock = vi.hoisted(() => vi.fn(() => Promise.resolve(true)));

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
  useWorkspacePaths: () => ({
    skills: () => "/acme/skills",
    skillDetail: (id: string) => `/acme/skills/${id}`,
  }),
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
vi.mock("@multica/ui/lib/clipboard", () => ({ copyText: copyTextMock }));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("../hooks/use-can-edit-skill", () => ({
  useCanEditSkill: () => canEditRef.current,
}));

// jsdom gives every element a zero-size rect, so a live resize handle claims
// every pointerdown on the page and no menu could open.
vi.mock("react-resizable-panels", () => ({
  useDefaultLayout: () => ({ defaultLayout: undefined, onLayoutChanged: vi.fn() }),
  usePanelRef: () => ({
    current: { isCollapsed: () => false, expand: vi.fn(), collapse: vi.fn() },
  }),
}));
vi.mock("@multica/ui/components/ui/resizable", () => ({
  ResizablePanelGroup: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  ResizablePanel: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  ResizableHandle: () => null,
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
vi.mock("./skill-list-actions", () => ({
  AddToAgentDialog: ({ open }: { open: boolean }) =>
    open ? <div data-testid="add-to-agent-dialog" /> : null,
}));
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

const SOURCE_URL = "https://github.com/anthropics/skills/tree/main/animations";

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
  const push = vi.fn();
  const navigation: NavigationAdapter = {
    push,
    replace: vi.fn(),
    back: vi.fn(),
    pathname: "/acme/skills/skill-1",
    searchParams,
    hash: "",
    getShareableUrl: (path) => `https://app.test${path}`,
  };
  renderWithI18n(
    <NavigationProvider value={navigation}>
      <QueryClientProvider client={queryClient}>
        <SkillDetailPage skillId="skill-1" />
      </QueryClientProvider>
    </NavigationProvider>,
  );
  return { push, queryClient };
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

function section(name: string) {
  return screen.getByRole("heading", { name, level: 2 }).closest("section")!;
}

/** The description lives behind its panel row; opening it yields the field. */
async function openDescription(current = LONG_DESCRIPTION) {
  await userEvent.click(await screen.findByRole("button", { name: current }));
  return (await screen.findByLabelText("Description")) as HTMLTextAreaElement;
}

async function openMoreActions() {
  await userEvent.click(await screen.findByRole("button", { name: "More actions" }));
  return screen.findByRole("menu");
}

beforeEach(() => {
  vi.clearAllMocks();
  skillRef.current = baseSkill;
  agentsRef.current = [];
  membersRef.current = [
    { user_id: "user-1", role: "member", name: "Ada Lovelace", avatar_url: null },
  ];
  canEditRef.current = true;
});

describe("SkillDetailPage header", () => {
  it("puts the skill under Skills in the breadcrumb, with Add to agent as the one solid action", async () => {
    renderPage();
    await screen.findByRole("heading", { name: "aiforui-animations", level: 1 });
    const header = screen.getByRole("banner");
    expect(within(header).getByRole("link", { name: "Skills" })).toHaveAttribute(
      "href",
      "/acme/skills",
    );
    expect(within(header).getByText("aiforui-animations")).toBeInTheDocument();

    await userEvent.click(within(header).getByRole("button", { name: "Add to agent" }));
    expect(screen.getByTestId("add-to-agent-dialog")).toBeInTheDocument();
  });

  it("keeps copy link, update from source and delete in the ⋯ menu", async () => {
    skillRef.current = {
      ...baseSkill,
      config: { origin: { type: "github", source_url: SOURCE_URL } },
    };
    renderPage();
    const menu = await openMoreActions();
    expect(
      within(menu).getAllByRole("menuitem").map((item) => item.textContent),
    ).toEqual(["Copy link", "Update from source", "Delete skill"]);
  });

  it("copies the skill's shareable link", async () => {
    renderPage();
    const menu = await openMoreActions();
    await userEvent.click(within(menu).getByRole("menuitem", { name: "Copy link" }));
    expect(copyTextMock).toHaveBeenCalledWith("https://app.test/acme/skills/skill-1");
    await vi.waitFor(() => expect(toast.success).toHaveBeenCalledWith("Link copied"));
  });

  it("confirms before deleting", async () => {
    renderPage();
    const menu = await openMoreActions();
    await userEvent.click(within(menu).getByRole("menuitem", { name: "Delete skill" }));
    expect(await screen.findByRole("dialog")).toHaveTextContent("Delete skill");
  });

  it("offers read-only viewers only what they can do", async () => {
    canEditRef.current = false;
    renderPage();
    const menu = await openMoreActions();
    expect(
      within(menu).getAllByRole("menuitem").map((item) => item.textContent),
    ).toEqual(["Copy link"]);
  });
});

describe("SkillDetailPage body", () => {
  it("reads as identity, then Content, Files and Used by — with no tabs", async () => {
    renderPage();
    expect(
      await screen.findByRole("heading", { name: "aiforui-animations", level: 1 }),
    ).toBeInTheDocument();
    expect(
      screen.getAllByRole("heading", { level: 2 }).map((h) => h.textContent),
    ).toEqual(["Content", "Files", "Used by"]);
    expect(screen.queryAllByRole("tab")).toHaveLength(0);
  });

  it("previews SKILL.md without its frontmatter", async () => {
    renderPage();
    const preview = await screen.findByTestId("skill-content-preview");
    expect(preview.textContent).toContain("# Interface Animations");
    expect(preview.textContent).not.toContain("name: aiforui-animations");
  });

  it("lists supporting files with their count", async () => {
    renderPage();
    await screen.findByRole("heading", { name: "Files", level: 2 });
    const files = section("Files");
    expect(within(files).getByText("1")).toBeInTheDocument();
    expect(within(files).getByRole("button", { name: "patterns.md" })).toBeInTheDocument();
  });

  it("opens Add to agent from the Used by section too", async () => {
    renderPage();
    await screen.findByRole("heading", { name: "Used by", level: 2 });
    await userEvent.click(
      within(section("Used by")).getByRole("button", { name: "Add to agent" }),
    );
    expect(screen.getByTestId("add-to-agent-dialog")).toBeInTheDocument();
  });

  it("gives read-only viewers Open instead of Edit, and no Add", async () => {
    canEditRef.current = false;
    renderPage();
    await screen.findByRole("heading", { name: "Content", level: 2 });
    expect(within(section("Content")).getByRole("button", { name: "Open" })).toBeInTheDocument();
    expect(within(section("Content")).queryByRole("button", { name: "Edit" })).toBeNull();
    expect(within(section("Files")).queryByRole("button", { name: "Add" })).toBeNull();
    expect(screen.getByText("Read-only")).toBeInTheDocument();
  });
});

describe("SkillDetailPage files view", () => {
  it("Content › Edit opens SKILL.md in a focused editor", async () => {
    const { push } = renderPage();
    await screen.findByRole("heading", { name: "Content", level: 2 });
    await userEvent.click(within(section("Content")).getByRole("button", { name: "Edit" }));

    expect(push).toHaveBeenCalledWith("/acme/skills/skill-1?view=files");
    expect(document.activeElement).toBe(
      screen.getByRole("textbox", { name: /SKILL\.md/ }),
    );
    // The breadcrumb walks back to the skill, then names the open file.
    const header = screen.getByRole("banner");
    expect(
      within(header).getByRole("link", { name: "aiforui-animations" }),
    ).toHaveAttribute("href", "/acme/skills/skill-1");
    expect(within(header).getByText("SKILL.md")).toBeInTheDocument();
  });

  it("opens a supporting file from the Files section", async () => {
    renderPage();
    await screen.findByRole("heading", { name: "Files", level: 2 });
    await userEvent.click(
      within(section("Files")).getByRole("button", { name: "patterns.md" }),
    );
    expect(
      screen.getByRole("tab", { name: "patterns.md" }).getAttribute("aria-selected"),
    ).toBe("true");
  });

  it("Files › Add opens the editor on the new-file field", async () => {
    renderPage();
    await screen.findByRole("heading", { name: "Files", level: 2 });
    await userEvent.click(within(section("Files")).getByRole("button", { name: "Add" }));
    expect(await screen.findByPlaceholderText("templates/review.md")).toBeInTheDocument();
  });

  it("restores the files from ?view=files, the link the retired Files tab wrote", async () => {
    renderPage(new URLSearchParams("view=files"));
    expect(
      (await screen.findByRole("tab", { name: "SKILL.md" })).getAttribute("aria-selected"),
    ).toBe("true");
  });

  it("lands on the overview for an old ?tab= link", async () => {
    renderPage(new URLSearchParams("tab=settings"));
    expect(
      await screen.findByRole("heading", { name: "Content", level: 2 }),
    ).toBeInTheDocument();
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

describe("SkillDetailPage properties panel", () => {
  it("edits the name in place", async () => {
    renderPage();
    const field = (await screen.findByLabelText("Name")) as HTMLInputElement;
    expect(field.value).toBe("aiforui-animations");
  });

  it("opens the whole description in a field sized for it", async () => {
    renderPage();
    const field = await openDescription();
    expect(field.value).toBe(LONG_DESCRIPTION);
    expect(Number(field.rows)).toBeGreaterThanOrEqual(4);
    expect(
      screen.getByText(`${LONG_DESCRIPTION.length} characters`, { exact: false }),
    ).toBeTruthy();
  });

  it("links an imported origin to its source", async () => {
    skillRef.current = {
      ...baseSkill,
      config: { origin: { type: "github", source_url: SOURCE_URL } },
    };
    renderPage();
    const link = (await screen.findByRole("link", {
      name: "Imported · GitHub",
    })) as HTMLAnchorElement;
    expect(link.getAttribute("href")).toBe(SOURCE_URL);
    expect(link.getAttribute("target")).toBe("_blank");
    expect(link.getAttribute("rel")).toContain("noopener");
  });

  it("keeps manual origins as plain text", async () => {
    renderPage();
    expect(await screen.findByText("Created manually")).toBeTruthy();
    expect(
      screen.queryByRole("link", { name: "Created manually" }),
    ).toBeNull();
  });

  // Which source_urls are linkable is originSourceUrl's contract; its full
  // matrix lives in ../lib/origin.test.ts. What belongs here is the row's
  // behaviour when the helper refuses: it degrades to plain text, still
  // naming the origin, rather than dropping the label along with the href.
  it("degrades a refused source_url to plain text, keeping the label", async () => {
    skillRef.current = {
      ...baseSkill,
      config: {
        origin: { type: "github", source_url: "https://evil.example/skills" },
      },
    };
    renderPage();
    expect(await screen.findByText("Imported · GitHub")).toBeTruthy();
    expect(
      screen.queryByRole("link", { name: "Imported · GitHub" }),
    ).toBeNull();
  });
});

describe("SkillDetailPage save pill", () => {
  it("is absent while clean and floats in with a change summary once dirty", async () => {
    renderPage();
    const field = await openDescription();
    expect(screen.queryByRole("button", { name: /Save changes/ })).toBeNull();

    fireEvent.change(field, { target: { value: "changed" } });
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
    fireEvent.change(await openDescription(), {
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
    await screen.findByRole("heading", { name: "Content", level: 2 });
    expect(screen.queryByText(/^Changed:/)).toBeNull();
  });

  it("stays clean after Discard on a trailing-newline description", async () => {
    skillRef.current = { ...baseSkill, description: `${LONG_DESCRIPTION}\n` };
    renderPage();
    fireEvent.change(await openDescription(), { target: { value: "edited" } });
    fireEvent.click(await screen.findByRole("button", { name: "Discard" }));
    expect(screen.queryByText(/^Changed:/)).toBeNull();
  });

  it("pulls a remote edit in silently while the draft is untouched", async () => {
    const { queryClient } = renderPage();
    await screen.findByRole("heading", { name: "Content", level: 2 });

    await remoteUpdate(queryClient, { description: "Rewritten by the agent" });

    // The new text reaching the field IS the fix: the old code left the editor
    // frozen on the pre-update value behind a conflict banner.
    expect(await openDescription("Rewritten by the agent")).toHaveValue(
      "Rewritten by the agent",
    );
    expect(screen.queryByText(CONFLICT_BANNER)).toBeNull();
    expect(screen.queryByText(/^Changed:/)).toBeNull();
  });

  it("pulls a remote SKILL.md edit in silently too", async () => {
    const { queryClient } = renderPage(new URLSearchParams("view=files"));
    await screen.findByRole("tab", { name: "SKILL.md" });

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
    const field = await openDescription();
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
    const field = await openDescription();
    fireEvent.change(field, { target: { value: "my unsaved edit" } });

    await remoteUpdate(queryClient, { description: "Rewritten by the agent" });

    expect(await screen.findByText(CONFLICT_BANNER)).toBeTruthy();
    expect(field.value).toBe("my unsaved edit");
  });
});
