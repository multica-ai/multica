import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

const pageSource = readFileSync(
  "autopilots/components/autopilots-page.tsx",
  "utf8",
);

describe("built-in Autopilot templates", () => {
  it("queries only backlog issues in the bug triage template", () => {
    const prompt = pageSource.match(/id: "bug_triage",\s*prompt: `([^`]*)`/s)?.[1];

    expect(prompt).toBeDefined();
    expect(prompt?.split("\n", 1)[0]?.trimEnd()).toBe(
      "1. List all backlog issues that have not been prioritized",
    );
    expect(prompt).not.toContain('status "triage"');
  });
});
