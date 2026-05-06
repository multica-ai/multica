import { describe, expect, it, vi } from "vitest";

import type { LocalStackStatus } from "../shared/local-stack-types";
import type { LocalDataPaths } from "./local-data-paths";
import { createDiagnosticsCollector } from "./local-diagnostics";

const samplePaths: LocalDataPaths = {
  root: "/tmp/multica-user-data",
  postgresData: "/tmp/multica-user-data/postgres/data",
  postgresLogs: "/tmp/multica-user-data/postgres/logs",
  daemonLogs: "/tmp/multica-user-data/daemon/logs",
  appLogs: "/tmp/multica-logs",
  appConfig: "/tmp/multica-user-data",
};

const sampleStack: LocalStackStatus = {
  overall: "ready",
  components: [
    { name: "database", state: "ready", detail: null, updatedAt: 1 },
    { name: "migrations", state: "ready", detail: null, updatedAt: 2 },
    { name: "api", state: "ready", detail: null, updatedAt: 3 },
    { name: "bootstrap", state: "ready", detail: null, updatedAt: 4 },
    { name: "daemon", state: "failing", detail: "boom", updatedAt: 5 },
    {
      name: "runtimeRegistration",
      state: "pending",
      detail: null,
      updatedAt: 6,
    },
  ],
};

describe("createDiagnosticsCollector", () => {
  it("snapshots all required fields from the injected dependencies", () => {
    const fixedNow = new Date("2026-05-05T10:11:12.000Z");
    const collector = createDiagnosticsCollector({
      appVersion: "1.2.3",
      apiUrl: "http://localhost:8080",
      os: "macos",
      paths: samplePaths,
      getStackStatus: () => sampleStack,
      getDaemonVersion: () => "0.1.42",
      now: () => fixedNow,
    });

    const snap = collector.snapshot();
    expect(snap.appVersion).toBe("1.2.3");
    expect(snap.apiUrl).toBe("http://localhost:8080");
    expect(snap.os).toBe("macos");
    expect(snap.paths).toEqual(samplePaths);
    expect(snap.stack).toEqual(sampleStack);
    expect(snap.daemonVersion).toBe("0.1.42");
    expect(snap.collectedAt).toBe("2026-05-05T10:11:12.000Z");
  });

  it("calls getStackStatus fresh on each snapshot (no memoization)", () => {
    const stackSpy = vi.fn(() => sampleStack);
    const collector = createDiagnosticsCollector({
      appVersion: "1.0.0",
      apiUrl: "http://localhost:8080",
      os: "linux",
      paths: samplePaths,
      getStackStatus: stackSpy,
      getDaemonVersion: () => null,
    });

    collector.snapshot();
    collector.snapshot();
    collector.snapshot();
    expect(stackSpy).toHaveBeenCalledTimes(3);
  });

  it("preserves null daemonVersion as null (not 'unknown' or '')", () => {
    const collector = createDiagnosticsCollector({
      appVersion: "1.0.0",
      apiUrl: "http://localhost:8080",
      os: "linux",
      paths: samplePaths,
      getStackStatus: () => sampleStack,
      getDaemonVersion: () => null,
    });

    expect(collector.snapshot().daemonVersion).toBeNull();
  });

  it("uses the injected now() for collectedAt", () => {
    const collector = createDiagnosticsCollector({
      appVersion: "1.0.0",
      apiUrl: "http://localhost:8080",
      os: "windows",
      paths: samplePaths,
      getStackStatus: () => sampleStack,
      getDaemonVersion: () => null,
      now: () => new Date("2030-01-02T03:04:05.000Z"),
    });

    expect(collector.snapshot().collectedAt).toBe(
      "2030-01-02T03:04:05.000Z",
    );
  });
});

describe("formatAsText", () => {
  const collector = createDiagnosticsCollector({
    appVersion: "9.9.9",
    apiUrl: "http://localhost:8080",
    os: "macos",
    paths: samplePaths,
    getStackStatus: () => sampleStack,
    getDaemonVersion: () => "0.1.42",
    now: () => new Date("2026-05-05T10:11:12.000Z"),
  });

  it("includes a header line", () => {
    const text = collector.formatAsText(collector.snapshot());
    expect(text).toContain("Multica local diagnostics");
  });

  it("includes app version and api url", () => {
    const text = collector.formatAsText(collector.snapshot());
    expect(text).toContain("9.9.9");
    expect(text).toContain("http://localhost:8080");
  });

  it("includes a line for every stack component with state and detail", () => {
    const text = collector.formatAsText(collector.snapshot());
    for (const component of sampleStack.components) {
      expect(text).toContain(component.name);
      expect(text).toContain(component.state);
    }
    // Failing detail should be visible so the user can paste it into a report.
    expect(text).toContain("boom");
  });

  it("includes every path label and value", () => {
    const text = collector.formatAsText(collector.snapshot());
    for (const [label, value] of Object.entries(samplePaths)) {
      expect(text).toContain(label);
      expect(text).toContain(value);
    }
  });

  it("renders a null daemon version without the literal string 'null'", () => {
    const noDaemon = createDiagnosticsCollector({
      appVersion: "1.0.0",
      apiUrl: "http://localhost:8080",
      os: "macos",
      paths: samplePaths,
      getStackStatus: () => sampleStack,
      getDaemonVersion: () => null,
    });
    const text = noDaemon.formatAsText(noDaemon.snapshot());
    expect(text).toContain("Daemon version");
    expect(text).not.toContain("Daemon version: null");
  });
});
