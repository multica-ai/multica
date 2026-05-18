// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, fireEvent, cleanup, waitFor } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import type { AgentDefaults, Workspace } from "@multica/core/types";
import enCommon from "../../locales/en/common.json";
import enAgents from "../../locales/en/agents.json";
import { PersonalDefaultsDetail } from "./defaults-detail";

const TEST_RESOURCES = { en: { common: enCommon, agents: enAgents } };

if (typeof Element.prototype.getAnimations !== "function") {
  Element.prototype.getAnimations = () => [];
}

const mockGetPersonalAgentDefaults = vi.hoisted(() => vi.fn());
const mockUpdatePersonalAgentDefaults = vi.hoisted(() => vi.fn());
const mockListInstructionsHistory = vi.hoisted(() => vi.fn());
const mockGetInstructionsHistory = vi.hoisted(() => vi.fn());
const mockEditorValues = vi.hoisted(() => [] as string[]);

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/api", () => ({
  ApiError: class ApiError extends Error {},
  api: {
    getPersonalAgentDefaults: (...args: unknown[]) => mockGetPersonalAgentDefaults(...args),
    updatePersonalAgentDefaults: (...args: unknown[]) => mockUpdatePersonalAgentDefaults(...args),
    listInstructionsHistory: (...args: unknown[]) => mockListInstructionsHistory(...args),
    getInstructionsHistory: (...args: unknown[]) => mockGetInstructionsHistory(...args),
    listSkills: vi.fn().mockResolvedValue([]),
  },
}));

vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: () => null,
}));

vi.mock("../../editor/content-editor", async () => {
  const React = await import("react");

  return {
    ContentEditor: ({ defaultValue, onUpdate, placeholder }: {
      defaultValue?: string;
      onUpdate?: (value: string) => void;
      placeholder?: string;
    }) => {
      const [value, setValue] = React.useState(defaultValue ?? "");
      mockEditorValues.push(defaultValue ?? "");
      return (
        <textarea
          aria-label={placeholder}
          value={value}
          onChange={(event) => {
            setValue(event.currentTarget.value);
            onUpdate?.(event.currentTarget.value);
          }}
        />
      );
    },
  };
});

vi.mock("sonner", () => ({
  toast: {
    error: vi.fn(),
    success: vi.fn(),
  },
}));

function makePersonalDefaults(instructions: string): AgentDefaults {
  return {
    id: "defaults-1",
    config: { instructions },
    created_at: "2026-05-18T00:00:00Z",
    updated_at: "2026-05-18T00:00:00Z",
  };
}

function makeWorkspace(): Workspace {
  return {
    id: "ws-1",
    name: "Workspace",
    slug: "workspace",
    description: null,
    context: null,
    wiki_content: null,
    settings: {},
    repos: [],
    issue_prefix: "OPE",
    created_at: "2026-05-18T00:00:00Z",
    updated_at: "2026-05-18T00:00:00Z",
  };
}

function renderPersonalDefaults() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
    },
  });
  queryClient.setQueryData(["workspaces", "list"], [makeWorkspace()]);

  render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <QueryClientProvider client={queryClient}>
        <PersonalDefaultsDetail />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

describe("PersonalDefaultsDetail", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockEditorValues.length = 0;
    mockGetPersonalAgentDefaults
      .mockResolvedValueOnce(makePersonalDefaults("old instructions"))
      .mockResolvedValue(makePersonalDefaults("restored instructions"));
    mockUpdatePersonalAgentDefaults.mockResolvedValue(makePersonalDefaults("restored instructions"));
    mockListInstructionsHistory.mockResolvedValue({
      items: [
        {
          id: "current-version",
          workspace_id: "ws-1",
          scope: "personal",
          created_at: "2026-05-18T00:01:00Z",
          content_preview: "old instructions",
        },
        {
          id: "restorable-version",
          workspace_id: "ws-1",
          scope: "personal",
          created_at: "2026-05-18T00:00:00Z",
          content_preview: "restored instructions",
        },
      ],
      total: 2,
    });
    mockGetInstructionsHistory.mockResolvedValue({
      id: "restorable-version",
      workspace_id: "ws-1",
      scope: "personal",
      created_at: "2026-05-18T00:00:00Z",
      content_preview: "restored instructions",
      content: "restored instructions",
    });
  });

  afterEach(() => {
    cleanup();
    document.body.innerHTML = "";
  });

  it("remounts the instructions editor with restored server content", async () => {
    renderPersonalDefaults();

    const editor = await screen.findByDisplayValue("old instructions");
    fireEvent.change(editor, { target: { value: "unsaved local draft" } });

    fireEvent.click(screen.getByRole("button", { name: /History/i }));
    const restoreButtons = await screen.findAllByRole("button", { name: /Restore/i });
    expect(restoreButtons[0]).toBeDefined();
    fireEvent.click(restoreButtons[0]!);
    fireEvent.click(screen.getByRole("button", { name: "Restore" }));

    await waitFor(() => {
      expect(mockUpdatePersonalAgentDefaults).toHaveBeenCalledWith("ws-1", {
        instructions: "restored instructions",
      });
    });
    expect(await screen.findByDisplayValue("restored instructions")).toBeInTheDocument();
    expect(screen.queryByDisplayValue("unsaved local draft")).not.toBeInTheDocument();
    expect(mockEditorValues).toContain("restored instructions");
  });
});
