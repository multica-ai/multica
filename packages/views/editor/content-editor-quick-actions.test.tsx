import type { Editor } from "@tiptap/core";
import { useState, type PropsWithChildren } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, cleanup, fireEvent, renderHook, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Agent, CommentTriggerPreview, Issue, ListQuickActionsResponse, MemberWithUser, QuickAction } from "@multica/core/types";
import { quickActionKeys } from "@multica/core/quick-actions";
import { issueKeys } from "@multica/core/issues/queries";
import { workspaceKeys } from "@multica/core/workspace/queries";
import { renderWithI18n } from "../test/i18n";
import { NavigationProvider } from "../navigation/context";
import { useQuickActionMenu } from "../issues/hooks/use-quick-action-menu";
import { useCommentTriggerPreview } from "../issues/hooks/use-comment-trigger-preview";
import { ContentEditor } from "./content-editor";
import { CommentInput } from "../issues/components/comment-input";
import { ReplyInput } from "../issues/components/reply-input";

const listQuickActions = vi.hoisted(() => vi.fn());
const renderQuickAction = vi.hoisted(() => vi.fn());
const getIssue = vi.hoisted(() => vi.fn());
const listAgents = vi.hoisted(() => vi.fn());
const listSquads = vi.hoisted(() => vi.fn());
const listMembers = vi.hoisted(() => vi.fn());
const previewCommentTriggers = vi.hoisted(() => vi.fn());
const toastError = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/permissions", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/permissions")>()),
  useIsGuest: () => false,
}));
vi.mock("sonner", () => ({ toast: { error: toastError } }));
vi.mock("@multica/core/auth", () => ({
  useAuthStore: { getState: () => ({ user: { id: "user-1" } }) },
}));

vi.mock("@multica/core/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/api")>();
  return { ...actual, api: { ...actual.api, getBaseUrl: () => "https://api.example.test", listQuickActions, renderQuickAction, getIssue, listAgents, listSquads, listMembers, previewCommentTriggers } };
});
vi.mock("@multica/core/paths", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/paths")>()),
  useCurrentWorkspace: () => ({ id: "ws", slug: "test" }),
  useWorkspaceSlug: () => "test",
  useWorkspacePaths: () => ({
    issueDetail: (id: string) => `/test/issues/${id}`,
    memberDetail: (id: string) => `/test/members/${id}`,
  }),
}));
vi.mock("@multica/core/hooks", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/hooks")>()),
  useWorkspaceId: () => "ws",
}));

function makeIssue(
  assigneeType: Issue["assignee_type"] = null,
  assigneeId: string | null = null,
): Issue {
  return {
    id: "issue-1",
    workspace_id: "ws",
    assignee_type: assigneeType,
    assignee_id: assigneeId,
  } as Issue;
}

function makeAgent(id: string, skills: Agent["skills"]): Agent {
  return {
    id,
    skills,
    runtime_id: "runtime-1",
    archived_at: null,
    owner_id: null,
    permission_mode: "public_to",
    invocation_targets: [{ target_type: "workspace", target_id: null }],
  } as Agent;
}

beforeEach(() => {
  listQuickActions.mockReset().mockResolvedValue({ quick_actions: [] });
  renderQuickAction.mockReset().mockResolvedValue("Rendered feedback");
  getIssue.mockReset().mockResolvedValue(makeIssue());
  listAgents.mockReset().mockResolvedValue([]);
  listSquads.mockReset().mockResolvedValue([]);
  listMembers.mockReset().mockResolvedValue([
    { user_id: "user-1", role: "member", name: "User" } as MemberWithUser,
  ]);
  toastError.mockReset();
  previewCommentTriggers.mockReset().mockReturnValue(new Promise(() => {}));
});

const clients: QueryClient[] = [];
afterEach(() => {
  cleanup();
  clients.splice(0).forEach((client) => client.clear());
});

const action: QuickAction = {
  id: "action",
  workspace_id: "ws",
  name: "delayed-feedback",
  description: "",
  assignee_type: "",
  assignee_id: "",
  prompt: "Rendered feedback",
  visibility: "public",
  status: "active",
  last_used_at: null,
  use_count: 0,
  created_by_id: "member",
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
  target_public: false,
  target_missing: false,
};

