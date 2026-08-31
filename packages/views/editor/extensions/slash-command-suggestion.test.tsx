import { act, render } from "@testing-library/react";
import { createRef, type ReactNode } from "react";
import { beforeAll, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "@multica/core/i18n/react";
import { workspaceKeys } from "@multica/core/workspace/queries";
import type { SkillSummary } from "@multica/core/types";
import type { QueryClient } from "@tanstack/react-query";
import enEditor from "../../locales/en/editor.json";

const TEST_RESOURCES = {
  en: { editor: enEditor },
};

function I18nWrapper({ children }: { children: ReactNode }) {
  return (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      {children}
    </I18nProvider>
  );
}

beforeAll(() => {
  Element.prototype.scrollIntoView = vi.fn();
});

vi.mock("@multica/core/platform", () => ({
  getCurrentWsId: () => "ws-1",
}));

import {
  SlashCommandList,
  type SlashCommandListRef,
  createSlashCommandSuggestion,
  type SlashCommandItem,
  buildBuiltinCommandItems,
  BUILTIN_COMMANDS,
  createBuiltinCommandSuggestion,
  QUICK_ACTION_ITEM_PREFIX,
} from "./slash-command-suggestion";

function skill(overrides: Partial<SkillSummary>): SkillSummary {
  return {
    id: "skill-1",
    workspace_id: "ws-1",
    name: "deploy",
    description: "",
    config: {},
    created_by: "u1",
    created_at: "",
    updated_at: "",
    ...overrides,
  };
}

function fakeQc(data: {
  skills?: SkillSummary[];
  fetchedSkills?: SkillSummary[];
}): QueryClient {
  const map = new Map<string, unknown>();
  if (data.skills) {
    map.set(JSON.stringify(workspaceKeys.skills("ws-1")), data.skills);
  }
  return {
    getQueryData: (key: readonly unknown[]) => map.get(JSON.stringify(key)),
    fetchQuery: () => Promise.resolve(data.fetchedSkills ?? []),
  } as unknown as QueryClient;
}

function itemResult(
  qc: QueryClient,
  query = "",
): SlashCommandItem[] | Promise<SlashCommandItem[]> {
  const config = createSlashCommandSuggestion(qc);
  return config.items!({
    query,
    editor: {} as never,
    signal: new AbortController().signal,
  });
}

function items(qc: QueryClient, query = ""): SlashCommandItem[] {
  return itemResult(qc, query) as SlashCommandItem[];
}

describe("slash command suggestion items", () => {
  it("returns the Workspace Skill library without requiring Agent assignments", () => {
    const qc = fakeQc({
      skills: [
        skill({ id: "s1", name: "deploy", description: "Ship changes" }),
        skill({ id: "s2", name: "review", description: "Review code" }),
      ],
    });

    expect(items(qc)).toEqual([
      { id: "s1", skillId: "s1", label: "deploy", description: "Ship changes" },
      { id: "s2", skillId: "s2", label: "review", description: "Review code" },
    ]);
  });

  it("filters by name or description and ranks exact/prefix matches first", () => {
    const qc = fakeQc({
      skills: [
        skill({ id: "s1", name: "reviewer", description: "" }),
        skill({ id: "s2", name: "review", description: "" }),
        skill({ id: "s3", name: "prototype", description: "Review a pull request" }),
      ],
    });

    expect(items(qc, "review").map((item) => item.id)).toEqual(["s2", "s1", "s3"]);
    expect(items(qc, "pull").map((item) => item.id)).toEqual(["s3"]);
  });

  it("keeps a name match inside the 20-item cap", () => {
    const qc = fakeQc({
      skills: [
        ...Array.from({ length: 25 }, (_, i) =>
          skill({ id: `d${i}`, name: `skill-${i}`, description: "wayfinder helper" }),
        ),
        skill({ id: "named", name: "wayfinder" }),
      ],
    });

    const result = items(qc, "way");
    expect(result).toHaveLength(20);
    expect(result[0]?.id).toBe("named");
  });

  it("tolerates a missing description from stale cached API data", () => {
    const qc = fakeQc({
      skills: [skill({ id: "s1", name: "deploy", description: undefined as never })],
    });

    expect(items(qc, "dep")).toEqual([
      { id: "s1", skillId: "s1", label: "deploy", description: "" },
    ]);
  });

  it("fetches a cold Workspace Skill cache lazily", async () => {
    const qc = fakeQc({ fetchedSkills: [skill({ id: "s1", name: "deploy" })] });
    await expect(itemResult(qc)).resolves.toEqual([
      { id: "s1", skillId: "s1", label: "deploy", description: "" },
    ]);
  });

  it("returns an empty picker for an empty Workspace Skill library", () => {
    expect(items(fakeQc({ skills: [] }))).toEqual([]);
  });
});

describe("SlashCommandList keyboard handling", () => {
  it("lets Enter and arrow keys fall through when there are no selectable items", () => {
    const ref = createRef<SlashCommandListRef>();

    render(
      <I18nWrapper>
        <SlashCommandList ref={ref} items={[]} query="" command={vi.fn()} />
      </I18nWrapper>,
    );

    expect(
      ref.current?.onKeyDown({
        event: new KeyboardEvent("keydown", { key: "Enter" }),
      }),
    ).toBe(false);
    expect(
      ref.current?.onKeyDown({
        event: new KeyboardEvent("keydown", { key: "Enter", metaKey: true }),
      }),
    ).toBe(false);
    expect(
      ref.current?.onKeyDown({
        event: new KeyboardEvent("keydown", { key: "ArrowUp" }),
      }),
    ).toBe(false);
    expect(
      ref.current?.onKeyDown({
        event: new KeyboardEvent("keydown", { key: "ArrowDown" }),
      }),
    ).toBe(false);
  });

  it("handles Enter and arrow keys when selectable items exist", () => {
    const ref = createRef<SlashCommandListRef>();
    const command = vi.fn();
    const selectableItems: SlashCommandItem[] = [
      { id: "s1", label: "deploy", description: "Ship changes" },
      { id: "s2", label: "review", description: "Review code" },
    ];

    render(
      <I18nWrapper>
        <SlashCommandList
          ref={ref}
          items={selectableItems}
          query=""
          command={command}
        />
      </I18nWrapper>,
    );

    expect(
      ref.current?.onKeyDown({
        event: new KeyboardEvent("keydown", { key: "ArrowUp" }),
      }),
    ).toBe(true);
    expect(
      ref.current?.onKeyDown({
        event: new KeyboardEvent("keydown", { key: "ArrowDown" }),
      }),
    ).toBe(true);
    expect(
      ref.current?.onKeyDown({
        event: new KeyboardEvent("keydown", { key: "Enter" }),
      }),
    ).toBe(true);
    expect(command).toHaveBeenCalledWith(selectableItems[0]);
  });

  // MUL-5495: same Ctrl aliases the command bar (cmdk) accepts, so the slash
  // picker navigates like every other list in the product.
  it("navigates with Ctrl+N/J and Ctrl+P/K, and leaves the bare letters alone", () => {
    const ref = createRef<SlashCommandListRef>();
    const command = vi.fn();
    const selectableItems: SlashCommandItem[] = [
      { id: "s1", label: "deploy", description: "Ship changes" },
      { id: "s2", label: "review", description: "Review code" },
      { id: "s3", label: "note", description: "Leave a note" },
    ];

    render(
      <I18nWrapper>
        <SlashCommandList
          ref={ref}
          items={selectableItems}
          query=""
          command={command}
        />
      </I18nWrapper>,
    );

    const highlightedLabel = () => {
      const buttons = Array.from(document.querySelectorAll<HTMLButtonElement>("button"));
      return buttons.find((b) => b.classList.contains("bg-accent"))?.textContent ?? "";
    };
    let handled: boolean | undefined;
    const press = (init: KeyboardEventInit) =>
      act(() => {
        handled = ref.current?.onKeyDown({ event: new KeyboardEvent("keydown", init) });
      });

    press({ key: "n", ctrlKey: true });
    expect(handled).toBe(true);
    expect(highlightedLabel()).toContain("/review");

    press({ key: "j", ctrlKey: true });
    expect(highlightedLabel()).toContain("/note");

    press({ key: "p", ctrlKey: true });
    expect(highlightedLabel()).toContain("/review");

    press({ key: "k", ctrlKey: true });
    expect(highlightedLabel()).toContain("/deploy");

    // Bare letters stay query characters — "/note" must remain typeable.
    press({ key: "n" });
    expect(handled).toBe(false);
    expect(highlightedLabel()).toContain("/deploy");

    press({ key: "Enter" });
    expect(command).toHaveBeenCalledWith(selectableItems[0]);
  });

  // MUL-3685: plain Tab accepts the highlighted item like Enter; Shift+Tab and
  // modifier+Tab fall through so reverse focus / OS switching are preserved.
  it("accepts the highlighted item on plain Tab, ignoring Shift/modifier+Tab", () => {
    const ref = createRef<SlashCommandListRef>();
    const command = vi.fn();
    const selectableItems: SlashCommandItem[] = [
      { id: "s1", label: "deploy", description: "Ship changes" },
      { id: "s2", label: "review", description: "Review code" },
    ];

    render(
      <I18nWrapper>
        <SlashCommandList
          ref={ref}
          items={selectableItems}
          query=""
          command={command}
        />
      </I18nWrapper>,
    );

    const press = (init: KeyboardEventInit) =>
      ref.current?.onKeyDown({ event: new KeyboardEvent("keydown", init) });

    expect(press({ key: "Tab", shiftKey: true })).toBe(false);
    expect(press({ key: "Tab", metaKey: true })).toBe(false);
    expect(command).not.toHaveBeenCalled();

    expect(press({ key: "Tab" })).toBe(true);
    expect(command).toHaveBeenCalledWith(selectableItems[0]);
  });

  it("lets Tab fall through when there are no selectable items, like Enter", () => {
    const ref = createRef<SlashCommandListRef>();

    render(
      <I18nWrapper>
        <SlashCommandList ref={ref} items={[]} query="" command={vi.fn()} />
      </I18nWrapper>,
    );

    expect(
      ref.current?.onKeyDown({
        event: new KeyboardEvent("keydown", { key: "Tab" }),
      }),
    ).toBe(false);
  });
});

describe("SlashCommandList empty states", () => {
  it("shows a configured-skills empty state before search text is entered", () => {
    const { getByText } = render(
      <I18nWrapper>
        <SlashCommandList items={[]} query="" command={vi.fn()} />
      </I18nWrapper>,
    );

    expect(getByText("No skills configured")).toBeInTheDocument();
  });

  it("shows a no-results empty state when search text has no matches", () => {
    const { getByText } = render(
      <I18nWrapper>
        <SlashCommandList items={[]} query="deploy" command={vi.fn()} />
      </I18nWrapper>,
    );

    expect(getByText("No matching skills")).toBeInTheDocument();
  });

  it("renders nothing on empty items when hideOnEmpty is set (command menu)", () => {
    const { container } = render(
      <I18nWrapper>
        <SlashCommandList items={[]} query="6" command={vi.fn()} hideOnEmpty />
      </I18nWrapper>,
    );

    // No popup box on a non-matching `/` (e.g. typing a date like 6/8).
    expect(container).toBeEmptyDOMElement();
  });
});

describe("buildBuiltinCommandItems", () => {
  it("returns the full built-in command set for an empty query", () => {
    expect(buildBuiltinCommandItems("")).toEqual(BUILTIN_COMMANDS);
  });

  it("includes /note while the query is a prefix of the label", () => {
    expect(buildBuiltinCommandItems("no").map((c) => c.id)).toEqual(["note"]);
    expect(buildBuiltinCommandItems("NOTE").map((c) => c.id)).toEqual(["note"]);
  });

  it("matches the label as a prefix only — not the description", () => {
    // "agent" appears in the description but is not a label prefix.
    expect(buildBuiltinCommandItems("agent")).toEqual([]);
    // A non-prefix substring of the label does not match either.
    expect(buildBuiltinCommandItems("ote")).toEqual([]);
  });

  it("returns nothing for a query that matches no command", () => {
    expect(buildBuiltinCommandItems("deploy")).toEqual([]);
  });

  it("merges Quick Actions, Workspace Skills, and built-ins", () => {
    const result = buildBuiltinCommandItems(
      "",
      [{ id: "qa-1", name: "summarize", description: "Summarize the issue" }],
      [skill({ id: "skill-1", name: "architecture-sweep", description: "Audit structure" })],
    );

    expect(result.map((item) => item.id)).toEqual([
      `${QUICK_ACTION_ITEM_PREFIX}qa-1`,
      "skill-1",
      "note",
    ]);
    expect(result[1]).toMatchObject({ skillId: "skill-1", label: "architecture-sweep" });
  });

  it("reserves a slot for /note when 20 or more Workspace Skills match", () => {
    const result = buildBuiltinCommandItems(
      "",
      [{ id: "qa-1", name: "summarize" }],
      Array.from({ length: 25 }, (_, index) =>
        skill({ id: `skill-${index}`, name: `skill-${index}` }),
      ),
    );

    expect(result).toHaveLength(20);
    expect(result[0]?.id).toBe(`${QUICK_ACTION_ITEM_PREFIX}qa-1`);
    expect(result.at(-1)?.id).toBe("note");
  });
});

describe("SlashCommandList built-in command rendering", () => {
  it("renders the localized description for a built-in command", () => {
    const { getByText } = render(
      <I18nWrapper>
        <SlashCommandList
          items={buildBuiltinCommandItems("")}
          query=""
          command={vi.fn()}
          hideOnEmpty
        />
      </I18nWrapper>,
    );

    expect(getByText("/note")).toBeInTheDocument();
    expect(
      getByText("Add a note — won't trigger any agents"),
    ).toBeInTheDocument();
  });
});

describe("issue comment `/` menu — Workspace Skills", () => {
  it("loads Workspace Skills through the same Query cache as Chat", () => {
    const qc = fakeQc({ skills: [skill({ id: "skill-1", name: "architecture-sweep" })] });
    const suggestion = createBuiltinCommandSuggestion({}, qc);
    const result = suggestion.items!({
      query: "arch",
      editor: {} as never,
      signal: new AbortController().signal,
    }) as SlashCommandItem[];

    expect(result).toEqual([
      {
        id: "skill-1",
        skillId: "skill-1",
        label: "architecture-sweep",
        description: "",
      },
    ]);
  });

  it("inserts a selected Skill as the durable rich Markdown marker", () => {
    const getSelection = vi
      .spyOn(window, "getSelection")
      .mockReturnValue({ collapseToEnd: vi.fn() } as unknown as Selection);
    const inserts: unknown[] = [];
    const chain = {
      focus: () => chain,
      insertContentAt: (_range: unknown, content: unknown) => {
        inserts.push(content);
        return chain;
      },
      run: () => true,
    };
    const editor = {
      chain: () => chain,
      view: { state: { selection: { $to: { nodeAfter: null } } } },
    };
    const suggestion = createBuiltinCommandSuggestion();

    suggestion.command!({
      editor,
      range: { from: 0, to: 5 },
      props: {
        id: "skill-1",
        skillId: "skill-1",
        label: "review",
        description: "Review code",
      },
    } as never);

    expect(inserts).toEqual([
      [
        {
          type: "slashCommand",
          attrs: {
            id: "skill-1",
            label: "review",
            mentionSuggestionChar: "/",
          },
        },
        { type: "text", text: " " },
      ],
    ]);
    getSelection.mockRestore();
  });
});


// Async quick-action rendering in the `/` menu (MUL-5465, review finding #4).
//
// The render request resolves after an arbitrary delay, during which the user
// keeps typing. Three behaviours have to hold, and each one was a real bug at
// some point in this PR:
//   - a rejection must not destroy what the user typed
//   - a success must replace the ORIGINAL command, not wherever the caret is
//   - a command edited mid-flight must be left alone, not overwritten
describe("builtin `/` menu — async quick action rendering", () => {
  // Minimal editor stand-in: enough ProseMirror surface for the command to
  // read the range text and issue its chain.
  function fakeEditor(text: string) {
    const calls: { from: number; to: number; content: string; contentType?: string }[] = [];
    let docText = text;
    const chain = {
      focus: () => chain,
      insertContentAt: (
        range: { from: number; to: number },
        content: string,
        opts?: { contentType?: string },
      ) => {
        calls.push({ from: range.from, to: range.to, content, contentType: opts?.contentType });
        return chain;
      },
      insertContent: (content: string) => {
        calls.push({ from: -1, to: -1, content });
        return chain;
      },
      deleteRange: () => chain,
      run: () => true,
    };
    return {
      calls,
      setText: (next: string) => {
        docText = next;
      },
      editor: {
        chain: () => chain,
        state: {
          doc: {
            get content() {
              return { size: docText.length + 1 };
            },
            textBetween: (from: number, to: number) => docText.slice(from, to),
          },
        },
        view: { state: { selection: { $to: { nodeAfter: null } } } },
      },
    };
  }

  const range = { from: 0, to: 7 };
  const item = { id: `${QUICK_ACTION_ITEM_PREFIX}qa-1`, label: "review" };

  it("leaves the typed command intact and reports the failure when render rejects", async () => {
    const { editor, calls } = fakeEditor("/review");
    const onRenderError = vi.fn();
    const suggestion = createBuiltinCommandSuggestion({
      renderQuickAction: () => Promise.reject(new Error("boom")),
      onRenderError,
    });

    suggestion.command!({ editor, range, props: item } as never);
    await act(async () => {
      await Promise.resolve();
      await Promise.resolve();
    });

    expect(calls).toHaveLength(0);
    expect(onRenderError).toHaveBeenCalledTimes(1);
  });

  it("replaces the original range once a delayed render resolves", async () => {
    const { editor, calls } = fakeEditor("/review");
    let resolve!: (v: string) => void;
    const suggestion = createBuiltinCommandSuggestion({
      renderQuickAction: () => new Promise<string>((r) => { resolve = r; }),
    });

    suggestion.command!({ editor, range, props: item } as never);
    expect(calls).toHaveLength(0); // nothing destroyed while in flight

    await act(async () => {
      resolve("rendered body");
      await Promise.resolve();
      await Promise.resolve();
    });

    // contentType must be "markdown": inserted as a plain string, the
    // server-rendered `[@Name](mention://…)` lands as literal text and
    // serialises back out with escaped brackets, so the mention never becomes
    // a node and renders as raw markup in the thread.
    expect(calls).toEqual([
      { from: 0, to: 7, content: "rendered body", contentType: "markdown" },
    ]);
  });

  it("abandons the insert when the command was edited while the request was open", async () => {
    const { editor, calls, setText } = fakeEditor("/review");
    let resolve!: (v: string) => void;
    const suggestion = createBuiltinCommandSuggestion({
      renderQuickAction: () => new Promise<string>((r) => { resolve = r; }),
    });

    suggestion.command!({ editor, range, props: item } as never);
    // The user rewrites the command; a prefix-only check would still see a
    // leading "/" here and clobber it.
    setText("/fixnow");

    await act(async () => {
      resolve("rendered body");
      await Promise.resolve();
      await Promise.resolve();
    });

    expect(calls).toHaveLength(0);
  });
});
