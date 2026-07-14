import { afterEach, describe, expect, it } from "vitest";
import { Recorder } from "../src/recorder";
import { uninstallRecorderHook } from "../src/install";

function tick(): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, 0));
}

afterEach(() => {
  document.body.innerHTML = "";
  uninstallRecorderHook();
});

describe("recording lifecycle", () => {
  it("moves through idle → recording → stopped → idle", () => {
    const recorder = new Recorder({ appVersion: "t", surface: "web", mode: "development" });
    expect(recorder.getState()).toBe("idle");
    recorder.start();
    expect(recorder.getState()).toBe("recording");
    recorder.stop();
    expect(recorder.getState()).toBe("stopped");
    recorder.clear();
    expect(recorder.getState()).toBe("idle");
  });

  it("degrades on unsupported entry types instead of throwing", () => {
    // The jsdom test env supports some PerformanceObserver entry types but not
    // others (e.g. no `longtask`). start() must probe capabilities and skip the
    // unsupported collectors without throwing.
    const recorder = new Recorder({ appVersion: "t", surface: "web", mode: "development" });
    expect(() => recorder.start()).not.toThrow();
    const caps = recorder.getCapabilities();
    // longtask is not implemented in jsdom → must be reported as unavailable.
    expect(caps.longTask).toBe(false);
    // every capability is a strict boolean regardless of environment.
    expect(Object.values(caps).every((v) => typeof v === "boolean")).toBe(true);
    recorder.stop();
  });

  it("releases the MutationObserver on stop (no incidents after stop)", async () => {
    const recorder = new Recorder({ appVersion: "t", surface: "web", mode: "development" });
    recorder.start();
    recorder.stop();
    document.body.appendChild(document.createElement("div"));
    await tick();
    expect(recorder.getIncidents()).toHaveLength(0);
  });

  it("start() drops prior un-exported data", async () => {
    const recorder = new Recorder({ appVersion: "t", surface: "web", mode: "development" });
    recorder.start();
    document.body.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    await tick();
    recorder.stop();
    expect(recorder.getIncidents().length).toBeGreaterThanOrEqual(1);
    recorder.start();
    expect(recorder.getIncidents()).toHaveLength(0);
    recorder.stop();
  });

  it("is idempotent on repeated stop", () => {
    const recorder = new Recorder({ appVersion: "t", surface: "web", mode: "development" });
    recorder.start();
    recorder.stop();
    expect(() => recorder.stop()).not.toThrow();
  });

  it("only allows export in the stopped state (RFC §7)", () => {
    const recorder = new Recorder({ appVersion: "t", surface: "web", mode: "development" });
    // idle
    expect(recorder.canExport()).toBe(false);
    expect(() => recorder.export()).toThrow();
    // recording
    recorder.start();
    expect(recorder.canExport()).toBe(false);
    expect(() => recorder.export()).toThrow();
    // stopped
    recorder.stop();
    expect(recorder.canExport()).toBe(true);
    expect(() => recorder.export()).not.toThrow();
  });
});
