import assert from "node:assert/strict";
import test from "node:test";
import { createComputer } from "./sdk.mjs";

test("app objects target the resolved identity and consume observations once", async () => {
  const calls = [];
  const computer = createComputer(async (action) => {
    calls.push(action);
    if (action.op === "get_app") return { id: "example.app", name: "Example" };
    if (action.op === "observe") return { observationId: "snapshot-1", elements: [{ ref: "e1" }] };
    return { status: "applied" };
  });
  const app = await computer.getApp("Example");
  await assert.rejects(app.click("e1"), /Observe/);
  await app.observe();
  await app.click("e1");
  assert.deepEqual(calls.at(-1), { op: "click", app: "example.app", ref: "e1", observationId: "snapshot-1" });
  await assert.rejects(app.typeText("hello"), /Observe/);
});

test("failed actions also invalidate their observation", async () => {
  const app = await createComputer(async ({ op }) => {
    if (op === "get_app") return { id: "app", name: "App" };
    if (op === "observe") return { observationId: "s" };
    throw new Error("outcome_unknown");
  }).getApp("App");
  await app.observe();
  await assert.rejects(app.pressKey("Enter"), /outcome_unknown/);
  await assert.rejects(app.pressKey("Enter"), /Observe/);
});

test("explicit window selection requires a fresh observation", async () => {
  const calls = [];
  const app = await createComputer(async (action) => {
    calls.push(action);
    if (action.op === "get_app") return { id: "app", name: "App" };
    if (action.op === "windows") return { windows: [{ id: "window-2", title: "Second" }] };
    if (action.op === "observe") return { observationId: "s" };
    return { status: "selected" };
  }).getApp("App");
  const list = await app.windows();
  await app.observe();
  await app.selectWindow(list.windows[0].id);
  assert.deepEqual(calls.at(-1), { op: "select_window", app: "app", windowId: "window-2" });
  await assert.rejects(app.click("old-ref"), /Observe/);
});
