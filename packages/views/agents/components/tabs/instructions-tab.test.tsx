// @vitest-environment jsdom

import { describe, expect, it, vi } from "vitest";
import { act, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Agent } from "@multica/core/types";
import { renderWithI18n } from "../../../test/i18n";
import { InstructionsTab } from "./instructions-tab";

const baseAgent: Agent = {
  id: "agent-1",
  workspace_id: "ws-1",
  runtime_id: "runtime-1",
  name: "Reviewer",
  description: "",
  instructions: "Review carefully.",
  conversation_starters: [{ label: "Review a PR", prompt: "Review it." }],
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
  return <InstructionsTab agent={agent} onSave={onSave} />;
}

async function typeInstructions(text: string) {
  const user = userEvent.setup();
  const field = screen.getByLabelText("System prompt");
  await user.clear(field);
  await user.type(field, text);
  return user;
}

describe("InstructionsTab", () => {
  it("saves only the instructions; starters live in General settings", async () => {
    const onSave = vi.fn().mockResolvedValue(undefined);
    renderWithI18n(tab(baseAgent, onSave));

    expect(screen.queryByText("Conversation starters")).toBeNull();
    const user = await typeInstructions("Updated instructions.");
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(onSave).toHaveBeenCalledWith({
      instructions: "Updated instructions.",
    });
  });

  it("discards unsaved edits back to the saved instructions", async () => {
    renderWithI18n(tab(baseAgent));

    const user = await typeInstructions("Scratch that.");
    expect(screen.getByText("Unsaved changes")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Discard" }));

    expect(screen.getByLabelText("System prompt")).toHaveValue(
      "Review carefully.",
    );
    expect(screen.queryByText("Unsaved changes")).toBeNull();
    expect(screen.getByRole("button", { name: "Save" })).toBeDisabled();
  });

  it("counts characters and previews an empty prompt without an editor", async () => {
    renderWithI18n(tab({ ...baseAgent, instructions: "" }));

    expect(screen.getByText("0 characters")).toBeInTheDocument();
    await userEvent.setup().click(screen.getByRole("tab", { name: "Preview" }));

    expect(screen.queryByLabelText("System prompt")).toBeNull();
    expect(screen.getByText("Nothing to preview yet.")).toBeInTheDocument();
  });

  it("preserves an unsaved edit when the server copy changes", async () => {
    const { rerender } = renderWithI18n(tab(baseAgent));
    await typeInstructions("Local draft.");

    rerender(tab({ ...baseAgent, instructions: "Server-side change." }));

    expect(screen.getByLabelText("System prompt")).toHaveValue("Local draft.");
  });

  it("adopts a server change while the editor is clean", () => {
    const { rerender } = renderWithI18n(tab(baseAgent));

    rerender(tab({ ...baseAgent, instructions: "Server-side change." }));

    expect(screen.getByLabelText("System prompt")).toHaveValue(
      "Server-side change.",
    );
  });

  it("keeps submitted edits when an optimistic update rolls back", async () => {
    let rejectSave!: (reason?: unknown) => void;
    const onSave = vi.fn(
      () =>
        new Promise<void>((_, reject) => {
          rejectSave = reject;
        }),
    );
    const { rerender } = renderWithI18n(tab(baseAgent, onSave));
    const user = await typeInstructions("Inspect the patch.");
    await user.click(screen.getByRole("button", { name: "Save" }));

    rerender(tab({ ...baseAgent, instructions: "Inspect the patch." }, onSave));
    rerender(tab(baseAgent, onSave));
    await act(async () => rejectSave(new Error("Update failed")));

    expect(screen.getByLabelText("System prompt")).toHaveValue(
      "Inspect the patch.",
    );
  });
});
