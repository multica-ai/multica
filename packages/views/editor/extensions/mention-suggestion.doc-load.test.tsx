import { act, cleanup, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { Editor, Extension } from "@tiptap/core";
import { EditorContent } from "@tiptap/react";
import StarterKit from "@tiptap/starter-kit";
import { Suggestion } from "@tiptap/suggestion";
import { I18nProvider } from "@multica/core/i18n/react";
import { docKeys } from "@multica/core/docs";
import { createMentionSuggestion } from "./mention-suggestion";
import { SuggestionTriggerArmingExtension } from "./suggestion-trigger-arming";
import enDocs from "../../locales/en/docs.json";
import enEditor from "../../locales/en/editor.json";
import enIssues from "../../locales/en/issues.json";

// The first `@` in a session loads the doc tree; the open menu must show the
// docs once it arrives, without another keystroke (PR #10926 review).
const apiMock = vi.hoisted(() => ({
  listDocTree: vi.fn(), moveDoc: vi.fn(), moveDocFolder: vi.fn(),
  searchIssues: vi.fn(), searchProjects: vi.fn(),
}));
vi.mock("@multica/core/api", () => ({ api: apiMock }));
vi.mock("@multica/core/hooks", async (original) => ({
  ...await original<object>(), useWorkspaceId: () => "review-ws",
}));
vi.mock("@multica/core/platform", async (original) => ({
  ...await original<object>(), getCurrentWsId: () => "review-ws",
}));
vi.mock("@multica/core/auth", () => ({
  useAuthStore: Object.assign(vi.fn(), { getState: () => ({ user: { id: "reviewer" } }) }),
}));
vi.mock("@multica/core/paths", async (original) => ({
  ...await original<object>(), useWorkspacePaths: () => ({
    docs: () => "/review/docs", docDetail: (id: string) => `/review/docs/${id}`,
    docFolder: (id: string) => `/review/docs/folders/${id}`,
  }),
}));
vi.mock("../../navigation", () => ({
  useNavigation: () => ({ pathname: "/review/docs", push: vi.fn() }),
  AppLink: ({ children, href }: { children: React.ReactNode; href: string }) => <a href={href}>{children}</a>,
}));
vi.mock("@multica/core/issue-statuses/hooks", () => ({
  useIssueStatuses: () => ({ iconOf: () => null, colorOf: () => null }),
}));
vi.mock("../../common/actor-avatar", () => ({ ActorAvatar: () => null }));

const tree = {
  folders: [
    { id: "parent", title: "Parent", parentId: null },
    { id: "child", title: "Child", parentId: "parent" },
    { id: "grandchild", title: "Grandchild", parentId: "child" },
    { id: "other", title: "Other", parentId: null },
  ],
  docs: [{ id: "runbook", title: "Release runbook", folderId: "child", updatedAt: "2026-11-18T00:00:00Z" }],
};
let qc: QueryClient;
let editor: Editor | undefined;
beforeEach(() => {
  vi.clearAllMocks();
  qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  apiMock.listDocTree.mockResolvedValue(tree);
    apiMock.searchIssues.mockResolvedValue({ issues: [] });
  apiMock.searchProjects.mockResolvedValue({ projects: [] });
});
afterEach(() => { act(() => editor?.destroy()); editor = undefined; cleanup(); qc.clear(); });

function mount(children: React.ReactNode) {
  return render(<QueryClientProvider client={qc}><I18nProvider locale="en" resources={{ en: { docs: enDocs, editor: enEditor, issues: enIssues } }}>{children}</I18nProvider></QueryClientProvider>);
}

it("first @ shows recent docs after the request resolves, without another keystroke", async () => {
  let resolve!: (value: typeof tree) => void;
  apiMock.listDocTree.mockReturnValue(new Promise((r) => { resolve = r; }));
  const suggestion = createMentionSuggestion(qc);
  const probe = Extension.create({
    name: "reviewMentionProbe",
    addProseMirrorPlugins() { return [Suggestion({ ...suggestion, editor: this.editor })]; },
  });
  editor = new Editor({ extensions: [StarterKit, SuggestionTriggerArmingExtension, probe], content: "" });
  mount(<EditorContent editor={editor} />);
  await act(async () => {
    editor!.commands.focus("end");
    const { from, to } = editor!.state.selection;
    const handled = editor!.view.someProp("handleTextInput", (fn) =>
      fn(editor!.view, from, to, "@", () => editor!.state.tr.insertText("@", from, to)),
    );
    if (!handled) editor!.view.dispatch(editor!.state.tr.insertText("@", from, to));
  });
  await screen.findByRole("button", { name: "All members" });
  expect(apiMock.listDocTree).toHaveBeenCalledOnce();
  await act(async () => { resolve(tree); });
  await waitFor(() => expect(qc.getQueryData(docKeys.tree("review-ws"))).toEqual(tree));
  expect(await screen.findByRole("button", { name: "Release runbook" })).toBeInTheDocument();
});
