import { act, createElement } from "react";
import { createRoot } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useComposerFocusAndDrop } from "./useComposerFocusAndDrop";

const { fileDrop } = vi.hoisted(() => ({ fileDrop: vi.fn() }));
vi.mock("./useComposerFileDrop", () => ({
  useComposerFileDrop: fileDrop
}));

type Input = Parameters<typeof useComposerFocusAndDrop>[0];
let frames: Map<number, FrameRequestCallback>;
let nextFrame: number;
const unmounts = new Set<() => void>();

beforeEach(() => {
  frames = new Map();
  nextFrame = 0;
  vi.spyOn(window, "requestAnimationFrame").mockImplementation((callback) => {
    frames.set(++nextFrame, callback);
    return nextFrame;
  });
  vi.spyOn(window, "cancelAnimationFrame").mockImplementation((id) => {
    frames.delete(id);
  });
  fileDrop.mockReturnValue({});
});

afterEach(() => {
  for (const unmount of unmounts) unmount();
  document.body.replaceChildren();
  vi.restoreAllMocks();
  fileDrop.mockClear();
});

function flushFrame() {
  const callbacks = [...frames.values()];
  frames.clear();
  act(() => callbacks.forEach((callback) => callback(0)));
}

function flushFocus() {
  flushFrame();
  flushFrame();
}

function setup(embedded = false) {
  const search = document.createElement("input");
  document.body.append(search);
  const iframe = embedded ? document.createElement("iframe") : null;
  if (iframe) document.body.append(iframe);
  const editorDocument = iframe?.contentDocument ?? document;
  const form = editorDocument.createElement("form");
  const editor = editorDocument.createElement("input");
  form.append(editor);
  editorDocument.body.append(form);
  const focusAtEnd = vi.fn(() => editor.focus());
  const input: Input = {
    composerControlsHardDisabled: false,
    inputDisabled: false,
    editorHandleRef: { current: { focusAtEnd } as Input["editorHandleRef"]["current"] },
    composerRef: { current: form },
    wasActiveRef: { current: false },
    lastComposerFocusRequestRef: { current: null },
    isActive: false,
    composerFocusRequestSequence: null,
    promptFilesSupported: true,
    promptImagesSupported: true,
    addDraftImages: vi.fn(),
    addDraftFiles: vi.fn()
  };
  const container = editorDocument.createElement("div");
  editorDocument.body.append(container);
  const root = createRoot(container);
  const result = { current: null as unknown as ReturnType<typeof useComposerFocusAndDrop> };
  function Harness({ overrides }: { overrides: Partial<Input> }) {
    result.current = useComposerFocusAndDrop({ ...input, ...overrides });
    return null;
  }
  const rerender = (overrides: Partial<Input>) => {
    act(() => root.render(createElement(Harness, { overrides })));
  };
  const unmount = () => {
    act(() => root.unmount());
    unmounts.delete(unmount);
  };
  unmounts.add(unmount);
  rerender({});
  return { rerender, unmount, result, search, editor, form, editorDocument, iframe, focusAtEnd, input };
}