function pendingCatalog() {
  let resolve!: (data: ListQuickActionsResponse) => void;
  const promise = new Promise<ListQuickActionsResponse>((done) => { resolve = done; });
  listQuickActions.mockReturnValue(promise);
  return { resolve };
}

function Composer() {
  const [content, setContent] = useState("");
  const triggerPreview = useCommentTriggerPreview({ issueId: "issue-1", content });
  return (
    <ContentEditor
      onUpdate={setContent}
      quickActionMenu={useQuickActionMenu("issue-1", { triggerPreview, previewContent: content })}
      enableSlashCommands
      slashCommandMode="command"
      showBubbleMenu={false}
    />
  );
}

async function mount(
  initialActions?: QuickAction[],
  composer: "standalone" | "comment" | "reply" = "standalone",
) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } });
  if (initialActions) {
    client.setQueryData(quickActionKeys.list("ws"), { quick_actions: initialActions });
  }
  clients.push(client);
  const composerUi = composer === "comment" ? (
    <CommentInput issueId="issue-1" onSubmit={vi.fn().mockResolvedValue(true)} />
  ) : composer === "reply" ? (
    <ReplyInput
      issueId="issue-1"
      parentId="comment-1"
      avatarType="member"
      avatarId="user-1"
      onSubmit={vi.fn().mockResolvedValue(true)}
    />
  ) : <Composer />;
  const result = renderWithI18n(
    <NavigationProvider value={{
      push: vi.fn(),
      replace: vi.fn(),
      back: vi.fn(),
      pathname: "/test",
      searchParams: new URLSearchParams(),
      hash: "",
      getShareableUrl: (path) => path,
    }}>
      <QueryClientProvider client={client}>{composerUi}</QueryClientProvider>
    </NavigationProvider>,
  );
  if (composer !== "standalone") {
    fireEvent.click(screen.getByTestId(`${composer}-composer-shell`));
  }
  const dom = await waitFor(() => {
    const el = result.container.querySelector<HTMLElement & { editor: Editor }>(".ProseMirror");
    expect(el?.editor).toBeDefined();
    return el!;
  });
  return { ...result, client, editor: dom.editor, dom };
}

async function type(editor: Editor, text: string) {
  await act(async () => {
    editor.commands.focus("end");
    // Exercise the typed-trigger guard as well as the real suggestion plugin.
    for (const ch of text) {
      const { from, to } = editor.state.selection;
      const handled = editor.view.someProp("handleTextInput", (fn) =>
        fn(editor.view, from, to, ch, () => editor.state.tr.insertText(ch, from, to)),
      );
      if (!handled) editor.view.dispatch(editor.state.tr.insertText(ch, from, to));
    }
  });
}

const menuItem = () => screen.queryByRole("button", { name: "/delayed-feedback" });

describe("quick action menu query lifecycle", () => {
  function renderMenu(client: QueryClient, enabled: boolean) {
    return renderHook(() => useQuickActionMenu("issue-1", { enabled }), {
      wrapper: ({ children }: PropsWithChildren) => (
        <QueryClientProvider client={client}>{children}</QueryClientProvider>
      ),
    });
  }

  it("disables every query while the composer is a static shell", () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    clients.push(client);
    const { unmount } = renderMenu(client, false);

    const queries = client.getQueryCache().getAll();
    const observers = queries.flatMap((query) => query.observers);
    expect(observers).toHaveLength(5);
    expect(observers.every((observer) => observer.options.enabled === false)).toBe(true);
    expect(listQuickActions).not.toHaveBeenCalled();
    expect(getIssue).not.toHaveBeenCalled();
    expect(listAgents).not.toHaveBeenCalled();
    expect(listSquads).not.toHaveBeenCalled();
    expect(listMembers).not.toHaveBeenCalled();
    unmount();
  });

  it("disables mount refetches for active Issue context queries", () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    clients.push(client);
    const { unmount } = renderMenu(client, true);

    const keys = [
      issueKeys.detail("ws", "issue-1"),
      workspaceKeys.agents("ws"),
      workspaceKeys.squads("ws"),
      workspaceKeys.members("ws"),
    ];
    for (const queryKey of keys) {
      const query = client.getQueryCache().find({ queryKey, exact: true });
      const observers = query?.observers ?? [];
      expect(observers).toHaveLength(1);
      expect(observers[0]?.options.enabled).toBe(true);
      expect(observers[0]?.options.refetchOnMount).toBe(false);
    }
    unmount();
  });
});

