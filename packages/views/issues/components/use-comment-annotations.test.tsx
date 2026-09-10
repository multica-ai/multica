import { act, fireEvent, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useCommentDraftStore } from "@multica/core/issues/stores";
import type { TimelineEntry } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { useCommentAnnotations } from "./use-comment-annotations";

const entry: TimelineEntry = { type: "comment", id: "root", actor_type: "agent", actor_id: "emacs", content: "Selected text", created_at: "2026-09-10T00:00:00Z" };
const key = "reply:issue:root" as const;

function Fixture({ actorType = "agent" }: { actorType?: string }) {
  const annotation = useCommentAnnotations({
    draftKey: key, entry: { ...entry, actor_type: actorType }, replies: [], enabled: true, getActorName: () => "Emacs",
  });
  return <div ref={annotation.cardRef} {...annotation.captureProps}>
    {annotation.popup}
    <div data-comment-content="root" tabIndex={0}>Selected text</div>
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
  Range.prototype.getBoundingClientRect = vi.fn(() => new DOMRect(10, 10, 120, 20));
  Range.prototype.getClientRects = vi.fn(() => [] as unknown as DOMRectList);
});

describe("selection to reply", () => {
  it("saves before typing, autosaves the note, and reopens duplicates without erasing it", async () => {
    const { container } = renderWithI18n(<Fixture />);
    selectText(container);
    fireEvent.click(await screen.findByRole("button", { name: "Add to reply" }));
    const note = await screen.findByRole("textbox", { name: "Comment (optional)" });
    expect(useCommentDraftStore.getState().getAnnotations(key)).toHaveLength(1);
    fireEvent.change(note, { target: { value: "Please explain" } });
    fireEvent.click(screen.getByRole("button", { name: "Done" }));
    await waitFor(() => expect(screen.queryByRole("textbox")).not.toBeInTheDocument());
    selectText(container);
    fireEvent.click(await screen.findByRole("button", { name: "Add to reply" }));
    expect(await screen.findByRole("textbox", { name: "Comment (optional)" })).toHaveValue("Please explain");
    expect(useCommentDraftStore.getState().getAnnotations(key)).toHaveLength(1);
    fireEvent.keyDown(screen.getByRole("textbox"), { key: "Escape" });
    await waitFor(() => expect(screen.queryByRole("textbox")).not.toBeInTheDocument());
    expect(useCommentDraftStore.getState().getAnnotations(key)[0]?.note).toBe("Please explain");
  });

  it("does not offer annotations on member comments", () => {
    const { container } = renderWithI18n(<Fixture actorType="member" />);
    selectText(container);
    expect(screen.queryByRole("button", { name: "Add to reply" })).not.toBeInTheDocument();
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
