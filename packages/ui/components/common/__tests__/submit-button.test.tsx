import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { SubmitButton } from "../submit-button";
import { ReactionBar } from "../reaction-bar";

afterEach(cleanup);

describe("SubmitButton", () => {
  it("keeps the same button element across send, loading, and stop modes", () => {
    const onClick = vi.fn();
    const onStop = vi.fn();
    const { rerender } = render(
      <SubmitButton onClick={onClick} onStop={onStop} ariaLabel="Send" stopAriaLabel="Stop" />,
    );
    const initialButton = screen.getByRole("button", { name: "Send" });
    fireEvent.click(initialButton);
    expect(onClick).toHaveBeenCalledTimes(1);

    rerender(
      <SubmitButton onClick={onClick} onStop={onStop} loading ariaLabel="Send" stopAriaLabel="Stop" />,
    );
    const loadingButton = screen.getByRole("button", { name: "Send" });
    expect(loadingButton).toBe(initialButton);
    expect(loadingButton).toHaveAttribute("aria-busy", "true");

    rerender(
      <SubmitButton onClick={onClick} onStop={onStop} running ariaLabel="Send" stopAriaLabel="Stop" />,
    );
    const stopButton = screen.getByRole("button", { name: "Stop" });
    expect(stopButton).toBe(initialButton);
    fireEvent.click(stopButton);
    expect(onStop).toHaveBeenCalledTimes(1);
  });
});

describe("ReactionBar", () => {
  it("only applies pop-in animation to newly added reactions", () => {
    const initial = [
      { id: "r1", actor_type: "member", actor_id: "u1", emoji: "👍" },
    ];
    const { rerender } = render(
      <ReactionBar
        reactions={initial}
        currentUserId="u1"
        onToggle={vi.fn()}
        getActorName={() => "Alice"}
        showPicker={false}
      />,
    );
    const initialBtn = screen.getByRole("button");
    expect(initialBtn.className).not.toContain("animate-in");

    rerender(
      <ReactionBar
        reactions={[
          ...initial,
          { id: "r2", actor_type: "member", actor_id: "u1", emoji: "🔥" },
        ]}
        currentUserId="u1"
        onToggle={vi.fn()}
        getActorName={() => "Alice"}
        showPicker={false}
      />,
    );
    const buttons = screen.getAllByRole("button");
    expect(buttons[0]?.className).not.toContain("animate-in");
    expect(buttons[1]?.className).toContain("animate-in");
    expect(buttons[1]?.className).toContain("motion-reduce:animate-none");
  });
});
