import { createElement } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { ApiError } from "@multica/core/api";
import {
  AgentNameField,
  ModeChooser,
  StudioFooter,
  buildInvocationTargets,
  classifyAgentCreateError,
  decodeBuilderInput,
  deriveDuplicateAccess,
  encodeBuilderInput,
  getAgentCreationScreenKey,
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
