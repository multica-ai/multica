// @vitest-environment jsdom
import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { MembersSection } from "./members-section";
import type { WorkspaceMember } from "@/lib/types";

function makeMember(overrides: Partial<WorkspaceMember> = {}): WorkspaceMember {
  return {
    id: "user-1",
    name: "Jane Doe",
    email: "jane@example.com",
    role: "member",
    ...overrides,
  };
}

describe("MembersSection", () => {
  it("renders a chip for each member", () => {
    render(
      <MembersSection
        members={[makeMember({ id: "user-1", name: "Jane Doe" }), makeMember({ id: "user-2", name: "Bob Roe" })]}
      />,
    );
    expect(screen.getByText("Jane Doe")).toBeInTheDocument();
    expect(screen.getByText("Bob Roe")).toBeInTheDocument();
  });

  it("derives the avatar fallback from first + last word initials", () => {
    render(<MembersSection members={[makeMember({ name: "Jane Doe" })]} />);
    expect(screen.getByText("JD")).toBeInTheDocument();
  });

  it("falls back to the first two letters for a single-word name", () => {
    render(<MembersSection members={[makeMember({ name: "Cher" })]} />);
    expect(screen.getByText("CH")).toBeInTheDocument();
  });

  it("falls back to '?' for an empty/whitespace name", () => {
    render(<MembersSection members={[makeMember({ name: "   " })]} />);
    expect(screen.getByText("?")).toBeInTheDocument();
  });

  it("renders an empty state when there are no members", () => {
    render(<MembersSection members={[]} />);
    expect(screen.getByText("No members.")).toBeInTheDocument();
  });
});
