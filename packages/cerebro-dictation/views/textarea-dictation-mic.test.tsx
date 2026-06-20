import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import * as React from "react";
import { TextareaDictationMic } from "./textarea-dictation-mic";

// Stub EditorDictationMic with a plain button that fires onTranscribed with a
// fixed clip. This isolates the only logic TextareaDictationMic owns — wiring a
// finished transcription into the host <textarea> at its caret — without
// dragging in the record → POST → flag-gated MicButton chain (covered by
// mic-button.test.tsx and insert-at-caret.test.ts).
const mocks = vi.hoisted(() => ({ transcript: "dikteret tekst" }));
vi.mock("./editor-dictation-mic", () => ({
  EditorDictationMic: ({
    onTranscribed,
  }: {
    onTranscribed: (text: string) => void;
  }) => (
    <button type="button" onClick={() => onTranscribed(mocks.transcript)}>
      mic
    </button>
  ),
}));

function Harness({ initial = "" }: { initial?: string }) {
  const ref = React.useRef<HTMLTextAreaElement>(null);
  return (
    <div>
      <textarea ref={ref} aria-label="body" defaultValue={initial} />
      <TextareaDictationMic textareaRef={ref} />
    </div>
  );
}

describe("TextareaDictationMic", () => {
  it("inserts the transcription into an empty textarea", () => {
    render(<Harness />);
    const ta = screen.getByLabelText("body") as HTMLTextAreaElement;
    ta.focus();
    ta.setSelectionRange(0, 0);

    fireEvent.click(screen.getByText("mic"));

    expect(ta.value).toBe("dikteret tekst");
  });

  it("inserts at the caret position rather than appending", () => {
    render(<Harness initial="AB" />);
    const ta = screen.getByLabelText("body") as HTMLTextAreaElement;
    ta.focus();
    ta.setSelectionRange(1, 1); // caret between A and B

    fireEvent.click(screen.getByText("mic"));

    expect(ta.value).toBe("Adikteret tekstB");
  });
});
