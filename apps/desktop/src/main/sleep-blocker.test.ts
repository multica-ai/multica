// @vitest-environment node
import { describe, expect, it, vi } from "vitest";
import { createSleepBlocker } from "./sleep-blocker";

function setup() {
  let nextId = 1;
  const start = vi.fn(() => nextId++);
  const stop = vi.fn();
  return { start, stop, blocker: createSleepBlocker({ start, stop }) };
}

describe("createSleepBlocker", () => {
  it("does not block sleep until a mode asks for it", () => {
    const { start, blocker } = setup();
    blocker.setAgentsWorking(true);
    expect(start).not.toHaveBeenCalled();
  });

  it("blocks sleep in on mode whether or not agents are working", () => {
    const { start, stop, blocker } = setup();
    blocker.setMode("on");
    expect(start).toHaveBeenCalledTimes(1);

    blocker.setAgentsWorking(true);
    blocker.setAgentsWorking(false);
    expect(start).toHaveBeenCalledTimes(1);
    expect(stop).not.toHaveBeenCalled();
  });

  it("blocks sleep in agent mode only while agents are working", () => {
    const { start, stop, blocker } = setup();
    blocker.setMode("agent");
    expect(start).not.toHaveBeenCalled();

    blocker.setAgentsWorking(true);
    blocker.setAgentsWorking(true);
    expect(start).toHaveBeenCalledTimes(1);

    blocker.setAgentsWorking(false);
    expect(stop).toHaveBeenCalledExactlyOnceWith(1);

    blocker.setAgentsWorking(true);
    expect(start).toHaveBeenCalledTimes(2);
  });

  it("releases the block when the mode is turned off", () => {
    const { stop, blocker } = setup();
    blocker.setAgentsWorking(true);
    blocker.setMode("on");
    blocker.setMode("off");
    expect(stop).toHaveBeenCalledExactlyOnceWith(1);
  });

  it("keeps one block when switching from on to agent with agents working", () => {
    const { start, stop, blocker } = setup();
    blocker.setAgentsWorking(true);
    blocker.setMode("on");
    blocker.setMode("agent");
    expect(start).toHaveBeenCalledTimes(1);
    expect(stop).not.toHaveBeenCalled();
  });

  it("releases the block when switching from on to agent with no agent working", () => {
    const { stop, blocker } = setup();
    blocker.setMode("on");
    blocker.setMode("agent");
    expect(stop).toHaveBeenCalledExactlyOnceWith(1);
  });
});