describe("comment quick-action catalog updates in the real editor", () => {
  it.each(["", "agent", "squad"])(
    "inserts a cached %s action synchronously on Enter without a render request",
    async (targetType) => {
      const configured = {
        ...action,
        assignee_type: targetType,
        assignee_id: targetType ? "target-id" : "",
        target_name: targetType ? "Review Team" : undefined,
      };
      listQuickActions.mockResolvedValue({ quick_actions: [configured] });
      renderQuickAction.mockReturnValue(new Promise(() => {}));
      const { editor, dom } = await mount([configured]);
      await type(editor, "/delayed");
      await waitFor(() => expect(menuItem()).toBeVisible());

      fireEvent.keyDown(dom, { key: "Enter" });

      expect(editor.getText()).toBe(targetType ? "@Review Team\n\nRendered feedback" : "Rendered feedback");
      expect(renderQuickAction).not.toHaveBeenCalled();
      const mentions: { type: string; id: string }[] = [];
      editor.state.doc.descendants((node) => {
        if (node.type.name === "mention") mentions.push({ type: node.attrs.type, id: node.attrs.id });
      });
      expect(mentions).toEqual(targetType ? [{ type: targetType, id: "target-id" }] : []);
    },
  );

  it("keeps the draft and reports an unavailable target without a render request", async () => {
    const unavailable = { ...action, assignee_type: "agent", assignee_id: "target-id", target_missing: true };
    listQuickActions.mockResolvedValue({ quick_actions: [unavailable] });
    renderQuickAction.mockReturnValue(new Promise(() => {}));
    const { editor, dom } = await mount([unavailable]);
    await type(editor, "/delayed");
    await waitFor(() => expect(menuItem()).toBeVisible());

    fireEvent.keyDown(dom, { key: "Enter" });

    expect(editor.getText()).toBe("/delayed");
    expect(toastError).toHaveBeenCalledWith(
      "This agent or squad can't be found — refresh the page and try again.",
    );
    expect(renderQuickAction).not.toHaveBeenCalled();
  });

  it("uses a refreshed body and target while preserving the surrounding draft and undo", async () => {
    listQuickActions.mockResolvedValue({ quick_actions: [action] });
    const { editor, dom, client } = await mount([action]);
    await type(editor, "Keep this /delayed");
    await waitFor(() => expect(menuItem()).toBeVisible());
    const updated = {
      ...action,
      name: "delayed-updated",
      prompt: "Updated feedback",
      assignee_type: "squad",
      assignee_id: "updated-target",
      target_name: "Current Team",
    };
    await act(async () => client.setQueryData(quickActionKeys.list("ws"), { quick_actions: [updated] }));
    await waitFor(() => expect(screen.getByRole("button", { name: "/delayed-updated" })).toBeVisible());
    expect(editor.getText()).toBe("Keep this /delayed");

    fireEvent.keyDown(dom, { key: "Enter" });

    expect(editor.getText()).toBe("Keep this \n\n@Current Team\n\nUpdated feedback");
    expect(renderQuickAction).not.toHaveBeenCalled();
    act(() => { editor.commands.undo(); });
    expect(editor.getText()).toBe("Keep this /delayed");
  });

  it.each(["/", "/delayed"])(
    "refreshes %s after a delayed catalog arrives without another keystroke or editor reset",
    async (query) => {
      const pending = pendingCatalog();
      const { editor, dom } = await mount();
      await type(editor, query);
      expect(menuItem()).not.toBeInTheDocument();
      if (query === "/") expect(screen.getByRole("button", { name: /\/note/ })).toBeVisible();
      const selection = editor.state.selection.toJSON();

      await act(async () => pending.resolve({ quick_actions: [action] }));
      await waitFor(() => expect(menuItem()).toBeVisible());
      expect(dom.editor).toBe(editor);
      expect(editor.getText()).toBe(query);
      expect(editor.state.selection.toJSON()).toEqual(selection);

      fireEvent.click(menuItem()!);
      await waitFor(() => expect(editor.getText()).toBe("Rendered feedback"));
      expect(renderQuickAction).not.toHaveBeenCalled();
    },
  );

  it("refreshes an open menu when cached actions are renamed, archived or removed", async () => {
    listQuickActions.mockResolvedValue({ quick_actions: [action] });
    const { editor, client } = await mount([action]);
    await type(editor, "/delayed");
    await waitFor(() => expect(menuItem()).toBeVisible());

    const renamed = { ...action, name: "delayed-updated" };
    await act(async () => client.setQueryData(quickActionKeys.list("ws"), { quick_actions: [renamed] }));
    await waitFor(() => expect(screen.getByRole("button", { name: "/delayed-updated" })).toBeVisible());
    expect(menuItem()).not.toBeInTheDocument();

    await act(async () => client.setQueryData(quickActionKeys.list("ws"), { quick_actions: [{ ...action, status: "archived" }] }));
    await waitFor(() => expect(screen.queryByRole("button", { name: /\/delayed/ })).not.toBeInTheDocument());
    await act(async () => client.setQueryData(quickActionKeys.list("ws"), { quick_actions: [action] }));
    await waitFor(() => expect(menuItem()).toBeVisible());
    await act(async () => client.setQueryData(quickActionKeys.list("ws"), { quick_actions: [] }));
    await waitFor(() => expect(menuItem()).not.toBeInTheDocument());
    expect(editor.getText()).toBe("/delayed");
    expect(renderQuickAction).not.toHaveBeenCalled();
  });

  it("does not reopen an escaped menu when the pending catalog arrives", async () => {
    const pending = pendingCatalog();
    const { editor, dom, client } = await mount();
    await type(editor, "/");
    expect(screen.getByRole("button", { name: /\/note/ })).toBeVisible();
    fireEvent.keyDown(dom, { key: "Escape" });
    expect(screen.queryByRole("button", { name: /\/note/ })).not.toBeInTheDocument();

    await act(async () => pending.resolve({ quick_actions: [action] }));
    await waitFor(() => expect(client.getQueryState(quickActionKeys.list("ws"))?.status).toBe("success"));
    expect(menuItem()).not.toBeInTheDocument();
    expect(editor.getText()).toBe("/");
  });

  it("cleans up an active menu when unmounted before the catalog arrives", async () => {
    const pending = pendingCatalog();
    const { editor, unmount } = await mount();
    await type(editor, "/");
    expect(screen.getByRole("button", { name: /\/note/ })).toBeVisible();
    unmount();
    await waitFor(() => expect(editor.isDestroyed).toBe(true));

    await act(async () => pending.resolve({ quick_actions: [action] }));
    expect(menuItem()).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /\/note/ })).not.toBeInTheDocument();
  });
});

