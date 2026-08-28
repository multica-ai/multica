import { createElement } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { QueryClient } from "@tanstack/react-query";
import { describe, expect, it, vi } from "vitest";
import { ApiError } from "@multica/core/api";
import type { Agent } from "@multica/core/types";
import {
  cacheAgentResponse,
  workspaceKeys,
} from "@multica/core/workspace/queries";
import { AgentNameField } from "./agent-configuration-panel";
import { CreateMethodChooser } from "./choose-create-method-page";
import { CreateAgentFooter } from "./create-agent-footer";
import { draftPreview } from "./unfinished-drafts";
import { classifyAgentCreateError } from "./use-create-agent-submit";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";

const TEST_NAVIGATION: NavigationAdapter = {
  push: vi.fn(),
  replace: vi.fn(),
  back: vi.fn(),
  pathname: "/acme/agents/new",
  searchParams: new URLSearchParams(),
  hash: "",
  getShareableUrl: (path: string) => path,
};

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
      createElement(CreateAgentFooter, {
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

const CREATED_AGENT: Agent = {
  id: "agent-new",
  workspace_id: "ws-1",
  runtime_id: "runtime-1",
  name: "Blank agent",
  description: "",
  instructions: "",
  avatar_url: null,
  runtime_mode: "local",
  runtime_config: {},
  custom_args: [],
  visibility: "workspace",
  permission_mode: "public_to",
  invocation_targets: [{ target_type: "workspace", target_id: null }],
  status: "idle",
  max_concurrent_tasks: 1,
  model: "",
  owner_id: "user-1",
  skills: [],
  created_at: "2026-08-15T00:00:00Z",
  updated_at: "2026-08-15T00:00:00Z",
  archived_at: null,
  archived_by: null,
};

describe("Agent creation cache handoff", () => {
  it("hydrates detail and upserts an already-loaded agent list", () => {
    const queryClient = new QueryClient();
    const existingAgent = { ...CREATED_AGENT, id: "agent-existing" };
    queryClient.setQueryData(workspaceKeys.agents("ws-1"), [existingAgent]);

    cacheAgentResponse(queryClient, "ws-1", CREATED_AGENT);

    expect(
      queryClient.getQueryData(workspaceKeys.agent("ws-1", CREATED_AGENT.id)),
    ).toEqual(CREATED_AGENT);
    expect(queryClient.getQueryData(workspaceKeys.agents("ws-1"))).toEqual([
      existingAgent,
      CREATED_AGENT,
    ]);
  });

  it("does not turn one create response into a partial cold list", () => {
    const queryClient = new QueryClient();

    cacheAgentResponse(queryClient, "ws-1", CREATED_AGENT);

    expect(
      queryClient.getQueryData(workspaceKeys.agents("ws-1")),
    ).toBeUndefined();
    expect(
      queryClient.getQueryData(workspaceKeys.agent("ws-1", CREATED_AGENT.id)),
    ).toEqual(CREATED_AGENT);
  });

  it("can update detail without inserting a list entry that was not present", () => {
    const queryClient = new QueryClient();
    queryClient.setQueryData<Agent[]>(workspaceKeys.agents("ws-1"), []);

    cacheAgentResponse(queryClient, "ws-1", CREATED_AGENT, {
      insertIntoList: false,
    });

    expect(queryClient.getQueryData(workspaceKeys.agents("ws-1"))).toEqual([]);
    expect(
      queryClient.getQueryData(workspaceKeys.agent("ws-1", CREATED_AGENT.id)),
    ).toEqual(CREATED_AGENT);
  });
});

// The list endpoint returns the stored message untouched, still in the builder
// wire format. Rendered raw it is a wall of JSON, which is what the user would
// see on the one screen meant to help them recognise their own draft.
describe("Unfinished draft preview", () => {
  it("unwraps the encoded envelope on a user message", () => {
    expect(
      draftPreview({
        last_message_role: "user",
        last_message_content:
          'MULTICA_AGENT_BUILDER_INPUT\n{"user_request":"Create a release manager","current_draft":{"name":"X"}}',
      }),
    ).toBe("Create a release manager");
  });

  it("hides the structured block on an assistant message", () => {
    expect(
      draftPreview({
        last_message_role: "assistant",
        last_message_content:
          'Here is a first draft.\n<agent_draft>{"name":"Researcher"}</agent_draft>',
      }),
    ).toBe("Here is a first draft.");
  });

  it("returns an empty preview when the server sent no message", () => {
    expect(
      draftPreview({ last_message_role: "", last_message_content: "" }),
    ).toBe("");
  });
});

describe("Agent creation method chooser", () => {
  it("always offers AI-assisted creation", () => {
    render(
      createElement(NavigationProvider, {
        value: TEST_NAVIGATION,
        children: createElement(CreateMethodChooser, {
          blankHref: "/acme/agents/new/manual",
          aiHref: "/acme/agents/new/ai",
        }),
      }),
    );

    expect(screen.getByText("Start blank")).toBeInTheDocument();
    expect(screen.getByText("Build with AI")).toBeInTheDocument();
    expect(
      screen.getByText("Start blank").closest("a"),
    ).toHaveAttribute("href", "/acme/agents/new/manual");
    expect(screen.getByText("Build with AI").closest("a")).toHaveAttribute(
      "href",
      "/acme/agents/new/ai",
    );
  });
});
