// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach } from "vitest";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Skill } from "@multica/core/types";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enSkills from "../../locales/en/skills.json";
import { useState } from "react";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";

const TEST_RESOURCES = { en: { common: enCommon, skills: enSkills } };

// MUL-7107: every band of a detail page reads the shared rail. The read-only
// capability banner was the one left behind, so a viewer without edit rights
// saw a near-full-width card above centred content on a wide window. The
// constants are overridden with sentinels because their real values are
// ordinary Tailwind classes a hand-written element could match by accident.
const RAIL_SENTINEL = "rail-sentinel";
const GUTTER_SENTINEL = "gutter-sentinel";
vi.mock("../../layout/page-header", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../../layout/page-header")>()),
  PAGE_RAIL: "rail-sentinel",
  PAGE_GUTTER: "gutter-sentinel",
}));

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

import { api } from "@multica/core/api";
import { SkillDetailPage } from "./skill-detail-page";

const source = (path: string) =>
  screen.getByRole("textbox", { name: `Source of ${path}` }) as HTMLTextAreaElement;
const findSource = async (path: string) =>
  (await screen.findByRole("textbox", { name: `Source of ${path}` })) as HTMLTextAreaElement;

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

const PATHNAME = "/acme/skills/skill-1";

/** The page reads the open file from `?file=`; `replace` moves the URL like the router would. */
function renderPage(searchParams = new URLSearchParams()) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const replace = vi.fn();
  const navigate: { current: (path: string) => void } = { current: () => {} };

  function Harness() {
    const [params, setParams] = useState(searchParams);
    navigate.current = (path) => setParams(new URLSearchParams(path.split("?")[1] ?? ""));
    const navigation: NavigationAdapter = {
      push: vi.fn(),
      replace: (path) => {
        replace(path);
        navigate.current(path);
      },
      back: vi.fn(),
      pathname: PATHNAME,
      searchParams: params,
      hash: "",
      getShareableUrl: (path) => path,
    };
    return (
      <NavigationProvider value={navigation}>
        <QueryClientProvider client={queryClient}>
          <SkillDetailPage skillId="skill-1" />
        </QueryClientProvider>
      </NavigationProvider>
    );
  }

  render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <Harness />
    </I18nProvider>,
  );
  /** Opens another file the way the knowledge base tree's links do. */
  const openFile = (path: string) => act(() => navigate.current(`${PATHNAME}?file=${path}`));
  return { replace, queryClient, openFile };
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

describe("SkillDetailPage file selection", () => {
  it("opens SKILL.md beside the skill's properties, with no tabs", async () => {
    renderPage();
    expect(await screen.findByLabelText("Description")).toBeTruthy();
    expect(source("SKILL.md").value).toBe(baseSkill.content);
    expect(screen.queryByRole("tab")).toBeNull();
  });

  it("opens the file named in ?file=, keeping the skill's properties beside it", async () => {
    renderPage(new URLSearchParams("file=patterns.md"));
    expect((await findSource("patterns.md")).value).toBe("# Patterns\n");
    expect(screen.getByText("Reference file of aiforui-animations")).toBeTruthy();
    expect(screen.getByLabelText("Description")).toBeTruthy();
  });

  it("folds the properties panel away and back from the header", async () => {
    renderPage();
    await screen.findByLabelText("Description");
    const toggle = screen.getByRole("button", { name: "Details" });
    expect(toggle.getAttribute("aria-pressed")).toBe("true");

    fireEvent.click(toggle);
    expect(screen.queryByLabelText("Description")).toBeNull();
    expect(source("SKILL.md")).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Details" }));
    expect(screen.getByLabelText("Description")).toBeTruthy();
  });

  it("falls back to SKILL.md when ?file= names no file", async () => {
    renderPage(new URLSearchParams("file=missing.md"));
    expect(await screen.findByLabelText("Description")).toBeTruthy();
  });

  it("follows a renamed file in the URL", async () => {
    const { replace } = renderPage(new URLSearchParams("file=patterns.md"));
    await userEvent.click(await screen.findByRole("button", { name: "Rename" }));
    const input = screen.getByDisplayValue("patterns.md");
    await userEvent.clear(input);
    await userEvent.type(input, "guides/patterns.md{Enter}");
    expect(replace).toHaveBeenLastCalledWith(`${PATHNAME}?file=guides%2Fpatterns.md`);
    expect(screen.getAllByText("guides/patterns.md").length).toBeGreaterThan(0);
    expect(screen.queryByText("patterns.md")).toBeNull();
  });

  it("returns to SKILL.md after deleting the open file", async () => {
    const { replace } = renderPage(new URLSearchParams("file=patterns.md"));
    await userEvent.click(await screen.findByRole("button", { name: "Delete file" }));
    expect(replace).toHaveBeenLastCalledWith(PATHNAME);
    expect(await screen.findByLabelText("Description")).toBeTruthy();
    expect(screen.getByText("Changed: 1 file")).toBeTruthy();
  });

  it("opens a new file in a focused editor", async () => {
    const { replace } = renderPage();
    await userEvent.click(await screen.findByRole("button", { name: "New file" }));
    await userEvent.type(screen.getByPlaceholderText("templates/review.md"), "notes.md");
    await userEvent.click(screen.getByRole("button", { name: "Add" }));

    expect(replace).toHaveBeenLastCalledWith(`${PATHNAME}?file=notes.md`);
    expect(source("notes.md").value).toBe("");
    expect(document.activeElement).toBe(source("notes.md"));
  });
});

