// @vitest-environment jsdom

import { useState } from "react";
import { cleanup, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { AgentConversationStarter } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { ConversationStartersEditor } from "./conversation-starters-editor";

function Controlled({
  initial,
  onChange = vi.fn(),
}: {
  initial: AgentConversationStarter[];
  onChange?: (value: AgentConversationStarter[]) => void;
}) {
  const [value, setValue] = useState(initial);
  return (
    <ConversationStartersEditor
      value={value}
      onChange={(next) => {
        setValue(next);
        onChange(next);
      }}
    />
  );
}

const review = { label: "Review the release PR", prompt: "Review it and list risks." };

describe("ConversationStartersEditor", () => {
  afterEach(cleanup);

  it("says the built-in suggestions are in use when nothing is configured", () => {
    renderWithI18n(<Controlled initial={[]} />);

    expect(screen.getByText("Using 3 built-in suggestions")).toBeInTheDocument();
    expect(screen.queryByLabelText("Suggestion 1 label")).toBeNull();
  });

  it("opens a blank first suggestion from Customize", async () => {
    const onChange = vi.fn();
    renderWithI18n(<Controlled initial={[]} onChange={onChange} />);

    await userEvent.setup().click(screen.getByRole("button", { name: "Customize" }));

    expect(onChange).toHaveBeenLastCalledWith([{ label: "", prompt: "" }]);
    expect(screen.getByLabelText("Suggestion 1 label")).toHaveFocus();
    expect(
      screen.getByText("Complete or remove each suggestion before saving."),
    ).toBeInTheDocument();
  });

  it("keeps an incomplete suggestion open until it is filled in", async () => {
    const user = userEvent.setup();
    renderWithI18n(<Controlled initial={[review]} />);

    await user.click(screen.getByRole("button", { name: "Add suggestion" }));
    const done = screen.getByRole("button", { name: "Done" });
    expect(done).toBeDisabled();
    expect(screen.getByRole("button", { name: "Add suggestion" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Edit suggestion 1" })).toBeDisabled();

    await user.type(screen.getByLabelText("Suggestion 2 label"), "Plan");
    expect(done).toBeDisabled();

    await user.type(screen.getByLabelText("Suggestion 2 prompt"), "Plan the week.");
    expect(done).toBeEnabled();
    await user.click(done);

    expect(screen.getByText("Plan")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Add suggestion" })).toBeEnabled();
  });

  it("summarises a configured suggestion and edits it in place", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    renderWithI18n(<Controlled initial={[review]} onChange={onChange} />);

    expect(screen.getByText("Review the release PR")).toBeInTheDocument();
    expect(screen.getByText("Review it and list risks.")).toBeInTheDocument();
    expect(screen.queryByLabelText("Suggestion 1 label")).toBeNull();

    await user.click(screen.getByRole("button", { name: "Edit suggestion 1" }));
    await user.type(screen.getByLabelText("Suggestion 1 label"), " today");
    await user.click(screen.getByRole("button", { name: "Done" }));

    expect(onChange).toHaveBeenLastCalledWith([
      { ...review, label: "Review the release PR today" },
    ]);
    expect(screen.getByText("Review the release PR today")).toBeInTheDocument();
  });

  it("returns to the built-in suggestions when the last one is removed", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    renderWithI18n(<Controlled initial={[review]} onChange={onChange} />);

    await user.click(screen.getByRole("button", { name: "Edit suggestion 1" }));
    await user.click(screen.getByRole("button", { name: "Remove suggestion 1" }));

    expect(onChange).toHaveBeenLastCalledWith([]);
    expect(screen.getByText("Using 3 built-in suggestions")).toBeInTheDocument();
  });

  it("stops offering to add once the limit is reached", () => {
    renderWithI18n(<Controlled initial={[review, review, review]} />);

    expect(screen.queryByRole("button", { name: "Add suggestion" })).toBeNull();
    expect(screen.getByText("3 / 3")).toBeInTheDocument();
  });
});
