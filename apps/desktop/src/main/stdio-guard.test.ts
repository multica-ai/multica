// @vitest-environment node
import { EventEmitter } from "node:events";
import { describe, expect, it } from "vitest";
import { ignoreBrokenStdio } from "./stdio-guard";

describe("ignoreBrokenStdio", () => {
  it("keeps a failed stdout or stderr write from becoming an uncaught exception", () => {
    const stdout = new EventEmitter();
    const stderr = new EventEmitter();
    ignoreBrokenStdio([stdout, stderr]);
    const error = Object.assign(new Error("write EPIPE"), { code: "EPIPE" });

    expect(() => stdout.emit("error", error)).not.toThrow();
    expect(() => stderr.emit("error", error)).not.toThrow();
  });
});
