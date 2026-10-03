import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, describe, expect, it } from "vitest";

import { applyDevAuthEnv } from "./dev-auth.mjs";

const cleanups = [];
afterEach(() => {
  while (cleanups.length) cleanups.pop()();
});

function profilesHome() {
  const dir = mkdtempSync(join(tmpdir(), "multica-desktop-auth-"));
  cleanups.push(() => rmSync(dir, { recursive: true, force: true }));
  return dir;
}

describe("desktop dev auth", () => {
  it("loads the token from the selected local development profile", () => {
    const home = profilesHome();
    mkdirSync(join(home, "dev-test"));
    writeFileSync(
      join(home, "dev-test", "config.json"),
      JSON.stringify({ token: "mul_local_token" }),
    );
    const env = {
      MULTICA_DEV_PROFILE: "dev-test",
      MULTICA_DEV_PROFILES_HOME: home,
    };

    expect(applyDevAuthEnv(env)).toBe(true);
    expect(env.VITE_DESKTOP_DEV_AUTH_TOKEN).toBe("mul_local_token");
  });

  it("does not expose a token when the profile is missing or invalid", () => {
    const home = profilesHome();
    const env = {
      MULTICA_DEV_PROFILE: "../production",
      MULTICA_DEV_PROFILES_HOME: home,
    };

    expect(applyDevAuthEnv(env)).toBe(false);
    expect(env.VITE_DESKTOP_DEV_AUTH_TOKEN).toBeUndefined();
  });

  it("preserves an explicit token override", () => {
    const env = { VITE_DESKTOP_DEV_AUTH_TOKEN: "explicit" };
    expect(applyDevAuthEnv(env)).toBe(true);
    expect(env.VITE_DESKTOP_DEV_AUTH_TOKEN).toBe("explicit");
  });
});
