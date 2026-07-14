import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";

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
