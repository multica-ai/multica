// @vitest-environment jsdom

import { act, fireEvent, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { DocCollaborator } from "../../docs/components/use-doc-collab";
import { renderWithI18n } from "../../test/i18n";
import { IssuePresenceHeaderChip } from "./issue-presence-header-chip";

vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: ({ name }: { name: string }) => <span data-testid="avatar">{name}</span>,
}));

const person = (key: number, name: string, editing = false): DocCollaborator => ({
  key,
  id: `user-${key}`,
  name,
  color: "#db2777",
  kind: "member",
  editing,
});

function renderChip(collaborators: DocCollaborator[], sections: Record<number, string> = {}) {
  const onJump = vi.fn();
  let move: (() => void) | null = null;
  const watchCarets = vi.fn((listener: () => void) => {
    move = listener;
    return () => {
      move = null;
    };
  });
  const view = renderWithI18n(
    <IssuePresenceHeaderChip
      collaborators={collaborators}
      locate={(key) => sections[key] ?? null}
      watchCarets={watchCarets}
      onJump={onJump}
    />,
  );
  return { ...view, onJump, watchCarets, move: () => move?.() };
}

describe("IssuePresenceHeaderChip", () => {
  it("renders nothing when the reader is alone", () => {
    const { container } = renderChip([]);
    expect(container).toBeEmptyDOMElement();
  });

  it("shows up to three people, editors first, and how many are here", () => {
    renderChip([person(1, "Ann"), person(2, "Bo", true), person(3, "Cy"), person(4, "Di")]);
    const chip = screen.getByRole("button", { name: "4 viewing" });
    expect([...chip.querySelectorAll("[data-testid=avatar]")].map((a) => a.textContent)).toEqual(["Bo", "Ann", "Cy"]);
  });

  it("lists editors with their section and jumps to the one clicked", async () => {
    const sections: Record<number, string> = { 2: "Goals" };
    const { onJump, watchCarets, move } = renderChip([person(1, "Ann"), person(2, "Bo", true)], sections);
    expect(watchCarets).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "2 viewing" }));

    expect(await screen.findByText("Editing · 1")).toBeInTheDocument();
    expect(screen.getByText("Viewing · 1")).toBeInTheDocument();
    expect(screen.getByText('In "Goals"')).toBeInTheDocument();
    expect(watchCarets).toHaveBeenCalledTimes(1);

    // An open list follows the editor to another section.
    sections[2] = "Risks";
    await act(async () => {
      move();
      await new Promise((resolve) => requestAnimationFrame(() => resolve(null)));
    });
    expect(screen.getByText('In "Risks"')).toBeInTheDocument();

    const row = document.querySelector<HTMLElement>('[data-presence-row="Bo"]')!;
    fireEvent.click(row);
    expect(onJump).toHaveBeenCalledWith(expect.objectContaining({ key: 2, name: "Bo" }));
  });

  it("names the viewers past the listed ones instead of listing them", async () => {
    const names = ["Ann", "Bo", "Cy", "Di", "Ed", "Fay", "Gus", "Hal", "Ivy", "Jo", "Kai", "Lu"];
    renderChip(names.map((name, i) => person(i + 1, name)));
    fireEvent.click(screen.getByRole("button", { name: "12 viewing" }));
    expect(await screen.findByText("6 more: Gus, Hal, Ivy, Jo...")).toBeInTheDocument();
    expect(document.querySelectorAll("[data-presence-row]")).toHaveLength(6);
    expect(screen.queryByText("Click a name to jump to their cursor")).not.toBeInTheDocument();
  });
});
