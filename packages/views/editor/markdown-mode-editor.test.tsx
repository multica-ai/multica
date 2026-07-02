import { forwardRef, useEffect, useImperativeHandle, useRef, useState } from "react";
import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { MarkdownModeEditor } from "./markdown-mode-editor";

vi.mock("./content-editor", () => ({
  ContentEditor: forwardRef(function MockContentEditor(
    {
      defaultValue = "",
      placeholder,
      onUpdate,
      flushPendingOnUnmount,
    }: {
      defaultValue?: string;
      placeholder?: string;
      onUpdate?: (value: string) => void;
      flushPendingOnUnmount?: boolean;
    },
    ref,
  ) {
    const [value, setValue] = useState(defaultValue);
    const valueRef = useRef(value);
    valueRef.current = value;

    useEffect(() => {
      return () => {
        if (!flushPendingOnUnmount) return;
        if (valueRef.current === defaultValue) return;
        onUpdate?.(valueRef.current);
      };
    }, []);

    useImperativeHandle(ref, () => ({
      getMarkdown: () => `serialized:${value}`,
      clearContent: vi.fn(),
      focus: vi.fn(),
      blur: vi.fn(),
      uploadFile: vi.fn(),
      hasActiveUploads: () => false,
    }));

    return (
      <textarea
        aria-label="rich editor"
        value={value}
        placeholder={placeholder}
        onChange={(event) => setValue(event.target.value)}
      />
    );
  }),
}));

function Harness({
  initial,
  onChange,
}: {
  initial: string;
  onChange: (value: string) => void;
}) {
  const [value, setValue] = useState(initial);
  return (
    <MarkdownModeEditor
      value={value}
      onChange={(next) => {
        onChange(next);
        setValue(next);
      }}
      placeholder="Write instructions"
      labels={{ rich: "Rich text", source: "Markdown source" }}
    />
  );
}

describe("MarkdownModeEditor", () => {
  it("edits exact markdown in source mode", () => {
    const onChange = vi.fn();
    render(<Harness initial={"# Role\n\n- Keep code focused"} onChange={onChange} />);

    fireEvent.click(screen.getByRole("button", { name: "Markdown source" }));
    const source = screen.getByPlaceholderText("Write instructions");

    fireEvent.change(source, {
      target: { value: "# Role\n\n```ts\nconst exact = true;\n```" },
    });

    expect(onChange).toHaveBeenLastCalledWith("# Role\n\n```ts\nconst exact = true;\n```");
    expect(source).toHaveValue("# Role\n\n```ts\nconst exact = true;\n```");
  });

  it("preserves the original markdown when switching to source without rich edits", () => {
    const onChange = vi.fn();
    render(<Harness initial={"# Role\n\n- exact spacing"} onChange={onChange} />);

    fireEvent.click(screen.getByRole("button", { name: "Markdown source" }));

    expect(onChange).not.toHaveBeenCalled();
    expect(screen.getByPlaceholderText("Write instructions")).toHaveValue("# Role\n\n- exact spacing");
  });

  it("flushes pending rich editor markdown when switching to source mode", () => {
    const onChange = vi.fn();
    render(<Harness initial="original" onChange={onChange} />);

    fireEvent.change(screen.getByLabelText("rich editor"), {
      target: { value: "typed **markdown**" },
    });
    expect(onChange).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "Markdown source" }));

    expect(onChange).toHaveBeenCalledWith("typed **markdown**");
    expect(screen.getByPlaceholderText("Write instructions")).toHaveValue("typed **markdown**");
  });
});
