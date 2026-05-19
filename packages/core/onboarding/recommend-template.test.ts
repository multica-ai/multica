import { describe, expect, it } from "vitest";
import { recommendTemplate } from "./recommend-template";
import type { Role, UseCase } from "./types";

const ALL_USE_CASES: UseCase[] = [
  "ship_code",
  "manage_team",
  "personal_tasks",
  "plan_research",
  "write_publish",
  "automate_ops",
  "evaluate",
  "other",
];

const ALL_ROLES: Role[] = [
  "engineer",
  "product",
  "designer",
  "founder",
  "marketing",
  "writer",
  "research",
  "ops",
  "student",
  "other",
];

describe("recommendTemplate (v2)", () => {
  describe("engineer × use_case tiebreaker", () => {
    it.each<UseCase>(["manage_team", "plan_research"])(
      "engineer × %s → planning",
      (use_case) => {
        expect(recommendTemplate({ role: "engineer", use_case })).toBe(
          "planning",
        );
      },
    );
    it("engineer × write_publish → writing", () => {
      expect(
        recommendTemplate({ role: "engineer", use_case: "write_publish" }),
      ).toBe("writing");
    });
    it.each<UseCase>([
      "ship_code",
      "personal_tasks",
      "automate_ops",
      "evaluate",
      "other",
    ])("engineer × %s → coding", (use_case) => {
      expect(recommendTemplate({ role: "engineer", use_case })).toBe("coding");
    });
    it("engineer × null → coding", () => {
      expect(recommendTemplate({ role: "engineer", use_case: null })).toBe(
        "coding",
      );
    });
  });

  describe("product × use_case", () => {
    it("product × ship_code → coding", () => {
      expect(
        recommendTemplate({ role: "product", use_case: "ship_code" }),
      ).toBe("coding");
    });
    it.each<UseCase>(["manage_team", "plan_research", "evaluate", "other"])(
      "product × %s → planning",
      (use_case) => {
        expect(recommendTemplate({ role: "product", use_case })).toBe(
          "planning",
        );
      },
    );
    it("product × null → planning", () => {
      expect(recommendTemplate({ role: "product", use_case: null })).toBe(
        "planning",
      );
    });
  });

  describe("marketing × use_case", () => {
    it.each<UseCase>(["write_publish", "plan_research"])(
      "marketing × %s → writing",
      (use_case) => {
        expect(recommendTemplate({ role: "marketing", use_case })).toBe(
          "writing",
        );
      },
    );
    it("marketing × manage_team → planning", () => {
      expect(
        recommendTemplate({ role: "marketing", use_case: "manage_team" }),
      ).toBe("planning");
    });
  });

  describe("single-template roles", () => {
    it.each(ALL_USE_CASES)("writer × %s → writing", (use_case) => {
      expect(recommendTemplate({ role: "writer", use_case })).toBe("writing");
    });
    it.each(ALL_USE_CASES)("designer × %s → assistant", (use_case) => {
      expect(recommendTemplate({ role: "designer", use_case })).toBe(
        "assistant",
      );
    });
    it.each(ALL_USE_CASES)("research × %s → planning", (use_case) => {
      expect(recommendTemplate({ role: "research", use_case })).toBe(
        "planning",
      );
    });
    it.each<Role>(["founder", "ops", "student", "other"])(
      "%s → assistant",
      (role) => {
        expect(recommendTemplate({ role, use_case: null })).toBe("assistant");
      },
    );
  });

  describe("role skipped — use_case fallback", () => {
    it("null × ship_code → coding", () => {
      expect(recommendTemplate({ role: null, use_case: "ship_code" })).toBe(
        "coding",
      );
    });
    it("null × write_publish → writing", () => {
      expect(
        recommendTemplate({ role: null, use_case: "write_publish" }),
      ).toBe("writing");
    });
    it.each<UseCase>(["manage_team", "plan_research"])(
      "null × %s → planning",
      (use_case) => {
        expect(recommendTemplate({ role: null, use_case })).toBe("planning");
      },
    );
    it("both null → assistant", () => {
      expect(recommendTemplate({ role: null, use_case: null })).toBe(
        "assistant",
      );
    });
  });

  describe("exhaustive role coverage", () => {
    it.each(ALL_ROLES)("role=%s returns a valid template id", (role) => {
      const result = recommendTemplate({ role, use_case: null });
      expect(["coding", "planning", "writing", "assistant"]).toContain(result);
    });
  });
});
