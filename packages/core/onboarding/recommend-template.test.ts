import { describe, expect, it } from "vitest";
import { recommendTemplate } from "./recommend-template";
import type { Role, UseCase } from "./types";

const ALL_USE_CASES: UseCase[] = [
  "coding",
  "planning",
  "writing_research",
  "explore",
  "other",
];

describe("recommendTemplate", () => {
  describe("identity fallbacks — role alone decides", () => {
    it.each(ALL_USE_CASES)(
      "role=other (use_case=%s) → assistant",
      (use_case) => {
        expect(recommendTemplate({ role: "other", use_case })).toBe(
          "assistant",
        );
      },
    );

    it.each(ALL_USE_CASES)(
      "role=founder (use_case=%s) → assistant",
      (use_case) => {
        expect(recommendTemplate({ role: "founder", use_case })).toBe(
          "assistant",
        );
      },
    );

    it.each(ALL_USE_CASES)(
      "role=writer (use_case=%s) → writing",
      (use_case) => {
        expect(recommendTemplate({ role: "writer", use_case })).toBe(
          "writing",
        );
      },
    );
  });

  describe("developer × use_case tiebreaker", () => {
    it("developer × planning → planning", () => {
      expect(
        recommendTemplate({ role: "developer", use_case: "planning" }),
      ).toBe("planning");
    });

    it.each<UseCase>([
      "coding",
      "writing_research",
      "explore",
      "other",
    ])("developer × %s → coding", (use_case) => {
      expect(recommendTemplate({ role: "developer", use_case })).toBe(
        "coding",
      );
    });

    it("developer × null use_case → coding (default)", () => {
      expect(
        recommendTemplate({ role: "developer", use_case: null }),
      ).toBe("coding");
    });
  });

  describe("product_lead × use_case tiebreaker", () => {
    it("product_lead × coding → coding", () => {
      expect(
        recommendTemplate({ role: "product_lead", use_case: "coding" }),
      ).toBe("coding");
    });

    it.each<UseCase>([
      "planning",
      "writing_research",
      "explore",
      "other",
    ])("product_lead × %s → planning", (use_case) => {
      expect(recommendTemplate({ role: "product_lead", use_case })).toBe(
        "planning",
      );
    });

    it("product_lead × null use_case → planning (default)", () => {
      expect(
        recommendTemplate({ role: "product_lead", use_case: null }),
      ).toBe("planning");
    });
  });

  describe("unanswered questionnaire", () => {
    it("null role → assistant regardless of use_case", () => {
      expect(recommendTemplate({ role: null, use_case: null })).toBe(
        "assistant",
      );
      expect(recommendTemplate({ role: null, use_case: "coding" })).toBe(
        "assistant",
      );
    });
  });

  describe("exhaustive role coverage", () => {
    const roles: Role[] = [
      "developer",
      "product_lead",
      "writer",
      "founder",
      "other",
    ];
    it.each(roles)("role=%s returns a valid template id", (role) => {
      const result = recommendTemplate({ role, use_case: null });
      expect(["coding", "planning", "writing", "assistant"]).toContain(result);
    });
  });
});