describe("Issue agent-scoped skills in the real editor", () => {
  it.each([false, true])("refreshes the open menu from a current preview (empty: %s)", async (empty) => {
    const assignee = makeAgent("assignee", [{ id: "fallback", name: "fallback-preview", description: "" }]);
    const target = makeAgent("target", [
      { id: "preview", name: "preview", description: "" },
      { id: "disabled", name: "disabled", description: "", enabled: false },
    ]);
    getIssue.mockResolvedValue(makeIssue("agent", assignee.id));
    listAgents.mockResolvedValue([assignee, target]);
    const { promise, resolve } = Promise.withResolvers<CommentTriggerPreview>();
    previewCommentTriggers.mockReturnValue(promise);
    const { editor, dom } = await mount();

    expect(previewCommentTriggers).not.toHaveBeenCalled();
    await type(editor, "/p");
    await waitFor(() => expect(screen.getByRole("button", { name: "/fallback-preview" })).toBeVisible());
    await waitFor(() => expect(previewCommentTriggers).toHaveBeenCalled());
    const selection = editor.state.selection.toJSON();

    await act(async () => resolve({
      agents: empty ? [] : [{ id: target.id, name: "Target", source: "assignee", reason: "" }],
      blocked: [],
    }));

    await waitFor(() => expect(screen.queryByRole("button", { name: "/fallback-preview" })).not.toBeInTheDocument());
    if (!empty) expect(screen.getByRole("button", { name: "/preview" })).toBeVisible();
    expect(screen.queryByRole("button", { name: "/disabled" })).not.toBeInTheDocument();
    expect(dom.editor).toBe(editor);
    expect(editor.getText()).toBe("/p");
    expect(editor.state.selection.toJSON()).toEqual(selection);
  });

  it("uses live synchronous targets while a changed mention is debouncing or loading", async () => {
    const previous = makeAgent("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", [{ id: "old", name: "old", description: "" }]);
    const next = makeAgent("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb", [{ id: "next", name: "next", description: "" }]);
    getIssue.mockResolvedValue(makeIssue("agent", previous.id));
    listAgents.mockResolvedValue([previous, next]);
    previewCommentTriggers.mockResolvedValueOnce({ agents: [
      { id: previous.id, name: "Previous", source: "assignee", reason: "" },
    ], blocked: [] });
    const { editor } = await mount();
    await type(editor, "draft");
    await waitFor(() => expect(previewCommentTriggers).toHaveBeenCalledTimes(1));
    await act(async () => {
      editor.commands.setContent("");
      editor.commands.insertContent([
        { type: "mention", attrs: { type: "agent", id: next.id, label: "Next", mentionSuggestionChar: "@" } },
        { type: "text", text: " " },
      ]);
    });
    await type(editor, "/ne");
    await waitFor(() => expect(screen.getByRole("button", { name: "/next" })).toBeVisible());
    expect(screen.queryByRole("button", { name: "/old" })).not.toBeInTheDocument();
    await waitFor(() => expect(previewCommentTriggers).toHaveBeenCalledTimes(2));
    expect(screen.getByRole("button", { name: "/next" })).toBeVisible();
  });

  it("uses a just-inserted mention and inserts the selected skill capsule as Markdown", async () => {
    const explicit = makeAgent("explicit", [
      { id: "deploy-skill", name: "deploy", description: "" },
    ]);
    const assignee = makeAgent("assignee", [
      { id: "fallback-skill", name: "fallback", description: "" },
    ]);
    getIssue.mockResolvedValue(makeIssue("agent", assignee.id));
    listAgents.mockResolvedValue([explicit, assignee]);
    listQuickActions.mockResolvedValue({ quick_actions: [action] });
    const { editor, dom } = await mount([action]);

    await act(async () => {
      editor.commands.insertContent([
        {
          type: "mention",
          attrs: {
            type: "agent",
            id: explicit.id,
            label: "Explicit",
            mentionSuggestionChar: "@",
          },
        },
        { type: "text", text: " " },
      ]);
    });
    await type(editor, "/d");

    await waitFor(() => expect(screen.getByRole("button", { name: "/deploy" })).toBeVisible());
    expect(screen.getByRole("button", { name: "/delayed-feedback" })).toBeVisible();
    expect(screen.queryByRole("button", { name: /\/note/ })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "/fallback" })).not.toBeInTheDocument();

    fireEvent.keyDown(dom, { key: "ArrowDown" });
    fireEvent.keyDown(dom, { key: "Enter" });

    expect(editor.getMarkdown()).toBe(
      "[@Explicit](mention://agent/explicit) [/deploy](slash://skill/deploy-skill) ",
    );
  });

  it("keeps /note selection as a plain-text prefix", async () => {
    const { editor, dom } = await mount();
    await type(editor, "/no");
    await waitFor(() => expect(screen.getByRole("button", { name: /\/note/ })).toBeVisible());

    fireEvent.keyDown(dom, { key: "Enter" });

    expect(editor.getText()).toBe("/note ");
  });

  it.each(["/usr/local/bin", "2026/11/16"])(
    "renders no empty menu for ordinary prose %s",
    async (text) => {
      const { editor } = await mount();

      await type(editor, text);

      expect(screen.queryByRole("button", { name: /\/note/ })).not.toBeInTheDocument();
      expect(screen.queryByText("No matching skills")).not.toBeInTheDocument();
    },
  );

  it("refreshes an open menu on agent and Issue cache updates without resetting the editor", async () => {
    const first = makeAgent("first", [{ id: "one", name: "target-one", description: "" }]);
    const second = makeAgent("second", [{ id: "two", name: "target-two", description: "" }]);
    getIssue.mockResolvedValue(makeIssue("agent", first.id));
    listAgents.mockResolvedValue([first, second]);
    const { client, dom, editor } = await mount();
    await type(editor, "/target");
    await waitFor(() => expect(screen.getByRole("button", { name: "/target-one" })).toBeVisible());

    await act(async () => client.setQueryData(workspaceKeys.agents("ws"), [
      makeAgent("first", [{ id: "updated", name: "target-updated", description: "" }]),
      second,
    ]));
    await waitFor(() => expect(screen.getByRole("button", { name: "/target-updated" })).toBeVisible());
    expect(screen.queryByRole("button", { name: "/target-one" })).not.toBeInTheDocument();

    await act(async () => client.setQueryData(
      issueKeys.detail("ws", "issue-1"),
      makeIssue("agent", second.id),
    ));
    await waitFor(() => expect(screen.getByRole("button", { name: "/target-two" })).toBeVisible());
    expect(screen.queryByRole("button", { name: "/target-updated" })).not.toBeInTheDocument();
    expect(dom.editor).toBe(editor);
    expect(editor.getText()).toBe("/target");
  });
});

