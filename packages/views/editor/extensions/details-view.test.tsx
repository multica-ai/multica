import { afterEach, describe, expect, it, vi } from "vitest";
import {
  cleanup,
  createEvent,
  fireEvent,
  render,
  screen,
} from "@testing-library/react";

// Tiptap's NodeView primitives need a full editor to instantiate. Stub the
// wrapper so <DetailsBlockView /> can render as a plain React component and we
// can inspect the DOM shape and interactions directly.
vi.mock("@tiptap/react", () => {
  const NodeViewWrapper = ({ children, ...rest }: any) => (
    <div data-testid="nvw" {...rest}>
      {children}
    </div>
  );
  return { NodeViewWrapper };
});

// The body preview goes through the heavy react-markdown renderer; a sentinel
// is enough to assert the readonly branch renders it (and the edit branch does
// not).
vi.mock("../readonly-content", () => ({
  ReadonlyContent: ({ content }: { content: string }) => (
    <div data-testid="readonly">{content}</div>
  ),
}));

import { DetailsBlockView } from "./details";

function makeProps(
  attrs: { summary?: string; body?: string; open?: boolean },
  { editable = true }: { editable?: boolean } = {},
) {
  const updateAttributes = vi.fn();
  const props = {
    node: { attrs: { summary: "", body: "", open: false, ...attrs } },
    updateAttributes,
    editor: { isEditable: editable },
  } as unknown as Parameters<typeof DetailsBlockView>[0];
  return { props, updateAttributes };
}

afterEach(() => {
  cleanup();
});

describe("DetailsBlockView — editable mode", () => {
  it("renders collapsed by default, keeping the editable fields", () => {
    const { props } = makeProps({ summary: "Click to expand", body: "Body." });
    render(<DetailsBlockView {...props} />);

    // The regression: editable mode used to render always-expanded form
    // controls, so the block never collapsed on issues. It must stay a
    // (collapsed) native <details> even while editable.
    const details = document.querySelector("details");
    expect(details).toBeTruthy();
    expect(details!.hasAttribute("open")).toBe(false);
    expect(screen.getByLabelText("Summary")).toBeTruthy();
  });

  it("respects the open attribute in editable mode", () => {
    const { props } = makeProps({ summary: "s", body: "b", open: true });
    render(<DetailsBlockView {...props} />);

    const details = document.querySelector("details");
    expect(details).toBeTruthy();
    expect(details!.hasAttribute("open")).toBe(true);
  });

  it("treats folding/unfolding as local UI state that does not dirty the doc", () => {
    const { props, updateAttributes } = makeProps({
      summary: "s",
      body: "b",
    });
    render(<DetailsBlockView {...props} />);

    const details = document.querySelector("details") as HTMLDetailsElement;
    expect(details.open).toBe(false);

    // Simulate the user unfolding the block: the browser flips the DOM `open`
    // property and fires `toggle`; the view must mirror that locally without
    // writing the `open` attribute back into the document.
    details.open = true;
    fireEvent(details, new Event("toggle"));
    expect(details.open).toBe(true);

    details.open = false;
    fireEvent(details, new Event("toggle"));
    expect(details.open).toBe(false);

    expect(updateAttributes).not.toHaveBeenCalled();
  });

  it("does not toggle the block when clicking into the summary field", () => {
    const { props } = makeProps({ summary: "s", body: "b" });
    render(<DetailsBlockView {...props} />);

    // Clicking a <summary> toggles the parent <details> by default; the click
    // on the input must be prevented so the user can focus/edit the field.
    const input = screen.getByLabelText("Summary");
    const click = createEvent.click(input);
    fireEvent(input, click);
    expect(click.defaultPrevented).toBe(true);
  });

  it("renders the summary in an editable text field", () => {
    const { props } = makeProps({ summary: "Click to expand", body: "Body." });
    render(<DetailsBlockView {...props} />);

    const input = screen.getByLabelText("Summary") as HTMLInputElement;
    expect(input.value).toBe("Click to expand");
    // A plain read-only <summary> text node is NOT editable — the bug.
    expect(input.tagName).toBe("INPUT");
  });

  it("renders the body in an editable (source) field holding the verbatim markdown", () => {
    const { props } = makeProps({
      summary: "s",
      body: "Hidden **markdown** body.\n\n- one\n- two",
    });
    render(<DetailsBlockView {...props} />);

    const body = screen.getByLabelText(/body/i) as HTMLTextAreaElement;
    expect(body.value).toBe("Hidden **markdown** body.\n\n- one\n- two");
    expect(body.tagName).toBe("TEXTAREA");
    // In edit mode the body is a source field, not the readonly preview.
    expect(screen.queryByTestId("readonly")).toBeNull();
  });

  it("writes summary edits back through updateAttributes", () => {
    const { props, updateAttributes } = makeProps({ summary: "Old" });
    render(<DetailsBlockView {...props} />);

    fireEvent.change(screen.getByLabelText("Summary"), {
      target: { value: "New summary" },
    });
    expect(updateAttributes).toHaveBeenCalledWith({ summary: "New summary" });
  });

  it("writes body edits back through updateAttributes", () => {
    const { props, updateAttributes } = makeProps({ body: "old body" });
    render(<DetailsBlockView {...props} />);

    fireEvent.change(screen.getByLabelText(/body/i), {
      target: { value: "new body" },
    });
    expect(updateAttributes).toHaveBeenCalledWith({ body: "new body" });
  });
});

describe("DetailsBlockView — readonly mode", () => {
  it("renders a collapsed <details> with the body through ReadonlyContent", () => {
    const { props } = makeProps(
      { summary: "Click to expand", body: "Body." },
      { editable: false },
    );
    render(<DetailsBlockView {...props} />);

    expect(document.querySelector("details")).toBeTruthy();
    expect(screen.getByTestId("readonly").textContent).toBe("Body.");
    // No editable fields when the editor is not editable.
    expect(screen.queryByLabelText("Summary")).toBeNull();
    expect(screen.queryByLabelText(/body/i)).toBeNull();
  });
});
