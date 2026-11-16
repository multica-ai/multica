import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

const script = fileURLToPath(new URL("./android-release.mjs", import.meta.url));

function preflight(overrides) {
  return spawnSync(process.execPath, [script], {
    encoding: "utf8",
    env: {
      ...process.env,
      EXPO_PUBLIC_API_URL: "https://api.example.test",
      EXPO_PUBLIC_WEB_URL: "https://app.example.test",
      EXPO_ANDROID_ALLOW_CLEARTEXT: "",
      MULTICA_ANDROID_KEYSTORE: "",
      MULTICA_ANDROID_STORE_PASSWORD_FILE: "",
      MULTICA_ANDROID_KEY_ALIAS: "",
      ...overrides,
    },
  });
}

test("refuses to build without explicit release signing inputs", () => {
  const result = preflight({});
  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /MULTICA_ANDROID_KEYSTORE is required/);
  assert.equal(result.stdout, "");
});

test("rejects HTTP before invoking the build unless explicitly enabled", () => {
  const result = preflight({ EXPO_PUBLIC_API_URL: "http://10.0.2.2:8080" });
  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /HTTP requires EXPO_ANDROID_ALLOW_CLEARTEXT=1/);
  const enabled = preflight({
    EXPO_PUBLIC_API_URL: "http://10.0.2.2:8080",
    EXPO_ANDROID_ALLOW_CLEARTEXT: "1",
  });
  assert.match(enabled.stderr, /MULTICA_ANDROID_KEYSTORE is required/);
});

test("rejects credentials and non-origin endpoints without echoing credentials", () => {
  for (const url of ["https://user:private-password@example.test", "https://example.test/api"]) {
    const result = preflight({ EXPO_PUBLIC_API_URL: url });
    assert.notEqual(result.status, 0);
    assert.match(result.stderr, /must be an HTTP\(S\) origin/);
    assert.doesNotMatch(result.stdout + result.stderr, /private-password/);
  }
});
