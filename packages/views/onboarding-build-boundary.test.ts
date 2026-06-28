import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const onboardingStepFiles = [
  "step-agent.tsx",
  "step-platform-fork.tsx",
  "step-question.tsx",
  "step-runtime-connect.tsx",
  "step-teammate.tsx",
  "step-welcome.tsx",
  "step-workspace.tsx",
];

describe("onboarding build boundary", () => {
  it("imports platform helpers through leaf exports", () => {
    for (const stepFile of onboardingStepFiles) {
      const source = readFileSync(
        resolve(__dirname, "onboarding/steps", stepFile),
        "utf8",
      );

      expect(source, stepFile).not.toContain("@multica/views/platform\"");
    }
  });
});
