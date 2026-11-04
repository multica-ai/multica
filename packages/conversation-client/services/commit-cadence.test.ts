// @vitest-environment node
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { COMMIT_INTERVAL_MS, CommitCadence } from "./commit-cadence";

describe("CommitCadence", () => {
  beforeEach(() => { vi.useFakeTimers(); });
  afterEach(() => { vi.useRealTimers(); vi.unstubAllGlobals(); });

  it("commits a burst once, after the interval", () => {
    const commit = vi.fn();
    const cadence = new CommitCadence<string>(commit);
    for (let index = 0; index < 50; index++) cadence.schedule("a");
    vi.advanceTimersByTime(COMMIT_INTERVAL_MS - 1);
    expect(commit).not.toHaveBeenCalled();
    vi.advanceTimersByTime(1);
    expect(commit.mock.calls).toEqual([["a"]]);
  });

  it("does not push the commit back while events keep arriving", () => {
    const commit = vi.fn();
    const cadence = new CommitCadence<string>(commit);
    cadence.schedule("a");
    vi.advanceTimersByTime(COMMIT_INTERVAL_MS - 1);
    cadence.schedule("a");
    vi.advanceTimersByTime(1);
    expect(commit).toHaveBeenCalledTimes(1);
  });

  it("commits each conversation that changed", () => {
    const commit = vi.fn();
    const cadence = new CommitCadence<string>(commit);
    cadence.schedule("a");
    cadence.schedule("b");
    vi.advanceTimersByTime(COMMIT_INTERVAL_MS);
    expect(commit.mock.calls).toEqual([["a"], ["b"]]);
  });

  it("flushes at once on a user action, and not again at the interval", () => {
    const commit = vi.fn();
    const cadence = new CommitCadence<string>(commit);
    cadence.schedule("a");
    cadence.flush();
    expect(commit.mock.calls).toEqual([["a"]]);
    vi.advanceTimersByTime(COMMIT_INTERVAL_MS * 2);
    expect(commit).toHaveBeenCalledTimes(1);
  });

  it("does nothing on a flush with nothing waiting", () => {
    const commit = vi.fn();
    new CommitCadence<string>(commit).flush();
    expect(commit).not.toHaveBeenCalled();
  });

  it("keeps committing in a hidden window, where animation frames never fire", () => {
    const requestAnimationFrame = vi.fn();
    vi.stubGlobal("requestAnimationFrame", requestAnimationFrame);
    vi.stubGlobal("document", { hidden: true, visibilityState: "hidden" });
    const commit = vi.fn();
    const cadence = new CommitCadence<string>(commit);
    cadence.schedule("a");
    vi.advanceTimersByTime(COMMIT_INTERVAL_MS);
    expect(commit).toHaveBeenCalledTimes(1);
    expect(requestAnimationFrame).not.toHaveBeenCalled();
  });

  it("does not re-enter when a commit flushes", () => {
    const seen: string[] = [];
    const cadence: CommitCadence<string> = new CommitCadence<string>((key) => { seen.push(key); cadence.flush(); });
    cadence.schedule("a");
    cadence.schedule("b");
    cadence.flush();
    expect(seen).toEqual(["a", "b"]);
  });

  it("drops what is waiting when disposed", () => {
    const commit = vi.fn();
    const cadence = new CommitCadence<string>(commit);
    cadence.schedule("a");
    cadence.dispose();
    vi.advanceTimersByTime(COMMIT_INTERVAL_MS);
    cadence.flush();
    expect(commit).not.toHaveBeenCalled();
  });
});
