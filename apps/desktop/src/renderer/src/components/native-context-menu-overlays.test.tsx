import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useState } from "react";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import { useCommentDraftStore } from "@multica/core/issues/stores";
import { RESOURCES } from "@multica/views/locales";
import type { ContextMenuRequest } from "../../../shared/context-menu";
import { SessionRenameInput } from "../../../../../../packages/views/chat/components/session-rename-input";
import { useCommentAnnotations } from "../../../../../../packages/views/issues/components/use-comment-annotations";
import { NativeContextMenu } from "./native-context-menu";

const api = vi.hoisted(() => ({
  listener: null as ((request: ContextMenuRequest) => void) | null,
  runContextMenuCommand: vi.fn(),
}));

vi.mock("@multica/core/api", async (original) => {
  const actual = await original<typeof import("@multica/core/api")>();
  return {
    ...actual,
    api: { ...actual.api, previewCommentTriggers: vi.fn(async () => ({ agents: [], blocked: [] })), listTasksByIssue: vi.fn(async () => []) },
  };
});
vi.mock("../../../../../../packages/views/editor/content-editor", async () => {
  const { forwardRef, useImperativeHandle, useRef } = await import("react");
  return {
    ContentEditor: forwardRef(function Editor({ ariaLabel }: { ariaLabel?: string }, ref) {
      const input = useRef<HTMLTextAreaElement>(null);
      useImperativeHandle(ref, () => ({
        getMarkdown: () => input.current?.value ?? "",
        focus: () => input.current?.focus(),
        hasActiveUploads: () => false,
      }));
      return <textarea ref={input} aria-label={ariaLabel} />;
    }),
  };
});

beforeEach(() => {
  api.listener = null;
  api.runContextMenuCommand.mockClear();
  useCommentDraftStore.setState({ drafts: {} });
  Object.assign(window, {
    desktopAPI: {
      onContextMenu: (callback: (request: ContextMenuRequest) => void) => {
        api.listener = callback;
        return () => {
          api.listener = null;
        };
      },
      runContextMenuCommand: api.runContextMenuCommand,
    },
  });
  // jsdom has no layout; give Floating UI a viewport so the bubble is visible.
  Object.defineProperty(document.documentElement, "clientWidth", { configurable: true, value: 1024 });
  Object.defineProperty(document.documentElement, "clientHeight", { configurable: true, value: 768 });
  vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue(new DOMRect(10, 10, 600, 400));
  Range.prototype.getBoundingClientRect = vi.fn(() => new DOMRect(10, 10, 120, 20));
  Range.prototype.getClientRects = vi.fn(() => [] as unknown as DOMRectList);
});

afterEach(() => {
  vi.restoreAllMocks();
  delete (window as { desktopAPI?: unknown }).desktopAPI;
});

function renderHost(page: React.ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <I18nProvider locale="en" resources={RESOURCES}>
        {page}
        <NativeContextMenu />
      </I18nProvider>
    </QueryClientProvider>,
  );
}

function request(over: Partial<ContextMenuRequest>): ContextMenuRequest {
  return {
    id: 1,
    x: 10,
    y: 20,
    isEditable: false,
    canCut: false,
    canCopy: false,
    canPaste: false,
    canSelectAll: false,
    suggestions: [],
    canAddToDictionary: false,
    hasLink: false,
    hasImage: false,
    ...over,
  };
}

/**
 * A real right-click on a field: the press lands on it, the DOM event fires,
 * main asks for the menu, the menu takes focus, and the item is pressed and
 * clicked.
 */
async function pickFromFieldMenu(field: HTMLElement, item: RegExp) {
  fireEvent.pointerDown(field, { button: 2 });
  fireEvent.contextMenu(field, { clientX: 5, clientY: 5 });
  act(() => api.listener?.(request({ isEditable: true, canCut: true, canCopy: true, canPaste: true })));
  const menu = await screen.findByRole("menu");
  await waitFor(() => expect(menu.contains(document.activeElement)).toBe(true));
  const entry = screen.getByRole("menuitem", { name: item });
  fireEvent.pointerDown(entry, { button: 0 });
  fireEvent.click(entry);
  await waitFor(() => expect(api.runContextMenuCommand).toHaveBeenCalledTimes(1));
  await waitFor(() => expect(screen.queryByRole("menu")).toBeNull());
}

