import { afterEach, describe, expect, it } from "vitest";
import { z } from "zod";
import { Recorder } from "../src/recorder";
import { uninstallRecorderHook } from "../src/install";

// The export contract from MUL-4466 §11 expressed as a runtime schema. If a
// future change adds a content-bearing field, `.strict()` here fails the test.
const routeSchema = z
  .object({ origin: z.string().nullable(), pathname: z.string().nullable() })
  .strict();

const incidentSchema = z
  .object({
    id: z.string(),
    offsetMs: z.number(),
    route: routeSchema,
    interaction: z
      .object({
        type: z.enum(["click", "scroll", "input", "navigation", "background"]),
        testId: z.string().optional(),
      })
      .strict(),
    totalDurationMs: z.number(),
    primaryEvidence: z.enum([
      "react_commit",
      "long_task",
      "frame",
      "resource",
      "interaction",
      "insufficient",
    ]),
    mutationCount: z.number(),
    reactCommits: z.array(
      z
        .object({
          boundaryId: z.string().optional(),
          phase: z.enum(["mount", "update", "nested-update", "unknown"]),
          actualDurationMs: z.number(),
        })
        .strict(),
    ),
    longTasks: z.array(z.object({ startOffsetMs: z.number(), durationMs: z.number() }).strict()),
    frames: z.array(
      z
        .object({ startOffsetMs: z.number(), durationMs: z.number(), source: z.enum(["loaf", "raf"]) })
        .strict(),
    ),
    resources: z.array(
      z
        .object({
          origin: z.string().nullable(),
          pathname: z.string().nullable(),
          initiatorType: z.string(),
          durationMs: z.number(),
          startOffsetMs: z.number(),
        })
        .strict(),
    ),
  })
  .strict();

const reportSchema = z
  .object({
    schemaVersion: z.literal("1.0"),
    recorderVersion: z.string(),
    host: z
      .object({
        appVersion: z.string(),
        surface: z.enum(["web", "desktop-renderer"]),
        mode: z.enum(["development", "profiling"]),
      })
      .strict(),
    capabilities: z
      .object({
        longTask: z.boolean(),
        longAnimationFrame: z.boolean(),
        eventTiming: z.boolean(),
        reactCommit: z.boolean(),
        resourceTiming: z.boolean(),
        mutationObserver: z.boolean(),
      })
      .strict(),
    thresholdsMs: z
      .object({
        frameGapMs: z.number(),
        reactCommitMs: z.number(),
        resourceMs: z.number(),
        interactionMs: z.number(),
      })
      .strict(),
    session: z.object({ durationMs: z.number(), incidentCount: z.number() }).strict(),
    incidents: z.array(incidentSchema),
  })
  .strict();

afterEach(() => uninstallRecorderHook());

describe("JSON export contract", () => {
  it("produces a schema-valid report for an empty session", () => {
    const recorder = new Recorder({
      appVersion: "1.2.3",
      surface: "desktop-renderer",
      mode: "development",
    });
    recorder.start();
    recorder.stop();
    const report = recorder.export();
    expect(() => reportSchema.parse(report)).not.toThrow();
    expect(report.host.surface).toBe("desktop-renderer");
    expect(report.session.incidentCount).toBe(0);
  });

  it("carries version, mode, capabilities, and thresholds in the header", () => {
    const recorder = new Recorder({ appVersion: "9.9.9", surface: "web", mode: "profiling" });
    recorder.start();
    recorder.stop();
    const report = recorder.export();
    expect(report.schemaVersion).toBe("1.0");
    expect(report.host.mode).toBe("profiling");
    expect(report.thresholdsMs.reactCommitMs).toBeGreaterThan(0);
    expect(typeof report.capabilities.reactCommit).toBe("boolean");
  });
});
