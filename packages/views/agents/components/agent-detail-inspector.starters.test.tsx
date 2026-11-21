// @vitest-environment jsdom

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { configStore } from "@multica/core/config";
import type { Agent, AgentRuntime } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { AgentDetailInspector } from "./agent-detail-inspector";

vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQuery: () => ({ data: undefined, isSuccess: false }),
}));
vi.mock("../../common/avatar-upload-control", () => ({
  AvatarUploadControl: () => <div data-testid="avatar-upload" />,
}));
vi.mock("./inspector/model-picker", () => ({
  ModelPicker: () => <div data-testid="model-picker" />,
}));
vi.mock("./inspector/runtime-picker", () => ({
  RuntimePicker: () => <div data-testid="runtime-picker" />,
}));
vi.mock("./inspector/thinking-prop-row", () => ({
  ThinkingSettingField: () => <div data-testid="thinking-field" />,
}));
vi.mock("./inspector/service-tier-setting-field", () => ({
  ServiceTierSettingField: () => <div data-testid="service-tier-field" />,
}));

const agent = {
  id: "agent-1",
  workspace_id: "workspace-1",
  name: "Lambda",
  description: "Coding agent",
  runtime_id: "runtime-1",
  avatar_url: null,
  conversation_starters: [],
  max_concurrent_tasks: 1,
} as unknown as Agent;

function renderInspector({
  runtime = null,
}: { runtime?: AgentRuntime | null } = {}) {
  const onUpdate = vi.fn(
    async (_id: string, _data: Record<string, unknown>) => {},
  );
  renderWithI18n(
    <AgentDetailInspector
      agent={agent}
      runtime={runtime}
      runtimes={[]}
      members={[]}
      currentUserId="user-1"
      canEdit
      onUpdate={onUpdate}
    />,
  );
  return onUpdate;
}

describe("AgentDetailInspector conversation starters", () => {
  beforeEach(() => {
    configStore.getState().setAgentConversationStartersSupported(true);
  });

  afterEach(() => {
    cleanup();
    act(() => {
      configStore.getState().setAgentConversationStartersSupported(false);
    });
  });

  it("lives in the profile beside a new-chat preview", () => {
    renderInspector();

    expect(screen.getByText("Conversation starters")).toBeInTheDocument();
    expect(screen.getByText("Using 3 built-in suggestions")).toBeInTheDocument();
    expect(screen.getByText("New chat preview")).toBeInTheDocument();
    expect(screen.getByText("Chat with Lambda")).toBeInTheDocument();
  });

  it("autosaves once a starter is complete, and not before", async () => {
    const user = userEvent.setup();
    const onUpdate = renderInspector();

    await user.click(screen.getByRole("button", { name: "Customize" }));
    await user.type(screen.getByLabelText("Suggestion 1 label"), "Review my PR");
    await new Promise((resolve) => setTimeout(resolve, 800));
    expect(onUpdate).not.toHaveBeenCalled();

    await user.type(screen.getByLabelText("Suggestion 1 prompt"), "Review it.");

    await waitFor(
      () =>
        expect(onUpdate).toHaveBeenCalledWith("agent-1", {
          conversation_starters: [{ label: "Review my PR", prompt: "Review it." }],
        }),
      { timeout: 2000 },
    );
  });

  it("offers starters and a chat preview for a Qoder agent", () => {
    renderInspector({
      runtime: { id: "runtime-1", provider: "qoder_cloud" } as AgentRuntime,
    });

    expect(screen.getByText("Conversation starters")).toBeInTheDocument();
    expect(screen.getByText("New chat preview")).toBeInTheDocument();
  });

  it("omits the editor on a backend that does not persist starters", () => {
    configStore.getState().setAgentConversationStartersSupported(false);
    renderInspector();

    expect(screen.queryByText("Conversation starters")).toBeNull();
    expect(screen.getByText("What can you help with?")).toBeInTheDocument();
  });
});
