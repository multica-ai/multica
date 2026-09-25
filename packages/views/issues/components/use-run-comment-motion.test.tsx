import { useState } from "react";
import { act, cleanup, fireEvent, render, renderHook, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { AgentTask } from "@multica/core/types";
import { useNewRunIds, useRunAnimationVisibility, useRunCommentMotion, useRunDisclosureMotion } from "./use-run-comment-motion";

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
    {open && <div>Activity</div>}
  </>;
}
function Visibility() {
  const { ref, visible } = useRunAnimationVisibility<HTMLDivElement>();
  return <div ref={ref} data-testid="visibility" data-visible={visible} />;
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

  it("animates live arrival once with shared timing and never replays on a virtualized remount", () => {
    const view = render(<Slot />);
    expect(animate).not.toHaveBeenCalled();
    view.rerender(<Slot entering />);
    expect(animate).toHaveBeenCalledWith(
      [{ opacity: 0, transform: "translateY(4px)" }, { opacity: 1, transform: "translateY(0)" }],
      expect.objectContaining({ duration: 150, easing: "cubic-bezier(0.23, 1, 0.32, 1)" }),
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

  it("prioritizes an arriving reply over a simultaneous status change", () => {
    const view = render(<Slot status="running" />);
    view.rerender(<Slot status="completed" replyId="reply" />);
    expect(animate.mock.instances).toEqual([screen.getByText("Reply")]);
    expect(animate.mock.calls.map((call) => call[1].duration)).toEqual([150]);
    view.rerender(<Slot status="completed" replyId="reply" />);
    expect(animate).toHaveBeenCalledTimes(1);
    view.unmount();
    animate.mockClear();
    render(<Slot status="completed" replyId="reply" />);
    expect(animate).not.toHaveBeenCalled();
  });

  it("uses the micro timing for a status-only change", () => {
    const view = render(<Slot status="running" />);
    view.rerender(<Slot status="completed" />);
    expect(animate.mock.instances).toEqual([screen.getByText("completed")]);
    expect(animate.mock.calls.map((call) => call[1].duration)).toEqual([100]);
  });

  it("disables run arrival motion when reduced motion is requested", () => {
    reduced = true;
    const view = render(<Slot />);
    view.rerender(<Slot entering />);
    expect(animate).not.toHaveBeenCalled();
  });

  it("pauses ambient run motion outside the viewport", async () => {
    let notify!: IntersectionObserverCallback;
    const observe = vi.fn();
    const disconnect = vi.fn();
    vi.stubGlobal("IntersectionObserver", class {
      constructor(callback: IntersectionObserverCallback) {
        notify = callback;
      }
      observe = observe;
      disconnect = disconnect;
    });
    const view = render(<Visibility />);
    await waitFor(() => expect(observe).toHaveBeenCalledWith(screen.getByTestId("visibility")));
    act(() => notify([{ isIntersecting: false } as IntersectionObserverEntry], {} as IntersectionObserver));
    expect(screen.getByTestId("visibility")).toHaveAttribute("data-visible", "false");
    act(() => notify([{ isIntersecting: true } as IntersectionObserverEntry], {} as IntersectionObserver));
    expect(screen.getByTestId("visibility")).toHaveAttribute("data-visible", "true");
    view.unmount();
    expect(disconnect).toHaveBeenCalledTimes(1);
  });

  it("rotates pointer disclosure with shared timing and keeps keyboard disclosure immediate", () => {
    render(<Disclosure />);
    const toggle = () => screen.getByRole("button", { name: "Toggle" });
    fireEvent.click(toggle(), { detail: 1 });
    expect(screen.getByText("Activity")).toBeInTheDocument();
    expect(animate).toHaveBeenCalledTimes(1);
    expect(animate).toHaveBeenCalledWith([{ rotate: "0deg" }, { rotate: "90deg" }], expect.objectContaining({ duration: 150 }));
    fireEvent.click(toggle(), { detail: 1 });
    expect(animate).toHaveBeenCalledWith([{ rotate: "90deg" }, { rotate: "0deg" }], expect.objectContaining({ duration: 100 }));
    animate.mockClear();
    fireEvent.click(toggle(), { detail: 0 });
    expect(screen.getByText("Activity")).toBeInTheDocument();
    expect(animate).not.toHaveBeenCalled();
  });

  it("disables pointer disclosure motion when reduced motion is requested", () => {
    reduced = true;
    render(<Disclosure />);
    fireEvent.click(screen.getByRole("button", { name: "Toggle" }), { detail: 1 });
    expect(animate).not.toHaveBeenCalled();
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
    expect(animate).toHaveBeenLastCalledWith([{ rotate: "42deg" }, { rotate: "0deg" }], expect.objectContaining({ duration: 100 }));
    for (const animation of firstAnimations) expect(animation.cancel).toHaveBeenCalled();
    style.mockRestore();
  });
});
