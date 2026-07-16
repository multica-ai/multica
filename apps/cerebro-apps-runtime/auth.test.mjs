import assert from "node:assert/strict";
import { createHash, createHmac } from "node:crypto";
import test from "node:test";

import { mintBundleToken, signServiceRequest, verifyServiceRequest } from "./auth.mjs";

test("binds signed service requests to method, path, body, and a two minute window", () => {
  const now = new Date("2026-07-15T18:00:00Z");
  const body = Buffer.from('{"status":"ready"}');
  const signed = signServiceRequest("secret", "POST", "/callback", body, now);
  assert.equal(verifyServiceRequest("secret", "POST", "/callback", body, signed, now), true);
  assert.equal(verifyServiceRequest("secret", "POST", "/other", body, signed, now), false);
  assert.equal(verifyServiceRequest("secret", "POST", "/callback", Buffer.from("{}"), signed, now), false);
  assert.equal(verifyServiceRequest("secret", "POST", "/callback", body, signed, new Date(now.getTime() + 121_000)), false);
});

test("accepts Go RFC3339 signatures without normalizing their timestamp", () => {
  const secret = "secret";
  const method = "POST";
  const path = "/deployments";
  const body = Buffer.from("{}");
  const timestamp = "2026-07-15T18:00:00Z";
  const bodyHash = createHash("sha256").update(body).digest("hex");
  const signature = `sha256=${createHmac("sha256", secret).update(`${method}\n${path}\n${bodyHash}\n${timestamp}`).digest("hex")}`;

  assert.equal(verifyServiceRequest(secret, method, path, body, { timestamp, signature }, new Date(timestamp)), true);
});

test("mints an opaque bundle token bound to one app version", () => {
  const now = new Date("2026-07-15T18:00:00Z");
  const token = mintBundleToken("secret", "f1540000-0000-4154-8154-000000000001", "1.0.0", now);
  assert.equal(token.split(".").length, 2);
  assert.doesNotMatch(token, /secret/);
  assert.notEqual(token, mintBundleToken("secret", "f1540000-0000-4154-8154-000000000001", "2.0.0", now));
});
