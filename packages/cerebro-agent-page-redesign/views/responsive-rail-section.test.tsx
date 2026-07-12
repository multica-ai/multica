import { expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ResponsiveRailSection } from "./responsive-rail-section";

it("lets a mobile user collapse and expand a rail section", async () => {
  const user = userEvent.setup();
  render(
    <ResponsiveRailSection title="Details">
      <span>Runtime version</span>
    </ResponsiveRailSection>,
  );

  const disclosure = screen.getByText("Details").closest("details");
  expect(disclosure).toHaveAttribute("open");
  await user.click(screen.getByText("Details"));
  expect(disclosure).not.toHaveAttribute("open");
  await user.click(screen.getByText("Details"));
  expect(disclosure).toHaveAttribute("open");
});