describe("Issue composer slash query lifecycle", () => {
  const skills = [
    { id: "code-review", name: "code-review", description: "" },
    { id: "write-tests", name: "write-tests", description: "" },
    ...Array.from({ length: 13 }, (_, index) => ({
      id: `extra-${index}`,
      name: `extra-${index}`,
      description: "",
    })),
  ];
  const requestChanges = { ...action, id: "request-changes", name: "Request changes" };

  async function expectOnlyQuickActionsAndNote() {
    await waitFor(() => {
      expect(screen.getByRole("button", { name: "/Request changes" })).toBeVisible();
      expect(screen.getByRole("button", { name: /^\/note/ })).toBeVisible();
      expect(document.querySelector("[data-kind='slash']")?.querySelectorAll("button")).toHaveLength(2);
    });
  }

  it.each(["comment", "reply"] as const)(
    "hides skills for bare and non-matching queries and restores the bare menu in the %s composer",
    async (composer) => {
      const target = makeAgent("target", skills);
      getIssue.mockResolvedValue(makeIssue("agent", target.id));
      listAgents.mockResolvedValue([target]);
      listQuickActions.mockResolvedValue({ quick_actions: [requestChanges] });
      const { editor } = await mount([requestChanges], composer);

      await type(editor, "/");
      await expectOnlyQuickActionsAndNote();
      expect(screen.queryByRole("button", { name: "/code-review" })).not.toBeInTheDocument();
      expect(screen.queryByRole("button", { name: "/write-tests" })).not.toBeInTheDocument();

      await type(editor, "co");
      await waitFor(() => expect(screen.getByRole("button", { name: "/code-review" })).toBeVisible());
      expect(document.querySelector("[data-kind='slash']")?.querySelectorAll("button")).toHaveLength(1);
      fireEvent.click(screen.getByRole("button", { name: "/code-review" }));
      expect(editor.getMarkdown()).toBe("[/code-review](slash://skill/code-review) ");

      await type(editor, "/zzzz");
      await waitFor(() => expect(document.querySelector("[data-kind='slash']")).not.toBeInTheDocument());

      await act(async () => {
        const { from, to } = editor.state.selection;
        editor.commands.deleteRange({ from: from - 4, to });
      });
      await expectOnlyQuickActionsAndNote();
      expect(screen.queryByRole("button", { name: "/code-review" })).not.toBeInTheDocument();
      expect(screen.queryByRole("button", { name: "/write-tests" })).not.toBeInTheDocument();
    },
  );
});
