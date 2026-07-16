import assert from "node:assert/strict";
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

test("mints an opaque bundle token bound to one app version", () => {
  const now = new Date("2026-07-15T18:00:00Z");
  const token = mintBundleToken("secret", "f1540000-0000-4154-8154-000000000001", "1.0.0", now);
  assert.equal(token.split(".").length, 2);
  assert.doesNotMatch(token, /secret/);
  assert.notEqual(token, mintBundleToken("secret", "f1540000-0000-4154-8154-000000000001", "2.0.0", now));
});
