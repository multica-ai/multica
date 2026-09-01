import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { DictationProvider } from "@multica/core/platform/dictation";
import { useCommentDraftStore } from "@multica/core/issues/stores";
import { WorkspaceSlugProvider } from "@multica/core/paths";
import { workspaceKeys } from "@multica/core/workspace/queries";
import { renderWithI18n } from "../../test/i18n";
import { CommentInput } from "./comment-input";
import { ReplyInput } from "./reply-input";

vi.mock("@multica/core/api", () => ({
  api: {
    getBaseUrl: () => "https://fixture.invalid",
    listWorkspaces: vi.fn().mockResolvedValue([{ id: "test-ws", slug: "test", name: "Test" }]),
    listMembers: vi.fn().mockResolvedValue([]),
    listAgents: vi.fn().mockResolvedValue([]),
    listSquads: vi.fn().mockResolvedValue([]),
    listQuickActions: vi.fn().mockResolvedValue({ quick_actions: [] }),
    previewCommentTriggers: vi.fn().mockResolvedValue({ agents: [], blocked: [] }),
  },
}));
vi.mock("../../common/actor-avatar", () => ({ ActorAvatar: () => <span /> }));

// The composer, lazy controller, mic and Tiptap editor are real. Only the host
// adapter and unrelated API/avatar plumbing are fake: no account, native
// shortcut or microphone is accessed.
describe("native dictation in real lazy composers", () => {
  beforeEach(() => {
    vi.spyOn(window, "scrollBy").mockImplementation(() => {});
    useCommentDraftStore.setState({ drafts: {} });
    vi.spyOn(HTMLElement.prototype, "getClientRects").mockImplementation(function (this: HTMLElement) {
      return (this.closest(".hidden") ? [] : [new DOMRect(0, 0, 300, 40)]) as unknown as DOMRectList;
    });
  });
  afterEach(() => { cleanup(); vi.restoreAllMocks(); });

  it.each(["comment", "reply"])("mounts the %s editor from the mic and keeps click/Enter separate from submit", async (kind) => {
    const submit = vi.fn().mockResolvedValue(true);
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    client.setQueryData(workspaceKeys.list(), [{ id: "test-ws", slug: "test", name: "Test" }]);
    const toggle = vi.fn(async () => {
      expect(document.activeElement).toHaveAttribute("contenteditable", "true");
      expect(document.activeElement?.closest(".hidden")).toBeNull();
      return { ok: true as const, shortcut: "Ctrl+Alt+Shift+D" };
    });
    const { container, unmount } = renderWithI18n(
      <QueryClientProvider client={client}>
        <WorkspaceSlugProvider slug="test">
        <DictationProvider adapter={{ toggle }}>
          {kind === "comment" ? <CommentInput issueId="test-issue" onSubmit={submit} /> : (
            <ReplyInput issueId="test-issue" parentId="test-comment" avatarType="member" avatarId="test-user" onSubmit={submit} />
          )}
        </DictationProvider>
        </WorkspaceSlugProvider>
      </QueryClientProvider>,
    );
    expect(container.querySelector("[contenteditable=true]")).toBeNull();
    const mic = screen.getByRole("button", { name: "Dictate with Codex" });
    expect(mic).toHaveAttribute("data-native-dictation");
    fireEvent.click(mic);
    await waitFor(() => expect(toggle).toHaveBeenCalledOnce());
    expect(submit).not.toHaveBeenCalled();

    await waitFor(() => expect(mic).not.toBeDisabled());
    mic.focus();
    const user = userEvent.setup();
    await user.keyboard("{Enter>}");
    expect(toggle).toHaveBeenCalledOnce();
    await user.keyboard("{/Enter}");
    await waitFor(() => expect(toggle).toHaveBeenCalledTimes(2));
    expect(submit).not.toHaveBeenCalled();
    expect(container.querySelector("[contenteditable=true]")?.textContent).toBe("");
    unmount();
    client.clear();
  });
});
