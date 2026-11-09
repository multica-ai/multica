import { act, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useState } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Editor } from "@tiptap/core";
import { configureShortcutPlatform, createShortcutChord, useShortcutStore } from "@multica/core/shortcuts";
import { WorkspaceSlugProvider } from "@multica/core/paths";
import { useCommentDraftStore } from "@multica/core/issues/stores";
import { composeAnnotatedReply, type ReplyAnnotation } from "@multica/core/drafts/reply-annotation";
import { toast } from "sonner";
import { renderWithI18n } from "../../test/i18n";
import { useCommentAnnotations } from "./use-comment-annotations";
import { ReplyInput } from "./reply-input";
import { CommentInput } from "./comment-input";

// Avatar profile navigation is unrelated to composer behavior.
vi.mock("../../common/actor-avatar", () => ({ ActorAvatar: () => null, AgentStatusDot: () => null }));

const preview = vi.hoisted(() => vi.fn<typeof import("@multica/core/api")["api"]["previewCommentTriggers"]>(async () => ({ agents: [], blocked: [] })));
vi.mock("@multica/core/api", async (original) => {
  const actual = await original<typeof import("@multica/core/api")>();
  return { ...actual, api: { ...actual.api, getBaseUrl: () => "https://api.example.test", previewCommentTriggers: preview, listTasksByIssue: vi.fn(async () => []),
    listWorkspaces: vi.fn(async () => [{ id: "workspace", slug: "test", name: "Test" }]),
    listMembers: vi.fn(async () => []), listAgents: vi.fn(async () => []), listSquads: vi.fn(async () => []),
    listQuickActions: vi.fn(async () => ({ quick_actions: [] })) } };
});

const key = "reply:issue:root" as const;
type Submit = (content: string, attachments?: string[], suppress?: string[], steer?: string[]) => Promise<string | boolean>;
function Fixture({ deleted = false, description = false, onSubmit = async () => false }: {
  deleted?: boolean; description?: boolean; onSubmit?: Submit;
}) {
  const [client] = useState(() => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    client.setQueryData(["workspaces", "list"], [{ id: "workspace", slug: "test", name: "Test" }]);
    return client;
  });
  const annotations = useCommentAnnotations({ issueId: "issue", draftKey: description ? "new:issue" : key, enabled: true,
    sources: [{ id: "root", name: "Root" }, ...(!deleted ? [{ id: "child", name: "Child" }] : [])],
  });
  const replyTarget = useCommentDraftStore((s) => s.drafts[key]?.replyTarget);
  return <QueryClientProvider client={client}><WorkspaceSlugProvider slug="test"><div ref={annotations.cardRef} {...annotations.captureProps}>
    {annotations.popup}
    <div data-comment-content="root" tabIndex={0}>Root text</div>
    {!deleted && <div data-comment-content="child" tabIndex={0}>Child text</div>}
    {description ? <CommentInput issueId="issue" onSubmit={onSubmit} annotationComposer={annotations.composer}
      onEditAnnotation={annotations.editAnnotation} /> : <ReplyInput issueId="issue" parentId={replyTarget?.commentId ?? "root"} draftKey={key}
      targetMissing={deleted && replyTarget?.commentId === "child"}
      avatarType="member" avatarId="" onSubmit={onSubmit} annotationComposer={annotations.composer}
      onEditAnnotation={annotations.editAnnotation} />}
  </div></WorkspaceSlugProvider></QueryClientProvider>;
}

function saved(id: string, quote = "Root text"): ReplyAnnotation {
  return { id, sourceCommentId: "root", sourceActorName: "Root", quote, start: 0, prefix: "", suffix: "", note: `Saved ${id}` };
}
function selectText(container: HTMLElement, id = "child") {
  const source = container.querySelector<HTMLElement>(`[data-comment-content="${id}"]`)!;
  fireEvent.pointerDown(source);
  const range = document.createRange(); range.selectNodeContents(source);
  act(() => { window.getSelection()!.removeAllRanges(); window.getSelection()!.addRange(range); });
  fireEvent.pointerUp(source);
}
async function open(container: HTMLElement, text = "Please revise") {
  selectText(container);
  fireEvent.click(await screen.findByRole("button", { name: "Add to reply" }));
  const input = await screen.findByRole("textbox", { name: "Comment" });
  await type(input, text);
  return input;
}
async function type(input: HTMLElement, text: string) {
  await act(async () => { (input as HTMLElement & { editor: Editor }).editor.commands.setContent(text, { contentType: text.startsWith("<") ? "html" : "markdown" }); });
  await waitFor(() => expect(screen.getByRole("button", { name: /^(Add|Save) annotation$/ })).toBeEnabled());
}
function popup(input: HTMLElement) { return within(input.closest("[data-reply-annotation-overlay]")! as HTMLElement); }

