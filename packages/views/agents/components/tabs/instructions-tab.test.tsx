// @vitest-environment jsdom

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { configStore } from "@multica/core/config";
import { I18nProvider } from "@multica/core/i18n/react";
import type { Agent } from "@multica/core/types";
import enCommon from "../../../locales/en/common.json";
import enAgents from "../../../locales/en/agents.json";
import { InstructionsTab } from "./instructions-tab";

const TEST_RESOURCES = { en: { common: enCommon, agents: enAgents } };
const persistedPrompt = {
  label: "Review a PR",
  prompt: "Review the open pull request.",
};
const baseAgent: Agent = {
  id: "agent-1",
  workspace_id: "ws-1",
  runtime_id: "runtime-1",
  name: "Reviewer",
  description: "",
  instructions: "Review carefully.",
  starter_prompts: [persistedPrompt],
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
  created_at: "2026-08-24T00:00:00Z",
  updated_at: "2026-08-24T00:00:00Z",
  archived_at: null,
  archived_by: null,
};

function tab(agent: Agent, onSave = vi.fn().mockResolvedValue(undefined)) {
  return (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <InstructionsTab agent={agent} onSave={onSave} />
    </I18nProvider>
  );
}

describe("InstructionsTab persisted-state synchronization", () => {
  beforeEach(() => {
    configStore.getState().setAgentStarterPromptsSupported(true);
  });

  afterEach(() => {
    act(() => {
      configStore.getState().setAgentStarterPromptsSupported(false);
    });
  });

  it("preserves an unsaved prompt across an equivalent agent-list refetch", async () => {
    const user = userEvent.setup();
    const { rerender } = render(tab(baseAgent));
    const label = screen.getByLabelText("Suggestion 1 label");
    await user.clear(label);
    await user.type(label, "Inspect the patch");

    rerender(
      tab({
        ...baseAgent,
        starter_prompts: [{ ...persistedPrompt }],
      }),
    );

    expect(screen.getByLabelText("Suggestion 1 label")).toHaveValue(
      "Inspect the patch",
    );
  });

  it("preserves dirty local state when persisted contents change", async () => {
    const user = userEvent.setup();
    const { rerender } = render(tab(baseAgent));
    const label = screen.getByLabelText("Suggestion 1 label");
    await user.clear(label);
    await user.type(label, "Inspect the patch");

    rerender(
      tab({
        ...baseAgent,
        starter_prompts: [
          { label: "Server-side change", prompt: "A different prompt." },
        ],
      }),
    );

    expect(screen.getByLabelText("Suggestion 1 label")).toHaveValue(
      "Inspect the patch",
    );
  });

  it("preserves submitted prompt edits when an optimistic update rolls back", async () => {
    let rejectSave!: (reason?: unknown) => void;
    const onSave = vi.fn(
      () =>
        new Promise<void>((_, reject) => {
          rejectSave = reject;
        }),
    );
    const user = userEvent.setup();
    const { rerender } = render(tab(baseAgent, onSave));
    const label = screen.getByLabelText("Suggestion 1 label");
    const prompt = screen.getByLabelText("Suggestion 1 prompt");
    await user.clear(label);
    await user.type(label, "Inspect the patch");
    await user.clear(prompt);
    await user.type(prompt, "Inspect the patch for correctness.");
    await user.click(screen.getByRole("button", { name: "Save" }));

    const optimisticPrompt = {
      label: "Inspect the patch",
      prompt: "Inspect the patch for correctness.",
    };
    rerender(
      tab(
        {
          ...baseAgent,
          starter_prompts: [optimisticPrompt],
        },
        onSave,
      ),
    );
    rerender(
      tab(
        {
          ...baseAgent,
          starter_prompts: [{ ...persistedPrompt }],
        },
        onSave,
      ),
    );
    await act(async () => rejectSave(new Error("Update failed")));

    expect(screen.getByLabelText("Suggestion 1 label")).toHaveValue(
      "Inspect the patch",
    );
    expect(screen.getByLabelText("Suggestion 1 prompt")).toHaveValue(
      "Inspect the patch for correctness.",
    );
  });

  it("omits starter prompts from settings writes to an older backend", async () => {
    configStore.getState().setAgentStarterPromptsSupported(false);
    const onSave = vi.fn().mockResolvedValue(undefined);
    const user = userEvent.setup();
    render(tab(baseAgent, onSave));

    expect(
      screen.queryByText("Suggested first actions"),
    ).not.toBeInTheDocument();
    const instructions = screen.getByLabelText("System prompt");
    await user.clear(instructions);
    await user.type(instructions, "Updated instructions.");
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(onSave).toHaveBeenCalledWith({
      instructions: "Updated instructions.",
    });
  });
});
