import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import test from "node:test";

import { BundleStore } from "./bundle-store.mjs";

const entry = (path, content) => ({ path, media_type: "text/html", content_base64: Buffer.from(content).toString("base64"), sha256: createHash("sha256").update(content).digest("hex") });

test("caches immutable frontend files only after every hash matches", async () => {
  let loads = 0;
  const store = new BundleStore({ backend: { bundle: async () => (loads++, { files: [entry("frontend/index.html", "<h1>App</h1>")] }) } });
  assert.equal((await store.get("app", "1.0.0", "frontend/index.html")).content.toString(), "<h1>App</h1>");
  assert.equal((await store.get("app", "1.0.0", "frontend/index.html")).content.toString(), "<h1>App</h1>");
  assert.equal(loads, 1);
});

test("rejects changed bundle content and unsafe paths", async () => {
  const changed = entry("frontend/index.html", "safe");
  changed.content_base64 = Buffer.from("changed").toString("base64");
  const badHash = new BundleStore({ backend: { bundle: async () => ({ files: [changed] }) } });
  await assert.rejects(badHash.get("app", "1.0.0", "frontend/index.html"), /App bundle failed validation/);
  const unsafe = new BundleStore({ backend: { bundle: async () => ({ files: [entry("../secret", "x")] }) } });
  await assert.rejects(unsafe.get("app", "1.0.0", "../secret"), /App bundle failed validation/);
});
