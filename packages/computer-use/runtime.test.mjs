import assert from "node:assert/strict";
import test from "node:test";
import { spawn } from "node:child_process";
import { createInterface } from "node:readline";

function fixture(t) {
  const runtime = new URL("./runtime.mjs", import.meta.url).href;
  const child = spawn(process.execPath, ["--input-type=module", "-e", `import {startRuntime} from ${JSON.stringify(runtime)}; startRuntime();`], { stdio: ["pipe", "pipe", "pipe"] });
  t.after(() => child.kill());
  const frames = [], waiters = [];
  const lines = createInterface({ input: child.stdout });
  lines.on("line", (line) => { const f = JSON.parse(line); const resolve = waiters.shift(); if (resolve) resolve(f); else frames.push(f); });
  const next = () => frames.length ? Promise.resolve(frames.shift()) : new Promise((resolve) => waiters.push(resolve));
  const send = (value) => child.stdin.write(JSON.stringify(value) + "\n");
  return { next, send };
}

test("persistent REPL supports top-level await, app RPC, and image output", { timeout: 8000 }, async (t) => {
  const f = fixture(t);
  assert.equal((await f.next()).type, "ready");
  f.send({ type: "execute", id: "one", code: "let app = await computer.getApp('Example');" });
  const get = await f.next();
  assert.deepEqual(get.action, { op: "get_app", app: "Example" });
  f.send({ type: "action_result", id: get.id, result: { id: "example.app", name: "Example" } });
  assert.equal((await f.next()).error, undefined);
  f.send({ type: "execute", id: "two", code: "await app.observe();" });
  const observe = await f.next();
  assert.equal(observe.executionId, "two");
  const image = { mimeType: "image/jpeg", data: "abc" };
  f.send({ type: "action_result", id: observe.id, result: { observationId: "s1", elements: [], image } });
  const result = await f.next();
  assert.equal(result.id, "two");
  assert.deepEqual(result.result.image, image);
  assert.doesNotMatch(result.result.value, /abc/);
  f.send({ type: "execute", id: "three", code: "app.id" });
  assert.match((await f.next()).result.value, /example.app/);
});

test("a failed script returns an error without destroying existing variables", { timeout: 8000 }, async (t) => {
  const f = fixture(t); await f.next();
  f.send({ type: "execute", id: "one", code: "let retained = 42;" }); await f.next();
  f.send({ type: "execute", id: "two", code: "throw new Error('failed');" });
  assert.match((await f.next()).error, /failed/);
  f.send({ type: "execute", id: "three", code: "retained" });
  assert.equal((await f.next()).result.value, "42");
});
