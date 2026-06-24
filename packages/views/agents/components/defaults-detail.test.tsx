// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, fireEvent, cleanup, waitFor, within } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import type { AgentConfigTemplate } from "@multica/core/types";
import enCommon from "../../locales/en/common.json";
import enAgents from "../../locales/en/agents.json";
import { ConfigTemplateDialog } from "./config-template-dialog";

const TEST_RESOURCES = { en: { common: enCommon, agents: enAgents } };

if (typeof Element.prototype.getAnimations !== "function") {
  Element.prototype.getAnimations = () => [];
}

const mockListAgentConfigTemplates = vi.hoisted(() => vi.fn());
const mockUpdateAgentConfigTemplate = vi.hoisted(() => vi.fn());
const mockListInstructionsHistory = vi.hoisted(() => vi.fn());
const mockGetInstructionsHistory = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/api", () => ({
  ApiError: class ApiError extends Error {},
  api: {
    listAgentConfigTemplates: (...args: unknown[]) =>
      mockListAgentConfigTemplates(...args),
    updateAgentConfigTemplate: (...args: unknown[]) =>
      mockUpdateAgentConfigTemplate(...args),
    createAgentConfigTemplate: vi.fn(),
    deleteAgentConfigTemplate: vi.fn(),
    listInstructionsHistory: (...args: unknown[]) =>
      mockListInstructionsHistory(...args),
    getInstructionsHistory: (...args: unknown[]) =>
      mockGetInstructionsHistory(...args),
    listSkills: vi.fn().mockResolvedValue([]),
  },
}));

vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: () => null,
}));

vi.mock("../../editor/content-editor", () => ({
  ContentEditor: ({ defaultValue, onUpdate, placeholder }: {
    defaultValue?: string;
    onUpdate?: (value: string) => void;
    placeholder?: string;
  }) => (
    <textarea
      aria-label={placeholder}
      defaultValue={defaultValue ?? ""}
      onChange={(event) => onUpdate?.(event.currentTarget.value)}
    />
  ),
}));

vi.mock("sonner", () => ({
  toast: { error: vi.fn(), success: vi.fn() },
}));

function makeDefaultTemplate(instructions: string): AgentConfigTemplate {
  return {
    id: "tpl-1",
    workspace_id: "ws-1",
    scope: "personal",
    name: "Personal Default",
    description: "",
    config: { instructions },
    is_default: true,
    created_at: "2026-05-18T00:00:00Z",
    updated_at: "2026-05-18T00:00:00Z",
  };
}

function renderDialog() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <QueryClientProvider client={queryClient}>
        <ConfigTemplateDialog
          open
          scope="personal"
          canEdit
          onOpenChange={() => {}}
        />
      </QueryClientProvider>
    </I18nProvider>,
  );
  return queryClient;
}

describe("ConfigTemplateDialog", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockListAgentConfigTemplates.mockResolvedValue([
      makeDefaultTemplate("old instructions"),
    ]);
    mockUpdateAgentConfigTemplate.mockResolvedValue(
      makeDefaultTemplate("restored instructions"),
    );
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

  it("restoring an instructions version saves it to the default template", async () => {
    renderDialog();

    // The default template is auto-selected; its instructions editor renders.
    const editor = await screen.findByDisplayValue("old instructions");
    fireEvent.change(editor, { target: { value: "unsaved local draft" } });

    fireEvent.click(screen.getByRole("button", { name: /History/i }));

    // The non-current version exposes a Restore button.
    const restoreButtons = await screen.findAllByRole("button", { name: /Restore/i });
    expect(restoreButtons[0]).toBeDefined();
    fireEvent.click(restoreButtons[0]!);

    // Confirm inside the alert dialog.
    const alertDialog = await screen.findByRole("alertdialog");
    fireEvent.click(
      within(alertDialog).getByRole("button", { name: "Restore" }),
    );

    await waitFor(() => {
      expect(mockUpdateAgentConfigTemplate).toHaveBeenCalledWith("tpl-1", {
        config: { instructions: "restored instructions" },
      });
    });
  });
});
