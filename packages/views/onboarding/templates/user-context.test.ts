import { describe, expect, it } from "vitest";
import {
  buildUserContextSection,
  type UserContextLabels,
} from "./user-context";

const EN_LABELS: UserContextLabels = {
  heading: "About me",
  roleLabel: "Role",
  useCaseLabel: "What I want to do",
  listSeparator: ", ",
  role: {
    engineer: "Engineer / developer",
    product: "Product manager",
    other: "Other",
  },
  useCase: {
    ship_code: "Ship code",
    manage_team: "Manage team",
    other: "Other",
  },
};

const ZH_LABELS: UserContextLabels = {
  heading: "关于我",
  roleLabel: "角色",
  useCaseLabel: "想用 Multica 做",
  listSeparator: "、",
  role: { engineer: "工程师", product: "产品经理", other: "其他" },
  useCase: {
    ship_code: "写代码",
    manage_team: "管理团队",
    other: "其他",
  },
};

describe("buildUserContextSection", () => {
  it("returns empty string when questionnaire is undefined/null", () => {
    expect(buildUserContextSection(undefined, EN_LABELS)).toBe("");
    expect(buildUserContextSection(null, EN_LABELS)).toBe("");
  });

  it("returns empty string when role + use_case are both missing", () => {
    expect(buildUserContextSection({}, EN_LABELS)).toBe("");
    expect(
      buildUserContextSection(
        { role: "", role_other: "", use_case: [], use_case_other: "" },
        EN_LABELS,
      ),
    ).toBe("");
  });

  it("renders role only when use_case is empty", () => {
    const out = buildUserContextSection(
      { role: "engineer", use_case: [] },
      EN_LABELS,
    );
    expect(out).toContain("**About me**");
    expect(out).toContain("Role: Engineer / developer");
    expect(out).not.toContain("What I want to do");
  });

  it("renders use_case only when role is empty", () => {
    const out = buildUserContextSection(
      { role: "", use_case: ["ship_code"] },
      EN_LABELS,
    );
    expect(out).toContain("**About me**");
    expect(out).toContain("What I want to do: Ship code");
    expect(out).not.toContain("Role:");
  });

  it("joins multi-select use_case with the locale separator", () => {
    expect(
      buildUserContextSection(
        { role: "engineer", use_case: ["ship_code", "manage_team"] },
        EN_LABELS,
      ),
    ).toContain("Ship code, Manage team");
    expect(
      buildUserContextSection(
        { role: "engineer", use_case: ["ship_code", "manage_team"] },
        ZH_LABELS,
      ),
    ).toContain("写代码、管理团队");
  });

  it("uses role_other free-text when role slug is 'other'", () => {
    const out = buildUserContextSection(
      { role: "other", role_other: "Teacher" },
      EN_LABELS,
    );
    expect(out).toContain("Role: Teacher");
    // The literal "Other" enum label must NOT leak when the free-text
    // is the actual answer.
    expect(out).not.toMatch(/Role: Other$/m);
  });

  it("uses use_case_other free-text alongside other use_case picks", () => {
    const out = buildUserContextSection(
      {
        use_case: ["ship_code", "other"],
        use_case_other: "study group coordination",
      },
      EN_LABELS,
    );
    expect(out).toContain("Ship code, study group coordination");
  });

  it("drops 'other' silently when use_case_other free-text is blank", () => {
    const out = buildUserContextSection(
      { use_case: ["ship_code", "other"], use_case_other: "" },
      EN_LABELS,
    );
    expect(out).toContain("Ship code");
    // Blank free-text on Other => the slug contributes nothing,
    // not even the generic "Other" label.
    expect(out).not.toMatch(/, Other$/m);
  });

  it("tolerates the legacy single-string use_case shape", () => {
    // Questionnaires written before the multi-select migration stored
    // use_case as a single string. The coercer wraps it into [x].
    const out = buildUserContextSection(
      { use_case: "ship_code" },
      EN_LABELS,
    );
    expect(out).toContain("What I want to do: Ship code");
  });

  it("falls back to the slug when a label is missing from the map", () => {
    // The labels map only covers a subset; an unmapped slug renders
    // raw rather than erroring out (defensive against locale drift).
    const out = buildUserContextSection(
      { role: "unknown_role" },
      EN_LABELS,
    );
    expect(out).toContain("Role: unknown_role");
  });

  it("starts with a horizontal rule so it doesn't fuse with the prompt", () => {
    const out = buildUserContextSection(
      { role: "engineer" },
      EN_LABELS,
    );
    expect(out.startsWith("\n\n---\n\n")).toBe(true);
  });
});
