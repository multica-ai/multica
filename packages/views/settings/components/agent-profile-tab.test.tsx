import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

vi.mock("@multica/core/auth", () => ({
  useAuthStore: Object.assign(
    (selector?: (s: { user: { name: string } | null }) => unknown) => {
      const state = { user: { name: "Jens" } };
      return selector ? selector(state) : state;
    },
    {
      getState: () => ({ user: { name: "Jens" } }),
    },
  ),
}));

import { AgentProfileTab } from "./agent-profile-tab";

describe("AgentProfileTab", () => {
  it("renders default profile (grundig, dansk) with the user's name in the preview", () => {
    render(<AgentProfileTab />);
    expect(screen.getByRole("radio", { name: /Den grundige/ })).toHaveAttribute("aria-checked", "true");
    expect(screen.getByText(/USER: Jens \(Den grundige, dansk\)/)).toBeInTheDocument();
  });

  it("switching persona updates the preview and slider captions", async () => {
    const user = userEvent.setup();
    render(<AgentProfileTab />);

    await user.click(screen.getByRole("radio", { name: /Den utålmodige/ }));

    expect(screen.getByText(/USER: Jens \(Den utålmodige, dansk\)/)).toBeInTheDocument();
    // utalmodig defaults: lengthPref=15 → "Kort", autonomyPref=85 → "Autonom"
    expect(screen.getByText(/Kort · 15/)).toBeInTheDocument();
    expect(screen.getByText(/Autonom · 85/)).toBeInTheDocument();
  });

  it("switching language re-localises the persona blurb and preview", async () => {
    const user = userEvent.setup();
    render(<AgentProfileTab />);

    await user.click(screen.getByRole("radio", { name: "English" }));

    expect(screen.getByRole("radio", { name: /The thorough/ })).toHaveAttribute("aria-checked", "true");
    expect(screen.getByText(/USER: Jens \(The thorough, english\)/)).toBeInTheDocument();
  });

  it("adding an anti-pattern appears in the preview AVOID section", async () => {
    const user = userEvent.setup();
    render(<AgentProfileTab />);

    const input = screen.getByPlaceholderText(/Let me know if you need anything else/);
    await user.type(input, "Aldrig opfinde tids-estimater{Enter}");

    expect(screen.getByText(/AVOID:/)).toBeInTheDocument();
    const previewBlock = screen.getByText(/AVOID:/).closest("pre");
    expect(previewBlock?.textContent).toContain("Aldrig opfinde tids-estimater");
  });

  it("blocks adding more than 20 anti-patterns and disables the input at the cap", () => {
    render(<AgentProfileTab />);

    const input = screen.getByPlaceholderText(/Let me know if you need anything else/) as HTMLInputElement;
    const addBtn = screen.getByRole("button", { name: /Tilføj/ });

    // Default grundig profile starts with 2 anti-patterns; add 18 more to reach 20.
    for (let i = 0; i < 18; i++) {
      fireEvent.change(input, { target: { value: `pattern-${i}` } });
      fireEvent.click(addBtn);
    }

    expect(screen.getByText("20 / 20")).toBeInTheDocument();
    expect(input).toBeDisabled();
  });

  it("removing an anti-pattern updates count and preview", async () => {
    const user = userEvent.setup();
    render(<AgentProfileTab />);

    const counter = screen.getByText("2 / 20");
    expect(counter).toBeInTheDocument();

    const removeBtn = screen.getByRole("button", { name: /Fjern: Hop over edge-cases/ });
    await user.click(removeBtn);

    expect(screen.getByText("1 / 20")).toBeInTheDocument();
    const preview = screen.getByText(/AVOID:/).closest("pre");
    expect(preview?.textContent).not.toContain("Hop over edge-cases");
  });

  it("token estimate badge renders and stays under cap for default profile", () => {
    render(<AgentProfileTab />);
    const badge = screen.getByText(/^~\d+ tokens$/);
    const tokens = Number(badge.textContent!.match(/\d+/)![0]);
    expect(tokens).toBeGreaterThan(0);
    expect(tokens).toBeLessThan(200);
  });

  it("Save button is disabled in PR 2 (persistence ships in PR 3)", () => {
    render(<AgentProfileTab />);
    const saveBtn = screen.getByRole("button", { name: /Gem profil/ });
    expect(saveBtn).toBeDisabled();
    expect(saveBtn).toHaveAttribute("title", expect.stringContaining("næste PR"));
  });

  it("rejects duplicate anti-patterns silently", async () => {
    const user = userEvent.setup();
    render(<AgentProfileTab />);

    const input = screen.getByPlaceholderText(/Let me know if you need anything else/);
    await user.type(input, "Hop over edge-cases{Enter}"); // already in default
    expect(screen.getByText("2 / 20")).toBeInTheDocument();
  });
});
