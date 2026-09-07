// @vitest-environment jsdom

import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "@multica/core/i18n/react";
import type { AgentConversationStarter } from "@multica/core/types";
import enAgents from "../../locales/en/agents.json";
import enChat from "../../locales/en/chat.json";
import { ConversationStartersEditor } from "./conversation-starters-editor";

// The selection matrix (which rows count, when the fallback wins) is covered
// once in packages/core/agents/conversation-starters.test.ts. This suite only
// proves the editor previews what a new chat would actually show.
function renderEditor(value: AgentConversationStarter[]) {
  render(
    <I18nProvider
      locale="en"
      resources={{ en: { agents: enAgents, chat: enChat } }}
    >
      <ConversationStartersEditor value={value} onChange={vi.fn()} />
    </I18nProvider>,
  );
}

describe("ConversationStartersEditor preview", () => {
  afterEach(cleanup);

  it("previews the built-in defaults and says so when nothing is configured", () => {
    renderEditor([]);

    expect(screen.getByText("Preview of a new chat")).toBeInTheDocument();
    expect(screen.getByText("What can you help with?")).toBeInTheDocument();
    expect(
      screen.getByText(
        "These are the built-in defaults. Add a suggestion above to replace all three.",
      ),
    ).toBeInTheDocument();
  });

  it("previews the configured labels and drops the defaults note", () => {
    renderEditor([
      { label: "Review the release PR", prompt: "Review it and list risks." },
    ]);

    // Twice: once as the editable label input's value, once in the preview.
    expect(
      screen.getByDisplayValue("Review the release PR"),
    ).toBeInTheDocument();
    expect(screen.getByText("Review the release PR")).toBeInTheDocument();
    expect(screen.queryByText("What can you help with?")).toBeNull();
    expect(
      screen.queryByText(/These are the built-in defaults/),
    ).toBeNull();
  });

  it("keeps previewing the defaults while a row is still half-filled", () => {
    renderEditor([{ label: "Draft a plan", prompt: "" }]);

    expect(screen.getByText("What can you help with?")).toBeInTheDocument();
    expect(
      screen.getByText(
        "Complete or remove each suggestion before saving.",
      ),
    ).toBeInTheDocument();
  });
});
