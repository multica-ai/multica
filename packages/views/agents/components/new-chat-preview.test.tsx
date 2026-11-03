// @vitest-environment jsdom

import { cleanup, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import type { AgentConversationStarter } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { NewChatPreview } from "./new-chat-preview";

// The selection matrix (which rows count, when the fallback wins) is covered
// once in packages/core/agents/conversation-starters.test.ts. This suite only
// proves the preview shows what a new chat would actually show.
function renderPreview(starters: AgentConversationStarter[]) {
  renderWithI18n(
    <NewChatPreview
      name="Lambda"
      description="Jiayuan's coding agent."
      avatarUrl={null}
      starters={starters}
    />,
  );
}

describe("NewChatPreview", () => {
  afterEach(cleanup);

  it("shows the profile and the built-in defaults when nothing is configured", () => {
    renderPreview([]);

    expect(screen.getByText("Chat with Lambda")).toBeInTheDocument();
    expect(screen.getByText("Jiayuan's coding agent.")).toBeInTheDocument();
    expect(screen.getByText("What can you help with?")).toBeInTheDocument();
    expect(screen.getByText(/These are the built-in suggestions/)).toBeInTheDocument();
  });

  it("shows configured starters and drops the defaults note", () => {
    renderPreview([
      { label: "Review the release PR", prompt: "Review it and list risks." },
    ]);

    expect(screen.getByText("Review the release PR")).toBeInTheDocument();
    expect(screen.queryByText("What can you help with?")).toBeNull();
    expect(screen.queryByText(/These are the built-in suggestions/)).toBeNull();
  });

  it("keeps showing the defaults while a starter is still half-filled", () => {
    renderPreview([{ label: "Draft a plan", prompt: "" }]);

    expect(screen.getByText("What can you help with?")).toBeInTheDocument();
  });
});
