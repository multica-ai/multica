import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { JoinDiscordCard } from "./join-discord-card";

// react-i18next isn't initialised in the views test env, so resolve the
// selector against the real en/layout.json to assert on actual copy.
vi.mock("../i18n", () => ({
  useT: () => ({
    t: (sel: (r: { sidebar: { discord_card: Record<string, string> } }) => string) =>
      sel({
        sidebar: {
          discord_card: {
            title: "Join our Discord",
            description: "Chat with the team and other builders.",
            dismiss: "Dismiss",
          },
        },
      }),
  }),
}));

const userId = { current: "user-1" as string | undefined };
vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: (s: { user?: { id?: string } }) => unknown) =>
    selector({ user: userId.current ? { id: userId.current } : undefined }),
}));

afterEach(() => {
  localStorage.clear();
  userId.current = "user-1";
});

describe("JoinDiscordCard", () => {
  it("links to the Discord invite", () => {
    render(<JoinDiscordCard />);
    const link = screen.getByRole("link", { name: /join our discord/i });
    expect(link).toHaveAttribute("href", "https://discord.gg/W8gYBn226t");
    expect(link).toHaveAttribute("target", "_blank");
  });

  it("hides and stays hidden after dismiss, persisting per user", async () => {
    const user = userEvent.setup();
    const { unmount } = render(<JoinDiscordCard />);

    await user.click(screen.getByRole("button", { name: "Dismiss" }));
    expect(screen.queryByText("Join our Discord")).not.toBeInTheDocument();

    // A fresh mount for the same user keeps the card hidden.
    unmount();
    render(<JoinDiscordCard />);
    expect(screen.queryByText("Join our Discord")).not.toBeInTheDocument();
  });

  it("keeps the card visible for a different user", async () => {
    const user = userEvent.setup();
    const { unmount } = render(<JoinDiscordCard />);
    await user.click(screen.getByRole("button", { name: "Dismiss" }));
    unmount();

    userId.current = "user-2";
    render(<JoinDiscordCard />);
    expect(screen.getByText("Join our Discord")).toBeInTheDocument();
  });
});
