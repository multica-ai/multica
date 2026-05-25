import { describe, it, expect } from "vitest";
import {
  CAPABILITY_CATALOG,
  DANGEROUS_CAPABILITIES,
  capabilityEntry,
  capabilityLabel,
  capabilityDescription,
} from "./capability-catalog";

describe("capability catalog", () => {
  it("covers the seven dangerous actions the issue requires", () => {
    const keys = DANGEROUS_CAPABILITIES.map((c) => c.key);
    expect(keys).toEqual([
      "shell.exec",
      "network.external",
      "message.send",
      "payment.commit",
      "deploy.restart",
      "credentials.read",
      "prod.write",
    ]);
  });

  it("gives every entry a plain-language label and description", () => {
    for (const entry of CAPABILITY_CATALOG) {
      expect(entry.label.length).toBeGreaterThan(0);
      expect(entry.description.length).toBeGreaterThan(0);
      // Labels are human text, not the raw dotted key.
      expect(entry.label).not.toBe(entry.key);
    }
  });

  it("has unique keys", () => {
    const keys = CAPABILITY_CATALOG.map((c) => c.key);
    expect(new Set(keys).size).toBe(keys.length);
  });

  it("resolves label + description by key", () => {
    expect(capabilityLabel("shell.exec")).toBe("Run a shell command");
    expect(capabilityDescription("payment.commit")).toMatch(/payment|purchase|cost/i);
    expect(capabilityEntry("network.external")?.category).toBe("dangerous");
  });

  it("falls back to the raw key for unknown capabilities so the UI never blanks", () => {
    expect(capabilityLabel("custom.legacy.cap")).toBe("custom.legacy.cap");
    expect(capabilityDescription("custom.legacy.cap")).toBe("");
    expect(capabilityEntry("custom.legacy.cap")).toBeUndefined();
  });
});
