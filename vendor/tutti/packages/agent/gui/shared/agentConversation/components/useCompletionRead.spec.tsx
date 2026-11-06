import { act, cleanup, fireEvent, render } from "@testing-library/react";
import { useRef } from "react";
import { CompletionReadModel } from "@multica/conversation-client/models";
import { afterEach, expect, it, vi } from "vitest";
import { useCompletionRead } from "./useCompletionRead";

afterEach(() => { cleanup(); vi.restoreAllMocks(); });

it("acknowledges a rendered result only when focused, visible and inside the scroll viewport", () => {
  let focused = false;
  let top = 900;
  const frames = new Map<number, FrameRequestCallback>();
  let next = 0;
  vi.spyOn(globalThis, "requestAnimationFrame").mockImplementation((fn) => { frames.set(++next, fn); return next; });
  vi.spyOn(globalThis, "cancelAnimationFrame").mockImplementation((id) => { frames.delete(id); });
  vi.spyOn(document, "hasFocus").mockImplementation(() => focused);
  vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockImplementation(function (this: HTMLElement) {
    return { top: this.dataset.agentTranscriptRow ? top : 0, bottom: this.dataset.agentTranscriptRow ? top + 50 : 600, left: 0, right: 500, width: 500, height: 50, x: 0, y: 0, toJSON: () => ({}) };
  });
  vi.spyOn(HTMLElement.prototype, "getClientRects").mockReturnValue([{}] as unknown as DOMRectList);
  const model = new CompletionReadModel();
  model.set("c", { version: 1, generation: "g", baselineTurnIndex: 0, summarySeq: 2, turnCount: 1, activeTurnId: null, status: "idle", completedTurnIndex: 0, completedTurnId: "t0", readTurnIndex: -1 });
  const completions = { pending: (id: string) => model.pending(id), markRead: vi.fn() };
  function Harness({ visible, settled }: { visible: boolean; settled: boolean }) {
    const ref = useRef<HTMLDivElement>(null);
    useCompletionRead(completions, "c", visible, ref);
    return <div data-testid="agent-gui-timeline"><div ref={ref}><div data-multica-turn-id="t0">
      <div data-agent-transcript-row="answer" data-agent-transcript-turn-completed={settled ? "true" : undefined}>Answer</div>
    </div></div></div>;
  }
  const frame = () => act(() => { const callbacks = [...frames.values()]; frames.clear(); callbacks.forEach((fn) => fn(0)); });
  const app = render(<Harness visible settled />);
  frame(); expect(completions.markRead).not.toHaveBeenCalled();
  focused = true; fireEvent.focus(window); frame();
  expect(completions.markRead).not.toHaveBeenCalled();
  top = 100; fireEvent.scroll(document); frame();
  expect(completions.markRead).toHaveBeenCalledWith("c", expect.objectContaining({ completedTurnId: "t0", completedTurnIndex: 0 }));
  completions.markRead.mockClear();
  app.rerender(<Harness visible={false} settled />); fireEvent.focus(window); frame();
  expect(completions.markRead).not.toHaveBeenCalled();
  app.rerender(<Harness visible settled={false} />); frame();
  expect(completions.markRead).not.toHaveBeenCalled();
  app.unmount(); fireEvent.focus(window); frame();
  expect(completions.markRead).not.toHaveBeenCalled();
});
