import { describe, expect, it } from "vitest";
import {
  buildInvocationTargets,
  decodeBuilderInput,
  deriveDuplicateAccess,
  encodeBuilderInput,
  mergeBuilderDraft,
  parseBuilderDraft,
  stripBuilderDraft,
  type AgentDraft,
} from "./agent-creation-studio";

const draft = (): AgentDraft => ({
  name: "Old name",
  description: "Old description",
  instructions: "Old instructions",
  avatarUrl: null,
  runtimeId: "runtime-1",
  model: "model-1",
  skillIds: new Set(["skill-1"]),
  permissionScope: "private",
  memberIds: new Set(),
  teamIds: new Set(),
});

describe("Agent creation studio builder protocol", () => {
  it("parses and hides the structured draft block", () => {
    const content =
      'Here is a first draft.\n<agent_draft>{"name":"Researcher","permission_scope":"workspace"}</agent_draft>';

    expect(parseBuilderDraft(content)).toEqual({
      name: "Researcher",
      permission_scope: "workspace",
    });
    expect(stripBuilderDraft(content)).toBe("Here is a first draft.");
    expect(parseBuilderDraft("<agent_draft>not json</agent_draft>")).toBeNull();
  });

  it("repairs literal line breaks emitted inside the instructions string", () => {
    const content = `<agent_draft>{"name":"Reviewer","instructions":"# Role
Review every change.

# Output
Return findings."}</agent_draft>`;

    expect(parseBuilderDraft(content)).toEqual({
      name: "Reviewer",
      instructions: "# Role\nReview every change.\n\n# Output\nReturn findings.",
    });
  });

  it("round-trips only the user's natural-language request for chat display", () => {
    const content = encodeBuilderInput(
      "Create a release manager",
      draft(),
      [],
      [],
      { id: "runtime-1", name: "Codex", provider: "codex" },
      [{ id: "gpt-5.5", label: "GPT-5.5", provider: "openai" }],
    );

    expect(decodeBuilderInput(content)).toBe("Create a release manager");
    expect(JSON.parse(content.slice(content.indexOf("\n") + 1))).toMatchObject({
      selected_runtime: {
        id: "runtime-1",
        name: "Codex",
        provider: "codex",
      },
      available_runtime_models: [
        { id: "gpt-5.5", label: "GPT-5.5", provider: "openai" },
      ],
    });
    expect(decodeBuilderInput("ordinary chat message")).toBe(
      "ordinary chat message",
    );
  });

  it("merges safe fields and rejects unknown workspace references", () => {
    const result = mergeBuilderDraft(
      draft(),
      {
        name: "Release manager",
        model: 123,
        skill_ids: ["skill-2", "unknown-skill"],
        permission_scope: "members",
        member_ids: ["member-1", "unknown-member"],
      },
      new Set(["skill-1", "skill-2"]),
      new Set(["member-1"]),
      new Set(["model-1"]),
    );

    expect(result.name).toBe("Release manager");
    expect(result.model).toBe("model-1");
    expect([...result.skillIds]).toEqual(["skill-2"]);
    expect(result.permissionScope).toBe("members");
    expect([...result.memberIds]).toEqual(["member-1"]);
  });

  it("accepts catalog models and rejects invented or cross-runtime models", () => {
    const validModelIds = new Set(["gpt-5.5", "gpt-5.3-codex"]);

    expect(
      mergeBuilderDraft(
        draft(),
        { model: "gpt-5.5" },
        new Set(),
        new Set(),
        validModelIds,
      ).model,
    ).toBe("gpt-5.5");
    expect(
      mergeBuilderDraft(
        draft(),
        { model: "claude-3-5-sonnet" },
        new Set(),
        new Set(),
        validModelIds,
      ).model,
    ).toBe("model-1");
    expect(
      mergeBuilderDraft(
        draft(),
        { model: "invented-model" },
        new Set(),
        new Set(),
        validModelIds,
      ).model,
    ).toBe("model-1");
    expect(
      mergeBuilderDraft(
        draft(),
        { model: "" },
        new Set(),
        new Set(),
        validModelIds,
      ).model,
    ).toBe("");
  });

  it("preserves a user-selected custom model when the catalog is unavailable", () => {
    expect(
      mergeBuilderDraft(
        draft(),
        { model: "invented-model" },
        new Set(),
        new Set(),
        null,
      ).model,
    ).toBe("model-1");
    expect(
      mergeBuilderDraft(
        draft(),
        { model: "model-1" },
        new Set(),
        new Set(),
        null,
      ).model,
    ).toBe("model-1");
    expect(
      mergeBuilderDraft(
        draft(),
        { model: "invented-model" },
        new Set(),
        new Set(),
        new Set(),
      ).model,
    ).toBe("model-1");
    expect(
      mergeBuilderDraft(
        draft(),
        { model: "" },
        new Set(),
        new Set(),
        new Set(),
      ).model,
    ).toBe("model-1");
  });

  it("preserves scoped member and team grants when duplicating an agent", () => {
    const access = deriveDuplicateAccess({
      permission_mode: "public_to",
      invocation_targets: [
        { target_type: "member", target_id: "member-1" },
        { target_type: "team", target_id: "team-1" },
      ],
    });
    const duplicateDraft = {
      ...draft(),
      ...access,
    };

    expect(access.permissionScope).toBe("members");
    expect(buildInvocationTargets(duplicateDraft)).toEqual([
      { target_type: "member", target_id: "member-1" },
      { target_type: "team", target_id: "team-1" },
    ]);
  });

  it("keeps workspace-wide duplicate access workspace-wide", () => {
    expect(
      deriveDuplicateAccess({
        permission_mode: "public_to",
        invocation_targets: [{ target_type: "workspace", target_id: null }],
      }).permissionScope,
    ).toBe("workspace");
  });
});
