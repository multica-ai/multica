// @vitest-environment node
import { describe, expect, it } from "vitest";

import { daemonSpawnEnv } from "./daemon-spawn-env";

describe("daemonSpawnEnv", () => {
  it("drops electron-vite's NODE_ENV and its marker under a dev Desktop", () => {
    const env = daemonSpawnEnv({
      PATH: "/usr/bin",
      NODE_ENV: "development",
      NODE_ENV_ELECTRON_VITE: "development",
    });

    expect(env).toEqual({ PATH: "/usr/bin", MULTICA_LAUNCHED_BY: "desktop" });
  });

  it("keeps a NODE_ENV that electron-vite did not set", () => {
    const env = daemonSpawnEnv({ PATH: "/usr/bin", NODE_ENV: "test" });

    expect(env).toEqual({
      PATH: "/usr/bin",
      NODE_ENV: "test",
      MULTICA_LAUNCHED_BY: "desktop",
    });
  });

  it("does not mutate the source env", () => {
    const source = {
      NODE_ENV: "development",
      NODE_ENV_ELECTRON_VITE: "development",
    };

    daemonSpawnEnv(source);

    expect(source).toEqual({
      NODE_ENV: "development",
      NODE_ENV_ELECTRON_VITE: "development",
    });
  });
});
