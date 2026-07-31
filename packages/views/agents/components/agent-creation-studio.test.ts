import { createElement } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { ApiError } from "@multica/core/api";
import type { Agent, RuntimeDevice } from "@multica/core/types";
import {
  AgentNameField,
  ModeChooser,
  StudioFooter,
  applyDraftModelChange,
  applyDraftRuntimeChange,
  buildCreateAgentRequest,
  buildDuplicateDraft,
  buildInvocationTargets,
  classifyAgentCreateError,
  decodeBuilderInput,
  deriveDuplicateAccess,
  encodeBuilderInput,
  getAgentCreationScreenKey,
  isDraftDescriptionWithinLimit,
  mergeBuilderDraft,
  parseBuilderDraft,
  pickBuilderRestore,
  stripBuilderDraft,
  type AgentDraft,
} from "./agent-creation-studio";

vi.mock("../../i18n", () => ({
  useT: () => ({
    t: (
      selector: (translations: {
        creation_studio: {
          eyebrow: string;
          choose_title: string;
          choose_description: string;
          recommended: string;
          continue: string;
          modes: {
            blank: { title: string; description: string };
            ai: { title: string; description: string };
          };
          create_and_open: string;
          create_and_add: string;
          creating: string;
          name_conflict: string;
        };
        create_dialog: {
          name_label: string;
          name_placeholder: string;
        };
      }) => string,
    ) =>
      selector({
        creation_studio: {
          eyebrow: "Agent creation",
          choose_title: "How would you like to start?",
          choose_description: "Choose a creation mode.",
          recommended: "Recommended",
          continue: "Continue",
          modes: {
            blank: {
              title: "Start blank",
              description: "Configure every field yourself.",
            },
            ai: {
              title: "Build with AI",
              description: "Describe the outcome you want.",
            },
          },
          create_and_open: "Create and open",
          create_and_add: "Create and add",
          creating: "Creating…",
          name_conflict: "An agent with this name already exists.",
        },
        create_dialog: {
          name_label: "Name",
          name_placeholder: "Agent name",
        },
      }),
  }),
}));

describe("Agent creation studio screen keys", () => {
  it("groups configuration modes and separates the AI setup and builder", () => {
    expect(getAgentCreationScreenKey("blank", "")).toBe("configure");
    expect(getAgentCreationScreenKey("template", "")).toBe("configure");
    expect(getAgentCreationScreenKey("ai", "")).toBe("ai-setup");
    expect(getAgentCreationScreenKey("ai", "session-1")).toBe("ai-builder");
  });
});

