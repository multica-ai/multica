import { act, fireEvent, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useCommentDraftStore } from "@multica/core/issues/stores";
import type { TimelineEntry } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { useCommentAnnotations } from "./use-comment-annotations";
import { Profiler } from "react";
import { toast } from "sonner";

vi.mock("sonner", () => ({ toast: { error: vi.fn() } }));

const entry: TimelineEntry = { type: "comment", id: "root", actor_type: "agent", actor_id: "emacs", content: "Selected text", created_at: "2026-09-10T00:00:00Z" };
const key = "reply:issue:root" as const;

function Fixture({ actorType = "agent", sourceKey = "initial" }: { actorType?: string; sourceKey?: string }) {
  const annotation = useCommentAnnotations({
    draftKey: key, sources: [{ id: entry.id, name: actorType }], enabled: true,
  });
  return <div ref={annotation.cardRef} {...annotation.captureProps}>
    {annotation.popup}
    <div key={sourceKey} data-comment-content="root" tabIndex={0}>Selected text</div>
  </div>;
}

function DescriptionFixture({ issueId = "issue", loaded = true }: { issueId?: string; loaded?: boolean }) {
  const sourceId = `description:${issueId}`;
  const annotations = useCommentAnnotations({
    draftKey: `new:${issueId}`, sources: [{ id: sourceId, name: "Description" }], enabled: loaded, editable: true,
  });
  if (!loaded) return null;
  return <div ref={annotations.cardRef} {...annotations.captureProps}>
    {annotations.popup}
    <div data-comment-content={sourceId}><div contentEditable suppressContentEditableWarning>Selected text</div></div>
    <button onMouseDown={(event) => event.preventDefault()} onClick={annotations.addSelection}>Add to comment</button>
  </div>;
}

function selectText(container: HTMLElement, input: "mouse" | "keyboard" = "mouse") {
  const source = container.querySelector<HTMLElement>("[data-comment-content]")!;
  if (input === "mouse") {
    fireEvent.pointerDown(source, { pointerType: "mouse", button: 0 });
    fireEvent.mouseDown(source, { button: 0 });
  } else {
    source.focus();
    fireEvent.keyDown(source, { key: "ArrowRight", shiftKey: true });
  }
  const range = document.createRange();
  range.selectNodeContents(source);
  act(() => {
    window.getSelection()!.removeAllRanges();
    window.getSelection()!.addRange(range);
  });
  if (input === "mouse") {
    fireEvent.pointerUp(source);
    fireEvent.mouseUp(source, { button: 0 });
    fireEvent.click(source, { button: 0 });
  } else {
    fireEvent.keyUp(source, { key: "ArrowRight", shiftKey: true });
  }
  return source;
}

beforeEach(() => {
  useCommentDraftStore.setState({ drafts: {} });
  // jsdom has no layout; give Floating UI a viewport for its real hide middleware.
  Object.defineProperty(document.documentElement, "clientWidth", { configurable: true, value: 1024 });
  Object.defineProperty(document.documentElement, "clientHeight", { configurable: true, value: 768 });
  vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue(new DOMRect(10, 10, 600, 400));
  Range.prototype.getBoundingClientRect = vi.fn(() => new DOMRect(10, 10, 120, 20));
  Range.prototype.getClientRects = vi.fn(() => [] as unknown as DOMRectList);
});

