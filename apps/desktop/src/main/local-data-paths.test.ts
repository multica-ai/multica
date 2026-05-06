import { describe, expect, it, vi } from "vitest";

// Mock electron's app.getPath to return deterministic paths so we can assert
// the path layout independently of the host machine.
vi.mock("electron", () => ({
  app: {
    getPath: vi.fn((key: string) => {
      if (key === "userData") return "/tmp/multica-user-data";
      if (key === "logs") return "/tmp/multica-logs";
      throw new Error(`unexpected app.getPath key: ${key}`);
    }),
  },
}));

import { resolveLocalDataPaths } from "./local-data-paths";

describe("resolveLocalDataPaths", () => {
  it("places postgres data under <userData>/postgres/data", () => {
    const paths = resolveLocalDataPaths();
    expect(paths.postgresData).toBe("/tmp/multica-user-data/postgres/data");
  });

  it("places postgres logs under <userData>/postgres/logs", () => {
    const paths = resolveLocalDataPaths();
    expect(paths.postgresLogs).toBe("/tmp/multica-user-data/postgres/logs");
  });

  it("places daemon logs under <userData>/daemon/logs", () => {
    const paths = resolveLocalDataPaths();
    expect(paths.daemonLogs).toBe("/tmp/multica-user-data/daemon/logs");
  });

  it("resolves appLogs via app.getPath('logs') (separate from userData on macOS)", () => {
    const paths = resolveLocalDataPaths();
    expect(paths.appLogs).toBe("/tmp/multica-logs");
  });

  it("uses userData root for appConfig", () => {
    const paths = resolveLocalDataPaths();
    expect(paths.appConfig).toBe("/tmp/multica-user-data");
    expect(paths.root).toBe("/tmp/multica-user-data");
  });

  it("returns no empty strings", () => {
    const paths = resolveLocalDataPaths();
    for (const value of Object.values(paths)) {
      expect(value).not.toBe("");
    }
  });

  it("nests every userData-rooted path under the resolved root", () => {
    const paths = resolveLocalDataPaths();
    expect(paths.postgresData.startsWith(paths.root)).toBe(true);
    expect(paths.postgresLogs.startsWith(paths.root)).toBe(true);
    expect(paths.daemonLogs.startsWith(paths.root)).toBe(true);
    expect(paths.appConfig.startsWith(paths.root)).toBe(true);
  });
});
