// FIR-2742 — pure enrich/filter/sort logic for cross-skill change review.

import { describe, it, expect } from "vitest";
import {
  enrichSkillChanges,
  filterSkillChanges,
  sortSkillChanges,
  selectMyPendingChanges,
} from "./select-changes";
import type { SkillChangeRequest, SkillSummary } from "@multica/core/types";

const skill = (over: Partial<SkillSummary>): SkillSummary =>
  ({
    id: "s1",
    workspace_id: "ws",
    name: "Skill One",
    description: "",
    config: {},
    created_by: "u-creator",
    owner_id: null,
    approver_ids: [],
    current_version: "1.0.0",
    notify_change_requests: true,
    notify_forks: true,
    notify_agent_assigned: true,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    ...over,
  }) as SkillSummary;

const cr = (over: Partial<SkillChangeRequest>): SkillChangeRequest =>
  ({
    id: "cr1",
    skill_id: "s1",
    title: "Tighten rules",
    description: "",
    base_version: "1.0.0",
    proposed_version: "1.1.0",
    proposed_content: "new",
    proposed_files: [],
    status: "pending",
    proposed_by: "u-proposer",
    reviewed_by: null,
    reviewed_at: null,
    review_comment: "",
    created_at: "2026-02-01T00:00:00Z",
    updated_at: "2026-02-01T00:00:00Z",
    ...over,
  }) as SkillChangeRequest;

describe("enrichSkillChanges", () => {
  it("marks a change on a skill I own as mine and joins the skill name/version", () => {
    const e = enrichSkillChanges(
      [cr({})],
      [skill({ id: "s1", name: "Skill One", owner_id: "me", current_version: "2.3.0" })],
      "me",
    )[0]!;
    expect(e.mine).toBe(true);
    expect(e.skill_name).toBe("Skill One");
    expect(e.current_version).toBe("2.3.0");
  });

  it("marks a change as mine when I am an approver (not owner)", () => {
    const e = enrichSkillChanges(
      [cr({})],
      [skill({ id: "s1", owner_id: "someone", approver_ids: ["me"] })],
      "me",
    )[0]!;
    expect(e.mine).toBe(true);
  });

  it("is not mine when I neither own nor approve", () => {
    const e = enrichSkillChanges(
      [cr({})],
      [skill({ id: "s1", owner_id: "someone", approver_ids: ["other"] })],
      "me",
    )[0]!;
    expect(e.mine).toBe(false);
  });

  it("drops a change whose skill is not visible", () => {
    expect(
      enrichSkillChanges([cr({ skill_id: "missing" })], [skill({ id: "s1" })], "me"),
    ).toHaveLength(0);
  });
});

describe("filterSkillChanges", () => {
  const skills = [
    skill({ id: "s1", name: "Alpha", owner_id: "me" }),
    skill({ id: "s2", name: "Beta", owner_id: "other" }),
  ];
  const enriched = enrichSkillChanges(
    [
      cr({ id: "a", skill_id: "s1", title: "Fix alpha" }),
      cr({ id: "b", skill_id: "s2", title: "Fix beta" }),
    ],
    skills,
    "me",
  );

  it("scope=mine keeps only my skills' changes", () => {
    const r = filterSkillChanges(enriched, { scope: "mine", search: "" });
    expect(r.map((c) => c.id)).toEqual(["a"]);
  });

  it("scope=all keeps everything", () => {
    const r = filterSkillChanges(enriched, { scope: "all", search: "" });
    expect(r.map((c) => c.id).sort()).toEqual(["a", "b"]);
  });

  it("search matches skill name or title case-insensitively", () => {
    expect(
      filterSkillChanges(enriched, { scope: "all", search: "beta" }).map((c) => c.id),
    ).toEqual(["b"]);
    expect(
      filterSkillChanges(enriched, { scope: "all", search: "FIX ALPHA" }).map((c) => c.id),
    ).toEqual(["a"]);
  });
});

describe("sortSkillChanges", () => {
  const list = enrichSkillChanges(
    [
      cr({ id: "old", created_at: "2026-01-01T00:00:00Z" }),
      cr({ id: "new", created_at: "2026-03-01T00:00:00Z" }),
    ],
    [skill({ id: "s1", owner_id: "me" })],
    "me",
  );

  it("newest first", () => {
    expect(sortSkillChanges(list, "newest").map((c) => c.id)).toEqual(["new", "old"]);
  });
  it("oldest first", () => {
    expect(sortSkillChanges(list, "oldest").map((c) => c.id)).toEqual(["old", "new"]);
  });
  it("does not mutate the input", () => {
    const before = list.map((c) => c.id);
    sortSkillChanges(list, "oldest");
    expect(list.map((c) => c.id)).toEqual(before);
  });
});

describe("selectMyPendingChanges", () => {
  it("returns only changes on skills I own/approve", () => {
    const r = selectMyPendingChanges(
      [cr({ id: "a", skill_id: "s1" }), cr({ id: "b", skill_id: "s2" })],
      [skill({ id: "s1", owner_id: "me" }), skill({ id: "s2", owner_id: "x" })],
      "me",
    );
    expect(r.map((c) => c.id)).toEqual(["a"]);
  });
});
