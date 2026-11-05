// @vitest-environment node
import { afterEach, expect, it, vi } from "vitest";
import { measureConversationAsyncWork, measureConversationWork, observeConversationWork } from "./conversationWorkProfile";

const cleanups: (() => void)[] = [];
afterEach(() => {
  cleanups.splice(0).forEach((dispose) => dispose());
  vi.restoreAllMocks();
});

it("does not read a clock without a matching capture and releases its subscription", () => {
  const clock = vi.spyOn(performance, "now");
  const record = vi.fn();
  const stop = observeConversationWork("selected", record);
  cleanups.push(stop);
  expect(measureConversationWork("other", "partition", () => 7)).toBe(7);
  expect(clock).not.toHaveBeenCalled();
  stop();
  expect(measureConversationWork("selected", "partition", () => 9)).toBe(9);
  expect(clock).not.toHaveBeenCalled();
  expect(record).not.toHaveBeenCalled();
  const pending = Promise.resolve(11);
  expect(measureConversationAsyncWork("selected", "source_load", () => pending)).toBe(pending);
  expect(clock).not.toHaveBeenCalled();
});

it("records durations without changing results or swallowing application failures", () => {
  let time = 0;
  vi.spyOn(performance, "now").mockImplementation(() => time);
  const record = vi.fn();
  cleanups.push(observeConversationWork("selected", record));
  cleanups.push(observeConversationWork("selected", () => { throw new Error("diagnostic failure"); }));
  expect(measureConversationWork("selected", "rows_projection", () => { time += 12; return 7; })).toBe(7);
  const failure = new Error("application failure");
  expect(() => measureConversationWork("selected", "partition", () => { time += 4; throw failure; })).toThrow(failure);
  expect(record.mock.calls.map(([sample]) => sample)).toEqual([
    { stage: "rows_projection", durationMs: 12 }, { stage: "partition", durationMs: 4 }
  ]);
});

it("does not attribute an earlier asynchronous read to a replacement capture", async () => {
  const previous = vi.fn(), next = vi.fn();
  const stop = observeConversationWork("selected", previous);
  cleanups.push(stop);
  let resolve!: (value: number) => void;
  const pending = measureConversationAsyncWork("selected", "source_load", () => new Promise<number>((done) => { resolve = done; }));
  stop();
  cleanups.push(observeConversationWork("selected", next));
  resolve(7);
  await expect(pending).resolves.toBe(7);
  expect(previous).not.toHaveBeenCalled();
  expect(next).not.toHaveBeenCalled();
});
