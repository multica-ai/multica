import { useState } from "react";
import { cleanup, fireEvent, render, renderHook, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { AgentTask } from "@multica/core/types";
import { useNewRunIds, useRunCommentMotion, useRunDisclosureMotion } from "./use-run-comment-motion";

const animate = vi.fn((_frames: Keyframe[], _options: KeyframeAnimationOptions) => ({ cancel: vi.fn() }));
const originalAnimate = Object.getOwnPropertyDescriptor(Element.prototype, "animate");
let reduced = false;
beforeEach(() => {
  reduced = false;
  animate.mockClear();
  Object.defineProperty(Element.prototype, "animate", { configurable: true, value: animate });
  vi.stubGlobal("matchMedia", vi.fn(() => ({ matches: reduced })));
});
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  if (originalAnimate) Object.defineProperty(Element.prototype, "animate", originalAnimate);
  else Reflect.deleteProperty(Element.prototype, "animate");
  vi.unstubAllGlobals();
});

function task(id: string): AgentTask {
  return { id, issue_id: "issue", agent_id: "agent", runtime_id: "runtime", status: "queued", priority: 0,
    created_at: "2026-09-07T00:00:00Z", started_at: null, dispatched_at: null, completed_at: null, result: null, error: null };
}

function Slot({ entering = false, replyId, status = "queued" }: { entering?: boolean; replyId?: string; status?: string }) {
  const ref = useRunCommentMotion(entering, replyId, status);
  return <div ref={ref}><span data-run-status>{status}</span><div data-comment-content={replyId}>Reply</div></div>;
}
function Disclosure() {
  const [open, setOpen] = useState(false);
  const motion = useRunDisclosureMotion(open);
  return <>
    <button key={String(open)} onClick={(event) => { motion.onTrigger(event); setOpen(!open); }}>
      <svg ref={motion.chevronRef} style={{ rotate: open ? "90deg" : "0deg" }} />Toggle
    </button>
    {open && <div ref={motion.contentRef}>Activity</div>}
  </>;
}

describe("run comment motion", () => {
  it("marks only runs added after the initial snapshot and resets across issues", async () => {
    const old = task("old");
    const added = task("new");
    const { result, rerender } = renderHook(({ issueId, tasks }: { issueId: string; tasks?: AgentTask[] }) => useNewRunIds(issueId, tasks),
      { initialProps: { issueId: "first", tasks: undefined } as { issueId: string; tasks?: AgentTask[] } });
    rerender({ issueId: "first", tasks: [old] });
    expect(result.current.size).toBe(0);
    rerender({ issueId: "first", tasks: [old, added] });
    await waitFor(() => expect([...result.current]).toEqual(["new"]));
    const marked = result.current;
    rerender({ issueId: "first", tasks: [old, { ...added, status: "running" }] });
    expect(result.current).toBe(marked);
    rerender({ issueId: "second", tasks: [task("other")] });
    expect(result.current.size).toBe(0);
  });

  it("animates live arrival once and never replays on a virtualized remount", () => {
    const view = render(<Slot />);
    expect(animate).not.toHaveBeenCalled();
    view.rerender(<Slot entering />);
    expect(animate).toHaveBeenCalledWith(
      [{ opacity: 0, transform: "translateY(4px)" }, { opacity: 1, transform: "translateY(0)" }],
      expect.objectContaining({ duration: 160 }),
    );
    view.rerender(<Slot entering />);
    expect(animate).toHaveBeenCalledTimes(1);
    const animation = animate.mock.results[0]!.value;
    view.unmount();
    expect(animation.cancel).toHaveBeenCalled();
    animate.mockClear();
    render(<Slot entering />);
    expect(animate).not.toHaveBeenCalled();
  });

  it("fades only the arriving reply and changed status, not routine rerenders", () => {
    const view = render(<Slot status="running" />);
    view.rerender(<Slot status="completed" replyId="reply" />);
    expect(animate.mock.instances).toEqual([screen.getByText("Reply"), screen.getByText("completed")]);
    expect(animate.mock.calls.map((call) => call[1].duration)).toEqual([160, 100]);
    view.rerender(<Slot status="completed" replyId="reply" />);
    expect(animate).toHaveBeenCalledTimes(2);
    view.unmount();
    animate.mockClear();
    render(<Slot status="completed" replyId="reply" />);
    expect(animate).not.toHaveBeenCalled();
  });

  it("keeps reduced motion to a short fade without displacement", () => {
    reduced = true;
    const view = render(<Slot />);
    view.rerender(<Slot entering />);
    expect(animate).toHaveBeenCalledWith([{ opacity: 0 }, { opacity: 1 }], expect.objectContaining({ duration: 60 }));
  });

  it("fades pointer disclosure once and keeps keyboard disclosure immediate", () => {
    render(<Disclosure />);
    const toggle = () => screen.getByRole("button", { name: "Toggle" });
    fireEvent.click(toggle(), { detail: 1 });
    expect(screen.getByText("Activity")).toBeInTheDocument();
    expect(animate).toHaveBeenCalledWith(
      [{ opacity: 0, transform: "translateY(-4px)" }, { opacity: 1, transform: "translateY(0)" }],
      expect.objectContaining({ duration: 180 }),
    );
    expect(animate).toHaveBeenCalledWith([{ rotate: "0deg" }, { rotate: "90deg" }], expect.objectContaining({ duration: 180 }));
    fireEvent.click(toggle(), { detail: 1 });
    expect(animate).toHaveBeenCalledWith([{ rotate: "90deg" }, { rotate: "0deg" }], expect.objectContaining({ duration: 120 }));
    animate.mockClear();
    fireEvent.click(toggle(), { detail: 0 });
    expect(screen.getByText("Activity")).toBeInTheDocument();
    expect(animate).not.toHaveBeenCalled();
  });

  it("uses only a short content fade for reduced-motion disclosure", () => {
    reduced = true;
    render(<Disclosure />);
    fireEvent.click(screen.getByRole("button", { name: "Toggle" }), { detail: 1 });
    expect(animate).toHaveBeenCalledTimes(1);
    expect(animate).toHaveBeenCalledWith([{ opacity: 0 }, { opacity: 1 }], expect.objectContaining({ duration: 60 }));
  });

  it("reverses a rapid toggle from the visible arrow angle and cancels stale motion", () => {
    render(<Disclosure />);
    fireEvent.click(screen.getByRole("button", { name: "Toggle" }), { detail: 1 });
    const firstAnimations = animate.mock.results.map((result) => result.value);
    const toggle = screen.getByRole("button", { name: "Toggle" });
    const computed = getComputedStyle(toggle.querySelector("svg")!);
    computed.rotate = "42deg";
    const style = vi.spyOn(window, "getComputedStyle").mockReturnValue(computed);
    fireEvent.click(toggle, { detail: 1 });
    expect(animate).toHaveBeenLastCalledWith([{ rotate: "42deg" }, { rotate: "0deg" }], expect.objectContaining({ duration: 120 }));
    for (const animation of firstAnimations) expect(animation.cancel).toHaveBeenCalled();
    style.mockRestore();
  });
});
