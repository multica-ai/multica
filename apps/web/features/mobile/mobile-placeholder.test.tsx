import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { MobilePlaceholderPage } from "./mobile-placeholder";

describe("MobilePlaceholderPage", () => {
  it("renders the fixed foundation placeholder copy", () => {
    render(<MobilePlaceholderPage title="Issues" />);

    expect(
      screen.getByRole("heading", { name: "Issues" }),
    ).toBeInTheDocument();
    expect(screen.getByText("ここに Issues が来ます")).toBeInTheDocument();
  });
});
