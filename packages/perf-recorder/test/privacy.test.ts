import { afterEach, describe, expect, it } from "vitest";
import { Recorder } from "../src/recorder";
import { uninstallRecorderHook } from "../src/install";

const FORBIDDEN_GETTERS = [
  "innerText",
  "textContent",
  "value",
  "defaultValue",
  "placeholder",
  "title",
  "ariaLabel",
];

function tick(): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, 0));
}

afterEach(() => {
  document.body.innerHTML = "";
  uninstallRecorderHook();
});

describe("Standard mode privacy boundary", () => {
  it("resolves a registered testId without ever reading page content", async () => {
    let forbiddenRead = false;
    const row = document.createElement("div");
    row.setAttribute("data-testid", "issue-row");
    const span = document.createElement("span");
    for (const key of FORBIDDEN_GETTERS) {
      Object.defineProperty(span, key, {
        configurable: true,
        get() {
          forbiddenRead = true;
          return "SECRET-USER-CONTENT";
        },
      });
    }
    row.appendChild(span);
    document.body.appendChild(row);

    const recorder = new Recorder({
      appVersion: "test",
      surface: "web",
      mode: "development",
      testIdAllowlist: ["issue-row"],
    });
    recorder.start();

    span.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    // let the MutationObserver flush
    row.appendChild(document.createElement("b"));
    await tick();

    recorder.stop();
    const report = recorder.export();

    // The interaction target was identified by its registered testId only.
    const click = report.incidents.find((i) => i.interaction.type === "click");
    expect(click?.interaction.testId).toBe("issue-row");
    // No forbidden DOM getter was ever accessed by any collector.
    expect(forbiddenRead).toBe(false);
    // And no user content leaked into the export.
    expect(JSON.stringify(report)).not.toContain("SECRET-USER-CONTENT");
  });

  it("does not emit an unregistered testId", async () => {
    const row = document.createElement("div");
    row.setAttribute("data-testid", "not-registered");
    document.body.appendChild(row);
    const recorder = new Recorder({ appVersion: "test", surface: "web", mode: "development" });
    recorder.start();
    row.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    await tick();
    recorder.stop();
    const click = recorder.export().incidents.find((i) => i.interaction.type === "click");
    expect(click?.interaction.testId).toBeUndefined();
  });

  it("MutationObserver contributes only a count, never record contents", async () => {
    const recorder = new Recorder({ appVersion: "test", surface: "web", mode: "development" });
    recorder.start();
    // open an interaction window so mutations attach somewhere
    document.body.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    const secret = document.createElement("div");
    secret.setAttribute("data-secret", "TOP-SECRET-ATTR");
    secret.textContent = "TOP-SECRET-TEXT";
    document.body.appendChild(secret);
    await tick();
    recorder.stop();
    const report = recorder.export();
    const serialized = JSON.stringify(report);
    expect(serialized).not.toContain("TOP-SECRET-ATTR");
    expect(serialized).not.toContain("TOP-SECRET-TEXT");
    // a mutationCount field exists and is numeric
    expect(report.incidents.every((i) => typeof i.mutationCount === "number")).toBe(true);
  });
});
