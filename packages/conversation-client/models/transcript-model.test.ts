// @vitest-environment node
import { autorun } from "mobx";
import { describe,expect,it } from "vitest";
import { TranscriptModel } from "./transcript-model";

describe("TranscriptModel", () => {
  it("keeps other turns unobserved during streaming and removes retracted turns", () => {
    const model = new TranscriptModel<{ turnId: string; text: string }>();
    const first = { turnId: "first", text: "done" };
    model.commit("a", [first, { turnId: "last", text: "hel" }]);
    const retained = model.turnMessages("a", "first");
    const reads = { first: 0, last: 0 };
    const stops = [
      autorun(() => {
        model.turnMessages("a", "first");
        reads.first++;
      }),
      autorun(() => {
        model.turnMessages("a", "last");
        reads.last++;
      })
    ];
    model.commit("a", [first, { turnId: "last", text: "hello" }]);
    expect(reads).toEqual({ first: 1, last: 2 });
    expect(model.turnMessages("a", "first")).toBe(retained);
    model.commit("a", [first]);
    expect(model.turnMessages("a", "last")).toEqual([]);
    expect(reads).toEqual({ first: 1, last: 3 });
    stops.forEach((stop) => stop());
  });
  it("returns the same empty list for a conversation with no commit", () => {
    const model = new TranscriptModel<object>();
    expect(model.messages("a")).toHaveLength(0);
    expect(model.messages("a")).toBe(model.messages("b"));
  });

  it("hands back the committed list and its objects", () => {
    const model = new TranscriptModel<object>();
    const message = {};
    const list = [message];
    model.commit("a", list);
    expect(model.messages("a")).toBe(list);
    expect(model.messages("a")[0]).toBe(message);
  });

  it("notifies only readers of the conversation that changed", () => {
    const model = new TranscriptModel<object>();
    const reads = { a: 0, b: 0 };
    const stop = [
      autorun(() => {
        model.messages("a");
        reads.a++;
      }),
      autorun(() => {
        model.messages("b");
        reads.b++;
      })
    ];
    model.commit("a", [{}]);
    expect(reads).toEqual({ a: 2, b: 1 });
    stop.forEach((dispose) => dispose());
  });

  it("keeps the list when a commit changed no message", () => {
    const model = new TranscriptModel<object>();
    const message = {};
    const first = [message];
    model.commit("a", first);
    let reads = 0;
    const stop = autorun(() => {
      model.messages("a");
      reads++;
    });
    model.commit("a", [message]);
    expect(model.messages("a")).toBe(first);
    expect(reads).toBe(1);
    model.commit("a", [{}]);
    expect(reads).toBe(2);
    stop();
  });
});
