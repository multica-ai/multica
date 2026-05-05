import { describe, expect, it, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { PrivacyToggle } from "./privacy-toggle";

describe("PrivacyToggle", () => {
  it("renders the off state with 'Workspace' label", () => {
    render(<PrivacyToggle isPrivate={false} onUpdate={() => {}} />);
    expect(screen.getByTestId("issue-privacy-toggle")).toHaveAttribute(
      "aria-pressed",
      "false",
    );
    expect(screen.getByText(/Workspace/)).toBeInTheDocument();
  });

  it("renders the on state with 'Private' label", () => {
    render(<PrivacyToggle isPrivate={true} onUpdate={() => {}} />);
    expect(screen.getByTestId("issue-privacy-toggle")).toHaveAttribute(
      "aria-pressed",
      "true",
    );
    expect(screen.getByText(/Private/)).toBeInTheDocument();
  });

  it("calls onUpdate with the inverted value when clicked", () => {
    const onUpdate = vi.fn();
    render(<PrivacyToggle isPrivate={false} onUpdate={onUpdate} />);
    fireEvent.click(screen.getByTestId("issue-privacy-toggle"));
    expect(onUpdate).toHaveBeenCalledWith(true);
  });

  it("toggles back to false when clicked while on", () => {
    const onUpdate = vi.fn();
    render(<PrivacyToggle isPrivate={true} onUpdate={onUpdate} />);
    fireEvent.click(screen.getByTestId("issue-privacy-toggle"));
    expect(onUpdate).toHaveBeenCalledWith(false);
  });
});
