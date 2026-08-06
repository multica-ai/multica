// @vitest-environment jsdom

import "@testing-library/jest-dom/vitest";
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const featureFlags = vi.hoisted(() => ({ accessDiagnostics: false }));

vi.mock("@multica/cerebro-feature-flags", () => ({
  useFeatureFlag: (key: string) =>
    key === "cerebro_access_diagnostics" ? featureFlags.accessDiagnostics : false,
}));

import { ConnectionAccessDiagnostics } from "./connection-access-diagnostics";

const diagnostics = [{
  code: "connection_discovery",
  state: "success" as const,
  title: "Connection discovery",
  message: "Discovery returned one capability.",
  source_policy: "Connection probe",
  recovery_action: "Retest after changing the Connection.",
}];

describe("ConnectionAccessDiagnostics", () => {
  beforeEach(() => {
    featureFlags.accessDiagnostics = false;
  });

  afterEach(cleanup);

  it("hides discovery diagnostics while the operator gate is off", () => {
    render(<ConnectionAccessDiagnostics diagnostics={diagnostics} />);

    expect(screen.queryByText("Connection discovery")).not.toBeInTheDocument();
  });

  it("shows discovery diagnostics when the operator gate is on", () => {
    featureFlags.accessDiagnostics = true;
    render(<ConnectionAccessDiagnostics diagnostics={diagnostics} />);

    expect(screen.getByText("Connection discovery")).toBeInTheDocument();
  });
});