describe("SkillDetailPage editor", () => {
  it("edits the whole file as source, with no preview or edit switch", async () => {
    renderPage();
    expect((await findSource("SKILL.md")).value).toBe(baseSkill.content);
    for (const name of ["Preview", "Edit", "Plain text"]) {
      expect(screen.queryByRole("button", { name })).toBeNull();
    }
  });

  it("saves exactly what was typed: frontmatter, prompt tags, placeholders, list markers", async () => {
    vi.mocked(api.updateSkill).mockImplementation(async (_id, payload) => ({
      ...(skillRef.current as Skill),
      ...payload,
      updated_at: "2026-07-29T11:00:00Z",
    }) as Skill);
    const typed =
      "---\nname: aiforui-animations\ndisable-model-invocation: true\n---\n\n" +
      "<instructions>\nUse {{reviewer_name}} and snake_case_names.\n</instructions>\n\n" +
      "* star bullet\n1) paren list\n\n| a | b |\n|---|---|\n";
    renderPage();
    fireEvent.change(await findSource("SKILL.md"), { target: { value: typed } });
    await userEvent.click(screen.getByRole("button", { name: /Save changes/ }));

    expect(vi.mocked(api.updateSkill).mock.calls[0]![1].content).toBe(typed);
  });

  it("edits other files as source too", async () => {
    skillRef.current = {
      ...baseSkill,
      files: [{ id: "f-2", path: "scripts/run.sh", content: "echo hi\n" } as Skill["files"][number]],
    };
    renderPage(new URLSearchParams("file=scripts/run.sh"));
    expect((await findSource("scripts/run.sh")).value).toBe("echo hi\n");
  });

  it("shows read-only viewers the rendered file, without frontmatter or file actions", async () => {
    canEditRef.current = false;
    renderPage();

    const preview = await screen.findByTestId("preview");
    expect(preview.textContent).toContain("# Interface Animations");
    expect(preview.textContent).not.toContain("name: aiforui-animations");
    expect(screen.queryByRole("textbox", { name: /^Source of/ })).toBeNull();
    for (const name of ["New file", "Rename", "Delete file"]) {
      expect(screen.queryByRole("button", { name })).toBeNull();
    }
  });
});

describe("SkillDetailPage save pill", () => {
  it("is absent while clean and floats in with a change summary once dirty", async () => {
    renderPage();
    await screen.findByLabelText("Description");
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
    renderPage(new URLSearchParams("file=patterns.md"));
    fireEvent.change(await findSource("patterns.md"), {
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
      screen.getByText(`${LONG_DESCRIPTION.length} characters`, { exact: false }),
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
    await screen.findByLabelText("Description");
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
    await screen.findByLabelText("Description");

    await remoteUpdate(queryClient, { description: "Rewritten by the agent" });

    // The new text reaching the field IS the fix: the old code left the editor
    // frozen on the pre-update value behind a conflict banner.
    expect(await screen.findByDisplayValue("Rewritten by the agent")).toBeTruthy();
    expect(screen.queryByText(CONFLICT_BANNER)).toBeNull();
    expect(screen.queryByText(/^Changed:/)).toBeNull();
  });

  it("pulls a remote SKILL.md edit in silently too", async () => {
    const { queryClient } = renderPage();
    await screen.findByLabelText("Description");

    await remoteUpdate(queryClient, {
      content: `${baseSkill.content}\n## Added remotely\n`,
    });

    await waitFor(() => expect(source("SKILL.md").value).toContain("Added remotely"));
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

describe("SkillDetailPage origin link", () => {
  const SOURCE_URL = "https://github.com/anthropics/skills/tree/main/animations";

  it("links the imported-origin chip to its source", async () => {
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
  // matrix lives in ../lib/origin.test.ts. What belongs here is the chip's
  // behaviour when the helper refuses: it degrades to plain text, still
  // naming the origin, rather than dropping the label along with the href.
  it("degrades a refused source_url to plain text, keeping the chip", async () => {
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


describe("SkillDetailPage rail", () => {
  it("keeps the read-only capability banner on the shared rail", async () => {
    canEditRef.current = false;
    renderPage();
    await screen.findByLabelText("Description");

    const banner = document.body.querySelector(
      `.${RAIL_SENTINEL}.${GUTTER_SENTINEL}.pt-3`,
    );
    expect(banner).toBeTruthy();
  });

  it("puts the file toolbar on that same rail", async () => {
    renderPage();
    await screen.findByLabelText("Description");

    const toolbar = screen.getByText("SKILL.md", { selector: "span.font-mono" }).closest("div");
    expect(toolbar?.className).toContain(RAIL_SENTINEL);
    expect(toolbar?.className).toContain(GUTTER_SENTINEL);
  });
});
