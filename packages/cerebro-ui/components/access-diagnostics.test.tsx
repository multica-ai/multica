// @vitest-environment jsdom

import "@testing-library/jest-dom/vitest";
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { AccessDiagnostics } from "./access-diagnostics";

describe("AccessDiagnostics", () => {
  it("shows the affected capability, source policy and recovery action", () => {
    render(
      <AccessDiagnostics
        diagnostics={[{
          code: "observed_denial",
          state: "denied",
          title: "Capability denied",
          message: "The live policy did not allow this call.",
          affected_capability: "connection:company-brain/search",
          source_policy: "Settings → Permissions",
          recovery_action: "Review the capability and start a new task after changing access.",
        }]}
      />,
    );

    expect(screen.getByText("connection:company-brain/search")).toBeInTheDocument();
    expect(screen.getByText("Settings → Permissions")).toBeInTheDocument();
    expect(screen.getByText(/start a new task/i)).toBeInTheDocument();
  });

  it("renders an explicit empty state", () => {
    render(<AccessDiagnostics diagnostics={[]} emptyMessage="No access diagnostics were recorded." />);
    expect(screen.getByText("No access diagnostics were recorded.")).toBeInTheDocument();
  });
});
