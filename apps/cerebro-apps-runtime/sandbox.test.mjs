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

test("loads relative modules from the immutable backend bundle", async () => {
  const output = await executeSandbox({
    source: `import { format } from "./format.mjs"; export default (input) => ({ value: format(input.value) })`,
    modules: { "backend/format.mjs": `export const format = (value) => value.toUpperCase()` },
    input: { value: "milk" },
  });
  assert.deepEqual(output, { value: "MILK" });
});

test("releases handles in release and debug variants under repeated memory pressure", async () => {
  for (const variant of [RELEASE_ASYNC, DEBUG_ASYNC]) {
    for (let index = 0; index < 2; index++) {
      await assert.rejects(executeSandbox({
        source: `export default () => { const values = []; while (true) values.push("x".repeat(65536)); }`,
        input: {},
        memoryBytes: 4 << 20,
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
