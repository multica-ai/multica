#!/usr/bin/env node
import assert from "node:assert/strict";
import { test } from "node:test";

import { buildSecretManifest, parseDotenv } from "./sync-env.mjs";

test("parseDotenv keeps .env.bot as the complete key source", () => {
  const entries = parseDotenv(
    `
# comment
POSTGRES_PASSWORD=plain
export DINGTALK_OAUTH_SCOPE="openid corpid Contact.User.Read"
EMPTY=
INLINE=value # comment
SINGLE='literal # hash'
DOUBLE="line\\nnext"
`,
    "fixture.env",
  );

  assert.deepEqual([...entries.keys()], [
    "POSTGRES_PASSWORD",
    "DINGTALK_OAUTH_SCOPE",
    "EMPTY",
    "INLINE",
    "SINGLE",
    "DOUBLE",
  ]);
  assert.equal(entries.get("DINGTALK_OAUTH_SCOPE"), "openid corpid Contact.User.Read");
  assert.equal(entries.get("EMPTY"), "");
  assert.equal(entries.get("INLINE"), "value");
  assert.equal(entries.get("SINGLE"), "literal # hash");
  assert.equal(entries.get("DOUBLE"), "line\nnext");
});

test("parseDotenv rejects duplicate keys", () => {
  assert.throws(
    () => parseDotenv("A=1\nA=2\n", "fixture.env"),
    /duplicate env key "A"/,
  );
});

test("buildSecretManifest writes values only into stringData", () => {
  const entries = new Map([
    ["A", "one"],
    ["B", "two"],
  ]);

  assert.deepEqual(buildSecretManifest(entries, "ns", "secret"), {
    apiVersion: "v1",
    kind: "Secret",
    metadata: {
      name: "secret",
      namespace: "ns",
      labels: {
        "app.kubernetes.io/managed-by": "multica-bot-env-sync",
      },
    },
    type: "Opaque",
    stringData: {
      A: "one",
      B: "two",
    },
  });
});
