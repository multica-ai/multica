import { afterEach, describe, expect, it } from "vitest";
import { Recorder } from "../src/recorder";
import { createRecorder } from "../src/index";
import { RECORDER_HOST_ID } from "../src/constants";
import { uninstallRecorderHook } from "../src/install";

function tick(): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, 0));
}

afterEach(() => {
  document.body.innerHTML = "";
  uninstallRecorderHook();
});

describe("recorder self-surface exclusion (MUL-4466 §10.2)", () => {
  it("ignores clicks from the recorder panel, records clicks elsewhere", async () => {
    const recorder = new Recorder({ appVersion: "t", surface: "web", mode: "development" });
    const clicks = () => recorder.getIncidents().filter((i) => i.interaction.type === "click");

    // Build the panel host + an unrelated control BEFORE start(), so their
    // insertion isn't counted by the mutation collector.
    const host = document.createElement("div");
    host.id = RECORDER_HOST_ID;
    const outside = document.createElement("button");
    document.body.append(host, outside);
    recorder.start();

    // A retargeted panel interaction — a real browser delivers a shadow click to
    // an outer-tree listener with target === the host — must be excluded.
    host.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    await tick();
    expect(clicks()).toHaveLength(0);

    // A click anywhere outside the panel is still recorded.
    outside.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    await tick();
    expect(clicks().length).toBeGreaterThanOrEqual(1);

    recorder.stop();
  });

  it("Howard's repro: mounted panel → start → click a panel control → stop yields no fake incident", async () => {
    const { recorder } = createRecorder({ appVersion: "t", surface: "web", mode: "development" });
    const host = document.getElementById(RECORDER_HOST_ID);
    expect(host).not.toBeNull();

    recorder.start();
    // The panel host was mounted before start(); clicking it (retargeted to the
    // host) must not produce a fake incident.
    host!.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    await tick();
    recorder.stop();

    expect(recorder.export().session.incidentCount).toBe(0);
  });
});
