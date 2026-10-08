/**
 * @vitest-environment jsdom
 */
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderWithI18n } from "../test/i18n";
import { ExpandableDescription } from "./expandable-description";

// jsdom reports every element as zero-height, so overflow has to be staged.
// scrollHeight/clientHeight are the pair the component compares.
function stageOverflow(scrollHeight: number, clientHeight: number) {
  Object.defineProperty(HTMLParagraphElement.prototype, "scrollHeight", {
    configurable: true,
    get: () => scrollHeight,
  });
  Object.defineProperty(HTMLParagraphElement.prototype, "clientHeight", {
    configurable: true,
    get: () => clientHeight,
  });
}

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe("ExpandableDescription", () => {
  it("offers no toggle when the text already fits", async () => {
    stageOverflow(48, 48);
    await renderWithI18n(
      <ExpandableDescription>Short enough</ExpandableDescription>,
    );

    // A toggle under a two-line description that is only one line long is a
    // control that reveals nothing.
    expect(screen.queryByRole("button")).toBeNull();
  });

  it("clamps by default and reveals the rest on demand", async () => {
    stageOverflow(120, 48);
    await renderWithI18n(
      <ExpandableDescription>A description that runs on</ExpandableDescription>,
    );

    const text = screen.getByText("A description that runs on");
    expect(text).toHaveClass("line-clamp-2");

    await userEvent.click(screen.getByRole("button"));
    expect(text).not.toHaveClass("line-clamp-2");

    // Still offered while expanded — the measurement is skipped in that state,
    // so nothing may clear the flag and strand the reader in the long form.
    await userEvent.click(screen.getByRole("button"));
    expect(text).toHaveClass("line-clamp-2");
  });
});
