import { homedir } from "os";
import { join } from "path";
import { describe, expect, it } from "vitest";

import {
  DEFAULT_HEALTH_PORT,
  deriveProfileName,
  healthPortForProfile,
  profileArgs,
  profileConfigPath,
  profileDir,
  profileLogPath,
  profileUserIdPath,
} from "./daemon-profile";

const MULTICA_DIR = join(homedir(), ".multica");
const DEFAULT_CLI_CONFIG = join(MULTICA_DIR, "config.json");

describe("deriveProfileName", () => {
  it("names the profile after the target host", () => {
    expect(deriveProfileName("https://api.multica.ai")).toBe(
      "desktop-api.multica.ai",
    );
  });

  it("replaces the port colon so the name is path-safe", () => {
    expect(deriveProfileName("http://localhost:8080")).toBe(
      "desktop-localhost-8080",
    );
  });

  it("falls back to a fixed name on an unparseable URL", () => {
    expect(deriveProfileName("not a url")).toBe("desktop");
  });
});

describe("profile paths", () => {
  it("always resolves under profiles/<name>", () => {
    const dir = join(MULTICA_DIR, "profiles", "desktop-api.multica.ai");
    expect(profileDir("desktop-api.multica.ai")).toBe(dir);
    expect(profileConfigPath("desktop-api.multica.ai")).toBe(
      join(dir, "config.json"),
    );
    expect(profileLogPath("desktop-api.multica.ai")).toBe(
      join(dir, "daemon.log"),
    );
    expect(profileUserIdPath("desktop-api.multica.ai")).toBe(
      join(dir, ".desktop-user-id"),
    );
  });

  // Regression: an unresolved profile used to resolve to ~/.multica, so Desktop
  // could overwrite server_url and token in the user's own CLI config. #6399.
  it("refuses to build a path for an unresolved profile", () => {
    expect(() => profileDir("")).toThrow(/unresolved/);
    expect(() => profileConfigPath("")).toThrow(/unresolved/);
    expect(() => profileLogPath("")).toThrow(/unresolved/);
    expect(() => profileUserIdPath("")).toThrow(/unresolved/);
  });

  it("never yields the default CLI config path for any input", () => {
    for (const name of ["desktop-api.multica.ai", "desktop", "x"]) {
      expect(profileConfigPath(name)).not.toBe(DEFAULT_CLI_CONFIG);
    }
    expect(() => profileConfigPath("")).toThrow();
  });
});

describe("profileArgs", () => {
  it("selects the Desktop-owned profile", () => {
    expect(profileArgs("desktop-api.multica.ai")).toEqual([
      "--profile",
      "desktop-api.multica.ai",
    ]);
  });

  // Regression: this returned [] for an unresolved profile, so the bundled CLI
  // ran against the user's default profile instead of Desktop's. #6399.
  it("refuses to spawn the CLI without a profile flag", () => {
    expect(() => profileArgs("")).toThrow(/unresolved/);
  });
});

describe("healthPortForProfile", () => {
  // Regression: this returned 19514 — the default profile's port — for an
  // unresolved profile, so Desktop would probe the user's own CLI daemon and
  // report it as its own. #6399.
  it("refuses to hand out a port for an unresolved profile", () => {
    expect(() => healthPortForProfile("")).toThrow(/unresolved/);
  });

  it("never derives the default profile's port", () => {
    for (const name of ["desktop-api.multica.ai", "desktop", "x", "a".repeat(50)]) {
      expect(healthPortForProfile(name)).not.toBe(DEFAULT_HEALTH_PORT);
    }
  });

  it("derives a stable per-profile port above the default", () => {
    const port = healthPortForProfile("desktop-api.multica.ai");
    expect(port).toBeGreaterThan(DEFAULT_HEALTH_PORT);
    expect(port).toBe(healthPortForProfile("desktop-api.multica.ai"));
  });
});
