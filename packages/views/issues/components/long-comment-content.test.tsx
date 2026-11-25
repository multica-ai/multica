import { beforeEach, describe, expect, it, onTestFinished, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import { useLongCommentExpandStore } from "@multica/core/issues/stores";
import { useAppearanceStore } from "@multica/core/appearance";
import { renderWithI18n } from "../../test/i18n";

// Comment card suites mock the editor barrel, so the component imports from it too.
vi.mock("../../editor", () => ({
  ReadonlyContent: ({ content }: { content: string }) =>
    <div data-testid="rendered-markdown" data-length={content.length}>{content}</div>,
}));

import { LongCommentContent } from "./long-comment-content";
import { LONG_COMMENT_CHARS, PLAIN_TEXT_COMMENT_CHARS, longCommentPreview } from "../utils/long-comment";

const rendered = () => screen.queryByTestId("rendered-markdown");

function renderComment(content: string, issueId = "issue-1") {
  return renderWithI18n(<LongCommentContent issueId={issueId} commentId="c1" content={content} />);
}

beforeEach(() => {
  useLongCommentExpandStore.setState({ expandedByIssue: {} });
});

describe("LongCommentContent", () => {
  it("renders a normal comment in full, with no toggle", () => {
    renderComment("A short comment");

    expect(rendered()!.textContent).toBe("A short comment");
    expect(screen.queryByRole("button")).toBeNull();
  });

  it("hands only the preview of a long comment to the renderer until it is expanded", () => {
    const long = `intro\n${"x".repeat(LONG_COMMENT_CHARS)}\nending`;
    renderComment(long);

    expect(rendered()!.textContent).toBe(longCommentPreview(long));
    const toggle = screen.getByRole("button", { name: /show full comment/i });
    expect(toggle.getAttribute("aria-expanded")).toBe("false");

    fireEvent.click(toggle);
    expect(rendered()!.textContent).toBe(long);
    const collapse = screen.getByRole("button", { name: /collapse comment/i });
    expect(collapse.getAttribute("aria-expanded")).toBe("true");

    fireEvent.click(collapse);
    expect(rendered()!.textContent).toBe(longCommentPreview(long));
  });

  it("keeps the choice when the row remounts, as a virtualized timeline does", () => {
    const long = "y".repeat(LONG_COMMENT_CHARS + 1);
    const first = renderComment(long);
    fireEvent.click(screen.getByRole("button", { name: /show full comment/i }));
    first.unmount();

    renderComment(long);
    expect(rendered()!.textContent).toBe(long);
    // Another issue keeps its own state.
    renderComment(long, "issue-2");
    expect(screen.getAllByRole("button", { name: /show full comment/i })).toHaveLength(1);
  });

  it("shows a huge comment as plain text instead of formatting it", () => {
    const huge = `{"rows":[${'{"id":1},'.repeat(PLAIN_TEXT_COMMENT_CHARS / 8)}]}`;
    renderComment(huge);

    fireEvent.click(screen.getByRole("button", { name: /show full comment/i }));
    expect(rendered()).toBeNull();
    const plain = screen.getByTestId("long-comment-plain-text");
    expect(plain.textContent).toBe(huge);
    expect(screen.getByRole("button", { name: /download as \.txt/i })).toBeTruthy();
  });

  // jsdom has no layout: report the body's height from what it renders.
  function fakeHeights() {
    vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockImplementation(function (this: HTMLElement) {
      return { height: this.textContent?.length ?? 0 } as DOMRect;
    });
    const animate = vi.fn(() => ({ cancel: vi.fn() }));
    Object.defineProperty(HTMLElement.prototype, "animate", { value: animate, configurable: true });
    onTestFinished(() => {
      vi.restoreAllMocks();
      delete (HTMLElement.prototype as { animate?: unknown }).animate;
      useAppearanceStore.setState({ reduceMotion: false });
    });
    return animate;
  }

  it("animates the height change when the reader toggles", () => {
    const animate = fakeHeights();
    renderComment("w".repeat(LONG_COMMENT_CHARS + 1));

    fireEvent.click(screen.getByRole("button", { name: /show full comment/i }));

    expect(animate).toHaveBeenCalledTimes(1);
  });

  it("lands the new height in the same frame under reduced motion", () => {
    const animate = fakeHeights();
    useAppearanceStore.setState({ reduceMotion: true });
    renderComment("w".repeat(LONG_COMMENT_CHARS + 1));

    fireEvent.click(screen.getByRole("button", { name: /show full comment/i }));

    expect(animate).not.toHaveBeenCalled();
  });

  it("brings the toggle back into its scroll container on collapse, without native scrollIntoView", () => {
    const long = "v".repeat(LONG_COMMENT_CHARS + 1);
    const scrollIntoView = vi.fn();
    Object.defineProperty(HTMLElement.prototype, "scrollIntoView", { value: scrollIntoView, configurable: true });
    onTestFinished(() => {
      vi.restoreAllMocks();
      delete (HTMLElement.prototype as { scrollIntoView?: unknown }).scrollIntoView;
    });
    const { container } = renderWithI18n(
      <div data-testid="scroller" style={{ overflowY: "auto" }}>
        <LongCommentContent issueId="issue-1" commentId="c1" content={long} />
      </div>,
    );
    const scroller = container.querySelector<HTMLElement>("[data-testid=scroller]")!;
    Object.defineProperty(scroller, "scrollHeight", { value: 5_000 });
    Object.defineProperty(scroller, "clientHeight", { value: 800 });
    scroller.scrollTop = 3_000;
    fireEvent.click(screen.getByRole("button", { name: /show full comment/i }));
    // After collapsing, the toggle sits 1,200px above the container's top edge.
    vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockImplementation(function (this: HTMLElement) {
      return { top: this === scroller ? 100 : this.tagName === "BUTTON" ? -1_100 : 0, height: 0 } as DOMRect;
    });

    fireEvent.click(screen.getByRole("button", { name: /collapse comment/i }));

    expect(scroller.scrollTop).toBe(1_800);
    expect(scrollIntoView).not.toHaveBeenCalled();
  });

  it("keeps its controls out of in-page find", () => {
    renderComment("u".repeat(PLAIN_TEXT_COMMENT_CHARS + 1));
    fireEvent.click(screen.getByRole("button", { name: /show full comment/i }));

    expect(screen.getByRole("button", { name: /collapse comment/i }).closest("[data-find-ignore]")).not.toBeNull();
    expect(screen.getByRole("button", { name: /download as \.txt/i }).closest("[data-find-ignore]")).not.toBeNull();
  });

  it("downloads the full text", () => {
    const createObjectURL = vi.fn((_blob: Blob) => "blob:comment");
    const revokeObjectURL = vi.fn();
    vi.stubGlobal("URL", Object.assign(Object.create(URL), { createObjectURL, revokeObjectURL }));
    onTestFinished(() => { vi.unstubAllGlobals(); });
    const click = vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(() => {});
    const huge = "z".repeat(PLAIN_TEXT_COMMENT_CHARS + 1);
    renderComment(huge);

    fireEvent.click(screen.getByRole("button", { name: /show full comment/i }));
    fireEvent.click(screen.getByRole("button", { name: /download as \.txt/i }));

    expect(click).toHaveBeenCalledTimes(1);
    expect(createObjectURL.mock.calls[0]![0].size).toBe(huge.length);
  });
});
