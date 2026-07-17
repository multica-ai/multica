import assert from "node:assert/strict";
import test from "node:test";
import { DEBUG_ASYNC, RELEASE_ASYNC } from "quickjs-emscripten";
import { inspect } from "node:util";

import { executeSandbox } from "./sandbox.mjs";

test("executes JSON input and output without Node globals", async () => {
  const output = await executeSandbox({
    source: `export default (input) => ({ value: input.value.toUpperCase(), process: typeof process, fetch: typeof fetch, require: typeof require })`,
    input: { value: "milk" },
  });
  assert.deepEqual(output, { value: "MILK", process: "undefined", fetch: "undefined", require: "undefined" });
});

test("supports allowlisted async Registry and Connection host calls", async () => {
  const calls = [];
  const output = await executeSandbox({
    source: `export default async (input, multica) => ({
      registry: await multica.registry.call("products", { id: input.id }),
      connection: await multica.connections.call("connection-1", "lookup", { id: input.id })
    })`,
    input: { id: "sku-1" },
    host: {
      registryCall: async (...args) => (calls.push(["registry", ...args]), { name: "Milk" }),
      connectionCall: async (...args) => (calls.push(["connection", ...args]), { stock: 2 }),
    },
  });
  assert.deepEqual(output, { registry: { name: "Milk" }, connection: { stock: 2 } });
  assert.deepEqual(calls, [
    ["registry", "products", { id: "sku-1" }],
    ["connection", "connection-1", "lookup", { id: "sku-1" }],
  ]);
});

test("executes async source byte-for-byte without rewriting strings, comments, or nested functions", async () => {
  const output = await executeSandbox({
    source: `export default async function run(input) {
      // await and async are source text, not rewrite instructions
      const nested = async () => await Promise.resolve(input.value.toUpperCase());
      return { value: await nested(), sourceWords: "await async stay unchanged" };
    }`,
    input: { value: "milk" },
  });
  assert.deepEqual(output, { value: "MILK", sourceWords: "await async stay unchanged" });
});

test("loads relative modules from the immutable backend bundle", async () => {
  const output = await executeSandbox({
    source: `import { format } from "./format.mjs"; export default (input) => ({ value: format(input.value) })`,
    modules: { "backend/format.mjs": `export const format = (value) => value.toUpperCase()` },
    input: { value: "milk" },
  });
  assert.deepEqual(output, { value: "MILK" });
});

// What actually stops the allocation loop below is the deadline, not memoryBytes:
// quickjs-emscripten 0.31.0 does not enforce setMemoryLimit against string
// allocation, so the loop runs to ~2GB of WASM heap and only then aborts. Left
// on the 5s default that cost this one test ~13s of the package's ~14s runtime,
// on every checkout's `make check` — which is what made this package a bad
// neighbour on a shared machine. Bounding the deadline here keeps the subject
// intact (each iteration still allocates hard, aborts, and must release its
// handles in both variants) while cutting the wall-clock cost ~50x. The
// product's own limits are untouched: workers run under the 5s default, and in
// production the container caps memory at 64MB via cgroups regardless of what
// QuickJS accounts for. See FIR-3452.
const MEMORY_PRESSURE_DEADLINE_MS = 200;

test("releases handles in release and debug variants under repeated memory pressure", async () => {
  for (const variant of [RELEASE_ASYNC, DEBUG_ASYNC]) {
    for (let index = 0; index < 2; index++) {
      await assert.rejects(executeSandbox({
        source: `export default () => { const values = []; while (true) values.push("x".repeat(65536)); }`,
        input: {},
        memoryBytes: 4 << 20,
        deadlineMs: MEMORY_PRESSURE_DEADLINE_MS,
        variant,
      }), /App worker failed/);
    }
  }
});

test("rejects Node imports, traversal imports, infinite loops, and oversized output", async () => {
  await assert.rejects(executeSandbox({ source: `import fs from "node:fs"; export default () => fs`, input: {} }), /App worker failed/);
  await assert.rejects(executeSandbox({ source: `import secret from "../secret.mjs"; export default () => secret`, input: {} }), /App worker failed/);
  await assert.rejects(executeSandbox({ source: `export default () => { while (true) {} }`, input: {}, deadlineMs: 20 }), /App worker failed/);
  await assert.rejects(executeSandbox({ source: `export default () => "x".repeat(1_048_577)`, input: {} }), /App worker failed/);
});

test("masks stack traces and host secrets", async () => {
  const original = console.error;
  const logs = [];
  console.error = (...values) => logs.push(values.map((value) => inspect(value)).join(" "));
  try {
    await assert.rejects(
      executeSandbox({ source: `export default () => { throw new Error("/srv/app/private.js secret=abc") }`, input: {} }),
      (error) => {
        assert.equal(error.message, "App worker failed");
        assert.doesNotMatch(error.message, /srv|secret|abc/);
        return true;
      },
    );
    assert.doesNotMatch(logs.join("\n"), /srv|secret|abc/);
  } finally {
    console.error = original;
  }
});
