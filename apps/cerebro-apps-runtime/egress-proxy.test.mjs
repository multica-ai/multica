import assert from "node:assert/strict";
import test from "node:test";

import { allowedHost } from "./egress-proxy.mjs";

test("egress only permits the Registry hosts", () => {
  const allowed = new Set(["registry.firtal.com", "firtal-data-registry.sliplane.app"]);
  assert.equal(allowedHost("registry.firtal.com", allowed), true);
  assert.equal(allowedHost("firtal-data-registry.sliplane.app", allowed), true);
  assert.equal(allowedHost("example.com", allowed), false);
  assert.equal(allowedHost("registry.firtal.com.evil.test", allowed), false);
});
