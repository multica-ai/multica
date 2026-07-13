// @vitest-environment jsdom
import "@testing-library/jest-dom/vitest";
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { AppViewFrame } from "./app-view-frame";

describe("AppViewFrame", () => {
  it("keeps app views in an opaque sandbox without parent-origin access", () => {
    render(<AppViewFrame title="Approve formatting" src="https://apps.example/apps/a/1.0.0/views/approve" />);
    const frame = screen.getByTitle("Approve formatting");
    expect(frame).toHaveAttribute("sandbox", "allow-scripts allow-forms");
    expect(frame.getAttribute("sandbox")).not.toContain("allow-same-origin");
  });
});