describe("selection to reply", () => {
  it.each([false, true])("does not rerender the host when unannotated source text changes (description: %s)", async (description) => {
    const onRender = vi.fn();
    const { container } = renderWithI18n(<Profiler id="host" onRender={onRender}>
      {description ? <DescriptionFixture /> : <Fixture />}
    </Profiler>);
    const count = onRender.mock.calls.length;
    const source = container.querySelector(description ? "[contenteditable]" : "[data-comment-content]")!;
    for (let i = 0; i < 10; i++) {
      await act(async () => { source.firstChild!.textContent += "x"; });
    }
    expect(onRender).toHaveBeenCalledTimes(count);
  });

  it("reports an uncapturable description selection without adding a draft", () => {
    renderWithI18n(<DescriptionFixture />);
    window.getSelection()?.removeAllRanges();
    fireEvent.click(screen.getByRole("button", { name: "Add to comment" }));
    expect(toast.error).toHaveBeenCalledWith("Could not capture the selection. Select text within one comment or description and try again.");
    expect(useCommentDraftStore.getState().getAnnotations("new:issue")).toHaveLength(0);
  });

  it.each([false, true])("removes a saved annotation at its source and preserves the rest of the draft (description: %s)", async (description) => {
    const draftKey = description ? "new:issue" as const : key;
    const sourceId = description ? "description:issue" : "root";
    const store = useCommentDraftStore.getState();
    store.setDraft(draftKey, "Keep my overall reply");
    for (const [id, quote, start] of [["first", "Selected", 0], ["second", "text", 9]] as const) {
      store.addAnnotation(draftKey, { id, sourceCommentId: sourceId, sourceActorName: "Author",
        quote, start, prefix: "", suffix: "", note: `Note ${id}` });
    }
    const { container } = renderWithI18n(description ? <DescriptionFixture /> : <Fixture />);
    const source = container.querySelector<HTMLElement>(description ? "[contenteditable]" : "[data-comment-content]")!;
    fireEvent.click(await screen.findByRole("button", { name: "Edit annotation 1" }));
    expect(await screen.findByRole("textbox", { name: "Comment (optional)" })).toHaveValue("Note first");
    const remove = screen.getByRole("button", { name: "Remove annotation 1" });
    fireEvent.pointerDown(remove);
    fireEvent.click(remove);
    await waitFor(() => expect(screen.queryByRole("textbox")).not.toBeInTheDocument());
    expect(source).toHaveFocus();
    expect(source).toHaveTextContent("Selected text");
    expect(store.getAnnotations(draftKey).map((a) => a.id)).toEqual(["second"]);
    expect(store.getDraft(draftKey)).toBe("Keep my overall reply");
    expect(screen.queryByRole("button", { name: "Edit annotation 2" })).not.toBeInTheDocument();

    fireEvent.click(await screen.findByRole("button", { name: "Edit annotation 1" }));
    expect(await screen.findByRole("textbox", { name: "Comment (optional)" })).toHaveValue("Note second");
    fireEvent.click(screen.getByRole("button", { name: "Remove annotation 1" }));
    await waitFor(() => expect(screen.queryByRole("button", { name: "Edit annotation 1" })).not.toBeInTheDocument());
    expect(store.getAnnotations(draftKey)).toHaveLength(0);
    expect(store.getDraft(draftKey)).toBe("Keep my overall reply");
    expect(useCommentDraftStore.getState().drafts[draftKey]?.replyTarget).toBeUndefined();
  });
  it("restores saved source markers when the description loads asynchronously", async () => {
    useCommentDraftStore.getState().addAnnotation("new:issue", {
      id: "saved", sourceCommentId: "description:issue", sourceActorName: "Description",
      quote: "Selected text", note: "Saved note", start: 0, prefix: "", suffix: "\n",
    });
    const { rerender } = renderWithI18n(<DescriptionFixture loaded={false} />);
    rerender(<DescriptionFixture />);
    fireEvent.click(await screen.findByRole("button", { name: "Edit annotation 1" }));
    expect(await screen.findByRole("textbox", { name: "Comment (optional)" })).toHaveValue("Saved note");
  });
  it("captures editable descriptions into a new thread draft and clears the popup on issue switch", async () => {
    const { container, rerender } = renderWithI18n(<DescriptionFixture />);
    selectText(container);
    expect(screen.queryByRole("button", { name: "Add to reply" })).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Add to comment" }));
    fireEvent.change(await screen.findByRole("textbox", { name: "Comment (optional)" }), { target: { value: "Question about description" } });
    expect(useCommentDraftStore.getState().getAnnotations("new:issue")[0]).toMatchObject({ quote: "Selected text", note: "Question about description" });
    expect(useCommentDraftStore.getState().drafts["new:issue"]?.replyTarget).toBeUndefined();
    expect(container.querySelector("[contenteditable]")).toHaveTextContent("Selected text");
    rerender(<DescriptionFixture issueId="other" />);
    await waitFor(() => expect(screen.queryByRole("textbox", { name: "Comment (optional)" })).not.toBeInTheDocument());
    expect(useCommentDraftStore.getState().getAnnotations("new:other")).toHaveLength(0);
    expect(useCommentDraftStore.getState().getAnnotations("new:issue")).toHaveLength(1);
  });
  it("keeps the source note usable when expanding the thread remounts its body", async () => {
    const view = renderWithI18n(<Fixture />);
    selectText(view.container);
    fireEvent.click(await screen.findByRole("button", { name: "Add to reply" }));
    fireEvent.change(await screen.findByRole("textbox", { name: "Comment (optional)" }), { target: { value: "Keep this note" } });
    view.rerender(<Fixture sourceKey="expanded" />);
    await waitFor(() => expect(screen.getByRole("textbox", { name: "Comment (optional)" })).toBeVisible());
    expect(screen.getByRole("textbox", { name: "Comment (optional)" })).toHaveValue("Keep this note");
  });
  it("saves before typing, autosaves the note, and reopens duplicates without erasing it", async () => {
    const { container } = renderWithI18n(<Fixture />);
    selectText(container);
    fireEvent.click(await screen.findByRole("button", { name: "Add to reply" }));
    const note = await screen.findByRole("textbox", { name: "Comment (optional)" });
    expect(useCommentDraftStore.getState().getAnnotations(key)).toHaveLength(1);
    fireEvent.change(note, { target: { value: "Please explain" } });
    expect(screen.queryByRole("button", { name: "Done" })).not.toBeInTheDocument();
    fireEvent.pointerDown(document.body);
    await waitFor(() => expect(screen.queryByRole("textbox")).not.toBeInTheDocument());
    selectText(container);
    fireEvent.click(await screen.findByRole("button", { name: "Add to reply" }));
    expect(await screen.findByRole("textbox", { name: "Comment (optional)" })).toHaveValue("Please explain");
    expect(useCommentDraftStore.getState().getAnnotations(key)).toHaveLength(1);
    fireEvent.keyDown(screen.getByRole("textbox"), { key: "Escape" });
    await waitFor(() => expect(screen.queryByRole("textbox")).not.toBeInTheDocument());
    expect(useCommentDraftStore.getState().getAnnotations(key)[0]?.note).toBe("Please explain");
    fireEvent.click(await screen.findByRole("button", { name: "Edit annotation 1" }));
    expect(await screen.findByRole("textbox", { name: "Comment (optional)" })).toHaveValue("Please explain");
  });

  it("offers annotations on member comments", async () => {
    const { container } = renderWithI18n(<Fixture actorType="member" />);
    selectText(container);
    expect(await screen.findByRole("button", { name: "Add to reply" })).toBeInTheDocument();
  });

  it("keeps the action open through the mouseup and click that finish a drag", async () => {
    const { container } = renderWithI18n(<Fixture />);
    selectText(container);
    expect(await screen.findByRole("button", { name: "Add to reply" })).toBeVisible();
    fireEvent.pointerDown(document.body, { pointerType: "mouse", button: 0 });
    fireEvent.mouseDown(document.body, { button: 0 });
    fireEvent.pointerUp(document.body);
    fireEvent.mouseUp(document.body, { button: 0 });
    fireEvent.click(document.body, { button: 0 });
    await waitFor(() => expect(screen.queryByRole("button", { name: "Add to reply" })).not.toBeInTheDocument());
  });

  it("dismisses when the next gesture clears the selection in the source", async () => {
    const { container } = renderWithI18n(<Fixture />);
    const source = selectText(container);
    expect(await screen.findByRole("button", { name: "Add to reply" })).toBeVisible();
    fireEvent.pointerDown(source, { pointerType: "mouse", button: 0 });
    fireEvent.mouseDown(source, { button: 0 });
    act(() => window.getSelection()!.removeAllRanges());
    fireEvent.pointerUp(source);
    fireEvent.mouseUp(source, { button: 0 });
    fireEvent.click(source, { button: 0 });
    await waitFor(() => expect(screen.queryByRole("button", { name: "Add to reply" })).not.toBeInTheDocument());
  });

  it("makes the action reachable from a keyboard selection without trapping Tab", async () => {
    const { container } = renderWithI18n(<Fixture />);
    const source = selectText(container, "keyboard");
    const action = await screen.findByRole("button", { name: "Add to reply" });
    fireEvent.keyDown(source, { key: "Tab" });
    expect(action).toHaveFocus();
    const event = new KeyboardEvent("keydown", { key: "Tab", bubbles: true, cancelable: true });
    action.dispatchEvent(event);
    expect(event.defaultPrevented).toBe(false);
  });
});
