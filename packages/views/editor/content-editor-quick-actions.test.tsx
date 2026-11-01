import type { Editor } from "@tiptap/core";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import type { ListQuickActionsResponse, QuickAction } from "@multica/core/types";
import { quickActionKeys } from "@multica/core/quick-actions";
import { renderWithI18n } from "../test/i18n";
import { NavigationProvider } from "../navigation/context";
import { useQuickActionMenu } from "../issues/hooks/use-quick-action-menu";
import { ContentEditor } from "./content-editor";

const listQuickActions = vi.hoisted(() => vi.fn());
const renderQuickAction = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/api")>();
  return { ...actual, api: { ...actual.api, listQuickActions, renderQuickAction } };
});
vi.mock("@multica/core/paths", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/paths")>()),
  useCurrentWorkspace: () => ({ id: "ws", slug: "test" }),
  useWorkspaceSlug: () => "test",
  useWorkspacePaths: () => ({ issueDetail: (id: string) => `/test/issues/${id}` }),
}));

beforeAll(() => {
  // jsdom has no layout; ProseMirror measures the caret while opening a picker.
  const rects = () => ({
    length: 0,
    item: () => null,
    [Symbol.iterator]: function* () {},
  }) as DOMRectList;
  Range.prototype.getBoundingClientRect = () => new DOMRect();
  Range.prototype.getClientRects = rects;
  HTMLElement.prototype.getClientRects = rects;
  Object.defineProperty(Text.prototype, "getClientRects", { configurable: true, value: rects });
});

beforeEach(() => {
  listQuickActions.mockReset();
  renderQuickAction.mockReset().mockResolvedValue("Rendered feedback");
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
  return (
    <ContentEditor
      quickActionMenu={useQuickActionMenu("issue")}
      enableSlashCommands
      slashCommandMode="command"
      showBubbleMenu={false}
    />
  );
}

async function mount(initialActions?: QuickAction[]) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } });
  if (initialActions) {
    client.setQueryData(quickActionKeys.list("ws"), { quick_actions: initialActions });
  }
  clients.push(client);
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
      <QueryClientProvider client={client}><Composer /></QueryClientProvider>
    </NavigationProvider>,
  );
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

describe("comment quick-action catalog updates in the real editor", () => {
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
      expect(renderQuickAction).toHaveBeenCalledExactlyOnceWith("issue", "action");
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