beforeEach(() => {
  useCommentDraftStore.setState({ drafts: {} });
  configureShortcutPlatform("windows");
  preview.mockReset();
  preview.mockResolvedValue({ agents: [], blocked: [] });
  Object.defineProperty(navigator, "clipboard", { configurable: true, value: { writeText: vi.fn(async () => {}) } });
  Object.defineProperty(document.documentElement, "clientWidth", { configurable: true, value: 1024 });
  Object.defineProperty(document.documentElement, "clientHeight", { configurable: true, value: 768 });
  vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue(new DOMRect(10, 10, 600, 400));
  Range.prototype.getBoundingClientRect = function () { return this.collapsed ? new DOMRect() : new DOMRect(10, 10, 120, 20); };
  Range.prototype.getClientRects = function () { return [this.getBoundingClientRect()] as unknown as DOMRectList; };
});
afterEach(() => { vi.restoreAllMocks(); useShortcutStore.getState().resetAll(); configureShortcutPlatform(null); });

describe("annotation draft submission through the real composer", () => {
  it.each([false, true])("sends all saved annotations and body through the existing composer (description: %s)", async (description) => {
    const draftKey = description ? "new:issue" as const : key;
    const store = useCommentDraftStore.getState();
    const first = saved("one"), second = saved("two", "Other quote");
    store.setDraft(draftKey, "Overall draft");
    store.addAnnotation(draftKey, first); store.addAnnotation(draftKey, second);
    const onSubmit = vi.fn<Submit>(async () => "published");
    const view = renderWithI18n(<Fixture onSubmit={onSubmit} description={description} />);
    const input = await open(view.container, "[@Agent](mention://agent/11111111-1111-1111-1111-111111111111) Please revise");
    const pending = { ...saved("new", "Child text"), sourceCommentId: "child", sourceActorName: "Child", note: input.textContent! };
    const expected = composeAnnotatedReply("Overall draft", [first, second, { ...pending, note: "[@Agent](mention://agent/11111111-1111-1111-1111-111111111111) Please revise" }]);
    await waitFor(() => expect(preview).toHaveBeenCalledWith("issue", expected, description ? undefined : "root", undefined));
    expect(popup(input).getByText("Will send with saved annotations (2) and draft text.")).toBeVisible();
    fireEvent.keyDown(input, { key: "Enter", ctrlKey: true });
    await waitFor(() => expect(onSubmit).toHaveBeenCalledWith(expected, undefined, undefined, undefined));
    await waitFor(() => expect(useCommentDraftStore.getState().drafts[draftKey]).toBeUndefined());
    expect(screen.queryByRole("textbox", { name: "Comment" })).not.toBeInTheDocument();
    // Acceptance must also release the selection dismissal/submission lock.
    const next = await open(view.container, "A second note");
    fireEvent.click(popup(next).getByRole("button", { name: "Send" }));
    await waitFor(() => expect(onSubmit).toHaveBeenCalledTimes(2));
  });

  it("uses Enter to save and Shift+Enter for a newline despite the Enter-send preference", async () => {
    useShortcutStore.getState().setShortcut("send", createShortcutChord("Enter"));
    const onSubmit = vi.fn(async () => true);
    const view = renderWithI18n(<Fixture onSubmit={onSubmit} />);
    const input = await open(view.container, "First line");
    await waitFor(() => expect(preview).toHaveBeenCalledWith("issue", "> Child text\n\nFirst line", "child", undefined));
    const before = (input as HTMLElement & { editor: Editor }).editor.getText();
    fireEvent.keyDown(input, { key: "Enter", isComposing: true });
    fireEvent.keyDown(input, { key: "Enter", keyCode: 229 });
    expect(useCommentDraftStore.getState().getAnnotations(key)).toHaveLength(0);
    fireEvent.keyDown(input, { key: "Enter", shiftKey: true });
    expect((input as HTMLElement & { editor: Editor }).editor.getText()).not.toBe(before);
    fireEvent.keyDown(input, { key: "Enter" });
    await waitFor(() => expect(useCommentDraftStore.getState().getAnnotations(key)).toHaveLength(1));
    expect(onSubmit).not.toHaveBeenCalled();
    fireEvent.click(await screen.findByRole("button", { name: "Edit annotation 1" }));
    const edit = await screen.findByRole("textbox", { name: "Comment" });
    await type(edit, "Updated existing note");
    fireEvent.keyDown(edit, { key: "Enter", ctrlKey: true });
    await waitFor(() => expect(onSubmit).toHaveBeenCalledWith("> Child text\n\nUpdated existing note", undefined, undefined, undefined));
  });

  it("keeps failed sends in the persisted draft and retries without duplicating the annotation", async () => {
    let finish!: (result: boolean) => void;
    const onSubmit = vi.fn(() => new Promise<boolean>((resolve) => { finish = resolve; }));
    const view = renderWithI18n(<Fixture onSubmit={onSubmit} />);
    const input = await open(view.container, "Keep on failure");
    fireEvent.keyDown(input, { key: "Enter", ctrlKey: true });
    fireEvent.keyDown(input, { key: "Enter", ctrlKey: true });
    fireEvent.pointerDown(document.body); fireEvent.keyDown(input, { key: "Escape" });
    expect(input).toBeInTheDocument();
    await waitFor(() => expect(onSubmit).toHaveBeenCalledTimes(1));
    expect(useCommentDraftStore.getState().getAnnotations(key)[0]?.note).toBe("Keep on failure");
    await act(async () => finish(false));
    expect(popup(input).getByRole("button", { name: "Send" })).toBeEnabled();
    fireEvent.pointerDown(document.body);
    view.unmount();
    renderWithI18n(<Fixture onSubmit={async () => "retry-accepted"} />);
    fireEvent.click(await screen.findByRole("button", { name: "Edit annotation 1" }));
    const recovered = await screen.findByRole("textbox", { name: "Comment" });
    expect(recovered).toHaveTextContent("Keep on failure");
    fireEvent.keyDown(recovered, { key: "Enter", ctrlKey: true });
    await waitFor(() => expect(useCommentDraftStore.getState().drafts[key]).toBeUndefined());
  });

  it("keeps a deleted non-target source's quote while the first annotation's target survives", async () => {
    const first = saved("first");
    useCommentDraftStore.getState().addAnnotation(key, first);
    const onSubmit = vi.fn<Submit>(async () => "posted");
    const view = renderWithI18n(<Fixture onSubmit={onSubmit} />);
    const input = await open(view.container, "Retain this instruction");
    view.rerender(<Fixture deleted onSubmit={onSubmit} />);
    expect(await screen.findByRole("alert")).toHaveTextContent("The source changed or is unavailable");
    expect(input).toHaveTextContent("Retain this instruction");
    fireEvent.keyDown(input, { key: "Enter", ctrlKey: true });
    await waitFor(() => expect(onSubmit).toHaveBeenCalledWith(composeAnnotatedReply("", [first, { ...saved("pending", "Child text"), sourceCommentId: "child", note: "Retain this instruction" }]), undefined, undefined, undefined));
  });

  it("blocks an extra annotation at the quota, while an existing annotation can still be saved and sent", async () => {
    const store = useCommentDraftStore.getState();
    for (let i = 0; i < 20; i++) store.addAnnotation(key, saved(`quota-${i}`, i ? `Quote ${i}` : "Root text"));
    const onSubmit = vi.fn<Submit>(async () => "posted");
    const view = renderWithI18n(<Fixture onSubmit={onSubmit} />);
    selectText(view.container);
    fireEvent.click(await screen.findByRole("button", { name: "Add to reply" }));
    const input = await screen.findByRole("textbox", { name: "Comment" });
    await act(async () => { (input as HTMLElement & { editor: Editor }).editor.commands.setContent("Over quota"); });
    await waitFor(() => expect(popup(input).getByRole("button", { name: "Add annotation" })).toBeDisabled());
    expect(popup(input).getByRole("button", { name: "Send all" })).toBeDisabled();
    fireEvent.keyDown(input, { key: "Enter", ctrlKey: true });
    expect(onSubmit).not.toHaveBeenCalled();
    fireEvent.keyDown(input, { key: "Escape" });
    fireEvent.click(await screen.findByRole("button", { name: "Edit annotation 1" }));
    const edit = await screen.findByRole("textbox", { name: "Comment" });
    await type(edit, "Revised at quota");
    fireEvent.keyDown(edit, { key: "Enter", ctrlKey: true });
    await waitFor(() => expect(onSubmit).toHaveBeenCalledTimes(1));
    expect(onSubmit.mock.calls[0]![0]).toContain("Revised at quota");
    expect(onSubmit.mock.calls[0]![0]).not.toContain("Over quota");
  });

  it.each(["popup", "lower"])("requires matching targets before applying recipient choices (send: %s)", async (sendFrom) => {
    const showError = vi.spyOn(toast, "error").mockReturnValue("test-toast");
    const agentId = "11111111-1111-1111-1111-111111111111";
    preview.mockResolvedValue({ agents: [{ id: agentId, name: "Agent", source: "thread_parent", reason: "" }] });
    if (sendFrom === "lower") useCommentDraftStore.getState().setDraft(key, "Root reply body");
    const onSubmit = vi.fn<Submit>(async () => "posted");
    const view = renderWithI18n(<Fixture onSubmit={onSubmit} />);
    const input = await open(view.container, "Please revise");
    const chip = await popup(input).findByRole("button", { name: "Agent trigger: Will start when sent" });
    fireEvent.pointerDown(chip); fireEvent.click(chip);
    const skip = await screen.findByRole("menuitemradio", { name: /Won't start this time/ });
    fireEvent.pointerDown(skip); fireEvent.click(skip);
    expect(input).toBeInTheDocument();
    await waitFor(() => expect(screen.getAllByRole("button", { name: "Agent trigger: Won't start this time" })).toHaveLength(2));
    if (sendFrom === "popup") fireEvent.keyDown(input, { key: "Enter", ctrlKey: true });
    else {
      fireEvent.click(within(view.container).getByRole("button", { name: "Send" }));
      await waitFor(() => expect(showError).toHaveBeenCalledWith("Reply target changed. Review the recipients before sending again."));
      expect(onSubmit).not.toHaveBeenCalled();
      expect(useCommentDraftStore.getState().getDraft(key)).toBe("Root reply body");
      expect(useCommentDraftStore.getState().getAnnotations(key)).toHaveLength(0);
      expect(input).toHaveTextContent("Please revise");
      // Return to the root reply and explicitly confirm its recipient choice.
      fireEvent.keyDown(input, { key: "Escape" });
      const rootChip = await within(view.container).findByRole("button", { name: "Agent trigger: Will start when sent" });
      fireEvent.click(rootChip);
      fireEvent.click(await screen.findByRole("menuitemradio", { name: /Won't start this time/ }));
      fireEvent.click(within(view.container).getByRole("button", { name: "Send" }));
    }
    await waitFor(() => expect(onSubmit).toHaveBeenCalledWith(sendFrom === "popup" ? "> Child text\n\nPlease revise" : "Root reply body", undefined, [agentId], undefined));
  });

  it("saves the note but respects the lower composer's in-flight upload gate", async () => {
    const onSubmit = vi.fn<Submit>(async () => "posted");
    const view = renderWithI18n(<Fixture onSubmit={onSubmit} />);
    const input = await open(view.container, "Wait for the file");
    await act(async () => { useCommentDraftStore.getState().addUpload(key, {
      clientUploadId: "upload", status: "uploading", filename: "pending.txt", size: 10,
    }); });
    expect(popup(input).getByRole("button", { name: "Send" })).toBeDisabled();
    fireEvent.keyDown(input, { key: "Enter", ctrlKey: true });
    await waitFor(() => expect(useCommentDraftStore.getState().getAnnotations(key)).toHaveLength(1));
    expect(onSubmit).not.toHaveBeenCalled();
    await act(async () => { useCommentDraftStore.getState().removeUpload(key, "upload"); });
    fireEvent.keyDown(input, { key: "Enter", ctrlKey: true });
    await waitFor(() => expect(onSubmit).toHaveBeenCalledTimes(1));
  });

  it.each([false, true])("keeps the note when copying its hovered link (clipboard fallback: %s)", async (fallback) => {
    if (fallback) {
      Object.defineProperty(navigator, "clipboard", { configurable: true, value: undefined });
      Object.defineProperty(document, "execCommand", { configurable: true, value: vi.fn(() => true) });
    }
    const view = renderWithI18n(<Fixture />);
    const input = await open(view.container, '<p>Please inspect <a href="https://example.com">this page</a></p>');
    fireEvent.mouseOver(input.querySelector("a")!);
    const copy = await screen.findByRole("button", { name: "Copy link" });
    fireEvent.pointerDown(copy); copy.focus(); fireEvent.click(copy);
    await waitFor(() => expect(fallback ? document.execCommand : navigator.clipboard.writeText).toHaveBeenCalledWith(fallback ? "copy" : "https://example.com"));
    expect(input).toBeInTheDocument();
    expect(input).toHaveTextContent("Please inspect this page");
  });
});
