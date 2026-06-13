// @vitest-environment jsdom
//
// Real-DOM render test (NO dropdown-menu mock). The other test file mocks
// @multica/ui/.../dropdown-menu, which means it cannot catch a crash that only
// happens when the *real* Base UI primitives mount — e.g. a GroupLabel rendered
// outside a Group throws and takes the whole inbox to the route error boundary
// (TECH-3494 staging regression). This test opens the menu for real so that
// class of bug fails CI instead of only showing up live.
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { DynamicInboxCreateMenu } from "./dynamic-inbox-create-menu";

afterEach(() => cleanup());

describe("DynamicInboxCreateMenu (real DOM)", () => {
  it("opens without throwing and shows all three actions", async () => {
    const user = userEvent.setup();
    render(
      <DynamicInboxCreateMenu
        onNewMessage={vi.fn()}
        onNewIssue={vi.fn()}
        onNewReminder={vi.fn()}
      />,
    );

    await user.click(screen.getByRole("button", { name: "Create" }));

    await waitFor(() => {
      expect(screen.getByText("New message")).toBeTruthy();
    });
    expect(screen.getByText("Send a direct message")).toBeTruthy();
    expect(screen.getByText("New issue")).toBeTruthy();
    expect(screen.getByText("New reminder")).toBeTruthy();
  });
});
