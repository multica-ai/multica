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

describe("recommendTemplate", () => {
  describe("engineer × use_case tiebreaker", () => {
    it.each<UseCase>(["manage_team", "plan_research"])(
      "engineer × [%s] → planning",
      (use_case) => {
        expect(
          recommendTemplate({ role: "engineer", use_case: [use_case] }),
        ).toBe("planning");
      },
    );
    it("engineer × [write_publish] → writing", () => {
      expect(
        recommendTemplate({ role: "engineer", use_case: ["write_publish"] }),
      ).toBe("writing");
    });
    it.each<UseCase>([
      "ship_code",
      "personal_tasks",
      "automate_ops",
      "evaluate",
      "other",
    ])("engineer × [%s] → coding", (use_case) => {
      expect(
        recommendTemplate({ role: "engineer", use_case: [use_case] }),
      ).toBe("coding");
    });
    it("engineer × [] → coding", () => {
      expect(recommendTemplate({ role: "engineer", use_case: [] })).toBe(
        "coding",
      );
    });
  });

  describe("multi-select priority (first matching branch wins)", () => {
    // Engineer + (manage_team OR plan_research) wins over write_publish
    // wins over the default coding fallback. The order inside the
    // recommendTemplate switch is the implicit priority.
    it("engineer × [ship_code, manage_team] → planning (manage_team wins over default)", () => {
      expect(
        recommendTemplate({
          role: "engineer",
          use_case: ["ship_code", "manage_team"],
        }),
      ).toBe("planning");
    });
    it("engineer × [write_publish, ship_code] → writing", () => {
      expect(
        recommendTemplate({
          role: "engineer",
          use_case: ["write_publish", "ship_code"],
        }),
      ).toBe("writing");
    });
    it("engineer × [manage_team, write_publish] → planning (earlier branch wins)", () => {
      expect(
        recommendTemplate({
          role: "engineer",
          use_case: ["manage_team", "write_publish"],
        }),
      ).toBe("planning");
    });
    it("null × [ship_code, write_publish] → coding (fallback priority)", () => {
      expect(
        recommendTemplate({
          role: null,
          use_case: ["ship_code", "write_publish"],
        }),
      ).toBe("coding");
    });
  });

  describe("product × use_case", () => {
    it("product × [ship_code] → coding", () => {
      expect(
        recommendTemplate({ role: "product", use_case: ["ship_code"] }),
      ).toBe("coding");
    });
    it.each<UseCase>(["manage_team", "plan_research", "evaluate", "other"])(
      "product × [%s] → planning",
      (use_case) => {
        expect(
          recommendTemplate({ role: "product", use_case: [use_case] }),
        ).toBe("planning");
      },
    );
    it("product × [] → planning", () => {
      expect(recommendTemplate({ role: "product", use_case: [] })).toBe(
        "planning",
      );
    });
  });

  describe("marketing × use_case", () => {
    it.each<UseCase>(["write_publish", "plan_research"])(
      "marketing × [%s] → writing",
      (use_case) => {
        expect(
          recommendTemplate({ role: "marketing", use_case: [use_case] }),
        ).toBe("writing");
      },
    );
    it("marketing × [manage_team] → planning", () => {
      expect(
        recommendTemplate({ role: "marketing", use_case: ["manage_team"] }),
      ).toBe("planning");
    });
  });

  describe("single-template roles", () => {
    it.each(ALL_USE_CASES)("writer × [%s] → writing", (use_case) => {
      expect(recommendTemplate({ role: "writer", use_case: [use_case] })).toBe(
        "writing",
      );
    });
    it.each(ALL_USE_CASES)("designer × [%s] → assistant", (use_case) => {
      expect(
        recommendTemplate({ role: "designer", use_case: [use_case] }),
      ).toBe("assistant");
    });
    it.each(ALL_USE_CASES)("research × [%s] → planning", (use_case) => {
      expect(
        recommendTemplate({ role: "research", use_case: [use_case] }),
      ).toBe("planning");
    });
    it.each<Role>(["founder", "ops", "student", "other"])(
      "%s → assistant",
      (role) => {
        expect(recommendTemplate({ role, use_case: [] })).toBe("assistant");
      },
    );
  });

  describe("role skipped — use_case fallback", () => {
    it("null × [ship_code] → coding", () => {
      expect(recommendTemplate({ role: null, use_case: ["ship_code"] })).toBe(
        "coding",
      );
    });
    it("null × [write_publish] → writing", () => {
      expect(
        recommendTemplate({ role: null, use_case: ["write_publish"] }),
      ).toBe("writing");
    });
    it.each<UseCase>(["manage_team", "plan_research"])(
      "null × [%s] → planning",
      (use_case) => {
        expect(recommendTemplate({ role: null, use_case: [use_case] })).toBe(
          "planning",
        );
      },
    );
    it("both empty → assistant", () => {
      expect(recommendTemplate({ role: null, use_case: [] })).toBe("assistant");
    });
  });

  describe("exhaustive role coverage", () => {
    it.each(ALL_ROLES)("role=%s returns a valid template id", (role) => {
      const result = recommendTemplate({ role, use_case: [] });
      expect(["coding", "planning", "writing", "assistant"]).toContain(result);
    });
  });
});
