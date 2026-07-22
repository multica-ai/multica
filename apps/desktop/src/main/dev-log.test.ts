import { EventEmitter } from "node:events";
import { describe, expect, it, vi } from "vitest";
import { createBestEffortDevLog } from "./dev-log";

function makeSink(write = vi.fn()) {
  return Object.assign(new EventEmitter(), {
    destroyed: false,
    writable: true,
    write,
  });
}

describe("createBestEffortDevLog", () => {
  it("formats renderer diagnostics for the sink", () => {
    const sink = makeSink();
    const log = createBestEffortDevLog(sink);

    log("console", "first", 2);

    expect(sink.write).toHaveBeenCalledWith("[renderer console] first 2\n");
  });

  it("does not throw when a synchronous sink write fails", () => {
    const error = Object.assign(new Error("write EIO"), { code: "EIO" });
    const sink = makeSink(
      vi.fn(() => {
        throw error;
      }),
    );
    const log = createBestEffortDevLog(sink);

    expect(() => log("process-gone", "crashed")).not.toThrow();
  });

  it("handles asynchronous sink errors without an uncaught exception", () => {
    const sink = makeSink();
    createBestEffortDevLog(sink);
    const error = Object.assign(new Error("write EIO"), { code: "EIO" });

    expect(() => sink.emit("error", error)).not.toThrow();
  });

  it("skips writes after the sink is no longer writable", () => {
    const sink = makeSink();
    const log = createBestEffortDevLog(sink);
    sink.writable = false;

    log("console", "ignored");

    expect(sink.write).not.toHaveBeenCalled();
  });

  it("skips writes after the sink is destroyed", () => {
    const sink = makeSink();
    const log = createBestEffortDevLog(sink);
    sink.destroyed = true;

    log("console", "ignored");

    expect(sink.write).not.toHaveBeenCalled();
  });
});