describe("Agent creation errors", () => {
  it("classifies a conflict as a name error", () => {
    expect(
      classifyAgentCreateError(
        new ApiError("An agent with this name already exists", 409, "Conflict"),
        "Could not create the agent.",
        "This localized name is already in use.",
      ),
    ).toEqual({
      nameError: "This localized name is already in use.",
      formError: null,
    });
  });

  it("classifies a network error as a form error", () => {
    expect(
      classifyAgentCreateError(
        new Error("Network request failed"),
        "Could not create the agent.",
        "An agent with this name already exists.",
      ),
    ).toEqual({
      nameError: null,
      formError: "Network request failed",
    });
  });

  it("renders a name error directly below the name input", () => {
    const onChange = vi.fn();
    render(
      createElement(AgentNameField, {
        name: "Existing agent",
        error: "An agent with this name already exists",
        onChange,
      }),
    );

    const input = screen.getByRole("textbox", { name: "Name" });
    const error = screen.getByText("An agent with this name already exists");
    expect(input).toHaveAttribute("aria-invalid", "true");
    expect(input).toHaveAttribute("aria-describedby", error.id);
    expect(error).not.toHaveAttribute("role");
    expect(
      input.compareDocumentPosition(error) & Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();

    fireEvent.change(input, { target: { value: "New agent" } });
    expect(onChange).toHaveBeenCalledWith("New agent");
  });

  it("focuses and selects the name input when a name error appears", () => {
    const onChange = vi.fn();
    const view = render(
      createElement(AgentNameField, {
        name: "Existing agent",
        error: null,
        onChange,
      }),
    );

    const input = screen.getByRole("textbox", { name: "Name" });
    expect(input).not.toHaveFocus();

    view.rerender(
      createElement(AgentNameField, {
        name: "Existing agent",
        error: "An agent with this name already exists",
        onChange,
      }),
    );

    expect(input).toHaveFocus();
    expect(input).toHaveValue("Existing agent");
    expect(input).toHaveProperty("selectionStart", 0);
    expect(input).toHaveProperty("selectionEnd", "Existing agent".length);
  });

  it("renders a generic error on the left side of the sticky footer", () => {
    render(
      createElement(StudioFooter, {
        canCreate: true,
        creating: false,
        squad: false,
        error: "Network request failed",
        onCreate: vi.fn(),
      }),
    );

    const error = screen.getByRole("alert");
    const button = screen.getByRole("button", { name: "Create and open" });
    expect(error.parentElement).toBe(button.parentElement);
    expect(
      error.compareDocumentPosition(button) & Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
  });
});

const draft = (): AgentDraft => ({
  name: "Old name",
  description: "Old description",
  instructions: "Old instructions",
  avatarUrl: null,
  runtimeId: "runtime-1",
  model: "model-1",
  thinkingLevel: "",
  serviceTier: "",
  skillIds: new Set(["skill-1"]),
  permissionScope: "private",
  memberIds: new Set(),
  teamIds: new Set(),
});

describe("Agent creation studio mode chooser", () => {
  it("always offers AI-assisted creation", () => {
    render(
      createElement(ModeChooser, {
        onBlank: vi.fn(),
        onAI: vi.fn(),
      }),
    );

    expect(screen.getByText("Start blank")).toBeInTheDocument();
    expect(screen.getByText("Build with AI")).toBeInTheDocument();
  });
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

  // The builder chat is a real chat_session, so cancelling a started-but-empty
  // run defers the empty/non-empty judgment (#5219): the cancel response carries
  // no restore_to_input and the prompt arrives later as a durable draft-restore
  // row holding the ENCODED message. Handing that to the composer raw would show
  // the user a wall of JSON instead of the sentence they typed.
  it("decodes a durable draft restore before the builder composer adopts it", () => {
    const encoded = encodeBuilderInput(
      "Create a release manager",
      draft(),
      [],
      [],
      { id: "runtime-1", name: "Codex", provider: "codex" },
      [],
    );

    expect(pickBuilderRestore(null, { id: "msg-1", content: encoded })).toEqual({
      id: "msg-1",
      content: "Create a release manager",
    });
    expect(pickBuilderRestore(null, null)).toBeNull();
  });

  // The synchronous answer (task never started) is already decoded and already
  // in hand; it must not be displaced by a durable row for the same cancel.
  it("prefers the synchronous cancel answer over a durable restore", () => {
    expect(
      pickBuilderRestore(
        { id: "msg-1", content: "Create a release manager" },
        { id: "msg-1", content: "should not win" },
      ),
    ).toEqual({ id: "msg-1", content: "Create a release manager" });
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

const CODEX_RUNTIME: RuntimeDevice = {
  id: "runtime-1",
  workspace_id: "ws-1",
  name: "Codex laptop",
  provider: "codex",
  status: "online",
  owner_id: "user-1",
  visibility: "private",
} as RuntimeDevice;

const OTHER_RUNTIME: RuntimeDevice = {
  ...CODEX_RUNTIME,
  id: "runtime-2",
  name: "Spare laptop",
} as RuntimeDevice;

const sourceAgent = (overrides: Partial<Agent> = {}): Agent =>
  ({
    id: "agent-1",
    workspace_id: "ws-1",
    runtime_id: "runtime-1",
    name: "Fast Codex",
    description: "Ships quickly",
    instructions: "Be quick",
    avatar_url: null,
    runtime_mode: "managed",
    runtime_config: {},
    custom_args: ["--verbose"],
    visibility: "private",
    permission_mode: "private",
    invocation_targets: [],
    status: "idle",
    max_concurrent_tasks: 9,
    model: "gpt-5.6-sol",
    thinking_level: "high",
    service_tier: "priority",
    owner_id: "user-1",
    skills: [{ id: "skill-1", name: "Review", description: "" }],
    created_at: "2026-07-28T00:00:00Z",
    updated_at: "2026-07-28T00:00:00Z",
    archived_at: null,
    archived_by: null,
    ...overrides,
  }) as Agent;

// MUL-5390: thinking_level / service_tier are runtime + model scoped. The create
// flow never exposed them, so a Fast Codex agent could only be configured after
// the fact and a Duplicate silently dropped the setting.
describe("Agent creation studio execution overrides", () => {
  it("sends the selected thinking level and service tier", () => {
    const request = buildCreateAgentRequest({
      draft: {
        ...draft(),
        model: "gpt-5.6-sol",
        thinkingLevel: "high",
        serviceTier: "priority",
      },
      runtimeId: "runtime-1",
    });

    expect(request).toMatchObject({
      runtime_id: "runtime-1",
      model: "gpt-5.6-sol",
      thinking_level: "high",
      service_tier: "priority",
    });
  });

  it("omits empty overrides instead of sending an empty string", () => {
    const request = buildCreateAgentRequest({
      draft: { ...draft(), model: "" },
      runtimeId: "runtime-1",
    });

    expect(request.model).toBeUndefined();
    expect(request.thinking_level).toBeUndefined();
    expect(request.service_tier).toBeUndefined();
    expect("thinking_level" in request).toBe(true);
    expect(JSON.parse(JSON.stringify(request))).not.toHaveProperty(
      "thinking_level",
    );
  });

  it("carries the runtime-independent duplicate config", () => {
    const request = buildCreateAgentRequest({
      draft: { ...draft(), thinkingLevel: "high", serviceTier: "priority" },
      runtimeId: "runtime-1",
      duplicateSource: sourceAgent(),
    });

    expect(request.custom_args).toEqual(["--verbose"]);
    expect(request.max_concurrent_tasks).toBe(9);
    expect(request.thinking_level).toBe("high");
  });

  it.each([0, -1, 51])(
    "omits an invalid historical duplicate concurrency of %i",
    (maxConcurrentTasks) => {
      const request = buildCreateAgentRequest({
        draft: draft(),
        runtimeId: "runtime-1",
        duplicateSource: sourceAgent({
          max_concurrent_tasks: maxConcurrentTasks,
        }),
      });

      expect(request.max_concurrent_tasks).toBeUndefined();
      expect(request).not.toHaveProperty("max_concurrent_tasks");
    },
  );

  it("clears model, thinking level and service tier on a runtime change", () => {
    const current = {
      ...draft(),
      thinkingLevel: "high",
      serviceTier: "priority",
    };

    expect(applyDraftRuntimeChange(current, "runtime-2")).toMatchObject({
      runtimeId: "runtime-2",
      model: "",
      thinkingLevel: "",
      serviceTier: "",
    });
  });

  it("clears only the per-model overrides on a model change", () => {
    const current = {
      ...draft(),
      thinkingLevel: "high",
      serviceTier: "priority",
    };
    const next = applyDraftModelChange(current, "gpt-5.4-mini");

    expect(next).toMatchObject({
      runtimeId: "runtime-1",
      model: "gpt-5.4-mini",
      thinkingLevel: "",
      serviceTier: "",
    });
    // Re-selecting the same model must not wipe a choice the user just made.
    expect(applyDraftModelChange(current, "model-1")).toBe(current);
  });

  it("copies the execution config when the duplicate stays on its runtime", () => {
    const duplicate = buildDuplicateDraft(sourceAgent(), {
      runtimes: [CODEX_RUNTIME],
      currentUserId: "user-1",
      fallbackRuntimeId: "runtime-1",
      nameSuffix: " copy",
    });

    expect(duplicate).toMatchObject({
      name: "Fast Codex copy",
      runtimeId: "runtime-1",
      model: "gpt-5.6-sol",
      thinkingLevel: "high",
      serviceTier: "priority",
    });
    expect([...duplicate.skillIds]).toEqual(["skill-1"]);
  });

  it("drops the execution config when the duplicate falls back to another runtime", () => {
    // Source runtime is gone from the list (deleted, or private to someone
    // else), so the draft lands on the fallback. Keeping the source model here
    // is what persisted a cross-provider model before MUL-5390.
    const duplicate = buildDuplicateDraft(sourceAgent(), {
      runtimes: [OTHER_RUNTIME],
      currentUserId: "user-1",
      fallbackRuntimeId: "runtime-2",
      nameSuffix: " copy",
    });

    expect(duplicate).toMatchObject({
      runtimeId: "runtime-2",
      model: "",
      thinkingLevel: "",
      serviceTier: "",
    });
  });

  it("drops the execution config when the source runtime is private to someone else", () => {
    const duplicate = buildDuplicateDraft(sourceAgent(), {
      runtimes: [
        { ...CODEX_RUNTIME, owner_id: "user-2" } as RuntimeDevice,
        { ...OTHER_RUNTIME, owner_id: "user-1" } as RuntimeDevice,
      ],
      currentUserId: "user-1",
      fallbackRuntimeId: "runtime-2",
      nameSuffix: " copy",
    });

    expect(duplicate).toMatchObject({
      runtimeId: "runtime-2",
      model: "",
      thinkingLevel: "",
      serviceTier: "",
    });
  });

  it("keeps the overrides while the AI builder leaves the model alone", () => {
    const current = {
      ...draft(),
      thinkingLevel: "high",
      serviceTier: "priority",
    };

    expect(
      mergeBuilderDraft(
        current,
        { name: "Renamed" },
        new Set(),
        new Set(),
        new Set(["model-1"]),
      ),
    ).toMatchObject({
      name: "Renamed",
      model: "model-1",
      thinkingLevel: "high",
      serviceTier: "priority",
    });
  });

  it("drops the overrides when the AI builder moves the model", () => {
    const current = {
      ...draft(),
      thinkingLevel: "high",
      serviceTier: "priority",
    };

    expect(
      mergeBuilderDraft(
        current,
        { model: "gpt-5.4-mini" },
        new Set(),
        new Set(),
        new Set(["model-1", "gpt-5.4-mini"]),
      ),
    ).toMatchObject({
      model: "gpt-5.4-mini",
      thinkingLevel: "",
      serviceTier: "",
    });
  });

  it("enforces the 255-rune description limit the create API applies", () => {
    expect(isDraftDescriptionWithinLimit("a".repeat(255))).toBe(true);
    expect(isDraftDescriptionWithinLimit("a".repeat(256))).toBe(false);
    // Runes, not UTF-16 units: 255 CJK characters are exactly at the limit.
    expect(isDraftDescriptionWithinLimit("汉".repeat(255))).toBe(true);
    expect(isDraftDescriptionWithinLimit("汉".repeat(256))).toBe(false);
  });
});