describe("useComposerFocusAndDrop", () => {
  it.each([false, true])("preserves a search focused during activation (iframe=%s)", (embedded) => {
    const { rerender, search, focusAtEnd } = setup(embedded);
    rerender({ isActive: true });
    search.focus();
    flushFocus();
    expect(document.activeElement).toBe(search);
    expect(focusAtEnd).not.toHaveBeenCalled();
  });

  it("preserves parent search focus already present before activation", () => {
    const { rerender, search, focusAtEnd } = setup(true);
    search.focus();
    rerender({ isActive: true });
    flushFocus();
    expect(document.activeElement).toBe(search);
    expect(focusAtEnd).not.toHaveBeenCalled();
  });

  it("checks focus again after the first frame", () => {
    const { rerender, search, focusAtEnd } = setup(true);
    rerender({ isActive: true });
    flushFrame();
    search.focus();
    flushFrame();
    expect(document.activeElement).toBe(search);
    expect(focusAtEnd).not.toHaveBeenCalled();
  });

  it("preserves focus in another control inside the iframe", () => {
    const { rerender, editorDocument, focusAtEnd } = setup(true);
    const other = editorDocument.createElement("input");
    editorDocument.body.append(other);
    rerender({ isActive: true });
    other.focus();
    flushFocus();
    expect(editorDocument.activeElement).toBe(other);
    expect(focusAtEnd).not.toHaveBeenCalled();
  });

  it.each([false, true])("restores the editor with no competing control (iframe=%s)", (embedded) => {
    const { rerender, editor, editorDocument, focusAtEnd } = setup(embedded);
    rerender({ isActive: true });
    flushFocus();
    expect(focusAtEnd).toHaveBeenCalledOnce();
    expect(editorDocument.activeElement).toBe(editor);
  });

  it.each([0, 1])("cancels deactivated focus after %s frames", (count) => {
    const { rerender, focusAtEnd } = setup(true);
    rerender({ isActive: true });
    if (count) flushFrame();
    rerender({ isActive: false });
    expect(frames.size).toBe(0);
    flushFocus();
    expect(focusAtEnd).not.toHaveBeenCalled();
  });

  it.each(["inputDisabled", "composerControlsHardDisabled"] as const)("cancels focus when %s becomes true", (flag) => {
    const { rerender, focusAtEnd } = setup();
    rerender({ isActive: true });
    flushFrame();
    rerender({ isActive: true, [flag]: true });
    expect(frames.size).toBe(0);
    flushFocus();
    expect(focusAtEnd).not.toHaveBeenCalled();
  });

  it.each([0, 1])("cancels unmounted focus after %s frames", (count) => {
    const { rerender, unmount, focusAtEnd } = setup(true);
    rerender({ isActive: true });
    if (count) flushFrame();
    unmount();
    expect(frames.size).toBe(0);
    flushFocus();
    expect(focusAtEnd).not.toHaveBeenCalled();
  });

  it("lets an explicit new-conversation/draft request replace automatic restoration", () => {
    const { rerender, search, editor, editorDocument, focusAtEnd } = setup(true);
    search.focus();
    rerender({ isActive: true });
    flushFrame();
    rerender({ isActive: true, composerFocusRequestSequence: 1 });
    expect(frames.size).toBe(1);
    flushFocus();
    expect(focusAtEnd).toHaveBeenCalledOnce();
    expect(editorDocument.activeElement).toBe(editor);
    rerender({ isActive: true, composerFocusRequestSequence: 1 });
    flushFocus();
    expect(focusAtEnd).toHaveBeenCalledOnce();
  });

  it.each(["isActive", "inputDisabled", "composerControlsHardDisabled"] as const)("defers explicit requests until %s allows focus", (flag) => {
    const { rerender, search, focusAtEnd } = setup(true);
    search.focus();
    rerender({
      isActive: true,
      [flag]: flag !== "isActive",
      composerFocusRequestSequence: 1
    });
    flushFocus();
    expect(focusAtEnd).not.toHaveBeenCalled();
    expect(document.activeElement).toBe(search);
    rerender({ isActive: true, composerFocusRequestSequence: 1 });
    flushFocus();
    expect(focusAtEnd).toHaveBeenCalledOnce();
  });

  it("keeps paste and file-drop focus explicit", () => {
    const { rerender, result, input, search, focusAtEnd } = setup(true);
    rerender({ isActive: true });
    flushFocus();
    focusAtEnd.mockClear();
    search.focus();
    act(() => result.current.handlePastedImages([]));
    flushFocus();
    expect(input.addDraftImages).toHaveBeenCalledWith([]);
    expect(focusAtEnd).toHaveBeenCalledOnce();
    search.focus();
    const dropInput = fileDrop.mock.lastCall?.[0] as { scheduleComposerFocus: () => void };
    act(() => dropInput.scheduleComposerFocus());
    flushFocus();
    expect(focusAtEnd).toHaveBeenCalledTimes(2);
  });

  it("ignores late asynchronous drop callbacks after unmount", () => {
    const { rerender, unmount, focusAtEnd } = setup();
    rerender({ isActive: true });
    const dropInput = fileDrop.mock.lastCall?.[0] as { scheduleComposerFocus: () => void };
    unmount();
    dropInput.scheduleComposerFocus();
    expect(frames.size).toBe(0);
    flushFocus();
    expect(focusAtEnd).not.toHaveBeenCalled();
  });
});