const draftKey = "reply:issue:root" as const;

function AnnotationHost() {
  const annotation = useCommentAnnotations({
    issueId: "issue",
    draftKey,
    sources: [{ id: "root", name: "Agent" }],
    enabled: true,
  });
  return (
    <div ref={annotation.cardRef} {...annotation.captureProps}>
      {annotation.popup}
      {annotation.composer?.render()}
      <div data-comment-content="root" tabIndex={0}>Selected text</div>
      <button type="button">Elsewhere</button>
    </div>
  );
}

async function openAnnotationNote() {
  const source = document.querySelector<HTMLElement>("[data-comment-content]")!;
  const range = document.createRange();
  range.selectNodeContents(source);
  act(() => {
    window.getSelection()!.removeAllRanges();
    window.getSelection()!.addRange(range);
  });
  fireEvent.pointerUp(source);
  fireEvent.click(await screen.findByRole("button", { name: "Add to reply" }));
  const note = await screen.findByRole("textbox", { name: "Comment" });
  note.focus();
  return note;
}

describe("NativeContextMenu inside an overlay", () => {
  it("keeps a comment annotation's note open while its own menu pastes", async () => {
    renderHost(<AnnotationHost />);
    const note = await openAnnotationNote();

    await pickFromFieldMenu(note, /^Paste/);

    expect(api.runContextMenuCommand).toHaveBeenCalledWith({ id: 1, type: "paste" });
    expect(note.isConnected).toBe(true);
    await waitFor(() => expect(note).toHaveFocus());
  });

  it("lets Escape close only the menu over an annotation note", async () => {
    renderHost(<AnnotationHost />);
    const note = await openAnnotationNote();
    fireEvent.pointerDown(note, { button: 2 });
    fireEvent.contextMenu(note, { clientX: 5, clientY: 5 });
    act(() => api.listener?.(request({ isEditable: true, canPaste: true })));
    const menu = await screen.findByRole("menu");
    await waitFor(() => expect(menu.contains(document.activeElement)).toBe(true));

    fireEvent.keyDown(document.activeElement!, { key: "Escape" });

    await waitFor(() => expect(screen.queryByRole("menu")).toBeNull());
    expect(note.isConnected).toBe(true);
    expect(api.runContextMenuCommand).not.toHaveBeenCalled();
  });

  it("still closes the annotation note on a press outside", async () => {
    renderHost(<AnnotationHost />);
    const note = await openAnnotationNote();

    fireEvent.pointerDown(screen.getByRole("button", { name: "Elsewhere" }));

    await waitFor(() => expect(note.isConnected).toBe(false));
  });

  it("does not commit an inline title when choosing Cut from its context menu", async () => {
    const submit = vi.fn();
    function RenameHost() {
      const [editing, setEditing] = useState(true);
      return editing ? (
        <SessionRenameInput
          initialValue="alpha beta"
          onSubmit={(value) => {
            submit(value);
            setEditing(false);
          }}
          onCancel={() => setEditing(false)}
        />
      ) : (
        <span>Committed title</span>
      );
    }
    renderHost(<RenameHost />);
    const input = screen.getByRole("textbox") as HTMLInputElement;
    input.setSelectionRange(0, 5);

    await pickFromFieldMenu(input, /^Cut/);

    expect({ submitted: submit.mock.calls.length, inputConnected: input.isConnected }).toEqual({
      submitted: 0,
      inputConnected: true,
    });
    fireEvent.pointerDown(document.body);
    expect(submit).toHaveBeenCalledWith("alpha beta");
  });
});
