import { describe, expect, it, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { AutopilotPrivacySection } from "./autopilot-privacy-section";

describe("AutopilotPrivacySection", () => {
  it("calls onChange directly when flipping public → private (no confirmation)", () => {
    const onChange = vi.fn();
    render(
      <AutopilotPrivacySection
        value={false}
        onChange={onChange}
        initialIsPrivate={false}
      />,
    );
    fireEvent.click(screen.getByTestId("autopilot-privacy-toggle"));
    expect(onChange).toHaveBeenCalledWith(true);
    expect(screen.queryByText(/Make this autopilot public/)).toBeNull();
  });

  it("shows confirmation dialog when flipping persisted-private → public", () => {
    const onChange = vi.fn();
    render(
      <AutopilotPrivacySection
        value={true}
        onChange={onChange}
        initialIsPrivate={true}
      />,
    );
    fireEvent.click(screen.getByTestId("autopilot-privacy-toggle"));
    expect(onChange).not.toHaveBeenCalled();
    expect(screen.getByText(/Make this autopilot public/)).toBeInTheDocument();
    expect(
      screen.getByText(/Previously hidden runs, comments/),
    ).toBeInTheDocument();
  });

  it("applies the change after confirming the public switch", () => {
    const onChange = vi.fn();
    render(
      <AutopilotPrivacySection
        value={true}
        onChange={onChange}
        initialIsPrivate={true}
      />,
    );
    fireEvent.click(screen.getByTestId("autopilot-privacy-toggle"));
    fireEvent.click(screen.getByTestId("autopilot-privacy-confirm-public"));
    expect(onChange).toHaveBeenCalledWith(false);
  });

  it("does NOT confirm when the toggle was originally public (no hidden content yet)", () => {
    const onChange = vi.fn();
    render(
      <AutopilotPrivacySection
        value={true}
        onChange={onChange}
        initialIsPrivate={false}
      />,
    );
    fireEvent.click(screen.getByTestId("autopilot-privacy-toggle"));
    expect(onChange).toHaveBeenCalledWith(false);
    expect(screen.queryByText(/Make this autopilot public/)).toBeNull();
  });
});
