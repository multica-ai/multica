// @vitest-environment jsdom

import "@testing-library/jest-dom/vitest";
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { PermissionDecisionGuide } from "./workspace-settings-tab";

describe("PermissionDecisionGuide", () => {
  it("distinguishes live Permissions authoring from the frozen Task Mandate", () => {
    render(<PermissionDecisionGuide />);

    expect(screen.getByText(/Settings → Permissions is the live authoring source/i)).toBeInTheDocument();
    expect(screen.getByText(/later Deny or safety ceiling can tighten the active run/i)).toBeInTheDocument();
    expect(screen.getByText(/later Allow never widens its frozen Task Mandate/i)).toBeInTheDocument();
    expect(screen.getByText(/start a new task/i)).toBeInTheDocument();
  });
});
